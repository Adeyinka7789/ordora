package handlers

import (
	"net/http"

	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// DashboardHandler serves the post-login landing page.
type DashboardHandler struct {
	Renderer *render.Renderer
}

type dashboardPage struct {
	Title     string
	UserName  string
	UserEmail string
	OrgName   string
	OrgRole   string
	CSRFToken string
}

func (h *DashboardHandler) Index(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	data := dashboardPage{
		Title:     "Dashboard",
		UserName:  s.User.Name,
		UserEmail: s.User.Email.String(),
		OrgRole:   string(s.Scope.Role),
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/app.html", "dashboard/index.html", data)
}
