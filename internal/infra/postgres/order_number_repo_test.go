package postgres_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

// TestOrderNumberRepo_Sequential verifies that consecutive allocations return
// consecutive numbers.
func TestOrderNumberRepo_Sequential(t *testing.T) {
	db, scope := setupDB(t)
	repo := postgres.NewOrderNumberRepo(db)
	ctx := context.Background()
	year := time.Now().Year()

	var nums []string
	err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		for i := 0; i < 5; i++ {
			n, err := repo.AllocateTx(ctx, tx, scope.OrgID, year)
			if err != nil {
				return err
			}
			nums = append(nums, n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}

	want := []string{
		order.FormatOrderNumber(year, 1),
		order.FormatOrderNumber(year, 2),
		order.FormatOrderNumber(year, 3),
		order.FormatOrderNumber(year, 4),
		order.FormatOrderNumber(year, 5),
	}
	for i, got := range nums {
		if got != want[i] {
			t.Errorf("alloc %d: got %s, want %s", i, got, want[i])
		}
	}
}

// TestOrderNumberRepo_Concurrent verifies that 50 concurrent goroutines each
// get a unique number and that the final sequence is exactly 50. This is the
// real test of the counter's correctness.
//
// Note: because the repo methods run inside a *single* connection's
// transaction, we can't easily parallelize at the pgx.Tx level across
// goroutines. Instead we open 50 independent transactions and run them
// concurrently against the same DB — that's what actually happens under load.
func TestOrderNumberRepo_Concurrent(t *testing.T) {
	db, scope := setupDB(t)
	ctx := context.Background()
	year := time.Now().Year()

	const N = 50
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		seen    = make(map[string]int)
		failErr error
	)

	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
				repo := postgres.NewOrderNumberRepo(db)
				n, err := repo.AllocateTx(ctx, tx, scope.OrgID, year)
				if err != nil {
					return err
				}
				mu.Lock()
				seen[n]++
				if seen[n] > 1 && failErr == nil {
					failErr = fmt.Errorf("duplicate number allocated: %s", n)
				}
				mu.Unlock()
				return nil
			})
			if err != nil {
				mu.Lock()
				if failErr == nil {
					failErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if failErr != nil {
		t.Fatalf("concurrent allocation: %v", failErr)
	}
	if len(seen) != N {
		t.Fatalf("got %d unique numbers, want %d", len(seen), N)
	}

	// Verify the final sequence value.
	var final int64
	err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT last_sequence FROM order_counters WHERE organization_id = $1 AND year = $2`, scope.OrgID, year).Scan(&final)
	})
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	if final != N {
		t.Fatalf("final sequence = %d, want %d", final, N)
	}
}

// TestOrderNumberRepo_PerOrgIsolation verifies that two orgs get independent
// sequences.
func TestOrderNumberRepo_PerOrgIsolation(t *testing.T) {
	db, scopeA := setupDB(t)
	ctx := context.Background()
	year := time.Now().Year()

	// Second org.
	orgB := uuid.New()
	userB := uuid.New()
	if err := db.WithTenant(ctx, orgB, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO organizations (id, name, slug, currency, timezone)
			VALUES ($1, $2, $3, 'NGN', 'Africa/Lagos')`, orgB, "Org B", "orgb-"+orgB.String()[:8])
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO users (id, email, password_hash, name)
			VALUES ($1, $2, 'x', $3)`, userB, "b-"+userB.String()[:8]+"@example.local", "B")
		return err
	}); err != nil {
		t.Fatalf("setup B: %v", err)
	}
	t.Cleanup(func() {
		_ = db.WithTenant(ctx, orgB, func(tx pgx.Tx) error {
			_, _ = tx.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgB)
			_, _ = tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userB)
			return nil
		})
	})

	repo := postgres.NewOrderNumberRepo(db)

	// Org A allocates 3.
	var aNums []string
	_ = db.WithTenant(ctx, scopeA.OrgID, func(tx pgx.Tx) error {
		for i := 0; i < 3; i++ {
			n, _ := repo.AllocateTx(ctx, tx, scopeA.OrgID, year)
			aNums = append(aNums, n)
		}
		return nil
	})

	// Org B allocates 2 — should start at 1.
	var bNums []string
	_ = db.WithTenant(ctx, orgB, func(tx pgx.Tx) error {
		for i := 0; i < 2; i++ {
			n, _ := repo.AllocateTx(ctx, tx, orgB, year)
			bNums = append(bNums, n)
		}
		return nil
	})

	if aNums[0] != order.FormatOrderNumber(year, 1) {
		t.Errorf("org A first = %s", aNums[0])
	}
	if bNums[0] != order.FormatOrderNumber(year, 1) {
		t.Errorf("org B first = %s (should be independent)", bNums[0])
	}
}
