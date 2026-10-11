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
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/domain/user"
)

// Dead-simple team invites: max 3 people per org (1 owner + 2 staff).
// No email sending — the owner copies the /join/{token} link.
const (
	MaxTeamMembers = 3
	TeamInviteTTL  = 7 * 24 * time.Hour
)

// TeamMember is one active membership with user details for the settings UI.
type TeamMember struct {
	UserID    uuid.UUID
	Name      string
	Email     string
	Role      tenant.Role
	CreatedAt time.Time
}

// TeamInvite is one row of organization_invites.
type TeamInvite struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	OrgName   string
	Email     string
	Status    string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// TeamInviteRecord is the insert shape (carries the token hash).
type TeamInviteRecord struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	Email     string
	TokenHash []byte
	Status    string
	InvitedBy uuid.UUID
	ExpiresAt time.Time
	CreatedAt time.Time
}

// TeamOverview is everything the settings page needs.
type TeamOverview struct {
	Members   []TeamMember
	Invites   []TeamInvite
	Total     int // active members + pending invites
	SlotsLeft int
	IsOwner   bool
}

// Errors.
var (
	ErrTeamNotOwner        = errors.New("team: only the owner can manage the team")
	ErrTeamLimitReached    = errors.New("team: team limit reached (max 3)")
	ErrTeamInviteNotFound  = errors.New("team: invite not found")
	ErrTeamInviteExpired   = errors.New("team: invite link has expired")
	ErrTeamInviteUsed      = errors.New("team: invite link has already been used")
	ErrTeamEmailTaken      = errors.New("team: that email is already registered")
	ErrTeamAlreadyMember   = errors.New("team: that email is already on the team")
	ErrTeamAlreadyInvited  = errors.New("team: that email already has a pending invite")
	ErrTeamCannotRemoveMe  = errors.New("team: you cannot remove yourself")
	ErrTeamLastOwner       = errors.New("team: every business needs at least one owner")
	ErrTeamMemberNotFound  = errors.New("team: member not found")
	ErrTeamCannotRemoveOwn = errors.New("team: owners cannot be removed")
)

// TeamInviteStore persists organization_invites. Tx variants run inside the
// caller's WithTenant block; GetByToken runs pre-auth via a SECURITY DEFINER
// function (no tenant).
type TeamInviteStore interface {
	CreateTx(ctx context.Context, tx pgx.Tx, inv *TeamInviteRecord) error
	ListPendingTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) ([]TeamInvite, error)
	GetByIDTx(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID) (*TeamInvite, error)
	RevokeTx(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID, now time.Time) error
	AcceptTx(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID, now time.Time) error
	CountPendingTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (int, error)
	GetByToken(ctx context.Context, hash []byte) (*TeamInvite, error)
}

// TeamMemberStore reads/writes organization_members for the team feature.
type TeamMemberStore interface {
	// LockOrgTx serializes cap-checked writes for one org (see impl).
	LockOrgTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error
	AddTx(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID, role tenant.Role, now time.Time) error
	ListActiveTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) ([]TeamMember, error)
	CountActiveTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (int, error)
	CountOwnersTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (int, error)
	GetTx(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID) (*TeamMember, error)
	DisableTx(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID) error
	// RevokeOrgSessions signs the removed member out of this org only.
	RevokeOrgSessions(ctx context.Context, orgID, userID uuid.UUID, now time.Time) error
}

// TeamUserStore is the user-repo subset the team flow needs.
type TeamUserStore interface {
	CreateTx(ctx context.Context, tx pgx.Tx, u *user.User) error
	GetByEmail(ctx context.Context, email user.Email) (*user.User, error)
}

// TeamSessionStore creates the auto-login session on invite accept.
type TeamSessionStore interface {
	Create(ctx context.Context, s *auth.Session) error
}

// TeamTxRunner is the tenant-tx subset (same shape as postgres.DB).
type TeamTxRunner interface {
	WithTenant(ctx context.Context, orgID uuid.UUID, fn func(pgx.Tx) error) error
	WithTx(ctx context.Context, fn func(pgx.Tx) error) error
}

// TeamIDs mints ids.
type TeamIDs interface {
	New() uuid.UUID
}

// TeamService orchestrates invites. Keep it boring: owner-only writes,
// STAFF shares the same organization_id data automatically via RLS.
type TeamService struct {
	db       TeamTxRunner
	invites  TeamInviteStore
	members  TeamMemberStore
	users    TeamUserStore
	sessions TeamSessionStore
	audit    AuditWriter
	ids      TeamIDs
	now      func() time.Time
}

type TeamServiceDeps struct {
	DB       TeamTxRunner
	Invites  TeamInviteStore
	Members  TeamMemberStore
	Users    TeamUserStore
	Sessions TeamSessionStore
	Audit    AuditWriter
	IDs      TeamIDs
	Now      func() time.Time
}

func NewTeamService(d TeamServiceDeps) *TeamService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &TeamService{
		db: d.DB, invites: d.Invites, members: d.Members,
		users: d.Users, sessions: d.Sessions, audit: d.Audit,
		ids: d.IDs, now: d.Now,
	}
}

// Overview loads members + pending invites for the settings page.
func (s *TeamService) Overview(ctx context.Context, scope tenant.TenantScope) (*TeamOverview, error) {
	out := &TeamOverview{IsOwner: scope.Role == tenant.RoleOwner}
	err := s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		members, err := s.members.ListActiveTx(ctx, tx, scope.OrgID)
		if err != nil {
			return err
		}
		invites, err := s.invites.ListPendingTx(ctx, tx, scope.OrgID)
		if err != nil {
			return err
		}
		out.Members = members
		out.Invites = invites
		out.Total = len(members) + len(invites)
		out.SlotsLeft = MaxTeamMembers - out.Total
		if out.SlotsLeft < 0 {
			out.SlotsLeft = 0
		}
		return nil
	})
	return out, err
}

// CreateInvite makes a PENDING invite and returns the raw token so the
// handler can show the /join/{token} link for the owner to copy.
func (s *TeamService) CreateInvite(ctx context.Context, scope tenant.TenantScope, rawEmail string) (*TeamInvite, string, error) {
	if scope.Role != tenant.RoleOwner {
		return nil, "", ErrTeamNotOwner
	}
	email, err := user.NewEmail(rawEmail)
	if err != nil {
		return nil, "", err
	}
	now := s.now()
	raw, hash, err := auth.NewToken(auth.TokenSession)
	if err != nil {
		return nil, "", err
	}
	inv := &TeamInvite{
		ID: s.ids.New(), OrgID: scope.OrgID,
		Email: email.String(), Status: "PENDING",
		ExpiresAt: now.Add(TeamInviteTTL), CreatedAt: now,
	}
	err = s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		// Serialize with concurrent invites/accepts for this org: without
		// the lock, two txs could both read active+pending = 2 and both
		// insert, exceeding the cap of 3.
		if err := s.members.LockOrgTx(ctx, tx, scope.OrgID); err != nil {
			return err
		}
		active, err := s.members.CountActiveTx(ctx, tx, scope.OrgID)
		if err != nil {
			return err
		}
		pending, err := s.invites.CountPendingTx(ctx, tx, scope.OrgID)
		if err != nil {
			return err
		}
		if active+pending >= MaxTeamMembers {
			return ErrTeamLimitReached
		}
		// Already on the team? Compare by email (members carry emails).
		existing, err := s.members.ListActiveTx(ctx, tx, scope.OrgID)
		if err != nil {
			return err
		}
		for _, m := range existing {
			if strings.EqualFold(m.Email, email.String()) {
				return ErrTeamAlreadyMember
			}
		}
		if err := s.invites.CreateTx(ctx, tx, &TeamInviteRecord{
			ID: inv.ID, OrgID: scope.OrgID, Email: email.String(),
			TokenHash: hash, Status: "PENDING",
			InvitedBy: scope.UserID, ExpiresAt: inv.ExpiresAt, CreatedAt: now,
		}); err != nil {
			if errors.Is(err, ErrTeamAlreadyInvited) {
				return ErrTeamAlreadyInvited
			}
			return err
		}
		if s.audit != nil {
			_ = s.audit.RecordTx(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID,
				ActorUserID:    scope.UserID,
				Action:         "team.invite_created",
				EntityType:     "TEAM_INVITE",
				EntityID:       inv.ID,
				After:          mustJSON(map[string]any{"email": email.String()}),
			})
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return inv, raw, nil
}

// RevokeInvite cancels a PENDING invite.
func (s *TeamService) RevokeInvite(ctx context.Context, scope tenant.TenantScope, inviteID uuid.UUID) error {
	if scope.Role != tenant.RoleOwner {
		return ErrTeamNotOwner
	}
	return s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		inv, err := s.invites.GetByIDTx(ctx, tx, scope.OrgID, inviteID)
		if err != nil {
			return err
		}
		if inv.Status != "PENDING" {
			return ErrTeamInviteUsed
		}
		if err := s.invites.RevokeTx(ctx, tx, scope.OrgID, inviteID, s.now()); err != nil {
			return err
		}
		if s.audit != nil {
			_ = s.audit.RecordTx(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID,
				ActorUserID:    scope.UserID,
				Action:         "team.invite_revoked",
				EntityType:     "TEAM_INVITE",
				EntityID:       inviteID,
			})
		}
		return nil
	})
}

// RemoveMember disables a STAFF membership. Owners cannot be removed and
// nobody can remove themselves (prevents lockout).
func (s *TeamService) RemoveMember(ctx context.Context, scope tenant.TenantScope, userID uuid.UUID) error {
	if scope.Role != tenant.RoleOwner {
		return ErrTeamNotOwner
	}
	if userID == scope.UserID {
		return ErrTeamCannotRemoveMe
	}
	if err := s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		m, err := s.members.GetTx(ctx, tx, scope.OrgID, userID)
		if err != nil {
			return err
		}
		if m.Role == tenant.RoleOwner {
			return ErrTeamCannotRemoveOwn
		}
		if err := s.members.DisableTx(ctx, tx, scope.OrgID, userID); err != nil {
			return err
		}
		if s.audit != nil {
			_ = s.audit.RecordTx(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID,
				ActorUserID:    scope.UserID,
				Action:         "team.member_removed",
				EntityType:     "TEAM_MEMBER",
				EntityID:       userID,
				After:          mustJSON(map[string]any{"email": m.Email}),
			})
		}
		return nil
	}); err != nil {
		return err
	}
	// Sign the removed member out of this org (best-effort, outside the tx:
	// sessions are global, not tenant-scoped).
	_ = s.members.RevokeOrgSessions(ctx, scope.OrgID, userID, s.now())
	return nil
}

// LookupInvite resolves a raw /join/{token} token pre-auth.
func (s *TeamService) LookupInvite(ctx context.Context, rawToken string) (*TeamInvite, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, ErrTeamInviteNotFound
	}
	inv, err := s.invites.GetByToken(ctx, auth.HashToken(rawToken))
	if err != nil {
		return nil, ErrTeamInviteNotFound
	}
	now := s.now()
	switch {
	case inv.Status != "PENDING":
		if inv.Status == "ACCEPTED" {
			return nil, ErrTeamInviteUsed
		}
		return nil, ErrTeamInviteNotFound
	case now.After(inv.ExpiresAt):
		return nil, ErrTeamInviteExpired
	}
	return inv, nil
}

// AcceptResult carries the auto-login session token.
type AcceptResult struct {
	User     *user.User
	OrgID    uuid.UUID
	RawToken string
}

// AcceptInvite creates the STAFF user + membership and consumes the invite.
func (s *TeamService) AcceptInvite(ctx context.Context, rawToken, name, password, userAgent, ip string) (*AcceptResult, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, ErrTeamInviteNotFound
	}
	hash := auth.HashToken(rawToken)
	inv, err := s.invites.GetByToken(ctx, hash)
	if err != nil {
		return nil, ErrTeamInviteNotFound
	}
	now := s.now()
	if inv.Status != "PENDING" {
		if inv.Status == "ACCEPTED" {
			return nil, ErrTeamInviteUsed
		}
		return nil, ErrTeamInviteNotFound
	}
	if now.After(inv.ExpiresAt) {
		return nil, ErrTeamInviteExpired
	}
	email, err := user.NewEmail(inv.Email)
	if err != nil {
		return nil, err
	}
	if err := auth.ValidatePasswordStrength(password); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, user.ErrNameRequired
	}
	// Email must be fresh: staff join with the invited address.
	if _, err := s.users.GetByEmail(ctx, email); err == nil {
		return nil, ErrTeamEmailTaken
	}
	pwHash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	userID := s.ids.New()
	verifiedAt := now // owner-invited: skip the verify-email friction
	u, err := user.New(userID, email, pwHash, name, now)
	if err != nil {
		return nil, err
	}
	u.EmailVerifiedAt = &verifiedAt
	onboarded := now // staff join an existing business: skip the wizard
	u.OnboardingCompletedAt = &onboarded

	raw, sessHash, err := auth.NewToken(auth.TokenSession)
	if err != nil {
		return nil, err
	}
	sess := &auth.Session{
		ID:             s.ids.New(),
		UserID:         userID,
		TokenHash:      sessHash,
		OrganizationID: &inv.OrgID,
		UserAgent:      userAgent,
		IP:             ip,
		ExpiresAt:      now.Add(auth.TokenSession.TTL()),
		CreatedAt:      now,
		LastSeenAt:     now,
	}

	// Enforce the cap at accept time too, under the same org lock: two
	// staff accepting two invites at once must not both slip past the count.
	err = s.db.WithTenant(ctx, inv.OrgID, func(tx pgx.Tx) error {
		if err := s.members.LockOrgTx(ctx, tx, inv.OrgID); err != nil {
			return err
		}
		active, err := s.members.CountActiveTx(ctx, tx, inv.OrgID)
		if err != nil {
			return err
		}
		if active >= MaxTeamMembers {
			return ErrTeamLimitReached
		}
		if err := s.users.CreateTx(ctx, tx, u); err != nil {
			if errors.Is(err, user.ErrEmailTaken) {
				return ErrTeamEmailTaken
			}
			return err
		}
		if err := s.members.AddTx(ctx, tx, inv.OrgID, userID, tenant.RoleStaff, now); err != nil {
			return err
		}
		return s.invites.AcceptTx(ctx, tx, inv.OrgID, inv.ID, now)
	})
	if err != nil {
		return nil, err
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, err
	}
	return &AcceptResult{User: u, OrgID: inv.OrgID, RawToken: raw}, nil
}
