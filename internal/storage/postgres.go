package storage

import (
	"database/sql"
	"fmt"
	"sync"

	_ "github.com/lib/pq"
)

type Store struct {
	DB *sql.DB
	mu sync.Mutex
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

func (s *Store) RecordTransaction(requestID, operation string, fromWallet, toWallet *string, amount float64, status string) error {
	_, err := s.DB.Exec(
		`INSERT INTO transactions (request_id, operation, from_wallet, to_wallet, amount, status) VALUES ($1, $2, $3, $4, $5, $6)`,
		requestID, operation, fromWallet, toWallet, amount, status,
	)
	return err
}

func (s *Store) Deposit(walletID string, amount float64) error {
	_, err := s.DB.Exec(`
		INSERT INTO wallets (wallet_id, balance)
		VALUES ($1, $2)
		ON CONFLICT (wallet_id)
		DO UPDATE SET balance = wallets.balance + EXCLUDED.balance, updated_at = NOW()
	`, walletID, amount)
	return err
}

func (s *Store) Withdraw(walletID string, amount float64) error {
	var balance float64
	if err := s.DB.QueryRow(`SELECT balance FROM wallets WHERE wallet_id = $1`, walletID).Scan(&balance); err != nil {
		return err
	}
	if balance < amount {
		return fmt.Errorf("insufficient funds")
	}
	_, err := s.DB.Exec(`UPDATE wallets SET balance = balance - $1, updated_at = NOW() WHERE wallet_id = $2`, amount, walletID)
	return err
}

func (s *Store) Transfer(fromWallet, toWallet string, amount float64) error {
	var balance float64
	if err := s.DB.QueryRow(`SELECT balance FROM wallets WHERE wallet_id = $1`, fromWallet).Scan(&balance); err != nil {
		return err
	}
	if balance < amount {
		return fmt.Errorf("insufficient funds")
	}
	if _, err := s.DB.Exec(`UPDATE wallets SET balance = balance - $1, updated_at = NOW() WHERE wallet_id = $2`, amount, fromWallet); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`UPDATE wallets SET balance = balance + $1, updated_at = NOW() WHERE wallet_id = $2`, amount, toWallet); err != nil {
		return err
	}
	return nil
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
