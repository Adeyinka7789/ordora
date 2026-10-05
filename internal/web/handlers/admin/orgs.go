package admin

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/flags"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// OrgHandler serves admin org pages.
type OrgHandler struct {
	Service  *app.AdminService
	Flags    *postgres.FlagRepo
	Provider *flags.Provider
	Audit    app.AdminAuditWriter
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
	Flags      []orgFlagRow
	Flash      string
}

// orgFlagRow is one flag as seen by this tenant: the global state plus
// any per-org override (which wins).
type orgFlagRow struct {
	Key         string
	Name        string
	GlobalOn    bool
	EffectiveOn bool
	Override    *bool
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
		Flags:     h.flagRows(r.Context(), id),
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

// flagRows builds the per-tenant flag table. Best-effort: a flag-store
// failure yields an empty section, never a 500 on the tenant page.
func (h *OrgHandler) flagRows(ctx context.Context, orgID uuid.UUID) []orgFlagRow {
	if h.Flags == nil {
		return nil
	}
	list, err := h.Flags.List(ctx)
	if err != nil {
		return nil
	}
	overrides, err := h.Flags.OverridesForOrg(ctx, orgID)
	if err != nil {
		overrides = map[string]bool{}
	}
	rows := make([]orgFlagRow, 0, len(list))
	for _, f := range list {
		row := orgFlagRow{Key: f.Key, Name: f.Name, GlobalOn: f.Enabled}
		if v, ok := overrides[f.Key]; ok {
			v := v
			row.Override = &v
			row.EffectiveOn = v
		} else if h.Provider != nil {
			row.EffectiveOn = h.Provider.Enabled(orgID, f.Key)
		} else {
			row.EffectiveOn = f.Enabled
		}
		rows = append(rows, row)
	}
	return rows
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

// -----------------------------------------------------------------------------
// Per-org feature-flag overrides.
// -----------------------------------------------------------------------------

// setFlagOverride handles POST {admin}/orgs/{id}/flags/{key}/on|off: force
// the flag on or off for this tenant only.
func (h *OrgHandler) setFlagOverride(w http.ResponseWriter, r *http.Request, enabled bool) {
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
	key := r.PathValue("key")
	if h.Flags == nil {
		http.Error(w, "flag store not configured", http.StatusInternalServerError)
		return
	}
	if err := h.Flags.SetOverride(r.Context(), key, id, enabled, time.Now()); err != nil {
		if errors.Is(err, postgres.ErrFlagNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, h.Cfg.Admin.Path+"/orgs/"+id.String()+"?flash=Could+not+save+override", http.StatusSeeOther)
		return
	}
	h.refreshFlags(r.Context())
	h.auditOverride(s.Admin.ID, id, key, enabled, r)
	state := "off for this business"
	if enabled {
		state = "on for this business"
	}
	http.Redirect(w, r, h.Cfg.Admin.Path+"/orgs/"+id.String()+"?flash="+key+"+is+now+"+state+".", http.StatusSeeOther)
}

// FlagOn handles POST {admin}/orgs/{id}/flags/{key}/on.
func (h *OrgHandler) FlagOn(w http.ResponseWriter, r *http.Request) {
	h.setFlagOverride(w, r, true)
}

// FlagOff handles POST {admin}/orgs/{id}/flags/{key}/off.
func (h *OrgHandler) FlagOff(w http.ResponseWriter, r *http.Request) {
	h.setFlagOverride(w, r, false)
}

// FlagClear handles POST {admin}/orgs/{id}/flags/{key}/clear: drop the
// override so the tenant follows the global rule again.
func (h *OrgHandler) FlagClear(w http.ResponseWriter, r *http.Request) {
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
	key := r.PathValue("key")
	if h.Flags == nil {
		http.Error(w, "flag store not configured", http.StatusInternalServerError)
		return
	}
	if err := h.Flags.ClearOverride(r.Context(), key, id); err != nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/orgs/"+id.String()+"?flash=Could+not+clear+override", http.StatusSeeOther)
		return
	}
	h.refreshFlags(r.Context())
	h.auditOverrideClear(s.Admin.ID, id, key, r)
	http.Redirect(w, r, h.Cfg.Admin.Path+"/orgs/"+id.String()+"?flash="+key+"+follows+the+global+rule+again.", http.StatusSeeOther)
}

func (h *OrgHandler) refreshFlags(ctx context.Context) {
	if h.Provider != nil {
		_ = h.Provider.Refresh(ctx)
	}
}

func (h *OrgHandler) auditOverride(adminID, orgID uuid.UUID, key string, enabled bool, r *http.Request) {
	if h.Audit == nil {
		return
	}
	_ = h.Audit.Record(r.Context(), adminID, "flag.override_set", "ORGANIZATION", orgID,
		map[string]any{"key": key, "enabled": enabled}, clientIP(r))
}

func (h *OrgHandler) auditOverrideClear(adminID, orgID uuid.UUID, key string, r *http.Request) {
	if h.Audit == nil {
		return
	}
	_ = h.Audit.Record(r.Context(), adminID, "flag.override_clear", "ORGANIZATION", orgID,
		map[string]any{"key": key}, clientIP(r))
}
