package app

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

type fakePublicDB struct {
	org      *PublicOrg
	products []PublicProduct
	result   *PublicOrderResult
	err      error
	got      PublicOrderInput
}

func (f *fakePublicDB) CreatePublicOrder(_ context.Context, in PublicOrderInput) (*PublicOrderResult, error) {
	f.got = in
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func (f *fakePublicDB) LookupOrgBySlug(_ context.Context, _ string) (*PublicOrg, error) {
	return f.org, nil
}

func (f *fakePublicDB) ListProductsBySlug(_ context.Context, _ string) ([]PublicProduct, error) {
	return f.products, nil
}

func testPublicSvc(f *fakePublicDB) *PublicOrderService {
	return NewPublicOrderService(PublicOrderServiceDeps{DB: f})
}

func TestPublicSubmit_ItemsOnly(t *testing.T) {
	orgID := uuid.New()
	f := &fakePublicDB{
		org:    &PublicOrg{ID: orgID, Name: "Shop", Currency: "NGN"},
		result: &PublicOrderResult{OrderID: uuid.New(), OrderNumber: "ORD-1", TotalMinor: 10000, Currency: "NGN"},
	}
	svc := testPublicSvc(f)
	res, err := svc.Submit(context.Background(), PublicOrderInput{
		Slug: "shop", CustomerName: "Ada", CustomerEmail: "ada@x.com",
		Items: []PublicOrderItemInput{{ProductID: uuid.New(), QuantityScaled: 2000}},
	})
	if err != nil {
		t.Fatalf("items-only submit rejected: %v", err)
	}
	if res.TotalMinor != 10000 || len(f.got.Items) != 1 {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestPublicSubmit_RequiresDescOrItems(t *testing.T) {
	svc := testPublicSvc(&fakePublicDB{})
	_, err := svc.Submit(context.Background(), PublicOrderInput{
		Slug: "shop", CustomerName: "Ada", CustomerEmail: "ada@x.com",
	})
	if err != ErrPublicOrderEmpty {
		t.Errorf("expected ErrPublicOrderEmpty, got %v", err)
	}
}

func TestPublicSubmit_RejectsBadItems(t *testing.T) {
	svc := testPublicSvc(&fakePublicDB{})
	_, err := svc.Submit(context.Background(), PublicOrderInput{
		Slug: "shop", CustomerName: "Ada", CustomerEmail: "ada@x.com",
		Items: []PublicOrderItemInput{{ProductID: uuid.New(), QuantityScaled: 0}},
	})
	if err != ErrPublicQtyInvalid {
		t.Errorf("expected ErrPublicQtyInvalid, got %v", err)
	}

	many := make([]PublicOrderItemInput, MaxPublicItems+1)
	for i := range many {
		many[i] = PublicOrderItemInput{ProductID: uuid.New(), QuantityScaled: 1000}
	}
	_, err = svc.Submit(context.Background(), PublicOrderInput{
		Slug: "shop", CustomerName: "Ada", CustomerEmail: "ada@x.com", Items: many,
	})
	if err != ErrPublicTooManyItems {
		t.Errorf("expected ErrPublicTooManyItems, got %v", err)
	}
}
