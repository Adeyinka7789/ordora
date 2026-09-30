package customer

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNew_Minimal(t *testing.T) {
	c, err := New(uuid.New(), uuid.New(), "Alice", "", "", "", "", time.Now())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Name != "Alice" {
		t.Fatalf("name = %q", c.Name)
	}
	if c.HasEmail() || c.HasPhone() {
		t.Fatalf("expected no email/phone, got email=%q phone=%q", c.Email, c.Phone)
	}
}

func TestNew_NameRequired(t *testing.T) {
	_, err := New(uuid.New(), uuid.New(), "   ", "", "", "", "", time.Now())
	if !errors.Is(err, ErrNameRequired) {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}
}

func TestNew_NameTooLong(t *testing.T) {
	long := strings.Repeat("a", 201)
	_, err := New(uuid.New(), uuid.New(), long, "", "", "", "", time.Now())
	if !errors.Is(err, ErrNameTooLong) {
		t.Fatalf("expected ErrNameTooLong, got %v", err)
	}
}

func TestNew_EmailNormalization(t *testing.T) {
	c, err := New(uuid.New(), uuid.New(), "A", "  ALICE@Example.COM ", "", "", "", time.Now())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Email != "alice@example.com" {
		t.Fatalf("email = %q, want alice@example.com", c.Email)
	}
}

func TestNew_EmailInvalid(t *testing.T) {
	cases := []string{
		"not-an-email",
		"@nodomain.com",
		"a@b",
		"a@.b.c",
		"a@b.",
		"a b@c.com",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, err := New(uuid.New(), uuid.New(), "A", c, "", "", "", time.Now())
			if !errors.Is(err, ErrEmailInvalid) {
				t.Fatalf("expected ErrEmailInvalid for %q, got %v", c, err)
			}
		})
	}
}

func TestNew_PhoneNormalization(t *testing.T) {
	c, err := New(uuid.New(), uuid.New(), "A", "", "  +234   803   123   4567  ", "", "", time.Now())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want := "+234 803 123 4567"
	if c.Phone != want {
		t.Fatalf("phone = %q, want %q", c.Phone, want)
	}
}

func TestNew_PhoneTooLong(t *testing.T) {
	long := strings.Repeat("1", 41)
	_, err := New(uuid.New(), uuid.New(), "A", "", long, "", "", time.Now())
	if !errors.Is(err, ErrPhoneTooLong) {
		t.Fatalf("expected ErrPhoneTooLong, got %v", err)
	}
}

func TestNew_AddressTooLong(t *testing.T) {
	long := strings.Repeat("a", 501)
	_, err := New(uuid.New(), uuid.New(), "A", "", "", long, "", time.Now())
	if !errors.Is(err, ErrAddressTooLong) {
		t.Fatalf("expected ErrAddressTooLong, got %v", err)
	}
}

func TestNew_NotesTooLong(t *testing.T) {
	long := strings.Repeat("a", 5001)
	_, err := New(uuid.New(), uuid.New(), "A", "", "", "", long, time.Now())
	if !errors.Is(err, ErrNotesTooLong) {
		t.Fatalf("expected ErrNotesTooLong, got %v", err)
	}
}

func TestUpdate(t *testing.T) {
	orgID := uuid.New()
	c, _ := New(uuid.New(), orgID, "Alice", "a@b.com", "123", "1 Main St", "note", time.Now())

	// Sleep a microsecond so UpdatedAt is measurably later.
	time.Sleep(2 * time.Millisecond)
	before := c.UpdatedAt

	if err := c.Update("Alice Smith", "", "", "2 Main St", "", time.Now()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if c.Name != "Alice Smith" {
		t.Fatalf("name = %q", c.Name)
	}
	if c.Email != "" || c.Phone != "" || c.Notes != "" {
		t.Fatalf("expected cleared fields, got %+v", c)
	}
	if c.Address != "2 Main St" {
		t.Fatalf("address = %q", c.Address)
	}
	if !c.UpdatedAt.After(before) {
		t.Fatal("UpdatedAt not advanced")
	}
	if c.OrganizationID != orgID {
		t.Fatal("org changed during update")
	}
}

func TestUpdate_NameRequired(t *testing.T) {
	c, _ := New(uuid.New(), uuid.New(), "Alice", "", "", "", "", time.Now())
	if err := c.Update("", "", "", "", "", time.Now()); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}
}
