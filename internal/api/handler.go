package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/slapshot/gorelay/internal/domain"
	"github.com/slapshot/gorelay/internal/security"
	"github.com/slapshot/gorelay/internal/strictjson"
)

var (
	eventType = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)
	destID    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	uuid      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

type Server struct {
	store            domain.Store
	auth             *security.Authenticator
	maxBytes         int64
	admissionPreview bool
	draining         atomic.Bool
}

func New(store domain.Store, auth *security.Authenticator, maxBodyBytes int64, admissionPreview bool) (*Server, error) {
	if store == nil || auth == nil || maxBodyBytes < 1024 || maxBodyBytes > 1048576 {
		return nil, errors.New("invalid API configuration")
	}
	return &Server{store: store, auth: auth, maxBytes: maxBodyBytes, admissionPreview: admissionPreview}, nil
}

func (s *Server) Drain() {
	s.draining.Store(true)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		write(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if s.draining.Load() || s.store.Ready(ctx) != nil {
			write(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready"})
			return
		}
		write(w, http.StatusOK, map[string]any{"status": "ready"})
	})
	mux.HandleFunc("POST /v1/events", s.ingest)
	mux.HandleFunc("GET /v1/events/{id}", s.getEvent)
	return mux
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request, requestID string) (security.Client, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		fail(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required", requestID)
		return security.Client{}, false
	}
	client, ok := s.auth.Authenticate(strings.TrimPrefix(values[0], "Bearer "))
	if !ok {
		fail(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required", requestID)
	}
	return client, ok
}

func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	requestID := requestIdentifier(w, r)
	w.Header().Set("Cache-Control", "no-store")
	if s.draining.Load() {
		fail(w, http.StatusServiceUnavailable, "DRAINING", "Service is shutting down", requestID)
		return
	}
	client, ok := s.authorize(w, r, requestID)
	if !ok {
		return
	}
	if !s.admissionPreview {
		fail(w, http.StatusServiceUnavailable, "DELIVERY_NOT_IMPLEMENTED", "Event admission disabled until delivery workers are available", requestID)
		return
	}
	keyValues := r.Header.Values("Idempotency-Key")
	if len(keyValues) != 1 || !validKey(keyValues[0]) {
		fail(w, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Valid Idempotency-Key header required", requestID)
		return
	}
	contentType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" || len(r.Header.Values("Content-Type")) != 1 || r.Header.Get("Content-Encoding") != "" || len(params) > 1 || (len(params) == 1 && !strings.EqualFold(params["charset"], "utf-8")) {
		fail(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Expected unencoded application/json", requestID)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			fail(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "Request body exceeds limit", requestID)
		} else {
			fail(w, http.StatusBadRequest, "INVALID_BODY", "Could not read request body", requestID)
		}
		return
	}
	obj, err := strictjson.Object(body)
	if err != nil || len(obj) != 3 {
		fail(w, http.StatusBadRequest, "INVALID_JSON", "Expected type, destination_id and object payload", requestID)
		return
	}
	typ, okType := obj["type"].(string)
	dest, okDest := obj["destination_id"].(string)
	payload, okPayload := obj["payload"].(map[string]any)
	if !okType || !okDest || !okPayload || !eventType.MatchString(typ) || !destID.MatchString(dest) {
		fail(w, http.StatusBadRequest, "INVALID_EVENT", "Invalid event envelope", requestID)
		return
	}
	if !client.Destinations[dest] {
		fail(w, http.StatusForbidden, "DESTINATION_FORBIDDEN", "Destination is not authorized", requestID)
		return
	}
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		fail(w, http.StatusBadRequest, "INVALID_EVENT", "Invalid payload", requestID)
		return
	}
	// encoding/json sorts object keys. JSON number spellings remain distinct
	// (e.g. 1 vs 1.0); this rule is part of the API's fingerprint contract.
	normalized, err := json.Marshal(struct {
		Type          string          `json:"type"`
		DestinationID string          `json:"destination_id"`
		Payload       json.RawMessage `json:"payload"`
	}{typ, dest, encodedPayload})
	if err != nil {
		fail(w, http.StatusBadRequest, "INVALID_EVENT", "Invalid event", requestID)
		return
	}
	input := domain.Input{
		ClientID: client.ID, Type: typ, DestinationID: dest, Payload: encodedPayload,
		IdempotencyKey: keyValues[0], Fingerprint: sha256.Sum256(normalized),
	}
	event, created, err := s.store.Admit(r.Context(), input)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			fail(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency key used for another event", requestID)
		} else {
			fail(w, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "Event could not be committed", requestID)
		}
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusAccepted
	}
	write(w, status, map[string]any{
		"id": event.ID, "status": event.Status, "created_at": event.CreatedAt.UTC().Format(time.RFC3339Nano),
	})
}

func (s *Server) getEvent(w http.ResponseWriter, r *http.Request) {
	requestID := requestIdentifier(w, r)
	w.Header().Set("Cache-Control", "no-store")
	client, ok := s.authorize(w, r, requestID)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !uuid.MatchString(id) {
		fail(w, http.StatusNotFound, "NOT_FOUND", "Event not found", requestID)
		return
	}
	event, err := s.store.GetOwned(r.Context(), client.ID, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			fail(w, http.StatusNotFound, "NOT_FOUND", "Event not found", requestID)
		} else {
			fail(w, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "Event status unavailable", requestID)
		}
		return
	}
	write(w, http.StatusOK, struct {
		ID            string        `json:"id"`
		Type          string        `json:"type"`
		DestinationID string        `json:"destination_id"`
		Status        domain.Status `json:"status"`
		Attempts      int           `json:"attempts"`
		NextAttemptAt *time.Time    `json:"next_attempt_at"`
		CreatedAt     time.Time     `json:"created_at"`
		DeliveredAt   *time.Time    `json:"delivered_at"`
	}{event.ID, event.Type, event.DestinationID, event.Status, event.Attempts, event.NextAttemptAt, event.CreatedAt, event.DeliveredAt})
}

func validKey(key string) bool {
	if len(key) < 1 || len(key) > 200 {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 33 || key[i] > 126 {
			return false
		}
	}
	return true
}

func requestIdentifier(w http.ResponseWriter, r *http.Request) string {
	id := r.Header.Get("X-Request-ID")
	if len(id) == 0 || len(id) > 64 {
		id = ""
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			id = ""
			break
		}
	}
	if id == "" {
		var value [16]byte
		if _, err := rand.Read(value[:]); err == nil {
			id = hex.EncodeToString(value[:])
		} else {
			id = "unknown"
		}
	}
	w.Header().Set("X-Request-ID", id)
	return id
}

func fail(w http.ResponseWriter, status int, code string, message string, requestID string) {
	write(w, status, map[string]any{"error": map[string]string{
		"code": code, "message": message, "request_id": requestID,
	}})
}

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
