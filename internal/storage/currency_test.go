package storage

import (
	"errors"
	"testing"

	"github.com/fundingpips/wallet-service/internal/money"
)

func newWalletIn(t *testing.T, s *Store, currency string, initial money.Amount) string {
	t.Helper()
	id := newWallet(t, s, initial)
	if _, err := s.DB.Exec(`UPDATE wallets SET currency = $1 WHERE wallet_id = $2`, currency, id); err != nil {
		t.Fatalf("set currency: %v", err)
	}
	return id
}

func TestTransferBetweenCurrenciesIsRefused(t *testing.T) {
	s := testStore(t)
	src := newWalletIn(t, s, "USD", toAmount("100"))
	dst := newWalletIn(t, s, "EUR", toAmount("100"))

	if err := s.Transfer(newUUID(), src, dst, toAmount("30")); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("want ErrCurrencyMismatch, got %v", err)
	}
	if from, to := balanceOf(t, s, src), balanceOf(t, s, dst); !from.Equal(toAmount("100")) || !to.Equal(toAmount("100")) {
		t.Errorf("want both wallets untouched at 100, got %s/%s", from, to)
	}
	checkLedger(t, s, src)
	checkLedger(t, s, dst)
}
