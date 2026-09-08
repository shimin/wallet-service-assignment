package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/fundingpips/wallet-service/internal/config"
	"golang.org/x/sync/errgroup"
)

type op func(reqID string) error

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(config.Load().PGUrl)
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

func newWallet(t *testing.T, s *Store, initial float64) string {
	t.Helper()
	id := newUUID()
	t.Cleanup(func() {
		s.DB.Exec(`DELETE FROM requests WHERE request_id IN (
			SELECT request_id FROM transactions WHERE from_wallet = $1 OR to_wallet = $1)`, id)
		s.DB.Exec(`DELETE FROM transactions WHERE from_wallet = $1 OR to_wallet = $1`, id)
		s.DB.Exec(`DELETE FROM wallets WHERE wallet_id = $1`, id)
	})
	if err := s.Deposit(newUUID(), id, initial); err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	return id
}

func balanceOf(t *testing.T, s *Store, id string) float64 {
	t.Helper()
	b, _, err := s.GetWalletBalance(id)
	if err != nil {
		t.Fatalf("read balance: %v", err)
	}
	return b
}

func checkLedger(t *testing.T, s *Store, id string) {
	t.Helper()
	balance := balanceOf(t, s, id)
	ledger, err := s.SumBalanceFromTransactions(id)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if ledger != balance {
		t.Errorf("wallet %s: ledger %.4f != balance %.4f", id, ledger, balance)
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
		wallet := newWallet(t, s, 100)
		ops := make([]op, 20)
		for i := range ops {
			ops[i] = func(reqID string) error { return s.Withdraw(reqID, wallet, 100) }
		}

		successes, _ := runRace(ops)
		balance := balanceOf(t, s, wallet)
		checkLedger(t, s, wallet)
		if successes != 1 || balance != 0 {
			t.Fatalf("round %d: want 1 withdrawal and balance 0, got %d and %.4f", round, successes, balance)
		}
	}
}

func TestWithdrawAndTransfer(t *testing.T) {
	s := testStore(t)
	warmPool(t, s)

	for round := range 5 {
		src := newWallet(t, s, 100)
		dst := newWallet(t, s, 100)
		ops := make([]op, 0, 20)
		for range 10 {
			ops = append(ops,
				func(reqID string) error { return s.Withdraw(reqID, src, 100) },
				func(reqID string) error { return s.Transfer(reqID, src, dst, 100) },
			)
		}

		successes, _ := runRace(ops)
		from, to := balanceOf(t, s, src), balanceOf(t, s, dst)
		checkLedger(t, s, src)
		checkLedger(t, s, dst)
		if successes != 1 || from != 0 {
			t.Fatalf("round %d: want 1 op and source 0, got %d and %.4f (destination %.4f)", round, successes, from, to)
		}
		if to != 100 && to != 200 {
			t.Fatalf("round %d: want destination 100 or 200, got %.4f", round, to)
		}
	}
}

func TestOppositeTransfers(t *testing.T) {
	s := testStore(t)
	warmPool(t, s)

	for round := range 3 {
		a := newWallet(t, s, 1000)
		b := newWallet(t, s, 1000)
		ops := make([]op, 0, 20)
		for range 10 {
			ops = append(ops,
				func(reqID string) error { return s.Transfer(reqID, a, b, 10) },
				func(reqID string) error { return s.Transfer(reqID, b, a, 10) },
			)
		}

		successes, firstErr := runRace(ops)
		checkLedger(t, s, a)
		checkLedger(t, s, b)
		if total := balanceOf(t, s, a) + balanceOf(t, s, b); total != 2000 {
			t.Fatalf("round %d: want total 2000, got %.4f", round, total)
		}
		if successes != len(ops) {
			t.Fatalf("round %d: want all %d transfers to succeed, got %d (first error: %v)", round, len(ops), successes, firstErr)
		}
	}
}

func TestTransferToMissingWallet(t *testing.T) {
	s := testStore(t)
	src := newWallet(t, s, 100)

	if err := s.Transfer(newUUID(), src, missingWalletID, 100); err == nil {
		t.Error("want an error transferring to a missing wallet, got nil")
	}
	if b := balanceOf(t, s, src); b != 100 {
		t.Errorf("want source untouched at 100, got %.4f", b)
	}
}
