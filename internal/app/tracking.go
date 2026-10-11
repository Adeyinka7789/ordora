package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/google/uuid"
)

var (
	// ErrTrackingNotConfigured is returned when no portal secret is set.
	// Callers omit the tracking link (templates hide the button).
	ErrTrackingNotConfigured = errors.New("app: tracking links not configured (empty portal secret)")
	// ErrTrackingBadOrder is returned for a nil order id.
	ErrTrackingBadOrder = errors.New("app: tracking link needs an order id")
)

// AbsURL joins a configured base URL and a path for use in emails.
// Email clients have no request context, so every emailed link must be
// absolute — bare paths like "/orders/..." arrive broken. When baseURL is
// empty the bare path is returned unchanged and callers should omit the
// link (templates guard with {{if .PortalURL}} etc.).
func AbsURL(baseURL, path string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return path
	}
	if path == "" {
		return strings.TrimSuffix(baseURL, "/")
	}
	return strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(path, "/")
}

const trackingTokenPrefix = "o1_"

// trackingTokenDomain separates tracking-link signatures from every other
// HMAC in the system.
const trackingTokenDomain = "ordora-track-v1"

// TrackingToken mints a long-lived, self-validating customer tracking link
// token for an order: "o1_" + base64url(orderID || HMAC(secret, orderID)).
//
// Why not reuse the stored public_token_hash? The raw bearer behind that
// hash exists only at mint time (creation discards it, regeneration shows
// it once), so later events — status changes, payments, reschedules — can
// never rebuild the link. A signed token is derivable anywhere from just
// (secret, orderID), so every customer email can carry a working tracking
// link without rotating (and breaking previously shared) tokens.
//
// Properties:
//   - Unguessable without the secret (256-bit HMAC).
//   - Stable across restarts and regenerations: old emails keep working,
//     and staff rotating the shareable portal token never kills email links.
//   - Bearer credential: anyone holding the link can view that one order's
//     tracking page. It cannot be revoked per-link — treat it like the
//     shareable portal token (rotating that token does NOT kill these).
//
// An empty secret yields an error: callers must omit the link instead of
// emitting an unsigned (forgeable) one.
func TrackingToken(secret string, orderID uuid.UUID) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", ErrTrackingNotConfigured
	}
	if orderID == uuid.Nil {
		return "", ErrTrackingBadOrder
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(trackingTokenDomain))
	mac.Write(orderID[:])
	sum := mac.Sum(nil)
	raw := append(orderID[:], sum...)
	return trackingTokenPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// VerifyTrackingToken checks a raw token from GET /o/{token} and returns
// the order it grants access to. Unknown prefixes and bad signatures read
// as not-found (no existence signal), so callers map false to 404.
func VerifyTrackingToken(secret, raw string) (uuid.UUID, bool) {
	var zero uuid.UUID
	if strings.TrimSpace(secret) == "" {
		return zero, false
	}
	body, ok := strings.CutPrefix(strings.TrimSpace(raw), trackingTokenPrefix)
	if !ok {
		return zero, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || len(decoded) != 16+32 {
		return zero, false
	}
	var orderID uuid.UUID
	copy(orderID[:], decoded[:16])
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(trackingTokenDomain))
	mac.Write(orderID[:])
	if !hmac.Equal(mac.Sum(nil), decoded[16:]) {
		return zero, false
	}
	return orderID, true
}

// TrackingLink builds the absolute customer tracking URL for an order, or
// "" when links are not configured (empty base URL or secret). Templates
// hide their buttons on "", so "" is always safe to enqueue.
func TrackingLink(baseURL, secret string, orderID uuid.UUID) string {
	// A tracking URL is emitted into email and must never degrade to a bare
	// path. AbsURL intentionally preserves bare paths for callers that need
	// same-origin links, but emailed tracking links have no request origin.
	if strings.TrimSpace(baseURL) == "" {
		return ""
	}
	raw, err := TrackingToken(secret, orderID)
	if err != nil {
		return ""
	}
	return AbsURL(baseURL, "/o/"+raw)
}
