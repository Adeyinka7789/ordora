package postgres_test

import (
	"context"
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

// eggTestOrg sets up an isolated perishable org: category + flag override.
// setupDB's cleanup (org cascade) removes settings/counters/members too.
func eggTestOrg(t *testing.T) (*postgres.DB, tenant.TenantScope, string) {
	t.Helper()
	db, scope := setupDB(t)
	ctx := context.Background()

	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE organizations SET business_category = 'Poultry & Eggs' WHERE id = $1`, scope.OrgID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO feature_flag_overrides (flag_key, organization_id, enabled)
			VALUES ('egg_module', $1, TRUE)
			ON CONFLICT (flag_key, organization_id) DO UPDATE SET enabled = TRUE
		`, scope.OrgID)
		return err
	}); err != nil {
		t.Fatalf("egg setup: %v", err)
	}

	var slug string
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT slug::text FROM organizations WHERE id = $1`, scope.OrgID).Scan(&slug)
	}); err != nil {
		t.Fatalf("egg slug: %v", err)
	}
	return db, scope, slug
}

func TestEggRepo_SettingsRoundTrip(t *testing.T) {
	db, scope, _ := eggTestOrg(t)
	ctx := context.Background()
	repo := postgres.NewEggRepo(db)

	// Missing row reads as defaults (normal for new sellers).
	got, err := repo.GetSettings(ctx, scope)
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if got.MaxCratesPerDay != app.DefaultMaxCrates || !got.AcceptingOrders {
		t.Fatalf("want defaults 100/open, got %+v", got)
	}

	want := &app.PoultrySettings{
		OrgID: scope.OrgID, MaxCratesPerDay: 60, AcceptingOrders: false,
		BankName: "GTBank", AccountNumber: "0123456789",
		AccountName: "Egg Farm", Whatsapp: "+2348030000000",
		UpdatedAt: time.Now(),
	}
	if err := repo.UpsertSettings(ctx, scope, want); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err = repo.GetSettings(ctx, scope)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.MaxCratesPerDay != 60 || got.AcceptingOrders ||
		got.BankName != "GTBank" || got.AccountNumber != "0123456789" {
		t.Errorf("round trip mismatch: %+v", got)
	}

	// Toggle flips only the flag, preserving the rest.
	if err := repo.SetAccepting(ctx, scope, true, time.Now()); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	got, err = repo.GetSettings(ctx, scope)
	if err != nil {
		t.Fatalf("get after toggle: %v", err)
	}
	if !got.AcceptingOrders || got.MaxCratesPerDay != 60 || got.BankName != "GTBank" {
		t.Errorf("toggle must preserve other fields: %+v", got)
	}
}

// TestEggCap_ConcurrentConsumeAccumulates proves the counter upsert is
// atomic: 16 simultaneous 1-crate consumes against a cap of 16 must total
// exactly 16 (a read-modify-write would lose increments), and the 17th
// must report over-cap.
func TestEggCap_ConcurrentConsumeAccumulates(t *testing.T) {
	db, scope, slug := eggTestOrg(t)
	ctx := context.Background()
	repo := postgres.NewEggRepo(db)

	if err := repo.UpsertSettings(ctx, scope, &app.PoultrySettings{
		OrgID: scope.OrgID, MaxCratesPerDay: 16, AcceptingOrders: true,
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("cap: %v", err)
	}
	day := time.Now().In(app.EggTimezone("Africa/Lagos"))

	const racers = 16
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = db.WithTx(ctx, func(tx pgx.Tx) error {
				var sold float64
				var cap int
				q := `SELECT sold_after, cap FROM consume_crate_cap($1, $2::date, $3::numeric)`
				if e := tx.QueryRow(ctx, q, slug,
					day.Format("2006-01-02"), "1.000").Scan(&sold, &cap); e != nil {
					return e
				}
				if sold > float64(cap) {
					return fmt.Errorf("over cap: sold=%v cap=%d", sold, cap)
				}
				return nil
			})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d failed: %v", i, err)
		}
	}

	sold, err := repo.GetSold(ctx, scope, day)
	if err != nil {
		t.Fatalf("sold: %v", err)
	}
	if sold != racers {
		t.Fatalf("want sold=%d exactly (no lost increments), got %v", racers, sold)
	}

	// One more crate breaches the cap.
	breached := false
	_ = db.WithTx(ctx, func(tx pgx.Tx) error {
		var soldAfter float64
		var cap int
		q := `SELECT sold_after, cap FROM consume_crate_cap($1, $2::date, $3::numeric)`
		if e := tx.QueryRow(ctx, q, slug,
			day.Format("2006-01-02"), "1.000").Scan(&soldAfter, &cap); e != nil {
			return e
		}
		breached = soldAfter > float64(cap)
		// Roll back: keep the fixture at the cap for the release check.
		return fmt.Errorf("rollback-marker")
	})
	if !breached {
		t.Fatal("17th crate must report over-cap")
	}

	// Release floors at zero and subtracts on the way.
	if err := repo.ReleaseCap(ctx, scope, day, 5); err != nil {
		t.Fatalf("release: %v", err)
	}
	if sold, _ := repo.GetSold(ctx, scope, day); sold != racers-5 {
		t.Fatalf("want sold=%d after release, got %v", racers-5, sold)
	}
	if err := repo.ReleaseCap(ctx, scope, day, 1000); err != nil {
		t.Fatalf("over-release: %v", err)
	}
	if sold, _ := repo.GetSold(ctx, scope, day); sold != 0 {
		t.Fatalf("release must floor at zero, got %v", sold)
	}
}

// TestEggIntakeConfig verifies the anonymous config: unknown slugs read as
// non-egg, and a flagged perishable org reports gate + cap + bank details.
func TestEggIntakeConfig(t *testing.T) {
	db, scope, slug := eggTestOrg(t)
	ctx := context.Background()
	repo := postgres.NewEggRepo(db)
	pubs := postgres.NewPublicOrderRepo(db, id.Generator{})

	cfg, err := pubs.IntakeConfig(ctx, "definitely-not-a-real-slug-egg")
	if err != nil {
		t.Fatalf("unknown slug: %v", err)
	}
	if cfg.EggApplies() {
		t.Fatal("unknown slug must not apply the egg UI")
	}

	if err := repo.UpsertSettings(ctx, scope, &app.PoultrySettings{
		OrgID: scope.OrgID, MaxCratesPerDay: 40, AcceptingOrders: true,
		BankName: "GTBank", AccountNumber: "0123456789",
		AccountName: "Egg Farm", UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("settings: %v", err)
	}
	cfg, err = pubs.IntakeConfig(ctx, slug)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !cfg.EggApplies() {
		t.Fatalf("flagged poultry org must apply the egg UI: %+v", cfg)
	}
	if cfg.Cap != 40 || cfg.BankName != "GTBank" || cfg.AccountNumber != "0123456789" {
		t.Errorf("config mismatch: %+v", cfg)
	}
	if cfg.Timezone == "" || cfg.Currency == "" {
		t.Errorf("config must carry timezone+currency: %+v", cfg)
	}

	// Suspended orgs resolve as non-egg (intake 404s upstream).
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE organizations SET suspended_at = now() WHERE id = $1`, scope.OrgID)
		return err
	}); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	t.Cleanup(func() {
		_ = db.WithTenant(context.Background(), scope.OrgID, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(),
				`UPDATE organizations SET suspended_at = NULL WHERE id = $1`, scope.OrgID)
			return err
		})
	})
	cfg, err = pubs.IntakeConfig(ctx, slug)
	if err != nil {
		t.Fatalf("suspended config: %v", err)
	}
	if cfg.EggApplies() {
		t.Error("suspended org must not apply the egg UI")
	}
}

// TestEggAttachReceipt executes attach_intake_receipt end to end: with an
// active member present, the receipt row lands with purpose payment_proof.
func TestEggAttachReceipt(t *testing.T) {
	db, scope, _ := eggTestOrg(t)
	ctx := context.Background()

	// The function credits the first active member as uploader.
	staffID := uuid.New()
	var orderID, attachID uuid.UUID
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO users (id, email, password_hash, name)
			VALUES ($1, $2, 'x', 'Staff')`, staffID, "egg-staff-"+staffID.String()[:8]+"@example.local"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO organization_members (organization_id, user_id, role, status, created_at)
			VALUES ($1, $2, 'STAFF', 'ACTIVE', now())`, scope.OrgID, staffID)
		return err
	}); err != nil {
		t.Fatalf("member: %v", err)
	}
	t.Cleanup(func() {
		// Attachment first (FK to users), then the user. The org cascade
		// (setupDB cleanup, runs after) removes members/settings/counters.
		_ = db.WithTenant(context.Background(), scope.OrgID, func(tx pgx.Tx) error {
			if _, err := tx.Exec(context.Background(),
				`DELETE FROM attachments WHERE id = $1`, attachID); err != nil {
				return err
			}
			_, err := tx.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, staffID)
			return err
		})
	})

	orderID, attachID = uuid.New(), uuid.New()
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`SELECT attach_intake_receipt($1, $2, $3, $4, $5, $6, $7)`,
			orderID, scope.OrgID, "2026/10/key", "receipt.jpg",
			"image/jpeg", 12345, attachID)
		return err
	}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		var purpose, filename, mime string
		var size int64
		var uploadedBy uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT purpose, filename, mime_type, size_bytes, uploaded_by
			FROM attachments WHERE id = $1`, attachID).Scan(
			&purpose, &filename, &mime, &size, &uploadedBy); err != nil {
			return err
		}
		if purpose != "payment_proof" || filename != "receipt.jpg" ||
			mime != "image/jpeg" || size != 12345 || uploadedBy != staffID {
			t.Errorf("receipt row mismatch: %q %q %q %d %v",
				purpose, filename, mime, size, uploadedBy)
		}
		return nil
	}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}
