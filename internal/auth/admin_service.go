package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/platformadmin"
)

// AdminStore is the persistence contract for platform admins.
type AdminStore interface {
	Create(ctx context.Context, a *platformadmin.Admin) error
	GetByEmail(ctx context.Context, email string) (*platformadmin.Admin, error)
	GetByID(ctx context.Context, id uuid.UUID) (*platformadmin.Admin, error)
	UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string, now time.Time) error
	RecordLogin(ctx context.Context, id uuid.UUID, ip string, now time.Time) error
}

// AdminSessionStore is the persistence contract for admin sessions.
type AdminSessionStore interface {
	Create(ctx context.Context, s *AdminSession) error
	GetActiveByTokenHash(ctx context.Context, hash []byte) (*AdminSession, error)
	Revoke(ctx context.Context, id uuid.UUID, now time.Time) error
	RevokeAllForAdmin(ctx context.Context, adminID uuid.UUID, now time.Time) error
}

// AdminSession is the type the service works with.
type AdminSession struct {
	ID        uuid.UUID
	AdminID   uuid.UUID
	TokenHash []byte
	UserAgent string
	IP        string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// AdminIDGen produces new IDs.
type AdminIDGen interface {
	New() uuid.UUID
}

// ResolvedAdminSession bundles the session and admin.
type ResolvedAdminSession struct {
	Session *AdminSession
	Admin   *platformadmin.Admin
}

// AdminAuthService handles login, logout, and session resolution.
type AdminAuthService struct {
	admins   AdminStore
	sessions AdminSessionStore
	ids      AdminIDGen
	now      func() time.Time
	ttl      time.Duration
}

type AdminAuthDeps struct {
	Admins   AdminStore
	Sessions AdminSessionStore
	IDs      AdminIDGen
	Now      func() time.Time
	TTL      time.Duration
}

func NewAdminAuthService(d AdminAuthDeps) *AdminAuthService {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.TTL <= 0 {
		d.TTL = 8 * time.Hour
	}
	return &AdminAuthService{
		admins:   d.Admins,
		sessions: d.Sessions,
		ids:      d.IDs,
		now:      d.Now,
		ttl:      d.TTL,
	}
}

// AdminLoginInput carries admin credentials for the admin login flow.
type AdminLoginInput struct {
	Email     string
	Password  string
	IP        string
	UserAgent string
}

// AdminLoginResult is what the admin login handler needs.
type AdminLoginResult struct {
	Session  *AdminSession
	RawToken string
	Admin    *platformadmin.Admin
}

var (
	ErrAdminInvalidCredentials = errors.New("admin auth: invalid credentials")
	ErrAdminDisabled           = errors.New("admin auth: account disabled")
	ErrAdminNotFound           = errors.New("admin auth: not found")
	ErrAdminTokenInvalid       = errors.New("admin auth: token invalid")
	ErrAdminRequires2FA        = errors.New("admin auth: 2FA required")
)

func (s *AdminAuthService) Login(ctx context.Context, in AdminLoginInput) (*AdminLoginResult, error) {
	slog.Info("admin_auth.Login",
		"email", in.Email,
		"email_len", len(in.Email),
		"pw_len", len(in.Password),
	)
	a, err := s.admins.GetByEmail(ctx, in.Email)
	slog.Info("admin_auth.GetByEmail", "found", err == nil, "err", err)
	if err != nil {
		if errors.Is(err, platformadmin.ErrNotFound) {
			_, _ = HashPassword("timing-equalization")
			return nil, ErrAdminInvalidCredentials
		}
		return nil, err
	}
	if !a.IsActive() {
		return nil, ErrAdminDisabled
	}
	verifyErr := VerifyPassword(in.Password, a.PasswordHash)
	slog.Info("admin_auth.VerifyPassword",
		"hash_prefix", safePrefix(a.PasswordHash, 40),
		"hash_len", len(a.PasswordHash),
		"err", verifyErr,
	)
	if verifyErr != nil {
		return nil, ErrAdminInvalidCredentials
	}

	raw, hash, err := NewToken(TokenSession)
	if err != nil {
		return nil, err
	}
	now := s.now()
	sess := &AdminSession{
		ID:        s.ids.New(),
		AdminID:   a.ID,
		TokenHash: hash,
		UserAgent: in.UserAgent,
		IP:        in.IP,
		ExpiresAt: now.Add(s.ttl),
		CreatedAt: now,
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		slog.Error("admin_auth.CreateSession", "admin_id", a.ID, "err", err)
		return nil, err
	}
	_ = s.admins.RecordLogin(ctx, a.ID, in.IP, now)

	return &AdminLoginResult{Session: sess, RawToken: raw, Admin: a}, nil
}

func (s *AdminAuthService) Resolve(ctx context.Context, rawToken string) (*ResolvedAdminSession, error) {
	if rawToken == "" {
		return nil, ErrAdminTokenInvalid
	}
	hash := sha256.Sum256([]byte(rawToken))
	sess, err := s.sessions.GetActiveByTokenHash(ctx, hash[:])
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrAdminTokenInvalid
		}
		return nil, err
	}
	a, err := s.admins.GetByID(ctx, sess.AdminID)
	if err != nil {
		return nil, err
	}
	if !a.IsActive() {
		return nil, ErrAdminDisabled
	}
	return &ResolvedAdminSession{Session: sess, Admin: a}, nil
}

func (s *AdminAuthService) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return s.sessions.Revoke(ctx, sessionID, s.now())
}

// CreateAdminInput is used by cmd/seedadmin.
type CreateAdminInput struct {
	Email    string
	Password string
	Name     string
}

// CreateAdmin creates a new platform admin. Used only by the seed CLI.
func (s *AdminAuthService) CreateAdmin(ctx context.Context, in CreateAdminInput) (*platformadmin.Admin, error) {
	if err := ValidatePasswordStrength(in.Password); err != nil {
		return nil, err
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	a, err := platformadmin.New(s.ids.New(), in.Email, hash, in.Name, s.now())
	if err != nil {
		return nil, err
	}
	if err := s.admins.Create(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

// ResetAdminPassword replaces an existing admin password hash.
func (s *AdminAuthService) ResetAdminPassword(ctx context.Context, email, password string) error {
	if err := ValidatePasswordStrength(password); err != nil {
		return err
	}
	a, err := s.admins.GetByEmail(ctx, email)
	if err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.admins.UpdatePassword(ctx, a.ID, hash, s.now())
}

// safePrefix returns the first n characters of s, or the whole string if shorter.
// Used for debug logging of hash values.
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
