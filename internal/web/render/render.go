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

// templateFuncs defines custom functions available inside HTML templates.
func templateFuncs() template.FuncMap {
	return template.FuncMap{}
}

// Renderer loads templates once at startup and renders either full pages
// (wrapped in a layout) or fragments (just the requested template).
//
// Fragment detection is based on the HX-Request header: if present, render
// the fragment only. Otherwise render the fragment embedded in the layout.
type Renderer struct {
	tmpl      *template.Template
	layoutDir string
}

// New loads all .html files under templatesDir. Layouts live in templatesDir/layouts.
// Files ending in .html are parsed as templates; each must use {{ define "name" }}.
func New(templatesDir string) (*Renderer, error) {
	tmpl := template.New("").Funcs(funcMap())

	err := filepath.WalkDir(templatesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		rel, _ := filepath.Rel(templatesDir, path)
		// Normalize Windows backslashes to forward slashes for template names.
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

	return &Renderer{
		tmpl:      tmpl,
		layoutDir: filepath.Join(templatesDir, "layouts"),
	}, nil
}

// Page renders a full page: it looks up the fragment, executes it into a
// buffer, and then executes the layout with the fragment output as "Content".
func (r *Renderer) Page(w http.ResponseWriter, status int, layout, fragment string, data any) {
	if layout == "" {
		layout = "layouts/app.html"
	}

	var buf bytes.Buffer
	if err := r.tmpl.ExecuteTemplate(&buf, fragment, data); err != nil {
		r.serverError(w, err, fragment)
		return
	}

	// Provide Content as template.HTML so the layout does not escape it.
	view := struct {
		Data    any
		Content template.HTML
	}{
		Data:    data,
		Content: template.HTML(buf.String()), //nolint:gosec // content is our own rendered template
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := r.tmpl.ExecuteTemplate(w, layout, view); err != nil {
		slog.Error("render: layout exec", "layout", layout, "err", err)
	}
}

// Fragment renders just the fragment. This is what HTMX endpoints use.
//
// If HX-Request is absent, it falls back to a full page (progressive
// enhancement — the same URL works with or without JavaScript).
func (r *Renderer) Fragment(w http.ResponseWriter, req *http.Request, status int, fragment string, data any) {
	if req.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		if err := r.tmpl.ExecuteTemplate(w, fragment, data); err != nil {
			slog.Error("render: fragment exec", "fragment", fragment, "err", err)
		}
		return
	}
	// Non-HTMX request: wrap in the default layout.
	r.Page(w, status, "layouts/app.html", fragment, data)
}

// Raw executes a template without a layout. Useful for email templates
// rendered by the worker.
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
