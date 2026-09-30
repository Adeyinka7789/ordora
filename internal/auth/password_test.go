package auth

import (
	"strings"
	"testing"
)

func TestHashPassword_FormatAndVerify(t *testing.T) {
	pw := "correct horse battery staple"
	h, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	// Format check: starts with $argon2id$ and has 6 $-separated fields.
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("expected argon2id prefix, got %q", h)
	}
	if got := strings.Count(h, "$"); got != 5 {
		t.Fatalf("expected 5 dollar signs, got %d: %q", got, h)
	}

	// Correct password verifies.
	if err := VerifyPassword(pw, h); err != nil {
		t.Fatalf("VerifyPassword correct: %v", err)
	}

	// Wrong password fails.
	if err := VerifyPassword("wrong", h); err == nil {
		t.Fatal("VerifyPassword accepted wrong password")
	}
}

func TestHashPassword_DifferentSalts(t *testing.T) {
	pw := "same-password-here"
	h1, _ := HashPassword(pw)
	h2, _ := HashPassword(pw)
	if h1 == h2 {
		t.Fatal("same password produced identical hashes (salt not random?)")
	}
	// But both verify.
	if err := VerifyPassword(pw, h1); err != nil {
		t.Fatalf("h1 verify: %v", err)
	}
	if err := VerifyPassword(pw, h2); err != nil {
		t.Fatalf("h2 verify: %v", err)
	}
}

func TestValidatePasswordStrength(t *testing.T) {
	cases := []struct {
		name    string
		pw      string
		wantErr bool
	}{
		{"too short", "short", true},
		{"just right", "1234567890", false},
		{"empty", "", true},
		{"very long ok", strings.Repeat("a", 256), false},
		{"too long", strings.Repeat("a", 257), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePasswordStrength(c.pw)
			if (err != nil) != c.wantErr {
				t.Fatalf("got %v, wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func TestVerifyPassword_MalformedHash(t *testing.T) {
	cases := []string{
		"",
		"not-a-hash",
		"$argon2id$wrong",
		"$bcrypt$v=19$m=65536,t=3,p=2$abc$def",
	}
	for _, c := range cases {
		if err := VerifyPassword("pw", c); err == nil {
			t.Errorf("expected error for malformed hash %q", c)
		}
	}
}
