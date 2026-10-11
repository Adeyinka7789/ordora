package postgres_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/infra/id"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

// TestTeamService_ConcurrentInvitesRespectCap proves the 3-member cap is
// concurrency-safe: with 1 slot left, 8 simultaneous CreateInvite calls must
// yield exactly 1 invite and 7 ErrTeamLimitReached. The org row lock
// (LockOrgTx, SELECT ... FOR UPDATE) serializes the check-and-insert;
// without it, overlapping txs could all read total = 2 and all insert.
func TestTeamService_ConcurrentInvitesRespectCap(t *testing.T) {
	db, scope := setupDB(t)
	ctx := context.Background()

	members := postgres.NewTeamMemberRepo(db)
	invites := postgres.NewTeamInviteRepo(db)
	svc := app.NewTeamService(app.TeamServiceDeps{
		DB:       db,
		Invites:  invites,
		Members:  members,
		Users:    postgres.NewUserRepo(db),
		Sessions: postgres.NewSessionRepo(db),
		Audit:    nil,
		IDs:      id.Generator{},
		Now:      time.Now,
	})

	// 1 active member (owner) + 1 seeded pending invite = 1 slot left.
	now := time.Now()
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		return members.AddTx(ctx, tx, scope.OrgID, scope.UserID, tenant.RoleOwner, now)
	}); err != nil {
		t.Fatalf("seed owner membership: %v", err)
	}
	if _, _, err := svc.CreateInvite(ctx, scope, "seeded@example.local"); err != nil {
		t.Fatalf("seed invite: %v", err)
	}

	const racers = 8
	var wg sync.WaitGroup
	errs := make([]error, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release all goroutines at once to force overlap
			email := fmt.Sprintf("race-%d-%s@example.local", i, uuid.NewString()[:8])
			_, _, errs[i] = svc.CreateInvite(ctx, scope, email)
		}(i)
	}
	close(start)
	wg.Wait()

	var won, capped int
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, app.ErrTeamLimitReached):
			capped++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("exactly 1 racer must win the last slot, got %d (errors: %v)", won, errs)
	}
	if capped != racers-1 {
		t.Fatalf("remaining racers must hit the cap, got %d", capped)
	}

	// Final state: 1 active + 2 pending = at the cap, never over.
	var active, pending int
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		var err error
		active, err = members.CountActiveTx(ctx, tx, scope.OrgID)
		if err != nil {
			return err
		}
		pending, err = invites.CountPendingTx(ctx, tx, scope.OrgID)
		return err
	}); err != nil {
		t.Fatalf("final count: %v", err)
	}
	if active != 1 || pending != 2 {
		t.Fatalf("want 1 active + 2 pending, got %d + %d", active, pending)
	}
}

// TestTeamService_CreateInviteSeesInflightInvite deterministically proves the
// cap check cannot go stale: while another tx holds an uncommitted invite
// insert, CreateInvite must wait for it and then reject (the holder fills the
// last slot). Without the org row lock, CreateInvite reads the old committed
// counts, inserts anyway, and the cap is exceeded — this test then fails with
// a phantom success instead of ErrTeamLimitReached.
func TestTeamService_CreateInviteSeesInflightInvite(t *testing.T) {
	db, scope := setupDB(t)
	ctx := context.Background()

	members := postgres.NewTeamMemberRepo(db)
	svc := app.NewTeamService(app.TeamServiceDeps{
		DB:       db,
		Invites:  postgres.NewTeamInviteRepo(db),
		Members:  members,
		Users:    postgres.NewUserRepo(db),
		Sessions: postgres.NewSessionRepo(db),
		Audit:    nil,
		IDs:      id.Generator{},
		Now:      time.Now,
	})

	// 1 active member (owner) + 1 committed pending invite = 1 slot left.
	now := time.Now()
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		return members.AddTx(ctx, tx, scope.OrgID, scope.UserID, tenant.RoleOwner, now)
	}); err != nil {
		t.Fatalf("seed owner membership: %v", err)
	}
	if _, _, err := svc.CreateInvite(ctx, scope, "seeded@example.local"); err != nil {
		t.Fatalf("seed invite: %v", err)
	}

	// Open tx holding an uncommitted second invite (fills the last slot once
	// committed). Manual tx: set the tenant (RLS) and insert, keep it open.
	hold, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin hold tx: %v", err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if _, err := hold.Exec(ctx,
		"SELECT set_config('app.current_org_id', $1, true)", scope.OrgID.String()); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		t.Fatalf("rand: %v", err)
	}
	if _, err := hold.Exec(ctx, `
		INSERT INTO organization_invites
		    (id, organization_id, email, token_hash, status, invited_by, expires_at, created_at)
		VALUES ($1, $2, $3, $4, 'PENDING', $5, $6, $7)
	`, uuid.New(), scope.OrgID, "holder@example.local", token,
		scope.UserID, now.Add(7*24*time.Hour), now); err != nil {
		t.Fatalf("hold insert: %v", err)
	}

	// The racing invite must wait for the holder, then see total = 3 and
	// refuse. (Without LockOrgTx it reads the stale committed total = 2,
	// inserts immediately, and succeeds — over the cap.)
	done := make(chan error, 1)
	go func() {
		_, _, err := svc.CreateInvite(ctx, scope, "racer@example.local")
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("CreateInvite finished before the in-flight tx committed (err=%v); it must wait on the org lock", err)
	case <-time.After(500 * time.Millisecond):
		// Still waiting: correctly serialized behind the holder.
	}

	if err := hold.Commit(ctx); err != nil {
		t.Fatalf("commit holder: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, app.ErrTeamLimitReached) {
			t.Fatalf("want ErrTeamLimitReached after holder commits, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CreateInvite did not complete after the holder committed")
	}
}
