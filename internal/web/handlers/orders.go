package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/attachment"
	"github.com/Adeyinka7789/ordora/internal/domain/cost"
	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/measurement"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/payment"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// OrderHandler serves /orders/*.
type OrderHandler struct {
	Service      *app.OrderService
	OrderRepo    *postgres.OrderRepo
	CustRepo     *postgres.CustomerRepo
	Orgs         *postgres.OrgRepo
	Measurements *postgres.MeasurementRepo
	Attachments  *app.AttachmentService
	Payments     *app.PaymentService
	Audit        *postgres.AuditRepo
	Renderer     *render.Renderer
	Costs        *app.CostService
	Groups       *postgres.GroupRepo
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

// orderTimelineData is passed to the timeline fragment both from the show
// page and from the HTMX status-change response.

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
	FormDisc    string
	FormTax     string

	// Tailoring measurements (only populated for tailoring orgs).
	IsTailoring        bool
	MeasTemplates      []measTemplateOption
	FormMeasGender     string
	FormMeasTemplateID string
	FormMeasFields     []measFieldView
	FormMeasNotes      string
	FieldErrors        map[string]string
}

type orderFormItem struct {
	Description string
	Quantity    string
	UnitPrice   string
}

type orderEditPage struct {
	Title       string
	CSRFToken   string
	Order       *order.Order
	Customers   []*customer.Customer
	Error       string
	FormTitle   string
	FormDesc    string
	FormDueDate string
	FormItems   []orderFormItem
	FormDisc    string
	FormTax     string
	FormCustID  string

	// Tailoring measurements (only populated for tailoring orgs).
	IsTailoring        bool
	MeasTemplates      []measTemplateOption
	FormMeasGender     string
	FormMeasTemplateID string
	FormMeasFields     []measFieldView
	FormMeasNotes      string
	FieldErrors        map[string]string
}

// orderShowPage is passed to orders/show.html. The timeline and attachments
// fragments are rendered with the same page object (their `.Order`,
// `.CSRFToken`, `.Error`, `.OrderID`, and `.Attachments` fields all resolve
// against this struct), so no embedding or dict helper is needed.
type orderShowPage struct {
	Title       string
	CSRFToken   string
	Customer    *customer.Customer
	Balance     int64
	PaidPercent int
	FlashNotice string
	FlashError  string

	TokenRaw  string
	PortalURL string

	Error                string
	Order                *order.Order
	OrderID              uuid.UUID
	Measurement          *measShowView
	Group                *orderGroupBadge
	Attachments          []*attachment.Attachment
	HasInspiration       bool
	Payments             []*payment.Payment
	PayStatus            payment.Status
	AttachmentsByPayment map[uuid.UUID][]*attachment.Attachment
	AuditTrail           []postgres.AuditTrailRow
	Costs                *app.OrderCostSummary
	CostCategories       []cost.Category
}

// orderGroupBadge links an order to its aso-ebi group (nil when none).
type orderGroupBadge struct {
	ID   string
	Name string
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
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "orders/index.html", page)
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
	h.fillMeasNewForm(r, scope, &page, "", "", nil, "")
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "orders/new.html", page)
}

// measFormData builds the measurement section for new/edit pages. It returns
// zero-value (IsTailoring false) unless the org is a tailoring business.
// gender/templateID/values/notes carry the user's current (or re-rendered)
// selection; blanks resolve to defaults (male, first template).
func (h *OrderHandler) measFormData(r *http.Request, scope tenant.TenantScope, gender, templateID string, values map[string]string, notes string) measFormData {
	data := measFormData{}
	if h.Measurements == nil {
		return data
	}
	org, err := h.Orgs.GetByID(r.Context(), scope.OrgID)
	if err != nil || !isTailoringOrg(org) {
		return data
	}
	list := h.measTemplateList(r, scope)
	if len(list) == 0 {
		return data
	}
	data.IsTailoring = true
	data.Templates = measOptions(list)
	if gender != string(measurement.GenderMale) && gender != string(measurement.GenderFemale) {
		gender = string(measurement.GenderMale)
	}
	tmpl, ok := measTemplateByID(list, parseUUIDOrNil(templateID))
	if !ok {
		tmpl, _ = firstTemplateOfGender(list, gender)
		gender = string(tmpl.Gender)
	}
	data.Gender = gender
	data.TemplateID = tmpl.ID.String()
	data.Fields = measFieldsFor(tmpl, values)
	data.Notes = notes
	return data
}

// measFormData is the measurement section shared by new/edit pages.
type measFormData struct {
	IsTailoring bool
	Templates   []measTemplateOption
	Gender      string
	TemplateID  string
	Fields      []measFieldView
	Notes       string
}

// fillMeasNewForm assigns shared measurement data onto a new-order page.
func (h *OrderHandler) fillMeasNewForm(r *http.Request, scope tenant.TenantScope, page *orderNewPage, gender, templateID string, values map[string]string, notes string) {
	d := h.measFormData(r, scope, gender, templateID, values, notes)
	page.IsTailoring = d.IsTailoring
	page.MeasTemplates = d.Templates
	page.FormMeasGender = d.Gender
	page.FormMeasTemplateID = d.TemplateID
	page.FormMeasFields = d.Fields
	page.FormMeasNotes = d.Notes
}

// fillMeasEditForm assigns shared measurement data onto an edit-order page.
func (h *OrderHandler) fillMeasEditForm(r *http.Request, scope tenant.TenantScope, page *orderEditPage, gender, templateID string, values map[string]string, notes string) {
	d := h.measFormData(r, scope, gender, templateID, values, notes)
	page.IsTailoring = d.IsTailoring
	page.MeasTemplates = d.Templates
	page.FormMeasGender = d.Gender
	page.FormMeasTemplateID = d.TemplateID
	page.FormMeasFields = d.Fields
	page.FormMeasNotes = d.Notes
}

// parseUUIDOrNil parses an id, returning Nil on any error.
func parseUUIDOrNil(s string) uuid.UUID {
	id, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil {
		return uuid.Nil
	}
	return id
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
		Currency:    currencyFromRequest(r),
	}

	if due := formValue(r, "expected_completion"); due != "" {
		t, err := parseDateInput(due)
		if err != nil {
			h.respondCreateError(w, r, scope.OrgID, "Invalid expected completion date. Use day-month-year.", nil)
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

	// Optional tailoring measurements (order form section, tailoring orgs).
	// Echo data for error re-renders is rebuilt from in.Measurement.
	measIn, _, _, _ := parseMeasurementInput(r)
	in.Measurement = measIn

	o, err := h.Service.CreateOrder(r.Context(), scope, in)
	if err != nil {
		msg := humanizeMeasurementError(err)
		if msg == "" {
			msg = humanizeOrderError(err)
		}
		h.respondCreateError(w, r, scope.OrgID, msg, &in)
		return
	}

	http.Redirect(w, r, "/orders/"+o.ID.String(), http.StatusSeeOther)
}

func (h *OrderHandler) Edit(w http.ResponseWriter, r *http.Request) {
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
	if o.Status.IsTerminal() {
		http.Redirect(w, r, "/orders/"+o.ID.String(), http.StatusSeeOther)
		return
	}

	customers, err := h.CustRepo.List(r.Context(), scope, postgres.ListOptions{Limit: 500})
	if err != nil {
		http.Error(w, "could not load customers", http.StatusInternalServerError)
		return
	}

	page := orderEditPage{
		Title:      "Edit " + o.Number,
		CSRFToken:  csrfFromCtx(r),
		Order:      o,
		Customers:  customers.Customers,
		FormCustID: o.CustomerID.String(),
		FormTitle:  o.Title,
		FormDesc:   o.Description,
		FormDisc:   formatMoneyMinor(o.Discount.Amount()),
		FormTax:    formatMoneyMinor(o.Tax.Amount()),
	}
	if o.ExpectedCompletion != nil {
		page.FormDueDate = o.ExpectedCompletion.Format("2006-01-02")
	}
	for _, it := range o.Items {
		page.FormItems = append(page.FormItems, orderFormItem{
			Description: it.Description,
			Quantity:    formatQuantity(it.Quantity),
			UnitPrice:   formatMoneyMinor(it.UnitPrice.Amount()),
		})
	}
	if len(page.FormItems) == 0 {
		page.FormItems = []orderFormItem{{}}
	}
	// Prefill saved measurements (best-effort; blanks resolve to defaults).
	var eg, etid string
	evals := map[string]string{}
	enotes := ""
	if h.Measurements != nil {
		if m, err := h.Measurements.GetByOrder(r.Context(), scope, o.ID); err == nil {
			eg, etid, evals, enotes = string(m.Gender), m.TemplateID.String(), m.Values, m.Notes
		}
	}
	h.fillMeasEditForm(r, scope, &page, eg, etid, evals, enotes)
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "orders/edit.html", page)
}

// Update handles POST /orders/{id}.
func (h *OrderHandler) Update(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	oid, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	in, err := parseUpdateOrderInput(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err = h.Service.UpdateOrder(r.Context(), scope, oid, *in)
	if err != nil {
		if errors.Is(err, order.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		// Re-render with error.
		o, _ := h.Service.GetOrder(r.Context(), scope, oid)
		customers, _ := h.CustRepo.List(r.Context(), scope, postgres.ListOptions{Limit: 500})
		errMsg := humanizeMeasurementError(err)
		if errMsg == "" {
			errMsg = humanizeOrderError(err)
		}
		page := orderEditPage{
			Title:       "Edit order",
			CSRFToken:   csrfFromCtx(r),
			Order:       o,
			Customers:   customers.Customers,
			Error:       errMsg,
			FieldErrors: fieldErrorsForOrderForm(errMsg),
			FormTitle:   in.Title,
			FormDesc:    in.Description,
			FormDisc:    formatMoneyMinor(in.DiscountMinor),
			FormTax:     formatMoneyMinor(in.TaxMinor),
		}
		for _, it := range in.Items {
			page.FormItems = append(page.FormItems, orderFormItem{
				Description: it.Description,
				Quantity:    formatQuantity(it.QuantityScaled),
				UnitPrice:   formatMoneyMinor(it.UnitPriceMinor),
			})
		}
		if len(page.FormItems) == 0 {
			page.FormItems = []orderFormItem{{}}
		}
		if g, tid, fields, notes, ok := h.measEchoForError(r, scope, in.Measurement); ok {
			page.IsTailoring = true
			page.MeasTemplates = measOptions(h.measTemplateList(r, scope))
			page.FormMeasGender = g
			page.FormMeasTemplateID = tid
			page.FormMeasFields = fields
			page.FormMeasNotes = notes
		}
		renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "orders/edit.html", page)
		return
	}

	http.Redirect(w, r, "/orders/"+oid.String()+"?notice=Order+updated.", http.StatusSeeOther)
}

// ChangeStatus handles POST /orders/{id}/status.
//
// Returns the timeline fragment if HTMX, otherwise redirects to the order.
func (h *OrderHandler) ChangeStatus(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	oid, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	to := order.Status(formValue(r, "status"))
	if !to.IsValid() {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}

	o, err := h.Service.ChangeStatus(r.Context(), scope, oid, to)
	if err != nil {
		if errors.Is(err, order.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		// HTMX: return the timeline with an error banner.
		if isHTMX(r) {
			existing, _ := h.Service.GetOrder(r.Context(), scope, oid)
			h.Renderer.Fragment(w, r, http.StatusOK, "orders/_timeline.html", timelineFragment{
				Order:     existing,
				CSRFToken: csrfFromCtx(r),
				Error:     humanizeOrderError(err),
			})
			return
		}

		http.Redirect(w, r, "/orders/"+oid.String()+"?error="+humanizeOrderError(err), http.StatusSeeOther)
		return
	}

	if isHTMX(r) {
		h.Renderer.Fragment(w, r, http.StatusOK, "orders/_timeline.html", timelineFragment{
			Order:     o,
			CSRFToken: csrfFromCtx(r),
		})
		return
	}
	http.Redirect(w, r, "/orders/"+oid.String()+"?notice=Status+updated.", http.StatusSeeOther)
}

// parseUpdateOrderInput reads the shared form fields (used by Update).
func parseUpdateOrderInput(r *http.Request) (*app.UpdateOrderInput, error) {
	in := &app.UpdateOrderInput{
		Title:       formValue(r, "title"),
		Description: formValue(r, "description"),
	}
	if due := formValue(r, "expected_completion"); due != "" {
		t, err := parseDateInput(due)
		if err != nil {
			return nil, errors.New("invalid expected completion date")
		}
		in.ExpectedCompletion = &t
	}
	if d := formValue(r, "discount"); d != "" {
		v, err := parseMoneyMinor(d)
		if err != nil {
			return nil, errors.New("invalid discount")
		}
		in.DiscountMinor = v
	}
	if tx := formValue(r, "tax"); tx != "" {
		v, err := parseMoneyMinor(tx)
		if err != nil {
			return nil, errors.New("invalid tax")
		}
		in.TaxMinor = v
	}
	itemCount, _ := strconv.Atoi(formValue(r, "item_count"))
	for i := 0; i < itemCount; i++ {
		desc := formValue(r, "items["+strconv.Itoa(i)+"][description]")
		qtyRaw := formValue(r, "items["+strconv.Itoa(i)+"][quantity]")
		priceRaw := formValue(r, "items["+strconv.Itoa(i)+"][unit_price]")
		if desc == "" && qtyRaw == "" && priceRaw == "" {
			continue
		}
		q, err := parseQuantity(qtyRaw)
		if err != nil {
			return nil, errors.New("invalid quantity on line " + strconv.Itoa(i+1))
		}
		p, err := parseMoneyMinor(priceRaw)
		if err != nil {
			return nil, errors.New("invalid unit price on line " + strconv.Itoa(i+1))
		}
		in.Items = append(in.Items, app.CreateOrderItemInput{
			Description:    desc,
			QuantityScaled: q,
			UnitPriceMinor: p,
		})
	}
	measIn, _, _, _ := parseMeasurementInput(r)
	in.Measurement = measIn
	return in, nil
}

// respondCreateError re-renders the new-order page with an error message and
// the previously-entered values, so the user does not have to retype anything.
func (h *OrderHandler) respondCreateError(w http.ResponseWriter, r *http.Request, orgID uuid.UUID, msg string, in *app.CreateOrderInput) {
	scope := middleware.ScopeFromContext(r.Context())
	customers, _ := h.CustRepo.List(r.Context(), scope, postgres.ListOptions{Limit: 500})

	page := orderNewPage{
		Title:       "New order",
		CSRFToken:   csrfFromCtx(r),
		Customers:   customers.Customers,
		Error:       msg,
		FieldErrors: fieldErrorsForOrderForm(msg),
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
		// Preserve the measurement selection (gender/garment/values/notes).
		if g, tid, fields, notes, ok := h.measEchoForError(r, scope, in.Measurement); ok {
			page.IsTailoring = true
			page.MeasTemplates = measOptions(h.measTemplateList(r, scope))
			page.FormMeasGender = g
			page.FormMeasTemplateID = tid
			page.FormMeasFields = fields
			page.FormMeasNotes = notes
		}
	}
	if len(page.FormItems) == 0 {
		page.FormItems = []orderFormItem{{}}
	}
	renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "orders/new.html", page)
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
		slog.Error("orders: get order failed", "err", err, "order_id", oid)
		http.Error(w, "could not load order", http.StatusInternalServerError)
		return
	}

	// Load the customer for display (order stores only customer_id).
	cust, err := h.CustRepo.GetByID(r.Context(), scope, o.CustomerID)
	if err != nil && !errors.Is(err, customer.ErrNotFound) {
		slog.Error("orders: get customer failed", "err", err, "customer_id", o.CustomerID)
		http.Error(w, "could not load customer", http.StatusInternalServerError)
		return
	}

	page := orderShowPage{
		Title:     o.Number,
		CSRFToken: csrfFromCtx(r),
		Customer:  cust,
		Balance:   o.Balance().Amount(),
		Order:     o,
		OrderID:   o.ID,
	}

	// If a fresh token was just generated, display it once.
	if raw := queryValue(r, "token"); raw != "" {
		page.TokenRaw = raw
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		page.PortalURL = scheme + "://" + r.Host + "/o/" + raw
	}

	if v := queryValue(r, "notice"); v != "" {
		page.FlashNotice = v
	}
	if v := queryValue(r, "error"); v != "" {
		page.FlashError = v
	}
	// Load attachments (best-effort).
	if list, err := h.Attachments.List(r.Context(), scope, attachment.EntityOrder, o.ID); err == nil {
		page.Attachments = list
		page.HasInspiration = hasInspiration(list)
	}

	// Load tailoring measurements (best-effort; absent for non-tailoring).
	// The snapshot stored at order time renders history even if the template
	// was edited or deleted afterwards; the live template is only a fallback
	// for rows written before the snapshot migration.
	if h.Measurements != nil {
		if m, err := h.Measurements.GetByOrder(r.Context(), scope, o.ID); err == nil {
			fields := m.SnapshotFields
			if len(fields) == 0 && m.TemplateID != uuid.Nil {
				if tmpl, err := h.Measurements.GetTemplate(r.Context(), scope, m.TemplateID); err == nil {
					fields = tmpl.Fields
				}
			}
			view := &measShowView{
				GenderLabel:  m.Gender.Label(),
				Garment:      m.Garment,
				TemplateName: m.TemplateName,
				Notes:        m.Notes,
			}
			for _, f := range fields {
				if v := strings.TrimSpace(m.Values[f.Key]); v != "" {
					view.Rows = append(view.Rows, measFieldView{
						Label: f.Label, Value: v, Unit: f.Unit,
					})
				}
			}
			for k, v := range m.ExtraValues {
				if strings.TrimSpace(v) == "" && strings.TrimSpace(k) == "" {
					continue
				}
				view.Extras = append(view.Extras, measFieldView{Label: k, Value: v})
			}
			page.Measurement = view
		}
	}

	// Load the aso-ebi group badge (best-effort).
	if h.Groups != nil {
		if g, err := h.Groups.FindByOrder(r.Context(), scope, o.ID); err == nil {
			page.Group = &orderGroupBadge{ID: g.ID.String(), Name: g.Name}
		}
	}

	// Load payments + per-payment receipts (best-effort, one batched query).
	if list, err := h.Payments.ListForOrder(r.Context(), scope, o.ID); err == nil {
		page.Payments = list
		ids := make([]uuid.UUID, 0, len(list))
		for _, p := range list {
			ids = append(ids, p.ID)
		}
		byPayment := make(map[uuid.UUID][]*attachment.Attachment, len(list))
		if grouped, err := h.Attachments.ListForEntities(r.Context(), scope, attachment.EntityPayment, ids); err == nil {
			byPayment = grouped
		}
		page.AttachmentsByPayment = byPayment
	}

	// Costs summary (best-effort).
	if summary, err := h.Costs.SummaryForOrder(r.Context(), scope, o.ID); err == nil {
		page.Costs = summary
	}
	page.CostCategories = cost.AllCategories()

	// Audit trail (best-effort).
	if h.Audit != nil {
		if trail, err := h.Audit.ListTrailForEntity(r.Context(), scope, "ORDER", o.ID, 30); err == nil {
			page.AuditTrail = trail
		}
	}

	// Derived fields.
	page.PayStatus = payment.DeriveStatus(o.Total, o.Paid)
	if o.Total.Amount() > 0 {
		page.PaidPercent = int(o.Paid.Amount() * 100 / o.Total.Amount())
	}

	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "orders/show.html", page)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// normalizeNumberInput strips Nigerian-style grouping so "50,000.00",
// "50 000" and "₦50,000" all parse. Every money/quantity form funnels
// through parseQuantity/parseMoneyMinor below, so this covers all of them.
func normalizeNumberInput(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")
	upper := strings.ToUpper(s)
	for _, p := range []string{"₦", "$", "£", "€", "NGN", "USD", "GBP", "EUR"} {
		if strings.HasPrefix(upper, p) {
			s = strings.TrimSpace(s[len(p):])
			break
		}
	}
	return s
}

// parseQuantity parses "2", "2.5", "1.125" (or "1,000.5") into a scaled int64.
func parseQuantity(s string) (int64, error) {
	s = normalizeNumberInput(s)
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

// parseMoneyMinor parses "1500.00" (or "1,500.00") into int64 minor units (150000).
func parseMoneyMinor(s string) (int64, error) {
	s = normalizeNumberInput(s)
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

// RegenerateToken creates a fresh public portal token for the order and
// redirects back to the order detail with the raw token in the URL.
func (h *OrderHandler) RegenerateToken(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	oid, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	raw, err := h.Service.RegeneratePublicToken(r.Context(), scope, oid)
	if err != nil {
		if errors.Is(err, order.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not regenerate token", http.StatusInternalServerError)
		return
	}

	// The receipt page mints its client link through this same endpoint and
	// asks to land back on the receipt (?next=receipt).
	if r.FormValue("next") == "receipt" {
		http.Redirect(w, r, "/orders/"+oid.String()+"/receipt?token="+raw, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/orders/"+oid.String()+"?token="+raw, http.StatusSeeOther)
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

// fieldErrorsForOrderForm maps an order-form error message to the field it
// belongs to. Title/date/customer errors point at their input; item,
// money, and measurement problems point at the items section.
func fieldErrorsForOrderForm(msg string) map[string]string {
	lower := strings.ToLower(msg)
	field := ""
	switch {
	case strings.Contains(lower, "title"):
		field = "title"
	case strings.Contains(lower, "customer"):
		field = "customer_id"
	case strings.Contains(lower, "completion date"),
		strings.Contains(lower, "expected completion"):
		field = "expected_completion"
	case strings.Contains(lower, "item"),
		strings.Contains(lower, "quantity"),
		strings.Contains(lower, "unit price"),
		strings.Contains(lower, "line "),
		strings.Contains(lower, "discount"),
		strings.Contains(lower, "measurement"):
		field = "items"
	}
	if field == "" {
		return nil
	}
	return map[string]string{field: msg}
}

// costsFragment builds the map passed to the orders/_costs.html fragment.
func costsFragment(page orderShowPage) map[string]any {
	return map[string]any{
		"Summary":    page.Costs,
		"CSRFToken":  page.CSRFToken,
		"Flash":      "",
		"Error":      "",
		"Categories": cost.AllCategories(),
	}
}
