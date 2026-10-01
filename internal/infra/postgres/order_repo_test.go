package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

func mustOrderMoney(t *testing.T, minor int64, cur string) money.Money {
	t.Helper()
	m, err := money.New(minor, cur)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestOrderRepo_CreateAndGet(t *testing.T) {
	db, scope := setupDB(t)
	ctx := context.Background()

	custRepo := postgres.NewCustomerRepo(db)
	cust, _ := customer.New(uuid.New(), scope.OrgID, "Test Customer", "", "", "", "", time.Now())
	if err := custRepo.Create(ctx, scope, cust); err != nil {
		t.Fatalf("customer: %v", err)
	}

	orderRepo := postgres.NewOrderRepo(db)
	numRepo := postgres.NewOrderNumberRepo(db)
	year := time.Now().Year()

	var created *order.Order
	err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		num, err := numRepo.AllocateTx(ctx, tx, scope.OrgID, year)
		if err != nil {
			return err
		}
		o, err := order.New(uuid.New(), scope.OrgID, cust.ID, num, "2 Senator Outfits", "", "NGN", scope.UserID, time.Now())
		if err != nil {
			return err
		}
		item, err := order.NewItem(uuid.New(), "Senator outfit", 2*order.QuantityScale, mustOrderMoney(t, 90_000, "NGN"), 0)
		if err != nil {
			return err
		}
		if err := o.AddItem(item); err != nil {
			return err
		}
		if err := orderRepo.CreateTx(ctx, tx, o); err != nil {
			return err
		}
		created = o
		return nil
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	got, err := orderRepo.GetByID(ctx, scope, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "2 Senator Outfits" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.Total.Amount() != 180_000 {
		t.Fatalf("total = %d, want 180000", got.Total.Amount())
	}
	if len(got.Items) != 1 {
		t.Fatalf("items = %d", len(got.Items))
	}
	if got.Items[0].Quantity != 2*order.QuantityScale {
		t.Fatalf("quantity = %d", got.Items[0].Quantity)
	}
}

func TestOrderRepo_CrossTenantIsolation(t *testing.T) {
	db, scopeA := setupDB(t)
	ctx := context.Background()

	custRepo := postgres.NewCustomerRepo(db)
	cust, _ := customer.New(uuid.New(), scopeA.OrgID, "Alice", "", "", "", "", time.Now())
	_ = custRepo.Create(ctx, scopeA, cust)

	orderRepo := postgres.NewOrderRepo(db)
	numRepo := postgres.NewOrderNumberRepo(db)
	year := time.Now().Year()

	var orderA *order.Order
	_ = db.WithTenant(ctx, scopeA.OrgID, func(tx pgx.Tx) error {
		num, _ := numRepo.AllocateTx(ctx, tx, scopeA.OrgID, year)
		o, _ := order.New(uuid.New(), scopeA.OrgID, cust.ID, num, "Order A", "", "NGN", scopeA.UserID, time.Now())
		item, _ := order.NewItem(uuid.New(), "Widget", 1*order.QuantityScale, mustOrderMoney(t, 1000, "NGN"), 0)
		_ = o.AddItem(item)
		_ = orderRepo.CreateTx(ctx, tx, o)
		orderA = o
		return nil
	})

	orgB := uuid.New()
	userB := uuid.New()
	_ = db.WithTenant(ctx, orgB, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO organizations (id, name, slug, currency, timezone)
			VALUES ($1, $2, $3, 'NGN', 'Africa/Lagos')`, orgB, "Org B", "orgb-"+orgB.String()[:8])
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO users (id, email, password_hash, name)
			VALUES ($1, $2, 'x', 'B')`, userB, "b-"+userB.String()[:8]+"@example.local")
		return err
	})
	t.Cleanup(func() {
		_ = db.WithTenant(ctx, orgB, func(tx pgx.Tx) error {
			_, _ = tx.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgB)
			_, _ = tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userB)
			return nil
		})
	})

	scopeB := tenant.TenantScope{OrgID: orgB, UserID: userB, Role: tenant.RoleOwner}
	_, err := orderRepo.GetByID(ctx, scopeB, orderA.ID)
	if !errors.Is(err, order.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
