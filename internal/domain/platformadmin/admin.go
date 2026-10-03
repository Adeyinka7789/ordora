// Package platformadmin defines the PlatformAdmin entity — the identity used
// to log into the admin panel. It is separate from the business-user identity
// in package user.
package platformadmin

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Admin is a platform administrator.
type Admin struct {
	ID            uuid.UUID
	Email         string
	PasswordHash  string
	Name          string
	TOTPSecret    string     // empty when 2FA not set up
	TOTPEnabledAt *time.Time // null when 2FA not enabled
	LastLoginAt   *time.Time
	LastLoginIP   string
	DisabledAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// IsActive reports whether the admin can log in.
func (a *Admin) IsActive() bool { return a.DisabledAt == nil }

// Has2FA reports whether TOTP is enabled.
func (a *Admin) Has2FA() bool { return a.TOTPEnabledAt != nil && a.TOTPSecret != "" }

// New constructs a new admin.
func New(id uuid.UUID, email, passwordHash, name string, now time.Time) (*Admin, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil, ErrEmailRequired
	}
	if len(email) > 254 {
		return nil, ErrEmailInvalid
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameRequired
	}
	if passwordHash == "" {
		return nil, ErrPasswordRequired
	}
	return &Admin{
		ID:           id,
		Email:        email,
		PasswordHash: passwordHash,
		Name:         name,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// RecordLogin updates last-login metadata.
func (a *Admin) RecordLogin(ip string, now time.Time) {
	a.LastLoginAt = &now
	a.LastLoginIP = ip
	a.UpdatedAt = now
}
