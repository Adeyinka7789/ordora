package handlers

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// AuthHandler groups all authentication HTTP endpoints.
type AuthHandler struct {
	Auth     *auth.Service
	Renderer *render.Renderer
	Cfg      *config.Config
}

type authPage struct {
	Title        string
	CSRFToken    string
	Error        string
	Notice       string
	Email        string
	Name         string
	BusinessName string
	VerifyLink   string

	// Registration analytics fields (raw select values + Other free text,
	// so error re-renders show exactly what the user picked).
	BusinessPhone    string
	BusinessAddress  string
	BusinessType     string
	TypeOther        string
	BusinessCategory string
	CategoryOther    string
	TeamSize         string
	ReferralSource   string
	ReferralOther    string

	// Option lists for the registration selects.
	Categories []string
	Types      []string
	TeamSizes  []string
	Referrals  []string
}

// registerPage returns the base registration page with option lists loaded.
func registerPage() authPage {
	return authPage{
		Title:      "Create account",
		Categories: org.BusinessCategories(),
		Types:      org.BusinessTypes(),
		TeamSizes:  org.TeamSizes(),
		Referrals:  org.ReferralSources(),
	}
}

func (h *AuthHandler) renderAuth(w http.ResponseWriter, r *http.Request, status int, templateName string, page authPage) {
	page.CSRFToken = middleware.CSRFTokenFrom(r.Context())
	h.Renderer.Page(w, status, "layouts/auth.html", templateName, page)
}

// -----------------------------------------------------------------------------
// Register
// -----------------------------------------------------------------------------

func (h *AuthHandler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	if middleware.SessionFromContext(r.Context()) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	h.renderAuth(w, r, http.StatusOK, "auth/register.html", registerPage())
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	page := registerPage()
	page.Email = formValue(r, "email")
	page.Name = formValue(r, "name")
	page.BusinessName = formValue(r, "business_name")
	page.BusinessPhone = formValue(r, "business_phone")
	page.BusinessAddress = formValue(r, "business_address")
	page.BusinessType = formValue(r, "business_type")
	page.TypeOther = formValue(r, "business_type_other")
	page.BusinessCategory = formValue(r, "business_category")
	page.CategoryOther = formValue(r, "business_category_other")
	page.TeamSize = formValue(r, "team_size")
	page.ReferralSource = formValue(r, "referral_source")
	page.ReferralOther = formValue(r, "referral_source_other")

	in := auth.RegisterInput{
		Email:            page.Email,
		Password:         r.PostFormValue("password"),
		Name:             page.Name,
		BusinessName:     page.BusinessName,
		BusinessPhone:    page.BusinessPhone,
		BusinessAddress:  page.BusinessAddress,
		BusinessType:     org.ResolveOther(page.BusinessType, page.TypeOther),
		BusinessCategory: org.ResolveOther(page.BusinessCategory, page.CategoryOther),
		TeamSize:         page.TeamSize,
		ReferralSource:   org.ResolveOther(page.ReferralSource, page.ReferralOther),
	}
	if err := validatePasswordConfirm(in.Password, r.PostFormValue("password_confirm")); err != nil {
		page.Error = err.Error()
		h.renderAuth(w, r, http.StatusBadRequest, "auth/register.html", page)
		return
	}

	result, err := h.Auth.Register(r.Context(), in)
	if err != nil {
		page.Error = humanizeAuthError(err)
		h.renderAuth(w, r, http.StatusBadRequest, "auth/register.html", page)
		return
	}

	verifyURL := h.Cfg.BaseURL + "/verify?token=" + result.VerifyToken
	if err := h.Auth.SendVerificationEmail(r.Context(), result.User, verifyURL); err != nil {
		slog.Error("auth: send verify email", "err", err, "user_id", result.User.ID)
	}

	page = authPage{
		Title:      "Check your email",
		Email:      result.User.Email.String(),
		VerifyLink: verifyURL,
	}
	h.renderAuth(w, r, http.StatusOK, "auth/verify_sent.html", page)
}

// -----------------------------------------------------------------------------
// Login
// -----------------------------------------------------------------------------

func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if middleware.SessionFromContext(r.Context()) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	h.renderAuth(w, r, http.StatusOK, "auth/login.html", authPage{Title: "Sign in"})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	in := auth.LoginInput{
		Email:     r.PostFormValue("email"),
		Password:  r.PostFormValue("password"),
		UserAgent: r.UserAgent(),
		IP:        middleware.ClientIP(r),
	}
	result, err := h.Auth.Login(r.Context(), in)
	if err != nil {
		page := authPage{
			Title: "Sign in",
			Error: humanizeAuthError(err),
			Email: in.Email,
		}
		h.renderAuth(w, r, http.StatusUnauthorized, "auth/login.html", page)
		return
	}

	h.setSessionCookie(w, result.RawToken)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// -----------------------------------------------------------------------------
// Logout
// -----------------------------------------------------------------------------

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s != nil {
		_ = h.Auth.Logout(r.Context(), s.Session.ID)
	}
	h.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// -----------------------------------------------------------------------------
// Email verification
// -----------------------------------------------------------------------------

func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		h.renderAuth(w, r, http.StatusBadRequest, "auth/verify_sent.html", authPage{
			Title: "Verification link invalid",
			Error: "This link is missing its token.",
		})
		return
	}
	_, err := h.Auth.VerifyEmail(r.Context(), token)
	if err != nil {
		h.renderAuth(w, r, http.StatusBadRequest, "auth/verify_sent.html", authPage{
			Title: "Verification failed",
			Error: humanizeAuthError(err),
		})
		return
	}
	h.renderAuth(w, r, http.StatusOK, "auth/verify_sent.html", authPage{
		Title:  "Email verified",
		Notice: "Your email is verified. You can now sign in.",
	})
}

// -----------------------------------------------------------------------------
// Password reset
// -----------------------------------------------------------------------------

func (h *AuthHandler) ForgotPage(w http.ResponseWriter, r *http.Request) {
	h.renderAuth(w, r, http.StatusOK, "auth/forgot.html", authPage{Title: "Reset your password"})
}

func (h *AuthHandler) Forgot(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := r.PostFormValue("email")
	_ = h.Auth.RequestPasswordReset(r.Context(), email)
	h.renderAuth(w, r, http.StatusOK, "auth/forgot.html", authPage{
		Title:  "Reset your password",
		Notice: "If that email is registered, a reset link is on the way.",
		Email:  email,
	})
}

func (h *AuthHandler) ResetPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Redirect(w, r, "/password/forgot", http.StatusSeeOther)
		return
	}
	page := authPage{Title: "Choose a new password"}
	h.renderAuthWithToken(w, r, http.StatusOK, "auth/reset.html", page, token)
}

func (h *AuthHandler) Reset(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	token := r.PostFormValue("token")
	password := r.PostFormValue("password")
	if token == "" {
		http.Redirect(w, r, "/password/forgot", http.StatusSeeOther)
		return
	}
	if err := h.Auth.ResetPassword(r.Context(), token, password); err != nil {
		h.renderAuthWithToken(w, r, http.StatusBadRequest, "auth/reset.html", authPage{
			Title: "Choose a new password",
			Error: humanizeAuthError(err),
		}, token)
		return
	}
	h.renderAuth(w, r, http.StatusOK, "auth/login.html", authPage{
		Title:  "Sign in",
		Notice: "Password reset. Please sign in with your new password.",
	})
}

// -----------------------------------------------------------------------------
// Cookie helpers
// -----------------------------------------------------------------------------

func (h *AuthHandler) setSessionCookie(w http.ResponseWriter, raw string) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Session.CookieName,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		Secure:   !h.Cfg.IsDev(),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(h.Cfg.Session.TTL),
	})
}

func (h *AuthHandler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Session.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   !h.Cfg.IsDev(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// -----------------------------------------------------------------------------
// Error humanization
// -----------------------------------------------------------------------------

// validatePasswordConfirm ensures the two password entries match. The client
// also checks live, but the server is authoritative — never trust the browser.
func validatePasswordConfirm(password, confirm string) error {
	if password != confirm {
		return errors.New("Passwords do not match. Please enter the same password twice.")
	}
	return nil
}

func humanizeAuthError(err error) string {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		return "Email or password is incorrect."
	case errors.Is(err, auth.ErrEmailNotVerified):
		return "Please verify your email before signing in."
	case errors.Is(err, auth.ErrTokenInvalid):
		return "This link is invalid or has expired."
	case errors.Is(err, auth.ErrNoMembership):
		return "This account has no active organization."
	case errors.Is(err, auth.ErrPasswordTooShort):
		return "Password must be at least 10 characters."
	case errors.Is(err, auth.ErrPasswordTooLong):
		return "Password must be at most 256 characters."
	case errors.Is(err, org.ErrProfileCategoryRequired):
		return "Please choose your business category."
	case errors.Is(err, org.ErrProfileCategoryInvalid):
		return "Please choose a valid business category."
	case errors.Is(err, org.ErrProfileTypeInvalid):
		return "Please choose a valid business type."
	case errors.Is(err, org.ErrProfileTeamSizeInvalid):
		return "Please choose a valid team size."
	case errors.Is(err, org.ErrProfileReferralInvalid):
		return "Please choose a valid option for how you heard about us."
	case errors.Is(err, org.ErrProfilePhoneInvalid):
		return "Please enter a valid business phone number."
	default:
		slog.Error("auth: unhandled error", "err", err)
		return "Something went wrong. Please try again."
	}
}

func (h *AuthHandler) renderAuthWithToken(w http.ResponseWriter, r *http.Request, status int, templateName string, page authPage, token string) {
	page.CSRFToken = middleware.CSRFTokenFrom(r.Context())
	data := struct {
		authPage
		ResetToken string
	}{
		authPage:   page,
		ResetToken: token,
	}
	h.Renderer.Page(w, status, "layouts/auth.html", templateName, data)
}

// unused guard, can be removed later
var _ = net.SplitHostPort
var _ = strings.TrimSpace
