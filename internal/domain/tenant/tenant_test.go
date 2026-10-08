package tenant

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestRequireWrite_RoleMatrix(t *testing.T) {
	writers := []Role{RoleOwner, RoleAdmin, RoleManager, RoleStaff, RoleAccountant}
	for _, role := range writers {
		s := TenantScope{OrgID: uuid.New(), UserID: uuid.New(), Role: role}
		if err := s.RequireWrite(); err != nil {
			t.Errorf("%s must be able to write, got %v", role, err)
		}
		if !role.CanWrite() {
			t.Errorf("%s: CanWrite must be true", role)
		}
	}

	// VIEWER is read-only at both layers.
	s := TenantScope{OrgID: uuid.New(), UserID: uuid.New(), Role: RoleViewer}
	if RoleViewer.CanWrite() {
		t.Error("VIEWER: CanWrite must be false")
	}
	if err := s.RequireWrite(); !errors.Is(err, ErrReadOnly) {
		t.Errorf("VIEWER RequireWrite must return ErrReadOnly, got %v", err)
	}

	// Zero scope (anonymous) fails closed, never as a writer.
	if err := (TenantScope{}).RequireWrite(); !errors.Is(err, ErrNoTenant) {
		t.Errorf("zero scope must return ErrNoTenant, got %v", err)
	}
}
