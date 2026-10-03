package admin

import (
	"net/http"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// OpsHandler serves the ops dashboard and audit log.
type OpsHandler struct {
	Service  *app.AdminOpsService
	Renderer *render.Renderer
	Cfg      *config.Config
}

type opsPageData struct {
	Title      string
	CSRFToken  string
	AdminName  string
	AdminEmail string
	Path       string
	Health     *app.HealthSummary
	Outbox     *app.OutboxSummary
	Notifs     *app.NotificationSummary
}

// Ops serves GET {admin}/ops.
func (h *OpsHandler) Ops(w http.ResponseWriter, r *http.Request) {
	health, err := h.Service.Health(r.Context())
	if err != nil {
		http.Error(w, "could not load health", http.StatusInternalServerError)
		return
	}
	outbox, _ := h.Service.Outbox(r.Context())
	notifs, _ := h.Service.Notifications(r.Context())

	s := middleware.AdminFromContext(r.Context())
	data := opsPageData{
		Title:     "Ops dashboard",
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
		Path:      h.Cfg.Admin.Path,
		Health:    health,
		Outbox:    outbox,
		Notifs:    notifs,
	}
	if s != nil {
		data.AdminName = s.Admin.Name
		data.AdminEmail = s.Admin.Email
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/ops.html", data)
}

type auditPageData struct {
	Title      string
	CSRFToken  string
	AdminName  string
	AdminEmail string
	Path       string

	Action     string
	TargetType string
	Entries    []app.AdminAuditEntry
	Total      int
	Page       int
	HasPrev    bool
	HasNext    bool
}

// Audit serves GET {admin}/audit.
func (h *OpsHandler) Audit(w http.ResponseWriter, r *http.Request) {
	s := middleware.AdminFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/login", http.StatusSeeOther)
		return
	}
	action := r.URL.Query().Get("action")
	targetType := r.URL.Query().Get("target_type")
	page := atoiOr(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	perPage := 50
	offset := (page - 1) * perPage

	entries, total, err := h.Service.ListAudit(r.Context(), action, targetType, perPage, offset)
	if err != nil {
		http.Error(w, "could not load audit log", http.StatusInternalServerError)
		return
	}

	data := auditPageData{
		Title:      "Audit log",
		CSRFToken:  middleware.CSRFTokenFrom(r.Context()),
		Path:       h.Cfg.Admin.Path,
		Action:     action,
		TargetType: targetType,
		Entries:    entries,
		Total:      total,
		Page:       page,
		HasPrev:    page > 1,
		HasNext:    offset+len(entries) < total,
		AdminName:  s.Admin.Name,
		AdminEmail: s.Admin.Email,
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/audit_list.html", data)
}
