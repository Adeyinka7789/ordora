package middleware

import (
	"net/http"
)

// RequirePerishable gates a route behind the perishable-goods business
// categories (poultry/egg sellers and food vendors). Other orgs 404,
// indistinguishable from "no such page" so trade-specific features don't
// leak their existence — the same posture as RequireTailoring/RequireFlag.
//
// Compose inside RequireTenant, e.g. for /dashboard/egg-orders:
//
//	mux.Handle("GET /dashboard/egg-orders", RequireTenant(RequirePerishable(h)))
func RequirePerishable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := SessionFromContext(r.Context())
		if s == nil || !s.IsPerishable {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
