package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimitConfig configures a limiter.
type RateLimitConfig struct {
	// Requests allowed per window, per client IP.
	Limit int
	// Window duration.
	Window time.Duration
	// Message shown on limit exceeded.
	Message string
}

// rateBucket tracks one client's recent requests.
type rateBucket struct {
	count   int
	resetAt time.Time
}

// limiter is the shared store. Map of IP → bucket.
type limiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	cfg     RateLimitConfig
}

// RateLimit returns a middleware that enforces a per-IP request limit.
//
// Client IP comes from RemoteAddr, except when the direct peer is a trusted
// proxy (loopback/private, e.g. local Caddy) — then X-Forwarded-For (first
// entry) or X-Real-IP is used. Headers from untrusted peers are ignored so
// attackers cannot spoof their IP to dodge limits.
//
// In-memory only. Not suitable for multi-server. Fine for V1.
func RateLimit(cfg RateLimitConfig) func(http.Handler) http.Handler {
	if cfg.Message == "" {
		cfg.Message = "Too many requests. Please try again later."
	}
	if cfg.Limit <= 0 {
		cfg.Limit = 30
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Minute
	}

	l := &limiter{
		buckets: map[string]*rateBucket{},
		cfg:     cfg,
	}

	// Background cleanup of expired buckets.
	go func() {
		t := time.NewTicker(cfg.Window)
		defer t.Stop()
		for range t.C {
			l.cleanup()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if !l.allow(ip) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, cfg.Message, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (l *limiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[ip]
	if !ok || now.After(b.resetAt) {
		l.buckets[ip] = &rateBucket{count: 1, resetAt: now.Add(l.cfg.Window)}
		return true
	}
	if b.count >= l.cfg.Limit {
		return false
	}
	b.count++
	return true
}

func (l *limiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for ip, b := range l.buckets {
		if now.After(b.resetAt) {
			delete(l.buckets, ip)
		}
	}
}

// ClientIP extracts the best-guess client IP for handlers that need to
// record it (e.g. login sessions). Same logic as the rate limiter uses.
func ClientIP(r *http.Request) string {
	return clientIP(r)
}

// clientIP extracts the best-guess client IP.
//
// X-Forwarded-For / X-Real-IP are only honored when the direct TCP peer is
// a trusted proxy (loopback or private network — the production Caddy on
// the same host, or a LAN reverse proxy). Otherwise they are ignored and
// RemoteAddr is used, so a client that reaches the app directly cannot
// rotate fake IPs to bypass login/register/reset rate limits.
func clientIP(r *http.Request) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	if isTrustedProxy(peer) {
		// X-Forwarded-For: client, proxy1, proxy2 — take the leftmost.
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			if i := strings.IndexByte(fwd, ','); i >= 0 {
				return strings.TrimSpace(fwd[:i])
			}
			return strings.TrimSpace(fwd)
		}
		if rip := strings.TrimSpace(r.Header.Get("X-Real-IP")); rip != "" {
			return rip
		}
	}
	if peer != "" {
		return peer
	}
	return r.RemoteAddr
}

// isTrustedProxy reports whether the direct peer is expected to be our
// reverse proxy rather than the end client.
func isTrustedProxy(host string) bool {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}
