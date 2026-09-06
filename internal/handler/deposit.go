package handler

import (
	"encoding/json"
	"fmt"

	"github.com/fundingpips/wallet-service/internal/nats"
	"github.com/fundingpips/wallet-service/internal/storage"
	natsgo "github.com/nats-io/nats.go"
)

type DepositRequest struct {
	RequestID string  `json:"request_id"`
	WalletID  string  `json:"wallet_id"`
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
}

func HandleDeposit(store *storage.Store, nc *nats.Client) natsgo.MsgHandler {
	return func(msg *natsgo.Msg) {
		var req DepositRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			fmt.Println("deposit: bad payload:", err)
			return
		}

		if err := store.Deposit(req.WalletID, req.Amount); err != nil {
			fmt.Println("deposit failed:", err)
			publishFailed(nc, req.RequestID, "deposit", err.Error())
			return
		}

		publishCompleted(nc, req.RequestID, "deposit")

		toWallet := req.WalletID
		if err := store.RecordTransaction(req.RequestID, "deposit", nil, &toWallet, req.Amount, "completed"); err != nil {
			fmt.Println("deposit: failed to record transaction:", err)
		}
	}
}
