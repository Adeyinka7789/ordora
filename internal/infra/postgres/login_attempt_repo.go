package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/auth"
)

// LoginAttemptRepo persists consecutive login failures per email for
// brute-force lockout. Platform-level table (no RLS): reads/writes use
// the pool directly, called only from the auth service.
type LoginAttemptRepo struct {
	db *DB
}

func NewLoginAttemptRepo(db *DB) *LoginAttemptRepo { return &LoginAttemptRepo{db: db} }

// Locked reports whether email is inside a lockout window. Expired locks
// read as unlocked (the row is reset on the next failure or success).
func (r *LoginAttemptRepo) Locked(ctx context.Context, email string, now time.Time) (bool, error) {
	const q = `
		SELECT COALESCE(locked_until, '-infinity') > $2
		FROM login_attempts
		WHERE email = $1
	`
	var locked bool
	if err := r.db.Pool().QueryRow(ctx, q, email, now).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return locked, nil
}

// RecordFailure increments failures, arming the lockout at the threshold.
// Creates the row when absent. Prunes stale rows opportunistically.
func (r *LoginAttemptRepo) RecordFailure(ctx context.Context, email string, now time.Time) error {
	const q = `
		INSERT INTO login_attempts (email, failures, locked_until, updated_at)
		VALUES ($1, 1, NULL, $2)
		ON CONFLICT (email) DO UPDATE SET
			failures = CASE
				WHEN login_attempts.locked_until IS NOT NULL AND login_attempts.locked_until > $2
				THEN login_attempts.failures
				WHEN login_attempts.locked_until IS NOT NULL AND login_attempts.locked_until <= $2
				THEN 1
				ELSE login_attempts.failures + 1
			END,
			locked_until = CASE
				WHEN login_attempts.locked_until IS NOT NULL AND login_attempts.locked_until > $2
				THEN login_attempts.locked_until
				WHEN (CASE
					WHEN login_attempts.locked_until IS NOT NULL AND login_attempts.locked_until <= $2
					THEN 1
					ELSE login_attempts.failures + 1
				END) >= $3
				THEN $2 + ($4 * interval '1 second')
				ELSE NULL
			END,
			updated_at = $2
	`
	if _, err := r.db.Pool().Exec(ctx, q, email, now, auth.MaxLoginFailures, int(auth.LoginLockout.Seconds())); err != nil {
		return err
	}
	// Opportunistic prune (best-effort, keeps the table bounded against
	// enumeration/bloat attacks).
	_, _ = r.db.Pool().Exec(ctx,
		`DELETE FROM login_attempts WHERE updated_at < $1`,
		now.Add(-auth.LoginAttemptTTL),
	)
	return nil
}

// Clear resets failures after a successful login.
func (r *LoginAttemptRepo) Clear(ctx context.Context, email string) error {
	const q = `DELETE FROM login_attempts WHERE email = $1`
	_, err := r.db.Pool().Exec(ctx, q, email)
	return err
}

// Attempt describes the lockout state of one email (admin display use).
type Attempt struct {
	Failures    int
	LockedUntil *time.Time
	Found       bool
}

// Get loads the lockout row for an email. found=false when the email has
// no recorded failures.
func (r *LoginAttemptRepo) Get(ctx context.Context, email string) (Attempt, error) {
	const q = `SELECT failures, locked_until FROM login_attempts WHERE email = $1`
	var a Attempt
	var locked *time.Time
	var failures int
	if err := r.db.Pool().QueryRow(ctx, q, email).Scan(&failures, &locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Attempt{}, nil
		}
		return Attempt{}, err
	}
	a.Found = true
	a.Failures = failures
	a.LockedUntil = locked
	return a, nil
}
