package handler

import (
	"time"

	"github.com/fundingpips/wallet-service/internal/nats"
)

type Event struct {
	RequestID string  `json:"request_id"`
	Operation string  `json:"operation"`
	Status    string  `json:"status"`
	Reason    *string `json:"reason"`
	Timestamp string  `json:"timestamp"`
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
