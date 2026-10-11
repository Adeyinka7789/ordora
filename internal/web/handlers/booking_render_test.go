package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/booking"
	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// TestBookFormRenders guards the public booking form: services, day
// bounds, slots, details, and the no-services fallback.
func TestBookFormRenders(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("book/form.html", bookFormData{
		Title: "Book an appointment", OrgName: "Glam Salon", Slug: "glam",
		Services: []bookServiceView{
			{ID: uuid.NewString(), Name: "Braids", PriceMinor: 15000, Currency: "NGN", DurationMin: 120, DurationLabel: "2 hr"},
			{ID: uuid.NewString(), Name: "Consult", PriceMinor: 0, Currency: "NGN"},
		},
		Staff: []bookStaffView{
			{ID: uuid.NewString(), Name: "Ada"},
			{ID: uuid.NewString(), Name: "Bola"},
		},
		Date: "2026-10-13", MinDate: "2026-10-12", MaxDate: "2026-11-11",
		Slots: []bookSlotView{
			{Value: "09:00", Label: "09:00 – 10:00"},
			{Value: "09:30", Label: "09:30 – 10:30"},
		},
		CancelPolicy: "24h notice",
	})
	if err != nil {
		t.Fatalf("book/form.html: %v", err)
	}
	for _, want := range []string{
		"Book with Glam Salon", "Braids", "Consult", "(2 hr)",
		`name="service"`, `name="staff"`, "Anyone available", "Ada", "Bola",
		`name="date"`, `name="start"`,
		`value="09:00"`, "09:00 – 10:00",
		`name="name"`, `name="phone"`, `name="email"`, `name="notes"`,
		"24h notice", "Request booking",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("book form missing %q", want)
		}
	}

	// No services: contact fallback, no slots, no submit.
	out, err = r.Raw("book/form.html", bookFormData{
		Title: "Book an appointment", OrgName: "Glam Salon", Slug: "glam",
	})
	if err != nil {
		t.Fatalf("book/form.html (no services): %v", err)
	}
	if !strings.Contains(out, "isn't set up here yet") {
		t.Error("service-less form must show the contact fallback")
	}
	if strings.Contains(out, "Request booking") {
		t.Error("service-less form must not offer submit")
	}
}

// TestBookConfirmationRenders guards the post-submit page incl. tracking.
func TestBookConfirmationRenders(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("book/confirmation.html", struct {
		Title        string
		CustomerName string
		OrderNumber  string
		OrgName      string
		Start        string
		End          string
		Token        string
		TotalMinor   int64
		Currency     string
	}{
		Title: "Booking received", CustomerName: "Ada",
		OrderNumber: "ORD-1", OrgName: "Glam Salon",
		Start: "Mon 12 Jan, 09:00", End: "10:00",
		Token: "tok123", TotalMinor: 15000, Currency: "NGN",
	})
	if err != nil {
		t.Fatalf("book/confirmation.html: %v", err)
	}
	for _, want := range []string{
		"Booking received", "Ada", "ORD-1", "Mon 12 Jan, 09:00",
		"/o/tok123", "Track your booking",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("confirmation missing %q", want)
		}
	}
}

// TestBookingsPanelRenders guards the staff day view.
func TestBookingsPanelRenders(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("bookings/index.html", bookingsPage{
		Title: "Bookings", Date: "2026-10-12",
		PrevDate: "2026-10-11", NextDate: "2026-10-13", TodayDate: "2026-10-12",
		Rows: []bookingRow{
			{ID: uuid.NewString(), OrderNumber: "ORD-1", CustomerName: "Ada",
				CustomerPhone: "0803", ServiceName: "Braids", StaffName: "Bola",
				StartLabel: "Mon 12 Jan, 09:00", EndLabel: "10:00", DurationMin: 60,
				Status: "REQUESTED", TotalMinor: 15000, Currency: "NGN",
				IsRequested: true, IsLive: true},
			{ID: uuid.NewString(), OrderNumber: "ORD-2", CustomerName: "Bola",
				StartLabel: "Mon 12 Jan, 10:00", EndLabel: "11:00", DurationMin: 60,
				Status: "CONFIRMED", Currency: "NGN", IsConfirmed: true, IsLive: true},
		},
		Total: 2, Requested: 1, IsOwner: true,
	})
	if err != nil {
		t.Fatalf("bookings/index.html: %v", err)
	}
	for _, want := range []string{
		"Ada", "ORD-1", "Braids", "with Bola", "Mon 12 Jan, 09:00",
		"Confirm", "Complete", "No-show", "Cancel", "Move",
		"new_date", "new_time", "reason",
		"2 visits", "1 awaiting confirmation",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("bookings panel missing %q", want)
		}
	}

	out, err = r.Raw("bookings/index.html", bookingsPage{
		Title: "Bookings", Date: "2026-10-12", TodayDate: "2026-10-12",
	})
	if err != nil {
		t.Fatalf("bookings/index.html (empty): %v", err)
	}
	if !strings.Contains(out, "No visits this day") {
		t.Error("empty day must show the empty state")
	}
}

// TestBookingSettingsSection renders the booking card on /settings.
func TestBookingSettingsSection(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("settings/index.html", settingsPage{
		Title: "Settings", FormName: "Glam Salon", FormSlug: "glam",
		Categories: org.BusinessCategories(), Types: org.BusinessTypes(),
		TeamSizes: org.TeamSizes(), Referrals: org.ReferralSources(),
		TeamIsOwner: true,
		Booking: &bookingSettingsData{
			Accepting: true, DurationMin: 60, BufBefore: 10, BufAfter: 15,
			MinNoticeHours: 2, MaxAdvanceDays: 30, CancelPolicy: "24h",
			Hours: []bookingHourRow{
				{Dow: 1, DayName: "Monday", Open: "09:00", Close: "17:00", IsOpen: true},
			},
			Blackouts: []bookingBlackoutRow{{Day: "2026-12-25", Reason: "Christmas"}},
		},
	})
	if err != nil {
		t.Fatalf("settings/index.html (booking): %v", err)
	}
	for _, want := range []string{
		"Booking", `name="duration_min"`, `name="buf_before"`,
		`name="min_notice_hours"`, `name="max_advance_days"`,
		`name="cancel_policy"`, "/settings/booking",
		`name="open_1"`, `name="close_1"`, `name="closed_1"`,
		"2026-12-25", "Christmas", "/settings/booking/blackouts/2026-12-25/remove",
		"Save booking settings",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("settings booking section missing %q", want)
		}
	}
}

// fakeBookingStore serves the QR tests: only PublicConfig matters.
type fakeBookingStore struct {
	cfg *app.BookingConfig
}

func (f *fakeBookingStore) GetSettings(context.Context, tenant.TenantScope) (*app.BookingSettings, error) {
	return &app.BookingSettings{}, nil
}
func (f *fakeBookingStore) UpsertSettings(context.Context, tenant.TenantScope, *app.BookingSettings) error {
	return nil
}
func (f *fakeBookingStore) SetHours(context.Context, tenant.TenantScope, []app.BookingHour) error {
	return nil
}
func (f *fakeBookingStore) ListHours(context.Context, tenant.TenantScope) ([]app.BookingHour, error) {
	return nil, nil
}
func (f *fakeBookingStore) AddBlackout(context.Context, tenant.TenantScope, time.Time, string, uuid.UUID, time.Time) error {
	return nil
}
func (f *fakeBookingStore) RemoveBlackout(context.Context, tenant.TenantScope, time.Time) error {
	return nil
}
func (f *fakeBookingStore) ListBlackouts(context.Context, tenant.TenantScope, time.Time, time.Time) ([]app.BookingBlackout, error) {
	return nil, nil
}
func (f *fakeBookingStore) GetBooking(context.Context, tenant.TenantScope, uuid.UUID) (*app.Booking, error) {
	return nil, app.ErrBookingNotFound
}
func (f *fakeBookingStore) ListDayBookings(context.Context, tenant.TenantScope, time.Time, time.Time) ([]app.Booking, error) {
	return nil, nil
}
func (f *fakeBookingStore) UpdateBookingStatus(context.Context, tenant.TenantScope, uuid.UUID, booking.Status, booking.Status, string, time.Time) error {
	return nil
}
func (f *fakeBookingStore) Reschedule(context.Context, tenant.TenantScope, uuid.UUID, time.Time, time.Time, time.Time) (*app.Booking, error) {
	return nil, app.ErrBookingNotFound
}
func (f *fakeBookingStore) BookingConfig(context.Context, string) (*app.BookingConfig, error) {
	if f.cfg != nil {
		return f.cfg, nil
	}
	return &app.BookingConfig{}, nil
}
func (f *fakeBookingStore) BookingHours(context.Context, string) ([]app.BookingHour, error) {
	return nil, nil
}
func (f *fakeBookingStore) BookingBlackouts(context.Context, string, time.Time, time.Time) ([]app.BookingBlackout, error) {
	return nil, nil
}
func (f *fakeBookingStore) BookingBusy(context.Context, string, time.Time, time.Time) ([]app.BusyBlock, error) {
	return nil, nil
}
func (f *fakeBookingStore) CreateBooking(context.Context, app.CreateBookingInput) (*app.CreateBookingResult, error) {
	return nil, app.ErrBookingBadSlot
}
func (f *fakeBookingStore) BookingByToken(context.Context, []byte) (*app.PublicBooking, error) {
	return nil, nil
}

func (f *fakeBookingStore) BookingStaff(context.Context, string) ([]app.BookingStaffMember, error) {
	return nil, nil
}

// TestBookQR serves the share-code endpoint: eligible slugs get a PNG,
// anything else 404s like the form itself.
func TestBookQR(t *testing.T) {
	newHandler := func(cfg *app.BookingConfig) *BookHandler {
		svc := app.NewBookingService(app.BookingServiceDeps{Store: &fakeBookingStore{cfg: cfg}})
		return &BookHandler{Booking: svc, BaseURL: "https://example.com"}
	}

	// Unknown/ineligible slug: 404.
	h := newHandler(&app.BookingConfig{})
	req := httptest.NewRequest("GET", "/book/glam/qr", nil)
	req.SetPathValue("slug", "glam")
	rec := httptest.NewRecorder()
	h.QR(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("ineligible QR: got %d, want 404", rec.Code)
	}

	// Eligible: PNG bytes with the PNG magic header.
	h = newHandler(&app.BookingConfig{Eligible: true, FlagOn: true})
	req = httptest.NewRequest("GET", "/book/glam/qr", nil)
	req.SetPathValue("slug", "glam")
	rec = httptest.NewRecorder()
	h.QR(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("eligible QR: got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content type: got %q", ct)
	}
	if body := rec.Body.Bytes(); !bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
		t.Error("QR body must be a PNG")
	}
}
