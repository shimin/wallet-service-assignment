package handler

import (
	"encoding/json"
	"fmt"

	"github.com/fundingpips/wallet-service/internal/money"
	"github.com/fundingpips/wallet-service/internal/nats"
	"github.com/fundingpips/wallet-service/internal/storage"
	natsgo "github.com/nats-io/nats.go"
)

type TransferRequest struct {
	RequestID    string       `json:"request_id"`
	FromWalletID string       `json:"from_wallet_id"`
	ToWalletID   string       `json:"to_wallet_id"`
	Amount       money.Amount `json:"amount"`
	Currency     string       `json:"currency"`
}

func HandleTransfer(store *storage.Store, nc *nats.Client) natsgo.MsgHandler {
	return func(msg *natsgo.Msg) {
		var req TransferRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			fmt.Println("transfer: bad payload:", err)
			return
		}

		publishResult(nc, req.RequestID, "transfer", store.Transfer(req.RequestID, req.FromWalletID, req.ToWalletID, req.Amount))
	}
}
