package security

import (
	"strings"
	"testing"
)

func TestAuthenticator(t *testing.T) {
	auth, err := NewAuthenticator([]Credential{
		{Client: Client{ID: "alpha", Destinations: map[string]bool{"sink": true}}, Token: strings.Repeat("a", 40)},
		{Client: Client{ID: "beta", Destinations: map[string]bool{"sink": true}}, Token: strings.Repeat("b", 40)},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, ok := auth.Authenticate(strings.Repeat("a", 40))
	if !ok || client.ID != "alpha" {
		t.Fatalf("unexpected client: %v %v", client.ID, ok)
	}
	if _, ok := auth.Authenticate("not-a-secret"); ok {
		t.Fatal("accepted invalid credential")
	}
	if _, ok := auth.Authenticate(strings.Repeat("a", 41)); ok {
		t.Fatal("accepted unknown credential")
	}
}

func TestAuthenticatorRejectsBadSetup(t *testing.T) {
	_, duplicateErr := NewAuthenticator([]Credential{
		{Client: Client{ID: "a", Destinations: map[string]bool{"sink": true}}, Token: strings.Repeat("t", 40)},
		{Client: Client{ID: "b", Destinations: map[string]bool{"sink": true}}, Token: strings.Repeat("t", 40)},
	})
	if duplicateErr == nil {
		t.Fatal("accepted duplicate client tokens")
	}
	_, err := NewAuthenticator([]Credential{
		{Client: Client{ID: "x", Destinations: map[string]bool{"sink": true}}, Token: "short"},
	})
	if err == nil {
		t.Fatal("accepted short token")
	}
	_, err = NewAuthenticator(nil)
	if err == nil {
		t.Fatal("accepted empty setup")
	}
}
