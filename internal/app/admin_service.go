package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/platformadmin"
)

// AdminOrgReader reads orgs across tenants.
type AdminOrgReader interface {
	ListOrgs(ctx context.Context, query string, limit, offset int) ([]AdminOrgRow, int, error)
	GetOrg(ctx context.Context, id uuid.UUID) (*AdminOrgDetail, error)
	ListMembersForOrg(ctx context.Context, orgID uuid.UUID) ([]AdminMemberRow, error)
	SuspendOrg(ctx context.Context, orgID uuid.UUID, reason string) error
	UnsuspendOrg(ctx context.Context, orgID uuid.UUID) error
	DeleteOrg(ctx context.Context, orgID uuid.UUID) error
}

// AdminUserReader reads users across tenants.
type AdminUserReader interface {
	ListUsers(ctx context.Context, query string, limit, offset int) ([]AdminUserRow, int, error)
	GetUser(ctx context.Context, id uuid.UUID) (*AdminUserDetail, error)
	RevokeAllSessions(ctx context.Context, userID uuid.UUID, now time.Time) (int64, error)
}

// AdminImpersonationStore persists impersonation sessions.
type AdminImpersonationStore interface {
	Create(ctx context.Context, s *platformadmin.ImpersonationSession) error
	GetActiveByTokenHash(ctx context.Context, hash []byte) (*platformadmin.ImpersonationSession, error)
	Revoke(ctx context.Context, id uuid.UUID, now time.Time) error
	RevokeByTokenHash(ctx context.Context, hash []byte, now time.Time) error
}

// AdminOrgRow mirrors postgres.AdminOrgRepo.OrgRow but as a domain type.
type AdminOrgRow struct {
	ID              uuid.UUID
	Name            string
	Slug            string
	Currency        string
	Email           string
	Phone           string
	MemberCount     int
	CustomerCount   int
	OrderCount      int
	TotalOrdersGMV  int64
	OutstandingGMV  int64
	CreatedAt       time.Time
	SuspendedAt     *time.Time
	SuspendedReason string
}

// AdminOrgDetail bundles the org plus its members.
type AdminOrgDetail struct {
	ID              uuid.UUID
	Name            string
	Slug            string
	Currency        string
	Timezone        string
	Email           string
	Phone           string
	Address         string
	MemberCount     int
	CustomerCount   int
	OrderCount      int
	TotalOrdersGMV  int64
	OutstandingGMV  int64
	CreatedAt       time.Time
	Members         []AdminMemberRow
	SuspendedAt     *time.Time
	SuspendedReason string
}

type AdminMemberRow struct {
	UserID        uuid.UUID
	Role          string
	Status        string
	Email         string
	Name          string
	EmailVerified bool
	CreatedAt     time.Time
}

type AdminUserRow struct {
	ID              uuid.UUID
	Email           string
	Name            string
	EmailVerified   bool
	MembershipCount int
	SessionCount    int
	CreatedAt       time.Time
}

type AdminUserDetail struct {
	ID            uuid.UUID
	Email         string
	Name          string
	EmailVerified bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Memberships   []AdminMembershipRow
	Sessions      []AdminSessionRow
}

type AdminMembershipRow struct {
	OrganizationID   uuid.UUID
	OrganizationName string
	OrganizationSlug string
	Role             string
	Status           string
	CreatedAt        time.Time
}

type AdminSessionRow struct {
	ID        uuid.UUID
	UserAgent string
	IP        string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// AdminService orchestrates admin panel operations.
type AdminService struct {
	orgs           AdminOrgReader
	users          AdminUserReader
	impersonations AdminImpersonationStore
	audit          AdminAuditWriter
	ids            IDGen
	now            func() time.Time
}

// AdminAuditWriter writes admin audit rows.
type AdminAuditWriter interface {
	Record(ctx context.Context, adminID uuid.UUID, action, targetType string, targetID uuid.UUID, metadata map[string]any, ip string) error
}

type AdminServiceDeps struct {
	Orgs           AdminOrgReader
	Users          AdminUserReader
	Impersonations AdminImpersonationStore
	Audit          AdminAuditWriter
	IDs            IDGen
	Now            func() time.Time
}

func NewAdminService(d AdminServiceDeps) *AdminService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &AdminService{
		orgs:           d.Orgs,
		users:          d.Users,
		impersonations: d.Impersonations,
		audit:          d.Audit,
		ids:            d.IDs,
		now:            d.Now,
	}
}

// Errors.
var (
	ErrAdminImpersonationInvalid = errors.New("admin: invalid impersonation session")
)

// ----- Orgs -----

func (s *AdminService) ListOrgs(ctx context.Context, query string, limit, offset int) ([]AdminOrgRow, int, error) {
	return s.orgs.ListOrgs(ctx, query, limit, offset)
}

func (s *AdminService) GetOrg(ctx context.Context, id uuid.UUID) (*AdminOrgDetail, error) {
	detail, err := s.orgs.GetOrg(ctx, id)
	if err != nil {
		return nil, err
	}
	members, err := s.orgs.ListMembersForOrg(ctx, id)
	if err != nil {
		return nil, err
	}
	detail.Members = members
	return detail, nil
}

// ----- Users -----

func (s *AdminService) ListUsers(ctx context.Context, query string, limit, offset int) ([]AdminUserRow, int, error) {
	return s.users.ListUsers(ctx, query, limit, offset)
}

func (s *AdminService) GetUser(ctx context.Context, id uuid.UUID) (*AdminUserDetail, error) {
	return s.users.GetUser(ctx, id)
}

func (s *AdminService) ForceLogoutUser(ctx context.Context, adminID uuid.UUID, userID uuid.UUID, ip string) (int64, error) {
	n, err := s.users.RevokeAllSessions(ctx, userID, s.now())
	if err != nil {
		return 0, err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, adminID, "user.sessions_revoked", "USER", userID,
			map[string]any{"sessions_revoked": n}, ip)
	}
	return n, nil
}

// ----- Impersonation -----

// ImpersonationResult is what the handler needs to set the cookie.
type ImpersonationResult struct {
	Session  *platformadmin.ImpersonationSession
	RawToken string
}

// StartImpersonation creates a new impersonation session for the given org.
func (s *AdminService) StartImpersonation(ctx context.Context, adminID uuid.UUID, orgID uuid.UUID, ip string) (*ImpersonationResult, error) {
	raw, hash, err := generateAdminToken()
	if err != nil {
		return nil, err
	}
	now := s.now()
	sess := &platformadmin.ImpersonationSession{
		ID:             s.ids.New(),
		AdminID:        adminID,
		OrganizationID: orgID,
		TokenHash:      hash,
		ExpiresAt:      now.Add(30 * time.Minute),
		CreatedAt:      now,
	}
	if err := s.impersonations.Create(ctx, sess); err != nil {
		return nil, err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, adminID, "org.impersonate_start", "ORGANIZATION", orgID, nil, ip)
	}
	return &ImpersonationResult{Session: sess, RawToken: raw}, nil
}

// EndImpersonation revokes the session identified by the raw token.
func (s *AdminService) EndImpersonation(ctx context.Context, rawToken string) error {
	hash := sha256.Sum256([]byte(rawToken))
	return s.impersonations.RevokeByTokenHash(ctx, hash[:], s.now())
}

// ResolveImpersonation reads the raw token and returns the active session.
func (s *AdminService) ResolveImpersonation(ctx context.Context, rawToken string) (*platformadmin.ImpersonationSession, error) {
	if rawToken == "" {
		return nil, ErrAdminImpersonationInvalid
	}
	hash := sha256.Sum256([]byte(rawToken))
	sess, err := s.impersonations.GetActiveByTokenHash(ctx, hash[:])
	if err != nil {
		return nil, ErrAdminImpersonationInvalid
	}
	return sess, nil
}

// generateAdminToken returns a URL-safe random token and its SHA-256 hash.
func generateAdminToken() (raw string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	return raw, sum[:], nil
}

func (s *AdminService) SuspendOrg(ctx context.Context, adminID uuid.UUID, orgID uuid.UUID, reason string, ip string) error {
	if err := s.orgs.SuspendOrg(ctx, orgID, reason); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, adminID, "org.suspend", "ORGANIZATION", orgID,
			map[string]any{"reason": reason}, ip)
	}
	return nil
}

func (s *AdminService) UnsuspendOrg(ctx context.Context, adminID uuid.UUID, orgID uuid.UUID, ip string) error {
	if err := s.orgs.UnsuspendOrg(ctx, orgID); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, adminID, "org.unsuspend", "ORGANIZATION", orgID, nil, ip)
	}
	return nil
}

func (s *AdminService) DeleteOrg(ctx context.Context, adminID uuid.UUID, orgID uuid.UUID, ip string) error {
	if err := s.orgs.DeleteOrg(ctx, orgID); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, adminID, "org.delete", "ORGANIZATION", orgID, nil, ip)
	}
	return nil
}
