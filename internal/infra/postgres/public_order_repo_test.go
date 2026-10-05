package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

// TestPublicOrderRepo_LookupOrg exercises lookup_public_org end to end.
// It guards against PL/pgSQL variable/column ambiguity (SQLSTATE 42702),
// which once made every public intake link fail with
// "could not load business".
func TestPublicOrderRepo_LookupOrg(t *testing.T) {
	db, scope := setupDB(t)
	ctx := context.Background()
	repo := postgres.NewPublicOrderRepo(db, nil)

	slug := "test-" + strings.ToLower(scope.OrgID.String()[:8])
	got, err := repo.LookupOrgBySlug(ctx, slug)
	if err != nil {
		t.Fatalf("lookup %q: %v", slug, err)
	}
	if got.Name == "" || got.Currency == "" {
		t.Fatalf("unexpected org projection: %+v", got)
	}
	if got.Slug == "" {
		t.Fatalf("expected slug in projection: %+v", got)
	}

	if _, err := repo.LookupOrgBySlug(ctx, "no-such-shop-xyz"); err == nil {
		t.Fatal("unknown slug should fail")
	} else if err != app.ErrPublicOrgNotFound {
		t.Fatalf("unknown slug should map to ErrPublicOrgNotFound, got %v", err)
	}
}

// TestPublicOrderRepo_ProductsAndCreate covers the storefront surface:
// product listing plus order creation with snapshotted lines.
func TestPublicOrderRepo_ProductsAndCreate(t *testing.T) {
	db, scope := setupDB(t)
	ctx := context.Background()
	repo := postgres.NewPublicOrderRepo(db, testIDGen{})
	slug := "test-" + strings.ToLower(scope.OrgID.String()[:8])

	productID := uuid.New()
	err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO organization_members (organization_id, user_id, role, status)
			VALUES ($1, $2, 'OWNER', 'ACTIVE')
		`, scope.OrgID, scope.UserID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO products (id, organization_id, name, description, unit_price_minor, currency, active, created_by)
			VALUES ($1, $2, 'Meat Pie', 'Tasty', 150000, 'NGN', true, $3)
		`, productID, scope.OrgID, scope.UserID)
		return err
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	products, err := repo.ListProductsBySlug(ctx, slug)
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	if len(products) != 1 || products[0].UnitPriceMinor != 150000 || products[0].Name != "Meat Pie" {
		t.Fatalf("unexpected products: %+v", products)
	}

	res, err := repo.CreatePublicOrder(ctx, app.PublicOrderInput{
		Slug: slug, CustomerName: "Ada", CustomerEmail: "ada@example.com",
		Items: []app.PublicOrderItemInput{{ProductID: productID, QuantityScaled: 2000}},
	})
	if err != nil {
		t.Fatalf("create with items: %v", err)
	}
	if res.TotalMinor != 300000 {
		t.Fatalf("expected total 300000 (2 x 150000), got %d", res.TotalMinor)
	}
	if res.Currency != "NGN" || res.OrderNumber == "" {
		t.Fatalf("unexpected result: %+v", res)
	}

	// Unknown products are skipped, not fatal.
	res, err = repo.CreatePublicOrder(ctx, app.PublicOrderInput{
		Slug: slug, CustomerName: "Ada", CustomerEmail: "ada@example.com",
		Description: "Custom cake",
		Items:       []app.PublicOrderItemInput{{ProductID: uuid.New(), QuantityScaled: 1000}},
	})
	if err != nil {
		t.Fatalf("create with unknown product: %v", err)
	}
	if res.TotalMinor != 0 {
		t.Fatalf("unknown products must not contribute, got %d", res.TotalMinor)
	}
}

type testIDGen struct{}

func (testIDGen) New() uuid.UUID { return uuid.New() }
