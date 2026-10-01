package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

// CSRFConfig configures the CSRF middleware.
type CSRFConfig struct {
	CookieName string // e.g. "ordora_csrf"
	FieldName  string // form field / header name; default "_csrf"
	HeaderName string // default "X-CSRF-Token"
	Secure     bool   // set true in production (HTTPS)
}

// CSRF issues a CSRF token per session (stored in a cookie) and verifies it
// on unsafe methods.
func CSRF(cfg CSRFConfig) func(http.Handler) http.Handler {
	if cfg.FieldName == "" {
		cfg.FieldName = "_csrf"
	}
	if cfg.HeaderName == "" {
		cfg.HeaderName = "X-CSRF-Token"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := tokenFromCookie(r, cfg.CookieName)
			if token == "" {
				token = newCSRFToken()
				http.SetCookie(w, &http.Cookie{
					Name:     cfg.CookieName,
					Value:    token,
					Path:     "/",
					HttpOnly: false, // must be readable by JS to set HX-CSRF header
					Secure:   cfg.Secure,
					SameSite: http.SameSiteLaxMode,
					MaxAge:   12 * 60 * 60, // 12h
				})
			}

			// Enforce on unsafe methods.
			// Enforce on unsafe methods.
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				// safe — fall through
			default:
				submitted := r.Header.Get(cfg.HeaderName)
				if submitted == "" {
					submitted = extractFormToken(r, cfg.FieldName)
				}
				if subtle.ConstantTimeCompare([]byte(submitted), []byte(token)) != 1 {
					http.Error(w, "invalid CSRF token", http.StatusForbidden)
					return
				}
			}

			ctx := context.WithValue(r.Context(), ctxKeyCSRFToken, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CSRFTokenFrom returns the current CSRF token for template rendering.
func CSRFTokenFrom(ctx context.Context) string {
	if t, ok := ctx.Value(ctxKeyCSRFToken).(string); ok {
		return t
	}
	return ""
}

func tokenFromCookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func newCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("csrf: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// extractFormToken reads the CSRF token from a form body, handling both
// urlencoded and multipart content types.
//
// Why this is non-trivial:
//
//   - r.ParseForm() only handles application/x-www-form-urlencoded. It
//     silently ignores multipart bodies, leaving PostForm empty.
//   - r.ParseMultipartForm() handles multipart but does NOT populate
//     r.PostForm for urlencoded requests, so it's not a complete replacement.
//   - r.FormValue("_csrf") checks query, urlencoded POST body, and multipart
//     form body all at once — but it depends on the appropriate Parse* call
//     having run first.
//
// The correct sequence: call both parsers (they're idempotent and safe to
// call on the wrong content type), then use r.FormValue.
//
// We cap the multipart memory usage at 32 MiB (matches net/http's default;
// our upload handler applies its own tighter limit of 10 MiB+margin).
func extractFormToken(r *http.Request, fieldName string) string {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		_ = r.ParseMultipartForm(32 << 20)
	} else {
		_ = r.ParseForm()
	}
	return r.FormValue(fieldName)
}
