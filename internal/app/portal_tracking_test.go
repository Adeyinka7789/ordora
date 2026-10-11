package app

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/portal"
)

// fakePortalRepo serves canned portal rows for the tracking-link tests.
type fakePortalRepo struct {
	byID   map[uuid.UUID]*portal.Order
	byHash map[string]*portal.Order
}

func (f *fakePortalRepo) GetByTokenHash(_ context.Context, h []byte) (*portal.Order, error) {
	if o, ok := f.byHash[string(h)]; ok {
		return o, nil
	}
	return nil, ErrPortalNotFound
}

func (f *fakePortalRepo) GetByOrderID(_ context.Context, id uuid.UUID) (*portal.Order, error) {
	if o, ok := f.byID[id]; ok {
		return o, nil
	}
	return nil, ErrPortalNotFound
}

func (f *fakePortalRepo) ListItems(_ context.Context, _ uuid.UUID) ([]portal.Item, error) {
	return nil, nil
}

func (f *fakePortalRepo) ListPayments(_ context.Context, _ uuid.UUID) ([]portal.Payment, error) {
	return nil, nil
}

func (f *fakePortalRepo) GetPortalAttachment(_ context.Context, _ []byte, _ uuid.UUID) (*PortalAttachment, error) {
	return nil, ErrPortalNotFound
}

func (f *fakePortalRepo) GetPublicProductImage(_ context.Context, _ uuid.UUID) (*PublicProductImage, error) {
	return nil, ErrPortalNotFound
}

// TestPortalLoadTrackingToken proves emailed tracking links resolve without
// any stored bearer: HMAC verify → by-id lookup → same PortalView shape.
func TestPortalLoadTrackingToken(t *testing.T) {
	const secret = "test-secret-do-not-use-in-prod"
	orderID := uuid.New()
	orgID := uuid.New()
	row := &portal.Order{OrderID: orderID, OrganizationID: orgID, OrderNumber: "ORD-1", Title: "Gown"}
	svc := NewPortalService(PortalServiceDeps{
		Repo:           &fakePortalRepo{byID: map[uuid.UUID]*portal.Order{orderID: row}},
		TrackingSecret: secret,
	})

	raw, err := TrackingToken(secret, orderID)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	v, err := svc.Load(context.Background(), raw)
	if err != nil {
		t.Fatalf("tracking load: %v", err)
	}
	if v.OrderID != orderID || v.OrderNumber != "ORD-1" {
		t.Errorf("wrong view: %+v", v)
	}

	// Unknown order id behind a valid signature is a 404, not a 500.
	other, _ := TrackingToken(secret, uuid.New())
	if _, err := svc.Load(context.Background(), other); !errors.Is(err, ErrPortalNotFound) {
		t.Errorf("unknown order must 404, got %v", err)
	}
	// Garbage never reaches the by-id path.
	if _, err := svc.Load(context.Background(), "nope"); !errors.Is(err, ErrPortalNotFound) {
		t.Errorf("garbage must 404, got %v", err)
	}
	// No secret configured: tracking links are dead, bearer path unaffected.
	bearerRow := &portal.Order{OrderID: uuid.New(), OrderNumber: "ORD-2"}
	bearerHash := HashToken("bearer-raw")
	nosecret := NewPortalService(PortalServiceDeps{
		Repo: &fakePortalRepo{byHash: map[string]*portal.Order{string(bearerHash): bearerRow}},
	})
	if _, err := nosecret.Load(context.Background(), raw); !errors.Is(err, ErrPortalNotFound) {
		t.Errorf("tracking without secret must 404, got %v", err)
	}
	v, err = nosecret.Load(context.Background(), "bearer-raw")
	if err != nil || v.OrderNumber != "ORD-2" {
		t.Errorf("bearer path must keep working, got %+v, %v", v, err)
	}
}
