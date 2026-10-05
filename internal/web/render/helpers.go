package render

import (
	"fmt"
	"html/template"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/contact"
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
		// currency formats minor units as "CODE 1,234,567.89".
		// Thousands grouping lives here and only here: every money
		// display in every template flows through this function.
		"currency": func(minor int64, code string) string {
			sign := ""
			if minor < 0 {
				sign = "-"
				minor = -minor
			}
			return fmt.Sprintf("%s %s%s.%02d", code, sign, groupThousands(minor/100), minor%100)
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
		// sub subtracts two integers of any int kind. Templates require
		// exact type matches for func args, and call sites mix int (page
		// numbers, percentages) with int64 (money minor units), so a fixed
		// (int, int) signature 500s on int64 callers (admin/ops.html).
		"sub": func(a, b any) int64 { return toInt64(a) - toInt64(b) },

		// contact returns the editable public contact details for the
		// utility top bar and floating buttons. Usage: {{ with contact }}.
		// Nil Contact renders nothing (safe in tests without wiring).
		"contact": func() *contact.Info { return Contact },
	}
}

// toInt64 coerces any integer value for template math.
func toInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int8:
		return int64(n)
	case int16:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case uint:
		return int64(n)
	case uint32:
		return int64(n)
	case uint64:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

// groupThousands inserts comma separators: 1234567 -> "1,234,567".
func groupThousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	// Build from the right in chunks of three.
	out := make([]byte, 0, len(s)+len(s)/3)
	first := len(s) % 3
	if first == 0 {
		first = 3
	}
	out = append(out, s[:first]...)
	for i := first; i < len(s); i += 3 {
		out = append(out, ',')
		out = append(out, s[i:i+3]...)
	}
	return string(out)
}
