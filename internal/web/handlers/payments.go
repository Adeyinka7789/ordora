package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/payment"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// PaymentHandler serves payment endpoints.
type PaymentHandler struct {
	Service  *app.PaymentService
	Orders   *app.OrderService
	Renderer *render.Renderer
}

// Record handles POST /orders/{id}/payments.
func (h *PaymentHandler) Record(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
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
		h.renderError(w, r, scope, orderID, "Please enter a valid amount.")
		return
	}

	method := payment.Method(formValue(r, "method"))
	if !method.IsValid() {
		h.renderError(w, r, scope, orderID, "Please choose a payment method.")
		return
	}

	var paidAt time.Time
	if v := formValue(r, "paid_at"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			h.renderError(w, r, scope, orderID, "Invalid payment date.")
			return
		}
		paidAt = t
	} else {
		paidAt = time.Now()
	}

	_, err = h.Service.RecordPayment(r.Context(), scope, app.RecordPaymentInput{
		OrderID:   orderID,
		Amount:    amount,
		Method:    method,
		Reference: formValue(r, "reference"),
		PaidAt:    paidAt,
		Notes:     formValue(r, "notes"),
	})
	if err != nil {
		h.renderError(w, r, scope, orderID, humanizePaymentError(err))
		return
	}

	h.renderPaymentsFragment(w, r, scope, orderID, "")
}

// renderError re-renders the payments fragment with an error banner.
func (h *PaymentHandler) renderError(w http.ResponseWriter, r *http.Request, scope tenant.TenantScope, orderID uuid.UUID, msg string) {
	h.renderPaymentsFragment(w, r, scope, orderID, msg)
}

// renderPaymentsFragment loads the order + payments and renders the fragment.
func (h *PaymentHandler) renderPaymentsFragment(w http.ResponseWriter, r *http.Request, scope tenant.TenantScope, orderID uuid.UUID, errMsg string) {
	o, err := h.Orders.GetOrder(r.Context(), scope, orderID)
	if err != nil {
		if errors.Is(err, order.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load order", http.StatusInternalServerError)
		return
	}

	list, err := h.Service.ListForOrder(r.Context(), scope, orderID)
	if err != nil {
		http.Error(w, "could not load payments", http.StatusInternalServerError)
		return
	}

	data := paymentsFragment{
		Order:     o,
		Payments:  list,
		Balance:   o.Balance().Amount(),
		PayStatus: payment.DeriveStatus(o.Total, o.Paid),
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
		Error:     errMsg,
	}
	h.Renderer.Fragment(w, r, http.StatusOK, "orders/_payments.html", data)
}

func humanizePaymentError(err error) string {
	switch {
	case errors.Is(err, app.ErrPaymentAmountRequired):
		return "Please enter an amount."
	case errors.Is(err, app.ErrPaymentMethodInvalid):
		return "Please choose a payment method."
	case errors.Is(err, app.ErrOverpaymentNotAllowed):
		return "That amount is more than the outstanding balance."
	case errors.Is(err, app.ErrOrderIsCancelled):
		return "This order is cancelled."
	case errors.Is(err, app.ErrPaidAtInFuture):
		return "Payment date cannot be in the future."
	case errors.Is(err, order.ErrNotFound):
		return "Order not found."
	default:
		return "Could not record payment: " + err.Error()
	}
}
