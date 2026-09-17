package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fundingpips/wallet-service/internal/config"
	"github.com/fundingpips/wallet-service/internal/handler"
	walletnats "github.com/fundingpips/wallet-service/internal/nats"
	"github.com/fundingpips/wallet-service/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Println("config:", err)
		os.Exit(1)
	}

	store, err := storage.NewStore(cfg.PGUrl)
	if err != nil {
		fmt.Println("failed to connect to postgres:", err)
		os.Exit(1)
	}
	defer store.Close()
	fmt.Println("connected to postgres")

	nc, err := walletnats.Connect(cfg.NATSUrl)
	if err != nil {
		fmt.Println("failed to connect to nats:", err)
		os.Exit(1)
	}
	fmt.Println("connected to nats")

	work, cancel := context.WithCancel(context.Background())
	defer cancel()

	nc.Conn.Subscribe("wallet.deposit", handler.HandleDeposit(work, store, nc))
	nc.Conn.Subscribe("wallet.withdraw", handler.HandleWithdraw(work, store, nc))
	nc.Conn.Subscribe("wallet.transfer", handler.HandleTransfer(work, store, nc))
	nc.Conn.Subscribe("wallet.balance", handler.HandleBalance(work, store))

	fmt.Println("wallet-service is running")

	signals, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-signals.Done()

	fmt.Println("shutting down")
	if err := nc.Drain(5 * time.Second); err != nil {
		fmt.Println(err)
	}
}
