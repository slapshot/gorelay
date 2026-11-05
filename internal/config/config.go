package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/slapshot/gorelay/internal/security"
)

type Config struct {
	ListenAddr string
	DatabaseURL string
	MaxBodyBytes int64
	Credentials []security.Credential
}

type clientFile struct {
	Clients []struct {
		ID string `json:"id"`
		TokenEnv string `json:"token_env"`
		Destinations []string `json:"destinations"`
	} `json:"clients"`
}

func Load() (Config, error) {
	cfg := Config{ListenAddr: "127.0.0.1:8092", MaxBodyBytes: 262144}
	if value := os.Getenv("GORELAY_LISTEN_ADDR"); value != "" {
		cfg.ListenAddr = value
	}
	// The first milestone intentionally binds to loopback only. Public
	// deployment requires a separate TLS/reverse proxy security review.
	host, port, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil || host != "127.0.0.1" || port == "" {
		return Config{}, errors.New("GORELAY_LISTEN_ADDR must use 127.0.0.1 and an explicit port")
	}
	if _, err := strconv.Atoi(port); err != nil {
		return Config{}, errors.New("invalid listen port")
	}
	if value := os.Getenv("GORELAY_MAX_BODY_BYTES"); value != "" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 1024 || n > 1048576 {
			return Config{}, errors.New("GORELAY_MAX_BODY_BYTES must be between 1024 and 1048576")
		}
		cfg.MaxBodyBytes = n
	}
	cfg.DatabaseURL = os.Getenv("GORELAY_DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("GORELAY_DATABASE_URL is required")
	}
	file := os.Getenv("GORELAY_CLIENTS_FILE")
	if file == "" || !filepath.IsAbs(file) {
		return Config{}, errors.New("GORELAY_CLIENTS_FILE must be an absolute path")
	}
	stat, err := os.Lstat(file)
	if err != nil {
		return Config{}, fmt.Errorf("could not stat client config: %w", err)
	}
	if !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 {
		return Config{}, errors.New("client config must be a regular file inaccessible to group/others")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return Config{}, fmt.Errorf("could not read client config: %w", err)
	}
	if len(data) > 65536 {
		return Config{}, errors.New("client config exceeds size limit")
	}
	var parsed clientFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Config{}, errors.New("invalid client config JSON")
	}
	if len(parsed.Clients) == 0 {
		return Config{}, errors.New("no producer clients configured")
	}
	seen := map[string]bool{}
	for _, c := range parsed.Clients {
		if c.ID == "" || seen[c.ID] || !strings.HasPrefix(c.TokenEnv, "GORELAY_") {
			return Config{}, errors.New("invalid client identifier or token_env")
		}
		seen[c.ID] = true
		token := os.Getenv(c.TokenEnv)
		dests := map[string]bool{}
		for _, d := range c.Destinations {
			if d == "" || dests[d] {
				return Config{}, errors.New("invalid or duplicate authorized destination")
			}
			dests[d] = true
		}
		cfg.Credentials = append(cfg.Credentials, security.Credential{
			Client: security.Client{ID: c.ID, Destinations: dests}, Token: token,
		})
	}
	if _, err := security.NewAuthenticator(cfg.Credentials); err != nil {
		return Config{}, fmt.Errorf("invalid credentials: %w", err)
	}
	return cfg, nil
}
