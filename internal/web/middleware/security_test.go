package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/flags"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders(okHandler()).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	for _, h := range []string{
		"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options",
		"Referrer-Policy", "Permissions-Policy", "Strict-Transport-Security",
	} {
		if rec.Header().Get(h) == "" {
			t.Errorf("missing security header %s", h)
		}
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"frame-ancestors 'deny'", "object-src 'none'", "form-action 'self'"} {
		if !containsStr(csp, want) {
			t.Errorf("CSP missing %q: %s", want, csp)
		}
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func testProvider() *flags.Provider {
	p := flags.NewProvider(func(ctx context.Context) ([]flags.Flag, error) {
		return []flags.Flag{
			{Key: "on", Enabled: true, RolloutPercent: 100},
			{Key: "off", Enabled: false, RolloutPercent: 100},
		}, nil
	}, 0)
	if err := p.Refresh(context.Background()); err != nil {
		panic(err)
	}
	return p
}

func withScope(r *http.Request, orgID uuid.UUID) *http.Request {
	s := &auth.ResolvedSession{Scope: tenant.TenantScope{OrgID: orgID, Role: tenant.RoleOwner}}
	return r.WithContext(withSession(r.Context(), s))
}

func TestRequireFlag(t *testing.T) {
	// Nil provider must deny (fail closed), never pass through.
	rec := httptest.NewRecorder()
	RequireFlag(nil, "on")(okHandler()).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("nil provider: got %d, want 404", rec.Code)
	}

	p := testProvider()
	org := uuid.New()

	rec = httptest.NewRecorder()
	RequireFlag(p, "off")(okHandler()).ServeHTTP(rec,
		withScope(httptest.NewRequest("GET", "/", nil), org))
	if rec.Code != http.StatusNotFound {
		t.Errorf("disabled flag: got %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	RequireFlag(p, "on")(okHandler()).ServeHTTP(rec,
		withScope(httptest.NewRequest("GET", "/", nil), org))
	if rec.Code != http.StatusOK {
		t.Errorf("enabled flag: got %d, want 200", rec.Code)
	}

	// No session at all must 404, not proceed.
	rec = httptest.NewRecorder()
	RequireFlag(p, "on")(okHandler()).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("no session: got %d, want 404", rec.Code)
	}
}

func TestRateLimit_BlocksOverLimit(t *testing.T) {
	h := RateLimit(RateLimitConfig{Limit: 2, Window: time.Minute})(okHandler())
	var last int
	for i := 0; i < 4; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/login", nil)
		req.RemoteAddr = "10.0.0.9:1234"
		h.ServeHTTP(rec, req)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("expected 429 after exceeding limit, got %d", last)
	}
}
