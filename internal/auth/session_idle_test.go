package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/domain/user"
)

type fakeSessionStore struct {
	session   *Session
	touchSeen int
}

func (f *fakeSessionStore) Create(ctx context.Context, s *Session) error { return nil }
func (f *fakeSessionStore) GetActiveByTokenHash(ctx context.Context, hash []byte) (*Session, error) {
	if f.session == nil {
		return nil, ErrNotFound
	}
	cp := *f.session
	return &cp, nil
}
func (f *fakeSessionStore) Revoke(ctx context.Context, id uuid.UUID, now time.Time) error {
	return nil
}
func (f *fakeSessionStore) RevokeAllForUser(ctx context.Context, id uuid.UUID, now time.Time) error {
	return nil
}
func (f *fakeSessionStore) RevokeOthersForUser(ctx context.Context, id, except uuid.UUID, now time.Time) error {
	return nil
}
func (f *fakeSessionStore) SetActiveOrg(ctx context.Context, id, orgID uuid.UUID) error {
	return nil
}
func (f *fakeSessionStore) Touch(ctx context.Context, id uuid.UUID, exp time.Time) error {
	return nil
}
func (f *fakeSessionStore) TouchSeen(ctx context.Context, id uuid.UUID, now time.Time) error {
	f.touchSeen++
	return nil
}
func (f *fakeSessionStore) RoleForSession(ctx context.Context, s *Session) (tenant.Role, error) {
	return tenant.RoleOwner, nil
}

type fakeUserStore struct{ u *user.User }

func (f *fakeUserStore) CreateTx(ctx context.Context, tx pgx.Tx, u *user.User) error { return nil }
func (f *fakeUserStore) GetByEmail(ctx context.Context, e user.Email) (*user.User, error) {
	return nil, user.ErrNotFound
}
func (f *fakeUserStore) GetByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	if f.u == nil {
		return nil, user.ErrNotFound
	}
	return f.u, nil
}
func (f *fakeUserStore) MarkEmailVerified(ctx context.Context, id uuid.UUID, now time.Time) error {
	return nil
}
func (f *fakeUserStore) UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string, now time.Time) error {
	return nil
}

type fakeTxRunner struct{}

func (fakeTxRunner) WithTenant(ctx context.Context, orgID uuid.UUID, fn func(pgx.Tx) error) error {
	return errors.New("fakeTxRunner: unexpected WithTenant")
}
func (fakeTxRunner) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	return errors.New("fakeTxRunner: unexpected WithTx")
}

func idleTestService(store *fakeSessionStore, idle time.Duration, now time.Time) *Service {
	uid := uuid.New()
	return NewService(Deps{
		DB:       fakeTxRunner{},
		Users:    &fakeUserStore{u: &user.User{ID: uid, Name: "T"}},
		Sessions: store,
		IDs:      nil,
		Now:      func() time.Time { return now },
		IdleTTL:  idle,
	})
}

func TestResolveSession_IdleExpired(t *testing.T) {
	now := time.Now()
	store := &fakeSessionStore{session: &Session{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		LastSeenAt: now.Add(-8 * 24 * time.Hour),
		ExpiresAt:  now.Add(24 * time.Hour),
		// OrganizationID nil: skips role/org DB loads.
	}}
	svc := idleTestService(store, 7*24*time.Hour, now)

	_, err := svc.ResolveSession(context.Background(), "raw-token")
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
	if store.touchSeen != 0 {
		t.Errorf("expired session must not be touched")
	}
}

func TestResolveSession_ActiveNoTouchWhenFresh(t *testing.T) {
	now := time.Now()
	store := &fakeSessionStore{session: &Session{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		LastSeenAt: now.Add(-time.Minute),
		ExpiresAt:  now.Add(24 * time.Hour),
	}}
	svc := idleTestService(store, 7*24*time.Hour, now)

	if _, err := svc.ResolveSession(context.Background(), "raw-token"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if store.touchSeen != 0 {
		t.Errorf("fresh session must not trigger a write")
	}
}

func TestResolveSession_TouchesStaleSession(t *testing.T) {
	now := time.Now()
	store := &fakeSessionStore{session: &Session{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		LastSeenAt: now.Add(-2 * time.Hour),
		ExpiresAt:  now.Add(24 * time.Hour),
	}}
	svc := idleTestService(store, 7*24*time.Hour, now)

	res, err := svc.ResolveSession(context.Background(), "raw-token")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if store.touchSeen != 1 {
		t.Errorf("stale session: touchSeen = %d, want 1", store.touchSeen)
	}
	if !res.Session.LastSeenAt.Equal(now) {
		t.Errorf("returned session should carry refreshed LastSeenAt")
	}
}

func TestResolveSession_IdleDisabled(t *testing.T) {
	now := time.Now()
	store := &fakeSessionStore{session: &Session{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		LastSeenAt: now.Add(-365 * 24 * time.Hour),
		ExpiresAt:  now.Add(24 * time.Hour),
	}}
	svc := idleTestService(store, 0, now) // 0 = disabled

	if _, err := svc.ResolveSession(context.Background(), "raw-token"); err != nil {
		t.Fatalf("disabled idle must accept old session, got %v", err)
	}
}
