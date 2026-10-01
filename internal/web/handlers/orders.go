package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// OrderHandler serves /orders/*.
type OrderHandler struct {
	Service   *app.OrderService
	OrderRepo *postgres.OrderRepo
	CustRepo  *postgres.CustomerRepo
	Renderer  *render.Renderer
}

// -----------------------------------------------------------------------------
// Page data
// -----------------------------------------------------------------------------

type ordersIndexPage struct {
	Title       string
	CSRFToken   string
	Query       string
	Status      string
	Rows        []postgres.OrderListRow
	Pagination  Pagination
	FlashNotice string
	FlashError  string
}

type orderNewPage struct {
	Title       string
	CSRFToken   string
	Customers   []*customer.Customer
	Error       string
	FormTitle   string
	FormDesc    string
	FormDueDate string
	FormItems   []orderFormItem
	FormCustID  string
}

type orderFormItem struct {
	Description string
	Quantity    string
	UnitPrice   string
}

type orderShowPage struct {
	Title     string
	CSRFToken string
	Order     *order.Order
	Customer  *customer.Customer
	Balance   int64
}

// -----------------------------------------------------------------------------
// Index / List
// -----------------------------------------------------------------------------

func (h *OrderHandler) Index(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}

	query := queryValue(r, "q")
	statusStr := queryValue(r, "status")
	limit := 20
	offset := queryInt(r, "offset", 0)

	var statusFilter order.Status
	if statusStr != "" && order.Status(statusStr).IsValid() {
		statusFilter = order.Status(statusStr)
	}

	res, err := h.OrderRepo.List(r.Context(), scope, postgres.OrderListOptions{
		Query:  query,
		Status: statusFilter,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		h.renderIndex(w, r, ordersIndexPage{
			Title:      "Orders",
			FlashError: "Could not load orders.",
		})
		return
	}

	page := ordersIndexPage{
		Title:      "Orders",
		Query:      query,
		Status:     statusStr,
		Rows:       res.Rows,
		Pagination: newPagination(res.Total, res.Limit, res.Offset),
	}
	if v := queryValue(r, "notice"); v != "" {
		page.FlashNotice = v
	}
	if v := queryValue(r, "error"); v != "" {
		page.FlashError = v
	}

	if isHTMX(r) {
		page.CSRFToken = csrfFromCtx(r)
		h.Renderer.Fragment(w, r, http.StatusOK, "orders/_table.html", page)
		return
	}

	h.renderIndex(w, r, page)
}

func (h *OrderHandler) renderIndex(w http.ResponseWriter, r *http.Request, page ordersIndexPage) {
	page.CSRFToken = csrfFromCtx(r)
	h.Renderer.Page(w, http.StatusOK, "layouts/app.html", "orders/index.html", page)
}

// -----------------------------------------------------------------------------
// New / Create
// -----------------------------------------------------------------------------

func (h *OrderHandler) New(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}

	// Load customers for the dropdown. Limit to a reasonable number — a
	// proper implementation would offer autocomplete, but 500 is plenty for
	// a first version.
	customers, err := h.CustRepo.List(r.Context(), scope, postgres.ListOptions{Limit: 500})
	if err != nil {
		http.Error(w, "could not load customers", http.StatusInternalServerError)
		return
	}

	page := orderNewPage{
		Title:     "New order",
		CSRFToken: csrfFromCtx(r),
		Customers: customers.Customers,
		FormItems: []orderFormItem{{}},
	}
	if len(customers.Customers) == 0 {
		page.Error = "You need to create a customer before you can create an order."
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/app.html", "orders/new.html", page)
}

func (h *OrderHandler) Create(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	custID, err := uuid.Parse(formValue(r, "customer_id"))
	if err != nil {
		h.respondCreateError(w, r, scope.OrgID, "Please choose a customer.", nil)
		return
	}

	in := app.CreateOrderInput{
		CustomerID:  custID,
		Title:       formValue(r, "title"),
		Description: formValue(r, "description"),
		Currency:    "NGN", // TODO: from org settings
	}

	if due := formValue(r, "expected_completion"); due != "" {
		t, err := time.Parse("2006-01-02", due)
		if err != nil {
			h.respondCreateError(w, r, scope.OrgID, "Invalid expected completion date.", nil)
			return
		}
		in.ExpectedCompletion = &t
	}

	// Parse line items from the form. Fields are named
	// items[0][description], items[0][quantity], items[0][unit_price], etc.
	itemCount, _ := strconv.Atoi(formValue(r, "item_count"))
	if itemCount <= 0 {
		h.respondCreateError(w, r, scope.OrgID, "At least one item is required.", nil)
		return
	}
	for i := 0; i < itemCount; i++ {
		desc := formValue(r, "items["+strconv.Itoa(i)+"][description]")
		qtyRaw := formValue(r, "items["+strconv.Itoa(i)+"][quantity]")
		priceRaw := formValue(r, "items["+strconv.Itoa(i)+"][unit_price]")
		if desc == "" && qtyRaw == "" && priceRaw == "" {
			continue // blank row, skip
		}
		qty, err := parseQuantity(qtyRaw)
		if err != nil {
			h.respondCreateError(w, r, scope.OrgID, "Invalid quantity on line "+strconv.Itoa(i+1)+".", nil)
			return
		}
		price, err := parseMoneyMinor(priceRaw)
		if err != nil {
			h.respondCreateError(w, r, scope.OrgID, "Invalid unit price on line "+strconv.Itoa(i+1)+".", nil)
			return
		}
		in.Items = append(in.Items, app.CreateOrderItemInput{
			Description:    desc,
			QuantityScaled: qty,
			UnitPriceMinor: price,
		})
	}

	o, err := h.Service.CreateOrder(r.Context(), scope, in)
	if err != nil {
		h.respondCreateError(w, r, scope.OrgID, humanizeOrderError(err), &in)
		return
	}

	http.Redirect(w, r, "/orders/"+o.ID.String(), http.StatusSeeOther)
}

// respondCreateError re-renders the new-order page with an error message and
// the previously-entered values, so the user does not have to retype anything.
func (h *OrderHandler) respondCreateError(w http.ResponseWriter, r *http.Request, orgID uuid.UUID, msg string, in *app.CreateOrderInput) {
	scope := middleware.ScopeFromContext(r.Context())
	customers, _ := h.CustRepo.List(r.Context(), scope, postgres.ListOptions{Limit: 500})

	page := orderNewPage{
		Title:     "New order",
		CSRFToken: csrfFromCtx(r),
		Customers: customers.Customers,
		Error:     msg,
	}
	if in != nil {
		page.FormCustID = in.CustomerID.String()
		page.FormTitle = in.Title
		page.FormDesc = in.Description
		if in.ExpectedCompletion != nil {
			page.FormDueDate = in.ExpectedCompletion.Format("2006-01-02")
		}
		for _, it := range in.Items {
			page.FormItems = append(page.FormItems, orderFormItem{
				Description: it.Description,
				Quantity:    formatQuantity(it.QuantityScaled),
				UnitPrice:   formatMoneyMinor(it.UnitPriceMinor),
			})
		}
	}
	if len(page.FormItems) == 0 {
		page.FormItems = []orderFormItem{{}}
	}
	h.Renderer.Page(w, http.StatusBadRequest, "layouts/app.html", "orders/new.html", page)
}

// -----------------------------------------------------------------------------
// Show
// -----------------------------------------------------------------------------

func (h *OrderHandler) Show(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	oid, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	o, err := h.Service.GetOrder(r.Context(), scope, oid)
	if err != nil {
		if errors.Is(err, order.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load order", http.StatusInternalServerError)
		return
	}

	// Load the customer for display (order stores only customer_id).
	cust, err := h.CustRepo.GetByID(r.Context(), scope, o.CustomerID)
	if err != nil && !errors.Is(err, customer.ErrNotFound) {
		http.Error(w, "could not load customer", http.StatusInternalServerError)
		return
	}

	page := orderShowPage{
		Title:     o.Number,
		CSRFToken: csrfFromCtx(r),
		Order:     o,
		Customer:  cust,
		Balance:   o.Balance().Amount(),
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/app.html", "orders/show.html", page)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// parseQuantity parses "2", "2.5", "1.125" into a scaled int64.
func parseQuantity(s string) (int64, error) {
	if s == "" {
		return 0, errors.New("empty quantity")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if f <= 0 {
		return 0, errors.New("quantity must be positive")
	}
	return int64(f*float64(order.QuantityScale) + 0.5), nil
}

func formatQuantity(scaled int64) string {
	// Preserve up to 3 decimal places, trim trailing zeros.
	s := strconv.FormatFloat(float64(scaled)/float64(order.QuantityScale), 'f', 3, 64)
	for len(s) > 1 && s[len(s)-1] == '0' && s[len(s)-2] != '.' {
		s = s[:len(s)-1]
	}
	return s
}

// parseMoneyMinor parses "1500.00" into int64 minor units (150000).
func parseMoneyMinor(s string) (int64, error) {
	if s == "" {
		return 0, errors.New("empty amount")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if f < 0 {
		return 0, errors.New("amount cannot be negative")
	}
	return int64(f*100 + 0.5), nil
}

func formatMoneyMinor(minor int64) string {
	return strconv.FormatFloat(float64(minor)/100, 'f', 2, 64)
}

func humanizeOrderError(err error) string {
	switch {
	case errors.Is(err, app.ErrOrderTitleRequired):
		return "Please enter a title for the order."
	case errors.Is(err, app.ErrOrderNoItems):
		return "An order must have at least one item."
	case errors.Is(err, app.ErrCustomerNotFound):
		return "The selected customer does not exist."
	case errors.Is(err, order.ErrInvalidTransition):
		return "That status change is not allowed."
	case errors.Is(err, order.ErrDiscountTooLarge):
		return "The discount cannot exceed the subtotal."
	default:
		return "Something went wrong. Please try again."
	}
}
