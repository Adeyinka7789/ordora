package admin

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// UserHandler serves admin user pages.
type UserHandler struct {
	Service  *app.AdminService
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
	Flash      string
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
