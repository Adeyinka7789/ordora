package auth

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Login brute-force protection: consecutive failures per normalized email,
// lockout after MaxLoginFailures for LoginLockout.
//
// Rows exist even for unregistered emails so locked/unknown responses stay
// uniform (no account-enumeration oracle). Stale rows are pruned
// opportunistically on record (older than LoginAttemptTTL).
const (
	MaxLoginFailures = 5
	LoginLockout     = 15 * time.Minute
	LoginAttemptTTL  = time.Hour
)

// ErrAccountLocked is returned when the email is inside a lockout window.
// The HTTP layer shows a generic "try again later" message.
var ErrAccountLocked = errors.New("auth: account temporarily locked")

// LoginAttemptStore persists consecutive login failures per email.
// Implemented by the postgres repo; fakes in tests.
type LoginAttemptStore interface {
	// Locked reports whether email is inside a lockout window.
	Locked(ctx context.Context, email string, now time.Time) (bool, error)
	// RecordFailure increments failures, arming the lockout when the
	// threshold is reached. Creates the row when absent.
	RecordFailure(ctx context.Context, email string, now time.Time) error
	// Clear resets failures after a successful login.
	Clear(ctx context.Context, email string) error
}

// normalizeAttemptEmail lowercases/trims raw login input for the attempts
// table. Callers only reach this with syntactically valid emails.
func normalizeAttemptEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
