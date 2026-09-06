package handler

import (
	"encoding/json"
	"fmt"

	"github.com/fundingpips/wallet-service/internal/nats"
	"github.com/fundingpips/wallet-service/internal/storage"
	natsgo "github.com/nats-io/nats.go"
)

type TransferRequest struct {
	RequestID    string  `json:"request_id"`
	FromWalletID string  `json:"from_wallet_id"`
	ToWalletID   string  `json:"to_wallet_id"`
	Amount       float64 `json:"amount"`
	Currency     string  `json:"currency"`
}

func HandleTransfer(store *storage.Store, nc *nats.Client) natsgo.MsgHandler {
	return func(msg *natsgo.Msg) {
		var req TransferRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			fmt.Println("transfer: bad payload:", err)
			return
		}

		if err := store.Transfer(req.FromWalletID, req.ToWalletID, req.Amount); err != nil {
			fmt.Println("transfer failed:", err)
			publishFailed(nc, req.RequestID, "transfer", err.Error())
			return
		}

		publishCompleted(nc, req.RequestID, "transfer")

		if err := store.RecordTransaction(req.RequestID, "transfer", &req.FromWalletID, &req.ToWalletID, req.Amount, "completed"); err != nil {
			fmt.Println("transfer: failed to record transaction:", err)
		}
	}
}
