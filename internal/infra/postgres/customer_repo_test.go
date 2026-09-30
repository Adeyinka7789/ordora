package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

// setupDB connects using the same env vars the server uses. If it fails,
// the test is skipped — this lets `go test ./...` pass on machines without
// Postgres.
func setupDB(t *testing.T) (*postgres.DB, tenant.TenantScope) {
	t.Helper()

	user := os.Getenv("ORDORA_DB_USER")
	if user == "" {
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

	// Create an isolated test org + user.
	orgID := uuid.New()
	userID := uuid.New()

	// Organizations and users are RLS-protected. We must create them via
	// a role that can bypass, or set the tenant first. For the test we use
	// the create_organization path: set tenant to the org we're about to
	// create, then insert.
	err = db.WithTenant(context.Background(), orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `
			INSERT INTO organizations (id, name, slug, currency, timezone)
			VALUES ($1, $2, $3, 'NGN', 'Africa/Lagos')
		`, orgID, "Test Org", "test-"+orgID.String()[:8])
		if err != nil {
			return err
		}
		_, err = tx.Exec(context.Background(), `
			INSERT INTO users (id, email, password_hash, name)
			VALUES ($1, $2, $3, $4)
		`, userID, "test-"+userID.String()[:8]+"@example.local", "x", "Test User")
		return err
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	t.Cleanup(func() {
		// Clean up: delete org (cascades to customers)
		_ = db.WithTenant(context.Background(), orgID, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
			return err
		})
		_ = db.WithTenant(context.Background(), orgID, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
			return err
		})
		db.Close()
	})

	scope := tenant.TenantScope{OrgID: orgID, UserID: userID, Role: tenant.RoleOwner}
	return db, scope
}

func TestCustomerRepo_CreateAndGet(t *testing.T) {
	db, scope := setupDB(t)
	repo := postgres.NewCustomerRepo(db)
	ctx := context.Background()

	c, err := customer.New(uuid.New(), scope.OrgID, "Alice", "alice@example.com", "+234 803 123 4567", "1 Main St", "vip", time.Now())
	if err != nil {
		t.Fatalf("domain: %v", err)
	}
	if err := repo.Create(ctx, scope, c); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetByID(ctx, scope, c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Alice" || got.Email != "alice@example.com" {
		t.Fatalf("got %+v", got)
	}
}

func TestCustomerRepo_CrossTenantIsolation(t *testing.T) {
	db, scopeA := setupDB(t)
	repo := postgres.NewCustomerRepo(db)
	ctx := context.Background()

	// Create a customer in org A.
	c, _ := customer.New(uuid.New(), scopeA.OrgID, "Alice", "", "", "", "", time.Now())
	if err := repo.Create(ctx, scopeA, c); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Set up a second org and try to read the same customer id from its scope.
	orgB := uuid.New()
	userB := uuid.New()
	if err := db.WithTenant(ctx, orgB, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO organizations (id, name, slug, currency, timezone)
			VALUES ($1, $2, $3, 'NGN', 'Africa/Lagos')`, orgB, "Org B", "orgb-"+orgB.String()[:8])
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO users (id, email, password_hash, name)
			VALUES ($1, $2, $3, $4)`, userB, "b-"+userB.String()[:8]+"@example.local", "x", "B")
		return err
	}); err != nil {
		t.Fatalf("setup B: %v", err)
	}
	t.Cleanup(func() {
		_ = db.WithTenant(ctx, orgB, func(tx pgx.Tx) error {
			_, _ = tx.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgB)
			_, _ = tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userB)
			return nil
		})
	})

	scopeB := tenant.TenantScope{OrgID: orgB, UserID: userB, Role: tenant.RoleOwner}
	_, err := repo.GetByID(ctx, scopeB, c.ID)
	if !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant read, got %v", err)
	}
}

func TestCustomerRepo_List_Search(t *testing.T) {
	db, scope := setupDB(t)
	repo := postgres.NewCustomerRepo(db)
	ctx := context.Background()

	names := []string{"Alice Smith", "Bob Jones", "Charlie Brown"}
	for _, n := range names {
		c, _ := customer.New(uuid.New(), scope.OrgID, n, "", "", "", "", time.Now())
		if err := repo.Create(ctx, scope, c); err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
	}

	// No filter: all three.
	res, err := repo.List(ctx, scope, postgres.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if res.Total != 3 {
		t.Fatalf("total = %d, want 3", res.Total)
	}

	// Search "li" matches Alice and Charlie.
	res, err = repo.List(ctx, scope, postgres.ListOptions{Query: "li", Limit: 10})
	if err != nil {
		t.Fatalf("list search: %v", err)
	}
	if res.Total != 2 {
		t.Fatalf("search total = %d, want 2", res.Total)
	}
}
