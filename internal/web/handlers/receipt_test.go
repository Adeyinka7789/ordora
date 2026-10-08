package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/payment"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// TestReceiptPrintRules guards the printout against regressions: the receipt
// must force block layout (flex gets cut across pages), keep backgrounds
// (browsers strip them by default), keep blocks together, and center the
// page without depending on screen utilities.
func TestReceiptPrintRules(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "static", "css", "receipt.css"))
	if err != nil {
		t.Fatalf("read receipt.css: %v", err)
	}
	css := string(raw)
	for _, want := range []string{
		"@media print",
		"print-color-adjust: exact",
		"break-inside: avoid",
		".rcpt-page",
		"margin: 0 auto",
		"display: block",
		"@page",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("receipt.css print block missing %q", want)
		}
	}

	// Both receipt pages must use the centered print wrapper.
	for _, file := range []string{"receipts/show.html", "portal/receipt.html"} {
		traw, err := os.ReadFile(filepath.Join("..", "templates", file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if !strings.Contains(string(traw), "rcpt-page") {
			t.Errorf("%s: page must use the rcpt-page print wrapper", file)
		}
	}
}

// TestReceiptImageShareCard guards the PNG contract: the export must build a
// fixed-size pure-SVG share card (SHARE_W/SHARE_H) from the card's data-*
// attributes — never serialize page HTML (foreignObject screenshots can't
// fit long receipts on a phone screen).
func TestReceiptImageShareCard(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "static", "js", "receipt.js"))
	if err != nil {
		t.Fatalf("read receipt.js: %v", err)
	}
	js := string(raw)
	for _, want := range []string{
		"SHARE_W",
		"SHARE_H",
		"shareSVG",
		"shareData",
		"node.dataset",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("receipt.js export missing %q (fixed-size share-card contract)", want)
		}
	}
	for _, forbidden := range []string{"foreignObject", "EXPORT_CARD_WIDTH", "cloneNode"} {
		if strings.Contains(js, forbidden) {
			t.Errorf("receipt.js must not contain %q (screenshot export abandoned)", forbidden)
		}
	}
}

// TestReceiptMethods ensures voided payments don't appear in the share-card
// methods line and duplicates collapse.
func TestReceiptMethods(t *testing.T) {
	got := receiptMethods([]receiptPayment{
		{MethodLabel: "Bank transfer"},
		{MethodLabel: "Cash"},
		{MethodLabel: "Card", IsReversed: true},
		{MethodLabel: "Cash"},
		{MethodLabel: "POS", IsReversal: true},
	})
	if got != "Bank transfer, Cash" {
		t.Errorf("receiptMethods = %q, want %q", got, "Bank transfer, Cash")
	}
	if got := receiptMethods(nil); got != "" {
		t.Errorf("receiptMethods(nil) = %q, want empty", got)
	}
}

// TestReceiptAllowed locks the availability rule: COMPLETED always has a
// receipt; otherwise only fully-paid (total > 0, balance <= 0) orders do.
func TestReceiptAllowed(t *testing.T) {
	cases := []struct {
		name    string
		status  order.Status
		total   int64
		balance int64
		want    bool
	}{
		{"completed unpaid still has receipt", order.StatusCompleted, 50000, 50000, true},
		{"completed paid", order.StatusCompleted, 50000, 0, true},
		{"delivered fully paid", order.StatusDelivered, 50000, 0, true},
		{"delivered partial", order.StatusDelivered, 50000, 10000, false},
		{"in progress fully paid", order.StatusInProgress, 20000, 0, true},
		{"new unpaid", order.StatusNew, 20000, 20000, false},
		{"zero total is not paid", order.StatusDelivered, 0, 0, false},
		{"cancelled even when paid", order.StatusCancelled, 50000, 0, false},
		{"cancelled unpaid", order.StatusCancelled, 50000, 50000, false},
	}
	for _, tc := range cases {
		if got := receiptAllowed(tc.status, tc.total, tc.balance); got != tc.want {
			t.Errorf("%s: receiptAllowed(%s, total=%d, balance=%d) = %v, want %v",
				tc.name, tc.status, tc.total, tc.balance, got, tc.want)
		}
	}
}

// TestReceiptShareText ensures the shared summary carries who (customer),
// what (order + items total), and payment (paid amount), plus the link only
// when the owner minted a client token.
func TestReceiptShareText(t *testing.T) {
	card := receiptCard{
		OrgName: "Ada's Cakes", OrgPhone: "08012345678",
		CustomerName: "Chidi", CustomerAddress: "12 Allen Ave",
		OrderNumber: "ORD-2026-00007", OrderTitle: "Birthday cake",
		Currency: "NGN", TotalMinor: 50000, PaidMinor: 50000,
	}
	withLink := receiptShareText(card, "https://x/o/abc/receipt")
	for _, want := range []string{"Chidi", "Ada's Cakes", "ORD-2026-00007", "Birthday cake", "NGN", "https://x/o/abc/receipt", "08012345678"} {
		if !strings.Contains(withLink, want) {
			t.Errorf("share text missing %q:\n%s", want, withLink)
		}
	}
	withoutLink := receiptShareText(card, "")
	if strings.Contains(withoutLink, "View / download receipt") {
		t.Errorf("share text without link must not advertise a link:\n%s", withoutLink)
	}
	for _, want := range []string{"Chidi", "ORD-2026-00007", "NGN"} {
		if !strings.Contains(withoutLink, want) {
			t.Errorf("link-less share text missing %q:\n%s", want, withoutLink)
		}
	}
}

// TestReceiptShareLinks checks mailto/WhatsApp URL shapes, including the
// no-email and no-phone fallbacks.
func TestReceiptShareLinks(t *testing.T) {
	m := receiptMailto("chidi@example.com", "Receipt ORD-1 from Ada", "Hello Chidi")
	if !strings.HasPrefix(m, "mailto:chidi@example.com?") {
		t.Errorf("mailto with address: got %q", m)
	}
	if !strings.Contains(m, "subject=Receipt+ORD-1") && !strings.Contains(m, "subject=Receipt%20ORD-1") {
		t.Errorf("mailto subject not encoded: %q", m)
	}
	m2 := receiptMailto("", "Hi", "Body here")
	if !strings.HasPrefix(m2, "mailto:?") {
		t.Errorf("mailto without address should use mailto:?: got %q", m2)
	}

	w := receiptWhatsApp("+234 801 234 5678", "Your receipt")
	if !strings.HasPrefix(w, "https://wa.me/2348012345678?") {
		t.Errorf("whatsapp digits not normalized: got %q", w)
	}
	if !strings.Contains(w, "text=Your+receipt") && !strings.Contains(w, "text=Your%20receipt") {
		t.Errorf("whatsapp text not encoded: %q", w)
	}
	w2 := receiptWhatsApp("", "Your receipt")
	if !strings.HasPrefix(w2, "https://wa.me/?") {
		t.Errorf("whatsapp without phone should open picker: got %q", w2)
	}
}

func testReceiptCard() receiptCard {
	now := time.Now()
	return receiptCard{
		OrgName: "Ada's Cakes", OrgEmail: "hello@adas.ng", OrgPhone: "08012345678", OrgAddress: "12 Allen Ave, Lagos",
		CustomerName: "Chidi", CustomerEmail: "chidi@example.com", CustomerPhone: "+234 801 234 5678", CustomerAddress: "5 Main St",
		OrderNumber: "ORD-2026-00007", OrderTitle: "Birthday cake",
		Status: "COMPLETED", StatusLabel: "Completed",
		CreatedAt: now, FulfilledAt: &now,
		Currency:      "NGN",
		SubtotalMinor: 50000, DiscountMinor: 0, TaxMinor: 0,
		TotalMinor: 50000, PaidMinor: 50000, BalanceMinor: 0,
		PaidPercent: 100, PayStatus: "PAID",
		Lines: []receiptLine{
			{Description: "Chocolate cake 10in", QuantityScaled: 1000, UnitPriceMinor: 40000, SubtotalMinor: 40000, Currency: "NGN"},
			{Description: "Delivery", QuantityScaled: 1000, UnitPriceMinor: 10000, SubtotalMinor: 10000, Currency: "NGN"},
		},
		Payments: []receiptPayment{
			{
				MethodLabel: "Bank transfer", Reference: "REF-123", PaidAt: now,
				AmountMinor: 30000, Currency: "NGN", Notes: "Deposit",
				Proofs: []receiptProof{{ID: uuid.NewString(), Filename: "transfer_receipt_30k.pdf", URL: "/attachments/" + uuid.NewString()}},
			},
			{
				MethodLabel: "Cash", PaidAt: now,
				AmountMinor: 20000, Currency: "NGN",
				Proofs: []receiptProof{{Filename: "teller-copy.jpg"}}, // public style: text only
			},
			{
				MethodLabel: "Card", Reference: "VOID", PaidAt: now,
				AmountMinor: 5000, Currency: "NGN", IsReversed: true,
			},
		},
		ReceiptURL: "https://example.com/o/abc/receipt", GeneratedAt: now,
	}
}

// TestReceiptTemplatesExecute renders every receipt template with realistic
// data, including proof links (staff), proof text (public), reversed flags,
// and nil FulfilledAt. Template/data mismatches only fail at execution time,
// so this is the regression net for "receipt page 500s".
func TestReceiptTemplatesExecute(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	card := testReceiptCard()
	card.Methods = receiptMethods(card.Payments)
	// Share-card data attributes feed receipt.js: all present, voided Card
	// excluded from the methods line.

	staff := receiptShowPage{
		Title: "Receipt ORD-2026-00007", CSRFToken: "csrf",
		OrderID: uuid.NewString(), Card: card,
		MailtoURL: "mailto:x", WhatsAppURL: "https://wa.me/x",
		ClientLink: "https://example.com/o/abc/receipt", HasToken: true,
	}
	out, err := r.Raw("receipts/show.html", staff)
	if err != nil {
		t.Fatalf("receipts/show.html: %v", err)
	}
	for _, want := range []string{
		"ORD-2026-00007", "Chidi", "5 Main St", "Ada&#39;s Cakes", "12 Allen Ave",
		"Chocolate cake 10in", "Bank transfer", "REF-123",
		"transfer_receipt_30k.pdf", "/attachments/", "REVERSED",
		"Print / PDF", "Save image", "WhatsApp", "Copy client link",
		`data-org="Ada&#39;s Cakes"`, `data-amount="NGN 500.00"`,
		`data-order="ORD-2026-00007"`, `data-customer="Chidi"`,
		`data-methods="Bank transfer, Cash"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("receipts/show.html missing %q", want)
		}
	}

	// No-token variant renders the mint-link form instead.
	staff.HasToken = false
	staff.ClientLink = ""
	out, err = r.Raw("receipts/show.html", staff)
	if err != nil {
		t.Fatalf("receipts/show.html (no token): %v", err)
	}
	if !strings.Contains(out, "Generate client link") {
		t.Error("receipts/show.html without token should offer Generate client link")
	}

	portal := portalReceiptPage{Title: "Receipt ORD-2026-00007", OrgName: "Ada's Cakes", Card: card}
	out, err = r.Raw("portal/receipt.html", portal)
	if err != nil {
		t.Fatalf("portal/receipt.html: %v", err)
	}
	for _, want := range []string{"ORD-2026-00007", "Your receipt", "Print / PDF", "teller-copy.jpg"} {
		if !strings.Contains(out, want) {
			t.Errorf("portal/receipt.html missing %q", want)
		}
	}
}

// TestOrderShowReceiptButton ensures the Receipt entry point appears for a
// completed (or fully-paid) order and the condition doesn't break the page.
func TestOrderShowReceiptButton(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	total, _ := money.New(50000, "NGN")
	paid, _ := money.New(50000, "NGN")
	zero, _ := money.New(0, "NGN")
	now := time.Now()
	oid := uuid.New()
	mkOrder := func(status order.Status, p money.Money) *order.Order {
		return &order.Order{
			ID: oid, Number: "ORD-2026-00007", Title: "Cake",
			Status: status, Currency: "NGN",
			Subtotal: total, Total: total, Paid: p, Discount: zero, Tax: zero,
			CreatedAt: now, DeliveredAt: &now,
		}
	}
	mkPage := func(o *order.Order) orderShowPage {
		return orderShowPage{
			Title: o.Number, Order: o, OrderID: oid,
			Balance:     o.Balance().Amount(),
			PayStatus:   payment.DeriveStatus(o.Total, o.Paid),
			PaidPercent: 100,
			Customer:    &customer.Customer{Name: "Chidi", Address: "5 Main St"},
		}
	}

	done := mkPage(mkOrder(order.StatusCompleted, paid))
	out, err := r.Raw("orders/show.html", done)
	if err != nil {
		t.Fatalf("orders/show.html (completed): %v", err)
	}
	if want := "/orders/" + oid.String() + "/receipt"; !strings.Contains(out, want) {
		t.Errorf("completed order page should link %q", want)
	}

	partial := mkPage(mkOrder(order.StatusInProgress, zero))
	partial.PaidPercent = 0
	partial.PayStatus = payment.DeriveStatus(partial.Order.Total, partial.Order.Paid)
	out, err = r.Raw("orders/show.html", partial)
	if err != nil {
		t.Fatalf("orders/show.html (in progress): %v", err)
	}
	if strings.Contains(out, "/receipt") {
		t.Error("partially-paid order page must not link a receipt")
	}
}

// TestPortalShowReceiptButton ensures the public portal exposes the receipt
// link (with token) once the order is completed/paid.
func TestPortalShowReceiptButton(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	total, _ := money.New(50000, "NGN")
	paid, _ := money.New(50000, "NGN")
	bal, _ := money.New(0, "NGN")
	view := &app.PortalView{
		OrderNumber: "ORD-2026-00007", Title: "Cake",
		Status: order.StatusCompleted, StatusLabel: "Completed",
		Currency: "NGN", Total: total, Paid: paid, Balance: bal,
		PayStatus: payment.Status("PAID"), PaidPercent: 100,
		CustomerName: "Chidi", OrgName: "Ada's Cakes",
	}
	data := portalPageData{
		Title: "ORD-2026-00007", OrgName: "Ada's Cakes", Token: "tok123",
		View: view, OrderForStepper: &order.Order{Status: order.StatusCompleted},
	}
	out, err := r.Raw("portal/show.html", data)
	if err != nil {
		t.Fatalf("portal/show.html: %v", err)
	}
	if !strings.Contains(out, "/o/tok123/receipt") {
		t.Error("completed portal page should link /o/tok123/receipt")
	}
}
