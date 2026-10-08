package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/attachment"
	"github.com/Adeyinka7789/ordora/internal/domain/product"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// ProductHandler serves /products/*.
type ProductHandler struct {
	Service     *app.ProductService
	Repo        *postgres.ProductRepo
	Attachments *app.AttachmentService
	Renderer    *render.Renderer
}

// Page data.

type productsIndexPage struct {
	Title       string
	CSRFToken   string
	Query       string
	IncludeArch bool
	Products    []*product.Product
	Pagination  Pagination
	FlashNotice string
	FlashError  string
	// UndoID carries an archived product id so the notice can offer Undo.
	UndoID string
}

type productFormPage struct {
	Title       string
	CSRFToken   string
	Product     *product.Product
	Error       string
	FieldErrors map[string]string
	IsEdit      bool
	FormName    string
	FormDesc    string
	FormSKU     string
	FormPrice   string
	Catalog     product.CatalogDetails
	// QuestionRows precomputes the question editor rows: existing
	// questions followed by blank rows (indices match qkeyN inputs).
	QuestionRows []productQuestionRow
}

type productQuestionRow struct {
	Index    int
	Key      string
	Label    string
	Required bool
}

// questionRows builds editor rows: existing questions then blank rows
// (3 blanks, 10 rows max).
func questionRows(existing []product.ProductQuestion) []productQuestionRow {
	if len(existing) > product.MaxProductQuestions {
		existing = existing[:product.MaxProductQuestions]
	}
	out := make([]productQuestionRow, 0, 10)
	for i, q := range existing {
		out = append(out, productQuestionRow{Index: i, Key: q.Key, Label: q.Label, Required: q.Required})
	}
	for i := len(out); i < product.MaxProductQuestions && i < len(existing)+3; i++ {
		out = append(out, productQuestionRow{Index: i})
	}
	return out
}

// parseCatalogForm reads merchandising fields from a parsed product form.
func parseCatalogForm(r *http.Request) product.CatalogDetails {
	days := 0
	if v := strings.TrimSpace(r.PostFormValue("production_days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	return product.CatalogDetails{
		Material:         formValue(r, "material"),
		Color:            formValue(r, "color"),
		ShortDescription: formValue(r, "short_description"),
		InternalNotes:    formValue(r, "internal_notes"),
		Specs:            formValue(r, "specs"),
		ProductionDays:   days,
		QuoteOnly:        r.PostFormValue("quote_only") == "on",
		StartingFrom:     r.PostFormValue("starting_from") == "on",
		Hidden:           r.PostFormValue("hidden") == "on",
		Availability:     formValue(r, "availability"),
		Category:         formValue(r, "category"),
		Questions:        parseProductQuestionsForm(r),
	}
}

// echoCatalogForm copies raw catalog form values onto p so an error
// re-render echoes what the user typed (no validation — display only).
func echoCatalogForm(p *product.Product, r *http.Request) {
	if p == nil {
		return
	}
	p.Material = formValue(r, "material")
	p.Color = formValue(r, "color")
	p.ShortDescription = formValue(r, "short_description")
	p.InternalNotes = formValue(r, "internal_notes")
	p.Specs = formValue(r, "specs")
	if v := formValue(r, "production_days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			p.ProductionDays = n
		}
	}
	p.QuoteOnly = r.PostFormValue("quote_only") == "on"
	p.StartingFrom = r.PostFormValue("starting_from") == "on"
	p.Hidden = r.PostFormValue("hidden") == "on"
	if v := formValue(r, "availability"); v != "" {
		p.Availability = v
	}
	p.Category = formValue(r, "category")
	p.Questions = parseProductQuestionsForm(r)
}

// parseProductQuestionsForm reads up to 10 qkeyN/qlabelN/qreqN rows.
func parseProductQuestionsForm(r *http.Request) []product.ProductQuestion {
	var out []product.ProductQuestion
	for i := 0; i < product.MaxProductQuestions; i++ {
		p := strconv.Itoa(i)
		label := strings.TrimSpace(r.PostFormValue("qlabel" + p))
		if label == "" {
			continue
		}
		out = append(out, product.ProductQuestion{
			Key:      strings.TrimSpace(r.PostFormValue("qkey" + p)),
			Label:    label,
			Required: r.PostFormValue("qreq"+p) == "on",
		})
	}
	return out
}

type productShowPage struct {
	Title     string
	CSRFToken string
	Product   *product.Product
	Images    []*attachment.Attachment
}

// Index.

func (h *ProductHandler) Index(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	query := queryValue(r, "q")
	includeArch := queryValue(r, "archived") == "1"
	offset := queryInt(r, "offset", 0)

	res, err := h.Repo.List(r.Context(), scope, postgres.ProductListOptions{
		Query:           query,
		IncludeArchived: includeArch,
		Limit:           50,
		Offset:          offset,
	})
	if err != nil {
		h.renderIndex(w, r, productsIndexPage{
			Title:      "Products",
			FlashError: "Could not load products.",
		})
		return
	}

	page := productsIndexPage{
		Title:       "Products",
		Query:       query,
		IncludeArch: includeArch,
		Products:    res.Products,
		Pagination:  newPagination(res.Total, res.Limit, res.Offset),
	}
	if v := queryValue(r, "notice"); v != "" {
		page.FlashNotice = v
	}
	if v := queryValue(r, "error"); v != "" {
		page.FlashError = v
	}
	if v := queryValue(r, "undo"); v != "" {
		if _, err := uuid.Parse(strings.TrimSpace(v)); err == nil {
			page.UndoID = strings.TrimSpace(v)
		}
	}

	if isHTMX(r) {
		page.CSRFToken = csrfFromCtx(r)
		h.Renderer.Fragment(w, r, http.StatusOK, "products/_table.html", page)
		return
	}
	h.renderIndex(w, r, page)
}

func (h *ProductHandler) renderIndex(w http.ResponseWriter, r *http.Request, page productsIndexPage) {
	page.CSRFToken = csrfFromCtx(r)
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "products/index.html", page)
}

// New.

func (h *ProductHandler) New(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireWriteScope(w, r); !ok {
		return
	}
	page := productFormPage{
		Title:        "New product",
		CSRFToken:    csrfFromCtx(r),
		QuestionRows: questionRows(nil),
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "products/new.html", page)
}

func (h *ProductHandler) Create(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	price, err := parseMoneyMinor(formValue(r, "unit_price"))
	if err != nil {
		msg := "Please enter a valid price."
		cat := parseCatalogForm(r)
		page := productFormPage{
			Title:       "New product",
			CSRFToken:   csrfFromCtx(r),
			Error:       msg,
			FieldErrors: map[string]string{"unit_price": msg},
			FormName:    formValue(r, "name"),
			FormDesc:    formValue(r, "description"),
			FormSKU:     formValue(r, "sku"),
			FormPrice:   formValue(r, "unit_price"),
			Catalog:     cat,
		}
		page.QuestionRows = questionRows(cat.Questions)
		renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "products/new.html", page)
		return
	}

	_, err = h.Service.Create(r.Context(), scope, app.CreateProductInput{
		Name:           formValue(r, "name"),
		Description:    formValue(r, "description"),
		SKU:            formValue(r, "sku"),
		UnitPriceMinor: price,
		Currency:       currencyFromRequest(r),
		Catalog:        parseCatalogForm(r),
	})

	if err != nil {
		msg := humanizeProductError(err)
		fields := map[string]string{}
		if f := fieldForProductError(err); f != "" {
			fields[f] = msg
		}
		page := productFormPage{
			Title:       "New product",
			CSRFToken:   csrfFromCtx(r),
			Error:       msg,
			FieldErrors: fields,
			FormName:    formValue(r, "name"),
			FormDesc:    formValue(r, "description"),
			FormSKU:     formValue(r, "sku"),
			FormPrice:   formValue(r, "unit_price"),
			Catalog:     parseCatalogForm(r),
		}
		page.QuestionRows = questionRows(page.Catalog.Questions)
		renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "products/new.html", page)
		return
	}

	http.Redirect(w, r, "/products?notice=Product+created.", http.StatusSeeOther)
}

// Show / Edit / Update / Archive.

func (h *ProductHandler) Show(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	p, err := h.Service.Get(r.Context(), scope, id)
	if err != nil {
		if errors.Is(err, product.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load product", http.StatusInternalServerError)
		return
	}
	page := productShowPage{
		Title:     p.Name,
		CSRFToken: csrfFromCtx(r),
		Product:   p,
	}
	if h.Attachments != nil {
		if images, err := h.Attachments.List(r.Context(), scope, attachment.EntityProduct, id); err == nil {
			page.Images = images
		}
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "products/show.html", page)
}

func (h *ProductHandler) Edit(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	p, err := h.Service.Get(r.Context(), scope, id)
	if err != nil {
		if errors.Is(err, product.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load product", http.StatusInternalServerError)
		return
	}
	page := productFormPage{
		Title:     "Edit " + p.Name,
		CSRFToken: csrfFromCtx(r),
		Product:   p,
		IsEdit:    true,
		FormName:  p.Name,
		FormDesc:  p.Description,
		FormSKU:   p.SKU,
		FormPrice: formatMoneyMinor(p.UnitPrice.Amount()),
	}
	page.QuestionRows = questionRows(p.Questions)
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "products/edit.html", page)
}

func (h *ProductHandler) Update(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
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

	price, err := parseMoneyMinor(formValue(r, "unit_price"))
	if err != nil {
		msg := "Please enter a valid price."
		p, _ := h.Service.Get(r.Context(), scope, id)
		echoCatalogForm(p, r)
		page := productFormPage{
			Title:       "Edit product",
			CSRFToken:   csrfFromCtx(r),
			Product:     p,
			IsEdit:      true,
			Error:       msg,
			FieldErrors: map[string]string{"unit_price": msg},
			FormName:    formValue(r, "name"),
			FormDesc:    formValue(r, "description"),
			FormSKU:     formValue(r, "sku"),
			FormPrice:   formValue(r, "unit_price"),
		}
		if p != nil {
			page.QuestionRows = questionRows(p.Questions)
		} else {
			page.QuestionRows = questionRows(nil)
		}
		renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "products/edit.html", page)
		return
	}

	_, err = h.Service.Update(r.Context(), scope, id, app.UpdateProductInput{
		Name:           formValue(r, "name"),
		Description:    formValue(r, "description"),
		SKU:            formValue(r, "sku"),
		UnitPriceMinor: price,
		Catalog:        parseCatalogForm(r),
	})
	if err != nil {
		if errors.Is(err, product.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		msg := humanizeProductError(err)
		fields := map[string]string{}
		if f := fieldForProductError(err); f != "" {
			fields[f] = msg
		}
		p, _ := h.Service.Get(r.Context(), scope, id)
		echoCatalogForm(p, r)
		page := productFormPage{
			Title:       "Edit product",
			CSRFToken:   csrfFromCtx(r),
			Product:     p,
			IsEdit:      true,
			Error:       msg,
			FieldErrors: fields,
			FormName:    formValue(r, "name"),
			FormDesc:    formValue(r, "description"),
			FormSKU:     formValue(r, "sku"),
			FormPrice:   formValue(r, "unit_price"),
		}
		if p != nil {
			page.QuestionRows = questionRows(p.Questions)
		} else {
			page.QuestionRows = questionRows(nil)
		}
		renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "products/edit.html", page)
		return
	}
	http.Redirect(w, r, "/products?notice=Product+updated.", http.StatusSeeOther)
}

func (h *ProductHandler) Archive(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.Service.Archive(r.Context(), scope, id); err != nil {
		http.Redirect(w, r, "/products?error=Could+not+archive+product.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/products?notice=Product+archived.&undo="+id.String(), http.StatusSeeOther)
}

// Unarchive handles POST /products/{id}/unarchive (undo for Archive).
func (h *ProductHandler) Unarchive(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.Service.Unarchive(r.Context(), scope, id); err != nil {
		http.Redirect(w, r, "/products?error=Could+not+restore+product.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/products?notice=Product+restored.", http.StatusSeeOther)
}

// Picker: GET /products/picker
//
// Returns just the picker rows (HTMX), used by the order form's "Add from
// catalog" panel.
func (h *ProductHandler) Picker(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	query := queryValue(r, "q")
	res, err := h.Repo.List(r.Context(), scope, postgres.ProductListOptions{
		Query: query,
		Limit: 30,
	})
	if err != nil {
		http.Error(w, "could not load products", http.StatusInternalServerError)
		return
	}

	h.Renderer.Fragment(w, r, http.StatusOK, "products/_picker.html", map[string]any{
		"Products": res.Products,
		"Query":    query,
		"Covers":   h.productCovers(r.Context(), scope, res.Products),
	})
}

// productCovers maps product id → cover image id (best-effort; empty on
// any failure so the picker never breaks). Every listed product gets an
// entry ("" when no cover) so the template never prints "<no value>".
func (h *ProductHandler) productCovers(ctx context.Context, scope tenant.TenantScope, list []*product.Product) map[string]string {
	out := make(map[string]string, len(list))
	for _, p := range list {
		out[p.ID.String()] = ""
	}
	if h.Attachments == nil || len(list) == 0 {
		return out
	}
	ids := make([]uuid.UUID, 0, len(list))
	for _, p := range list {
		ids = append(ids, p.ID)
	}
	grouped, err := h.Attachments.ListForEntities(ctx, scope, attachment.EntityProduct, ids)
	if err != nil {
		return out
	}
	for pid, atts := range grouped {
		for _, a := range atts {
			if a.IsCover() {
				out[pid.String()] = a.ID.String()
				break
			}
		}
	}
	return out
}

func humanizeProductError(err error) string {
	switch {
	case errors.Is(err, app.ErrProductNameRequired):
		return "Please enter a product name."
	case errors.Is(err, app.ErrProductPriceNegative):
		return "Price cannot be negative."
	case errors.Is(err, app.ErrProductCurrencyReq):
		return "Currency is required."
	case errors.Is(err, product.ErrNameTooLong):
		return "Name is too long."
	case errors.Is(err, product.ErrDescriptionTooLong):
		return "Description is too long."
	case errors.Is(err, product.ErrSKUTooLong):
		return "SKU is too long."
	case errors.Is(err, product.ErrMaterialTooLong):
		return "Material is too long (max 120)."
	case errors.Is(err, product.ErrColorTooLong):
		return "Color info is too long (max 120)."
	case errors.Is(err, product.ErrShortDescTooLong):
		return "Short description is too long (max 500)."
	case errors.Is(err, product.ErrNotesTooLong):
		return "Internal notes are too long (max 5000)."
	case errors.Is(err, product.ErrSpecsTooLong):
		return "Specifications are too long (max 2000)."
	case errors.Is(err, product.ErrProductionDays):
		return "Production days cannot be negative."
	case errors.Is(err, product.ErrAvailabilityInvalid):
		return "Please choose a valid availability."
	case errors.Is(err, product.ErrCategoryTooLong):
		return "Category is too long (max 80)."
	case errors.Is(err, product.ErrQuestionLabel):
		return "Each question needs a label (max 120)."
	case errors.Is(err, product.ErrQuestionKey):
		return "Two questions share the same key. Keys must be unique."
	case errors.Is(err, product.ErrTooManyQuestions):
		return "Too many questions (max 10)."
	default:
		return "Something went wrong."
	}
}

// fieldForProductError maps a product validation error to its form field.
func fieldForProductError(err error) string {
	switch {
	case errors.Is(err, app.ErrProductNameRequired),
		errors.Is(err, product.ErrNameTooLong):
		return "name"
	case errors.Is(err, app.ErrProductPriceNegative):
		return "unit_price"
	case errors.Is(err, product.ErrDescriptionTooLong):
		return "description"
	case errors.Is(err, product.ErrSKUTooLong):
		return "sku"
	default:
		return ""
	}
}
