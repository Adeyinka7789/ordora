package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/user"
)

// stubAttemptStore scripts lockout state for service-level tests. The real
// threshold logic lives in SQL (LoginAttemptRepo) and is covered DB-backed.
type stubAttemptStore struct {
	locked  map[string]bool
	records []string
	cleared []string
}

func (s *stubAttemptStore) Locked(_ context.Context, email string, _ time.Time) (bool, error) {
	return s.locked[email], nil
}
func (s *stubAttemptStore) RecordFailure(_ context.Context, email string, _ time.Time) error {
	s.records = append(s.records, email)
	return nil
}
func (s *stubAttemptStore) Clear(_ context.Context, email string) error {
	s.cleared = append(s.cleared, email)
	return nil
}

type countingUserStore struct {
	calls int
	u     *user.User
}

func (f *countingUserStore) CreateTx(ctx context.Context, tx pgx.Tx, u *user.User) error {
	return nil
}
func (f *countingUserStore) GetByEmail(ctx context.Context, e user.Email) (*user.User, error) {
	f.calls++
	if f.u == nil {
		return nil, user.ErrNotFound
	}
	return f.u, nil
}
func (f *countingUserStore) GetByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	return f.u, nil
}
func (f *countingUserStore) MarkEmailVerified(ctx context.Context, id uuid.UUID, now time.Time) error {
	return nil
}
func (f *countingUserStore) UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string, now time.Time) error {
	return nil
}
func (f *countingUserStore) MarkOnboarded(ctx context.Context, id uuid.UUID, now time.Time) error {
	return nil
}

func lockoutTestService(users *countingUserStore, attempts *stubAttemptStore) *Service {
	return NewService(Deps{
		DB:       fakeTxRunner{},
		Users:    users,
		Attempts: attempts,
		Now:      time.Now,
	})
}

func lockoutTestUser(t *testing.T) *user.User {
	t.Helper()
	email, err := user.NewEmail("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	return &user.User{ID: uuid.New(), Email: email, PasswordHash: hash, Name: "Ada"}
}

func TestLogin_LockedShortCircuits(t *testing.T) {
	users := &countingUserStore{u: lockoutTestUser(t)}
	attempts := &stubAttemptStore{locked: map[string]bool{"ada@example.com": true}}
	svc := lockoutTestService(users, attempts)

	_, err := svc.Login(context.Background(), LoginInput{Email: "ada@example.com", Password: "correct-horse-battery"})
	if !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("expected ErrAccountLocked, got %v", err)
	}
	if users.calls != 0 {
		t.Error("locked login must not touch the user store (no password check, no oracle)")
	}
}

func TestLogin_FailuresRecorded(t *testing.T) {
	users := &countingUserStore{u: lockoutTestUser(t)}
	attempts := &stubAttemptStore{locked: map[string]bool{}}
	svc := lockoutTestService(users, attempts)

	for i := 0; i < 3; i++ {
		_, err := svc.Login(context.Background(), LoginInput{Email: "ada@example.com", Password: "wrong"})
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: expected ErrInvalidCredentials, got %v", i, err)
		}
	}
	if len(attempts.records) != 3 {
		t.Errorf("expected 3 recorded failures, got %d", len(attempts.records))
	}

	// Unknown emails record failures too (uniform behavior, no oracle).
	unknown := &countingUserStore{u: nil}
	svc2 := lockoutTestService(unknown, attempts)
	_, err := svc2.Login(context.Background(), LoginInput{Email: "ghost@example.com", Password: "wrong"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown email: expected ErrInvalidCredentials, got %v", err)
	}
	if len(attempts.records) != 4 || attempts.records[3] != "ghost@example.com" {
		t.Errorf("unknown emails must record failures too: %v", attempts.records)
	}
}
