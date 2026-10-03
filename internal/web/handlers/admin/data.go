package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// DataHandler serves the generic data browser.
type DataHandler struct {
	Service  *app.AdminDataService
	Renderer *render.Renderer
	Cfg      *config.Config
}

type tablesListPageData struct {
	Title      string
	CSRFToken  string
	AdminName  string
	AdminEmail string
	Path       string
	Tables     []app.BrowsableTable
}

// Tables serves GET {admin}/data.
func (h *DataHandler) Tables(w http.ResponseWriter, r *http.Request) {
	tables, err := h.Service.ListTables(r.Context())
	if err != nil {
		http.Error(w, "could not list tables", http.StatusInternalServerError)
		return
	}
	s := middleware.AdminFromContext(r.Context())
	data := tablesListPageData{
		Title:     "Data browser",
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
		Path:      h.Cfg.Admin.Path,
		Tables:    tables,
	}
	if s != nil {
		data.AdminName = s.Admin.Name
		data.AdminEmail = s.Admin.Email
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/data_list.html", data)
}

type tableRowsPageData struct {
	Title      string
	CSRFToken  string
	AdminName  string
	AdminEmail string
	Path       string

	Table  *app.BrowsableTable
	Cols   []app.TableColumn
	Rows   *app.TableRows
	Search string

	Page    int
	PerPage int
	HasPrev bool
	HasNext bool
}

// Rows serves GET {admin}/data/{table}.
func (h *DataHandler) Rows(w http.ResponseWriter, r *http.Request) {
	s := middleware.AdminFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/login", http.StatusSeeOther)
		return
	}
	tableName := r.PathValue("table")
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	page := atoiOr(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	perPage := 50
	offset := (page - 1) * perPage

	t, cols, rows, err := h.Service.Browse(r.Context(), s.Admin.ID, tableName, search, perPage, offset)
	if err != nil {
		if errors.Is(err, app.ErrTableNotWhitelisted) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not browse table", http.StatusInternalServerError)
		return
	}

	data := tableRowsPageData{
		Title:      t.DisplayName,
		CSRFToken:  middleware.CSRFTokenFrom(r.Context()),
		Path:       h.Cfg.Admin.Path,
		Table:      t,
		Cols:       cols,
		Rows:       rows,
		Search:     search,
		Page:       page,
		PerPage:    perPage,
		HasPrev:    page > 1,
		HasNext:    offset+len(rows.Rows) < rows.Total,
		AdminName:  s.Admin.Name,
		AdminEmail: s.Admin.Email,
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/data_rows.html", data)
}
