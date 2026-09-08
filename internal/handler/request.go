package handler

import (
	"encoding/json"
	"fmt"

	"github.com/fundingpips/wallet-service/internal/nats"
	natsgo "github.com/nats-io/nats.go"
)

type validator interface {
	validate() error
}

func parse(nc *nats.Client, msg *natsgo.Msg, operation string, req validator) bool {
	err := json.Unmarshal(msg.Data, req)
	if err == nil {
		err = req.validate()
	}
	if err == nil {
		return true
	}

	requestID := parseRequestID(msg.Data)
	if requestID == "" {
		fmt.Printf("%s: dropped payload with no request id: %v\n", operation, err)
		return false
	}
	fmt.Printf("%s rejected: %v\n", operation, err)
	publishFailed(nc, requestID, operation, err.Error())
	return false
}

func parseRequestID(data []byte) string {
	var envelope struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(data, &envelope)
	return envelope.RequestID
}
