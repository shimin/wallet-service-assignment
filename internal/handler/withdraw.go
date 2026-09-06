package handler

import (
	"encoding/json"
	"fmt"

	"github.com/fundingpips/wallet-service/internal/nats"
	"github.com/fundingpips/wallet-service/internal/storage"
	natsgo "github.com/nats-io/nats.go"
)

type WithdrawRequest struct {
	RequestID string  `json:"request_id"`
	WalletID  string  `json:"wallet_id"`
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
}

func HandleWithdraw(store *storage.Store, nc *nats.Client) natsgo.MsgHandler {
	return func(msg *natsgo.Msg) {
		var req WithdrawRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			fmt.Println("withdraw: bad payload:", err)
			return
		}

		if err := store.Withdraw(req.WalletID, req.Amount); err != nil {
			fmt.Println("withdraw failed:", err)
			publishFailed(nc, req.RequestID, "withdraw", err.Error())
			return
		}

		publishCompleted(nc, req.RequestID, "withdraw")

		fromWallet := req.WalletID
		if err := store.RecordTransaction(req.RequestID, "withdraw", &fromWallet, nil, req.Amount, "completed"); err != nil {
			fmt.Println("withdraw: failed to record transaction:", err)
		}
	}
}
