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
