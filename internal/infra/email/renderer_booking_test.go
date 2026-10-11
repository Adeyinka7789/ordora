package email_test

import (
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
