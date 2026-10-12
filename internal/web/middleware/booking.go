package middleware

import (
	"net/http"
)

// RequireBooking gates a route behind the appointment-booking business
// categories (salons + photography). Other orgs 404, indistinguishable
// from "no such page" so trade-specific features don't leak their
// existence — the same posture as RequireTailoring/RequirePerishable/RequireFlag.
//
// Compose inside RequireTenant, e.g. for /dashboard/bookings:
//
//	mux.Handle("GET /dashboard/bookings", RequireTenant(RequireBooking(h)))
func RequireBooking(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := SessionFromContext(r.Context())
		if s == nil || !s.IsBooking {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
