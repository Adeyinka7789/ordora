package middleware

import (
	"net"
	"net/http"
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
// Client IP is taken from X-Forwarded-For or the request's RemoteAddr.
// Behind a reverse proxy (nginx, Cloudflare), configure the proxy to set
// X-Forwarded-For, or this will bucket everyone into one slot.
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

// clientIP extracts the best-guess client IP.
func clientIP(r *http.Request) string {
	// X-Forwarded-For: client, proxy1, proxy2 — take the first.
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		for i := 0; i < len(fwd); i++ {
			if fwd[i] == ',' {
				return fwd[:i]
			}
		}
		return fwd
	}
	if rip := r.Header.Get("X-Real-IP"); rip != "" {
		return rip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
