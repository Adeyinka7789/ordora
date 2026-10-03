package platformadmin

import (
	"time"

	"github.com/google/uuid"
)

// ImpersonationSession represents an admin acting on behalf of a business.
// It expires automatically and is always attributable to the admin.
type ImpersonationSession struct {
	ID             uuid.UUID
	AdminID        uuid.UUID
	OrganizationID uuid.UUID
	TokenHash      []byte
	ExpiresAt      time.Time
	RevokedAt      *time.Time
	CreatedAt      time.Time
}

// IsActive reports whether the impersonation session is currently valid.
func (s *ImpersonationSession) IsActive(now time.Time) bool {
	return s.RevokedAt == nil && s.ExpiresAt.After(now)
}
