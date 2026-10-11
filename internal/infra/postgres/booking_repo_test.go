package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/infra/id"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

// bookingTestOrg sets up an isolated Beauty & Salon org with the booking
// flag override. setupDB's cleanup (org cascade) removes everything made
// here: settings, hours, blackouts, bookings, members, products.
func bookingTestOrg(t *testing.T) (*postgres.DB, tenant.TenantScope, string) {
	t.Helper()
	db, scope := setupDB(t)
	ctx := context.Background()

	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE organizations SET business_category = 'Beauty & Salon' WHERE id = $1`, scope.OrgID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO feature_flag_overrides (flag_key, organization_id, enabled)
			VALUES ('booking_module', $1, TRUE)
			ON CONFLICT (flag_key, organization_id) DO UPDATE SET enabled = TRUE
		`, scope.OrgID)
		return err
	}); err != nil {
		t.Fatalf("booking setup: %v", err)
	}

	var slug string
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT slug::text FROM organizations WHERE id = $1`, scope.OrgID).Scan(&slug)
	}); err != nil {
		t.Fatalf("booking slug: %v", err)
	}
	return db, scope, slug
}

func bookingTestProduct(t *testing.T, db *postgres.DB, scope tenant.TenantScope) uuid.UUID {
	t.Helper()
	productID := uuid.New()
	if err := db.WithTenant(context.Background(), scope.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `
			INSERT INTO products
			    (id, organization_id, name, unit_price_minor, currency,
			     active, hidden, availability, quote_only, created_by, created_at, updated_at)
			VALUES ($1, $2, 'Box Braids', 15000, 'NGN',
			        TRUE, FALSE, 'in_stock', FALSE, $3, now(), now())`,
			productID, scope.OrgID, scope.UserID)
		return err
	}); err != nil {
		t.Fatalf("product: %v", err)
	}
	return productID
}

func bookingOwnerMember(t *testing.T, db *postgres.DB, scope tenant.TenantScope) {
	t.Helper()
	if err := db.WithTenant(context.Background(), scope.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `
			INSERT INTO organization_members (organization_id, user_id, role, status, created_at)
			VALUES ($1, $2, 'OWNER', 'ACTIVE', now())`, scope.OrgID, scope.UserID)
		return err
	}); err != nil {
		t.Fatalf("owner membership: %v", err)
	}
}

// bookingStaffMember adds one STAFF member and returns the user id. The org
// cascade removes the membership; the user row is cleaned by the caller.
func bookingStaffMember(t *testing.T, db *postgres.DB, scope tenant.TenantScope, name string) uuid.UUID {
	t.Helper()
	staffID := uuid.New()
	if err := db.WithTenant(context.Background(), scope.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), `
			INSERT INTO users (id, email, password_hash, name)
			VALUES ($1, $2, 'x', $3)`, staffID, name+"-"+staffID.String()[:8]+"@example.local", name); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(), `
			INSERT INTO organization_members (organization_id, user_id, role, status, created_at)
			VALUES ($1, $2, 'STAFF', 'ACTIVE', now())`, scope.OrgID, staffID)
		return err
	}); err != nil {
		t.Fatalf("staff member: %v", err)
	}
	t.Cleanup(func() {
		// Membership first (FK to users), then the user. The org cascade
		// (setupDB cleanup, runs after) would remove the membership
		// anyway; this keeps no orphan user row behind.
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
	return staffID
}

// bookingMonday returns next Monday 10:00 local (inside default hours).
func bookingMonday(tz string) time.Time {
	loc, _ := time.LoadLocation(tz)
	now := time.Now().In(loc)
	days := (8 - int(now.Weekday())) % 7
	if days == 0 {
		days = 7
	}
	day := now.AddDate(0, 0, days)
	return time.Date(day.Year(), day.Month(), day.Day(), 10, 0, 0, 0, loc)
}

func TestBookingRepo_SettingsHoursBlackouts(t *testing.T) {
	db, scope, _ := bookingTestOrg(t)
	ctx := context.Background()
	repo := postgres.NewBookingRepo(db, id.Generator{})

	// Defaults before first save.
	got, err := repo.GetSettings(ctx, scope)
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if got.DurationMin != 60 || !got.Accepting || got.MinNoticeHours != 2 || got.MaxAdvanceDays != 30 {
		t.Fatalf("bad defaults: %+v", got)
	}

	if err := repo.UpsertSettings(ctx, scope, &app.BookingSettings{
		OrgID: scope.OrgID, Accepting: false, DurationMin: 45,
		BufBefore: 10, BufAfter: 15, MinNoticeHours: 24,
		MaxAdvanceDays: 60, CancelPolicy: "24h", UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err = repo.GetSettings(ctx, scope)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DurationMin != 45 || got.Accepting || got.CancelPolicy != "24h" {
		t.Errorf("round trip mismatch: %+v", got)
	}

	// Hours round trip (absent = empty, defaults live in app code).
	if hours, err := repo.ListHours(ctx, scope); err != nil || len(hours) != 0 {
		t.Fatalf("fresh hours: %v %d", err, len(hours))
	}
	want := []app.BookingHour{{Dow: 1, OpenMin: 540, CloseMin: 1020, IsOpen: true}}
	for dow := 0; dow <= 6; dow++ {
		found := false
		for _, h := range want {
			if h.Dow == dow {
				found = true
			}
		}
		if !found {
			want = append(want, app.BookingHour{Dow: dow, OpenMin: 540, CloseMin: 1020, IsOpen: dow != 0})
		}
	}
	// Order by dow for the replace.
	ordered := make([]app.BookingHour, 0, 7)
	for dow := 0; dow <= 6; dow++ {
		for _, h := range want {
			if h.Dow == dow {
				ordered = append(ordered, h)
			}
		}
	}
	if err := repo.SetHours(ctx, scope, ordered); err != nil {
		t.Fatalf("set hours: %v", err)
	}
	hours, err := repo.ListHours(ctx, scope)
	if err != nil || len(hours) != 7 {
		t.Fatalf("hours: %v %d", err, len(hours))
	}
	if hours[1].OpenMin != 540 || hours[0].IsOpen {
		t.Errorf("hours mismatch: %+v", hours)
	}

	// Blackout add/remove.
	day := time.Now().AddDate(0, 0, 7)
	if err := repo.AddBlackout(ctx, scope, day, "Holiday", uuid.New(), time.Now()); err != nil {
		t.Fatalf("add blackout: %v", err)
	}
	list, err := repo.ListBlackouts(ctx, scope, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1))
	if err != nil || len(list) != 1 || list[0].Reason != "Holiday" {
		t.Fatalf("blackouts: %v %+v", err, list)
	}
	if err := repo.RemoveBlackout(ctx, scope, day); err != nil {
		t.Fatalf("remove blackout: %v", err)
	}
	if list, _ := repo.ListBlackouts(ctx, scope, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1)); len(list) != 0 {
		t.Fatalf("blackout must be gone: %+v", list)
	}
}

// TestBookingRepo_GateMatrix covers flag OFF, wrong category, and override ON.
func TestBookingRepo_GateMatrix(t *testing.T) {
	db, scope, slug := bookingTestOrg(t)
	ctx := context.Background()
	repo := postgres.NewBookingRepo(db, id.Generator{})

	// Override ON + salon category: applies.
	cfg, err := repo.BookingConfig(ctx, slug)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !cfg.BookingApplies() || !cfg.Accepting || cfg.DurationMin != 60 {
		t.Fatalf("flagged salon must apply: %+v", cfg)
	}

	// Flag override OFF: no booking UI.
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE feature_flag_overrides SET enabled = FALSE
			WHERE flag_key = 'booking_module' AND organization_id = $1`, scope.OrgID)
		return err
	}); err != nil {
		t.Fatalf("flag off: %v", err)
	}
	if cfg, _ := repo.BookingConfig(ctx, slug); cfg.BookingApplies() {
		t.Fatal("flag OFF must not apply")
	}

	// Wrong category (even with flag on): no booking UI, and perishable
	// gating stays untouched.
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE feature_flag_overrides SET enabled = TRUE
			WHERE flag_key = 'booking_module' AND organization_id = $1`, scope.OrgID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`UPDATE organizations SET business_category = 'Tailoring & Fashion' WHERE id = $1`, scope.OrgID)
		return err
	}); err != nil {
		t.Fatalf("recategorize: %v", err)
	}
	if cfg, _ := repo.BookingConfig(ctx, slug); cfg.BookingApplies() {
		t.Fatal("tailoring org must not apply booking")
	}
	if cfg, _ := repo.BookingConfig(ctx, slug); cfg.Eligible {
		t.Error("tailoring org must not be eligible")
	}
}

// TestBookingRepo_SameSlotRace proves one winner: two concurrent bookings
// for the identical slot yield exactly one REQUESTED row and one SLOT_TAKEN.
func TestBookingRepo_SameSlotRace(t *testing.T) {
	db, scope, slug := bookingTestOrg(t)
	ctx := context.Background()
	const tz = "Africa/Lagos"
	bookingOwnerMember(t, db, scope)
	productID := bookingTestProduct(t, db, scope)
	repo := postgres.NewBookingRepo(db, id.Generator{})

	// Next Monday 10:00 local, safely inside defaults (Mon-Sat 09-17,
	// 2h notice, 30-day window).
	loc, _ := time.LoadLocation(tz)
	now := time.Now().In(loc)
	daysUntilMonday := (8 - int(now.Weekday())) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	day := now.AddDate(0, 0, daysUntilMonday)
	mid := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	start := mid.Add(10 * time.Hour)
	end := start.Add(time.Hour)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	results := make([]*app.CreateBookingResult, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = repo.CreateBooking(ctx, app.CreateBookingInput{
				Slug: slug, CustomerName: fmt.Sprintf("Racer %d", i),
				CustomerPhone: fmt.Sprintf("080%d", i),
				ServiceName:   "Box Braids", ProductID: &productID,
				Start: start, End: end, Timezone: tz,
			})
		}(i)
	}
	wg.Wait()

	won, taken := 0, 0
	for _, err := range errs {
		switch err {
		case nil:
			won++
		default:
			if errors.Is(err, app.ErrBookingSlotTaken) {
				taken++
			} else {
				t.Fatalf("unexpected race error: %v", err)
			}
		}
	}
	if won != 1 || taken != 1 {
		t.Fatalf("want exactly 1 winner + 1 SLOT_TAKEN, got %d + %d (%v)", won, taken, errs)
	}
	for _, res := range results {
		if res == nil {
			continue
		}
		if res.OrderID == uuid.Nil || res.BookingID == uuid.Nil || res.RawToken == "" {
			t.Fatalf("winner must carry ids + tracking token: %+v", res)
		}
	}

	// Cancel frees the slot: the same time books cleanly again.
	winner := results[0]
	if winner == nil {
		winner = results[1]
	}
	bk, err := repo.GetBooking(ctx, scope, winner.BookingID)
	if err != nil {
		t.Fatalf("get winner: %v", err)
	}
	if bk.Status != "REQUESTED" {
		t.Fatalf("fresh booking must be REQUESTED, got %s", bk.Status)
	}
	if err := repo.UpdateBookingStatus(ctx, scope, bk.ID, "REQUESTED", "CANCELLED", "test", time.Now()); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	res, err := repo.CreateBooking(ctx, app.CreateBookingInput{
		Slug: slug, CustomerName: "After Cancel", CustomerPhone: "0809",
		ServiceName: "Box Braids", ProductID: &productID,
		Start: start, End: end, Timezone: tz,
	})
	if err != nil {
		t.Fatalf("rebook after cancel must succeed: %v", err)
	}
	if res.BookingID == uuid.Nil {
		t.Fatal("rebook must return a booking id")
	}
}

// TestBookingRepo_StaffLanes proves per-stylist independence: two stylists
// take the same slot concurrently and both win; the same stylist twice
// yields one winner; anyone else (or a bogus id) loses.
func TestBookingRepo_StaffLanes(t *testing.T) {
	db, scope, slug := bookingTestOrg(t)
	ctx := context.Background()
	const tz = "Africa/Lagos"
	bookingOwnerMember(t, db, scope)
	ada := bookingStaffMember(t, db, scope, "Ada")
	bola := bookingStaffMember(t, db, scope, "Bola")
	productID := bookingTestProduct(t, db, scope)
	repo := postgres.NewBookingRepo(db, id.Generator{})

	// Staff picker lists writers, owner first (owner has no membership row
	// here beyond OWNER... owner IS a writer; staff follow by name).
	staff, err := repo.BookingStaff(ctx, slug)
	if err != nil {
		t.Fatalf("staff list: %v", err)
	}
	if len(staff) != 3 { // OWNER + Ada + Bola
		t.Fatalf("want 3 bookable staff, got %+v", staff)
	}
	if staff[0].UserID != scope.UserID {
		t.Errorf("owner must list first: %+v", staff)
	}

	start := bookingMonday(tz)
	end := start.Add(time.Hour)
	book := func(name, phone string, staffID *uuid.UUID) (*app.CreateBookingResult, error) {
		return repo.CreateBooking(ctx, app.CreateBookingInput{
			Slug: slug, CustomerName: name, CustomerPhone: phone,
			ServiceName: "Box Braids", ProductID: &productID,
			Start: start, End: end, Timezone: tz, StaffUserID: staffID,
		})
	}

	// Same slot, different stylists, concurrently: both win.
	var wg sync.WaitGroup
	type outcome struct {
		res *app.CreateBookingResult
		err error
	}
	outs := make([]outcome, 2)
	lanes := []*uuid.UUID{&ada, &bola}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outs[i].res, outs[i].err = book(fmt.Sprintf("Lane %d", i), fmt.Sprintf("081%d", i), lanes[i])
		}(i)
	}
	wg.Wait()
	for i, o := range outs {
		if o.err != nil {
			t.Fatalf("lane %d must win its own stylist: %v", i, o.err)
		}
		if o.res.BookingID == uuid.Nil {
			t.Fatalf("lane %d: missing booking id", i)
		}
	}

	// Same stylist again: taken.
	if _, err := book("Ada again", "0820", &ada); !errors.Is(err, app.ErrBookingSlotTaken) {
		t.Fatalf("same stylist double-book: got %v, want ErrBookingSlotTaken", err)
	}
	// Anyone: taken (conservative — a named visit occupies the pool).
	if _, err := book("Anyone", "0821", nil); !errors.Is(err, app.ErrBookingSlotTaken) {
		t.Fatalf("anyone over named visits: got %v, want ErrBookingSlotTaken", err)
	}
	// Bogus stylist id: rejected, not booked.
	bogus := uuid.New()
	if _, err := book("Ghost", "0822", &bogus); !errors.Is(err, app.ErrBookingBadSlot) {
		t.Fatalf("bogus stylist: got %v, want ErrBookingBadSlot", err)
	}
}

// TestBookingRepo_ServiceDuration proves the visit length comes from the
// service row (org default fallback) and that a spoofed length fails in
// the transaction even when the caller computed something else.
func TestBookingRepo_ServiceDuration(t *testing.T) {
	db, scope, slug := bookingTestOrg(t)
	ctx := context.Background()
	const tz = "Africa/Lagos"
	bookingOwnerMember(t, db, scope)
	productID := bookingTestProduct(t, db, scope)
	repo := postgres.NewBookingRepo(db, id.Generator{})

	// Braids take 90 minutes for this shop.
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE products SET booking_duration_min = 90 WHERE id = $1`, productID)
		return err
	}); err != nil {
		t.Fatalf("duration: %v", err)
	}

	monday := bookingMonday(tz)
	book := func(start, end time.Time) (*app.CreateBookingResult, error) {
		return repo.CreateBooking(ctx, app.CreateBookingInput{
			Slug: slug, CustomerName: "Dur", CustomerPhone: "0830",
			ServiceName: "Box Braids", ProductID: &productID,
			Start: start, End: end, Timezone: tz,
		})
	}
	// 60 minutes against a 90-minute service: rejected.
	if _, err := book(monday, monday.Add(time.Hour)); !errors.Is(err, app.ErrBookingBadSlot) {
		t.Fatalf("wrong length: got %v, want ErrBookingBadSlot", err)
	}
	// 90 minutes: books, and the row keeps it.
	res, err := book(monday, monday.Add(90*time.Minute))
	if err != nil {
		t.Fatalf("90min visit: %v", err)
	}
	got, err := repo.GetBooking(ctx, scope, res.BookingID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.EndAt.Sub(got.StartAt) != 90*time.Minute {
		t.Fatalf("stored visit must be 90min: %v", got.EndAt.Sub(got.StartAt))
	}
}
