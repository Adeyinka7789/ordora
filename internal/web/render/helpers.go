package render

import (
	"fmt"
	"html/template"
	"os"
	"reflect"
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
		"formatQty": func(scaled int64) string {
			// Quantity is scaled by 1000. Format with up to 3 decimals,
			// trimming trailing zeros.
			s := fmt.Sprintf("%.3f", float64(scaled)/1000.0)
			s = strings.TrimRight(s, "0")
			s = strings.TrimRight(s, ".")
			if s == "" {
				return "0"
			}
			return s
		},
		"statusClass": func(current, step interface{}) string {
			c := fmt.Sprintf("%v", current)
			s := fmt.Sprintf("%v", step)
			order := []string{"NEW", "CONFIRMED", "IN_PROGRESS", "READY", "OUT_FOR_DELIVERY", "DELIVERED", "COMPLETED"}
			ci, si := -1, -1
			for i, v := range order {
				if v == c {
					ci = i
				}
				if v == s {
					si = i
				}
			}
			if c == "CANCELLED" {
				return "step"
			}
			switch {
			case si < ci:
				return "step done"
			case si == ci:
				return "step active"
			default:
				return "step"
			}
		},

		"csrfToken": func(data any) string {
			if data == nil {
				return ""
			}
			rv := reflect.ValueOf(data)
			if rv.Kind() == reflect.Ptr {
				if rv.IsNil() {
					return ""
				}
				rv = rv.Elem()
			}
			switch rv.Kind() {
			case reflect.Map:
				v := rv.MapIndex(reflect.ValueOf("CSRFToken"))
				if v.IsValid() {
					return fmt.Sprintf("%v", v.Interface())
				}
			case reflect.Struct:
				f := rv.FieldByName("CSRFToken")
				if f.IsValid() && f.Kind() == reflect.String {
					return f.String()
				}
			}
			return ""
		},
	}
}
