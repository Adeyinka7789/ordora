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
	// Baseline headers are always present; HSTS only on HTTPS (see below).
	rec := httptest.NewRecorder()
	SecurityHeaders(okHandler()).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	for _, h := range []string{
		"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options",
		"Referrer-Policy", "Permissions-Policy",
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
	// htmx + fonts are self-hosted: no third-party hosts except Tailwind.
	for _, gone := range []string{"unpkg.com", "fonts.googleapis.com", "fonts.gstatic.com"} {
		if containsStr(csp, gone) {
			t.Errorf("CSP must not reference %q (self-hosted): %s", gone, csp)
		}
	}
	// Plain HTTP must NOT carry HSTS.
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("plain HTTP must not send HSTS, got %q", got)
	}

	// Trusted proxy reporting https (prod Caddy on localhost) must send HSTS.
	rec = httptest.NewRecorder()
	httpsReq := httptest.NewRequest("GET", "/", nil)
	httpsReq.RemoteAddr = "127.0.0.1:8080"
	httpsReq.Header.Set("X-Forwarded-Proto", "https")
	SecurityHeaders(okHandler()).ServeHTTP(rec, httpsReq)
	if got := rec.Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("https via trusted proxy must send HSTS")
	}

	// Spoofed X-Forwarded-Proto from an untrusted peer must NOT send HSTS.
	rec = httptest.NewRecorder()
	spoofReq := httptest.NewRequest("GET", "/", nil)
	spoofReq.RemoteAddr = "203.0.113.7:1234"
	spoofReq.Header.Set("X-Forwarded-Proto", "https")
	SecurityHeaders(okHandler()).ServeHTTP(rec, spoofReq)
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("spoofed proto must not send HSTS, got %q", got)
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

func withTailoringScope(r *http.Request, orgID uuid.UUID, tailoring bool) *http.Request {
	s := &auth.ResolvedSession{
		Scope:       tenant.TenantScope{OrgID: orgID, Role: tenant.RoleOwner},
		IsTailoring: tailoring,
	}
	return r.WithContext(withSession(r.Context(), s))
}

func TestRequireTailoring(t *testing.T) {
	org := uuid.New()

	// No session at all must 404.
	rec := httptest.NewRecorder()
	RequireTailoring(okHandler()).ServeHTTP(rec, httptest.NewRequest("GET", "/groups", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("no session: got %d, want 404", rec.Code)
	}

	// Non-tailoring org must 404 (feature hidden, not forbidden).
	rec = httptest.NewRecorder()
	RequireTailoring(okHandler()).ServeHTTP(rec,
		withTailoringScope(httptest.NewRequest("GET", "/groups", nil), org, false))
	if rec.Code != http.StatusNotFound {
		t.Errorf("non-tailoring: got %d, want 404", rec.Code)
	}

	// Tailoring org passes through.
	rec = httptest.NewRecorder()
	RequireTailoring(okHandler()).ServeHTTP(rec,
		withTailoringScope(httptest.NewRequest("GET", "/groups", nil), org, true))
	if rec.Code != http.StatusOK {
		t.Errorf("tailoring: got %d, want 200", rec.Code)
	}
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

func withRole(r *http.Request, role tenant.Role) *http.Request {
	s := &auth.ResolvedSession{
		Scope: tenant.TenantScope{OrgID: uuid.New(), UserID: uuid.New(), Role: role},
	}
	return r.WithContext(withSession(r.Context(), s))
}

func TestRequireWrite(t *testing.T) {
	// VIEWER is blocked.
	rec := httptest.NewRecorder()
	RequireWrite(okHandler()).ServeHTTP(rec,
		withRole(httptest.NewRequest("POST", "/orders", nil), tenant.RoleViewer))
	if rec.Code != http.StatusForbidden {
		t.Errorf("viewer: got %d, want 403", rec.Code)
	}

	// Writers pass through.
	for _, role := range []tenant.Role{
		tenant.RoleOwner, tenant.RoleAdmin, tenant.RoleManager,
		tenant.RoleStaff, tenant.RoleAccountant,
	} {
		rec := httptest.NewRecorder()
		RequireWrite(okHandler()).ServeHTTP(rec,
			withRole(httptest.NewRequest("POST", "/orders", nil), role))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: got %d, want 200", role, rec.Code)
		}
	}

	// Anonymous redirects to login.
	rec = httptest.NewRecorder()
	RequireWrite(okHandler()).ServeHTTP(rec, httptest.NewRequest("POST", "/orders", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("anonymous: got %d, want 303", rec.Code)
	}
}

func TestClientIP_IgnoresSpoofedHeaders(t *testing.T) {
	// Direct connection with forged XFF: header must be ignored.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.7:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := ClientIP(req); got != "203.0.113.7" {
		t.Errorf("spoofed XFF: got %q, want direct peer", got)
	}

	// Trusted proxy (loopback): first XFF entry wins.
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:8080"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	if got := ClientIP(req); got != "1.2.3.4" {
		t.Errorf("trusted proxy XFF: got %q, want 1.2.3.4", got)
	}
}
