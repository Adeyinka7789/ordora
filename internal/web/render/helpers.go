package render

import (
	"fmt"
	"html/template"
	"os"
	"strings"
	"time"
)

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) }, //nolint:gosec
		"lower":    strings.ToLower,
		"upper":    strings.ToUpper,
		"title":    strings.Title, //nolint:staticcheck // fine for ASCII labels
		"formatTime": func(t time.Time, layout string) string {
			if t.IsZero() {
				return ""
			}
			return t.Format(layout)
		},
		"formatDate": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format("Jan 2, 2006")
		},
		"currency": func(minor int64, code string) string {
			// Format minor units (kobo, cents) as a human string.
			// Extend when we add non-2-decimal currencies.
			major := minor / 100
			frac := minor % 100
			if frac < 0 {
				frac = -frac
			}
			return fmt.Sprintf("%s %d.%02d", code, major, frac)
		},
	}
}
