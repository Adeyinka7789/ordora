package jobs

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

// openRelayDB connects with the server's env vars. Skips when Postgres
// is unavailable, like the other DB-backed tests.
func openRelayDB(t *testing.T) *postgres.DB {
	t.Helper()
	if os.Getenv("ORDORA_DB_USER") == "" {
		t.Skip("ORDORA_DB_USER not set; skipping DB-backed test")
	}
	dsn := "postgres://" + os.Getenv("ORDORA_DB_USER") + ":" +
		os.Getenv("ORDORA_DB_PASSWORD") + "@" +
		os.Getenv("ORDORA_DB_HOST") + ":" +
		os.Getenv("ORDORA_DB_PORT") + "/" +
		os.Getenv("ORDORA_DB_NAME") + "?sslmode=" +
		os.Getenv("ORDORA_DB_SSLMODE")
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Skipf("cannot connect to DB: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func insertOutboxRow(t *testing.T, db *postgres.DB, orgID uuid.UUID, event string, payload map[string]any) uuid.UUID {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	id := uuid.New()
	// RLS needs a tenant context for direct writes (the relay's own
	// reads go through definer functions instead).
	if err := db.WithTenant(context.Background(), orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `
			INSERT INTO outbox (id, organization_id, event_name, payload)
			VALUES ($1, $2, $3, $4)`, id, orgID, event, raw)
		return err
	}); err != nil {
		t.Fatalf("insert outbox: %v", err)
	}
	return id
}

func countJobs(t *testing.T, db *postgres.DB, orgID uuid.UUID, kind, recipient string) int {
	t.Helper()
	var n int
	if err := db.WithTenant(context.Background(), orgID, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `
			SELECT count(*) FROM notification_jobs WHERE kind = $1 AND recipient = $2`,
			kind, recipient).Scan(&n)
	}); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	return n
}

// TestRelay_BookingEventsDispatchOnce proves the booking notifications land
// exactly once even when the relay ticks twice (retry safety): the second
// tick finds dispatched rows, and the idempotency key would dedup anyway.
func TestRelay_BookingEventsDispatchOnce(t *testing.T) {
	db := openRelayDB(t)
	ctx := context.Background()
	orgID := uuid.New()

	submittedID := insertOutboxRow(t, db, orgID, "booking.submitted", map[string]any{
		"OrderID": uuid.NewString(), "OrderNumber": "ORD-B1",
		"CustomerName": "Ada", "CustomerEmail": "ada@example.com",
		"CustomerPhone": "0803", "Description": "Booking: Braids",
		"OrgName": "Glam", "OrgEmail": "owner@example.com",
		"AdminURL": "/dashboard/bookings?date=2026-10-12",
	})
	reschedID := insertOutboxRow(t, db, orgID, "booking.rescheduled", map[string]any{
		"OrderID": uuid.NewString(), "OrderNumber": "ORD-B1",
		"CustomerName": "Ada", "CustomerEmail": "ada@example.com",
		"StartLabel": "Mon 12 Jan, 10:00", "PortalURL": "/o/tok",
	})
	t.Cleanup(func() {
		_ = db.WithTenant(context.Background(), orgID, func(tx pgx.Tx) error {
			if _, err := tx.Exec(context.Background(),
				`DELETE FROM notification_jobs WHERE organization_id = $1`, orgID); err != nil {
				return err
			}
			_, err := tx.Exec(context.Background(),
				`DELETE FROM outbox WHERE id = ANY($1)`, []uuid.UUID{submittedID, reschedID})
			return err
		})
	})

	relay := NewOutboxRelay(db, OutboxRelayConfig{})
	if err := relay.tick(ctx); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	if err := relay.tick(ctx); err != nil {
		t.Fatalf("tick 2: %v", err)
	}

	if n := countJobs(t, db, orgID, "PUBLIC_INTAKE_RECEIVED", "owner@example.com"); n != 1 {
		t.Errorf("submitted owner mail: want exactly 1 job, got %d", n)
	}
	if n := countJobs(t, db, orgID, "BOOKING_RESCHEDULED", "ada@example.com"); n != 1 {
		t.Errorf("rescheduled customer mail: want exactly 1 job, got %d", n)
	}

	// Deterministic idempotency keys tie each job to its outbox row.
	var key string
	if err := db.WithTenant(ctx, orgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT idempotency_key FROM notification_jobs
			WHERE kind = 'PUBLIC_INTAKE_RECEIVED' AND recipient = 'owner@example.com'`,
		).Scan(&key)
	}); err != nil {
		t.Fatalf("key: %v", err)
	}
	want := "booking.submitted:" + submittedID.String() + ":owner@example.com"
	if key != want {
		t.Errorf("idempotency key: got %q want %q", key, want)
	}
}
