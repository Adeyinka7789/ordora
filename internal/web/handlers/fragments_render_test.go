package handlers

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/payment"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// TestFragmentsExecute renders representative pages/fragments with realistic
// data. Template/data mismatches (e.g. referencing a field the page struct
// doesn't carry) only surface at execution time — go build won't catch
// them — so this is the regression net for "page renders 500".
func TestFragmentsExecute(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	now := time.Now()
	amt := money.MustNew(5000, "NGN")

	ledgerRows := []postgres.LedgerRow{{
		Payment: &payment.Payment{
			ID:             uuid.New(),
			OrganizationID: uuid.New(),
			OrderID:        uuid.New(),
			Amount:         amt,
			Method:         payment.MethodCash,
			Reference:      "REF-1",
			PaidAt:         now,
			CreatedBy:      uuid.New(),
			CreatedAt:      now,
		},
		OrderID:      uuid.New(),
		OrderNumber:  "ORD-2026-00001",
		CustomerName: "Ada",
	}}
	ledgerData := ledgerIndexPage{
		Title:         "Payments & Ledger",
		Rows:          ledgerRows,
		Totals:        &postgres.LedgerTotals{Count: 1, TotalMinor: 5000},
		Currency:      "NGN",
		ExportAllowed: true,
		Pagination:    newPagination(1, 20, 0),
	}
	ledgerData.Totals.ByMethod = []postgres.LedgerMethodTotal{
		{Method: payment.MethodCash, Count: 1, TotalMinor: 5000},
	}

	complaint := app.Complaint{
		ID:        uuid.New(),
		OrgID:     uuid.New(),
		UserID:    uuid.New(),
		Subject:   "Test subject",
		Message:   "Test message",
		Status:    app.ComplaintOpen,
		CreatedAt: now,
		UpdatedAt: now,
	}

	cases := map[string]any{
		"payments/index.html": ledgerData,
		"payments/_table.html": ledgerData,
		"support/index.html": supportIndexPage{
			Title:      "Support",
			Complaints: []app.Complaint{complaint},
			Total:      1,
		},
		"support/show.html": supportShowPage{
			Title: "Test subject",
			Thread: &app.ComplaintThread{
				Complaint: complaint,
				Replies: []app.ComplaintReplyView{{
					Reply:      app.ComplaintReply{Body: "Admin reply", CreatedAt: now},
					AuthorName: "Ordora Support",
					IsAdmin:    true,
				}},
			},
		},
		"notifications/index.html": notificationsPage{
			Title: "Notifications",
			Rows: []app.Notification{{
				ID:        uuid.New(),
				Kind:      app.NotifKindBroadcast,
				Title:     "Hello",
				Body:      "World",
				CreatedAt: now,
			}},
			Pagination: newPagination(1, 20, 0),
		},
	}

	for name, data := range cases {
		if _, err := r.Raw(name, data); err != nil {
			t.Errorf("%s: execute: %v", name, err)
		}
	}
}
