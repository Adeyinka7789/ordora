package middleware

import (
	"net/http"
)

// RequireTailoring gates a route behind the tailoring business category
// (fashion/tailoring shops). Non-tailoring orgs 404, indistinguishable
// from "no such page" so trade-specific features don't leak their
// existence — the same posture as RequireFlag.
//
// Compose inside RequireTenant, e.g. for /groups:
//
//	mux.Handle("GET /groups", RequireTenant(RequireTailoring(h)))
func RequireTailoring(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := SessionFromContext(r.Context())
		if s == nil || !s.IsTailoring {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
