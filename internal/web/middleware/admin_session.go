package middleware

import (
	"context"
	"net/http"

	"github.com/Adeyinka7789/ordora/internal/auth"
)

type adminCtxKey string

const ctxKeyAdminSession adminCtxKey = "admin_session"

// AdminSessionResolver is the subset of *auth.AdminAuthService the middleware needs.
type AdminSessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (*auth.ResolvedAdminSession, error)
}

// AdminSessionMiddleware reads the admin session cookie and injects the
// resolved session into the request context. On failure, proceeds
// unauthenticated.
func AdminSessionMiddleware(resolver AdminSessionResolver, cookieName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(cookieName)
			if err != nil || c.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			resolved, err := resolver.Resolve(r.Context(), c.Value)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyAdminSession, resolved)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AdminFromContext returns the resolved admin session, or nil.
func AdminFromContext(ctx context.Context) *auth.ResolvedAdminSession {
	s, _ := ctx.Value(ctxKeyAdminSession).(*auth.ResolvedAdminSession)
	return s
}

// RequireAdmin redirects unauthenticated requests to the admin login path.
func RequireAdmin(loginPath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if AdminFromContext(r.Context()) == nil {
				http.Redirect(w, r, loginPath, http.StatusSeeOther)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
