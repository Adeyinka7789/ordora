package user

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewEmail(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr error
	}{
		{"  Michael@Example.COM  ", "michael@example.com", nil},
		{"a@b.co", "a@b.co", nil},
		{"", "", ErrEmailRequired},
		{"no-at-sign", "", ErrEmailInvalid},
		{"@nodomain.com", "", ErrEmailInvalid},
		{"no@tld", "", ErrEmailInvalid},
		{"a@b..c", "", ErrEmailInvalid},
		{"a@.b.c", "", ErrEmailInvalid},
		{"a@b.", "", ErrEmailInvalid},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := NewEmail(c.in)
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("got %v, want %v", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.String() != c.want {
				t.Fatalf("got %q, want %q", got.String(), c.want)
			}
		})
	}
}

func TestNewUser(t *testing.T) {
	email, _ := NewEmail("a@b.com")
	now := time.Now()
	u, err := New(uuid.New(), email, "$argon2id$hash", "  Alice  ", now)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if u.Name != "Alice" {
		t.Fatalf("expected trimmed name, got %q", u.Name)
	}
	if u.IsEmailVerified() {
		t.Fatal("new user should not be email-verified")
	}
	u.MarkEmailVerified(now)
	if !u.IsEmailVerified() {
		t.Fatal("user should be email-verified after MarkEmailVerified")
	}
}

func TestNewUser_Validation(t *testing.T) {
	email, _ := NewEmail("a@b.com")
	if _, err := New(uuid.New(), email, "hash", "", time.Now()); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}
	if _, err := New(uuid.New(), email, "", "Alice", time.Now()); !errors.Is(err, ErrPasswordHashRequired) {
		t.Fatalf("expected ErrPasswordHashRequired, got %v", err)
	}
}
