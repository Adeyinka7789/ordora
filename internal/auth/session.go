package auth

import (
	"time"

	"github.com/google/uuid"
)

// Session is the domain shape of a login session. The Postgres layer stores
// and retrieves rows of this shape; the auth service reasons about it.
type Session struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	TokenHash      []byte
	OrganizationID *uuid.UUID
	UserAgent      string
	IP             string
	ExpiresAt      time.Time
	RevokedAt      *time.Time
	CreatedAt      time.Time
}

// IsActive reports whether the session is usable right now.
func (s *Session) IsActive(now time.Time) bool {
	return s.RevokedAt == nil && s.ExpiresAt.After(now)
}
