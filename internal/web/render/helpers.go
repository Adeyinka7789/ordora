package render

import (
	"fmt"
	"html/template"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
)

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) }, //nolint:gosec
		"lower":    strings.ToLower,
		"upper":    strings.ToUpper,
		"title":    strings.Title, //nolint:staticcheck

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
			major := minor / 100
			frac := minor % 100
			if frac < 0 {
				frac = -frac
			}
			return fmt.Sprintf("%s %d.%02d", code, major, frac)
		},
		"formatQty": func(scaled int64) string {
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
		// flag reports whether a feature flag is on for the given org id
		// string. Usage: {{ if flag "ledger_export" .Shell.OrgID }}.
		// Unparseable org ids evaluate as the zero org (stable per flag).
		"flag": func(key, orgID string) bool {
			if Flags == nil {
				return true
			}
			id, err := uuid.Parse(orgID)
			if err != nil {
				id = uuid.Nil
			}
			return Flags.Enabled(id, key)
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
		"divf": func(minor int64, divisor float64) float64 {
			return float64(minor) / divisor
		},
		"initials": func(name string) string {
			name = strings.TrimSpace(name)
			if name == "" {
				return "?"
			}
			parts := strings.Fields(name)
			if len(parts) == 0 {
				return "?"
			}
			var b strings.Builder
			for i, p := range parts {
				if i >= 2 {
					break
				}
				r := []rune(p)
				if len(r) > 0 {
					b.WriteString(strings.ToUpper(string(r[0])))
				}
			}
			if b.Len() == 0 {
				return "?"
			}
			return b.String()
		},
		"list": func(items ...any) []any { return items },
		"statusLabel": func(status string) string {
			switch status {
			case "NEW":
				return "New"
			case "CONFIRMED":
				return "Confirmed"
			case "IN_PROGRESS":
				return "In Progress"
			case "READY":
				return "Ready"
			case "OUT_FOR_DELIVERY":
				return "In Transit"
			case "DELIVERED":
				return "Delivered"
			case "COMPLETED":
				return "Completed"
			case "CANCELLED":
				return "Cancelled"
			}
			return status
		},
		"dict": func(values ...any) map[string]any {
			if len(values)%2 != 0 {
				return nil
			}
			m := make(map[string]any, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					continue
				}
				m[key] = values[i+1]
			}
			return m
		},
		"add1": func(i int) int { return i + 1 },

		"fieldOr": func(data any, field, fallback string) string {
			if data == nil {
				return fallback
			}
			rv := reflect.ValueOf(data)
			if rv.Kind() == reflect.Ptr {
				if rv.IsNil() {
					return fallback
				}
				rv = rv.Elem()
			}
			if rv.Kind() != reflect.Struct {
				return fallback
			}
			f := rv.FieldByName(field)
			if !f.IsValid() || f.Kind() != reflect.String {
				return fallback
			}
			s := f.String()
			if s == "" {
				return fallback
			}
			return s
		},
		"inc": func(i int) int { return i + 1 },
		"dec": func(i int) int { return i - 1 },
		"sub": func(a, b int) int { return a - b },
	}
}
