package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/product"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// ProductHandler serves /products/*.
type ProductHandler struct {
	Service  *app.ProductService
	Repo     *postgres.ProductRepo
	Renderer *render.Renderer
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
}

type productShowPage struct {
	Title     string
	CSRFToken string
	Product   *product.Product
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
	if _, ok := requireScope(w, r); !ok {
		return
	}
	page := productFormPage{
		Title:     "New product",
		CSRFToken: csrfFromCtx(r),
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "products/new.html", page)
}

func (h *ProductHandler) Create(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
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
		page := productFormPage{
			Title:       "New product",
			CSRFToken:   csrfFromCtx(r),
			Error:       msg,
			FieldErrors: map[string]string{"unit_price": msg},
			FormName:    formValue(r, "name"),
			FormDesc:    formValue(r, "description"),
			FormSKU:     formValue(r, "sku"),
			FormPrice:   formValue(r, "unit_price"),
		}
		renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "products/new.html", page)
		return
	}

	_, err = h.Service.Create(r.Context(), scope, app.CreateProductInput{
		Name:           formValue(r, "name"),
		Description:    formValue(r, "description"),
		SKU:            formValue(r, "sku"),
		UnitPriceMinor: price,
		Currency:       currencyFromRequest(r),
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
		}
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
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "products/show.html", page)
}

func (h *ProductHandler) Edit(w http.ResponseWriter, r *http.Request) {
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
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "products/edit.html", page)
}

func (h *ProductHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	price, err := parseMoneyMinor(formValue(r, "unit_price"))
	if err != nil {
		msg := "Please enter a valid price."
		p, _ := h.Service.Get(r.Context(), scope, id)
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
		renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "products/edit.html", page)
		return
	}

	_, err = h.Service.Update(r.Context(), scope, id, app.UpdateProductInput{
		Name:           formValue(r, "name"),
		Description:    formValue(r, "description"),
		SKU:            formValue(r, "sku"),
		UnitPriceMinor: price,
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
		renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "products/edit.html", page)
		return
	}
	http.Redirect(w, r, "/products?notice=Product+updated.", http.StatusSeeOther)
}

func (h *ProductHandler) Archive(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
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
	scope, ok := requireScope(w, r)
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
	})
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
