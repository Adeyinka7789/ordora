package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/measurement"
	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// TailoringCategory is the signup business_category that unlocks measurement
// features on order forms (canonical value lives on org.TailoringCategory).
const TailoringCategory = org.TailoringCategory

func isTailoringOrg(o *org.Organization) bool {
	return o != nil && org.IsTailoringCategory(o.BusinessCategory)
}

// measTemplateOption is one garment choice on the order form.
type measTemplateOption struct {
	ID      string
	Gender  string
	Garment string
	Name    string
}

// measFieldView is one rendered measurement input (or show row).
type measFieldView struct {
	Key      string
	Label    string
	Unit     string
	Required bool
	Value    string
}

// measShowView is the read-only measurement block on the order page.
type measShowView struct {
	GenderLabel  string
	Garment      string
	TemplateName string
	Notes        string
	Rows         []measFieldView
	Extras       []measFieldView
}

// measTemplateList returns all visible templates (male + female genders,
// tenant-owned first). Unisex rows match both gender queries, so results are
// de-duplicated by id. Used to build the garment picker.
func (h *OrderHandler) measTemplateList(r *http.Request, scope tenant.TenantScope) []measurement.Template {
	if h.Measurements == nil {
		return nil
	}
	var out []measurement.Template
	seen := map[string]bool{}
	for _, g := range measurement.Genders {
		tmpls, err := h.Measurements.ListTemplates(r.Context(), scope, g)
		if err != nil {
			continue
		}
		for _, t := range tmpls {
			key := t.ID.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, t)
		}
	}
	return out
}

// measOptions converts templates to garment-picker options.
func measOptions(list []measurement.Template) []measTemplateOption {
	out := make([]measTemplateOption, 0, len(list))
	for _, t := range list {
		out = append(out, measTemplateOption{
			ID:      t.ID.String(),
			Gender:  string(t.Gender),
			Garment: t.Garment,
			Name:    t.Name,
		})
	}
	return out
}

// measTemplateByID finds a template in an already-loaded list.
func measTemplateByID(list []measurement.Template, id uuid.UUID) (measurement.Template, bool) {
	for _, t := range list {
		if t.ID == id {
			return t, true
		}
	}
	return measurement.Template{}, false
}

// firstTemplateOfGender returns the first template for a gender (tenant rows
// sort before system rows), falling back to the list head.
func firstTemplateOfGender(list []measurement.Template, gender string) (measurement.Template, bool) {
	for _, t := range list {
		if string(t.Gender) == gender || t.Gender == measurement.GenderUnisex {
			return t, true
		}
	}
	if len(list) > 0 {
		return list[0], true
	}
	return measurement.Template{}, false
}

// measFieldsFor builds field views for a template, prefilled from values
// (used for initial render and error re-renders).
func measFieldsFor(tmpl measurement.Template, values map[string]string) []measFieldView {
	fields := make([]measFieldView, 0, len(tmpl.Fields))
	for _, f := range tmpl.Fields {
		fields = append(fields, measFieldView{
			Key:      f.Key,
			Label:    f.Label,
			Unit:     f.Unit,
			Required: f.Required,
			Value:    values[f.Key],
		})
	}
	return fields
}

// parseMeasurementInput reads meas_template_id + measure[key] + meas_notes.
// Empty template id means "no measurements" (nil, nil).
func parseMeasurementInput(r *http.Request) (*app.CreateMeasurementInput, string, map[string]string, string) {
	tmplID := strings.TrimSpace(r.PostFormValue("meas_template_id"))
	gender := strings.TrimSpace(r.PostFormValue("meas_gender"))
	notes := strings.TrimSpace(r.PostFormValue("meas_notes"))
	values := map[string]string{}
	for key, vals := range r.PostForm {
		name := strings.TrimSpace(key)
		if !strings.HasPrefix(name, "measure[") || !strings.HasSuffix(name, "]") || len(vals) == 0 {
			continue
		}
		k := strings.TrimSpace(name[len("measure[") : len(name)-1])
		if k == "" {
			continue
		}
		values[k] = strings.TrimSpace(vals[0])
	}
	if tmplID == "" {
		return nil, gender, values, notes
	}
	id, err := uuid.Parse(tmplID)
	if err != nil {
		return nil, gender, values, notes
	}
	// Drop fully-empty submissions (template picked, nothing entered): the
	// service would reject required fields anyway; treat as none only when
	// every value and notes are blank so tailors aren't forced.
	any := notes != ""
	for _, v := range values {
		if v != "" {
			any = true
			break
		}
	}
	if !any {
		return nil, gender, map[string]string{}, ""
	}
	return &app.CreateMeasurementInput{TemplateID: id, Values: values, Notes: notes}, gender, values, notes
}

// measEchoForError reloads the posted template and overlays the posted
// values, so error re-renders preserve the measurement section. ok=false
// means there is nothing to preserve (no measurement posted or template
// gone); callers then leave the section at defaults.
func (h *OrderHandler) measEchoForError(r *http.Request, scope tenant.TenantScope, inMeas *app.CreateMeasurementInput) (gender, templateID string, fields []measFieldView, notes string, ok bool) {
	if h.Measurements == nil || inMeas == nil {
		return "", "", nil, "", false
	}
	tmpl, err := h.Measurements.GetTemplate(r.Context(), scope, inMeas.TemplateID)
	if err != nil {
		return "", "", nil, "", false
	}
	return string(tmpl.Gender), tmpl.ID.String(), measFieldsFor(tmpl, inMeas.Values), inMeas.Notes, true
}

// MeasurementFields handles GET /measurements/fields?template_id= — the
// HTMX fragment that swaps the measurement inputs when the garment changes.
func (h *OrderHandler) MeasurementFields(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(queryValue(r, "template_id"))
	if err != nil {
		// The order form posts the same select as meas_template_id.
		id, err = uuid.Parse(queryValue(r, "meas_template_id"))
	}
	if err != nil {
		http.Error(w, "bad template", http.StatusBadRequest)
		return
	}
	tmpl, err := h.Measurements.GetTemplate(r.Context(), scope, id)
	if err != nil {
		if errors.Is(err, measurement.ErrTemplateNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load template", http.StatusInternalServerError)
		return
	}
	h.Renderer.Fragment(w, r, http.StatusOK, "measurements/_fields.html", map[string]any{
		"Fields": measFieldsFor(tmpl, nil),
	})
}

func humanizeMeasurementError(err error) string {
	switch {
	case errors.Is(err, measurement.ErrTemplateNotFound):
		return "That measurement template no longer exists. Please pick another."
	case errors.Is(err, measurement.ErrValueRequired):
		return "Please fill in all required measurements."
	case errors.Is(err, measurement.ErrValueTooLong):
		return "A measurement value is too long."
	case errors.Is(err, measurement.ErrNotesTooLong):
		return "Measurement notes are too long (max 2000)."
	default:
		return ""
	}
}
