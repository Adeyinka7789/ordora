package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/infra/id"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

// bookingLoopFixtures wires real services on an isolated salon org:
// owner membership, one service product, defaults left as-is
// (Mon–Sat 09–17, 60min, 2h notice, 30-day window).
func bookingLoopFixtures(t *testing.T) (*postgres.DB, tenant.TenantScope, string, uuid.UUID, *app.BookingService) {
	t.Helper()
	db, scope, slug := bookingTestOrg(t)
	ctx := context.Background()

	productID := uuid.New()
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO organization_members (organization_id, user_id, role, status, created_at)
			VALUES ($1, $2, 'OWNER', 'ACTIVE', now())`, scope.OrgID, scope.UserID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO products
			    (id, organization_id, name, unit_price_minor, currency,
			     active, hidden, availability, quote_only, created_by, created_at, updated_at)
			VALUES ($1, $2, 'Box Braids', 15000, 'NGN',
			        TRUE, FALSE, 'in_stock', FALSE, $3, now(), now())`,
			productID, scope.OrgID, scope.UserID)
		return err
	}); err != nil {
		t.Fatalf("fixtures: %v", err)
	}

	orderRepo := postgres.NewOrderRepo(db)
	orderSvc := app.NewOrderService(app.OrderServiceDeps{
		DB: db, Customers: postgres.NewCustomerRepo(db),
		Products: postgres.NewProductRepo(db), Orders: orderRepo,
		OrderRead: orderRepo, Numbers: postgres.NewOrderNumberRepo(db),
		Audit: postgres.NewAuditRepo(db), Outbox: postgres.NewOutboxRepo(db),
		Measurements: postgres.NewMeasurementRepo(db), IDs: id.Generator{},
	})
	bookingSvc := app.NewBookingService(app.BookingServiceDeps{
		DB: db, Store: postgres.NewBookingRepo(db, id.Generator{}),
		Orders: orderSvc, Outbox: postgres.NewOutboxRepo(db), IDs: id.Generator{},
	})
	return db, scope, slug, productID, bookingSvc
}

// nextWeekday returns the next given weekday (at least tomorrow) at hh:mm
// in tz.
func nextWeekday(from time.Time, tz string, weekday time.Weekday, hh, mm int) time.Time {
	loc := app.BookingTimezone(tz)
	now := from.In(loc)
	days := (int(weekday) - int(now.Weekday()) + 7) % 7
	if days == 0 {
		days = 7
	}
	day := now.AddDate(0, 0, days)
	y, m, d := day.Date()
	return time.Date(y, m, d, hh, mm, 0, 0, loc)
}

// TestBookingFullLoop runs the whole flow on real services: public submit
// (one order + one appointment + tracking token) -> staff confirm ->
// reschedule conflict rejected -> reschedule ok -> complete. A second
// booking is cancelled and its slot rebooks cleanly.
func TestBookingFullLoop(t *testing.T) {
	db, scope, slug, productID, bookingSvc := bookingLoopFixtures(t)
	ctx := context.Background()
	const tz = "Africa/Lagos"

	// Monday 10:00 local, safely inside defaults.
	start := nextWeekday(time.Now(), tz, time.Monday, 10, 0)
	dateStr := start.Format("2006-01-02")

	submit := func(name, phone string, at time.Time) *app.SubmitBookingResult {
		t.Helper()
		res, err := bookingSvc.SubmitBooking(ctx, app.SubmitBookingInput{
			Slug: slug, CustomerName: name, CustomerPhone: phone,
			Notes: "Sensitive scalp", ProductID: productID, ServiceName: "Box Braids",
			Date: at.Format("2006-01-02"), StartTime: at.Format("15:04"),
		})
		if err != nil {
			t.Fatalf("submit %s: %v", name, err)
		}
		if res.OrderNumber == "" || res.RawToken == "" || res.TotalMinor != 15000 {
			t.Fatalf("submit %s: bad result %+v", name, res)
		}
		return res
	}

	res := submit("Ada", "08031", start)

	// Tracking token resolves the appointment (public tracking link).
	tracked, err := bookingSvc.BookingByToken(ctx, res.RawToken)
	if err != nil || tracked == nil {
		t.Fatalf("tracking: %v %+v", err, tracked)
	}
	if tracked.ServiceName != "Box Braids" || tracked.Status != "REQUESTED" {
		t.Fatalf("tracking mismatch: %+v", tracked)
	}
	if !tracked.StartAt.Equal(start) {
		t.Fatalf("tracking start: got %v want %v", tracked.StartAt, start)
	}

	// Panel shows it on the day.
	panel, err := bookingSvc.DayPanel(ctx, scope, tz, dateStr)
	if err != nil {
		t.Fatalf("panel: %v", err)
	}
	if len(panel.Bookings) != 1 || panel.Requested != 1 {
		t.Fatalf("panel: %+v", panel.Bookings)
	}

	// Confirm pairs booking CONFIRMED with order CONFIRMED.
	bk, err := bookingSvc.ConfirmBooking(ctx, scope, res.BookingID)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if bk.Status != "CONFIRMED" {
		t.Fatalf("after confirm: %s", bk.Status)
	}

	// Reschedule into an overlapping live visit fails...
	other := submit("Bola", "08032", start.Add(90*time.Minute)) // 11:30, overlaps buffer-free? 11:30-12:30 vs 10:00-11:00: no overlap, ok
	if err := func() error {
		// Move other onto 10:30: overlaps Ada 10:00-11:00.
		loc := app.BookingTimezone(tz)
		y, m, d := start.In(loc).Date()
		ns := time.Date(y, m, d, 10, 30, 0, 0, loc)
		_, err := bookingSvc.RescheduleBooking(ctx, scope, other.BookingID, ns, ns.Add(time.Hour))
		return err
	}(); !errors.Is(err, app.ErrBookingSlotTaken) {
		t.Fatalf("overlapping reschedule: got %v, want ErrBookingSlotTaken", err)
	}
	// ...but a free time works and keeps the same duration.
	loc := app.BookingTimezone(tz)
	y, m, d := start.In(loc).Date()
	free := time.Date(y, m, d, 13, 0, 0, 0, loc)
	moved, err := bookingSvc.RescheduleBooking(ctx, scope, other.BookingID, free, free.Add(time.Hour))
	if err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	if !moved.StartAt.Equal(free) || moved.RescheduledAt == nil {
		t.Fatalf("reschedule metadata wrong: %+v", moved)
	}

	// Complete walks both records shut.
	if _, err := bookingSvc.CompleteBooking(ctx, scope, res.BookingID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	done, err := bookingSvc.GetBooking(ctx, scope, res.BookingID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if done.Status != "COMPLETED" || done.OrderStatus != "COMPLETED" {
		t.Fatalf("after complete: booking=%s order=%s", done.Status, done.OrderStatus)
	}

	// Cancel frees the slot: 10:00 books again after Ada is done...
	// (Ada completed, so 10:00 is past-blocked only by history, not by
	// status — rebooking the same slot now must still conflict? No:
	// COMPLETED rows don't block. Rebook 10:00 fresh.)
	res2, err := bookingSvc.SubmitBooking(ctx, app.SubmitBookingInput{
		Slug: slug, CustomerName: "Chidi", CustomerPhone: "08033",
		ProductID: productID, ServiceName: "Box Braids",
		Date: dateStr, StartTime: "10:00",
	})
	if err != nil {
		t.Fatalf("rebook completed slot: %v", err)
	}
	if _, err := bookingSvc.CancelBooking(ctx, scope, res2.BookingID, "client called off"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	cancelled, err := bookingSvc.GetBooking(ctx, scope, res2.BookingID)
	if err != nil {
		t.Fatalf("get cancelled: %v", err)
	}
	if cancelled.Status != "CANCELLED" || cancelled.OrderStatus != "CANCELLED" {
		t.Fatalf("after cancel: booking=%s order=%s", cancelled.Status, cancelled.OrderStatus)
	}
	if _, err := bookingSvc.SubmitBooking(ctx, app.SubmitBookingInput{
		Slug: slug, CustomerName: "Efe", CustomerPhone: "08034",
		ProductID: productID, ServiceName: "Box Braids",
		Date: dateStr, StartTime: "10:00",
	}); err != nil {
		t.Fatalf("rebook cancelled slot: %v", err)
	}

	// Negative: completing a REQUESTED visit fails outright.
	fresh := submit("Funmi", "08035", start.Add(4*time.Hour)) // 14:00, free
	if _, err := bookingSvc.CompleteBooking(ctx, scope, fresh.BookingID); !errors.Is(err, app.ErrBookingBadStatus) {
		t.Fatalf("completing REQUESTED: got %v, want ErrBookingBadStatus", err)
	}
	// Negative: staff viewer role cannot mutate (RequireWrite upstream, but
	// the service fails closed too).
	viewer := scope
	viewer.Role = tenant.RoleViewer
	if _, err := bookingSvc.ConfirmBooking(ctx, viewer, fresh.BookingID); err == nil {
		t.Fatal("viewer confirm must fail")
	}

	// Staff (writer role) confirms just like the owner: the sales-girl
	// flow this module exists for.
	staffID := uuid.New()
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO users (id, email, password_hash, name)
			VALUES ($1, $2, 'x', 'Girl Friday')`, staffID, "staff-"+staffID.String()[:8]+"@example.local"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO organization_members (organization_id, user_id, role, status, created_at)
			VALUES ($1, $2, 'STAFF', 'ACTIVE', now())`, scope.OrgID, staffID)
		return err
	}); err != nil {
		t.Fatalf("staff fixture: %v", err)
	}
	staffScope := scope
	staffScope.UserID = staffID
	staffScope.Role = tenant.RoleStaff
	t.Cleanup(func() {
		_ = db.WithTenant(context.Background(), scope.OrgID, func(tx pgx.Tx) error {
			if _, err := tx.Exec(context.Background(), `
				DELETE FROM organization_members WHERE organization_id = $1 AND user_id = $2`,
				scope.OrgID, staffID); err != nil {
				return err
			}
			_, err := tx.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, staffID)
			return err
		})
	})
	if _, err := bookingSvc.ConfirmBooking(ctx, staffScope, fresh.BookingID); err != nil {
		t.Fatalf("staff confirm: %v", err)
	}
	if got, _ := bookingSvc.GetBooking(ctx, scope, fresh.BookingID); got.Status != "CONFIRMED" {
		t.Fatalf("staff confirm left status %s", got.Status)
	}
}

// TestBookingRescheduleValidation proves moves are re-validated, not just
// overlap-checked: Sunday, blackouts, closed hours, past, window, and
// duration changes all fail — through the service (friendly errors) and,
// for Sunday/duration, straight at the repo (tx enforcement without the
// service in the path).
func TestBookingRescheduleValidation(t *testing.T) {
	db, scope, slug, productID, bookingSvc := bookingLoopFixtures(t)
	ctx := context.Background()
	const tz = "Africa/Lagos"

	monday := nextWeekday(time.Now(), tz, time.Monday, 10, 0)
	submit := func(name, phone string, at time.Time) uuid.UUID {
		t.Helper()
		res, err := bookingSvc.SubmitBooking(ctx, app.SubmitBookingInput{
			Slug: slug, CustomerName: name, CustomerPhone: phone,
			ProductID: productID, ServiceName: "Box Braids",
			Date: at.Format("2006-01-02"), StartTime: at.Format("15:04"),
		})
		if err != nil {
			t.Fatalf("submit %s: %v", name, err)
		}
		return res.BookingID
	}
	bkID := submit("Ada", "08031", monday)
	at := func(day time.Time, hh, mm int) (time.Time, time.Time) {
		y, m, d := day.Date()
		s := time.Date(y, m, d, hh, mm, 0, 0, app.BookingTimezone(tz))
		return s, s.Add(time.Hour)
	}
	mustBadSlot := func(label string, err error) {
		t.Helper()
		if !errors.Is(err, app.ErrBookingBadSlot) {
			t.Fatalf("%s: got %v, want ErrBookingBadSlot", label, err)
		}
	}

	// Valid move first: 10:00 -> 13:00 same Monday.
	s13, e13 := at(monday, 13, 0)
	if _, err := bookingSvc.RescheduleBooking(ctx, scope, bkID, s13, e13); err != nil {
		t.Fatalf("valid move: %v", err)
	}

	// Sunday (closed by default) via service and straight at the repo.
	sunday := nextWeekday(time.Now(), tz, time.Sunday, 10, 0)
	ss, se := at(sunday, 10, 0)
	mustBadSlot("sunday service", func() error {
		_, err := bookingSvc.RescheduleBooking(ctx, scope, bkID, ss, se)
		return err
	}())
	repo := postgres.NewBookingRepo(db, id.Generator{})
	mustBadSlot("sunday repo", func() error {
		_, err := repo.Reschedule(ctx, scope, bkID, ss, se, time.Now())
		return err
	}())

	// Blackout Monday blocks moves (service level).
	if err := bookingSvc.AddBlackout(ctx, scope, monday, "Holiday"); err != nil {
		t.Fatalf("blackout: %v", err)
	}
	s14, e14 := at(monday, 14, 0)
	mustBadSlot("blackout service", func() error {
		_, err := bookingSvc.RescheduleBooking(ctx, scope, bkID, s14, e14)
		return err
	}())
	if err := bookingSvc.RemoveBlackout(ctx, scope, monday); err != nil {
		t.Fatalf("unblackout: %v", err)
	}

	// Outside opening hours, past, beyond window, changed duration.
	s7, e7 := at(monday, 7, 0)
	mustBadSlot("closed hours", func() error {
		_, err := bookingSvc.RescheduleBooking(ctx, scope, bkID, s7, e7)
		return err
	}())
	past := time.Now().Add(-48 * time.Hour)
	mustBadSlot("past", func() error {
		_, err := bookingSvc.RescheduleBooking(ctx, scope, bkID, past, past.Add(time.Hour))
		return err
	}())
	farMonday := monday.AddDate(0, 0, 56) // same weekday, past the window
	sf, ef := at(farMonday, 10, 0)
	mustBadSlot("window", func() error {
		_, err := bookingSvc.RescheduleBooking(ctx, scope, bkID, sf, ef)
		return err
	}())
	mustBadSlot("duration service", func() error {
		_, err := bookingSvc.RescheduleBooking(ctx, scope, bkID, s14, e14.Add(30*time.Minute))
		return err
	}())
	mustBadSlot("duration repo", func() error {
		_, err := repo.Reschedule(ctx, scope, bkID, s14, e14.Add(30*time.Minute), time.Now())
		return err
	}())

	// Nothing above may have moved the visit: still Monday 13:00.
	got, err := bookingSvc.GetBooking(ctx, scope, bkID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.StartAt.Equal(s13) {
		t.Fatalf("rejected moves must not shift the visit: at %v", got.StartAt)
	}
}

// TestBookingCreateInvariants tampers with the submit payload at the repo
// boundary: duration, buffers, and timezone must match the org's configured
// values or the transaction reports BAD_SLOT. Control case succeeds.
func TestBookingCreateInvariants(t *testing.T) {
	db, _, slug, _, _ := bookingLoopFixtures(t)
	ctx := context.Background()
	const tz = "Africa/Lagos"
	repo := postgres.NewBookingRepo(db, id.Generator{})

	monday := nextWeekday(time.Now(), tz, time.Monday, 10, 0)
	base := app.CreateBookingInput{
		Slug: slug, CustomerName: "Tamper", CustomerEmail: "t@example.com",
		CustomerPhone: "08099", ServiceName: "Box Braids",
		Start: monday, End: monday.Add(time.Hour), Timezone: tz,
	}
	mustBadSlot := func(label string, in app.CreateBookingInput) {
		t.Helper()
		if _, err := repo.CreateBooking(ctx, in); !errors.Is(err, app.ErrBookingBadSlot) {
			t.Fatalf("%s: got %v, want ErrBookingBadSlot", label, err)
		}
	}
	long := base
	long.End = long.Start.Add(90 * time.Minute)
	mustBadSlot("duration", long)

	bufs := base
	bufs.BufBefore = 99
	mustBadSlot("buffers", bufs)

	zt := base
	zt.Timezone = "America/New_York"
	mustBadSlot("timezone", zt)

	res, err := repo.CreateBooking(ctx, base)
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	if res.OrderID == uuid.Nil || res.BookingID == uuid.Nil {
		t.Fatalf("control must create linked rows: %+v", res)
	}
}
