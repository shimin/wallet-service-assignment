package storage

import (
	"context"
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
	ErrCurrencyMismatch  = errors.New("wallet currency does not match")
)

const defaultCurrency = "USD"

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

func (s *Store) GetWalletBalance(ctx context.Context, walletID string) (money.Amount, string, error) {
	var balance money.Amount
	var currency string
	err := s.DB.QueryRowContext(ctx, `SELECT balance, currency FROM wallets WHERE wallet_id = $1`, walletID).Scan(&balance, &currency)
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
func claimRequest(ctx context.Context, tx *sql.Tx, requestID, operation, payload string) error {
	res, err := tx.ExecContext(ctx,
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
		if err := tx.QueryRowContext(ctx, `SELECT payload_fingerprint FROM requests WHERE request_id = $1`, requestID).Scan(&claimed); err != nil {
			return err
		}
		if claimed != payload {
			return ErrRequestConflict
		}
		return ErrDuplicateRequest
	}
	return nil
}

func recordTx(ctx context.Context, tx *sql.Tx, requestID, operation string, fromWallet, toWallet *string, amount money.Amount, status string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO transactions (request_id, operation, from_wallet, to_wallet, amount, status) VALUES ($1, $2, $3, $4, $5, $6)`,
		requestID, operation, fromWallet, toWallet, amount, status,
	)
	return err
}

func (s *Store) Deposit(ctx context.Context, requestID, walletID string, amount money.Amount, currency string) error {
	if err := amount.Validate(); err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := claimRequest(ctx, tx, requestID, "deposit", fingerprint("deposit", walletID, amount.String(), currency)); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO wallets (wallet_id, balance, currency)
			VALUES ($1, $2, COALESCE(NULLIF($3, ''), $4))
			ON CONFLICT (wallet_id)
			DO UPDATE SET balance = wallets.balance + EXCLUDED.balance, updated_at = NOW()
			WHERE $3 = '' OR wallets.currency = $3
		`, walletID, amount, currency, defaultCurrency)
		if err != nil {
			return err
		}
		if affected, err := res.RowsAffected(); err != nil {
			return err
		} else if affected == 0 {
			return ErrCurrencyMismatch
		}
		return recordTx(ctx, tx, requestID, "deposit", nil, &walletID, amount, "completed")
	})
}

func (s *Store) Withdraw(ctx context.Context, requestID, walletID string, amount money.Amount, currency string) error {
	if err := amount.Validate(); err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := claimRequest(ctx, tx, requestID, "withdraw", fingerprint("withdraw", walletID, amount.String(), currency)); err != nil {
			return err
		}
		if err := requireCurrency(ctx, tx, walletID, currency); err != nil {
			return err
		}
		if err := debit(ctx, tx, walletID, amount); err != nil {
			return err
		}
		return recordTx(ctx, tx, requestID, "withdraw", &walletID, nil, amount, "completed")
	})
}

func (s *Store) Transfer(ctx context.Context, requestID, fromWallet, toWallet string, amount money.Amount, currency string) error {
	if err := amount.Validate(); err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := claimRequest(ctx, tx, requestID, "transfer", fingerprint("transfer", fromWallet, toWallet, amount.String(), currency)); err != nil {
			return err
		}
		if err := lockWallets(ctx, tx, fromWallet, toWallet); err != nil {
			return err
		}
		if err := requireSameCurrency(ctx, tx, fromWallet, toWallet); err != nil {
			return err
		}
		if err := requireCurrency(ctx, tx, fromWallet, currency); err != nil {
			return err
		}
		if err := debit(ctx, tx, fromWallet, amount); err != nil {
			return err
		}
		if err := credit(ctx, tx, toWallet, amount); err != nil {
			return err
		}
		return recordTx(ctx, tx, requestID, "transfer", &fromWallet, &toWallet, amount, "completed")
	})
}

func (s *Store) SumBalanceFromTransactions(ctx context.Context, walletID string) (money.Amount, error) {
	var balance money.Amount
	err := s.DB.QueryRowContext(ctx, `
		SELECT COALESCE(
			SUM(CASE WHEN to_wallet = $1 THEN amount ELSE 0 END) -
			SUM(CASE WHEN from_wallet = $1 THEN amount ELSE 0 END),
		0)
		FROM transactions WHERE from_wallet = $1 OR to_wallet = $1
	`, walletID).Scan(&balance)
	return balance, err
}

func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func debit(ctx context.Context, tx *sql.Tx, walletID string, amount money.Amount) error {
	res, err := tx.ExecContext(ctx, `
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

func credit(ctx context.Context, tx *sql.Tx, walletID string, amount money.Amount) error {
	res, err := tx.ExecContext(ctx, `
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

func requireCurrency(ctx context.Context, tx *sql.Tx, walletID, currency string) error {
	if currency == "" {
		return nil
	}
	var actual string
	switch err := tx.QueryRowContext(ctx, `SELECT currency FROM wallets WHERE wallet_id = $1`, walletID).Scan(&actual); {
	case errors.Is(err, sql.ErrNoRows):
		return ErrWalletNotFound
	case err != nil:
		return err
	case actual != currency:
		return ErrCurrencyMismatch
	}
	return nil
}

func requireSameCurrency(ctx context.Context, tx *sql.Tx, a, b string) error {
	var distinct int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT currency) FROM wallets WHERE wallet_id IN ($1, $2)`, a, b).Scan(&distinct); err != nil {
		return err
	}
	if distinct > 1 {
		return ErrCurrencyMismatch
	}
	return nil
}

func lockWallets(ctx context.Context, tx *sql.Tx, a, b string) error {
	if a > b {
		a, b = b, a
	}
	_, err := tx.ExecContext(ctx, `SELECT 1 FROM wallets WHERE wallet_id IN ($1, $2) ORDER BY wallet_id FOR UPDATE`, a, b)
	return err
}
