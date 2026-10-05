package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

// TestPublicOrderRepo_LookupOrg exercises lookup_public_org end to end.
// It guards against PL/pgSQL variable/column ambiguity (SQLSTATE 42702),
// which once made every public intake link fail with
// "could not load business".
func TestPublicOrderRepo_LookupOrg(t *testing.T) {
	db, scope := setupDB(t)
	ctx := context.Background()
	repo := postgres.NewPublicOrderRepo(db, nil)

	slug := "test-" + strings.ToLower(scope.OrgID.String()[:8])
	got, err := repo.LookupOrgBySlug(ctx, slug)
	if err != nil {
		t.Fatalf("lookup %q: %v", slug, err)
	}
	if got.Name == "" || got.Currency == "" {
		t.Fatalf("unexpected org projection: %+v", got)
	}
	if got.Slug == "" {
		t.Fatalf("expected slug in projection: %+v", got)
	}

	if _, err := repo.LookupOrgBySlug(ctx, "no-such-shop-xyz"); err == nil {
		t.Fatal("unknown slug should fail")
	} else if err != app.ErrPublicOrgNotFound {
		t.Fatalf("unknown slug should map to ErrPublicOrgNotFound, got %v", err)
	}
}
