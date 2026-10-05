package handlers

import (
	"net/http"

	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/domain/user"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// OnboardingHandler serves the first-run wizard. Only users who have never
// finished or skipped it get here (login + dashboard both redirect); the
// wizard itself sends everyone else back to the dashboard.
type OnboardingHandler struct {
	Auth     *auth.Service
	Renderer *render.Renderer
}

// Show handles GET /onboarding.
func (h *OnboardingHandler) Show(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil || s.User == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !s.User.NeedsOnboarding() {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	page := authPage{
		Title:     "Welcome to Ordora",
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
		Name:      s.User.Name,
	}
	// Standalone takeover page (no sidebar/layout chrome).
	h.Renderer.RenderFragment(w, http.StatusOK, "onboarding/show.html", page)
}

// finish marks the wizard seen (finished or skipped — seen is seen) and
// sends the user to the dashboard.
func (h *OnboardingHandler) finish(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil || s.User == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// Best-effort: even if the write fails, the dashboard remains usable and
	// the wizard simply shows again next login.
	_ = h.Auth.CompleteOnboarding(r.Context(), s.User.ID)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// Complete handles POST /onboarding/complete (finished the last step).
func (h *OnboardingHandler) Complete(w http.ResponseWriter, r *http.Request) {
	h.finish(w, r)
}

// Skip handles POST /onboarding/skip (dismissed early).
func (h *OnboardingHandler) Skip(w http.ResponseWriter, r *http.Request) {
	h.finish(w, r)
}

// afterLoginTarget decides where a fresh login lands: first-timers see the
// wizard, everyone else goes to the dashboard.
func afterLoginTarget(u *user.User) string {
	if u != nil && u.NeedsOnboarding() {
		return "/onboarding"
	}
	return "/dashboard"
}
