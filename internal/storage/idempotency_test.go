package storage

import (
	"errors"
	"testing"
)

func txCount(t *testing.T, s *Store, requestID string) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM transactions WHERE request_id = $1`, requestID).Scan(&n); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	return n
}

func TestRetriedRequestAppliedOnce(t *testing.T) {
	s := testStore(t)

	t.Run("deposit", func(t *testing.T) {
		wallet := newWallet(t, s, toAmount("100"))
		reqID := newUUID()

		if err := s.Deposit(t.Context(), reqID, wallet, toAmount("50"), ""); err != nil {
			t.Fatalf("first deposit: %v", err)
		}
		if err := s.Deposit(t.Context(), reqID, wallet, toAmount("50"), ""); err != nil {
			t.Logf("retry rejected: %v", err)
		}

		if b := balanceOf(t, s, wallet); !b.Equal(toAmount("150")) {
			t.Errorf("want balance 150 after a retried deposit, got %s", b)
		}
		if n := txCount(t, s, reqID); n != 1 {
			t.Errorf("want 1 ledger row for the request, got %d", n)
		}
		checkLedger(t, s, wallet)
	})

	t.Run("withdraw", func(t *testing.T) {
		wallet := newWallet(t, s, toAmount("100"))
		reqID := newUUID()

		if err := s.Withdraw(t.Context(), reqID, wallet, toAmount("40"), ""); err != nil {
			t.Fatalf("first withdraw: %v", err)
		}
		if err := s.Withdraw(t.Context(), reqID, wallet, toAmount("40"), ""); err != nil {
			t.Logf("retry rejected: %v", err)
		}

		if b := balanceOf(t, s, wallet); !b.Equal(toAmount("60")) {
			t.Errorf("want balance 60 after a retried withdrawal, got %s", b)
		}
		if n := txCount(t, s, reqID); n != 1 {
			t.Errorf("want 1 ledger row for the request, got %d", n)
		}
		checkLedger(t, s, wallet)
	})

	t.Run("transfer", func(t *testing.T) {
		src, dst := newWallet(t, s, toAmount("100")), newWallet(t, s, toAmount("100"))
		reqID := newUUID()

		if err := s.Transfer(t.Context(), reqID, src, dst, toAmount("30"), ""); err != nil {
			t.Fatalf("first transfer: %v", err)
		}
		if err := s.Transfer(t.Context(), reqID, src, dst, toAmount("30"), ""); err != nil {
			t.Logf("retry rejected: %v", err)
		}

		if from, to := balanceOf(t, s, src), balanceOf(t, s, dst); !from.Equal(toAmount("70")) || !to.Equal(toAmount("130")) {
			t.Errorf("want 70/130 after a retried transfer, got %s/%s", from, to)
		}
		if n := txCount(t, s, reqID); n != 1 {
			t.Errorf("want 1 ledger row for the request, got %d", n)
		}
		checkLedger(t, s, src)
		checkLedger(t, s, dst)
	})
}

func TestReusedRequestIDWithDifferentPayload(t *testing.T) {
	s := testStore(t)

	t.Run("amount", func(t *testing.T) {
		wallet := newWallet(t, s, toAmount("100"))
		reqID := newUUID()

		if err := s.Deposit(t.Context(), reqID, wallet, toAmount("50"), ""); err != nil {
			t.Fatalf("first deposit: %v", err)
		}
		if err := s.Deposit(t.Context(), reqID, wallet, toAmount("70"), ""); !errors.Is(err, ErrRequestConflict) {
			t.Fatalf("want ErrRequestConflict, got %v", err)
		}
		if b := balanceOf(t, s, wallet); !b.Equal(toAmount("150")) {
			t.Errorf("want balance 150, got %s", b)
		}
	})

	t.Run("wallet", func(t *testing.T) {
		src, dst := newWallet(t, s, toAmount("100")), newWallet(t, s, toAmount("100"))
		reqID := newUUID()

		if err := s.Transfer(t.Context(), reqID, src, dst, toAmount("30"), ""); err != nil {
			t.Fatalf("first transfer: %v", err)
		}
		if err := s.Transfer(t.Context(), reqID, dst, src, toAmount("30"), ""); !errors.Is(err, ErrRequestConflict) {
			t.Fatalf("want ErrRequestConflict, got %v", err)
		}
		if from, to := balanceOf(t, s, src), balanceOf(t, s, dst); !from.Equal(toAmount("70")) || !to.Equal(toAmount("130")) {
			t.Errorf("want 70/130, got %s/%s", from, to)
		}
	})

	t.Run("operation", func(t *testing.T) {
		wallet := newWallet(t, s, toAmount("100"))
		reqID := newUUID()

		if err := s.Deposit(t.Context(), reqID, wallet, toAmount("50"), ""); err != nil {
			t.Fatalf("first deposit: %v", err)
		}
		if err := s.Withdraw(t.Context(), reqID, wallet, toAmount("50"), ""); !errors.Is(err, ErrRequestConflict) {
			t.Fatalf("want ErrRequestConflict, got %v", err)
		}
		if b := balanceOf(t, s, wallet); !b.Equal(toAmount("150")) {
			t.Errorf("want balance 150, got %s", b)
		}
	})
}

func TestConcurrentRetriesOfSameTransfer(t *testing.T) {
	s := testStore(t)
	warmPool(t, s)

	for round := range 5 {
		src, dst := newWallet(t, s, toAmount("1000")), newWallet(t, s, toAmount("1000"))
		reqID := newUUID()
		ops := make([]op, 20)
		for i := range ops {
			ops[i] = func(string) error { return s.Transfer(t.Context(), reqID, src, dst, toAmount("100"), "") }
		}

		successes, _ := runRace(ops)
		from, to := balanceOf(t, s, src), balanceOf(t, s, dst)
		checkLedger(t, s, src)
		checkLedger(t, s, dst)
		if successes != 1 || !from.Equal(toAmount("900")) || !to.Equal(toAmount("1100")) {
			t.Fatalf("round %d: want 1 transfer and 900/1100, got %d and %s/%s", round, successes, from, to)
		}
		if n := txCount(t, s, reqID); n != 1 {
			t.Fatalf("round %d: want 1 ledger row for the request, got %d", round, n)
		}
	}
}

func TestConcurrentRetriesOfSameRequest(t *testing.T) {
	s := testStore(t)
	warmPool(t, s)

	for round := range 5 {
		wallet := newWallet(t, s, toAmount("1000"))
		reqID := newUUID()
		ops := make([]op, 20)
		for i := range ops {
			ops[i] = func(string) error { return s.Withdraw(t.Context(), reqID, wallet, toAmount("100"), "") }
		}

		successes, _ := runRace(ops)
		balance := balanceOf(t, s, wallet)
		checkLedger(t, s, wallet)
		if successes != 1 || !balance.Equal(toAmount("900")) {
			t.Fatalf("round %d: want 1 withdrawal and balance 900, got %d and %s", round, successes, balance)
		}
		if n := txCount(t, s, reqID); n != 1 {
			t.Fatalf("round %d: want 1 ledger row for the request, got %d", round, n)
		}
	}
}
