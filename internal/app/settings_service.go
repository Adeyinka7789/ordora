package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/domain/audit"
	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/domain/user"
)

// OrgStore is the persistence contract for organizations.
type OrgStore interface {
	CreateTx(ctx context.Context, tx pgx.Tx, o *org.Organization) error
	Update(ctx context.Context, scope tenant.TenantScope, o *org.Organization) error
	UpdateLogoKey(ctx context.Context, scope tenant.TenantScope, key string, now time.Time) error
	GetTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*org.Organization, error)
}

// UserWriter is the write side of the user repo for profile changes.
type UserWriter interface {
	UpdateName(ctx context.Context, id uuid.UUID, name string, now time.Time) error
	UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string, now time.Time) error
	GetByID(ctx context.Context, id uuid.UUID) (*user.User, error)
}

// SessionAdmin lists and revokes sessions.
type SessionAdmin interface {
	ListActiveForUser(ctx context.Context, userID uuid.UUID) ([]*auth.Session, error)
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, now time.Time) error
	RevokeOthersForUser(ctx context.Context, userID, exceptSessionID uuid.UUID, now time.Time) error
	Revoke(ctx context.Context, sessionID uuid.UUID, now time.Time) error
}

// SettingsService handles org settings and user profile mutations.
type SettingsService struct {
	db       postgresDB
	orgs     OrgStore
	users    UserWriter
	sessions SessionAdmin
	audit    AuditWriter
	now      func() time.Time
}

// postgresDB is a minimal interface so the service doesn't need to import
// the concrete postgres.DB type.
type postgresDB interface {
	WithTenant(ctx context.Context, orgID uuid.UUID, fn func(pgx.Tx) error) error
	WithTx(ctx context.Context, fn func(pgx.Tx) error) error
}

type SettingsServiceDeps struct {
	DB       postgresDB
	Orgs     OrgStore
	Users    UserWriter
	Sessions SessionAdmin
	Audit    AuditWriter
	Now      func() time.Time
}

func NewSettingsService(d SettingsServiceDeps) *SettingsService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &SettingsService{
		db:       d.DB,
		orgs:     d.Orgs,
		users:    d.Users,
		sessions: d.Sessions,
		audit:    d.Audit,
		now:      d.Now,
	}
}

// Inputs.
type UpdateOrgInput struct {
	Name     string
	Slug     string
	Currency string
	Timezone string
	Email    string
	Phone    string
	Address  string
}

type UpdateProfileInput struct {
	Name string
}

type ChangePasswordInput struct {
	UserID          uuid.UUID
	CurrentPassword string
	NewPassword     string
}

// Errors.
var (
	ErrOrgNameRequired   = errors.New("settings: business name is required")
	ErrOrgSlugInvalid    = errors.New("settings: invalid slug")
	ErrOrgSlugTaken      = errors.New("settings: slug already taken")
	ErrOrgCurrencyLocked = errors.New("settings: currency cannot change after orders exist")
	ErrUserNameRequired  = errors.New("settings: name is required")
	ErrWrongPassword     = errors.New("settings: current password is incorrect")
	ErrNewPasswordWeak   = errors.New("settings: new password does not meet requirements")
	ErrSessionNotFound   = errors.New("settings: session not found")
	ErrCannotRevokeCurrent = errors.New("settings: sign out normally to end this session")
)

// GetOrg loads the current org for the given tenant.
func (s *SettingsService) GetOrg(ctx context.Context, scope tenant.TenantScope) (*org.Organization, error) {
	var out *org.Organization
	err := s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		var e error
		out, e = s.orgs.GetTx(ctx, tx, scope.OrgID)
		return e
	})
	return out, err
}

// UpdateOrg applies changes to the org.
func (s *SettingsService) UpdateOrg(ctx context.Context, scope tenant.TenantScope, in UpdateOrgInput) (*org.Organization, error) {
	slug, err := org.NewSlug(in.Slug)
	if err != nil {
		return nil, ErrOrgSlugInvalid
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, ErrOrgNameRequired
	}

	var updated *org.Organization
	err = s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		o, err := s.orgs.GetTx(ctx, tx, scope.OrgID)
		if err != nil {
			return err
		}

		newCurrency := strings.ToUpper(strings.TrimSpace(in.Currency))
		if newCurrency != o.Currency {
			if err := s.checkCurrencyChangeAllowed(ctx, tx, scope.OrgID); err != nil {
				return err
			}
			if err := o.SetCurrency(newCurrency, s.now()); err != nil {
				return err
			}
		}

		if err := o.Update(in.Name, slug, in.Email, in.Phone, in.Address, in.Timezone, s.now()); err != nil {
			return err
		}
		if err := s.orgs.Update(ctx, scope, o); err != nil {
			if errors.Is(err, org.ErrSlugTaken) {
				return ErrOrgSlugTaken
			}
			return err
		}

		if s.audit != nil {
			_ = s.audit.RecordTx(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID,
				ActorUserID:    scope.UserID,
				Action:         "org.updated",
				EntityType:     "ORGANIZATION",
				EntityID:       o.ID,
				After:          mustJSON(map[string]any{"name": o.Name, "slug": o.Slug.String()}),
			})
		}
		updated = o
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *SettingsService) checkCurrencyChangeAllowed(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	const q = `SELECT count(*) FROM orders WHERE organization_id = $1`
	var n int
	if err := tx.QueryRow(ctx, q, orgID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrOrgCurrencyLocked
	}
	return nil
}

// UpdateProfile changes the user's display name.
func (s *SettingsService) UpdateProfile(ctx context.Context, userID uuid.UUID, in UpdateProfileInput) error {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return ErrUserNameRequired
	}
	return s.users.UpdateName(ctx, userID, name, s.now())
}

// ChangePassword verifies the current password and updates it.
func (s *SettingsService) ChangePassword(ctx context.Context, in ChangePasswordInput) error {
	u, err := s.users.GetByID(ctx, in.UserID)
	if err != nil {
		return err
	}
	if err := auth.VerifyPassword(in.CurrentPassword, u.PasswordHash); err != nil {
		return ErrWrongPassword
	}
	if err := auth.ValidatePasswordStrength(in.NewPassword); err != nil {
		return ErrNewPasswordWeak
	}
	hash, err := auth.HashPassword(in.NewPassword)
	if err != nil {
		return err
	}
	return s.users.UpdatePasswordHash(ctx, in.UserID, hash, s.now())
}

// RevokeAllSessions signs the user out everywhere.
func (s *SettingsService) RevokeAllSessions(ctx context.Context, userID uuid.UUID) error {
	return s.sessions.RevokeAllForUser(ctx, userID, s.now())
}

// RevokeOtherSessions signs out every device except one. Used after a
// password change: the current device stays signed in.
func (s *SettingsService) RevokeOtherSessions(ctx context.Context, userID, exceptSessionID uuid.UUID) error {
	return s.sessions.RevokeOthersForUser(ctx, userID, exceptSessionID, s.now())
}

// RevokeSession ends one of the user's other sessions after verifying
// ownership. The current session can only be ended via logout.
func (s *SettingsService) RevokeSession(ctx context.Context, userID, sessionID, currentID uuid.UUID) error {
	if sessionID == currentID {
		return ErrCannotRevokeCurrent
	}
	list, err := s.sessions.ListActiveForUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, sess := range list {
		if sess.ID == sessionID {
			return s.sessions.Revoke(ctx, sessionID, s.now())
		}
	}
	return ErrSessionNotFound
}

// ListSessions returns all active sessions.
func (s *SettingsService) ListSessions(ctx context.Context, userID uuid.UUID) ([]*auth.Session, error) {
	return s.sessions.ListActiveForUser(ctx, userID)
}
