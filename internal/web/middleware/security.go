package middleware

import (
	"net"
	"net/http"
	"strings"
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
// HSTS is only emitted when the request actually arrived over HTTPS:
// direct TLS (r.TLS != nil) or a trusted proxy reporting
// X-Forwarded-Proto: https (production Caddy terminates TLS and forwards
// plain HTTP from localhost). Plain-HTTP responses carry no HSTS header.
// includeSubDomains is deliberately absent: single-domain deploys must not
// force HTTPS on subdomains the operator may not control.
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
		if isHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// isHTTPS reports whether the request arrived over TLS. Trusts
// X-Forwarded-Proto only from a trusted proxy peer (see clientIP), so a
// direct-HTTP client cannot induce HSTS output.
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			if isTrustedProxy(host) {
				return true
			}
		} else if isTrustedProxy(r.RemoteAddr) {
			return true
		}
	}
	return false
}
