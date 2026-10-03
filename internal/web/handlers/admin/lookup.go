package admin

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// LookupHandler serves the support lookup tools.
type LookupHandler struct {
	Service  *app.AdminLookupService
	Renderer *render.Renderer
	Cfg      *config.Config
}

type lookupPageData struct {
	Title      string
	CSRFToken  string
	AdminName  string
	AdminEmail string
	Path       string

	Query string
	Mode  string // "order" | "email" | "phone" | "jobs" | ""

	Order     *app.OrderLookup
	Customers []*app.CustomerLookup
	Jobs      []app.NotificationJobRow

	Error string
	Flash string
}

// Index serves GET {admin}/lookup.
func (h *LookupHandler) Index(w http.ResponseWriter, r *http.Request) {
	s := middleware.AdminFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/login", http.StatusSeeOther)
		return
	}

	// Parse query.
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))

	data := lookupPageData{
		Title:      "Support lookup",
		CSRFToken:  middleware.CSRFTokenFrom(r.Context()),
		Path:       h.Cfg.Admin.Path,
		Query:      q,
		Mode:       mode,
		AdminName:  s.Admin.Name,
		AdminEmail: s.Admin.Email,
	}
	if v := r.URL.Query().Get("flash"); v != "" {
		data.Flash = v
	}
	if v := r.URL.Query().Get("error"); v != "" {
		data.Error = v
	}

	// If no query, just render the form.
	if q == "" && mode == "" {
		h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/lookup.html", data)
		return
	}

	switch mode {
	case "order":
		o, err := h.Service.Order(r.Context(), s.Admin.ID, q)
		if err != nil {
			data.Error = "Order not found."
		} else {
			data.Order = o
		}
	case "email":
		rows, err := h.Service.CustomersByEmail(r.Context(), s.Admin.ID, q)
		if err != nil {
			data.Error = "Lookup failed."
		} else {
			data.Customers = rows
		}
	case "phone":
		rows, err := h.Service.CustomersByPhone(r.Context(), s.Admin.ID, q)
		if err != nil {
			data.Error = "Lookup failed."
		} else {
			data.Customers = rows
		}
	case "jobs":
		jobs, err := h.Service.RecentJobs(r.Context(), 50)
		if err != nil {
			data.Error = "Could not load jobs."
		} else {
			data.Jobs = jobs
		}
	default:
		data.Error = "Unknown lookup mode."
	}

	h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/lookup.html", data)
}

// ResendJob handles POST {admin}/lookup/jobs/{id}/resend.
func (h *LookupHandler) ResendJob(w http.ResponseWriter, r *http.Request) {
	s := middleware.AdminFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/login", http.StatusSeeOther)
		return
	}
	jobID, err := parseUUIDPath(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, err = h.Service.ResendJob(r.Context(), s.Admin.ID, jobID)
	if err != nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/lookup?mode=jobs&error=Could+not+resend+job", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, h.Cfg.Admin.Path+"/lookup?mode=jobs&flash=Job+re-enqueued", http.StatusSeeOther)
}

func parseUUIDPath(r *http.Request, key string) (uuid.UUID, error) {
	return uuid.Parse(r.PathValue(key))
}
