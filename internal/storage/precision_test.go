package storage

import "testing"

func balanceText(t *testing.T, s *Store, id string) string {
	t.Helper()
	var balance string
	if err := s.DB.QueryRow(`SELECT balance::text FROM wallets WHERE wallet_id = $1`, id).Scan(&balance); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	return balance
}

func TestSubScaleDepositIsNotRoundedUp(t *testing.T) {
	s := testStore(t)
	wallet := newWallet(t, s, toAmount("0"))

	if err := s.Deposit(newUUID(), wallet, toAmount("0.00006")); err != nil {
		t.Logf("deposit refused: %v", err)
		return
	}
	if b := balanceText(t, s, wallet); b != "0.00006" {
		t.Errorf("deposited 0.00006, wallet holds %s", b)
	}
}

func TestSubScaleWithdrawalCostsSomething(t *testing.T) {
	s := testStore(t)
	wallet := newWallet(t, s, toAmount("100"))

	if err := s.Withdraw(newUUID(), wallet, toAmount("0.00004")); err != nil {
		t.Logf("withdrawal refused: %v", err)
		return
	}
	if b := balanceText(t, s, wallet); b == "100.0000" {
		t.Errorf("withdrew 0.00004, balance is still %s", b)
	}
}

func TestLargeAmountsSurviveTheRoundTrip(t *testing.T) {
	s := testStore(t)

	for _, literal := range []string{"12345678901234.5678", "10000000000000.0001", "99999999999999.9999"} {
		wallet := newWallet(t, s, toAmount("0"))
		if err := s.Deposit(newUUID(), wallet, toAmount(literal)); err != nil {
			t.Errorf("deposit %s: %v", literal, err) // rounded past the top of the column
			continue
		}
		if b := balanceText(t, s, wallet); b != literal {
			t.Errorf("deposited %s, wallet holds %s", literal, b)
		}
	}
}
