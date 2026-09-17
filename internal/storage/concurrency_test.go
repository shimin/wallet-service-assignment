package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/fundingpips/wallet-service/internal/money"
	"golang.org/x/sync/errgroup"
)

type op func(reqID string) error

func toAmount(literal string) money.Amount { return money.MustParse(literal) }

// Defaults to what docker-compose serves; override with PG_URL.
func pgURL() string {
	if v := os.Getenv("PG_URL"); v != "" {
		return v
	}
	return "postgres://walletuser:walletpass@localhost:5432/wallet?sslmode=disable"
}

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(pgURL())
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func warmPool(t *testing.T, s *Store) {
	t.Helper()
	n := s.DB.Stats().MaxOpenConnections
	conns := make([]*sql.Conn, 0, n)
	for range n {
		c, err := s.DB.Conn(context.Background())
		if err != nil {
			t.Fatalf("warm pool: %v", err)
		}
		conns = append(conns, c)
	}
	for _, c := range conns {
		c.Close()
	}
}

const missingWalletID = "ffffffff-ffff-4fff-8fff-ffffffffffff"

var walletSeq atomic.Uint64

func newUUID() string {
	return fmt.Sprintf("%08x-0000-4000-8000-%012x", os.Getpid(), walletSeq.Add(1))
}

func newWallet(t *testing.T, s *Store, initial money.Amount) string {
	t.Helper()
	id := newUUID()
	t.Cleanup(func() {
		s.DB.Exec(`DELETE FROM requests WHERE request_id IN (
			SELECT request_id FROM transactions WHERE from_wallet = $1 OR to_wallet = $1)`, id)
		s.DB.Exec(`DELETE FROM transactions WHERE from_wallet = $1 OR to_wallet = $1`, id)
		s.DB.Exec(`DELETE FROM wallets WHERE wallet_id = $1`, id)
	})
	if initial.IsZero() {
		if _, err := s.DB.Exec(`INSERT INTO wallets (wallet_id, balance) VALUES ($1, 0)`, id); err != nil {
			t.Fatalf("create wallet: %v", err)
		}
		return id
	}
	if err := s.Deposit(t.Context(), newUUID(), id, initial, ""); err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	return id
}

func balanceOf(t *testing.T, s *Store, id string) money.Amount {
	t.Helper()
	b, _, err := s.GetWalletBalance(t.Context(), id)
	if err != nil {
		t.Fatalf("read balance: %v", err)
	}
	return b
}

func checkLedger(t *testing.T, s *Store, id string) {
	t.Helper()
	balance := balanceOf(t, s, id)
	ledger, err := s.SumBalanceFromTransactions(t.Context(), id)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if !ledger.Equal(balance) {
		t.Errorf("wallet %s: ledger %s != balance %s", id, ledger, balance)
	}
}

func runRace(ops []op) (int, error) {
	var g errgroup.Group
	var successes atomic.Int64
	start := make(chan struct{})

	for _, o := range ops {
		g.Go(func() error {
			<-start
			if err := o(newUUID()); err != nil {
				return err
			}
			successes.Add(1)
			return nil
		})
	}
	close(start)
	err := g.Wait()
	return int(successes.Load()), err
}

func TestConcurrentWithdraws(t *testing.T) {
	s := testStore(t)
	warmPool(t, s)

	for round := range 5 {
		wallet := newWallet(t, s, toAmount("100"))
		ops := make([]op, 20)
		for i := range ops {
			ops[i] = func(reqID string) error { return s.Withdraw(t.Context(), reqID, wallet, toAmount("100"), "") }
		}

		successes, _ := runRace(ops)
		balance := balanceOf(t, s, wallet)
		checkLedger(t, s, wallet)
		if successes != 1 || !balance.IsZero() {
			t.Fatalf("round %d: want 1 withdrawal and balance 0, got %d and %s", round, successes, balance)
		}
	}
}

func TestWithdrawAndTransfer(t *testing.T) {
	s := testStore(t)
	warmPool(t, s)

	for round := range 5 {
		src := newWallet(t, s, toAmount("100"))
		dst := newWallet(t, s, toAmount("100"))
		ops := make([]op, 0, 20)
		for range 10 {
			ops = append(ops,
				func(reqID string) error { return s.Withdraw(t.Context(), reqID, src, toAmount("100"), "") },
				func(reqID string) error { return s.Transfer(t.Context(), reqID, src, dst, toAmount("100"), "") },
			)
		}

		successes, _ := runRace(ops)
		from, to := balanceOf(t, s, src), balanceOf(t, s, dst)
		checkLedger(t, s, src)
		checkLedger(t, s, dst)
		if successes != 1 || !from.IsZero() {
			t.Fatalf("round %d: want 1 op and source 0, got %d and %s (destination %s)", round, successes, from, to)
		}
		if !to.Equal(toAmount("100")) && !to.Equal(toAmount("200")) {
			t.Fatalf("round %d: want destination 100 or 200, got %s", round, to)
		}
	}
}

func TestOppositeTransfers(t *testing.T) {
	s := testStore(t)
	warmPool(t, s)

	for round := range 3 {
		a := newWallet(t, s, toAmount("1000"))
		b := newWallet(t, s, toAmount("1000"))
		ops := make([]op, 0, 20)
		for range 10 {
			ops = append(ops,
				func(reqID string) error { return s.Transfer(t.Context(), reqID, a, b, toAmount("10"), "") },
				func(reqID string) error { return s.Transfer(t.Context(), reqID, b, a, toAmount("10"), "") },
			)
		}

		successes, firstErr := runRace(ops)
		checkLedger(t, s, a)
		checkLedger(t, s, b)
		if total := balanceOf(t, s, a).Add(balanceOf(t, s, b)); !total.Equal(toAmount("2000")) {
			t.Fatalf("round %d: want total 2000, got %s", round, total)
		}
		if successes != len(ops) {
			t.Fatalf("round %d: want all %d transfers to succeed, got %d (first error: %v)", round, len(ops), successes, firstErr)
		}
	}
}

func TestTransferToMissingWallet(t *testing.T) {
	s := testStore(t)
	src := newWallet(t, s, toAmount("100"))

	if err := s.Transfer(t.Context(), newUUID(), src, missingWalletID, toAmount("100"), ""); err == nil {
		t.Error("want an error transferring to a missing wallet, got nil")
	}
	if b := balanceOf(t, s, src); !b.Equal(toAmount("100")) {
		t.Errorf("want source untouched at 100, got %s", b)
	}
}
