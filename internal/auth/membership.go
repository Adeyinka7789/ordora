package auth

import (
	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// Membership is a lightweight projection of organization_members joined with
// organizations, used during login to pick an active org.
type Membership struct {
	OrgID   uuid.UUID
	OrgName string
	OrgSlug string
	Role    tenant.Role
}
