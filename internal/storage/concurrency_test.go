package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fundingpips/wallet-service/internal/config"
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

func newWalletID() string {
	return fmt.Sprintf("%08x-0000-4000-8000-%012x", os.Getpid(), walletSeq.Add(1))
}

func newWallet(t *testing.T, s *Store, initial float64) string {
	t.Helper()
	id := newWalletID()
	t.Cleanup(func() {
		s.DB.Exec(`DELETE FROM transactions WHERE from_wallet = $1 OR to_wallet = $1`, id)
		s.DB.Exec(`DELETE FROM wallets WHERE wallet_id = $1`, id)
	})
	if err := s.Deposit(id, initial); err != nil {
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

func runRace(ops []op) int {
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	start := make(chan struct{})

	for i, o := range ops {
		wg.Add(1)
		go func(i int, o op) {
			defer wg.Done()
			<-start
			if err := o(fmt.Sprintf("req-%d", i)); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(i, o)
	}
	close(start)
	wg.Wait()
	return successes
}

func TestConcurrentWithdraws(t *testing.T) {
	s := testStore(t)
	warmPool(t, s)

	for round := range 5 {
		wallet := newWallet(t, s, 100)
		ops := make([]op, 20)
		for i := range ops {
			ops[i] = func(reqID string) error { return s.Withdraw(wallet, 100) }
		}

		successes := runRace(ops)
		balance := balanceOf(t, s, wallet)
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
				func(reqID string) error { return s.Withdraw(src, 100) },
				func(reqID string) error { return s.Transfer(src, dst, 100) },
			)
		}

		successes := runRace(ops)
		from, to := balanceOf(t, s, src), balanceOf(t, s, dst)
		if successes != 1 || from != 0 {
			t.Fatalf("round %d: want 1 op and source 0, got %d and %.4f (destination %.4f)", round, successes, from, to)
		}
		if to != 100 && to != 200 {
			t.Fatalf("round %d: want destination 100 or 200, got %.4f", round, to)
		}
	}
}

func TestTransferToMissingWallet(t *testing.T) {
	s := testStore(t)
	src := newWallet(t, s, 100)

	if err := s.Transfer(src, missingWalletID, 100); err == nil {
		t.Error("want an error transferring to a missing wallet, got nil")
	}
	if b := balanceOf(t, s, src); b != 100 {
		t.Errorf("want source untouched at 100, got %.4f", b)
	}
}
