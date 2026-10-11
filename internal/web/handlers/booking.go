package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// BookHandler serves the public booking flow (no login): pick a service,
// choose a day, take a slot, submit. Eligibility (category + flag) is
// enforced by the service/config layer; unknown or ineligible slugs 404,
// indistinguishable from "no such page".
type BookHandler struct {
	Booking  *app.BookingService
	Catalog  *app.PublicOrderService
	BaseURL  string
	Renderer *render.Renderer
}

type bookServiceView struct {
	ID         string
	Name       string
	PriceMinor int64
	Currency   string
	// DurationMin is the visit length (0 = org default); DurationLabel
	// renders it ("45 min" or "1 hr").
	DurationMin   int
	DurationLabel string
}

type bookSlotView struct {
	Value string // "HH:MM" posted back
	Label string // "09:30 – 10:30"
}

type bookFormData struct {
	Title        string
	OrgName      string
	Slug         string
	CSRFToken    string
	Error        string
	Services     []bookServiceView
	ServiceID    string // echo
	Staff        []bookStaffView
	StaffID      string // echo; "" = anyone available
	Date         string // echo yyyy-mm-dd
	MinDate      string
	MaxDate      string
	Slots        []bookSlotView
	StartTime    string // echo HH:MM
	Name         string
	Phone        string
	Email        string
	Notes        string
	CancelPolicy string
	DayClosed    bool
	DayClosedWhy string
	Preview      bool
}

type bookStaffView struct {
	ID   string
	Name string
}

// servicesForBook projects the public catalog as bookable services.
func servicesForBook(r *http.Request, catalog *app.PublicOrderService, slug string) []bookServiceView {
	list := catalog.ListProducts(r.Context(), slug)
	out := make([]bookServiceView, 0, len(list))
	for _, p := range list {
		// Quote-only lines have no fixed price: still bookable, priced later.
		out = append(out, bookServiceView{
			ID: p.ID.String(), Name: p.Name,
			PriceMinor: p.UnitPriceMinor, Currency: p.Currency,
			DurationMin:   p.BookingDurationMin,
			DurationLabel: formatVisitLength(p.BookingDurationMin),
		})
	}
	return out
}

// formatVisitLength renders a duration override ("45 min", "2 hr").
// Empty means the org default applies.
func formatVisitLength(mins int) string {
	if mins <= 0 {
		return ""
	}
	if mins%60 == 0 {
		return strconv.Itoa(mins/60) + " hr"
	}
	return strconv.Itoa(mins) + " min"
}

// staffForBook projects the bookable team for the stylist picker.
func staffForBook(r *http.Request, booking *app.BookingService, slug string) []bookStaffView {
	list, err := booking.PublicStaff(r.Context(), slug)
	if err != nil {
		return nil
	}
	out := make([]bookStaffView, 0, len(list))
	for _, m := range list {
		out = append(out, bookStaffView{ID: m.UserID.String(), Name: m.Name})
	}
	return out
}

// resolveService finds the picked service server-side (client names and
// durations are untrusted).
func resolveService(services []bookServiceView, raw string) (bookServiceView, uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id == uuid.Nil {
		return bookServiceView{}, uuid.Nil, false
	}
	for _, s := range services {
		if s.ID == id.String() {
			return s, id, true
		}
	}
	return bookServiceView{}, uuid.Nil, false
}

// resolveStaff validates the picked stylist server-side ("" = anyone).
func resolveStaff(staff []bookStaffView, raw string) (*uuid.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return nil, false
	}
	for _, m := range staff {
		if m.ID == id.String() {
			out := id
			return &out, true
		}
	}
	return nil, false
}

// defaultBookDate returns today in the org timezone.
func defaultBookDate(tz string, now time.Time) string {
	return now.In(app.BookingTimezone(tz)).Format("2006-01-02")
}

// renderBookForm loads config + catalog + slots and renders the form.
// slug404 decides unknown slugs (plain 404 either way).
func (h *BookHandler) renderBookForm(w http.ResponseWriter, r *http.Request, slug string, status int, errMsg string, echo url.Values) {
	cfg, err := h.Booking.PublicConfig(r.Context(), slug)
	if err != nil || !cfg.BookingApplies() {
		http.NotFound(w, r)
		return
	}
	services := servicesForBook(r, h.Catalog, slug)
	staff := staffForBook(r, h.Booking, slug)
	date := echo.Get("date")
	if date == "" {
		date = defaultBookDate(cfg.Timezone, time.Now())
	}
	loc := app.BookingTimezone(cfg.Timezone)
	now := time.Now()
	minDate := now.In(loc).Format("2006-01-02")
	maxDate := now.In(loc).AddDate(0, 0, cfg.MaxAdvanceDays).Format("2006-01-02")
	// Slots follow the picked service's length (else the org default)
	// and the picked stylist's lane (else anyone).
	duration := cfg.DurationMin
	if svc, _, ok := resolveService(services, echo.Get("service")); ok && svc.DurationMin > 0 {
		duration = svc.DurationMin
	}
	staffID, staffOK := resolveStaff(staff, echo.Get("staff"))
	data := bookFormData{
		Title: "Book an appointment", OrgName: cfg.OrgName, Slug: slug,
		CSRFToken:    middleware.CSRFTokenFrom(r.Context()),
		Error:        errMsg,
		Services:     services,
		ServiceID:    echo.Get("service"),
		Staff:        staff,
		StaffID:      echo.Get("staff"),
		Date:         date,
		MinDate:      minDate,
		MaxDate:      maxDate,
		StartTime:    echo.Get("start"),
		Name:         echo.Get("name"),
		Phone:        echo.Get("phone"),
		Email:        echo.Get("email"),
		Notes:        echo.Get("notes"),
		CancelPolicy: cfg.CancelPolicy,
	}
	if r.URL.Query().Get("preview") == "1" {
		if s := middleware.SessionFromContext(r.Context()); s != nil && !s.Scope.IsZero() {
			data.Preview = true
		}
	}
	if len(services) > 0 && staffOK {
		if slots, err := h.Booking.SlotsForDate(r.Context(), slug, date, duration, staffID); err == nil && !slots.Closed {
			for _, sl := range slots.Slots {
				data.Slots = append(data.Slots, bookSlotView{
					Value: sl.Start.In(loc).Format("15:04"),
					Label: sl.Start.In(loc).Format("15:04") + " – " + sl.End.In(loc).Format("15:04"),
				})
			}
		} else if err == nil && slots.Closed {
			data.DayClosed = true
			data.DayClosedWhy = slots.ClosedWhy
		}
	}
	h.Renderer.PagePublic(w, status, "layouts/public.html", "book/form.html", data)
}

// BookForm handles GET /book/{slug}.
func (h *BookHandler) BookForm(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	h.renderBookForm(w, r, slug, http.StatusOK, "", r.URL.Query())
}

// BookSubmit handles POST /book/{slug}.
func (h *BookHandler) BookSubmit(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	echo := url.Values{}
	for _, k := range []string{"service", "staff", "date", "start", "name", "phone", "email", "notes"} {
		echo.Set(k, strings.TrimSpace(r.PostFormValue(k)))
	}
	fail := func(msg string) {
		h.renderBookForm(w, r, slug, http.StatusBadRequest, msg, echo)
	}

	svc, serviceID, ok := resolveService(servicesForBook(r, h.Catalog, slug), echo.Get("service"))
	if !ok {
		fail("Choose a service first.")
		return
	}
	staffID, ok := resolveStaff(staffForBook(r, h.Booking, slug), echo.Get("staff"))
	if !ok {
		fail("Choose a stylist (or anyone available).")
		return
	}

	res, err := h.Booking.SubmitBooking(r.Context(), app.SubmitBookingInput{
		Slug: slug, CustomerName: echo.Get("name"), CustomerEmail: echo.Get("email"),
		CustomerPhone: echo.Get("phone"), Notes: echo.Get("notes"),
		ProductID: serviceID, ServiceName: svc.Name,
		DurationMin: svc.DurationMin,
		StaffUserID: staffID,
		Date:        echo.Get("date"), StartTime: echo.Get("start"),
	})
	if err != nil {
		fail(humanizeBookingError(err))
		return
	}
	h.Renderer.PagePublic(w, http.StatusOK, "layouts/public.html", "book/confirmation.html", struct {
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
		Title: "Booking received", CustomerName: res.CustomerName,
		OrderNumber: res.OrderNumber, OrgName: res.OrgName,
		Start: res.Start.Format("Mon 02 Jan, 15:04"), End: res.End.Format("15:04"),
		Token: res.RawToken, TotalMinor: res.TotalMinor, Currency: res.Currency,
	})
}

// QR handles GET /book/{slug}/qr: a printable QR code for the booking
// link (counter cards, bios). The slug is public anyway; ineligible slugs
// 404 like the form itself.
func (h *BookHandler) QR(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	cfg, err := h.Booking.PublicConfig(r.Context(), slug)
	if err != nil || !cfg.BookingApplies() {
		http.NotFound(w, r)
		return
	}
	base := strings.TrimSuffix(h.BaseURL, "/")
	if base == "" {
		base = "http://localhost:8080"
	}
	png, err := qrcode.Encode(base+"/book/"+slug, qrcode.Medium, 256)
	if err != nil {
		http.Error(w, "could not make code", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(png)))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

func humanizeBookingError(err error) string {
	switch {
	case errors.Is(err, app.ErrBookingNotEligible):
		return "Online booking is not available for this business."
	case errors.Is(err, app.ErrBookingClosed):
		return "This business is not taking bookings right now. Please check back later."
	case errors.Is(err, app.ErrBookingSlotTaken):
		return "That time was just booked by someone else. Please pick another slot."
	case errors.Is(err, app.ErrBookingBadSlot):
		return "That time is not available. Please choose another day or slot."
	case errors.Is(err, app.ErrBookingNameRequired):
		return "Please enter your name."
	case errors.Is(err, app.ErrBookingContactNeeded):
		return "Please give an email or a phone number so the shop can reach you."
	case errors.Is(err, app.ErrBookingNoService):
		return "Choose a service first."
	default:
		return "Something went wrong. Please try again."
	}
}

// ---------------------------------------------------------------------------
// Staff panel.
// ---------------------------------------------------------------------------

// BookingsHandler serves the staff booking calendar + settings writes.
// Routes are booking-gated + flag-gated in main.go; settings writes are
// additionally owner-gated there and in the service.
type BookingsHandler struct {
	Booking  *app.BookingService
	Renderer *render.Renderer
}

type bookingRow struct {
	ID            string
	OrderNumber   string
	CustomerName  string
	CustomerPhone string
	ServiceName   string
	StaffName     string
	StartLabel    string // "Mon 02 Jan, 15:04"
	EndLabel      string // "16:00"
	DurationMin   int
	Status        string
	TotalMinor    int64
	PaidMinor     int64
	Currency      string
	IsRequested   bool
	IsConfirmed   bool
	IsLive        bool // requested or confirmed: actions available
}

type bookingsPage struct {
	Title       string
	CSRFToken   string
	FlashNotice string
	FlashError  string
	Date        string // echo yyyy-mm-dd
	PrevDate    string
	NextDate    string
	TodayDate   string
	Rows        []bookingRow
	Total       int
	Requested   int
	IsOwner     bool
}

// Index handles GET /dashboard/bookings.
func (h *BookingsHandler) Index(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	panel, err := h.Booking.DayPanel(r.Context(), s.Scope, s.OrgTimezone, queryValue(r, "date"))
	if err != nil {
		http.Error(w, "could not load bookings", http.StatusInternalServerError)
		return
	}
	loc := app.BookingTimezone(s.OrgTimezone)
	rows := make([]bookingRow, 0, len(panel.Bookings))
	for _, b := range panel.Bookings {
		rows = append(rows, bookingRow{
			ID: b.ID.String(), OrderNumber: b.OrderNumber,
			CustomerName: b.CustomerName, CustomerPhone: b.CustomerPhone,
			ServiceName: b.ServiceName, StaffName: b.StaffName,
			StartLabel:  b.StartAt.In(loc).Format("Mon 02 Jan, 15:04"),
			EndLabel:    b.EndAt.In(loc).Format("15:04"),
			DurationMin: int(b.EndAt.Sub(b.StartAt).Minutes()),
			Status:      string(b.Status), TotalMinor: b.TotalMinor,
			PaidMinor: b.PaidMinor, Currency: b.Currency,
			IsRequested: b.Status == "REQUESTED", IsConfirmed: b.Status == "CONFIRMED",
			IsLive: b.Status == "REQUESTED" || b.Status == "CONFIRMED",
		})
	}
	day := panel.Day
	page(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "bookings/index.html", bookingsPage{
		Title: "Bookings", CSRFToken: csrfFromCtx(r),
		FlashNotice: queryValue(r, "notice"), FlashError: queryValue(r, "error"),
		Date:      panel.DayLabel,
		PrevDate:  day.Add(-24 * time.Hour).Format("2006-01-02"),
		NextDate:  day.Add(24 * time.Hour).Format("2006-01-02"),
		TodayDate: time.Now().In(loc).Format("2006-01-02"),
		Rows:      rows, Total: len(rows), Requested: panel.Requested,
		IsOwner: s.Scope.Role == tenant.RoleOwner,
	})
}

// bookingBackTo returns the panel URL preserving the day filter.
func bookingBackTo(r *http.Request, notice, errMsg string) string {
	q := url.Values{}
	if d := queryValue(r, "date"); d != "" {
		q.Set("date", d)
	} else if d := r.PostFormValue("date"); d != "" {
		q.Set("date", d)
	}
	if notice != "" {
		q.Set("notice", notice)
	}
	if errMsg != "" {
		q.Set("error", errMsg)
	}
	u := "/dashboard/bookings"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// Confirm handles POST /dashboard/bookings/{id}/confirm.
func (h *BookingsHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, "confirmed", func(s tenant.TenantScope, id uuid.UUID) error {
		_, err := h.Booking.ConfirmBooking(r.Context(), s, id)
		return err
	}, "Booking confirmed.")
}

// Complete handles POST /dashboard/bookings/{id}/complete.
func (h *BookingsHandler) Complete(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, "completed", func(s tenant.TenantScope, id uuid.UUID) error {
		_, err := h.Booking.CompleteBooking(r.Context(), s, id)
		return err
	}, "Booking completed.")
}

// Cancel handles POST /dashboard/bookings/{id}/cancel.
func (h *BookingsHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, "cancelled", func(s tenant.TenantScope, id uuid.UUID) error {
		_, err := h.Booking.CancelBooking(r.Context(), s, id, formValue(r, "reason"))
		return err
	}, "Booking cancelled. The slot is free again.")
}

// NoShow handles POST /dashboard/bookings/{id}/no-show.
func (h *BookingsHandler) NoShow(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, "marked no-show", func(s tenant.TenantScope, id uuid.UUID) error {
		_, err := h.Booking.NoShowBooking(r.Context(), s, id)
		return err
	}, "Marked as no-show. The slot is free again.")
}

// Reschedule handles POST /dashboard/bookings/{id}/reschedule.
// The visit keeps its duration; only the start moves (date + time inputs).
func (h *BookingsHandler) Reschedule(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, "rescheduled", func(s tenant.TenantScope, id uuid.UUID) error {
		b, err := h.Booking.GetBooking(r.Context(), s, id)
		if err != nil {
			return err
		}
		loc := app.BookingTimezone(b.Timezone)
		day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(r.PostFormValue("new_date")), loc)
		if err != nil {
			return app.ErrBookingBadSlot
		}
		hm, err := time.ParseInLocation("15:04", strings.TrimSpace(r.PostFormValue("new_time")), loc)
		if err != nil {
			return app.ErrBookingBadSlot
		}
		y, m, d := day.Date()
		start := time.Date(y, m, d, hm.Hour(), hm.Minute(), 0, 0, loc)
		_, err = h.Booking.RescheduleBooking(r.Context(), s, id, start, start.Add(b.EndAt.Sub(b.StartAt)))
		return err
	}, "Booking rescheduled.")
}

// mutate parses the booking id + day echo and runs fn, redirecting back.
func (h *BookingsHandler) mutate(w http.ResponseWriter, r *http.Request, _ string, fn func(tenant.TenantScope, uuid.UUID) error, notice string) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := fn(s.Scope, id); err != nil {
		http.Redirect(w, r, bookingBackTo(r, "", humanizeBookingStaffError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, bookingBackTo(r, notice, ""), http.StatusSeeOther)
}

func humanizeBookingStaffError(err error) string {
	switch {
	case errors.Is(err, app.ErrBookingBadStatus):
		return "That booking is no longer awaiting that action. Refresh and try again."
	case errors.Is(err, app.ErrBookingStale):
		return "Someone just changed that booking. Refresh and try again."
	case errors.Is(err, app.ErrBookingSlotTaken):
		return "That time is already taken. Pick another."
	case errors.Is(err, app.ErrBookingBadSlot):
		return "That time is not available."
	case errors.Is(err, app.ErrBookingNotOwner):
		return "Only the business owner can do that."
	case errors.Is(err, app.ErrBookingSettingsBad):
		return "Check the booking settings values and try again."
	case errors.Is(err, app.ErrBookingNotFound):
		return "Booking not found."
	default:
		return "Something went wrong. Please try again."
	}
}
