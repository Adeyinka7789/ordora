package auth

import (
	"time"

	"github.com/google/uuid"
)

// AuthToken is the domain shape of an auth_tokens row.
type AuthToken struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Kind       string
	TokenHash  []byte
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}
