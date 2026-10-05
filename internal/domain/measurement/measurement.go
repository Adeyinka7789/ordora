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
// edited or removed later.
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
