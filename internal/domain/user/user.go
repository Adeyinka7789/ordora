package user

import (
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// User is a person who can log in. A user is global (not tenant-scoped);
// their membership in one or more organizations lives in organization_members.
type User struct {
	ID              uuid.UUID
	Email           Email
	PasswordHash    string // PHC-formatted Argon2id string; opaque to the domain
	Name            string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// New constructs a new User. The caller is responsible for hashing the
// password before calling this; the domain never sees plaintext passwords.
func New(id uuid.UUID, email Email, passwordHash, name string, now time.Time) (*User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameRequired
	}
	if passwordHash == "" {
		return nil, ErrPasswordHashRequired
	}
	return &User{
		ID:           id,
		Email:        email,
		PasswordHash: passwordHash,
		Name:         name,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// IsEmailVerified reports whether the user has completed email verification.
func (u *User) IsEmailVerified() bool { return u.EmailVerifiedAt != nil }

// MarkEmailVerified sets the verification timestamp.
func (u *User) MarkEmailVerified(now time.Time) {
	u.EmailVerifiedAt = &now
	u.UpdatedAt = now
}

// -----------------------------------------------------------------------------
// Email value object
// -----------------------------------------------------------------------------

// Email is a validated email address. The zero value is invalid.
type Email struct {
	value string
}

// NewEmail validates and normalizes an email address.
//
// Normalization: trim whitespace, lowercase. The lowercase form is what we
// store and what we look up. This is not RFC-perfect but is correct for the
// vast majority of real-world addresses; the DB column is CITEXT anyway, so
// case-insensitivity is enforced at both layers.
func NewEmail(s string) (Email, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return Email{}, ErrEmailRequired
	}
	if len(s) > 254 {
		return Email{}, ErrEmailTooLong
	}
	at := strings.LastIndex(s, "@")
	if at <= 0 || at == len(s)-1 {
		return Email{}, ErrEmailInvalid
	}
	local, domain := s[:at], s[at+1:]
	if local == "" || domain == "" {
		return Email{}, ErrEmailInvalid
	}
	if !strings.Contains(domain, ".") {
		return Email{}, ErrEmailInvalid
	}
	if strings.Contains(domain, "..") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return Email{}, ErrEmailInvalid
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return Email{}, ErrEmailInvalid
		}
	}
	return Email{value: s}, nil
}

// String returns the normalized address.
func (e Email) String() string { return e.value }

// IsZero reports whether the email is unset.
func (e Email) IsZero() bool { return e.value == "" }

// Equal compares two emails by their normalized form.
func (e Email) Equal(other Email) bool { return e.value == other.value }
