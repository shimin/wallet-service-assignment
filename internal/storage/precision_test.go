package storage

import (
	"errors"
	"testing"

	"github.com/fundingpips/wallet-service/internal/money"
)

func balanceText(t *testing.T, s *Store, id string) string {
	t.Helper()
	var balance string
	if err := s.DB.QueryRow(`SELECT balance::text FROM wallets WHERE wallet_id = $1`, id).Scan(&balance); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	return balance
}

func TestDepositFinerThanScaleIsRefused(t *testing.T) {
	s := testStore(t)
	wallet := newWallet(t, s, toAmount("0"))

	if err := s.Deposit(t.Context(), newUUID(), wallet, toAmount("0.00006"), ""); !errors.Is(err, money.ErrScale) {
		t.Fatalf("want ErrScale, got %v", err)
	}
	if b := balanceText(t, s, wallet); b != "0.0000" {
		t.Errorf("deposit refused, wallet holds %s", b)
	}
	checkLedger(t, s, wallet)
}

func TestWithdrawalFinerThanScaleIsRefused(t *testing.T) {
	s := testStore(t)
	wallet := newWallet(t, s, toAmount("100"))

	if err := s.Withdraw(t.Context(), newUUID(), wallet, toAmount("0.00004"), ""); !errors.Is(err, money.ErrScale) {
		t.Fatalf("want ErrScale, got %v", err)
	}
	if b := balanceText(t, s, wallet); b != "100.0000" {
		t.Errorf("withdrawal refused, wallet holds %s", b)
	}
	checkLedger(t, s, wallet)
}

func TestLargeAmountsSurviveTheRoundTrip(t *testing.T) {
	s := testStore(t)

	for _, literal := range []string{"12345678901234.5678", "10000000000000.0001", "99999999999999.9999"} {
		wallet := newWallet(t, s, toAmount("0"))
		if err := s.Deposit(t.Context(), newUUID(), wallet, toAmount(literal), ""); err != nil {
			t.Errorf("deposit %s: %v", literal, err)
			continue
		}
		if b := balanceText(t, s, wallet); b != literal {
			t.Errorf("deposited %s, wallet holds %s", literal, b)
		}
		checkLedger(t, s, wallet)
	}
}

func TestSmallestAmountsAccumulateExactly(t *testing.T) {
	s := testStore(t)
	wallet := newWallet(t, s, toAmount("0.0001"))

	for range 9 {
		if err := s.Deposit(t.Context(), newUUID(), wallet, toAmount("0.0001"), ""); err != nil {
			t.Fatalf("deposit: %v", err)
		}
	}

	if b := balanceText(t, s, wallet); b != "0.0010" {
		t.Errorf("want 0.0010 after ten deposits of 0.0001, got %s", b)
	}
	checkLedger(t, s, wallet)
}
