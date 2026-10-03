package admin

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// OrgHandler serves admin org pages.
type OrgHandler struct {
	Service  *app.AdminService
	Renderer *render.Renderer
	Cfg      *config.Config
}

type orgsListPageData struct {
	Title      string
	CSRFToken  string
	AdminName  string
	AdminEmail string
	Path       string
	Query      string
	Orgs       []app.AdminOrgRow
	Total      int
	Page       int
	PerPage    int
	HasPrev    bool
	HasNext    bool
}

// Index serves GET {admin}/orgs.
func (h *OrgHandler) Index(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	page := atoiOr(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	perPage := 25
	offset := (page - 1) * perPage

	orgs, total, err := h.Service.ListOrgs(r.Context(), query, perPage, offset)
	if err != nil {
		http.Error(w, "could not list orgs", http.StatusInternalServerError)
		return
	}

	s := middleware.AdminFromContext(r.Context())
	data := orgsListPageData{
		Title:     "Tenants",
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
		Path:      h.Cfg.Admin.Path,
		Query:     query,
		Orgs:      orgs,
		Total:     total,
		Page:      page,
		PerPage:   perPage,
		HasPrev:   page > 1,
		HasNext:   offset+len(orgs) < total,
	}
	if s != nil {
		data.AdminName = s.Admin.Name
		data.AdminEmail = s.Admin.Email
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/orgs_list.html", data)
}

type orgDetailPageData struct {
	Title      string
	CSRFToken  string
	AdminName  string
	AdminEmail string
	Path       string
	Org        *app.AdminOrgDetail
	Flash      string
}

// Show serves GET {admin}/orgs/{id}.
func (h *OrgHandler) Show(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	detail, err := h.Service.GetOrg(r.Context(), id)
	if err != nil {
		if errors.Is(err, org.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load org", http.StatusInternalServerError)
		return
	}
	s := middleware.AdminFromContext(r.Context())
	data := orgDetailPageData{
		Title:     detail.Name,
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
		Path:      h.Cfg.Admin.Path,
		Org:       detail,
	}
	if s != nil {
		data.AdminName = s.Admin.Name
		data.AdminEmail = s.Admin.Email
	}
	if v := r.URL.Query().Get("flash"); v != "" {
		data.Flash = v
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/org_detail.html", data)
}

func atoiOr(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return fallback
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// Suspend handles POST {admin}/orgs/{id}/suspend.
func (h *OrgHandler) Suspend(w http.ResponseWriter, r *http.Request) {
	s := middleware.AdminFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/login", http.StatusSeeOther)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	reason := r.PostFormValue("reason")
	if err := h.Service.SuspendOrg(r.Context(), s.Admin.ID, id, reason, clientIP(r)); err != nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/orgs/"+id.String()+"?flash=Could+not+suspend", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, h.Cfg.Admin.Path+"/orgs/"+id.String()+"?flash=Business+suspended", http.StatusSeeOther)
}

// Unsuspend handles POST {admin}/orgs/{id}/unsuspend.
func (h *OrgHandler) Unsuspend(w http.ResponseWriter, r *http.Request) {
	s := middleware.AdminFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/login", http.StatusSeeOther)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.Service.UnsuspendOrg(r.Context(), s.Admin.ID, id, clientIP(r)); err != nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/orgs/"+id.String()+"?flash=Could+not+unsuspend", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, h.Cfg.Admin.Path+"/orgs/"+id.String()+"?flash=Business+unsuspended", http.StatusSeeOther)
}

// Delete handles POST {admin}/orgs/{id}/delete. Irreversible.
func (h *OrgHandler) Delete(w http.ResponseWriter, r *http.Request) {
	s := middleware.AdminFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/login", http.StatusSeeOther)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// Require the admin to type the org slug to confirm.
	confirm := r.PostFormValue("confirm_slug")
	detail, err := h.Service.GetOrg(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if confirm != detail.Slug {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/orgs/"+id.String()+"?flash=Confirmation+did+not+match", http.StatusSeeOther)
		return
	}
	if err := h.Service.DeleteOrg(r.Context(), s.Admin.ID, id, clientIP(r)); err != nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/orgs/"+id.String()+"?flash=Delete+failed", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, h.Cfg.Admin.Path+"/orgs?flash=Business+deleted", http.StatusSeeOther)
}
