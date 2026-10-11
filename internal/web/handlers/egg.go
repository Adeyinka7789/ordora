package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// EggHandler serves the perishable-module seller panel
// (/dashboard/egg-orders) and the egg settings form. All routes are
// perishable-gated + flag-gated in main.go; settings writes are
// additionally owner-gated there and in the service.
type EggHandler struct {
	Egg      *app.EggService
	Renderer *render.Renderer
}

type eggOrderRow struct {
	ID            string
	Number        string
	CustomerName  string
	CustomerPhone string
	QtyCrates     string
	TotalMinor    int64
	PaidMinor     int64
	Currency      string
	Status        string
	ReceiptID     string
	IsNew         bool
	IsConfirmed   bool
}

type eggPanelPage struct {
	Title       string
	CSRFToken   string
	FlashNotice string
	FlashError  string
	Day         string // "today" | "tomorrow" (filter echo)
	DayLabel    string
	PendingOnly bool
	Cap         int
	Sold        string
	Left        string
	SoldPct     int // 0-100 cap bar width
	Orders      []eggOrderRow
	Pending     int
	Total       int
	IsOwner     bool
	Accepting   bool
}

// Index handles GET /dashboard/egg-orders.
func (h *EggHandler) Index(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	day := queryValue(r, "day")
	pendingOnly := queryValue(r, "pending") == "1"
	data, err := h.Egg.Panel(r.Context(), s.Scope, s.OrgTimezone, day, pendingOnly)
	if err != nil {
		http.Error(w, "could not load egg orders", http.StatusInternalServerError)
		return
	}
	left := float64(data.Cap) - data.Sold
	if left < 0 {
		left = 0
	}
	pct := 0
	if data.Cap > 0 {
		pct = int(data.Sold / float64(data.Cap) * 100)
		if pct > 100 {
			pct = 100
		}
	}
	rows := make([]eggOrderRow, 0, len(data.Orders))
	for _, o := range data.Orders {
		row := eggOrderRow{
			ID: o.ID.String(), Number: o.Number,
			CustomerName: o.CustomerName, CustomerPhone: o.CustomerPhone,
			QtyCrates:  app.FormatCrates(o.QtyCrates),
			TotalMinor: o.TotalMinor, PaidMinor: o.PaidMinor,
			Currency: o.Currency, Status: o.Status,
			IsNew: o.Status == "NEW", IsConfirmed: o.Status == "CONFIRMED",
		}
		if o.ReceiptID != nil {
			row.ReceiptID = o.ReceiptID.String()
		}
		rows = append(rows, row)
	}
	page(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "egg/index.html", eggPanelPage{
		Title:       "Egg orders",
		CSRFToken:   csrfFromCtx(r),
		FlashNotice: queryValue(r, "notice"),
		FlashError:  queryValue(r, "error"),
		Day:         data.DayLabel,
		DayLabel:    data.DayLabel,
		PendingOnly: pendingOnly,
		Cap:         data.Cap,
		Sold:        app.FormatCrates(data.Sold),
		Left:        app.FormatCrates(left),
		SoldPct:     pct,
		Orders:      rows,
		Pending:     data.Pending,
		Total:       len(rows),
		IsOwner:     data.IsOwner,
		Accepting:   data.Accepting,
	})
}

// backTo returns the panel URL preserving the day filter.
func backTo(r *http.Request, notice, errMsg string) string {
	q := url.Values{}
	if d := queryValue(r, "day"); d != "" {
		q.Set("day", d)
	}
	if notice != "" {
		q.Set("notice", notice)
	}
	if errMsg != "" {
		q.Set("error", errMsg)
	}
	u := "/dashboard/egg-orders"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// Confirm handles POST /dashboard/egg-orders/{id}/confirm: record the
// transfer payment and confirm, in one click.
func (h *EggHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
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
	if err := h.Egg.ConfirmOrder(r.Context(), s.Scope, id); err != nil {
		http.Redirect(w, r, backTo(r, "", humanizeEggError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, backTo(r, "Payment confirmed.", ""), http.StatusSeeOther)
}

// Reject handles POST /dashboard/egg-orders/{id}/reject: cancel, release
// the cap, reverse any payment. The actual refund happens offline; the
// optional reference is recorded in the reversal reason.
func (h *EggHandler) Reject(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
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
	if err := h.Egg.RejectOrder(r.Context(), s.Scope, id, formValue(r, "refund_ref")); err != nil {
		http.Redirect(w, r, backTo(r, "", humanizeEggError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, backTo(r, "Order rejected. Cap released.", ""), http.StatusSeeOther)
}

// Complete handles POST /dashboard/egg-orders/{id}/complete.
func (h *EggHandler) Complete(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
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
	if err := h.Egg.CompleteOrder(r.Context(), s.Scope, id); err != nil {
		http.Redirect(w, r, backTo(r, "", humanizeEggError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, backTo(r, "Order completed.", ""), http.StatusSeeOther)
}

// Toggle handles POST /dashboard/egg-orders/toggle (owner only upstream).
func (h *EggHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	accepting := formValue(r, "accepting") == "1"
	if err := h.Egg.SetAccepting(r.Context(), s.Scope, accepting); err != nil {
		http.Redirect(w, r, backTo(r, "", humanizeEggError(err)), http.StatusSeeOther)
		return
	}
	if accepting {
		http.Redirect(w, r, backTo(r, "Orders are open.", ""), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, backTo(r, "Orders are closed for today.", ""), http.StatusSeeOther)
}

// UpdateSettings handles POST /settings/egg (owner only upstream).
func (h *EggHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	maxCrates, err := strconv.Atoi(formValue(r, "max_crates_per_day"))
	if err != nil || maxCrates <= 0 {
		http.Redirect(w, r, "/settings?error="+url.QueryEscape("Daily cap must be a positive number."), http.StatusSeeOther)
		return
	}
	_, err = h.Egg.UpdateSettings(r.Context(), s.Scope, app.UpdateSettingsInput{
		MaxCratesPerDay: maxCrates,
		AcceptingOrders: formValue(r, "accepting_orders") == "1",
		BankName:        formValue(r, "bank_name"),
		AccountNumber:   formValue(r, "account_number"),
		AccountName:     formValue(r, "account_name"),
		Whatsapp:        formValue(r, "whatsapp_number"),
	})
	if err != nil {
		http.Redirect(w, r, "/settings?error="+url.QueryEscape(humanizeEggError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?notice="+url.QueryEscape("Egg settings saved."), http.StatusSeeOther)
}

func humanizeEggError(err error) string {
	switch {
	case errors.Is(err, app.ErrEggNotOwner):
		return "Only the business owner can do that."
	case errors.Is(err, app.ErrEggBadStatus):
		return "That order is no longer awaiting that action."
	case errors.Is(err, app.ErrEggSettingsInvalid):
		return "Check the egg settings values and try again."
	case errors.Is(err, app.ErrEggClosed):
		return "Orders are closed."
	case errors.Is(err, app.ErrEggOrderNotFound):
		return "Order not found."
	case errors.Is(err, order.ErrInvalidTransition):
		return "That status change is not allowed."
	case errors.Is(err, order.ErrNotFound):
		return "Order not found."
	case errors.Is(err, app.ErrOverpaymentNotAllowed):
		return "That amount is more than the outstanding balance."
	case errors.Is(err, app.ErrPaymentAlreadyReversed):
		return "This payment is already reversed."
	default:
		return "Something went wrong. Please try again."
	}
}
