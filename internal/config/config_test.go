package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.json")
	if err := os.WriteFile(path, []byte(`{"clients":[{"id":"test","token_env":"GORELAY_TEST_TOKEN","destinations":["sink"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GORELAY_CLIENTS_FILE", path)
	t.Setenv("GORELAY_TEST_TOKEN", strings.Repeat("t", 40))
	t.Setenv("GORELAY_DATABASE_URL", "postgres://unused@127.0.0.1/example")
	t.Setenv("GORELAY_LISTEN_ADDR", "127.0.0.1:8092")
	cfg, err := Load()
	if err != nil { t.Fatal(err) }
	if cfg.ListenAddr != "127.0.0.1:8092" || len(cfg.Credentials) != 1 { t.Fatalf("unexpected config: %+v", cfg) }
	t.Setenv("GORELAY_LISTEN_ADDR", "0.0.0.0:8092")
	if _, err := Load(); err == nil { t.Fatal("public bind must be rejected") }
	t.Setenv("GORELAY_LISTEN_ADDR", "127.0.0.1:8092")
	if err := os.Chmod(path, 0644); err != nil { t.Fatal(err) }
	if _, err := Load(); err == nil { t.Fatal("insecure client file permissions accepted") }
}
