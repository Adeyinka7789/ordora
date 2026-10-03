package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/infra/id"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// CustomerHandler serves /customers/*.
type CustomerHandler struct {
	Repo      *postgres.CustomerRepo
	OrderRepo *postgres.OrderRepo
	Renderer  *render.Renderer
	IDs       id.Generator
	Now       func() time.Time
}

func NewCustomerHandler(repo *postgres.CustomerRepo, orderRepo *postgres.OrderRepo, r *render.Renderer) *CustomerHandler {
	return &CustomerHandler{
		Repo:      repo,
		OrderRepo: orderRepo,
		Renderer:  r,
		Now:       time.Now,
	}
}

// -----------------------------------------------------------------------------
// Page data
// -----------------------------------------------------------------------------

type customersIndexPage struct {
	Title       string
	CSRFToken   string
	Query       string
	Customers   []*customer.Customer
	Pagination  Pagination
	FlashNotice string
	FlashError  string
}

type customerFormPage struct {
	Title     string
	CSRFToken string
	Customer  *customer.Customer
	Error     string
	IsEdit    bool
}

type customerShowPage struct {
	Title     string
	CSRFToken string
	Customer  *customer.Customer
	Orders    []postgres.OrderListRow
}

// -----------------------------------------------------------------------------
// Index / List
// -----------------------------------------------------------------------------

func (h *CustomerHandler) Index(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}

	query := queryValue(r, "q")
	limit := 20
	offset := queryInt(r, "offset", 0)

	res, err := h.Repo.List(r.Context(), scope, postgres.ListOptions{
		Query:  query,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		h.renderIndex(w, r, customersIndexPage{
			Title:      "Customers",
			FlashError: "Could not load customers.",
		})
		return
	}

	page := customersIndexPage{
		Title:      "Customers",
		Query:      query,
		Customers:  res.Customers,
		Pagination: newPagination(res.Total, res.Limit, res.Offset),
	}
	if v := queryValue(r, "notice"); v != "" {
		page.FlashNotice = v
	}
	if v := queryValue(r, "error"); v != "" {
		page.FlashError = v
	}

	// HTMX request? Return only the table fragment.
	if isHTMX(r) {
		page.CSRFToken = csrfFromCtx(r)
		h.Renderer.Fragment(w, r, http.StatusOK, "customers/_table.html", page)
		return
	}

	h.renderIndex(w, r, page)
}

func (h *CustomerHandler) renderIndex(w http.ResponseWriter, r *http.Request, page customersIndexPage) {
	page.CSRFToken = csrfFromCtx(r)
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "customers/index.html", page)
}

// -----------------------------------------------------------------------------
// New / Create
// -----------------------------------------------------------------------------

func (h *CustomerHandler) New(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireScope(w, r); !ok {
		return
	}
	page := customerFormPage{
		Title:     "New customer",
		CSRFToken: csrfFromCtx(r),
		Customer:  &customer.Customer{},
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "customers/new.html", page)
}

func (h *CustomerHandler) Create(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	c, err := customer.New(
		h.IDs.New(), scope.OrgID,
		formValue(r, "name"),
		formValue(r, "email"),
		formValue(r, "phone"),
		formValue(r, "address"),
		formValue(r, "notes"),
		h.Now(),
	)
	if err != nil {
		page := customerFormPage{
			Title:     "New customer",
			CSRFToken: csrfFromCtx(r),
			Error:     humanizeCustomerError(err),
			Customer: &customer.Customer{
				Name:    formValue(r, "name"),
				Email:   formValue(r, "email"),
				Phone:   formValue(r, "phone"),
				Address: formValue(r, "address"),
				Notes:   formValue(r, "notes"),
			},
		}
		renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "customers/new.html", page)
		return
	}

	if err := h.Repo.Create(r.Context(), scope, c); err != nil {
		page := customerFormPage{
			Title:     "New customer",
			CSRFToken: csrfFromCtx(r),
			Error:     "Could not save customer. Please try again.",
			Customer:  c,
		}
		renderPage(w, r, h.Renderer, http.StatusInternalServerError, "layouts/app.html", "customers/new.html", page)
		return
	}

	http.Redirect(w, r, "/customers/"+c.ID.String(), http.StatusSeeOther)
}

// -----------------------------------------------------------------------------
// Show
// -----------------------------------------------------------------------------

func (h *CustomerHandler) Show(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	cid, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	c, err := h.Repo.GetByID(r.Context(), scope, cid)
	if err != nil {
		if errors.Is(err, customer.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load customer", http.StatusInternalServerError)
		return
	}

	// Load recent orders for this customer (best-effort).
	var orders []postgres.OrderListRow
	if res, err := h.OrderRepo.List(r.Context(), scope, postgres.OrderListOptions{
		CustomerID: cid,
		Limit:      20,
	}); err == nil {
		orders = res.Rows
	}

	page := customerShowPage{
		Title:     c.Name,
		CSRFToken: csrfFromCtx(r),
		Customer:  c,
		Orders:    orders,
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "customers/show.html", page)
}

// -----------------------------------------------------------------------------
// Edit / Update
// -----------------------------------------------------------------------------

func (h *CustomerHandler) Edit(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	cid, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	c, err := h.Repo.GetByID(r.Context(), scope, cid)
	if err != nil {
		if errors.Is(err, customer.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load customer", http.StatusInternalServerError)
		return
	}

	page := customerFormPage{
		Title:     "Edit customer",
		CSRFToken: csrfFromCtx(r),
		Customer:  c,
		IsEdit:    true,
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "customers/edit.html", page)
}

func (h *CustomerHandler) Update(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	cid, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	c, err := h.Repo.GetByID(r.Context(), scope, cid)
	if err != nil {
		if errors.Is(err, customer.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load customer", http.StatusInternalServerError)
		return
	}

	if err := c.Update(
		formValue(r, "name"),
		formValue(r, "email"),
		formValue(r, "phone"),
		formValue(r, "address"),
		formValue(r, "notes"),
		h.Now(),
	); err != nil {
		page := customerFormPage{
			Title:     "Edit customer",
			CSRFToken: csrfFromCtx(r),
			Error:     humanizeCustomerError(err),
			Customer:  c,
			IsEdit:    true,
		}
		renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "customers/edit.html", page)
		return
	}

	if err := h.Repo.Update(r.Context(), scope, c); err != nil {
		page := customerFormPage{
			Title:     "Edit customer",
			CSRFToken: csrfFromCtx(r),
			Error:     "Could not save changes. Please try again.",
			Customer:  c,
			IsEdit:    true,
		}
		renderPage(w, r, h.Renderer, http.StatusInternalServerError, "layouts/app.html", "customers/edit.html", page)
		return
	}

	http.Redirect(w, r, "/customers/"+c.ID.String(), http.StatusSeeOther)
}

// -----------------------------------------------------------------------------
// Delete
// -----------------------------------------------------------------------------

func (h *CustomerHandler) Delete(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	cid, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	if err := h.Repo.Delete(r.Context(), scope, cid); err != nil {
		if errors.Is(err, customer.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		// Foreign key violation = customer has orders.
		http.Redirect(w, r, "/customers?error=This+customer+cannot+be+deleted.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/customers?notice=Customer+deleted.", http.StatusSeeOther)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// requireScope returns the tenant scope, writing a 401 and returning false
// if the request is not authenticated.
func requireScope(w http.ResponseWriter, r *http.Request) (tenant.TenantScope, bool) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil || s.Scope.IsZero() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return tenant.TenantScope{}, false
	}
	return s.Scope, true
}

// parseUUIDParam reads a UUID route parameter.
func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	raw := r.PathValue(name)
	if raw == "" {
		http.NotFound(w, r)
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		http.NotFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}

func humanizeCustomerError(err error) string {
	switch {
	case errors.Is(err, customer.ErrNameRequired):
		return "Please enter a name."
	case errors.Is(err, customer.ErrNameTooLong):
		return "Name is too long (max 200 characters)."
	case errors.Is(err, customer.ErrEmailInvalid):
		return "Please enter a valid email address."
	case errors.Is(err, customer.ErrEmailTooLong):
		return "Email is too long."
	case errors.Is(err, customer.ErrPhoneTooLong):
		return "Phone number is too long."
	case errors.Is(err, customer.ErrAddressTooLong):
		return "Address is too long."
	case errors.Is(err, customer.ErrNotesTooLong):
		return "Notes are too long."
	default:
		return "Something went wrong. Please try again."
	}
}
