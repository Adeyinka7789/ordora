package admin

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// UserHandler serves admin user pages.
type UserHandler struct {
	Service  *app.AdminService
	Attempts *postgres.LoginAttemptRepo
	Audit    app.AdminAuditWriter
	Renderer *render.Renderer
	Cfg      *config.Config
}

type usersListPageData struct {
	Title      string
	CSRFToken  string
	AdminName  string
	AdminEmail string
	Path       string
	Query      string
	Users      []app.AdminUserRow
	Total      int
	Page       int
	PerPage    int
	HasPrev    bool
	HasNext    bool
}

// Index serves GET {admin}/users.
func (h *UserHandler) Index(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	page := atoiOr(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	perPage := 25
	offset := (page - 1) * perPage

	users, total, err := h.Service.ListUsers(r.Context(), query, perPage, offset)
	if err != nil {
		http.Error(w, "could not list users", http.StatusInternalServerError)
		return
	}

	s := middleware.AdminFromContext(r.Context())
	data := usersListPageData{
		Title:     "Users",
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
		Path:      h.Cfg.Admin.Path,
		Query:     query,
		Users:     users,
		Total:     total,
		Page:      page,
		PerPage:   perPage,
		HasPrev:   page > 1,
		HasNext:   offset+len(users) < total,
	}
	if s != nil {
		data.AdminName = s.Admin.Name
		data.AdminEmail = s.Admin.Email
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/users_list.html", data)
}

type userDetailPageData struct {
	Title      string
	CSRFToken  string
	AdminName  string
	AdminEmail string
	Path       string
	User       *app.AdminUserDetail
	Lockout    *userLockoutView
	Flash      string
}

// userLockoutView is the brute-force lockout state for the user's email.
type userLockoutView struct {
	Failures    int
	Locked      bool
	LockedUntil string
	Remaining   string
}

// Show serves GET {admin}/users/{id}.
func (h *UserHandler) Show(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	user, err := h.Service.GetUser(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load user", http.StatusInternalServerError)
		return
	}
	s := middleware.AdminFromContext(r.Context())
	data := userDetailPageData{
		Title:     user.Name,
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
		Path:      h.Cfg.Admin.Path,
		User:      user,
		Lockout:   h.lockoutView(r.Context(), user.Email),
	}
	if s != nil {
		data.AdminName = s.Admin.Name
		data.AdminEmail = s.Admin.Email
	}
	if v := r.URL.Query().Get("flash"); v != "" {
		data.Flash = v
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/user_detail.html", data)
}

// lockoutView loads the login-attempt state for display. Best-effort: a
// store failure yields no panel, never a 500 on the user page.
func (h *UserHandler) lockoutView(ctx context.Context, email string) *userLockoutView {
	if h.Attempts == nil || email == "" {
		return nil
	}
	a, err := h.Attempts.Get(ctx, email)
	if err != nil || !a.Found {
		return nil
	}
	view := &userLockoutView{Failures: a.Failures}
	if a.LockedUntil != nil {
		view.LockedUntil = a.LockedUntil.Format("02-01-2006 · 15:04")
		if remain := time.Until(*a.LockedUntil); remain > 0 {
			view.Locked = true
			view.Remaining = formatDurationShort(remain)
		}
	}
	return view
}

func formatDurationShort(d time.Duration) string {
	if d < time.Minute {
		return "under a minute"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return strconv.Itoa(h) + "h " + strconv.Itoa(m) + "m"
	}
	return strconv.Itoa(m) + "m"
}

// ForceLogout handles POST {admin}/users/{id}/logout.
func (h *UserHandler) ForceLogout(w http.ResponseWriter, r *http.Request) {
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
	_, err = h.Service.ForceLogoutUser(r.Context(), s.Admin.ID, id, clientIP(r))
	if err != nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/users/"+id.String()+"?flash=Could+not+revoke+sessions", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, h.Cfg.Admin.Path+"/users/"+id.String()+"?flash=All+sessions+revoked", http.StatusSeeOther)
}

// ClearLockout handles POST {admin}/users/{id}/lockout/clear: reset the
// brute-force counter so the user can log in immediately.
func (h *UserHandler) ClearLockout(w http.ResponseWriter, r *http.Request) {
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
	if h.Attempts == nil {
		http.Error(w, "attempt store not configured", http.StatusInternalServerError)
		return
	}
	u, err := h.Service.GetUser(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.Attempts.Clear(r.Context(), u.Email); err != nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/users/"+id.String()+"?flash=Could+not+clear+lockout", http.StatusSeeOther)
		return
	}
	if h.Audit != nil {
		_ = h.Audit.Record(r.Context(), s.Admin.ID, "user.lockout_cleared", "USER", id,
			map[string]any{"email": u.Email}, clientIP(r))
	}
	http.Redirect(w, r, h.Cfg.Admin.Path+"/users/"+id.String()+"?flash=Login+lockout+cleared", http.StatusSeeOther)
}

// setMemberStatus handles POST {admin}/users/{id}/memberships/{orgID}/disable|enable.
func (h *UserHandler) setMemberStatus(w http.ResponseWriter, r *http.Request, status string) {
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
	orgID, err := uuid.Parse(r.PathValue("orgID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.Service.SetUserMemberStatus(r.Context(), s.Admin.ID, id, orgID, status, clientIP(r)); err != nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/users/"+id.String()+"?flash=Could+not+update+membership", http.StatusSeeOther)
		return
	}
	msg := "Membership+blocked.+The+user+loses+access+on+their+next+request."
	if status == "ACTIVE" {
		msg = "Membership+unblocked."
	}
	http.Redirect(w, r, h.Cfg.Admin.Path+"/users/"+id.String()+"?flash="+msg, http.StatusSeeOther)
}

// DisableMember handles POST {admin}/users/{id}/memberships/{orgID}/disable.
func (h *UserHandler) DisableMember(w http.ResponseWriter, r *http.Request) {
	h.setMemberStatus(w, r, "DISABLED")
}

// EnableMember handles POST {admin}/users/{id}/memberships/{orgID}/enable.
func (h *UserHandler) EnableMember(w http.ResponseWriter, r *http.Request) {
	h.setMemberStatus(w, r, "ACTIVE")
}
