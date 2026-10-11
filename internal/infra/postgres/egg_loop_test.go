package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/infra/id"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

// TestEggFullLoop exercises the whole seller flow against real services:
// intake (cap consume + receipt row) -> panel -> confirm (payment +
// CONFIRMED) -> reject (CANCELLED + cap release) -> complete (COMPLETED).
func TestEggFullLoop(t *testing.T) {
	db, scope, slug := eggTestOrg(t)
	ctx := context.Background()
	const tz = "Africa/Lagos"

	// Owner membership (receipt attribution) + one crate product.
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
			VALUES ($1, $2, 'Large Crate', 5500, 'NGN',
			        TRUE, FALSE, 'in_stock', FALSE, $3, now(), now())`,
			productID, scope.OrgID, scope.UserID)
		return err
	}); err != nil {
		t.Fatalf("fixtures: %v", err)
	}

	eggRepo := postgres.NewEggRepo(db)
	if err := eggRepo.UpsertSettings(ctx, scope, &app.PoultrySettings{
		OrgID: scope.OrgID, MaxCratesPerDay: 100, AcceptingOrders: true,
		BankName: "GTBank", AccountNumber: "0123456789",
		AccountName: "Egg Farm", UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("settings: %v", err)
	}

	orderRepo := postgres.NewOrderRepo(db)
	paymentRepo := postgres.NewPaymentRepo(db)
	orderSvc := app.NewOrderService(app.OrderServiceDeps{
		DB: db, Customers: postgres.NewCustomerRepo(db),
		Products: postgres.NewProductRepo(db), Orders: orderRepo,
		OrderRead: orderRepo, Numbers: postgres.NewOrderNumberRepo(db),
		Audit: postgres.NewAuditRepo(db), Outbox: postgres.NewOutboxRepo(db),
		Measurements: postgres.NewMeasurementRepo(db), IDs: id.Generator{},
	})
	_ = orderSvc
	paymentSvc := app.NewPaymentService(app.PaymentServiceDeps{
		DB: db, Payments: paymentRepo, PaymentRead: paymentRepo,
		Orders: orderRepo, Audit: postgres.NewAuditRepo(db),
		Outbox: postgres.NewOutboxRepo(db), IDs: id.Generator{},
	})
	eggSvc := app.NewEggService(app.EggServiceDeps{
		DB: db, Store: eggRepo, Payments: paymentSvc, Orders: orderSvc,
		IDs: id.Generator{},
	})
	publicSvc := app.NewPublicOrderService(app.PublicOrderServiceDeps{
		DB:     postgres.NewPublicOrderRepo(db, id.Generator{}),
		Outbox: postgres.NewOutboxRepo(db), IDs: id.Generator{},
	})

	today, err := app.DeliveryDay(tz, "today", time.Now())
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	tomorrow, err := app.DeliveryDay(tz, "tomorrow", time.Now())
	if err != nil {
		t.Fatalf("tomorrow: %v", err)
	}
	submit := func(name, phone string, crates int64, day time.Time) uuid.UUID {
		t.Helper()
		res, err := publicSvc.Submit(ctx, app.PublicOrderInput{
			Slug: slug, CustomerName: name, CustomerPhone: phone,
			Items:        []app.PublicOrderItemInput{{ProductID: productID, QuantityScaled: crates * 1000}},
			ExpectedDate: &day,
			Egg: &app.EggIntakeInput{
				DeliveryDate: day, ReceiptKey: "test/" + uuid.NewString(),
				ReceiptFilename: "receipt.jpg", ReceiptMime: "image/jpeg", ReceiptSize: 42,
			},
		})
		if err != nil {
			t.Fatalf("submit %s: %v", name, err)
		}
		if res.TotalMinor != crates*5500 {
			t.Fatalf("submit %s: total=%d, want %d", name, res.TotalMinor, crates*5500)
		}
		return res.OrderID
	}

	// --- Intake + panel + confirm ---
	order1 := submit("Ada", "08031", 2, today)
	panel, err := eggSvc.Panel(ctx, scope, tz, "today", false)
	if err != nil {
		t.Fatalf("panel: %v", err)
	}
	if panel.Sold != 2 || len(panel.Orders) != 1 || panel.Pending != 1 {
		t.Fatalf("panel after intake: sold=%v orders=%d pending=%d",
			panel.Sold, len(panel.Orders), panel.Pending)
	}
	if panel.Orders[0].ReceiptID == nil {
		t.Fatal("panel row must carry the receipt id")
	}
	if err := eggSvc.ConfirmOrder(ctx, scope, order1); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	sum, err := eggRepo.OrderSummary(ctx, scope, order1)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.Status != "CONFIRMED" || sum.Balance() != 0 {
		t.Fatalf("after confirm: status=%s balance=%d", sum.Status, sum.Balance())
	}
	payments, err := paymentSvc.ListForOrder(ctx, scope, order1)
	if err != nil {
		t.Fatalf("payments: %v", err)
	}
	active := 0
	for _, p := range payments {
		if !p.IsReversal() && !p.IsReversed() {
			active++
			if p.Amount.Amount() != 11000 || string(p.Method) != "BANK_TRANSFER" {
				t.Errorf("payment mismatch: %d %s", p.Amount.Amount(), p.Method)
			}
		}
	}
	if active != 1 {
		t.Fatalf("want 1 active payment, got %d", active)
	}

	// --- Reject releases tomorrow's cap ---
	order2 := submit("Bola", "08032", 3, tomorrow)
	if err := eggSvc.RejectOrder(ctx, scope, order2, "REF-1"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if sum, _ := eggRepo.OrderSummary(ctx, scope, order2); sum.Status != "CANCELLED" {
		t.Fatalf("after reject: status=%s", sum.Status)
	}
	if sold, _ := eggRepo.GetSold(ctx, scope, tomorrow); sold != 0 {
		t.Fatalf("tomorrow cap must be released, sold=%v", sold)
	}

	// --- Complete walks to COMPLETED; today keeps its crates ---
	order3 := submit("Chidi", "08033", 1, today)
	if err := eggSvc.ConfirmOrder(ctx, scope, order3); err != nil {
		t.Fatalf("confirm3: %v", err)
	}
	if err := eggSvc.CompleteOrder(ctx, scope, order3); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if sum, _ := eggRepo.OrderSummary(ctx, scope, order3); sum.Status != "COMPLETED" {
		t.Fatalf("after complete: status=%s", sum.Status)
	}
	if sold, _ := eggRepo.GetSold(ctx, scope, today); sold != 3 {
		t.Fatalf("today must keep 2+1 crates, sold=%v", sold)
	}

	// --- Negative: completed orders can't be confirmed ---
	if err := eggSvc.ConfirmOrder(ctx, scope, order3); err == nil {
		t.Fatal("confirming a COMPLETED order must fail")
	}
}
