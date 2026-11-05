// A localhost-only test receiver. Not a production webhook consumer:
// it does not durably deduplicate event IDs or persist business effects.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	secret := os.Getenv("GORELAY_DEV_SINK_SECRET")
	if len(secret) < 32 {
		log.Fatal("set a dev sink signing key of at least 32 characters")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 262144))
		if err != nil {
			http.Error(w, "body invalid", http.StatusBadRequest)
			return
		}
		timestamp := r.Header.Get("X-GoRelay-Timestamp")
		when, err := time.Parse(time.RFC3339, timestamp)
		if err != nil || time.Since(when) > 5*time.Minute || time.Until(when) > 5*time.Minute {
			http.Error(w, "stale timestamp", http.StatusUnauthorized)
			return
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = io.WriteString(mac, timestamp+".")
		_, _ = mac.Write(body)
		given := strings.TrimPrefix(r.Header.Get("X-GoRelay-Signature"), "v1=")
		digest, err := hex.DecodeString(given)
		if err != nil || !hmac.Equal(digest, mac.Sum(nil)) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		var event struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(body, &event); err != nil || event.ID == "" {
			http.Error(w, "invalid event", http.StatusBadRequest)
			return
		}
		attempt, err := strconv.Atoi(r.Header.Get("X-GoRelay-Attempt"))
		if err != nil || attempt < 1 {
			http.Error(w, "invalid attempt", http.StatusBadRequest)
			return
		}
		log.Printf("received event=%s type=%s attempt=%d", event.ID, event.Type, attempt)
		w.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{Addr: "127.0.0.1:9081", Handler: mux,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second}
	log.Printf("development receiver bound to 127.0.0.1:9081")
	log.Fatal(server.ListenAndServe())
}
