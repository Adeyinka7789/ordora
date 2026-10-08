package tenant

import (
	"errors"

	"github.com/google/uuid"
)

// Role is a member role within an organization.
type Role string

const (
	RoleOwner      Role = "OWNER"
	RoleAdmin      Role = "ADMIN"
	RoleManager    Role = "MANAGER"
	RoleStaff      Role = "STAFF"
	RoleAccountant Role = "ACCOUNTANT"
	RoleViewer     Role = "VIEWER"
)

func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleManager, RoleStaff, RoleAccountant, RoleViewer:
		return true
	}
	return false
}

// CanWrite reports whether the role can create or modify data (as opposed to
// read-only). The full permission matrix will come later; this is enough for
// M1.
func (r Role) CanWrite() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleManager, RoleStaff, RoleAccountant:
		return true
	}
	return false
}

// TenantScope is what every tenant-aware service and repository call carries.
// Its existence in a function signature is a promise: "this call is scoped to
// one organization, and RLS will enforce it."
//
// A TenantScope is only ever created inside the session middleware, after
// authenticating the request and loading the user's active membership. It is
// never constructed from user input.
type TenantScope struct {
	OrgID  uuid.UUID
	UserID uuid.UUID
	Role   Role
}

var ErrNoTenant = errors.New("tenant: no tenant scope in context")

// ErrReadOnly is returned when a read-only role attempts a write.
var ErrReadOnly = errors.New("tenant: role cannot modify data")

// IsZero reports whether the scope is unset.
func (s TenantScope) IsZero() bool {
	return s.OrgID == uuid.Nil && s.UserID == uuid.Nil
}

// RequireWrite returns ErrReadOnly unless the scope's role may create or
// modify data. Call at the top of every mutating service/handler path so
// VIEWER (and unknown future read-only roles) fail closed.
func (s TenantScope) RequireWrite() error {
	if s.IsZero() {
		return ErrNoTenant
	}
	if !s.Role.CanWrite() {
		return ErrReadOnly
	}
	return nil
}
