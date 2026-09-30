package auth

import (
	"encoding/base64"
	"testing"
)

func TestNewToken_Uniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		raw, _, err := NewToken(TokenSession)
		if err != nil {
			t.Fatalf("NewToken: %v", err)
		}
		if seen[raw] {
			t.Fatalf("duplicate token generated at iteration %d", i)
		}
		seen[raw] = true
	}
}

func TestNewToken_UrlSafe(t *testing.T) {
	raw, _, err := NewToken(TokenPasswordReset)
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if _, err := base64.RawURLEncoding.DecodeString(raw); err != nil {
		t.Fatalf("token is not URL-safe base64: %v", err)
	}
}

func TestHashToken_Deterministic(t *testing.T) {
	raw := "some-token-value"
	h1 := HashToken(raw)
	h2 := HashToken(raw)
	if string(h1) != string(h2) {
		t.Fatal("HashToken not deterministic")
	}
	if len(h1) != 32 {
		t.Fatalf("expected 32-byte SHA-256, got %d", len(h1))
	}
}

func TestTokenTTLs(t *testing.T) {
	cases := map[TokenKind]bool{
		TokenSession:       TokenSession.TTL() > 0,
		TokenEmailVerify:   TokenEmailVerify.TTL() > 0,
		TokenPasswordReset: TokenPasswordReset.TTL() > 0,
		TokenPublicOrder:   TokenPublicOrder.TTL() == 0,
	}
	for kind, ok := range cases {
		if !ok {
			t.Errorf("bad TTL for kind %s", kind)
		}
	}
}
