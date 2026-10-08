package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/cost"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// CostHandler serves cost CRUD endpoints.
type CostHandler struct {
	Service  *app.CostService
	Renderer *render.Renderer
}

// -----------------------------------------------------------------------------
// Add
// -----------------------------------------------------------------------------

// Add handles POST /orders/{id}/costs.
func (h *CostHandler) Add(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	orderID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	amount, err := parseMoneyMinor(formValue(r, "amount"))
	if err != nil || amount <= 0 {
		h.renderCosts(w, r, scope, orderID, "", "Please enter a valid amount.")
		return
	}

	cat := cost.Category(formValue(r, "category"))
	if !cat.IsValid() {
		h.renderCosts(w, r, scope, orderID, "", "Please choose a category.")
		return
	}

	incurredOn := time.Now()
	if v := formValue(r, "incurred_on"); v != "" {
		t, err := parseDateInput(v)
		if err != nil {
			h.renderCosts(w, r, scope, orderID, "", "Invalid date. Use day-month-year.")
			return
		}
		incurredOn = t
	}

	_, err = h.Service.AddCost(r.Context(), scope, app.AddCostInput{
		OrderID:     orderID,
		Category:    cat,
		Description: formValue(r, "description"),
		AmountMinor: amount,
		IncurredOn:  incurredOn,
		Vendor:      formValue(r, "vendor"),
		Notes:       formValue(r, "notes"),
	})
	if err != nil {
		h.renderCosts(w, r, scope, orderID, "", humanizeCostError(err))
		return
	}

	h.renderCosts(w, r, scope, orderID, "Cost added.", "")
}

// -----------------------------------------------------------------------------
// Update
// -----------------------------------------------------------------------------

// Update handles POST /costs/{id}.
func (h *CostHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	amount, err := parseMoneyMinor(formValue(r, "amount"))
	if err != nil || amount <= 0 {
		http.Error(w, "invalid amount", http.StatusBadRequest)
		return
	}

	cat := cost.Category(formValue(r, "category"))
	if !cat.IsValid() {
		http.Error(w, "invalid category", http.StatusBadRequest)
		return
	}

	incurredOn := time.Now()
	if v := formValue(r, "incurred_on"); v != "" {
		t, err := parseDateInput(v)
		if err != nil {
			http.Error(w, "invalid date", http.StatusBadRequest)
			return
		}
		incurredOn = t
	}

	updated, err := h.Service.UpdateCost(r.Context(), scope, id, app.UpdateCostInput{
		Category:    cat,
		Description: formValue(r, "description"),
		AmountMinor: amount,
		IncurredOn:  incurredOn,
		Vendor:      formValue(r, "vendor"),
		Notes:       formValue(r, "notes"),
	})
	if err != nil {
		http.Error(w, humanizeCostError(err), http.StatusBadRequest)
		return
	}

	h.renderCosts(w, r, scope, updated.OrderID, "Cost updated.", "")
}

// -----------------------------------------------------------------------------
// Delete
// -----------------------------------------------------------------------------

// Delete handles POST /costs/{id}/delete.
func (h *CostHandler) Delete(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	orderIDStr := r.URL.Query().Get("order_id")
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		http.Error(w, "missing order_id", http.StatusBadRequest)
		return
	}

	if err := h.Service.DeleteCost(r.Context(), scope, id); err != nil {
		http.Error(w, "could not delete cost", http.StatusInternalServerError)
		return
	}

	h.renderCosts(w, r, scope, orderID, "Cost deleted.", "")
}

// -----------------------------------------------------------------------------
// Rendering
// -----------------------------------------------------------------------------

// renderCosts loads the cost summary for the order and renders the
// orders/_costs.html fragment.
func (h *CostHandler) renderCosts(w http.ResponseWriter, r *http.Request, scope tenant.TenantScope, orderID uuid.UUID, flash, errMsg string) {
	summary, err := h.Service.SummaryForOrder(r.Context(), scope, orderID)
	if err != nil {
		http.Error(w, "could not load costs", http.StatusInternalServerError)
		return
	}
	h.Renderer.Fragment(w, r, http.StatusOK, "orders/_costs.html", map[string]any{
		"Costs":          summary,
		"CostCategories": cost.AllCategories(),
		"CSRFToken":      csrfFromCtx(r),
		"FlashNotice":    flash,
		"FlashError":     errMsg,
	})
}

func humanizeCostError(err error) string {
	switch {
	case errors.Is(err, cost.ErrDescriptionRequired):
		return "Please enter a description."
	case errors.Is(err, cost.ErrDescriptionTooLong):
		return "Description is too long."
	case errors.Is(err, cost.ErrAmountZero):
		return "Amount must be greater than zero."
	case errors.Is(err, cost.ErrAmountNegative):
		return "Amount cannot be negative."
	case errors.Is(err, cost.ErrCategoryInvalid):
		return "Please choose a valid category."
	case errors.Is(err, app.ErrCostOrderNotFound):
		return "Order not found."
	default:
		return "Something went wrong. Please try again."
	}
}
