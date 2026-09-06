package handler

import (
	"encoding/json"
	"fmt"

	"github.com/fundingpips/wallet-service/internal/storage"
	natsgo "github.com/nats-io/nats.go"
)

type BalanceRequest struct {
	WalletID string `json:"wallet_id"`
}

type BalanceResponse struct {
	WalletID string  `json:"wallet_id"`
	Balance  float64 `json:"balance"`
	Currency string  `json:"currency"`
}

func HandleBalance(store *storage.Store) natsgo.MsgHandler {
	return func(msg *natsgo.Msg) {
		var req BalanceRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			fmt.Println("balance: bad payload:", err)
			return
		}

		_, currency, err := store.GetWalletBalance(req.WalletID)
		if err != nil {
			fmt.Println("balance: wallet lookup failed:", err)
			return
		}

		balance, err := store.SumBalanceFromTransactions(req.WalletID)
		if err != nil {
			fmt.Println("balance: query failed:", err)
			return
		}

		resp := BalanceResponse{
			WalletID: req.WalletID,
			Balance:  balance,
			Currency: currency,
		}
		data, _ := json.Marshal(resp)
		msg.Respond(data)
	}
}
