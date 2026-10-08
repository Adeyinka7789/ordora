package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
)

// stubOrderLookup pretends no order exists (or returns a fixed one).
type stubOrderLookup struct {
	found *order.Order
}

func (s stubOrderLookup) GetOrder(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*order.Order, error) {
	if s.found != nil {
		return s.found, nil
	}
	return nil, order.ErrNotFound
}

// stubSessionResolver always resolves to a fixed session, letting tests
// exercise tenant handlers through the real exported SessionMiddleware.
type stubSessionResolver struct {
	sess *auth.ResolvedSession
}

func (s stubSessionResolver) ResolveSession(ctx context.Context, raw string) (*auth.ResolvedSession, error) {
	return s.sess, nil
}

func testTenantSession(role tenant.Role) *auth.ResolvedSession {
	return &auth.ResolvedSession{
		Scope: tenant.TenantScope{
			OrgID:  uuid.New(),
			UserID: uuid.New(),
			Role:   role,
		},
	}
}

// authedRequest wraps h with the real session middleware and issues a POST
// carrying the session cookie and route id.
func authedRequest(t *testing.T, h http.Handler, sess *auth.ResolvedSession, orderID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	wrapped := middleware.SessionMiddleware(stubSessionResolver{sess: sess}, "ordora_session", nil, nil)
	req := httptest.NewRequest("POST", "/orders/"+orderID.String()+"/attachments", nil)
	req.AddCookie(&http.Cookie{Name: "ordora_session", Value: "test-token"})
	req.SetPathValue("id", orderID.String())
	rec := httptest.NewRecorder()
	wrapped(h).ServeHTTP(rec, req)
	return rec
}

// TestUploadToOrder_RejectsUnknownOrder guards the orphan-attachment fix:
// an arbitrary UUID must 404 before any bytes are stored, even for writers.
func TestUploadToOrder_RejectsUnknownOrder(t *testing.T) {
	h := &AttachmentHandler{OrderService: stubOrderLookup{}}
	rec := authedRequest(t,
		http.HandlerFunc(h.UploadToOrder),
		testTenantSession(tenant.RoleStaff),
		uuid.New(),
	)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown order: got %d, want 404", rec.Code)
	}
}

// TestUploadToOrder_ViewerForbidden guards role enforcement at the handler
// layer: a VIEWER must get 403 without the order lookup even running.
func TestUploadToOrder_ViewerForbidden(t *testing.T) {
	h := &AttachmentHandler{OrderService: stubOrderLookup{}}
	rec := authedRequest(t,
		http.HandlerFunc(h.UploadToOrder),
		testTenantSession(tenant.RoleViewer),
		uuid.New(),
	)
	if rec.Code != http.StatusForbidden {
		t.Errorf("viewer upload: got %d, want 403", rec.Code)
	}
}
