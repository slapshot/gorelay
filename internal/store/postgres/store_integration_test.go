package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/slapshot/gorelay/internal/domain"
)

func TestAdmissionAndClientIsolation(t *testing.T) {
	dsn := os.Getenv("GORELAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set GORELAY_TEST_DATABASE_URL to run integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	id, err := uuidV4()
	if err != nil {
		t.Fatal(err)
	}
	client := "test-" + id
	key := "same-key"
	payload := json.RawMessage(`{"item":"foo"}`)
	in := domain.Input{ClientID: client, Type: "item.created", DestinationID: "sink", Payload: payload, IdempotencyKey: key, Fingerprint: sha256.Sum256([]byte("normalized-v1"))}
	defer func() {
		_, err := s.pool.Exec(context.Background(), "DELETE FROM events WHERE client_id=$1", client)
		if err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()

	const count = 30
	var wg sync.WaitGroup
	errs := make(chan error, count)
	events := make(chan string, count)
	created := make(chan bool, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, made, err := s.Admit(ctx, in)
			errs <- err
			events <- e.ID
			created <- made
		}()
	}
	wg.Wait()
	close(errs)
	close(events)
	close(created)
	var firstID string
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent admit: %v", err)
		}
	}
	for eventID := range events {
		if firstID == "" {
			firstID = eventID
		}
		if eventID != firstID {
			t.Errorf("different event IDs: %s / %s", firstID, eventID)
		}
	}
	var nCreated int
	for c := range created {
		if c {
			nCreated++
		}
	}
	if nCreated != 1 {
		t.Errorf("expected one insert, got %d", nCreated)
	}
	if _, err := s.GetOwned(ctx, client, firstID); err != nil {
		t.Fatalf("owner cannot read: %v", err)
	}
	if _, err := s.GetOwned(ctx, "someone-else", firstID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("isolation violation: %v", err)
	}
	in.Fingerprint = sha256.Sum256([]byte("different"))
	if _, _, err := s.Admit(ctx, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	var rows int
	err = s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM events WHERE client_id=$1", client).Scan(&rows)
	if err != nil || rows != 1 {
		t.Fatalf("expected one row, got %d %v", rows, err)
	}
}
