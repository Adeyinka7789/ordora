package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/measurement"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// MeasurementTemplateHandler serves /settings/measurements/* — the
// clone-and-customize manager (Model C). System templates are read-only;
// shops clone one and edit their own copy.
type MeasurementTemplateHandler struct {
	Repo     *postgres.MeasurementRepo
	Orgs     *postgres.OrgRepo
	Renderer *render.Renderer
}

type measTmplRow struct {
	ID         string
	Name       string
	Gender     string
	Garment    string
	FieldCount int
	IsSystem   bool
}

type measTmplIndexPage struct {
	Title       string
	CSRFToken   string
	IsTailoring bool
	Own         []measTmplRow
	System      []measTmplRow
	FlashNotice string
	FlashError  string
}

type measFieldForm struct {
	Key      string
	Label    string
	Unit     string
	Required bool
}

type measTmplFormPage struct {
	Title       string
	CSRFToken   string
	Error       string
	IsEdit      bool
	TemplateID  string
	FormName    string
	FormGender  string
	FormGarment string
	Fields      []measFieldForm
}

// Index lists own templates first, then system defaults.
func (h *MeasurementTemplateHandler) Index(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	all, err := h.Repo.ListAll(r.Context(), scope)
	if err != nil {
		http.Error(w, "could not load templates", http.StatusInternalServerError)
		return
	}
	page := measTmplIndexPage{Title: "Measurement templates"}
	if org, err := h.Orgs.GetByID(r.Context(), scope.OrgID); err == nil {
		page.IsTailoring = isTailoringOrg(org)
	} else {
		page.IsTailoring = true
	}
	for _, t := range all {
		row := measTmplRow{
			ID: t.ID.String(), Name: t.Name,
			Gender: string(t.Gender), Garment: t.Garment,
			FieldCount: len(t.Fields), IsSystem: t.IsSystem,
		}
		if t.IsSystem {
			page.System = append(page.System, row)
		} else {
			page.Own = append(page.Own, row)
		}
	}
	if v := queryValue(r, "notice"); v != "" {
		page.FlashNotice = v
	}
	if v := queryValue(r, "error"); v != "" {
		page.FlashError = v
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "settings/measurements/index.html", page)
}

// New renders a blank form, or a clone prefilled from ?clone=<id>.
func (h *MeasurementTemplateHandler) New(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	page := measTmplFormPage{
		Title:      "New measurement template",
		FormGender: string(measurement.GenderMale),
		Fields:     blankFieldForms(8),
	}
	if cloneID := queryValue(r, "clone"); cloneID != "" {
		if id, err := uuid.Parse(cloneID); err == nil {
			if src, err := h.Repo.GetTemplate(r.Context(), scope, id); err == nil {
				page.Title = "Clone — " + src.Name
				page.FormName = "Copy of " + src.Name
				page.FormGender = string(src.Gender)
				page.FormGarment = src.Garment
				page.Fields = templateFieldsToForms(src.Fields, 8)
			}
		}
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "settings/measurements/form.html", page)
}

// Create handles POST /settings/measurements.
func (h *MeasurementTemplateHandler) Create(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := formValue(r, "name")
	gender := measurement.Gender(formValue(r, "gender"))
	garment := measurement.NormalizeGarment(formValue(r, "garment"))
	fields := parseTemplateFieldsForm(r)
	if err := measurement.ValidateTemplate(name, gender, garment, fields); err != nil {
		h.renderFormError(w, r, measTmplFormPage{
			Title:    "New measurement template",
			FormName: name, FormGender: string(gender), FormGarment: garment,
			Fields: fieldsToFormsPreserve(fields, 8),
		}, humanizeTemplateError(err))
		return
	}
	t := measurement.Template{
		ID: uuid.New(), Gender: gender,
		Garment: garment, Name: strings.TrimSpace(name), Fields: fields,
	}
	if err := h.Repo.CreateTemplate(r.Context(), scope, t); err != nil {
		h.renderFormError(w, r, measTmplFormPage{
			Title:    "New measurement template",
			FormName: name, FormGender: string(gender), FormGarment: garment,
			Fields: fieldsToFormsPreserve(fields, 8),
		}, humanizeTemplateError(err))
		return
	}
	http.Redirect(w, r, "/settings/measurements?notice=Template+saved.", http.StatusSeeOther)
}

// Edit renders the edit form for an own template.
func (h *MeasurementTemplateHandler) Edit(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	tmpl, err := h.Repo.GetTemplate(r.Context(), scope, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if tmpl.IsSystem || tmpl.OrganizationID == nil {
		http.Redirect(w, r, "/settings/measurements/new?clone="+tmpl.ID.String(), http.StatusSeeOther)
		return
	}
	page := measTmplFormPage{
		Title: "Edit — " + tmpl.Name, IsEdit: true,
		TemplateID: tmpl.ID.String(),
		FormName:   tmpl.Name, FormGender: string(tmpl.Gender),
		FormGarment: tmpl.Garment,
		Fields:      templateFieldsToForms(tmpl.Fields, 8),
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "settings/measurements/form.html", page)
}

// Update handles POST /settings/measurements/{id}.
func (h *MeasurementTemplateHandler) Update(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	existing, err := h.Repo.GetTemplate(r.Context(), scope, id)
	if err != nil || existing.IsSystem {
		http.NotFound(w, r)
		return
	}
	name := formValue(r, "name")
	gender := measurement.Gender(formValue(r, "gender"))
	garment := measurement.NormalizeGarment(formValue(r, "garment"))
	fields := parseTemplateFieldsForm(r)
	if err := measurement.ValidateTemplate(name, gender, garment, fields); err != nil {
		h.renderFormError(w, r, measTmplFormPage{
			Title: "Edit template", IsEdit: true, TemplateID: id.String(),
			FormName: name, FormGender: string(gender), FormGarment: garment,
			Fields: fieldsToFormsPreserve(fields, 8),
		}, humanizeTemplateError(err))
		return
	}
	existing.Name = strings.TrimSpace(name)
	existing.Gender = gender
	existing.Garment = garment
	existing.Fields = fields
	if err := h.Repo.UpdateTemplate(r.Context(), scope, existing); err != nil {
		h.renderFormError(w, r, measTmplFormPage{
			Title: "Edit template", IsEdit: true, TemplateID: id.String(),
			FormName: name, FormGender: string(gender), FormGarment: garment,
			Fields: fieldsToFormsPreserve(fields, 8),
		}, humanizeTemplateError(err))
		return
	}
	http.Redirect(w, r, "/settings/measurements?notice=Template+updated.+Past+orders+keep+their+original+measurements.", http.StatusSeeOther)
}

// Delete handles POST /settings/measurements/{id}/delete.
func (h *MeasurementTemplateHandler) Delete(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := h.Repo.DeleteTemplate(r.Context(), scope, id); err != nil {
		msg := "Could not delete template."
		if errors.Is(err, measurement.ErrTemplateNotFound) {
			msg = "Template not found."
		}
		http.Redirect(w, r, "/settings/measurements?error="+msg, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/measurements?notice=Template+deleted.+Past+orders+keep+their+measurements.", http.StatusSeeOther)
}

func (h *MeasurementTemplateHandler) renderFormError(w http.ResponseWriter, r *http.Request, page measTmplFormPage, msg string) {
	page.CSRFToken = csrfFromCtx(r)
	page.Error = msg
	renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "settings/measurements/form.html", page)
}

// parseTemplateFieldsForm reads fields[0][key] / [label] / [unit] / [required]
// rows. Blank rows are skipped; a label without a key gets an auto key.
func parseTemplateFieldsForm(r *http.Request) []measurement.TemplateField {
	var out []measurement.TemplateField
	for i := 0; i < measurement.MaxTemplateFields; i++ {
		p := fmt.Sprintf("fields[%d]", i)
		key := strings.ToLower(strings.TrimSpace(r.PostFormValue(p + "[key]")))
		label := strings.TrimSpace(r.PostFormValue(p + "[label]"))
		unit := strings.TrimSpace(r.PostFormValue(p + "[unit]"))
		reqRaw := strings.TrimSpace(r.PostFormValue(p + "[required]"))
		required := reqRaw == "on" || reqRaw == "1" || reqRaw == "true"
		if key == "" && label == "" {
			continue
		}
		if key == "" {
			key = measurement.NormalizeFieldKey(label)
		}
		if unit == "" {
			unit = "in"
		}
		out = append(out, measurement.TemplateField{
			Key: key, Label: label, Unit: unit, Required: required,
		})
	}
	return out
}

func blankFieldForms(n int) []measFieldForm {
	out := make([]measFieldForm, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, measFieldForm{Unit: "in"})
	}
	return out
}

func templateFieldsToForms(fields []measurement.TemplateField, minRows int) []measFieldForm {
	out := make([]measFieldForm, 0, len(fields)+minRows+3)
	for _, f := range fields {
		out = append(out, measFieldForm{Key: f.Key, Label: f.Label, Unit: f.Unit, Required: f.Required})
	}
	for len(out) < minRows {
		out = append(out, measFieldForm{Unit: "in"})
	}
	// Always offer blank rows so shops can add fields without a second pass.
	for i := 0; i < 3; i++ {
		out = append(out, measFieldForm{Unit: "in"})
	}
	return out
}

func fieldsToFormsPreserve(fields []measurement.TemplateField, minRows int) []measFieldForm {
	return templateFieldsToForms(fields, minRows)
}

func humanizeTemplateError(err error) string {
	switch {
	case errors.Is(err, measurement.ErrTemplateNameRequired):
		return "Please give the template a name."
	case errors.Is(err, measurement.ErrInvalidGender):
		return "Gender must be male, female or unisex."
	case errors.Is(err, measurement.ErrInvalidGarment):
		return "Garment is required (e.g. senator, kaftan, dress)."
	case errors.Is(err, measurement.ErrInvalidFields):
		return "Add at least one measurement field."
	case errors.Is(err, measurement.ErrFieldKeyRequired):
		return "Every field needs a key (or a label to generate one)."
	case errors.Is(err, measurement.ErrFieldLabelRequired):
		return "Every field needs a label."
	case errors.Is(err, measurement.ErrFieldKeyInvalid):
		return "Field keys must be lowercase letters, numbers or underscore."
	case errors.Is(err, measurement.ErrTooManyFields):
		return "Too many fields (max 30)."
	case errors.Is(err, measurement.ErrTemplateNotEditable):
		return "System templates cannot be edited — clone it first."
	case errors.Is(err, measurement.ErrTemplateNotFound):
		return "That template no longer exists."
	default:
		if strings.Contains(err.Error(), "duplicate") {
			return "Two fields share the same key. Keys must be unique."
		}
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate key") {
			return "You already have a template with that name."
		}
		return "Could not save the template. Please check the fields."
	}
}
