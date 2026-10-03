package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/attachment"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

func mkAttachment(t *testing.T, scopeOrg, uploader, entityID uuid.UUID, name string, createdAt time.Time) *attachment.Attachment {
	t.Helper()
	a, err := attachment.New(
		uuid.New(), scopeOrg,
		attachment.EntityPayment, entityID,
		"test/"+uuid.NewString(), name, "image/png", 1024,
		uploader, createdAt,
	)
	if err != nil {
		t.Fatalf("domain: %v", err)
	}
	return a
}

// TestAttachmentRepo_ListForEntities verifies the batched listing used by
// the order page: one query for many payments, grouped by payment, newest
// first — the replacement for the old per-payment loop (N+1).
func TestAttachmentRepo_ListForEntities(t *testing.T) {
	db, scope := setupDB(t)
	repo := postgres.NewAttachmentRepo(db)
	ctx := context.Background()

	payA, payB := uuid.New(), uuid.New()
	now := time.Now()
	seed := []*attachment.Attachment{
		mkAttachment(t, scope.OrgID, scope.UserID, payA, "a-old.png", now.Add(-2*time.Hour)),
		mkAttachment(t, scope.OrgID, scope.UserID, payA, "a-new.png", now.Add(-time.Hour)),
		mkAttachment(t, scope.OrgID, scope.UserID, payB, "b-only.png", now.Add(-30*time.Minute)),
	}
	for _, a := range seed {
		if err := repo.Create(ctx, scope, a); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	got, err := repo.ListForEntities(ctx, scope, attachment.EntityPayment, []uuid.UUID{payA, payB, uuid.New()})
	if err != nil {
		t.Fatalf("list batch: %v", err)
	}
	if len(got[payA]) != 2 {
		t.Fatalf("payA: got %d attachments, want 2", len(got[payA]))
	}
	if got[payA][0].Filename != "a-new.png" {
		t.Errorf("payA: newest first, got %q first", got[payA][0].Filename)
	}
	if len(got[payB]) != 1 || got[payB][0].Filename != "b-only.png" {
		t.Errorf("payB: unexpected group %+v", got[payB])
	}

	// Empty input: no query, empty map.
	empty, err := repo.ListForEntities(ctx, scope, attachment.EntityPayment, nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("empty input: got %v, %v", empty, err)
	}
}

// TestAttachmentRepo_ListForEntities_Isolation ensures another org's rows
// never leak through the batched query.
func TestAttachmentRepo_ListForEntities_Isolation(t *testing.T) {
	db, scopeA := setupDB(t)
	repo := postgres.NewAttachmentRepo(db)
	ctx := context.Background()

	pay := uuid.New()
	a := mkAttachment(t, scopeA.OrgID, scopeA.UserID, pay, "a.png", time.Now())
	if err := repo.Create(ctx, scopeA, a); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Second org asking for the same entity id must see nothing.
	_, scopeB := setupDB(t)
	got, err := repo.ListForEntities(ctx, scopeB, attachment.EntityPayment, []uuid.UUID{pay})
	if err != nil {
		t.Fatalf("list batch: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("cross-tenant leak: got %v", got)
	}
}
