package app

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAbsURL(t *testing.T) {
	cases := []struct {
		base, path, want string
	}{
		{"https://app.admtechub.com", "/password/reset?token=x", "https://app.admtechub.com/password/reset?token=x"},
		{"https://app.admtechub.com/", "/o/abc", "https://app.admtechub.com/o/abc"},
		{"https://app.admtechub.com/", "o/abc", "https://app.admtechub.com/o/abc"},
		{"", "/orders/1", "/orders/1"}, // unwired: bare path preserved
		{"https://app.admtechub.com", "", "https://app.admtechub.com"},
	}
	for _, c := range cases {
		if got := AbsURL(c.base, c.path); got != c.want {
			t.Errorf("AbsURL(%q, %q) = %q, want %q", c.base, c.path, got, c.want)
		}
	}
}

// TestTrackingTokenRoundTrip proves an email tracking link minted for one
// order verifies for exactly that order.
func TestTrackingTokenRoundTrip(t *testing.T) {
	secret := "test-secret-do-not-use-in-prod"
	id := uuid.New()
	raw, err := TrackingToken(secret, id)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if !strings.HasPrefix(raw, "o1_") {
		t.Errorf("token must carry the o1_ prefix, got %q", raw)
	}
	if strings.ContainsAny(raw, "+/=") {
		t.Errorf("token must be URL-safe without escaping, got %q", raw)
	}
	got, ok := VerifyTrackingToken(secret, raw)
	if !ok || got != id {
		t.Errorf("verify = (%v, %v), want (%v, true)", got, ok, id)
	}
}

func TestTrackingTokenRejects(t *testing.T) {
	secret := "test-secret-do-not-use-in-prod"
	id := uuid.New()
	raw, err := TrackingToken(secret, id)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	// Wrong secret.
	if _, ok := VerifyTrackingToken("other-secret", raw); ok {
		t.Error("wrong secret must not verify")
	}
	// Tampered body.
	tampered := raw[:len(raw)-2] + "AA"
	if _, ok := VerifyTrackingToken(secret, tampered); ok {
		t.Error("tampered token must not verify")
	}
	// Foreign prefix (random bearer tokens must not route here).
	if _, ok := VerifyTrackingToken(secret, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"); ok {
		t.Error("bearer-shaped token must not verify as tracking")
	}
	// Garbage and empty.
	for _, bad := range []string{"", "o1_", "o1_!!!", "o1_aGk"} {
		if _, ok := VerifyTrackingToken(secret, bad); ok {
			t.Errorf("garbage %q must not verify", bad)
		}
	}
	// Unconfigured: minting fails closed, verification fails closed.
	if _, err := TrackingToken("", id); err == nil {
		t.Error("empty secret must fail closed")
	}
	if _, ok := VerifyTrackingToken("", raw); ok {
		t.Error("empty secret must never verify")
	}
	if _, err := TrackingToken(secret, uuid.Nil); err == nil {
		t.Error("nil order must fail")
	}
}

// TestTrackingLinkAbsolute is the regression net for the password-reset
// class of bug: every emailed customer link must be absolute.
func TestTrackingLinkAbsolute(t *testing.T) {
	const base = "https://app.admtechub.com"
	link := TrackingLink(base, "test-secret", uuid.New())
	if !strings.HasPrefix(link, base+"/o/o1_") {
		t.Errorf("tracking link must be absolute under the base URL, got %q", link)
	}
	// Unconfigured links degrade to "" (templates hide the button) rather
	// than emitting a bare path.
	if got := TrackingLink("", "test-secret", uuid.New()); got != "" {
		t.Errorf("empty base must yield no link, got %q", got)
	}
	if got := TrackingLink(base, "", uuid.New()); got != "" {
		t.Errorf("empty secret must yield no link, got %q", got)
	}
}
