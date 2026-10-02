package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// isHTMX reports whether the current request is an HTMX request.
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// formValue returns the trimmed value of a form field.
func formValue(r *http.Request, key string) string {
	return strings.TrimSpace(r.PostFormValue(key))
}

// queryValue returns the trimmed value of a query string parameter.
func queryValue(r *http.Request, key string) string {
	return strings.TrimSpace(r.URL.Query().Get(key))
}

// queryInt returns an integer query parameter, or fallback if absent/invalid.
func queryInt(r *http.Request, key string, fallback int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// csrfFromCtx is a one-line convenience for templates.
func csrfFromCtx(r *http.Request) string {
	return middleware.CSRFTokenFrom(r.Context())
}

// Pagination describes a page of results for templates.
type Pagination struct {
	Page       int // 1-based
	PerPage    int
	Total      int
	TotalPages int
	HasPrev    bool
	HasNext    bool
	PrevOffset int
	NextOffset int
	From       int // 1-based index of first item on this page
	To         int // 1-based index of last item on this page
}

// newPagination computes pagination metadata.
func newPagination(total, limit, offset int) Pagination {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	p := Pagination{
		PerPage: limit,
		Total:   total,
	}
	p.Page = offset/limit + 1
	if total > 0 {
		p.TotalPages = (total + limit - 1) / limit
	}
	p.HasPrev = offset > 0
	p.HasNext = offset+limit < total
	if p.HasPrev {
		p.PrevOffset = offset - limit
		if p.PrevOffset < 0 {
			p.PrevOffset = 0
		}
	}
	if p.HasNext {
		p.NextOffset = offset + limit
	}
	if total > 0 {
		p.From = offset + 1
		p.To = offset + limit
		if p.To > total {
			p.To = total
		}
	}
	return p
}

// shellFromRequest builds the render.Shell from the current request's session
// and CSRF token. Used by handlers when rendering a full page.
// shellFromRequest builds the render.Shell from the current request's session
// and CSRF token. Used when rendering a full authenticated page.
func shellFromRequest(r *http.Request) render.Shell {
	s := middleware.SessionFromContext(r.Context())
	sh := render.Shell{
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
	}
	if s != nil {
		sh.UserName = s.User.Name
		sh.UserEmail = s.User.Email.String()
		sh.OrgRole = string(s.Scope.Role)
		sh.OrgName = s.OrgName
		sh.OrgSlug = s.OrgSlug
	}
	return sh
}

// page is a convenience for rendering an authenticated page with a populated
// shell. Replaces the direct h.Renderer.Page(...) calls throughout this package.
func page(w http.ResponseWriter, r *http.Request, renderer *render.Renderer, status int, layout, fragment string, data any) {
	renderer.PageWithShell(w, status, layout, fragment, data, shellFromRequest(r))
}

// renderPage is a convenience wrapper: it renders a full page with the shell
// populated from the request context. The name avoids shadowing the common
// page data variable used by handlers.
// renderPage is an alias for page. Both names exist because different handlers
// use slightly different conventions; they do the same thing.
func renderPage(w http.ResponseWriter, r *http.Request, renderer *render.Renderer, status int, layout, fragment string, data any) {
	page(w, r, renderer, status, layout, fragment, data)
}
