package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/platformadmin"
)

type impCtxKey string

const (
	ctxKeyImpersonation      impCtxKey = "impersonation_session"
	ctxKeyImpersonationOrgID impCtxKey = "impersonation_org_id"
)

// ImpersonationResolver resolves an impersonation cookie into the underlying
// session. Implemented by *app.AdminService.
type ImpersonationResolver interface {
	ResolveImpersonation(ctx context.Context, rawToken string) (*platformadmin.ImpersonationSession, error)
}

// ImpersonationCookieName is the name of the impersonation cookie. Must match
// the constant used by the admin handler when setting/clearing it.
const ImpersonationCookieName = "ordora_impersonate"

// ImpersonationMiddleware reads the impersonation cookie and injects the
// target org id into the request context. If the cookie is missing or
// invalid, the request proceeds normally.
func ImpersonationMiddleware(resolver ImpersonationResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(ImpersonationCookieName)
			if err != nil || c.Value == "" {
				next.ServeHTTP(w, r)
				return
			}
			sess, err := resolver.ResolveImpersonation(r.Context(), c.Value)
			if err != nil {
				// Invalid or expired — proceed unauthenticated.
				next.ServeHTTP(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyImpersonation, sess)
			ctx = context.WithValue(ctx, ctxKeyImpersonationOrgID, sess.OrganizationID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ImpersonatedOrgID returns the org id being impersonated, or uuid.Nil.
func ImpersonatedOrgID(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(ctxKeyImpersonationOrgID).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}

// ImpersonatingAdminID returns the admin id doing the impersonation, or
// uuid.Nil if not impersonating.
func ImpersonatingAdminID(ctx context.Context) uuid.UUID {
	s, ok := ctx.Value(ctxKeyImpersonation).(*platformadmin.ImpersonationSession)
	if !ok || s == nil {
		return uuid.Nil
	}
	return s.AdminID
}

// IsImpersonating reports whether the request is under impersonation.
func IsImpersonating(ctx context.Context) bool {
	return ImpersonatedOrgID(ctx) != uuid.Nil
}
