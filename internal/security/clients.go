package security

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"
)

type Client struct {
	ID           string
	Destinations map[string]bool
}

type Credential struct {
	Client Client
	Token  string
}

type entry struct {
	client Client
	hash   [sha256.Size]byte
}

type Authenticator struct {
	entries []entry
}

func NewAuthenticator(credentials []Credential) (*Authenticator, error) {
	if len(credentials) == 0 {
		return nil, errors.New("at least one client is required")
	}
	a := &Authenticator{}
	ids := make(map[string]bool)
	for _, c := range credentials {
		if c.Client.ID == "" || ids[c.Client.ID] || len(c.Token) < 32 || strings.TrimSpace(c.Token) != c.Token {
			return nil, errors.New("invalid or duplicate client credential")
		}
		if len(c.Client.Destinations) == 0 {
			return nil, errors.New("client has no authorized destinations")
		}
		ids[c.Client.ID] = true
		a.entries = append(a.entries, entry{client: c.Client, hash: sha256.Sum256([]byte(c.Token))})
	}
	return a, nil
}

func (a *Authenticator) Authenticate(bearer string) (Client, bool) {
	if bearer == "" || len(bearer) > 4096 {
		return Client{}, false
	}
	hash := sha256.Sum256([]byte(bearer))
	var match Client
	found := 0
	for _, e := range a.entries {
		equal := subtle.ConstantTimeCompare(hash[:], e.hash[:])
		if equal == 1 {
			match = e.client
		}
		found |= equal
	}
	return match, found == 1
}
