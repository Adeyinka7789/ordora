package middleware

import (
	"net/http"

	"github.com/Adeyinka7789/ordora/internal/flags"
)

// RequireFlag gates a route behind a feature flag, evaluated for the
// request's active org. Failing closed: missing session, missing org,
// missing provider, or a disabled flag all 404 (indistinguishable from
// "no such page", so disabled features don't leak their existence).
//
// Compose inside RequireTenant, e.g.:
//
//	mux.Handle("GET /orders", RequireTenant(RequireFlag(provider, "orders")(h)))
func RequireFlag(provider *flags.Provider, key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if provider == nil {
				// No provider means no flag data — deny, never assume on.
				http.NotFound(w, r)
				return
			}
			s := SessionFromContext(r.Context())
			if s == nil {
				http.NotFound(w, r)
				return
			}
			if !provider.Enabled(s.Scope.OrgID, key) {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
