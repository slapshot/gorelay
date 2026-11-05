package domain

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"
)

type Status string

const (
	Pending    Status = "PENDING"
	Processing Status = "PROCESSING"
	Delivered  Status = "DELIVERED"
	RetryWait  Status = "RETRY_WAIT"
	DeadLetter Status = "DEAD_LETTER"
)

var (
	ErrNotFound     = errors.New("event not found")
	ErrConflict     = errors.New("idempotency conflict")
	ErrUnavailable  = errors.New("storage unavailable")
)

type Input struct {
	ClientID       string
	Type           string
	DestinationID  string
	Payload        json.RawMessage
	IdempotencyKey string
	Fingerprint    [sha256.Size]byte
}

type Event struct {
	ID             string
	ClientID       string
	Type           string
	DestinationID  string
	Status         Status
	Attempts       int
	CreatedAt      time.Time
	NextAttemptAt  *time.Time
	DeliveredAt    *time.Time
}

type Store interface {
	Admit(ctx Context, input Input) (event Event, created bool, err error)
	GetOwned(ctx Context, clientID string, eventID string) (Event, error)
	Ready(ctx Context) error
}

// Context makes the storage boundary explicit without linking domain to infrastructure.
type Context interface {
	Done() <-chan struct{}
	Err() error
	Deadline() (deadline time.Time, ok bool)
	Value(key any) any
}
