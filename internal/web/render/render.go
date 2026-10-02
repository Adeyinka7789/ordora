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
)

// Renderer loads templates once at startup and renders either full pages
// (wrapped in a layout) or fragments (just the requested template).
type Renderer struct {
	tmpl      *template.Template
	layoutDir string
}

// Shell carries values needed by the application layout.
type Shell struct {
	UserName  string
	UserEmail string
	OrgName   string
	OrgRole   string
	CSRFToken string
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

func (r *Renderer) serverError(w http.ResponseWriter, err error, name string) {
	slog.Error("render: template error", "template", name, "err", err)
	var execErr *texttemplate.ExecError
	if errors.As(err, &execErr) {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
