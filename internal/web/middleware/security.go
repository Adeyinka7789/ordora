package middleware

import (
	"net/http"
)

// SecurityHeaders sets baseline response headers on every response.
//
// Content-Security-Policy is deliberately pragmatic, not maximal: the app
// loads Google Fonts, the Tailwind Play CDN, and htmx from unpkg, and uses
// inline scripts/styles plus inline event handlers. A strict
// script-src 'self' would break the product, so inline scripts are still
// allowed — the policy's value today is: no plugins, no framing, no
// form posts off-site, and scripts/styles only from 'self' plus the three
// pinned third parties. Template output is escaped by html/template, which
// remains the primary XSS defense. The follow-up is nonce-based
// script-src (propagate nonces through templates and HTMX swaps).
//
// HSTS is safe to send on plain HTTP (browsers ignore it there); behind
// the production reverse proxy it takes effect.
func SecurityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; " +
		"script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://unpkg.com; " +
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
		"font-src 'self' https://fonts.gstatic.com; " +
		"img-src 'self' data:; " +
		"connect-src 'self'; " +
		"object-src 'none'; " +
		"base-uri 'self'; " +
		"frame-ancestors 'deny'; " +
		"form-action 'self'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Strict-Transport-Security", "max-age=31536000")
		next.ServeHTTP(w, r)
	})
}
