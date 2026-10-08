package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/product"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// stubProductReader serves canned catalog products for pricing tests.
type stubProductReader struct {
	byID map[uuid.UUID]*product.Product
	err  error
}

func (s stubProductReader) GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*product.Product, error) {
	if s.err != nil {
		return nil, s.err
	}
	p, ok := s.byID[id]
	if !ok {
		return nil, product.ErrNotFound
	}
	return p, nil
}

func quoteTestProduct(quote bool) *product.Product {
	price, _ := money.New(50000, "NGN")
	p, _ := product.New(uuid.New(), uuid.New(), "Gown", "", "", price, uuid.New(), time.Now())
	_ = p.ApplyCatalog(product.CatalogDetails{QuoteOnly: quote}, time.Now())
	return p
}

// TestResolveItemPrice_QuoteOnlyForcedZero is the server-side counterpart of
// the public SQL rule: a staff line linking a quote_only product is priced
// at 0 no matter what the form submitted (the picker fills list price).
func TestResolveItemPrice_QuoteOnlyForcedZero(t *testing.T) {
	quote := quoteTestProduct(true)
	fixed := quoteTestProduct(false)
	svc := NewOrderService(OrderServiceDeps{
		Products: stubProductReader{byID: map[uuid.UUID]*product.Product{
			quote.ID: quote, fixed.ID: fixed,
		}},
	})
	scope := tenant.TenantScope{OrgID: uuid.New(), UserID: uuid.New(), Role: tenant.RoleStaff}
	ctx := context.Background()

	in := CreateOrderItemInput{Description: "Gown", QuantityScaled: 1000, UnitPriceMinor: 50000, ProductID: quote.ID}
	if err := svc.resolveItemPrice(ctx, scope, &in); err != nil {
		t.Fatalf("quote-only resolve: %v", err)
	}
	if in.UnitPriceMinor != 0 {
		t.Errorf("quote-only line price = %d, want 0", in.UnitPriceMinor)
	}
	if in.ProductID != quote.ID {
		t.Error("quote-only line must keep its product link")
	}

	in = CreateOrderItemInput{Description: "Senator", QuantityScaled: 1000, UnitPriceMinor: 30000, ProductID: fixed.ID}
	if err := svc.resolveItemPrice(ctx, scope, &in); err != nil {
		t.Fatalf("fixed-price resolve: %v", err)
	}
	if in.UnitPriceMinor != 30000 {
		t.Errorf("fixed-price line price = %d, want submitted 30000", in.UnitPriceMinor)
	}

	// Unknown/deleted product: link cleared, submitted values kept so the
	// line degrades to free-hand instead of failing the order.
	in = CreateOrderItemInput{Description: "Old", QuantityScaled: 1000, UnitPriceMinor: 999, ProductID: uuid.New()}
	if err := svc.resolveItemPrice(ctx, scope, &in); err != nil {
		t.Fatalf("unknown product resolve: %v", err)
	}
	if in.ProductID != uuid.Nil {
		t.Error("unknown product link must be cleared")
	}
	if in.UnitPriceMinor != 999 {
		t.Errorf("unknown product price = %d, want submitted 999", in.UnitPriceMinor)
	}

	// Transient lookup failure aborts: the line keeps its link and the
	// error propagates so create/update fails instead of silently
	// converting a catalog line into a free-hand one.
	dbErr := errors.New("connection reset")
	broken := NewOrderService(OrderServiceDeps{
		Products: stubProductReader{err: dbErr},
	})
	in = CreateOrderItemInput{Description: "Gown", QuantityScaled: 1000, UnitPriceMinor: 50000, ProductID: quote.ID}
	if err := broken.resolveItemPrice(ctx, scope, &in); !errors.Is(err, dbErr) {
		t.Errorf("transient error must propagate, got %v", err)
	}
	if in.ProductID != quote.ID || in.UnitPriceMinor != 50000 {
		t.Errorf("failed lookup must leave input untouched, got %+v", in)
	}

	// Free-hand lines and nil readers are untouched.
	in = CreateOrderItemInput{Description: "Free", QuantityScaled: 1000, UnitPriceMinor: 123}
	if err := svc.resolveItemPrice(ctx, scope, &in); err != nil {
		t.Fatalf("free-hand resolve: %v", err)
	}
	if in.UnitPriceMinor != 123 {
		t.Errorf("free-hand price = %d, want 123", in.UnitPriceMinor)
	}
	bare := NewOrderService(OrderServiceDeps{})
	in = CreateOrderItemInput{Description: "Gown", QuantityScaled: 1000, UnitPriceMinor: 50000, ProductID: quote.ID}
	if err := bare.resolveItemPrice(ctx, scope, &in); err != nil {
		t.Fatalf("nil reader resolve: %v", err)
	}
	if in.UnitPriceMinor != 50000 {
		t.Errorf("nil reader must skip enforcement, got %d", in.UnitPriceMinor)
	}
}
