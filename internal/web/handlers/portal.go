package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

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

// buildItemViews is a stub: our PortalView doesn't include items yet,
// so the items section will render empty until we extend the projection.
func buildItemViews(v *app.PortalView) []portalItemView {
	return nil
}

// --- Intake form ---

type intakePageData struct {
	Title     string
	OrgName   string
	Slug      string
	CSRFToken string
	Error     string
	Form      intakeForm
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
		http.Error(w, "could not load business", http.StatusInternalServerError)
		return
	}

	data := intakePageData{
		Title:     "Place an order",
		OrgName:   org.Name,
		Slug:      slug,
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
	}
	h.Renderer.PagePublic(w, http.StatusOK, "layouts/public.html", "portal/intake.html", data)
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

	if d := formValue(r, "expected_completion"); d != "" {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			h.intakeError(w, r, org, "Invalid completion date.", in)
			return
		}
		in.ExpectedDate = &t
	}
	if b := formValue(r, "budget"); b != "" {
		minor, err := parseMoneyMinor(b)
		if err != nil {
			h.intakeError(w, r, org, "Invalid budget.", in)
			return
		}
		in.BudgetMinor = &minor
	}

	result, err := h.Public.Submit(r.Context(), in)
	if err != nil {
		h.intakeError(w, r, org, humanizePublicOrderError(err), in)
		return
	}

	data := struct {
		Title        string
		CustomerName string
		OrderNumber  string
		OrgName      string
		OrgPhone     string
	}{
		Title:        "Request received",
		CustomerName: result.CustomerName,
		OrderNumber:  result.OrderNumber,
		OrgName:      result.OrgName,
		OrgPhone:     result.OrgPhone,
	}
	h.Renderer.PagePublic(w, http.StatusOK, "layouts/public.html", "portal/success.html", data)
}

func (h *PortalHandler) intakeError(w http.ResponseWriter, r *http.Request, org *app.PublicOrg, msg string, form app.PublicOrderInput) {
	data := intakePageData{
		Title:     "Place an order",
		OrgName:   org.Name,
		Slug:      form.Slug,
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
		Error:     msg,
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
	case errors.Is(err, app.ErrPublicDescRequired):
		return "Please tell us what you'd like to order."
	case errors.Is(err, app.ErrPublicOrgNotFound):
		return "This business could not be found."
	default:
		return "Something went wrong. Please try again."
	}
}

// unused guard
var _ = strings.TrimSpace
