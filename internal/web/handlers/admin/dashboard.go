package admin

import (
	"net/http"

	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// DashboardHandler serves the admin landing page.
type DashboardHandler struct {
	Renderer *render.Renderer
	Cfg      *config.Config
}

type dashboardPageData struct {
	Title      string
	CSRFToken  string
	AdminName  string
	AdminEmail string
	Path       string
}

// Index serves GET {admin}/dashboard.
func (h *DashboardHandler) Index(w http.ResponseWriter, r *http.Request) {
	s := middleware.AdminFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/login", http.StatusSeeOther)
		return
	}

	data := dashboardPageData{
		Title:      "Admin dashboard",
		CSRFToken:  middleware.CSRFTokenFrom(r.Context()),
		AdminName:  s.Admin.Name,
		AdminEmail: s.Admin.Email,
		Path:       h.Cfg.Admin.Path,
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/dashboard.html", data)
}
