package handlers

import (
	"net/http"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// DashboardHandler serves the post-login landing page.
type DashboardHandler struct {
	Service  *app.DashboardService
	Renderer *render.Renderer
}

type dashboardPage struct {
	Title       string
	CSRFToken   string
	UserName    string
	UserEmail   string
	OrgRole     string
	Stats       *app.DashboardStats
	FlashNotice string
	FlashError  string
}

func (h *DashboardHandler) Index(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	// First-run users belong in the wizard, even via a deep link.
	if s.User != nil && s.User.NeedsOnboarding() {
		http.Redirect(w, r, "/onboarding", http.StatusSeeOther)
		return
	}

	scope := s.Scope
	page := dashboardPage{
		Title:     "Dashboard",
		CSRFToken: csrfFromCtx(r),
		UserName:  s.User.Name,
		UserEmail: s.User.Email.String(),
		OrgRole:   string(scope.Role),
	}

	if !scope.IsZero() {
		if stats, err := h.Service.Load(r.Context(), scope, currencyFromRequest(r)); err == nil {
			page.Stats = stats
		}
	}

	if v := queryValue(r, "notice"); v != "" {
		page.FlashNotice = v
	}
	if v := queryValue(r, "error"); v != "" {
		page.FlashError = v
	}

	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "dashboard/index.html", page)
}
