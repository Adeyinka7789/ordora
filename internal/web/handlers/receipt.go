package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/attachment"
	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/payment"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// -----------------------------------------------------------------------------
// Shared receipt model
// -----------------------------------------------------------------------------
//
// Both receipt pages (staff + public portal) render the same OPay-style card
// (receipts/_card.html) from this normalized shape. Normalizing in Go keeps
// the template free of domain specifics and makes the share-text builders
// testable without a database.

// receiptLine is one "what they did" row: description + quantity + prices.
type receiptLine struct {
	Description    string
	QuantityScaled int64 // x1000, rendered with formatQty
	UnitPriceMinor int64
	SubtotalMinor  int64
	Currency       string
}

// receiptProof is one already-uploaded payment-proof file. ID is empty on
// the public receipt (downloads stay behind staff auth), in which case the
// template renders the filename as text instead of a link.
type receiptProof struct {
	ID       string
	Filename string
}

// receiptPayment is one "what they paid" row.
type receiptPayment struct {
	MethodLabel string
	Reference   string
	PaidAt      time.Time
	AmountMinor int64
	Currency    string
	Notes       string
	IsReversed  bool
	IsReversal  bool
	Proofs      []receiptProof
}

// receiptCard is the full data for receipts/_card.html.
type receiptCard struct {
	OrgName    string
	OrgEmail   string
	OrgPhone   string
	OrgAddress string

	CustomerName    string
	CustomerEmail   string
	CustomerPhone   string
	CustomerAddress string

	OrderNumber string
	OrderTitle  string
	Status      string
	StatusLabel string
	CreatedAt   time.Time
	FulfilledAt *time.Time

	Currency      string
	SubtotalMinor int64
	DiscountMinor int64
	TaxMinor      int64
	TotalMinor    int64
	PaidMinor     int64
	BalanceMinor  int64
	PaidPercent   int
	PayStatus     string

	Lines    []receiptLine
	Payments []receiptPayment
	// Methods summarizes non-voided payment methods for the share card.
	Methods string

	ReceiptURL  string
	GeneratedAt time.Time
}

// receiptAllowed reports whether a receipt may be shown/shared: the order is
// COMPLETED, or it is fully paid (total > 0 and nothing outstanding).
// Cancelled and partially-paid orders have no receipt.
func receiptAllowed(status order.Status, totalMinor, balanceMinor int64) bool {
	if status == order.StatusCompleted {
		return true
	}
	if status == order.StatusCancelled {
		return false
	}
	return totalMinor > 0 && balanceMinor <= 0
}

// receiptShareText builds the plain-text summary shared via Email/WhatsApp.
// It carries who, what, and what was paid — the link (when present) lets the
// customer open the full receipt with print/PDF/PNG download.
func receiptShareText(card receiptCard, link string) string {
	var b strings.Builder
	b.WriteString("Hello " + card.CustomerName + ",\n")
	b.WriteString("Your receipt from " + card.OrgName + ":\n")
	b.WriteString("Order " + card.OrderNumber)
	if card.OrderTitle != "" {
		b.WriteString(" — " + card.OrderTitle)
	}
	b.WriteString("\nTotal: " + card.Currency + " " + formatMoneyMinor(card.TotalMinor))
	b.WriteString("\nPaid: " + card.Currency + " " + formatMoneyMinor(card.PaidMinor))
	if link != "" {
		b.WriteString("\nView / download receipt: " + link)
	}
	if card.OrgPhone != "" {
		b.WriteString("\n" + card.OrgName + ": " + card.OrgPhone)
	}
	return b.String()
}

// receiptMethods summarizes the payment methods that actually moved money:
// reversed and reversal rows are excluded, duplicates collapsed.
func receiptMethods(payments []receiptPayment) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range payments {
		if p.IsReversed || p.IsReversal {
			continue
		}
		label := p.MethodLabel
		if label == "" {
			label = "Other"
		}
		if !seen[label] {
			seen[label] = true
			out = append(out, label)
		}
	}
	return strings.Join(out, ", ")
}
func receiptMailto(to, subject, body string) string {
	v := url.Values{}
	v.Set("subject", subject)
	v.Set("body", body)
	if strings.TrimSpace(to) == "" {
		return "mailto:?" + v.Encode()
	}
	return "mailto:" + to + "?" + v.Encode()
}

// receiptWhatsApp builds a wa.me share link. Digits are extracted from the
// phone number; with no number it opens the generic share picker.
func receiptWhatsApp(phone, text string) string {
	digits := strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, phone)
	v := url.Values{}
	v.Set("text", text)
	if digits == "" {
		return "https://wa.me/?" + v.Encode()
	}
	return "https://wa.me/" + digits + "?" + v.Encode()
}

// absoluteURL rebuilds an absolute URL for sharing (receipts are shared
// outside the app, so relative links are useless).
func absoluteURL(r *http.Request, path string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if h := r.Header.Get("X-Forwarded-Proto"); h != "" {
		scheme = strings.ToLower(strings.TrimSpace(strings.Split(h, ",")[0]))
	}
	return scheme + "://" + r.Host + path
}

// -----------------------------------------------------------------------------
// Staff receipt: GET /orders/{id}/receipt
// -----------------------------------------------------------------------------

type receiptShowPage struct {
	Title       string
	CSRFToken   string
	OrderID     string
	Card        receiptCard
	MailtoURL   string
	WhatsAppURL string
	// ClientLink is the public receipt URL when a ?token= is present.
	ClientLink string
	HasToken   bool
}

func (h *OrderHandler) Receipt(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	oid, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	d, err := h.loadReceiptDetail(r.Context(), scope, oid)
	if err != nil {
		if errors.Is(err, order.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load order", http.StatusInternalServerError)
		return
	}

	totalMinor := d.Order.Total.Amount()
	balanceMinor := d.Order.Balance().Amount()
	if !receiptAllowed(d.Order.Status, totalMinor, balanceMinor) {
		http.Redirect(w, r, "/orders/"+oid.String()+"?error=Receipt+is+available+once+the+order+is+completed+or+fully+paid.", http.StatusSeeOther)
		return
	}

	card := staffReceiptCard(r, d, "")
	page := receiptShowPage{
		Title:     "Receipt " + d.Order.Number,
		CSRFToken: csrfFromCtx(r),
		OrderID:   oid.String(),
		Card:      card,
	}
	// Public client link (for Email/WhatsApp) when the staff minted one via
	// the "Client link" button (?token=...). Without it, share text carries
	// the order summary and the owner forwards the downloaded PDF/PNG.
	if raw := queryValue(r, "token"); raw != "" {
		clientURL := absoluteURL(r, "/o/"+raw+"/receipt")
		page.ClientLink = clientURL
		page.HasToken = true
		page.Card.ReceiptURL = clientURL
		summary := receiptShareText(card, clientURL)
		page.MailtoURL = receiptMailto(d.customerEmail(), "Receipt "+d.Order.Number+" from "+d.Org.Name, summary)
		page.WhatsAppURL = receiptWhatsApp(d.customerPhone(), summary)
	} else {
		page.Card.ReceiptURL = absoluteURL(r, "/orders/"+oid.String()+"/receipt")
		summary := receiptShareText(card, "")
		page.MailtoURL = receiptMailto(d.customerEmail(), "Receipt "+d.Order.Number+" from "+d.Org.Name, summary)
		page.WhatsAppURL = receiptWhatsApp(d.customerPhone(), summary)
	}

	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "receipts/show.html", page)
}

// receiptDetail bundles everything the staff receipt needs.
type receiptDetail struct {
	Order           *order.Order
	Customer        *customer.Customer
	Org             orgView
	Payments        []*payment.Payment
	ProofsByPayment map[uuid.UUID][]*attachment.Attachment
}

type orgView struct {
	Name    string
	Email   string
	Phone   string
	Address string
}

func (d *receiptDetail) customerEmail() string {
	if d.Customer == nil {
		return ""
	}
	return d.Customer.Email
}

func (d *receiptDetail) customerPhone() string {
	if d.Customer == nil {
		return ""
	}
	return d.Customer.Phone
}

// loadReceiptDetail loads order + customer (tolerates deleted) + org contact
// + payments + per-payment proof uploads (the already-built staff-side
// receipt attachments, shown per payment on the receipt).
func (h *OrderHandler) loadReceiptDetail(ctx context.Context, scope tenant.TenantScope, oid uuid.UUID) (*receiptDetail, error) {
	o, err := h.Service.GetOrder(ctx, scope, oid)
	if err != nil {
		return nil, err
	}

	d := &receiptDetail{Order: o, ProofsByPayment: map[uuid.UUID][]*attachment.Attachment{}}

	if cust, err := h.CustRepo.GetByID(ctx, scope, o.CustomerID); err == nil {
		d.Customer = cust
	} else if !errors.Is(err, customer.ErrNotFound) {
		return nil, err
	}

	if h.Orgs != nil {
		if org, err := h.Orgs.GetByID(ctx, scope.OrgID); err == nil {
			d.Org = orgView{Name: org.Name, Email: org.Email, Phone: org.Phone, Address: org.Address}
		}
	}

	if list, err := h.Payments.ListForOrder(ctx, scope, o.ID); err == nil {
		d.Payments = list
		ids := make([]uuid.UUID, 0, len(list))
		for _, p := range list {
			ids = append(ids, p.ID)
		}
		if grouped, err := h.Attachments.ListForEntities(ctx, scope, attachment.EntityPayment, ids); err == nil {
			d.ProofsByPayment = grouped
		}
	}
	return d, nil
}

func staffReceiptCard(r *http.Request, d *receiptDetail, _ string) receiptCard {
	o := d.Order
	card := receiptCard{
		OrgName:    d.Org.Name,
		OrgEmail:   d.Org.Email,
		OrgPhone:   d.Org.Phone,
		OrgAddress: d.Org.Address,

		OrderNumber: o.Number,
		OrderTitle:  o.Title,
		Status:      string(o.Status),
		StatusLabel: o.Status.Label(),
		CreatedAt:   o.CreatedAt,
		FulfilledAt: o.DeliveredAt,

		Currency:      o.Currency,
		SubtotalMinor: o.Subtotal.Amount(),
		DiscountMinor: o.Discount.Amount(),
		TaxMinor:      o.Tax.Amount(),
		TotalMinor:    o.Total.Amount(),
		PaidMinor:     o.Paid.Amount(),
		BalanceMinor:  o.Balance().Amount(),
		PayStatus:     string(payment.DeriveStatus(o.Total, o.Paid)),
		GeneratedAt:   time.Now(),
	}
	if d.Customer != nil {
		card.CustomerName = d.Customer.Name
		card.CustomerEmail = d.Customer.Email
		card.CustomerPhone = d.Customer.Phone
		card.CustomerAddress = d.Customer.Address
	}
	if o.Total.Amount() > 0 {
		card.PaidPercent = int(o.Paid.Amount() * 100 / o.Total.Amount())
	}
	for _, it := range o.Items {
		card.Lines = append(card.Lines, receiptLine{
			Description:    it.Description,
			QuantityScaled: it.Quantity,
			UnitPriceMinor: it.UnitPrice.Amount(),
			SubtotalMinor:  it.Subtotal.Amount(),
			Currency:       it.UnitPrice.Currency(),
		})
	}
	for _, p := range d.Payments {
		rp := receiptPayment{
			MethodLabel: p.Method.Label(),
			Reference:   p.Reference,
			PaidAt:      p.PaidAt,
			AmountMinor: p.Amount.Amount(),
			Currency:    p.Amount.Currency(),
			Notes:       p.Notes,
			IsReversed:  p.IsReversed(),
			IsReversal:  p.IsReversal(),
		}
		for _, a := range d.ProofsByPayment[p.ID] {
			rp.Proofs = append(rp.Proofs, receiptProof{ID: a.ID.String(), Filename: a.Filename})
		}
		card.Payments = append(card.Payments, rp)
	}
	card.Methods = receiptMethods(card.Payments)
	_ = r
	return card
}

// -----------------------------------------------------------------------------
// Public receipt: GET /o/{token}/receipt
// -----------------------------------------------------------------------------
//
// Same card, public layout. Available under the same token rules as the
// portal itself, plus the receipt gate (COMPLETED or fully paid) so an
// in-progress order's receipt can't be opened early. Proof downloads stay
// staff-only: the card lists proof filenames as text.

type portalReceiptPage struct {
	Title   string
	OrgName string
	Card    receiptCard
}

// Receipt handles GET /o/{token}/receipt.
func (h *PortalHandler) Receipt(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "could not load order", http.StatusInternalServerError)
		return
	}
	if !receiptAllowed(v.Status, v.Total.Amount(), v.Balance.Amount()) {
		h.notFound(w, r)
		return
	}

	card := portalReceiptCard(v)
	card.ReceiptURL = absoluteURL(r, "/o/"+token+"/receipt")
	data := portalReceiptPage{
		Title:   "Receipt " + v.OrderNumber,
		OrgName: v.OrgName,
		Card:    card,
	}
	h.Renderer.PagePublic(w, http.StatusOK, "layouts/public.html", "portal/receipt.html", data)
}

func portalReceiptCard(v *app.PortalView) receiptCard {
	card := receiptCard{
		OrgName:    v.OrgName,
		OrgEmail:   v.OrgEmail,
		OrgPhone:   v.OrgPhone,
		OrgAddress: v.OrgAddress,

		CustomerName:    v.CustomerName,
		CustomerEmail:   v.CustomerEmail,
		CustomerPhone:   v.CustomerPhone,
		CustomerAddress: v.CustomerAddress,

		OrderNumber: v.OrderNumber,
		OrderTitle:  v.Title,
		Status:      string(v.Status),
		StatusLabel: v.StatusLabel,
		CreatedAt:   v.CreatedAt,
		FulfilledAt: v.DeliveredAt,

		Currency:      v.Currency,
		SubtotalMinor: v.Subtotal.Amount(),
		DiscountMinor: v.Discount.Amount(),
		TaxMinor:      v.Tax.Amount(),
		TotalMinor:    v.Total.Amount(),
		PaidMinor:     v.Paid.Amount(),
		BalanceMinor:  v.Balance.Amount(),
		PaidPercent:   v.PaidPercent,
		PayStatus:     string(v.PayStatus),
		GeneratedAt:   time.Now(),
	}
	for _, it := range v.Items {
		card.Lines = append(card.Lines, receiptLine{
			Description:    it.Description,
			QuantityScaled: it.Quantity,
			UnitPriceMinor: it.UnitPrice.Amount(),
			SubtotalMinor:  it.Subtotal.Amount(),
			Currency:       it.UnitPrice.Currency(),
		})
	}
	for _, p := range v.Payments {
		rp := receiptPayment{
			MethodLabel: p.MethodLabel,
			Reference:   p.Reference,
			PaidAt:      p.PaidAt,
			AmountMinor: p.Amount.Amount(),
			Currency:    p.Amount.Currency(),
			Notes:       p.Notes,
			IsReversed:  p.IsReversed,
			IsReversal:  p.IsReversal,
		}
		// No download IDs on the public receipt: filenames as text only.
		for _, name := range strings.Split(p.ProofNames, ",") {
			if name = strings.TrimSpace(name); name != "" {
				rp.Proofs = append(rp.Proofs, receiptProof{Filename: name})
			}
		}
		card.Payments = append(card.Payments, rp)
	}
	card.Methods = receiptMethods(card.Payments)
	return card
}
