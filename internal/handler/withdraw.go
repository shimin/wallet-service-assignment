package handler

import (
	"context"

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

func (r WithdrawRequest) validate() error {
	if err := r.Amount.Validate(); err != nil {
		return err
	}
	return validateCurrency(r.Currency)
}

func HandleWithdraw(ctx context.Context, store *storage.Store, nc *nats.Client) natsgo.MsgHandler {
	return func(msg *natsgo.Msg) {
		var req WithdrawRequest
		if !parse(nc, msg, "withdraw", &req) {
			return
		}

		publishResult(nc, req.RequestID, "withdraw", store.Withdraw(ctx, req.RequestID, req.WalletID, req.Amount, req.Currency))
	}
}
