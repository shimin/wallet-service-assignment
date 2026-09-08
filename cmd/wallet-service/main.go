package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/fundingpips/wallet-service/internal/config"
	"github.com/fundingpips/wallet-service/internal/handler"
	walletnats "github.com/fundingpips/wallet-service/internal/nats"
	"github.com/fundingpips/wallet-service/internal/storage"
)

func main() {
	cfg := config.Load()

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
	defer nc.Close()
	fmt.Println("connected to nats")

	nc.Conn.Subscribe("wallet.deposit", handler.HandleDeposit(store, nc))
	nc.Conn.Subscribe("wallet.withdraw", handler.HandleWithdraw(store, nc))
	nc.Conn.Subscribe("wallet.transfer", handler.HandleTransfer(store, nc))
	nc.Conn.Subscribe("wallet.balance", handler.HandleBalance(store))

	fmt.Println("wallet-service is running")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Println("shutting down")
}
