// Package queue defines backend-independent durable delivery operations.
package queue

import (
	"context"
	"encoding/json"
	"time"
)

type Job struct {
	ID            string
	ClientID      string
	Type          string
	DestinationID string
	Payload       json.RawMessage
	CreatedAt     time.Time
	Attempt       int
	LeaseToken    string
}

type OutcomeKind string

const (
	Succeeded OutcomeKind = "DELIVERED"
	Retryable OutcomeKind = "RETRY_WAIT"
	Permanent OutcomeKind = "DEAD_LETTER"
)

type Outcome struct {
	Kind       OutcomeKind
	HTTPStatus int
	ErrorCode  string
	RetryDelay time.Duration
	Duration   time.Duration
}

// Store performs atomic work claims and fenced state changes.
// Implementations must use the storage server's clock, not the host's clock.
type Store interface {
	Claim(ctx context.Context, limit int, lease time.Duration) ([]Job, error)
	Finish(ctx context.Context, job Job, result Outcome) (bool, error)
	ExpireExhausted(ctx context.Context, limit int) (int, error)
}
