// Package postgres provides transactional event admission and client-scoped status queries.
package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/slapshot/gorelay/internal/domain"
)

type Store struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	cfg.MaxConns = 8
	cfg.MinConns = 0
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("could not create PostgreSQL pool")
	}
	store := &Store{pool: pool}
	if err := store.Ready(ctx); err != nil {
		pool.Close()
		return nil, errors.New("PostgreSQL is unavailable or schema version is incompatible")
	}
	return store, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var version int
	if err := s.pool.QueryRow(ctx, "SELECT version FROM schema_migrations WHERE version = 1").Scan(&version); err != nil {
		return fmt.Errorf("%w: readiness failed", domain.ErrUnavailable)
	}
	return nil
}

func (s *Store) Admit(ctx context.Context, input domain.Input) (domain.Event, bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return domain.Event{}, false, fmt.Errorf("%w: begin admission", domain.ErrUnavailable)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	id, err := uuidV4()
	if err != nil {
		return domain.Event{}, false, fmt.Errorf("%w: generate ID", domain.ErrUnavailable)
	}
	var event domain.Event
	event.ID = id
	event.ClientID = input.ClientID
	event.Type = input.Type
	event.DestinationID = input.DestinationID

	err = tx.QueryRow(ctx, `
		INSERT INTO events (
			id, client_id, event_type, destination_id, payload,
			idempotency_key, request_fingerprint
		) VALUES ($1::uuid, $2, $3, $4, $5::jsonb, $6, $7)
		ON CONFLICT (client_id, idempotency_key) DO NOTHING
		RETURNING created_at
	`, id, input.ClientID, input.Type, input.DestinationID,
		string(input.Payload), input.IdempotencyKey, input.Fingerprint[:],
	).Scan(&event.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		var existingFingerprint []byte
		err = tx.QueryRow(ctx, `
			SELECT id::text, event_type, destination_id, status, attempts, created_at,
			       request_fingerprint
			FROM events
			WHERE client_id = $1 AND idempotency_key = $2
		`, input.ClientID, input.IdempotencyKey).Scan(
			&event.ID, &event.Type, &event.DestinationID, &event.Status,
			&event.Attempts, &event.CreatedAt, &existingFingerprint,
		)
		if err != nil {
			return domain.Event{}, false, fmt.Errorf("%w: conflicting admission lookup", domain.ErrUnavailable)
		}
		if !bytes.Equal(existingFingerprint, input.Fingerprint[:]) {
			return domain.Event{}, false, domain.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Event{}, false, fmt.Errorf("%w: commit duplicate admission", domain.ErrUnavailable)
		}
		return event, false, nil
	}
	if err != nil {
		return domain.Event{}, false, fmt.Errorf("%w: persist event", domain.ErrUnavailable)
	}
	event.Status = domain.Pending
	if err := tx.Commit(ctx); err != nil {
		return domain.Event{}, false, fmt.Errorf("%w: commit admission", domain.ErrUnavailable)
	}
	return event, true, nil
}

func (s *Store) GetOwned(ctx context.Context, clientID, eventID string) (domain.Event, error) {
	var event domain.Event
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, client_id, event_type, destination_id, status,
		       attempts,
		       CASE WHEN status = 'RETRY_WAIT' THEN available_at ELSE NULL END,
		       created_at, delivered_at
		FROM events WHERE id = $1::uuid AND client_id = $2
	`, eventID, clientID).Scan(
		&event.ID, &event.ClientID, &event.Type, &event.DestinationID, &event.Status,
		&event.Attempts, &event.NextAttemptAt, &event.CreatedAt, &event.DeliveredAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Event{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Event{}, fmt.Errorf("%w: read event", domain.ErrUnavailable)
	}
	return event, nil
}

func uuidV4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
