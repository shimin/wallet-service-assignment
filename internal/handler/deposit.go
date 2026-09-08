package handler

import (
	"github.com/fundingpips/wallet-service/internal/money"
	"github.com/fundingpips/wallet-service/internal/nats"
	"github.com/fundingpips/wallet-service/internal/storage"
	natsgo "github.com/nats-io/nats.go"
)

type DepositRequest struct {
	RequestID string       `json:"request_id"`
	WalletID  string       `json:"wallet_id"`
	Amount    money.Amount `json:"amount"`
	Currency  string       `json:"currency"`
}

func (r DepositRequest) validate() error { return r.Amount.Validate() }

func HandleDeposit(store *storage.Store, nc *nats.Client) natsgo.MsgHandler {
	return func(msg *natsgo.Msg) {
		var req DepositRequest
		if !parse(nc, msg, "deposit", &req) {
			return
		}

		publishResult(nc, req.RequestID, "deposit", store.Deposit(req.RequestID, req.WalletID, req.Amount))
	}
}
