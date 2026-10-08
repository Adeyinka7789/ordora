package handlers

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// PortalHandler serves the public customer-facing pages.
type PortalHandler struct {
	Portal   *app.PortalService
	Public   *app.PublicOrderService
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
	}
	h.Renderer.PagePublic(w, http.StatusOK, "layouts/public.html", "portal/show.html", data)
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
	if err := r.ParseForm(); err != nil {
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
	if msg := missingRequiredAnswer(products, picked, answered); msg != "" {
		h.intakeError(w, r, org, msg, in, echo, answersEcho)
		return
	}

	if d := formValue(r, "expected_completion"); d != "" {
		t, err := parseDateInput(d)
		if err != nil {
			h.intakeError(w, r, org, "Invalid completion date. Use day-month-year.", in, echo, answersEcho)
			return
		}
		in.ExpectedDate = &t
	}
	if b := formValue(r, "budget"); b != "" {
		minor, err := parseMoneyMinor(b)
		if err != nil {
			h.intakeError(w, r, org, "Invalid budget.", in, echo, answersEcho)
			return
		}
		in.BudgetMinor = &minor
	}

	result, err := h.Public.Submit(r.Context(), in)
	if err != nil {
		h.intakeError(w, r, org, humanizePublicOrderError(err), in, echo, answersEcho)
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

func (h *PortalHandler) intakeError(w http.ResponseWriter, r *http.Request, org *app.PublicOrg, msg string, form app.PublicOrderInput, echo map[string]string, answersEcho map[string]string) {
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
		Form: intakeForm{
			CustomerName:  form.CustomerName,
			CustomerEmail: form.CustomerEmail,
			CustomerPhone: form.CustomerPhone,
			Description:   form.Description,
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
	switch {
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
