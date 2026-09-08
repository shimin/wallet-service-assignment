package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/fundingpips/wallet-service/internal/money"
	_ "github.com/lib/pq"
)

var (
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrWalletNotFound    = errors.New("wallet not found")
	ErrDuplicateRequest  = errors.New("duplicate request")
	ErrRequestConflict   = errors.New("request id reused with a different payload")
)

type Store struct {
	DB *sql.DB
}

func NewStore(pgURL string) (*Store, error) {
	db, err := sql.Open("postgres", pgURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(10)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error {
	return s.DB.Close()
}

func (s *Store) GetWalletBalance(walletID string) (money.Amount, string, error) {
	var balance money.Amount
	var currency string
	err := s.DB.QueryRow(`SELECT balance, currency FROM wallets WHERE wallet_id = $1`, walletID).Scan(&balance, &currency)
	return balance, currency, err
}

// fingerprint identifies what an operation applies, so a request id reused with
// different values can be told apart from a retry of the same one.
func fingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

// claimRequest reserves the request before any balance changes, so a concurrent
// retry blocks on the primary key until the first attempt commits or rolls back.
func claimRequest(tx *sql.Tx, requestID, operation, payload string) error {
	res, err := tx.Exec(
		`INSERT INTO requests (request_id, operation, payload_fingerprint) VALUES ($1, $2, $3) ON CONFLICT (request_id) DO NOTHING`,
		requestID, operation, payload,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		var claimed string
		if err := tx.QueryRow(`SELECT payload_fingerprint FROM requests WHERE request_id = $1`, requestID).Scan(&claimed); err != nil {
			return err
		}
		if claimed != payload {
			return ErrRequestConflict
		}
		return ErrDuplicateRequest
	}
	return nil
}

func recordTx(tx *sql.Tx, requestID, operation string, fromWallet, toWallet *string, amount money.Amount, status string) error {
	_, err := tx.Exec(
		`INSERT INTO transactions (request_id, operation, from_wallet, to_wallet, amount, status) VALUES ($1, $2, $3, $4, $5, $6)`,
		requestID, operation, fromWallet, toWallet, amount, status,
	)
	return err
}

func (s *Store) Deposit(requestID, walletID string, amount money.Amount) error {
	if err := amount.Validate(); err != nil {
		return err
	}
	return s.inTx(func(tx *sql.Tx) error {
		if err := claimRequest(tx, requestID, "deposit", fingerprint("deposit", walletID, amount.String())); err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO wallets (wallet_id, balance)
			VALUES ($1, $2)
			ON CONFLICT (wallet_id)
			DO UPDATE SET balance = wallets.balance + EXCLUDED.balance, updated_at = NOW()
		`, walletID, amount); err != nil {
			return err
		}
		return recordTx(tx, requestID, "deposit", nil, &walletID, amount, "completed")
	})
}

func (s *Store) Withdraw(requestID, walletID string, amount money.Amount) error {
	if err := amount.Validate(); err != nil {
		return err
	}
	return s.inTx(func(tx *sql.Tx) error {
		if err := claimRequest(tx, requestID, "withdraw", fingerprint("withdraw", walletID, amount.String())); err != nil {
			return err
		}
		if err := debit(tx, walletID, amount); err != nil {
			return err
		}
		return recordTx(tx, requestID, "withdraw", &walletID, nil, amount, "completed")
	})
}

func (s *Store) Transfer(requestID, fromWallet, toWallet string, amount money.Amount) error {
	if err := amount.Validate(); err != nil {
		return err
	}
	return s.inTx(func(tx *sql.Tx) error {
		if err := claimRequest(tx, requestID, "transfer", fingerprint("transfer", fromWallet, toWallet, amount.String())); err != nil {
			return err
		}
		if err := lockWallets(tx, fromWallet, toWallet); err != nil {
			return err
		}
		if err := debit(tx, fromWallet, amount); err != nil {
			return err
		}
		if err := credit(tx, toWallet, amount); err != nil {
			return err
		}
		return recordTx(tx, requestID, "transfer", &fromWallet, &toWallet, amount, "completed")
	})
}

func (s *Store) SumBalanceFromTransactions(walletID string) (money.Amount, error) {
	var balance money.Amount
	err := s.DB.QueryRow(`
		SELECT COALESCE(
			SUM(CASE WHEN to_wallet = $1 THEN amount ELSE 0 END) -
			SUM(CASE WHEN from_wallet = $1 THEN amount ELSE 0 END),
		0)
		FROM transactions WHERE from_wallet = $1 OR to_wallet = $1
	`, walletID).Scan(&balance)
	return balance, err
}

func (s *Store) inTx(fn func(*sql.Tx) error) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func debit(tx *sql.Tx, walletID string, amount money.Amount) error {
	res, err := tx.Exec(`
		UPDATE wallets SET balance = balance - $1, updated_at = NOW()
		WHERE wallet_id = $2 AND balance >= $1
	`, amount, walletID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrInsufficientFunds
	}
	return nil
}

func credit(tx *sql.Tx, walletID string, amount money.Amount) error {
	res, err := tx.Exec(`
		UPDATE wallets SET balance = balance + $1, updated_at = NOW()
		WHERE wallet_id = $2
	`, amount, walletID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrWalletNotFound
	}
	return nil
}

func lockWallets(tx *sql.Tx, a, b string) error {
	if a > b {
		a, b = b, a
	}
	_, err := tx.Exec(`SELECT 1 FROM wallets WHERE wallet_id IN ($1, $2) ORDER BY wallet_id FOR UPDATE`, a, b)
	return err
}
