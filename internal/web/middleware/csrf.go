package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
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
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				// safe — fall through
			default:
				submitted := r.Header.Get(cfg.HeaderName)
				if submitted == "" {
					// Try form (ParseForm is idempotent).
					_ = r.ParseForm()
					submitted = r.PostFormValue(cfg.FieldName)
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
