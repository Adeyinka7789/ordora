package middleware

import (
	"context"
	"net/http"

	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// SessionResolver is the subset of *auth.Service the middleware needs.
// Declared as an interface so the middleware can be tested without the DB.
type SessionResolver interface {
	ResolveSession(ctx context.Context, rawToken string) (*auth.ResolvedSession, error)
}

// SessionMiddleware reads the session cookie, resolves it, and injects the
// resolved session into the request context. If the cookie is missing or
// invalid, the request proceeds unauthenticated (no error, no redirect).
//
// Requiring authentication is the job of a *separate* middleware. This one is
// deliberately permissive so that public pages can coexist with authed ones.
func SessionMiddleware(resolver SessionResolver, cookieName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(cookieName)
			if err != nil || c.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			resolved, err := resolver.ResolveSession(r.Context(), c.Value)
			if err != nil {
				// Invalid/expired token: don't fail. Just treat as anonymous.
				next.ServeHTTP(w, r)
				return
			}
			ctx := withSession(r.Context(), resolved)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth rejects unauthenticated requests with a redirect to /login.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if SessionFromContext(r.Context()) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireTenant requires an authenticated session *and* a tenant scope.
func RequireTenant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := SessionFromContext(r.Context())
		if s == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if s.Scope.IsZero() {
			http.Error(w, "no active organization", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withSession stores the resolved session in the context.
func withSession(ctx context.Context, s *auth.ResolvedSession) context.Context {
	return context.WithValue(ctx, ctxKeySession, s)
}

// SessionFromContext returns the resolved session, or nil if anonymous.
func SessionFromContext(ctx context.Context) *auth.ResolvedSession {
	s, _ := ctx.Value(ctxKeySession).(*auth.ResolvedSession)
	return s
}

// ScopeFromContext returns the tenant scope, or zero-value if not authed.
// Callers under RequireTenant can rely on this being non-zero.
func ScopeFromContext(ctx context.Context) tenant.TenantScope {
	if s := SessionFromContext(ctx); s != nil {
		return s.Scope
	}
	return tenant.TenantScope{}
}
