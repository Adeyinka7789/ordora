package render

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	texttemplate "text/template"

	"github.com/Adeyinka7789/ordora/internal/web/middleware"
)

// Renderer loads templates once at startup and renders either full pages
// (wrapped in a layout) or fragments (just the requested template).
type Renderer struct {
	tmpl      *template.Template
	layoutDir string
}

// SupportEmail is the help/complaints address shown in the app footer.
// Set once at startup from config (main.go). Empty means "don't display".
var SupportEmail string

// Shell carries values needed by the application layout.
type Shell struct {
	UserName     string
	UserEmail    string
	SupportEmail string
	OrgName      string
	OrgSlug     string
	OrgRole     string
	OrgCurrency string
	OrgTimezone string
	CSRFToken   string

	// Impersonation
	Impersonating        bool
	ImpersonatingOrgID   string
	ImpersonatingOrgName string
}

// New loads all .html files under templatesDir.
func New(templatesDir string) (*Renderer, error) {
	tmpl := template.New("").Funcs(templateFuncs())
	err := filepath.WalkDir(templatesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		rel, _ := filepath.Rel(templatesDir, path)
		name := strings.ReplaceAll(rel, "\\", "/")
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("render: read %s: %w", path, err)
		}
		if _, err := tmpl.New(name).Parse(string(content)); err != nil {
			return fmt.Errorf("render: parse %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Renderer{tmpl: tmpl, layoutDir: filepath.Join(templatesDir, "layouts")}, nil
}

// Page renders a full page without shell data.
func (r *Renderer) Page(w http.ResponseWriter, status int, layout, fragment string, data any) {
	r.PageWithShell(w, status, layout, fragment, data, Shell{})
}

// PageWithShell renders a full page with layout shell data.
func (r *Renderer) PageWithShell(w http.ResponseWriter, status int, layout, fragment string, data any, shell Shell) {
	if layout == "" {
		layout = "layouts/app.html"
	}
	var buf bytes.Buffer
	if err := r.tmpl.ExecuteTemplate(&buf, fragment, data); err != nil {
		r.serverError(w, err, fragment)
		return
	}
	view := struct {
		Data    any
		Content template.HTML
		Shell   Shell
	}{Data: data, Content: template.HTML(buf.String()), Shell: shell} //nolint:gosec
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := r.tmpl.ExecuteTemplate(w, layout, view); err != nil {
		slog.Error("render: layout exec", "layout", layout, "err", err)
	}
}

// PagePublic renders a full page with a public (non-authenticated) layout.
// No sidebar, no topbar, no shell.
func (r *Renderer) PagePublic(w http.ResponseWriter, status int, layout, fragment string, data any) {
	if layout == "" {
		layout = "layouts/public.html"
	}
	var buf bytes.Buffer
	if err := r.tmpl.ExecuteTemplate(&buf, fragment, data); err != nil {
		r.serverError(w, err, fragment)
		return
	}
	view := struct {
		Data    any
		Content template.HTML
	}{
		Data:    data,
		Content: template.HTML(buf.String()), //nolint:gosec
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := r.tmpl.ExecuteTemplate(w, layout, view); err != nil {
		slog.Error("render: public layout exec", "layout", layout, "err", err)
	}
}

// Fragment renders just the fragment for HTMX requests, or a full page otherwise.
func (r *Renderer) Fragment(w http.ResponseWriter, req *http.Request, status int, fragment string, data any) {
	if req.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		if err := r.tmpl.ExecuteTemplate(w, fragment, data); err != nil {
			slog.Error("render: fragment exec", "fragment", fragment, "err", err)
		}
		return
	}
	r.Page(w, status, "layouts/app.html", fragment, data)
}

// Raw executes a template without a layout.
func (r *Renderer) Raw(name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := r.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// NotFound renders the 404 page with the public layout. Used by error
// middleware and by handlers that call http.NotFound indirectly.
func (r *Renderer) NotFound(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	// Try the app layout first. If it fails (e.g. session missing), fall
	// back to the public layout.
	var buf bytes.Buffer
	if err := r.tmpl.ExecuteTemplate(&buf, "errors/404.html", nil); err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	// Wrap in the app layout if the session is available, otherwise the
	// public layout.
	layout := "layouts/public.html"
	if session := middleware.SessionFromContext(req.Context()); session != nil {
		layout = "layouts/app.html"
	}
	view := struct {
		Data    any
		Content template.HTML
		Shell   Shell
	}{
		Data:    nil,
		Content: template.HTML(buf.String()), //nolint:gosec
		Shell:   Shell{CSRFToken: middleware.CSRFTokenFrom(req.Context())},
	}
	if err := r.tmpl.ExecuteTemplate(w, layout, view); err != nil {
		slog.Error("render: 404 layout", "err", err)
	}
}

// ServerError renders the 500 page.
func (r *Renderer) ServerError(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)

	var buf bytes.Buffer
	if err := r.tmpl.ExecuteTemplate(&buf, "errors/500.html", map[string]any{
		"RequestID": middleware.RequestIDFrom(req.Context()),
	}); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	layout := "layouts/public.html"
	if session := middleware.SessionFromContext(req.Context()); session != nil {
		layout = "layouts/app.html"
	}
	view := struct {
		Data    any
		Content template.HTML
		Shell   Shell
	}{
		Data:    nil,
		Content: template.HTML(buf.String()), //nolint:gosec
		Shell:   Shell{CSRFToken: middleware.CSRFTokenFrom(req.Context())},
	}
	if err := r.tmpl.ExecuteTemplate(w, layout, view); err != nil {
		slog.Error("render: 500 layout", "err", err)
	}
}

func (r *Renderer) serverError(w http.ResponseWriter, err error, name string) {
	slog.Error("render: template error", "template", name, "err", err)
	var execErr *texttemplate.ExecError
	if errors.As(err, &execErr) {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// RenderFragment writes a fragment template directly with no layout.
// Used for full-page templates like /offline that don't need a shell.
func (r *Renderer) RenderFragment(w http.ResponseWriter, status int, fragment string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := r.tmpl.ExecuteTemplate(w, fragment, data); err != nil {
		slog.Error("render: fragment exec", "fragment", fragment, "err", err)
	}
}
