package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/attachment"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/infra/id"
	"github.com/Adeyinka7789/ordora/internal/infra/storage"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// PortalHandler serves the public customer-facing pages.
type PortalHandler struct {
	Portal   *app.PortalService
	Public   *app.PublicOrderService
	Booking  *app.BookingService
	Blobs    storage.Storage
	IDs      id.Generator
	Renderer *render.Renderer
}

// --- Portal view ---

type portalPageData struct {
	Title           string
	OrgName         string // for the public layout header
	Token           string // raw public token, for building the receipt URL
	View            *app.PortalView
	Items           []portalItemView
	OrderForStepper *order.Order
	// Booking is the appointment behind this order, if any. Nil for
	// ordinary orders — the block hides.
	Booking *bookingTrackView
}

// bookingTrackView is the tracking-page projection of an appointment.
type bookingTrackView struct {
	When        string // "Mon 02 Jan, 15:04 – 16:00" in the org timezone
	Status      string // raw status for statusLabel
	ServiceName string
	// BookAgain carries the prefilled rebooking link; empty when the
	// service is unknown (very old rows).
	BookAgain string
}

type portalItemView struct {
	Description string
	Quantity    int64
	UnitPrice   interface {
		Amount() int64
		Currency() string
	}
	Subtotal interface {
		Amount() int64
		Currency() string
	}
	Material string
	ImageRef string
}

// Show handles GET /o/{token}.
func (h *PortalHandler) Show(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		h.notFound(w, r)
		return
	}
	v, err := h.Portal.Load(r.Context(), token)
	if err != nil {
		if errors.Is(err, app.ErrPortalNotFound) {
			h.notFound(w, r)
			return
		}
		slog.Error("portal: load failed", "err", err, "token_hash", app.HashTokenHex(token))
		http.Error(w, "could not load order", http.StatusInternalServerError)
		return
	}

	// Build a minimal order.Order for the stepper fragment.
	stepper := &order.Order{
		Status: v.Status,
	}

	data := portalPageData{
		Title:           v.OrderNumber,
		OrgName:         v.OrgName,
		Token:           token,
		View:            v,
		OrderForStepper: stepper,
		Items:           buildItemViews(v),
		Booking:         h.trackBooking(r, token, v.OrgSlug),
	}
	h.Renderer.PagePublic(w, http.StatusOK, "layouts/public.html", "portal/show.html", data)
}

// trackBooking loads the appointment behind an order for the tracking
// page. Best-effort and silent: ordinary orders simply show no block.
func (h *PortalHandler) trackBooking(r *http.Request, token, orgSlug string) *bookingTrackView {
	if h.Booking == nil {
		return nil
	}
	b, err := h.Booking.BookingByToken(r.Context(), token)
	if err != nil || b == nil {
		return nil
	}
	loc := app.BookingTimezone(b.Timezone)
	v := &bookingTrackView{
		When: b.StartAt.In(loc).Format("Mon 02 Jan, 15:04") +
			" – " + b.EndAt.In(loc).Format("15:04"),
		Status:      b.Status,
		ServiceName: b.ServiceName,
	}
	// Prefilled rebooking link (service unknown on no rows: no button).
	if b.ProductID != nil && orgSlug != "" {
		v.BookAgain = "/book/" + orgSlug + "?service=" + b.ProductID.String()
	}
	return v
}

func (h *PortalHandler) notFound(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Title   string
		OrgName string
	}{Title: "Order not found"}
	h.Renderer.PagePublic(w, http.StatusNotFound, "layouts/public.html", "portal/not_found.html", data)
}

// buildItemViews projects portal line items (with order-time snapshots)
// for the tracking page.
func buildItemViews(v *app.PortalView) []portalItemView {
	out := make([]portalItemView, 0, len(v.Items))
	for _, it := range v.Items {
		out = append(out, portalItemView{
			Description: it.Description,
			Quantity:    it.Quantity,
			UnitPrice:   it.UnitPrice,
			Subtotal:    it.Subtotal,
			Material:    it.Material,
			ImageRef:    it.ImageRef,
		})
	}
	return out
}

// --- Intake form ---

type intakePageData struct {
	Title      string
	OrgName    string
	Slug       string
	CSRFToken  string
	Error      string
	Form       intakeForm
	Products   []intakeProductView
	Categories []string // distinct sorted product categories for filtering
	Quantities map[string]string
	Answers    map[string]string // pq_<product>_<key> -> raw answer (error re-render)
	Preview    bool              // staff preview (?preview=1): banner only
	// Egg is the perishable-module block. Nil for normal businesses.
	Egg *eggIntakeData
}

// eggIntakeData carries the egg-module display block on the intake form.
type eggIntakeData struct {
	Accepting     bool // false -> "orders closed" replaces the form
	Cap           int
	LeftToday     string // crates left, pre-formatted ("67", "2.5", "0")
	LeftTomorrow  string
	HasBank       bool
	BankName      string
	AccountNumber string
	AccountName   string
	Whatsapp      string
	Currency      string
}

// buildEggData projects a PerishableConfig for the template. Nil unless the
// egg UI applies (perishable category + flag on).
func buildEggData(cfg *app.PerishableConfig) *eggIntakeData {
	if !cfg.EggApplies() {
		return nil
	}
	left := func(cap int, sold float64) string {
		if l := float64(cap) - sold; l > 0 {
			return app.FormatCrates(l)
		}
		return "0"
	}
	return &eggIntakeData{
		Accepting:     cfg.Accepting,
		Cap:           cfg.Cap,
		LeftToday:     left(cfg.Cap, cfg.SoldToday),
		LeftTomorrow:  left(cfg.Cap, cfg.SoldTomorrow),
		HasBank:       cfg.BankName != "" || cfg.AccountNumber != "" || cfg.AccountName != "",
		BankName:      cfg.BankName,
		AccountNumber: cfg.AccountNumber,
		AccountName:   cfg.AccountName,
		Whatsapp:      cfg.Whatsapp,
		Currency:      cfg.Currency,
	}
}

// productCategories returns distinct non-blank categories, sorted.
func productCategories(products []intakeProductView) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, p := range products {
		if p.Category == "" {
			continue
		}
		if _, dup := seen[p.Category]; dup {
			continue
		}
		seen[p.Category] = struct{}{}
		out = append(out, p.Category)
	}
	sort.Strings(out)
	return out
}

type intakeProductView struct {
	ID               string
	Name             string
	Description      string
	ShortDescription string
	Material         string
	Category         string
	PriceMinor       int64
	Currency         string
	QuoteOnly        bool
	StartingFrom     bool
	Availability     string
	CoverImageID     string // "" when none; served via /public/product-images/{id}
	Questions        []intakeQuestionView
	Specs            string
	Color            string
	ProductionDays   int
	ImageIDs         []string // gallery ids for the detail modal, cover first
}

type intakeQuestionView struct {
	Key      string
	Label    string
	Required bool
}

type intakeForm struct {
	CustomerName       string
	CustomerEmail      string
	CustomerPhone      string
	Description        string
	ExpectedCompletion string
	Budget             string
	// Egg extras (echo only; empty for normal intakes).
	DeliveryDay string // "today" | "tomorrow"
	AmountClaim string // claimed amount paid, raw input
}

// IntakeForm handles GET /order/{slug}.
func (h *PortalHandler) IntakeForm(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	org, err := h.Public.LookupOrg(r.Context(), slug)
	if err != nil {
		if errors.Is(err, app.ErrPublicOrgNotFound) {
			http.NotFound(w, r)
			return
		}
		slog.Error("portal: intake lookup failed", "err", err, "slug", slug)
		http.Error(w, "could not load business", http.StatusInternalServerError)
		return
	}

	data := intakePageData{
		Title:      "Place an order",
		OrgName:    org.Name,
		Slug:       slug,
		CSRFToken:  middleware.CSRFTokenFrom(r.Context()),
		Products:   h.intakeProducts(r, slug),
		Quantities: map[string]string{},
		Egg:        buildEggData(h.Public.EggConfig(r.Context(), slug)),
	}
	data.Categories = productCategories(data.Products)
	// Preview mode: staff opening ?preview=1 see exactly what customers
	// see, plus a banner. Requires a logged-in session; the public form
	// itself is unchanged.
	if r.URL.Query().Get("preview") == "1" {
		if s := middleware.SessionFromContext(r.Context()); s != nil && !s.Scope.IsZero() {
			data.Preview = true
		}
	}
	h.Renderer.PagePublic(w, http.StatusOK, "layouts/public.html", "portal/intake.html", data)
}

// intakeProducts loads the shop's visible catalog (best-effort: a catalog
// failure must never break the free-text form).
func (h *PortalHandler) intakeProducts(r *http.Request, slug string) []intakeProductView {
	list := h.Public.ListProducts(r.Context(), slug)
	out := make([]intakeProductView, 0, len(list))
	for _, p := range list {
		v := intakeProductView{
			ID: p.ID.String(), Name: p.Name, Description: p.Description,
			ShortDescription: p.ShortDescription, Material: p.Material,
			Category:   p.Category,
			PriceMinor: p.UnitPriceMinor, Currency: p.Currency,
			QuoteOnly: p.QuoteOnly, StartingFrom: p.StartingFrom,
			Availability: p.Availability,
			Specs:        p.Specs, Color: p.Color, ProductionDays: p.ProductionDays,
		}
		if p.CoverImageID != uuid.Nil {
			v.CoverImageID = p.CoverImageID.String()
		}
		for _, id := range p.ImageIDs {
			v.ImageIDs = append(v.ImageIDs, id.String())
		}
		for _, q := range p.Questions {
			v.Questions = append(v.Questions, intakeQuestionView{
				Key: q.Key, Label: q.Label, Required: q.Required,
			})
		}
		out = append(out, v)
	}
	return out
}

// parseIntakeItems reads qty_<productID> fields. Blank/zero quantities are
// skipped; the raw strings are echoed back for error re-renders.
func parseIntakeItems(r *http.Request) ([]app.PublicOrderItemInput, map[string]string) {
	var items []app.PublicOrderItemInput
	echo := map[string]string{}
	for key, vals := range r.PostForm {
		if !strings.HasPrefix(key, "qty_") || len(vals) == 0 {
			continue
		}
		raw := strings.TrimSpace(vals[0])
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(strings.TrimPrefix(key, "qty_"))
		if err != nil || id == uuid.Nil {
			continue
		}
		echo[id.String()] = raw
		scaled, err := parseQuantity(raw)
		if err != nil || scaled <= 0 {
			continue
		}
		items = append(items, app.PublicOrderItemInput{ProductID: id, QuantityScaled: scaled})
	}
	return items, echo
}

// parseIntakeAnswers reads pq_<productID>_<key> fields, resolving labels
// from the visible catalog. Only answers for picked products are kept;
// unknown products/keys are skipped; the service trims and caps the rest.
// The picked set may be nil (keeps everything, used pre-validation).
func parseIntakeAnswers(r *http.Request, products []intakeProductView, picked map[string]struct{}) ([]app.PublicAnswer, map[string]string) {
	byProduct := make(map[string]intakeProductView, len(products))
	for _, p := range products {
		byProduct[p.ID] = p
	}
	var out []app.PublicAnswer
	echo := map[string]string{}
	for key, vals := range r.PostForm {
		if !strings.HasPrefix(key, "pq_") || len(vals) == 0 {
			continue
		}
		raw := strings.TrimSpace(vals[0])
		if raw == "" {
			continue
		}
		rest := strings.TrimPrefix(key, "pq_")
		if len(rest) < 38 || rest[36] != '_' { // uuid + "_" + qkey
			continue
		}
		pid, qkey := rest[:36], rest[37:]
		prod, ok := byProduct[pid]
		if !ok || qkey == "" {
			continue
		}
		if _, err := uuid.Parse(pid); err != nil {
			continue
		}
		if picked != nil {
			if _, want := picked[pid]; !want {
				continue
			}
		}
		label := qkey
		for _, q := range prod.Questions {
			if q.Key == qkey {
				label = q.Label
				break
			}
		}
		echo[key] = raw
		out = append(out, app.PublicAnswer{Product: prod.Name, Q: label, A: raw})
	}
	return out, echo
}

// answerEcho re-reads raw answers for error re-renders (all products,
// since the quantities may themselves be the error).
func answerEcho(r *http.Request, products []intakeProductView) map[string]string {
	_, echo := parseIntakeAnswers(r, products, nil)
	return echo
}

// answeredKeys returns the set of "productID\x00qkey" with a non-blank
// answer, for required-question validation.
func answeredKeys(r *http.Request, products []intakeProductView, picked map[string]struct{}) map[string]struct{} {
	byProduct := make(map[string]intakeProductView, len(products))
	for _, p := range products {
		byProduct[p.ID] = p
	}
	out := map[string]struct{}{}
	for key, vals := range r.PostForm {
		if !strings.HasPrefix(key, "pq_") || len(vals) == 0 {
			continue
		}
		if strings.TrimSpace(vals[0]) == "" {
			continue
		}
		rest := strings.TrimPrefix(key, "pq_")
		if len(rest) < 38 || rest[36] != '_' {
			continue
		}
		pid, qkey := rest[:36], rest[37:]
		if _, ok := byProduct[pid]; !ok {
			continue
		}
		if picked != nil {
			if _, want := picked[pid]; !want {
				continue
			}
		}
		out[pid+"\x00"+qkey] = struct{}{}
	}
	return out
}

// missingRequiredAnswer returns an error message when a picked product has
// a required question with no answer.
func missingRequiredAnswer(products []intakeProductView, picked map[string]struct{}, answered map[string]struct{}) string {
	for _, p := range products {
		if _, want := picked[p.ID]; !want {
			continue
		}
		for _, q := range p.Questions {
			if q.Required {
				if _, ok := answered[p.ID+"\x00"+q.Key]; !ok {
					return "Please answer “" + q.Label + "” for " + p.Name + "."
				}
			}
		}
	}
	return ""
}

// IntakeSubmit handles POST /order/{slug}.
func (h *PortalHandler) IntakeSubmit(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	// Egg orgs accept a receipt photo, so they post multipart. Normal
	// orgs keep the urlencoded form.
	eggCfg := h.Public.EggConfig(r.Context(), slug)
	isEgg := eggCfg.EggApplies()
	if isEgg {
		r.Body = http.MaxBytesReader(nil, r.Body, attachment.MaxSizeBytes+1024*1024)
		if err := r.ParseMultipartForm(attachment.MaxSizeBytes + 1024*1024); err != nil {
			// Re-render below needs org + form; fall through to LookupOrg
			// first so the error shows on the branded page.
			org, lerr := h.Public.LookupOrg(r.Context(), slug)
			if lerr != nil {
				http.NotFound(w, r)
				return
			}
			h.intakeError(w, r, org, "That receipt photo is too large (max 10 MB).", app.PublicOrderInput{Slug: slug}, map[string]string{}, map[string]string{}, "", "")
			return
		}
	} else if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	org, err := h.Public.LookupOrg(r.Context(), slug)
	if err != nil {
		if errors.Is(err, app.ErrPublicOrgNotFound) {
			http.NotFound(w, r)
			return
		}
		slog.Error("portal: intake submit lookup failed", "err", err, "slug", slug)
		http.Error(w, "could not load business", http.StatusInternalServerError)
		return
	}

	blank := func(msg string) {
		h.intakeError(w, r, org, msg, app.PublicOrderInput{Slug: slug}, map[string]string{}, map[string]string{}, "today", "")
	}
	if isEgg && !eggCfg.Accepting {
		blank("Orders are closed for today. Please check back tomorrow.")
		return
	}

	in := app.PublicOrderInput{
		Slug:          slug,
		CustomerName:  formValue(r, "customer_name"),
		CustomerEmail: formValue(r, "customer_email"),
		CustomerPhone: formValue(r, "customer_phone"),
		Description:   formValue(r, "description"),
	}
	items, echo := parseIntakeItems(r)
	in.Items = items
	products := h.intakeProducts(r, slug)
	// Keep answers only for picked products (picked set from the quantities).
	picked := make(map[string]struct{}, len(items))
	for _, it := range items {
		picked[it.ProductID.String()] = struct{}{}
	}
	in.Answers, _ = parseIntakeAnswers(r, products, picked)
	answersEcho := answerEcho(r, products)
	answered := answeredKeys(r, products, picked)
	fail := func(msg string) {
		h.intakeError(w, r, org, msg, in, echo, answersEcho, formValue(r, "delivery_day"), formValue(r, "amount_claim"))
	}
	if msg := missingRequiredAnswer(products, picked, answered); msg != "" {
		fail(msg)
		return
	}

	// The receipt blob is written before Submit; on submit failure it is
	// deleted best-effort below (same orphan tradeoff as staff uploads).
	receiptKey := ""
	if isEgg {
		day, err := app.DeliveryDay(eggCfg.Timezone, formValue(r, "delivery_day"), time.Now())
		if err != nil {
			fail("Choose Today or Tomorrow for delivery.")
			return
		}
		in.ExpectedDate = &day
		in.Egg = &app.EggIntakeInput{DeliveryDate: day}
		if c := formValue(r, "amount_claim"); c != "" {
			minor, err := parseMoneyMinor(c)
			if err != nil {
				fail("That amount paid looks wrong. Enter numbers only, e.g. 55000.")
				return
			}
			in.Egg.AmountClaimMinor = &minor
		}
		receipt, err := h.readReceipt(r, eggCfg.OrgID)
		if err != nil {
			fail(humanizeReceiptError(err))
			return
		}
		in.Egg.ReceiptKey = receipt.Key
		in.Egg.ReceiptFilename = receipt.Filename
		in.Egg.ReceiptMime = receipt.Mime
		in.Egg.ReceiptSize = receipt.Size
		if err := h.Blobs.Put(r.Context(), receipt.Key, bytes.NewReader(receipt.Bytes), receipt.Size); err != nil {
			slog.Error("portal: receipt store failed", "err", err, "slug", slug)
			fail("Could not save your receipt photo. Please try again.")
			return
		}
		receiptKey = receipt.Key
	} else {
		if d := formValue(r, "expected_completion"); d != "" {
			t, err := parseDateInput(d)
			if err != nil {
				fail("Invalid completion date. Use day-month-year.")
				return
			}
			in.ExpectedDate = &t
		}
		if b := formValue(r, "budget"); b != "" {
			minor, err := parseMoneyMinor(b)
			if err != nil {
				fail("Invalid budget.")
				return
			}
			in.BudgetMinor = &minor
		}
	}

	result, err := h.Public.Submit(r.Context(), in)
	if err != nil {
		if receiptKey != "" {
			_ = h.Blobs.Delete(r.Context(), receiptKey)
		}
		fail(humanizePublicOrderError(err))
		return
	}

	data := struct {
		Title        string
		CustomerName string
		OrderNumber  string
		OrgName      string
		OrgPhone     string
		TotalMinor   int64
		Currency     string
	}{
		Title:        "Request received",
		CustomerName: result.CustomerName,
		OrderNumber:  result.OrderNumber,
		OrgName:      result.OrgName,
		OrgPhone:     result.OrgPhone,
		TotalMinor:   result.TotalMinor,
		Currency:     result.Currency,
	}
	h.Renderer.PagePublic(w, http.StatusOK, "layouts/public.html", "portal/success.html", data)
}

// receiptUpload is a validated, buffered receipt photo awaiting storage.
type receiptUpload struct {
	Key      string
	Filename string
	Mime     string
	Size     int64
	Bytes    []byte
}

// readReceipt parses the "receipt" file from an egg intake post and
// validates it through the attachment domain (type + size whitelist).
func (h *PortalHandler) readReceipt(r *http.Request, orgID uuid.UUID) (*receiptUpload, error) {
	file, header, err := r.FormFile("receipt")
	if err != nil {
		return nil, attachment.ErrFilenameRequired
	}
	defer file.Close()

	// Sniff MIME from the first 512 bytes (staff-upload pattern).
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	head = head[:n]
	sniffed := http.DetectContentType(head)
	full, err := io.ReadAll(io.MultiReader(bytes.NewReader(head), file))
	if err != nil {
		return nil, err
	}

	// Full metadata validation (MIME whitelist, 10 MiB cap, filename).
	if _, err := attachment.New(uuid.Nil, orgID, attachment.EntityOrder, uuid.Nil,
		"pending", header.Filename, sniffed, int64(len(full)), uuid.Nil, time.Now()); err != nil {
		return nil, err
	}
	now := time.Now()
	y, m, _ := now.Date()
	return &receiptUpload{
		Key:      fmt.Sprintf("%04d/%02d/%s", y, m, h.IDs.New().String()),
		Filename: header.Filename,
		Mime:     sniffed,
		Size:     int64(len(full)),
		Bytes:    full,
	}, nil
}

// humanizeReceiptError maps receipt upload failures to form messages.
func humanizeReceiptError(err error) string {
	switch {
	case errors.Is(err, attachment.ErrMimeNotAllowed):
		return "That receipt must be a photo or PDF (JPG, PNG, WEBP, GIF, PDF)."
	case errors.Is(err, attachment.ErrSizeTooLarge):
		return "That receipt photo is too large (max 10 MB)."
	case errors.Is(err, attachment.ErrSizeZero),
		errors.Is(err, attachment.ErrFilenameRequired):
		return "Please attach a photo of your payment receipt."
	default:
		return "Could not read that receipt photo. Please try another file."
	}
}

func (h *PortalHandler) intakeError(w http.ResponseWriter, r *http.Request, org *app.PublicOrg, msg string, form app.PublicOrderInput, echo map[string]string, answersEcho map[string]string, deliveryDay, amountClaim string) {
	products := h.intakeProducts(r, form.Slug)
	data := intakePageData{
		Title:      "Place an order",
		OrgName:    org.Name,
		Slug:       form.Slug,
		CSRFToken:  middleware.CSRFTokenFrom(r.Context()),
		Error:      msg,
		Products:   products,
		Categories: productCategories(products),
		Quantities: echo,
		Answers:    answersEcho,
		Egg:        buildEggData(h.Public.EggConfig(r.Context(), form.Slug)),
		Form: intakeForm{
			CustomerName:  form.CustomerName,
			CustomerEmail: form.CustomerEmail,
			CustomerPhone: form.CustomerPhone,
			Description:   form.Description,
			DeliveryDay:   deliveryDay,
			AmountClaim:   amountClaim,
		},
	}
	if form.ExpectedDate != nil {
		data.Form.ExpectedCompletion = form.ExpectedDate.Format("2006-01-02")
	}
	if form.BudgetMinor != nil {
		data.Form.Budget = formatMoneyMinor(*form.BudgetMinor)
	}
	h.Renderer.PagePublic(w, http.StatusBadRequest, "layouts/public.html", "portal/intake.html", data)
}

func humanizePublicOrderError(err error) string {
	var soldOut *app.EggSoldOutError
	switch {
	case errors.As(err, &soldOut):
		other := "tomorrow"
		if soldOut.DayLabel == "tomorrow" {
			other = "today"
		}
		return "Only " + app.FormatCrates(soldOut.Left) + " crates left for " + soldOut.DayLabel +
			". Please reduce quantity or choose " + other + "."
	case errors.Is(err, app.ErrEggClosed):
		return "Orders are closed for today. Please check back tomorrow."
	case errors.Is(err, app.ErrEggReceiptRequired):
		return "Please attach a photo of your payment receipt."
	case errors.Is(err, app.ErrEggNoItems):
		return "Pick at least one crate type and quantity."
	case errors.Is(err, app.ErrPublicNameRequired):
		return "Please enter your name."
	case errors.Is(err, app.ErrPublicEmailRequired):
		return "Please enter your email."
	case errors.Is(err, app.ErrPublicContactRequired):
		return "Please give an email or a phone/WhatsApp number so the shop can reach you."
	case errors.Is(err, app.ErrPublicDescRequired):
		return "Please tell us what you'd like to order."
	case errors.Is(err, app.ErrPublicOrderEmpty):
		return "Pick at least one product or tell us what you'd like to order."
	case errors.Is(err, app.ErrPublicTooManyItems):
		return "Too many items — please keep it to 25 or fewer."
	case errors.Is(err, app.ErrPublicQtyInvalid):
		return "One of the quantities looks wrong."
	case errors.Is(err, app.ErrPublicOrgNotFound):
		return "This business could not be found."
	default:
		return "Something went wrong. Please try again."
	}
}

// unused guard
var _ = strings.TrimSpace

// --- Token-bound file downloads ---

// ProofDownload handles GET /o/{token}/attachments/{id}: a customer-visible
// payment proof or order file. Authorization happens in
// get_portal_attachment (token, org, ownership, purpose); any failure is a
// 404 with no existence signal. Images and PDFs render inline;
// ?download=1 (and all other types) force a download.
func (h *PortalHandler) ProofDownload(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	id, err := uuid.Parse(r.PathValue("id"))
	if token == "" || err != nil || id == uuid.Nil {
		h.notFound(w, r)
		return
	}
	f, err := h.Portal.LoadAttachment(r.Context(), token, id)
	if err != nil {
		if errors.Is(err, app.ErrPortalFileUnavailable) {
			slog.Error("portal: proof bytes missing", "token_hash", app.HashTokenHex(token))
			http.Error(w, "file unavailable", http.StatusInternalServerError)
			return
		}
		h.notFound(w, r)
		return
	}
	defer f.Body.Close()
	servePortalFile(w, r, f, r.URL.Query().Get("download") == "1")
}

// ProductImage handles GET /public/product-images/{id}: public catalog
// imagery. Only images on active, visible products resolve; the response
// is cacheable (image ids are content-stable).
func (h *PortalHandler) ProductImage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	f, err := h.Portal.LoadProductImage(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Body.Close()
	w.Header().Set("Content-Type", f.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(f.SizeBytes, 10))
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, f.Body)
}

// servePortalFile streams a token-protected file with safe headers.
// Token-protected responses are never cached.
func servePortalFile(w http.ResponseWriter, r *http.Request, f *app.PortalFile, forceDownload bool) {
	w.Header().Set("Content-Type", f.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(f.SizeBytes, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	inline := !forceDownload &&
		(strings.HasPrefix(f.MimeType, "image/") || f.MimeType == "application/pdf")
	if inline {
		w.Header().Set("Content-Disposition", "inline")
	} else {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, f.Filename))
	}
	_, _ = io.Copy(w, f.Body)
}
