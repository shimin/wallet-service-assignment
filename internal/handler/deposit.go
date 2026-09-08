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

		publishResult(nc, req.RequestID, "deposit", store.Deposit(req.RequestID, req.WalletID, req.Amount))
	}
}
