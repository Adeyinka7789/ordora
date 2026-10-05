package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/measurement"
	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

func TestMeasurementRepo_TemplatesAndSave(t *testing.T) {
	db, scope := setupDB(t)
	ctx := context.Background()
	repo := postgres.NewMeasurementRepo(db)

	// System seeds: male + unisex visible for male...
	male, err := repo.ListTemplates(ctx, scope, measurement.GenderMale)
	if err != nil {
		t.Fatalf("list male: %v", err)
	}
	if len(male) < 6 {
		t.Fatalf("expected at least 6 male-visible templates, got %d", len(male))
	}
	female, err := repo.ListTemplates(ctx, scope, measurement.GenderFemale)
	if err != nil {
		t.Fatalf("list female: %v", err)
	}
	if len(female) < 6 {
		t.Fatalf("expected at least 6 female-visible templates, got %d", len(female))
	}
	// Tenant templates sort before system ones.
	var agbada measurement.Template
	for _, tm := range male {
		if tm.Garment == "agbada" {
			agbada = tm
		}
	}
	if agbada.ID == uuid.Nil {
		t.Fatal("seeded agbada template not found")
	}

	got, err := repo.GetTemplate(ctx, scope, agbada.ID)
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	if len(got.Fields) == 0 || got.Fields[0].Key == "" {
		t.Fatalf("template fields not parsed: %+v", got)
	}
	if _, err := repo.GetTemplate(ctx, scope, uuid.New()); err == nil {
		t.Fatal("unknown template should fail")
	}

	// Save + load an order measurement (needs a real order).
	custRepo := postgres.NewCustomerRepo(db)
	cust, _ := customer.New(uuid.New(), scope.OrgID, "Ada", "", "", "", "", time.Now())
	if err := custRepo.Create(ctx, scope, cust); err != nil {
		t.Fatalf("customer: %v", err)
	}
	orderRepo := postgres.NewOrderRepo(db)
	numRepo := postgres.NewOrderNumberRepo(db)
	var orderID uuid.UUID
	err = db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
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
		m := measurement.Measurement{
			ID:             uuid.New(),
			OrganizationID: scope.OrgID,
			OrderID:        o.ID,
			TemplateID:     agbada.ID,
			Gender:         agbada.Gender,
			Garment:        agbada.Garment,
			TemplateName:   agbada.Name,
			Values:         map[string]string{"neck": "16", "chest": "42", "shoulder": "19", "sleeve": "25", "full_length": "60"},
			SnapshotFields: agbada.Fields,
			CreatedBy:      scope.UserID,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := m.Validate(agbada); err != nil {
			return err
		}
		return repo.SaveMeasurementTx(ctx, tx, m)
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := repo.GetByOrder(ctx, scope, orderID)
	if err != nil {
		t.Fatalf("get by order: %v", err)
	}
	if loaded.Values["chest"] != "42" || loaded.TemplateName != "Agbada" {
		t.Fatalf("unexpected loaded measurement: %+v", loaded)
	}

	// Replace on re-save (one row per order).
	err = db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		m := measurement.Measurement{
			ID:             uuid.New(),
			OrganizationID: scope.OrgID,
			OrderID:        orderID,
			TemplateID:     agbada.ID,
			Gender:         agbada.Gender,
			Garment:        agbada.Garment,
			TemplateName:   agbada.Name,
			Values:         map[string]string{"chest": "43", "full_length": "60", "neck": "16", "shoulder": "19", "sleeve": "25"},
			SnapshotFields: agbada.Fields,
			CreatedBy:      scope.UserID,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		return repo.SaveMeasurementTx(ctx, tx, m)
	})
	if err != nil {
		t.Fatalf("re-save: %v", err)
	}
	loaded, err = repo.GetByOrder(ctx, scope, orderID)
	if err != nil {
		t.Fatalf("get after re-save: %v", err)
	}
	if loaded.Values["chest"] != "43" {
		t.Fatalf("expected replaced values, got %+v", loaded.Values)
	}

	// Unknown order → ErrNotFound.
	if _, err := repo.GetByOrder(ctx, scope, uuid.New()); err == nil {
		t.Fatal("missing measurement should fail")
	}
}
