package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/group"
	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

// TestGroupRepo_Flow covers create → add order → totals → remove → delete.
func TestGroupRepo_Flow(t *testing.T) {
	db, scope := setupDB(t)
	ctx := context.Background()
	repo := postgres.NewGroupRepo(db)

	custRepo := postgres.NewCustomerRepo(db)
	cust, _ := customer.New(uuid.New(), scope.OrgID, "Ada", "", "08031234567", "", "", time.Now())
	if err := custRepo.Create(ctx, scope, cust); err != nil {
		t.Fatalf("customer: %v", err)
	}
	orderRepo := postgres.NewOrderRepo(db)
	numRepo := postgres.NewOrderNumberRepo(db)
	var orderID uuid.UUID
	err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		num, err := numRepo.AllocateTx(ctx, tx, scope.OrgID, time.Now().Year())
		if err != nil {
			return err
		}
		o, err := order.New(uuid.New(), scope.OrgID, cust.ID, num, "Agbada", "", "NGN", scope.UserID, time.Now())
		if err != nil {
			return err
		}
		amt, _ := money.New(50_000, "NGN")
		item, err := order.NewItem(uuid.New(), "Agbada", order.QuantityScale, amt, 0)
		if err != nil {
			return err
		}
		if err := o.AddItem(item); err != nil {
			return err
		}
		if err := orderRepo.CreateTx(ctx, tx, o); err != nil {
			return err
		}
		orderID = o.ID
		return nil
	})
	if err != nil {
		t.Fatalf("order: %v", err)
	}

	g, err := group.New(uuid.New(), scope.OrgID, "Test Wedding", nil, "", scope.UserID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, scope, g); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.AddOrder(ctx, scope, g.ID, orderID); err != nil {
		t.Fatalf("add order: %v", err)
	}
	// Adding twice must fail (already grouped).
	if err := repo.AddOrder(ctx, scope, g.ID, orderID); err == nil {
		t.Fatal("double-add should fail")
	}

	d, err := repo.Get(ctx, scope, g.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if d.MemberCount != 1 || d.TotalMinor != 50_000 || d.Currency != "NGN" {
		t.Fatalf("unexpected detail: %+v", d)
	}

	rows, total, err := repo.List(ctx, scope, "wedding", 20, 0)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].TotalMinor != 50_000 {
		t.Fatalf("list: %v %d %+v", err, total, rows)
	}

	if _, err := repo.FindByOrder(ctx, scope, orderID); err != nil {
		t.Fatalf("find by order: %v", err)
	}
	if err := repo.RemoveOrder(ctx, scope, g.ID, orderID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := repo.FindByOrder(ctx, scope, orderID); err == nil {
		t.Fatal("removed order should not resolve a group")
	}
	if err := repo.Delete(ctx, scope, g.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, scope, g.ID); err == nil {
		t.Fatal("deleted group should be gone")
	}
}
