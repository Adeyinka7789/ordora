package middleware

import (
	"context"
	"net/http"

	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/google/uuid"
)

// SessionResolver is the subset of *auth.Service the middleware needs.
type SessionResolver interface {
	ResolveSession(ctx context.Context, rawToken string) (*auth.ResolvedSession, error)
}

// SessionMiddleware reads the session cookie, resolves it, and injects the
// resolved session into the request context.
//
// If an impersonation cookie is present and valid, it takes precedence: the
// session context is built from the impersonation target instead of the
// business session.
func SessionMiddleware(
	resolver SessionResolver,
	cookieName string,
	impersonationResolver ImpersonationResolver,
	buildImpersonatedSession func(ctx context.Context, orgID uuid.UUID) (*auth.ResolvedSession, error),
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Impersonation check first.
			if c, err := r.Cookie(ImpersonationCookieName); err == nil && c.Value != "" {
				if sess, err := impersonationResolver.ResolveImpersonation(r.Context(), c.Value); err == nil {
					// Build a synthetic session for the org's owner.
					if resolved, err := buildImpersonatedSession(r.Context(), sess.OrganizationID); err == nil {
						ctx := context.WithValue(r.Context(), ctxKeyImpersonation, sess)
						ctx = context.WithValue(ctx, ctxKeyImpersonationOrgID, sess.OrganizationID)
						ctx = withSession(ctx, resolved)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
				}
			}

			// Normal business session.
			c, err := r.Cookie(cookieName)
			if err != nil || c.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			resolved, err := resolver.ResolveSession(r.Context(), c.Value)
			if err != nil {
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
// Suspended orgs are rejected with 403: suspension must actually lock
// the business out, not just show a banner in the admin panel.
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
		if s.OrgSuspended {
			http.Error(w, "This business has been suspended. Please contact support.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withSession(ctx context.Context, s *auth.ResolvedSession) context.Context {
	return context.WithValue(ctx, ctxKeySession, s)
}

// SessionFromContext returns the resolved session, or nil if anonymous.
func SessionFromContext(ctx context.Context) *auth.ResolvedSession {
	s, _ := ctx.Value(ctxKeySession).(*auth.ResolvedSession)
	return s
}

// ScopeFromContext returns the tenant scope, or zero-value if not authed.
func ScopeFromContext(ctx context.Context) tenant.TenantScope {
	if s := SessionFromContext(ctx); s != nil {
		return s.Scope
	}
	return tenant.TenantScope{}
}
