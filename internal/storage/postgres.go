package storage

import (
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/lib/pq"
)

var (
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrWalletNotFound    = errors.New("wallet not found")
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

func (s *Store) GetWalletBalance(walletID string) (float64, string, error) {
	var balance float64
	var currency string
	err := s.DB.QueryRow(`SELECT balance, currency FROM wallets WHERE wallet_id = $1`, walletID).Scan(&balance, &currency)
	return balance, currency, err
}

func recordTx(tx *sql.Tx, requestID, operation string, fromWallet, toWallet *string, amount float64, status string) error {
	_, err := tx.Exec(
		`INSERT INTO transactions (request_id, operation, from_wallet, to_wallet, amount, status) VALUES ($1, $2, $3, $4, $5, $6)`,
		requestID, operation, fromWallet, toWallet, amount, status,
	)
	return err
}

func (s *Store) Deposit(requestID, walletID string, amount float64) error {
	return s.inTx(func(tx *sql.Tx) error {
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

func (s *Store) Withdraw(requestID, walletID string, amount float64) error {
	return s.inTx(func(tx *sql.Tx) error {
		if err := debit(tx, walletID, amount); err != nil {
			return err
		}
		return recordTx(tx, requestID, "withdraw", &walletID, nil, amount, "completed")
	})
}

func (s *Store) Transfer(requestID, fromWallet, toWallet string, amount float64) error {
	return s.inTx(func(tx *sql.Tx) error {
		if err := debit(tx, fromWallet, amount); err != nil {
			return err
		}
		if err := credit(tx, toWallet, amount); err != nil {
			return err
		}
		return recordTx(tx, requestID, "transfer", &fromWallet, &toWallet, amount, "completed")
	})
}

func (s *Store) SumBalanceFromTransactions(walletID string) (float64, error) {
	var balance float64
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

func debit(tx *sql.Tx, walletID string, amount float64) error {
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

func credit(tx *sql.Tx, walletID string, amount float64) error {
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
