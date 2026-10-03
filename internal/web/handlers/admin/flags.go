package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/flags"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// FlagHandler serves the admin feature-flag panel: list, toggle, edit.
// Every write is audit-logged and busts the in-process flag cache so it
// takes effect immediately.
type FlagHandler struct {
	Flags    *postgres.FlagRepo
	Audit    app.AdminAuditWriter
	Provider *flags.Provider
	Renderer *render.Renderer
	Cfg      *config.Config
}

type flagsPage struct {
	Title      string
	CSRFToken  string
	AdminName  string
	AdminEmail string
	Path       string
	Flags      []flags.Flag
	Flash      string
	FormError  string
}

// Index serves GET {admin}/flags.
func (h *FlagHandler) Index(w http.ResponseWriter, r *http.Request) {
	s := middleware.AdminFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/login", http.StatusSeeOther)
		return
	}
	list, err := h.Flags.List(r.Context())
	if err != nil {
		http.Error(w, "could not list flags", http.StatusInternalServerError)
		return
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/flags.html", flagsPage{
		Title:      "Feature Flags",
		CSRFToken:  middleware.CSRFTokenFrom(r.Context()),
		AdminName:  s.Admin.Name,
		AdminEmail: s.Admin.Email,
		Path:       h.Cfg.Admin.Path,
		Flags:      list,
		Flash:      r.URL.Query().Get("flash"),
	})
}

// Toggle handles POST {admin}/flags/{key}/toggle.
func (h *FlagHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	s := middleware.AdminFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/login", http.StatusSeeOther)
		return
	}
	key := r.PathValue("key")
	f, err := h.Flags.Toggle(r.Context(), key, time.Now())
	if err != nil {
		if errors.Is(err, postgres.ErrFlagNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not toggle flag", http.StatusInternalServerError)
		return
	}
	h.afterWrite(r.Context())
	h.audit(s.Admin.ID, "flag.toggle", key, map[string]any{"enabled": f.Enabled}, r)
	state := "off"
	if f.Enabled {
		state = "on"
	}
	http.Redirect(w, r, h.Cfg.Admin.Path+"/flags?flash="+key+"+is+now+"+state+".", http.StatusSeeOther)
}

// Update handles POST {admin}/flags/{key}.
func (h *FlagHandler) Update(w http.ResponseWriter, r *http.Request) {
	s := middleware.AdminFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/login", http.StatusSeeOther)
		return
	}
	key := r.PathValue("key")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	desc := strings.TrimSpace(r.PostFormValue("description"))
	percent, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("rollout_percent")))
	if err != nil || percent < 0 || percent > 100 {
		h.renderError(w, r, "Rollout must be a number between 0 and 100.")
		return
	}
	if name == "" {
		h.renderError(w, r, "Name is required.")
		return
	}
	if len(desc) > 500 {
		h.renderError(w, r, "Description is too long (max 500).")
		return
	}
	_, err = h.Flags.Save(r.Context(), key, postgres.FlagUpdate{
		Name:           name,
		Description:    desc,
		Enabled:        r.PostFormValue("enabled") == "1",
		RolloutPercent: percent,
	}, time.Now())
	if err != nil {
		if errors.Is(err, postgres.ErrFlagNotFound) {
			http.NotFound(w, r)
			return
		}
		h.renderError(w, r, "Could not save flag.")
		return
	}
	h.afterWrite(r.Context())
	h.audit(s.Admin.ID, "flag.update", key, map[string]any{"percent": percent}, r)
	http.Redirect(w, r, h.Cfg.Admin.Path+"/flags?flash="+key+"+updated.", http.StatusSeeOther)
}

func (h *FlagHandler) renderError(w http.ResponseWriter, r *http.Request, msg string) {
	s := middleware.AdminFromContext(r.Context())
	list, _ := h.Flags.List(r.Context())
	data := flagsPage{
		Title:     "Feature Flags",
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
		Path:      h.Cfg.Admin.Path,
		Flags:     list,
		FormError: msg,
	}
	if s != nil {
		data.AdminName = s.Admin.Name
		data.AdminEmail = s.Admin.Email
	}
	h.Renderer.Page(w, http.StatusBadRequest, "layouts/admin.html", "admin/flags.html", data)
}

// afterWrite refreshes the in-process cache so the change takes effect
// immediately on this instance.
func (h *FlagHandler) afterWrite(ctx context.Context) {
	if h.Provider != nil {
		_ = h.Provider.Refresh(ctx)
	}
}

func (h *FlagHandler) audit(adminID uuid.UUID, action, key string, meta map[string]any, r *http.Request) {
	if h.Audit == nil {
		return
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["key"] = key
	_ = h.Audit.Record(r.Context(), adminID, action, "FEATURE_FLAG", uuid.Nil, meta, clientIP(r))
}
