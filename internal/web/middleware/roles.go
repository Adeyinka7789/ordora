package middleware

import (
	"net/http"
)

// RequireWrite rejects read-only tenant roles (e.g. VIEWER) with 403.
// Compose outside RequireTenant so anonymous users still redirect to login
// via RequireTenant, e.g.:
//
//	mux.Handle("POST /orders", RequireTenant(RequireWrite(http.HandlerFunc(h.Create))))
//
// Impersonated sessions resolve to OWNER in SessionMiddleware, so platform
// admins acting via impersonation are unaffected.
func RequireWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := SessionFromContext(r.Context())
		if s == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if err := s.Scope.RequireWrite(); err != nil {
			http.Error(w, "You do not have permission to modify data.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole restricts a route to an explicit allowlist of tenant roles.
// Prefer RequireWrite for generic mutations; use this for sensitive actions
// (e.g. refunds, reversals, deletes) that should be Manager+ only.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s := SessionFromContext(r.Context())
			if s == nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			if _, ok := allowed[string(s.Scope.Role)]; !ok {
				http.Error(w, "You do not have permission to perform this action.", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
