package measurement

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Gender scopes a measurement template. Unisex templates appear for both
// male and female picks on the order form.
type Gender string

const (
	GenderMale   Gender = "male"
	GenderFemale Gender = "female"
	GenderUnisex Gender = "unisex"
)

// Genders offered on the order form.
var Genders = []Gender{GenderMale, GenderFemale}

func (g Gender) IsValid() bool {
	switch g {
	case GenderMale, GenderFemale, GenderUnisex:
		return true
	}
	return false
}

func (g Gender) Label() string {
	switch g {
	case GenderMale:
		return "Male"
	case GenderFemale:
		return "Female"
	default:
		return "Unisex"
	}
}

// TemplateField is one row of a measurement form, e.g. Chest (in).
type TemplateField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Unit     string `json:"unit"`
	Required bool   `json:"required"`
}

// MaxValueLen caps a single entered measurement value.
const MaxValueLen = 40

// MaxNotesLen caps the free-text notes on an order measurement.
const MaxNotesLen = 2000

// Template is a garment measurement form: system-wide (OrganizationID nil)
// or customized by one business.
type Template struct {
	ID             uuid.UUID
	OrganizationID *uuid.UUID
	Gender         Gender
	Garment        string
	Name           string
	Fields         []TemplateField
	IsSystem       bool
}

// ParseFields decodes and validates the fields JSON stored on a template.
func ParseFields(raw []byte) ([]TemplateField, error) {
	var fields []TemplateField
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFields, err)
	}
	if len(fields) == 0 {
		return nil, ErrInvalidFields
	}
	seen := map[string]bool{}
	for _, f := range fields {
		key := strings.TrimSpace(f.Key)
		if key == "" {
			return nil, ErrFieldKeyRequired
		}
		if strings.TrimSpace(f.Label) == "" {
			return nil, ErrFieldLabelRequired
		}
		if seen[key] {
			return nil, fmt.Errorf("%w: duplicate %q", ErrInvalidFields, key)
		}
		seen[key] = true
	}
	return fields, nil
}

// MaxTemplateFields caps the field editor so forms stay usable.
const MaxTemplateFields = 30

// NormalizeGarment lowercases and trims a garment slug.
func NormalizeGarment(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ValidateTemplate checks a tenant-owned template before persistence.
// System templates are never validated here — they are seed data.
func ValidateTemplate(name string, gender Gender, garment string, fields []TemplateField) error {
	if strings.TrimSpace(name) == "" {
		return ErrTemplateNameRequired
	}
	if len(strings.TrimSpace(name)) > 120 {
		return fmt.Errorf("%w: max 120", ErrTemplateNameRequired)
	}
	if !gender.IsValid() {
		return ErrInvalidGender
	}
	g := NormalizeGarment(garment)
	if g == "" || len(g) > 60 {
		return ErrInvalidGarment
	}
	if len(fields) == 0 {
		return ErrInvalidFields
	}
	if len(fields) > MaxTemplateFields {
		return ErrTooManyFields
	}
	seen := map[string]bool{}
	for _, f := range fields {
		key := strings.TrimSpace(f.Key)
		if key == "" {
			return ErrFieldKeyRequired
		}
		if !validFieldKey(key) {
			return fmt.Errorf("%w: %q", ErrFieldKeyInvalid, key)
		}
		if strings.TrimSpace(f.Label) == "" {
			return ErrFieldLabelRequired
		}
		if len(f.Label) > 80 {
			return fmt.Errorf("%w: label too long %q", ErrFieldLabelRequired, key)
		}
		if len(f.Unit) > 10 {
			return fmt.Errorf("%w: unit too long %q", ErrInvalidFields, key)
		}
		if seen[key] {
			return fmt.Errorf("%w: duplicate %q", ErrInvalidFields, key)
		}
		seen[key] = true
	}
	return nil
}

func validFieldKey(key string) bool {
	if len(key) > 40 {
		return false
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// NormalizeFieldKey converts a free-form label into a storage key.
func NormalizeFieldKey(label string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '/':
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	if out == "" {
		out = "field"
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

// ValidateValues checks entered values against the template: required fields
// must be non-empty, every value length-capped. Unknown keys are rejected so
// typos and forged fields fail loudly instead of silently storing junk.
func (t Template) ValidateValues(values map[string]string) error {
	allowed := map[string]bool{}
	for _, f := range t.Fields {
		allowed[strings.TrimSpace(f.Key)] = true
		if f.Required && strings.TrimSpace(values[f.Key]) == "" {
			return fmt.Errorf("%w: %s", ErrValueRequired, f.Label)
		}
	}
	for k, v := range values {
		if !allowed[strings.TrimSpace(k)] {
			return fmt.Errorf("%w: unknown field %q", ErrInvalidFields, k)
		}
		if len(v) > MaxValueLen {
			return fmt.Errorf("%w: %q", ErrValueTooLong, k)
		}
	}
	return nil
}

// Measurement captures the values taken for one order, plus a snapshot of
// the template identity so old orders still read sensibly if a template is
// edited or removed later. TemplateID is uuid.Nil when the template was
// deleted after the order was taken — the snapshot still renders.
// SnapshotFields holds the field definitions at order time (labels/units).
type Measurement struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	OrderID        uuid.UUID
	TemplateID     uuid.UUID
	Gender         Gender
	Garment        string
	TemplateName   string
	Values         map[string]string
	Notes          string
	SnapshotFields []TemplateField
	CreatedBy      uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Validate checks a measurement before persistence.
func (m Measurement) Validate(t Template) error {
	if !m.Gender.IsValid() {
		return ErrInvalidGender
	}
	if strings.TrimSpace(m.Garment) == "" {
		return ErrInvalidGarment
	}
	if len(m.Notes) > MaxNotesLen {
		return ErrNotesTooLong
	}
	return t.ValidateValues(m.Values)
}
