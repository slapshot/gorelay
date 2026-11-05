package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/slapshot/gorelay/internal/queue"
)

const claimSQL = `
WITH due AS (
 SELECT id
 FROM events
 WHERE (
   (status IN ('PENDING','RETRY_WAIT') AND available_at <= now())
   OR (status='PROCESSING' AND lease_until <= now())
 )
 AND attempts < max_attempts
 ORDER BY available_at, created_at, id
 FOR UPDATE SKIP LOCKED
 LIMIT $1
)
UPDATE events AS e
SET status='PROCESSING',
    attempts=e.attempts+1,
    lease_token=gen_random_uuid(),
    lease_until=now()+($2::bigint * interval '1 millisecond'),
    updated_at=now()
FROM due
WHERE e.id=due.id
RETURNING e.id::text,e.client_id,e.event_type,e.destination_id,
          e.payload::text,e.created_at,e.attempts,e.lease_token::text
`

// Claim claims events and records their started attempt in one transaction.
// This method must never hold a transaction during outbound HTTP.
func (s *Store) Claim(ctx context.Context, limit int, lease time.Duration) ([]queue.Job, error) {
	if limit < 1 || limit > 128 || lease < 2*time.Second || lease > time.Hour {
		return nil, errors.New("invalid claim bounds")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim: %w", err)
	}
	defer rollback(tx)
	rows, err := tx.Query(ctx, claimSQL, limit, lease.Milliseconds())
	if err != nil {
		return nil, fmt.Errorf("claim events: %w", err)
	}
	var jobs []queue.Job
	for rows.Next() {
		var j queue.Job
		var payload string
		if err := rows.Scan(&j.ID, &j.ClientID, &j.Type, &j.DestinationID,
			&payload, &j.CreatedAt, &j.Attempt, &j.LeaseToken); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan claimed event: %w", err)
		}
		j.Payload = []byte(payload)
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("read claims: %w", err)
	}
	for _, j := range jobs {
		_, err = tx.Exec(ctx, `
			INSERT INTO delivery_attempts(event_id,attempt_number,lease_token)
			VALUES ($1::uuid,$2,$3::uuid)`, j.ID, j.Attempt, j.LeaseToken)
		if err != nil {
			return nil, fmt.Errorf("record claim: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claims: %w", err)
	}
	return jobs, nil
}

// Finish uses a matching, *unexpired* lease. A stale worker cannot overwrite
// a newer owner's result, even if no other worker has claimed the row yet.
func (s *Store) Finish(ctx context.Context, job queue.Job, result queue.Outcome) (bool, error) {
	if result.Kind != queue.Succeeded && result.Kind != queue.Retryable && result.Kind != queue.Permanent {
		return false, errors.New("invalid delivery outcome")
	}
	if result.HTTPStatus < 0 || result.HTTPStatus > 599 || result.Duration < 0 || result.RetryDelay < 0 {
		return false, errors.New("invalid delivery result")
	}
	if result.RetryDelay > 24*time.Hour {
		return false, errors.New("retry delay too large")
	}
	if len(result.ErrorCode) > 64 {
		return false, errors.New("error code too long")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin completion: %w", err)
	}
	defer rollback(tx)
	var finalStatus string
	err = tx.QueryRow(ctx, `
		UPDATE events
		SET status=CASE
		      WHEN $3='DELIVERED' THEN 'DELIVERED'
		      WHEN $3='RETRY_WAIT' AND attempts < max_attempts THEN 'RETRY_WAIT'
		      ELSE 'DEAD_LETTER' END,
		    available_at=CASE
		      WHEN $3='RETRY_WAIT' AND attempts < max_attempts
		      THEN now()+($4::bigint * interval '1 millisecond')
		      ELSE available_at END,
		    delivered_at=CASE WHEN $3='DELIVERED' THEN now() ELSE delivered_at END,
		    lease_token=NULL,lease_until=NULL,
		    last_http_status=NULLIF($5,0),last_error_code=NULLIF($6,''),
		    updated_at=now()
		WHERE id=$1::uuid AND lease_token=$2::uuid
		  AND status='PROCESSING' AND lease_until > now()
		RETURNING status
	`, job.ID, job.LeaseToken, string(result.Kind), result.RetryDelay.Milliseconds(),
		result.HTTPStatus, result.ErrorCode).Scan(&finalStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Rollback(ctx); err != nil {
			return false, fmt.Errorf("rollback stale result: %w", err)
		}
		return false, nil
	}
	if err != nil { return false, fmt.Errorf("update delivery: %w", err) }
	tag, err := tx.Exec(ctx, `
		UPDATE delivery_attempts
		SET finished_at=now(),outcome_code=$4,http_status=NULLIF($5,0),duration_ms=$6
		WHERE event_id=$1::uuid AND lease_token=$2::uuid AND attempt_number=$3
	`, job.ID, job.LeaseToken, job.Attempt, finalStatus, result.HTTPStatus, result.Duration.Milliseconds())
	if err != nil {
		return false, fmt.Errorf("finish audit: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return false, errors.New("missing delivery attempt audit row")
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit completion: %w", err)
	}
	return true, nil
}

// ExpireExhausted resolves interrupted attempts that reached their retry budget.
// Rows are locked before changes so concurrent claimers cannot steal them.
func (s *Store) ExpireExhausted(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 128 {
		return 0, errors.New("invalid expiration batch")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin expire: %w", err)
	}
	defer rollback(tx)
	rows, err := tx.Query(ctx, `
		WITH expired AS (
			SELECT id FROM events
			WHERE status='PROCESSING' AND lease_until <= now()
			  AND attempts >= max_attempts
			ORDER BY lease_until,id FOR UPDATE SKIP LOCKED LIMIT $1
		)
		UPDATE events AS e
		SET status='DEAD_LETTER',lease_token=NULL,lease_until=NULL,
		    last_error_code='LEASE_EXHAUSTED',updated_at=now()
		FROM expired WHERE e.id=expired.id
		RETURNING e.id::text,e.attempts
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("expire claim: %w", err)
	}
	type item struct {
		id      string
		attempt int
	}
	var expired []item
	for rows.Next() {
		var v item
		if err := rows.Scan(&v.id, &v.attempt); err != nil {
			rows.Close()
			return 0, err
		}
		expired = append(expired, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, v := range expired {
		tag, err := tx.Exec(ctx, `
			UPDATE delivery_attempts SET finished_at=now(),outcome_code='LEASE_EXHAUSTED'
			WHERE event_id=$1::uuid AND attempt_number=$2 AND finished_at IS NULL
		`, v.id, v.attempt)
		if err != nil {
			return 0, err
		}
		if tag.RowsAffected() != 1 {
			return 0, errors.New("missing expired delivery attempt")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit expiration: %w", err)
	}
	return len(expired), nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
