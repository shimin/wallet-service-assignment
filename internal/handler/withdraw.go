package handler

import (
	"encoding/json"
	"fmt"

	"github.com/fundingpips/wallet-service/internal/money"
	"github.com/fundingpips/wallet-service/internal/nats"
	"github.com/fundingpips/wallet-service/internal/storage"
	natsgo "github.com/nats-io/nats.go"
)

type WithdrawRequest struct {
	RequestID string       `json:"request_id"`
	WalletID  string       `json:"wallet_id"`
	Amount    money.Amount `json:"amount"`
	Currency  string       `json:"currency"`
}

func HandleWithdraw(store *storage.Store, nc *nats.Client) natsgo.MsgHandler {
	return func(msg *natsgo.Msg) {
		var req WithdrawRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			fmt.Println("withdraw: bad payload:", err)
			return
		}

		publishResult(nc, req.RequestID, "withdraw", store.Withdraw(req.RequestID, req.WalletID, req.Amount))
	}
}
