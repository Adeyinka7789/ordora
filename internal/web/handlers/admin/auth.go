// Package admin contains HTTP handlers for the platform admin panel.
// Handlers in this package are mounted under the configurable admin path
// (e.g. /ops-x9k2m) and use a separate session cookie.
package admin

import (
	"errors"
	"net/http"
	"time"

	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// AuthHandler serves admin login/logout.
type AuthHandler struct {
	Auth     *auth.AdminAuthService
	Renderer *render.Renderer
	Cfg      *config.Config
}

type loginPageData struct {
	Title     string
	CSRFToken string
	Error     string
	Path      string
}

// LoginPage serves GET {admin}/login.
func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	// If already logged in, redirect to admin dashboard.
	if s := middleware.AdminFromContext(r.Context()); s != nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/dashboard", http.StatusSeeOther)
		return
	}

	data := loginPageData{
		Title:     "Admin login",
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
		Path:      h.Cfg.Admin.Path,
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/admin_login.html", "admin/login.html", data)
}

// Login handles POST {admin}/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	in := auth.AdminLoginInput{
		Email:     r.PostFormValue("email"),
		Password:  r.PostFormValue("password"),
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	}
	result, err := h.Auth.Login(r.Context(), in)
	if err != nil {
		if !errors.Is(err, auth.ErrAdminInvalidCredentials) &&
			!errors.Is(err, auth.ErrAdminDisabled) {
			h.Renderer.Page(w, http.StatusInternalServerError, "layouts/admin_login.html", "admin/login.html", loginPageData{
				Title:     "Admin login",
				CSRFToken: middleware.CSRFTokenFrom(r.Context()),
				Error:     "Unable to sign you in right now.",
				Path:      h.Cfg.Admin.Path,
			})
			return
		}
		msg := "Invalid email or password."
		switch {
		case errors.Is(err, auth.ErrAdminDisabled):
			msg = "This admin account is disabled."
		}
		data := loginPageData{
			Title:     "Admin login",
			CSRFToken: middleware.CSRFTokenFrom(r.Context()),
			Error:     msg,
			Path:      h.Cfg.Admin.Path,
		}
		h.Renderer.Page(w, http.StatusUnauthorized, "layouts/admin_login.html", "admin/login.html", data)
		return
	}

	// Set admin session cookie. HttpOnly, Secure on https, SameSite=Strict.
	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Admin.CookieName,
		Value:    result.RawToken,
		Path:     h.Cfg.Admin.Path, // scoped to admin path only
		HttpOnly: true,
		Secure:   h.Cfg.SecureCookies(),
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(h.Cfg.Admin.SessionTTL),
	})

	http.Redirect(w, r, h.Cfg.Admin.Path+"/dashboard", http.StatusSeeOther)
}

// Logout handles POST {admin}/logout.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if s := middleware.AdminFromContext(r.Context()); s != nil {
		_ = h.Auth.Logout(r.Context(), s.Session.ID)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Admin.CookieName,
		Value:    "",
		Path:     h.Cfg.Admin.Path,
		HttpOnly: true,
		Secure:   h.Cfg.SecureCookies(),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	http.Redirect(w, r, h.Cfg.Admin.Path+"/login", http.StatusSeeOther)
}

// clientIP delegates to the shared trusted-proxy-aware extractor so audit
// logs cannot be spoofed with a forged X-Forwarded-For from an untrusted peer.
func clientIP(r *http.Request) string {
	return middleware.ClientIP(r)
}
