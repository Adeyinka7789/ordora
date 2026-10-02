package email

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"path"
	texttemplate "text/template"
)

//go:embed templates/*.html templates/*.txt
var templateFS embed.FS

// Renderer loads and renders email templates.
//
// For each template name X, there are two files:
//
//   - templates/X.html — the HTML version
//   - templates/X.txt  — the plain-text fallback
//
// Both are executed with the same data and rendered into a Message.
type Renderer struct {
	html *htmltemplate.Template
	text *texttemplate.Template
}

// NewRenderer parses all embedded templates.
func NewRenderer() (*Renderer, error) {
	htmlTmpl := htmltemplate.New("").Option("missingkey=error")
	textTmpl := texttemplate.New("").Option("missingkey=error")

	err := fs.WalkDir(templateFS, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		content, err := templateFS.ReadFile(p)
		if err != nil {
			return err
		}
		name := path.Base(p)
		// Strip the extension so "order_created.html" and "order_created.txt"
		// both register as "order_created".
		base := name[:len(name)-len(path.Ext(name))]

		switch path.Ext(p) {
		case ".html":
			_, err = htmlTmpl.New(base).Parse(string(content))
		case ".txt":
			_, err = textTmpl.New(base).Parse(string(content))
		}
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("email: parse templates: %w", err)
	}

	return &Renderer{html: htmlTmpl, text: textTmpl}, nil
}

// Render produces a Message ready to be sent.
//
// templateName is the base name without extension, e.g. "order_created".
// The subject is passed through verbatim.
func (r *Renderer) Render(templateName, subject, to string, data any) (Message, error) {
	var htmlBuf, textBuf bytes.Buffer

	if err := r.html.ExecuteTemplate(&htmlBuf, templateName, data); err != nil {
		return Message{}, fmt.Errorf("email: render html %s: %w", templateName, err)
	}
	if err := r.text.ExecuteTemplate(&textBuf, templateName, data); err != nil {
		return Message{}, fmt.Errorf("email: render text %s: %w", templateName, err)
	}

	return Message{
		To:      to,
		Subject: subject,
		HTML:    htmlBuf.String(),
		Text:    textBuf.String(),
	}, nil
}
