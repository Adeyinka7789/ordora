package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

// TestLoginAttemptRepo_Lockout arms the lockout after MaxLoginFailures
// consecutive failures and releases it on Clear.
func TestLoginAttemptRepo_Lockout(t *testing.T) {
	db, _ := setupDB(t)
	ctx := context.Background()
	repo := postgres.NewLoginAttemptRepo(db)
	email := "brute@example.com"
	now := time.Now()

	locked, err := repo.Locked(ctx, email, now)
	if err != nil || locked {
		t.Fatalf("fresh email must not be locked: %v %v", locked, err)
	}
	for i := 0; i < auth.MaxLoginFailures-1; i++ {
		if err := repo.RecordFailure(ctx, email, now); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
		if locked, _ := repo.Locked(ctx, email, now); locked {
			t.Fatalf("must not lock before %d failures", auth.MaxLoginFailures)
		}
	}
	if err := repo.RecordFailure(ctx, email, now); err != nil {
		t.Fatalf("record final: %v", err)
	}
	if locked, _ := repo.Locked(ctx, email, now); !locked {
		t.Fatal("must lock after MaxLoginFailures failures")
	}
	// Further attempts inside the window don't extend confusion: still locked.
	if err := repo.RecordFailure(ctx, email, now); err != nil {
		t.Fatalf("record while locked: %v", err)
	}
	if locked, _ := repo.Locked(ctx, email, now.Add(auth.LoginLockout-time.Minute)); !locked {
		t.Error("must stay locked inside the window")
	}
	if locked, _ := repo.Locked(ctx, email, now.Add(auth.LoginLockout+time.Minute)); locked {
		t.Error("lock must expire after the window")
	}
	if err := repo.Clear(ctx, email); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if locked, _ := repo.Locked(ctx, email, now); locked {
		t.Error("clear must release the lock")
	}
}
