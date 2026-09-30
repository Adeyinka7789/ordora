package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Token kinds. Each kind gets its own TTL and its own table (or table row) so
// they cannot be confused with each other.
type TokenKind string

const (
	TokenSession       TokenKind = "SESSION"
	TokenEmailVerify   TokenKind = "EMAIL_VERIFY"
	TokenPasswordReset TokenKind = "PASSWORD_RESET"
	TokenPublicOrder   TokenKind = "PUBLIC_ORDER"
)

// TTLs by kind.
func (k TokenKind) TTL() time.Duration {
	switch k {
	case TokenSession:
		return 30 * 24 * time.Hour // 30 days idle
	case TokenEmailVerify:
		return 24 * time.Hour
	case TokenPasswordReset:
		return 1 * time.Hour
	case TokenPublicOrder:
		return 0 // no expiry — lives as long as the order
	}
	return time.Hour
}

// NewToken returns a cryptographically random, URL-safe token and its SHA-256
// hash. The token goes to the user (in a cookie, in an email link, in a public
// URL); the hash goes in the database.
//
// Rule: never store raw tokens. If the DB is leaked, hashed tokens are useless
// without a preimage attack on SHA-256, which is infeasible.
func NewToken(kind TokenKind) (raw string, hash []byte, err error) {
	n := tokenBytes(kind)
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("auth: rand: %w", err)
	}
	// RawStdEncoding has no padding, so tokens are URL-safe without escaping.
	raw = base64.RawURLEncoding.EncodeToString(b)
	hash = HashToken(raw)
	return raw, hash, nil
}

// HashToken returns the SHA-256 of a raw token, in the same form used by
// NewToken. Used by the DB layer to look up a token by its raw value.
func HashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// HashTokenHex is a convenience for logging/debugging.
func HashTokenHex(raw string) string {
	return hex.EncodeToString(HashToken(raw))
}

// tokenBytes picks the entropy size per kind. More sensitive tokens get more
// bits. Session tokens and public-order tokens are the highest-risk, so they
// get 32 bytes (256 bits). Email/password tokens are short-lived, 16 bytes
// (128 bits) is fine.
func tokenBytes(kind TokenKind) int {
	switch kind {
	case TokenSession, TokenPublicOrder:
		return 32
	case TokenEmailVerify, TokenPasswordReset:
		return 16
	}
	return 32
}

// NewIdempotencyKey returns a random key suitable for notification idempotency.
// Deterministic from event identity in most cases, but this is a fallback.
func NewIdempotencyKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// SessionID generates a new session id (a UUID, separate from the token).
func SessionID() uuid.UUID {
	// Reuse the id package? We don't want to import infra here.
	// Use crypto/rand-derived UUIDv4 as a fallback for the session row id.
	return uuid.New()
}
