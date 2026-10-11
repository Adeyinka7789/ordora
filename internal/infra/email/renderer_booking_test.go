package email_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/Adeyinka7789/ordora/internal/infra/email"
)

// TestBookingTemplatesRender proves the booking mails render with the exact
// payload shapes the relay sends (the renderer errors on missing keys, so
// this guards the relay/service/template contract).
func TestBookingTemplatesRender(t *testing.T) {
	r, err := email.NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	msg, err := r.Render("booking_rescheduled", "Your appointment was moved", "c@example.com", map[string]any{
		"OrderID": "o1", "OrderNumber": "ORD-1", "CustomerName": "Ada",
		"CustomerEmail": "c@example.com", "StartLabel": "Mon 12 Jan, 09:00", "PortalURL": "/o/tok",
	})
	if err != nil {
		t.Fatalf("rescheduled: %v", err)
	}
	for _, want := range []string{"Mon 12 Jan, 09:00", "ORD-1", "/o/tok"} {
		if !strings.Contains(msg.HTML, want) || !strings.Contains(msg.Text, want) {
			t.Errorf("rescheduled mail missing %q", want)
		}
	}

	// The booking-submitted owner mail reuses intake_received: same keys.
	msg, err = r.Render("intake_received", "New order request", "o@example.com", map[string]any{
		"OrderID": "o1", "OrderNumber": "ORD-1", "CustomerName": "Ada",
		"CustomerEmail": "c@example.com", "CustomerPhone": "0803",
		"Description": "Booking: Braids — Mon 12 Jan, 09:00",
		"OrgName":     "Salon", "OrgEmail": "o@example.com",
		"AdminURL": "/dashboard/bookings?date=2026-10-12",
	})
	if err != nil {
		t.Fatalf("submitted owner mail: %v", err)
	}
	if !strings.Contains(msg.HTML, "Booking: Braids") {
		t.Error("submitted owner mail must carry the booking description")
	}
}

// TestTransactionalLinksAreAbsolute protects the class of regression where
// an email receives a bare "/orders/..." path. Email clients have no request
// origin, so every link rendered into a message must carry the configured app
// origin explicitly.
func TestTransactionalLinksAreAbsolute(t *testing.T) {
	r, err := email.NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	base := "https://app.admtechub.com"
	cases := []struct {
		name string
		data map[string]any
		urls []string
	}{
		{"welcome_verify", map[string]any{"Name": "Ada", "VerifyURL": base + "/verify?token=x", "OrgName": "Salon"}, []string{base + "/verify?token=x"}},
		{"password_reset", map[string]any{"Name": "Ada", "ResetURL": base + "/password/reset?token=x"}, []string{base + "/password/reset?token=x"}},
		{"order_created_customer", map[string]any{"OrgName": "Salon", "CustomerName": "Ada", "OrderNumber": "ORD-1", "Title": "Braids", "Currency": "NGN", "TotalMinor": int64(10000), "TotalMinorFormatted": "100.00", "ExpectedDate": nil, "PortalURL": base + "/o/o1_token"}, []string{base + "/o/o1_token"}},
		{"payment_received", map[string]any{"OrgName": "Salon", "CustomerName": "Ada", "OrderNumber": "ORD-1", "Currency": "NGN", "AmountMinorFormatted": "100.00", "PaidMinorFormatted": "100.00", "TotalMinorFormatted": "100.00", "BalanceMinorFormatted": "0.00", "PortalURL": base + "/o/o1_token"}, []string{base + "/o/o1_token"}},
		{"status_changed", map[string]any{"OrgName": "Salon", "CustomerName": "Ada", "OrderNumber": "ORD-1", "ToLabel": "Completed", "PortalURL": base + "/o/o1_token"}, []string{base + "/o/o1_token"}},
		{"order_ready", map[string]any{"OrgName": "Salon", "CustomerName": "Ada", "OrderNumber": "ORD-1", "PortalURL": base + "/o/o1_token"}, []string{base + "/o/o1_token"}},
		{"intake_received", map[string]any{"CustomerName": "Ada", "CustomerEmail": "ada@example.com", "CustomerPhone": "0803", "OrderNumber": "ORD-1", "Description": "Braids", "AdminURL": base + "/orders/id"}, []string{base + "/orders/id"}},
		{"booking_rescheduled", map[string]any{"CustomerName": "Ada", "OrderNumber": "ORD-1", "StartLabel": "Mon 12 Jan, 09:00", "PortalURL": base + "/o/o1_token"}, []string{base + "/o/o1_token"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := r.Render(tc.name, "Subject", "customer@example.com", tc.data)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			for _, want := range tc.urls {
				if !strings.Contains(msg.HTML, want) || !strings.Contains(msg.Text, want) {
					t.Fatalf("message missing absolute URL %q", want)
				}
				if parsed, err := url.Parse(want); err != nil || !parsed.IsAbs() || parsed.Scheme != "https" {
					t.Fatalf("test URL is not absolute HTTPS: %q", want)
				}
			}
		})
	}
}
