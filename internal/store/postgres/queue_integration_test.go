package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/slapshot/gorelay/internal/delivery"
	"github.com/slapshot/gorelay/internal/domain"
	"github.com/slapshot/gorelay/internal/queue"
	"github.com/slapshot/gorelay/internal/worker"
)

func queueTestStore(t *testing.T) (*Store, context.Context, string) {
	t.Helper()
	dsn := os.Getenv("GORELAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set GORELAY_TEST_DATABASE_URL for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	id, err := uuidV4()
	if err != nil {
		t.Fatal(err)
	}
	client := "queue-test-" + id
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := store.pool.Exec(cleanCtx, "DELETE FROM events WHERE client_id=$1", client); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	return store, ctx, client
}

func addQueueEvent(t *testing.T, s *Store, ctx context.Context, client, key string) domain.Event {
	t.Helper()
	input := domain.Input{
		ClientID: client, Type: "item.updated", DestinationID: "sink",
		Payload:        json.RawMessage(`{"sku":"item-a","quantity":2}`),
		IdempotencyKey: key, Fingerprint: sha256.Sum256([]byte(key)),
	}
	event, created, err := s.Admit(ctx, input)
	if err != nil || !created {
		t.Fatalf("admit: created=%t error=%v", created, err)
	}
	return event
}

func TestQueueClaimsCompletionAndFencing(t *testing.T) {
	s, ctx, client := queueTestStore(t)
	event := addQueueEvent(t, s, ctx, client, "first")
	jobs, err := s.Claim(ctx, 1, 5*time.Second)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim: jobs=%d err=%v", len(jobs), err)
	}
	first := jobs[0]
	if first.ID != event.ID || first.Attempt != 1 || first.LeaseToken == "" {
		t.Fatalf("unexpected claim: %+v", first)
	}
	if jobs, err := s.Claim(ctx, 1, 5*time.Second); err != nil || len(jobs) != 0 {
		t.Fatalf("double claim: jobs=%d err=%v", len(jobs), err)
	}
	applied, err := s.Finish(ctx, first, queue.Outcome{
		Kind: queue.Retryable, RetryDelay: time.Second, HTTPStatus: 429,
		ErrorCode: "REMOTE_RETRYABLE", Duration: 5 * time.Millisecond,
	})
	if err != nil || !applied {
		t.Fatalf("retry completion: %t %v", applied, err)
	}
	state, err := s.GetOwned(ctx, client, event.ID)
	if err != nil || state.Status != domain.RetryWait || state.NextAttemptAt == nil {
		t.Fatalf("retry status: %+v %v", state, err)
	}
	_, err = s.pool.Exec(ctx, "UPDATE events SET available_at=now()-interval '1 second' WHERE id=$1::uuid", event.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err = s.Claim(ctx, 1, 5*time.Second)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("retry claim: %d %v", len(jobs), err)
	}
	second := jobs[0]
	if second.Attempt != 2 || second.LeaseToken == first.LeaseToken {
		t.Fatalf("lease was not rotated: %+v", second)
	}
	applied, err = s.Finish(ctx, first, queue.Outcome{Kind: queue.Succeeded, HTTPStatus: 204})
	if err != nil || applied {
		t.Fatalf("stale worker overwrote outcome: %t %v", applied, err)
	}
	applied, err = s.Finish(ctx, second, queue.Outcome{Kind: queue.Succeeded, HTTPStatus: 204})
	if err != nil || !applied {
		t.Fatalf("successful completion: %t %v", applied, err)
	}
	state, err = s.GetOwned(ctx, client, event.ID)
	if err != nil || state.Status != domain.Delivered || state.DeliveredAt == nil {
		t.Fatalf("delivered state: %+v %v", state, err)
	}
	var attempts int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM delivery_attempts WHERE event_id=$1::uuid AND finished_at IS NOT NULL", event.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("expected two completed audit rows, got %d", attempts)
	}
}

func TestQueueLeaseRecoveryAndExhaustion(t *testing.T) {
	s, ctx, client := queueTestStore(t)
	event := addQueueEvent(t, s, ctx, client, "recover")
	_, err := s.pool.Exec(ctx, "UPDATE events SET max_attempts=2 WHERE id=$1::uuid", event.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Claim(ctx, 1, 5*time.Second)
	if err != nil || len(first) != 1 {
		t.Fatalf("claim: %v / %d", err, len(first))
	}
	_, err = s.pool.Exec(ctx, "UPDATE events SET lease_until=now()-interval '1 second' WHERE id=$1::uuid", event.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Claim(ctx, 1, 5*time.Second)
	if err != nil || len(second) != 1 || second[0].Attempt != 2 {
		t.Fatalf("reclaim: %v / %+v", err, second)
	}
	_, err = s.pool.Exec(ctx, "UPDATE events SET lease_until=now()-interval '1 second' WHERE id=$1::uuid", event.ID)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := s.ExpireExhausted(ctx, 16)
	if err != nil || expired != 1 {
		t.Fatalf("expire exhausted: %d %v", expired, err)
	}
	state, err := s.GetOwned(ctx, client, event.ID)
	if err != nil || state.Status != domain.DeadLetter {
		t.Fatalf("dead-letter: %+v %v", state, err)
	}
	if jobs, err := s.Claim(ctx, 1, 5*time.Second); err != nil || len(jobs) != 0 {
		t.Fatalf("exhausted was reclaimed: %d %v", len(jobs), err)
	}
	applied, err := s.Finish(ctx, second[0], queue.Outcome{Kind: queue.Succeeded, HTTPStatus: 200})
	if err != nil || applied {
		t.Fatalf("expired worker committed: %t %v", applied, err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM delivery_attempts WHERE event_id=$1::uuid", event.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatal(fmt.Sprintf("expected 2 attempt rows, got %d", count))
	}
}

func TestQueueInvalidOutcome(t *testing.T) {
	s, ctx, _ := queueTestStore(t)
	_, err := s.Finish(ctx, queue.Job{ID: "invalid"}, queue.Outcome{Kind: "UNKNOWN"})
	if err == nil {
		t.Fatal("invalid outcome accepted")
	}
}

func TestEndToEndDeliveryToLocalSink(t *testing.T) {
	s, ctx, client := queueTestStore(t)
	received := make(chan string, 2)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if r.Header.Get("X-GoRelay-Signature") == "" {
			t.Error("unsigned delivery")
		}
		var event struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(body, &event); err != nil {
			t.Error(err)
		}
		select {
		case received <- event.ID:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sink.Close()
	dispatcher, err := delivery.New([]delivery.Target{
		{ID: "sink", URL: sink.URL + "/events", AllowLoopback: true,
			Secret: []byte(strings.Repeat("k", 40))},
	}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer dispatcher.Close()
	pool, err := worker.New(s, dispatcher, worker.Config{
		Concurrency: 1, HTTPTimeout: 2 * time.Second,
		Lease: 9 * time.Second, Poll: 200 * time.Millisecond,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	event := addQueueEvent(t, s, ctx, client, "e2e")
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { pool.Run(runCtx); close(done) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("worker did not stop")
		}
	}()

	select {
	case eventID := <-received:
		if eventID != event.ID {
			t.Fatalf("wrong event delivered %s", eventID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("local sink did not receive an event")
	}
	deadline := time.Now().Add(4 * time.Second)
	for {
		state, err := s.GetOwned(ctx, client, event.ID)
		if err != nil {
			t.Fatal(err)
		}
		if state.Status == domain.Delivered {
			if state.Attempts != 1 {
				t.Fatalf("unexpected attempts %d", state.Attempts)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("event stuck at %s", state.Status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
