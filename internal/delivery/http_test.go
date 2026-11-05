package delivery

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slapshot/gorelay/internal/queue"
)

func job() queue.Job {
	return queue.Job{
		ID:       "7f3b8ad0-812a-4fc1-bc02-c2ba2472bce1",
		ClientID: "demo", Type: "inventory.updated", DestinationID: "sink",
		Payload: json.RawMessage(`{"count":3}`), CreatedAt: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC),
		Attempt: 1,
	}
}

func TestSignedDelivery(t *testing.T) {
	const secret = "this-is-a-unique-test-secret-with-40-bytes"
	var seen atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Add(1)
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("wrong HTTP method/content type")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		ts := r.Header.Get("X-GoRelay-Timestamp")
		if _, err := time.Parse(time.RFC3339, ts); err != nil {
			t.Errorf("invalid timestamp: %v", err)
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(append(append([]byte(nil), []byte(ts+".")...), body...))
		expected := "v1=" + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(r.Header.Get("X-GoRelay-Signature"))) {
			t.Error("invalid HMAC")
		}
		var envelope map[string]any
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope["id"] != job().ID {
			t.Error("unstable event identifier")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sink.Close()
	d, err := New([]Target{{ID: "sink", URL: sink.URL + "/hook", Secret: []byte(secret), AllowLoopback: true}}, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	out := d.Send(context.Background(), job())
	if !out.Success || out.HTTPStatus != 204 || seen.Load() != 1 {
		t.Fatalf("delivery outcome %+v seen=%d", out, seen.Load())
	}
}

func TestRetryAndRedirectAreNotFollowed(t *testing.T) {
	var redirected atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "/secret")
			w.WriteHeader(http.StatusFound)
			return
		}
		if r.URL.Path == "/secret" {
			redirected.Add(1)
			w.WriteHeader(200)
			return
		}
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer sink.Close()
	d, err := New([]Target{
		{ID: "sink", URL: sink.URL + "/retry", Secret: []byte(strings.Repeat("x", 40)), AllowLoopback: true},
		{ID: "redirect", URL: sink.URL + "/redirect", Secret: []byte(strings.Repeat("z", 40)), AllowLoopback: true},
	}, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	r := d.Send(context.Background(), job())
	if !r.Retryable || r.HTTPStatus != 429 || r.RetryAfter != 120*time.Second {
		t.Fatalf("retry result %+v", r)
	}
	j := job()
	j.DestinationID = "redirect"
	r = d.Send(context.Background(), j)
	if r.Retryable || r.Success || r.HTTPStatus != 302 || redirected.Load() != 0 {
		t.Fatalf("redirect was followed: %+v, count=%d", r, redirected.Load())
	}
}

func TestOutboundPolicy(t *testing.T) {
	testIPs := []struct {
		ip      string
		allowed bool
	}{
		{"8.8.8.8", true}, {"127.0.0.1", false}, {"10.0.0.5", false},
		{"169.254.169.254", false}, {"100.64.0.1", false},
		{"192.0.2.2", false}, {"2001:db8::1", false},
		{"::1", false}, {"::ffff:127.0.0.1", false},
	}
	for _, tc := range testIPs {
		if got := permittedIP(net.ParseIP(tc.ip), false, "https"); got != tc.allowed {
			t.Errorf("IP %s permitted=%v want %v", tc.ip, got, tc.allowed)
		}
	}
	if !permittedIP(net.ParseIP("127.0.0.1"), true, "http") {
		t.Error("approved loopback denied")
	}
	if permittedIP(net.ParseIP("8.8.8.8"), true, "http") {
		t.Error("plain HTTP to public IP allowed")
	}
	invalid := []Target{
		{ID: "sink", URL: "http://10.0.0.1/a", Secret: []byte(strings.Repeat("x", 40)), AllowLoopback: true},
		{ID: "sink", URL: "https://username:password@example.org/", Secret: []byte(strings.Repeat("x", 40))},
		{ID: "sink", URL: "https://example.org/path?token=abc", Secret: []byte(strings.Repeat("x", 40))},
		{ID: "sink", URL: "file:///etc/passwd", Secret: []byte(strings.Repeat("x", 40))},
	}
	for _, bad := range invalid {
		_, err := New([]Target{bad}, 2*time.Second)
		if err == nil {
			t.Errorf("accepted unsafe URL %q", bad.URL)
		}
	}
	u, err := url.Parse("http://127.0.0.1:80")
	if err != nil {
		t.Fatal(err)
	}
	if u.Hostname() != "127.0.0.1" {
		t.Fatal("URL parse test broken")
	}
}
