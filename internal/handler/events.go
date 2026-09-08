package handler

import (
	"errors"
	"fmt"
	"time"

	"github.com/fundingpips/wallet-service/internal/nats"
	"github.com/fundingpips/wallet-service/internal/storage"
)

type Event struct {
	RequestID string  `json:"request_id"`
	Operation string  `json:"operation"`
	Status    string  `json:"status"`
	Reason    *string `json:"reason"`
	Timestamp string  `json:"timestamp"`
}

// A duplicate reports the outcome of the request that already ran: completed.
func publishResult(nc *nats.Client, requestID, operation string, err error) {
	switch {
	case err == nil, errors.Is(err, storage.ErrDuplicateRequest):
		publishCompleted(nc, requestID, operation)
	default:
		fmt.Printf("%s failed: %v\n", operation, err)
		publishFailed(nc, requestID, operation, err.Error())
	}
}

func publishCompleted(nc *nats.Client, requestID, operation string) {
	nc.PublishEvent("wallet.events.completed", Event{
		RequestID: requestID,
		Operation: operation,
		Status:    "completed",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func publishFailed(nc *nats.Client, requestID, operation, reason string) {
	nc.PublishEvent("wallet.events.failed", Event{
		RequestID: requestID,
		Operation: operation,
		Status:    "failed",
		Reason:    &reason,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}
