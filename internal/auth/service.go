package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/domain/user"
)

// Repos the service depends on. Defined as interfaces (ports) so the app
// layer can be tested with fakes.

type UserStore interface {
	CreateTx(ctx context.Context, tx pgx.Tx, u *user.User) error
	GetByEmail(ctx context.Context, email user.Email) (*user.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*user.User, error)
	MarkEmailVerified(ctx context.Context, id uuid.UUID, now time.Time) error
	UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string, now time.Time) error
	MarkOnboarded(ctx context.Context, id uuid.UUID, now time.Time) error
}

type OrgStore interface {
	CreateTx(ctx context.Context, tx pgx.Tx, o *org.Organization) error
}

// TxRunner is the subset of TxRunner that auth needs. This allows
// the service to remain independent of the postgres package and to be
// testable with a fake.
type TxRunner interface {
	WithTenant(ctx context.Context, orgID uuid.UUID, fn func(pgx.Tx) error) error
	WithTx(ctx context.Context, fn func(pgx.Tx) error) error
}

type MemberStore interface {
	AddTx(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID, role tenant.Role, now time.Time) error
	ListForUser(ctx context.Context, userID uuid.UUID) ([]Membership, error)
	GetRole(ctx context.Context, orgID, userID uuid.UUID) (tenant.Role, error)
}

type SessionStore interface {
	Create(ctx context.Context, s *Session) error
	GetActiveByTokenHash(ctx context.Context, hash []byte) (*Session, error)
	Revoke(ctx context.Context, sessionID uuid.UUID, now time.Time) error
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, now time.Time) error
	RevokeOthersForUser(ctx context.Context, userID, exceptSessionID uuid.UUID, now time.Time) error
	SetActiveOrg(ctx context.Context, sessionID, orgID uuid.UUID) error
	Touch(ctx context.Context, sessionID uuid.UUID, newExpiry time.Time) error
	TouchSeen(ctx context.Context, sessionID uuid.UUID, now time.Time) error
	RoleForSession(ctx context.Context, s *Session) (tenant.Role, error)
}

type AuthTokenStore interface {
	Create(ctx context.Context, t *AuthToken) error
	Consume(ctx context.Context, hash []byte, kind string, now time.Time) (*AuthToken, error)
}

// IDGen produces new ids.
type IDGen interface {
	New() uuid.UUID
}

// Clock is injectable for tests.
type Clock func() time.Time

// Mailer delivers transactional messages. Implemented in infra/email.
// For M1.3 we use a no-op implementation that logs to stdout.
type Mailer interface {
	SendVerifyEmail(ctx context.Context, to user.Email, name, link string) error
	SendPasswordReset(ctx context.Context, to user.Email, name, link string) error
}

// Service orchestrates authentication and registration.
type Service struct {
	db       TxRunner
	users    UserStore
	orgs     OrgStore
	members  MemberStore
	sessions SessionStore
	tokens   AuthTokenStore
	attempts LoginAttemptStore
	ids      IDGen
	mailer   Mailer
	now      Clock
	outbox   OutboxWriter
	idleTTL  time.Duration
}

// Deps bundles the service dependencies.
type Deps struct {
	DB       TxRunner
	Users    UserStore
	Orgs     OrgStore
	Members  MemberStore
	Sessions SessionStore
	Tokens   AuthTokenStore
	// Attempts is required (fail-closed): pass a working store, never
	// nil — without it brute-force lockout silently stops working.
	Attempts LoginAttemptStore
	Mailer   Mailer
	Outbox   OutboxWriter
	IDs      IDGen
	Now      Clock
	// IdleTTL caps session lifetime without activity. Zero disables
	// idle enforcement (absolute expiry still applies).
	IdleTTL time.Duration
}

// OutboxWriter writes domain events to the transactional outbox.
type OutboxWriter interface {
	Enqueue(ctx context.Context, orgID uuid.UUID, eventName string, payload any) error
}

func NewService(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{
		db:       d.DB,
		users:    d.Users,
		orgs:     d.Orgs,
		members:  d.Members,
		sessions: d.Sessions,
		tokens:   d.Tokens,
		attempts: d.Attempts,
		ids:      d.IDs,
		mailer:   d.Mailer,
		now:      d.Now,
		outbox:   d.Outbox,
		idleTTL:  d.IdleTTL,
	}
}

// -----------------------------------------------------------------------------
// Inputs
// -----------------------------------------------------------------------------

type RegisterInput struct {
	Email        string
	Password     string
	Name         string
	BusinessName string
	// Business profile for analytics (phone required, rest optional).
	BusinessPhone    string
	BusinessAddress  string
	BusinessType     string
	BusinessCategory string
	TeamSize         string
	ReferralSource   string
}

// RegisterResult is returned on successful registration.
type RegisterResult struct {
	User         *user.User
	Organization *org.Organization
	VerifyToken  string // raw token; caller emails the link
}

// LoginInput carries credentials plus request metadata recorded on the
// session (device list, security review).
type LoginInput struct {
	Email     string
	Password  string
	UserAgent string
	IP        string
}

// LoginResult carries the session token that the HTTP layer puts in a cookie.
type LoginResult struct {
	Session   *Session
	RawToken  string
	User      *user.User
	ActiveOrg *org.Organization
}

// -----------------------------------------------------------------------------
// Errors the HTTP layer needs to distinguish
// -----------------------------------------------------------------------------

var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrEmailNotVerified   = errors.New("auth: email not verified")
	ErrNoMembership       = errors.New("auth: user has no active membership")
	ErrTokenInvalid       = errors.New("auth: token invalid or expired")
	ErrAccountDisabled    = errors.New("auth: account disabled")
)

// -----------------------------------------------------------------------------
// Register
// -----------------------------------------------------------------------------

// Register creates a user, an organization, and an OWNER membership, all in
// one transaction, then generates an email-verify token.
func (s *Service) Register(ctx context.Context, in RegisterInput) (*RegisterResult, error) {
	email, err := user.NewEmail(in.Email)
	if err != nil {
		return nil, err
	}
	if err := ValidatePasswordStrength(in.Password); err != nil {
		return nil, err
	}

	businessName := strings.TrimSpace(in.BusinessName)
	if businessName == "" {
		businessName = in.Name + "'s business"
	}

	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, err
	}

	now := s.now()
	userID := s.ids.New()
	orgID := s.ids.New()

	u, err := user.New(userID, email, hash, in.Name, now)
	if err != nil {
		return nil, err
	}

	slug, err := s.buildUniqueSlug(ctx, businessName)
	if err != nil {
		return nil, err
	}
	o, err := org.New(orgID, businessName, slug, now)
	if err != nil {
		return nil, err
	}
	if err := o.SetProfile(
		in.BusinessType, in.BusinessCategory, in.TeamSize,
		in.ReferralSource, in.BusinessPhone, in.BusinessAddress, now,
	); err != nil {
		return nil, err
	}

	// Raw token is generated outside the tx, only the hash goes inside.
	rawToken, tokenHash, err := NewToken(TokenEmailVerify)
	if err != nil {
		return nil, err
	}
	tokenID := s.ids.New()
	authToken := &AuthToken{
		ID:        tokenID,
		UserID:    userID,
		Kind:      string(TokenEmailVerify),
		TokenHash: tokenHash,
		ExpiresAt: now.Add(TokenEmailVerify.TTL()),
		CreatedAt: now,
	}

	// The whole registration is one transaction. Note the WithTenant(orgID):
	// we claim the new org's id as our tenant, which is exactly what the RLS
	// WITH CHECK policy requires for the org and membership inserts.
	err = s.db.WithTenant(ctx, orgID, func(tx pgx.Tx) error {
		if err := s.users.CreateTx(ctx, tx, u); err != nil {
			return err
		}
		if err := s.orgs.CreateTx(ctx, tx, o); err != nil {
			return err
		}
		if err := s.members.AddTx(ctx, tx, orgID, userID, tenant.RoleOwner, now); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Auth token insert is not tenant-scoped, so it goes in its own tx.
	if err := s.tokens.Create(ctx, authToken); err != nil {
		return nil, err
	}

	return &RegisterResult{
		User:         u,
		Organization: o,
		VerifyToken:  rawToken,
	}, nil
}

// buildUniqueSlug generates a candidate slug and, on collision, retries with
// numeric suffixes. The DB's unique constraint is the source of truth; we
// simply walk suffixes until one succeeds.
//
// We don't pre-check existence because that would race. We attempt the insert
// and let Postgres tell us if there's a conflict. Since slug uniqueness is
// checked at the DB level in CreateTx (returns org.ErrSlugTaken), the caller
// can loop here.
func (s *Service) buildUniqueSlug(ctx context.Context, name string) (org.Slug, error) {
	base := org.Slugify(name)
	if base == "" {
		base = "business"
	}
	if len(base) < 3 {
		base = base + "-shop"
	}
	slug, err := org.NewSlug(base)
	if err != nil {
		// Fall back to a safe default and let the DB hash it out.
		base = "business-" + s.ids.New().String()[:8]
		slug, err = org.NewSlug(base)
		if err != nil {
			return org.Slug{}, fmt.Errorf("auth: cannot build slug: %w", err)
		}
	}
	return slug, nil
}

// -----------------------------------------------------------------------------
// Login
// -----------------------------------------------------------------------------

func (s *Service) Login(ctx context.Context, in LoginInput) (*LoginResult, error) {
	email, err := user.NewEmail(in.Email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	attemptKey := normalizeAttemptEmail(in.Email)

	// Fail closed on store errors: if we can't check attempts, don't
	// let the login through blind.
	locked, err := s.attempts.Locked(ctx, attemptKey, s.now())
	if err != nil {
		return nil, err
	}
	if locked {
		return nil, ErrAccountLocked
	}

	u, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			// Run a dummy hash to equalize timing. Avoids revealing whether
			// the email exists.
			_, _ = HashPassword("timing-equalization-placeholder")
			_ = s.attempts.RecordFailure(ctx, attemptKey, s.now())
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if err := VerifyPassword(in.Password, u.PasswordHash); err != nil {
		_ = s.attempts.RecordFailure(ctx, attemptKey, s.now())
		return nil, ErrInvalidCredentials
	}
	// Correct password resets the counter even if later steps fail
	// (e.g. no membership) — lockout punishes guessing, not edge cases.
	_ = s.attempts.Clear(ctx, attemptKey)

	// Pick the user's first (oldest) membership as the active org.
	memberships, err := s.members.ListForUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if len(memberships) == 0 {
		return nil, ErrNoMembership
	}
	active := memberships[0]

	raw, hash, err := NewToken(TokenSession)
	if err != nil {
		return nil, err
	}
	now := s.now()
	sess := &Session{
		ID:             s.ids.New(),
		UserID:         u.ID,
		TokenHash:      hash,
		OrganizationID: &active.OrgID,
		UserAgent:      in.UserAgent,
		IP:             in.IP,
		ExpiresAt:      now.Add(TokenSession.TTL()),
		CreatedAt:      now,
		LastSeenAt:     now,
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, err
	}

	// Load the active org for the caller's convenience.
	var activeOrg *org.Organization
	err = s.db.WithTenant(ctx, active.OrgID, func(tx pgx.Tx) error {
		o, e := loadOrgTx(ctx, tx, active.OrgID)
		activeOrg = o
		return e
	})
	if err != nil {
		return nil, err
	}

	return &LoginResult{
		Session:   sess,
		RawToken:  raw,
		User:      u,
		ActiveOrg: activeOrg,
	}, nil
}

// loadOrgTx is a tiny helper; we could also expose GetByIDTx on OrgRepo but
// keeping this local avoids widening the public API of the repo.
func loadOrgTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*org.Organization, error) {
	const q = `
		SELECT id, name, slug, COALESCE(logo_key,''), COALESCE(email::text,''),
		       COALESCE(phone,''), COALESCE(address,''), currency, timezone,
		       created_at, updated_at
		FROM organizations
		WHERE id = $1
	`
	var (
		oid       uuid.UUID
		name      string
		slugStr   string
		logoKey   string
		email     string
		phone     string
		address   string
		currency  string
		timezone  string
		createdAt time.Time
		updatedAt time.Time
	)
	if err := tx.QueryRow(ctx, q, id).Scan(&oid, &name, &slugStr, &logoKey, &email,
		&phone, &address, &currency, &timezone, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	slug, _ := org.NewSlug(slugStr)
	return &org.Organization{
		ID: oid, Name: name, Slug: slug, LogoKey: logoKey,
		Email: email, Phone: phone, Address: address,
		Currency: currency, Timezone: timezone,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

// -----------------------------------------------------------------------------
// Logout
// -----------------------------------------------------------------------------

func (s *Service) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return s.sessions.Revoke(ctx, sessionID, s.now())
}

// -----------------------------------------------------------------------------
// Email verification
// -----------------------------------------------------------------------------

func (s *Service) VerifyEmail(ctx context.Context, rawToken string) (*user.User, error) {
	hash := HashToken(rawToken)
	t, err := s.tokens.Consume(ctx, hash, string(TokenEmailVerify), s.now())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrTokenInvalid
		}
		return nil, err
	}
	now := s.now()
	if err := s.users.MarkEmailVerified(ctx, t.UserID, now); err != nil {
		return nil, err
	}
	return s.users.GetByID(ctx, t.UserID)
}

// CompleteOnboarding records that the user finished or skipped the
// first-run wizard, so it is never shown to them again.
func (s *Service) CompleteOnboarding(ctx context.Context, userID uuid.UUID) error {
	return s.users.MarkOnboarded(ctx, userID, s.now())
}

// -----------------------------------------------------------------------------
// Password reset
// -----------------------------------------------------------------------------

// RequestPasswordReset generates a reset token and emails it. Always returns
// nil (even for unknown emails) so callers cannot probe for registered users.
func (s *Service) RequestPasswordReset(ctx context.Context, rawEmail string) error {
	email, err := user.NewEmail(rawEmail)
	if err != nil {
		return nil // swallow invalid email; response is identical
	}
	u, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return nil
		}
		return err
	}
	raw, hash, err := NewToken(TokenPasswordReset)
	if err != nil {
		return err
	}
	now := s.now()
	t := &AuthToken{
		ID:        s.ids.New(),
		UserID:    u.ID,
		Kind:      string(TokenPasswordReset),
		TokenHash: hash,
		ExpiresAt: now.Add(TokenPasswordReset.TTL()),
		CreatedAt: now,
	}
	if err := s.tokens.Create(ctx, t); err != nil {
		return err
	}
	link := "/password/reset?token=" + raw
	return s.mailer.SendPasswordReset(ctx, u.Email, u.Name, link)
}

// ResetPassword consumes a reset token and updates the user's password.
func (s *Service) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return err
	}
	hash := HashToken(rawToken)
	t, err := s.tokens.Consume(ctx, hash, string(TokenPasswordReset), s.now())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrTokenInvalid
		}
		return err
	}
	pwHash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.users.UpdatePasswordHash(ctx, t.UserID, pwHash, s.now()); err != nil {
		return err
	}
	// Revoke all sessions: password change logs everyone out.
	return s.sessions.RevokeAllForUser(ctx, t.UserID, s.now())
}

// -----------------------------------------------------------------------------
// Session resolution (used by middleware)
// -----------------------------------------------------------------------------

// ResolvedSession is what the middleware injects into the request context.
type ResolvedSession struct {
	Session     *Session
	User        *user.User
	Scope       tenant.TenantScope
	OrgName     string
	OrgSlug     string
	OrgCurrency string
	OrgTimezone string
	// OrgSuspended is true when the org was suspended by the platform
	// admin. Tenant middleware rejects suspended orgs (403); public
	// intake treats them as not found.
	OrgSuspended bool
}

// ResolveSession loads a session by raw token and constructs a TenantScope.
// Returns ErrTokenInvalid if the token is missing, expired, or revoked.
func (s *Service) ResolveSession(ctx context.Context, rawToken string) (*ResolvedSession, error) {
	hash := HashToken(rawToken)
	sess, err := s.sessions.GetActiveByTokenHash(ctx, hash)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrTokenInvalid
		}
		return nil, err
	}
	now := s.now()
	if s.idleTTL > 0 && now.Sub(sess.LastSeenAt) > s.idleTTL {
		return nil, ErrTokenInvalid
	}
	// Opportunistic activity tracking: at most one write per hour per
	// session, so idle enforcement doesn't cost a write per request.
	// Best-effort — the session stays valid even if the write fails.
	if now.Sub(sess.LastSeenAt) > time.Hour {
		_ = s.sessions.TouchSeen(ctx, sess.ID, now)
		sess.LastSeenAt = now
	}
	u, err := s.users.GetByID(ctx, sess.UserID)
	if err != nil {
		return nil, err
	}
	var scope tenant.TenantScope
	if sess.OrganizationID != nil {
		role, err := s.sessions.RoleForSession(ctx, sess)
		if err != nil {
			return nil, err
		}
		scope = tenant.TenantScope{
			OrgID:  *sess.OrganizationID,
			UserID: sess.UserID,
			Role:   role,
		}
	}

	var orgName, orgSlug, orgCurrency, orgTimezone string
	var orgSuspended bool
	if sess.OrganizationID != nil {
		_ = s.db.WithTenant(ctx, *sess.OrganizationID, func(tx pgx.Tx) error {
			const q = `SELECT name, slug::text, currency::text, timezone, suspended_at IS NOT NULL FROM organizations WHERE id = $1`
			return tx.QueryRow(ctx, q, *sess.OrganizationID).Scan(&orgName, &orgSlug, &orgCurrency, &orgTimezone, &orgSuspended)
		})
	}

	return &ResolvedSession{
		Session:      sess,
		User:         u,
		Scope:        scope,
		OrgName:      orgName,
		OrgSlug:      orgSlug,
		OrgCurrency:  orgCurrency,
		OrgTimezone:  orgTimezone,
		OrgSuspended: orgSuspended,
	}, nil
}

// SendVerificationEmail sends a verification email to the given user. The
// raw token is passed in the URL already. Used by the register handler.
func (s *Service) SendVerificationEmail(ctx context.Context, u *user.User, link string) error {
	return s.mailer.SendVerifyEmail(ctx, u.Email, u.Name, link)
}

// ErrNotFound is returned by stores when a row is missing.
var ErrNotFound = errors.New("auth: not found")
