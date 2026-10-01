package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/order"
)

// OrderNumberRepo allocates order numbers per (organization, year).
//
// The allocation is designed to be called inside the same transaction as the
// order insert. If the outer transaction rolls back, the counter increment is
// rolled back too — no gaps.
//
// Concurrency: SELECT ... FOR UPDATE on the counter row serializes concurrent
// allocations for the same (org, year). Different orgs never contend.
type OrderNumberRepo struct {
	db *DB
}

func NewOrderNumberRepo(db *DB) *OrderNumberRepo { return &OrderNumberRepo{db: db} }

// AllocateTx reserves the next number for the org in the given year and returns
// it formatted (ORD-YYYY-NNNNNN). Must be called inside a WithTenant tx.
//
// Behavior:
//   - If no counter row exists for (org, year), it's created with
//     last_sequence = 1 and the number "ORD-YYYY-000001" is returned.
//   - Otherwise, the row is locked, last_sequence is incremented, and the
//     next number is returned.
//
// Both cases happen in one statement via INSERT ... ON CONFLICT ... RETURNING,
// which is atomic.
func (r *OrderNumberRepo) AllocateTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, year int) (string, error) {
	const q = `
		INSERT INTO order_counters (organization_id, year, last_sequence, updated_at)
		VALUES ($1, $2, 1, $3)
		ON CONFLICT (organization_id, year)
		DO UPDATE SET last_sequence = order_counters.last_sequence + 1,
		              updated_at = EXCLUDED.updated_at
		RETURNING last_sequence
	`
	var seq int64
	if err := tx.QueryRow(ctx, q, orgID, year, time.Now()).Scan(&seq); err != nil {
		return "", fmt.Errorf("order_number_repo: allocate: %w", Classify(err))
	}
	return order.FormatOrderNumber(year, seq), nil
}

// PeekTx returns the current sequence value for (org, year) without incrementing
// it. Returns 0 if no counter exists. Useful for tests and admin views.
func (r *OrderNumberRepo) PeekTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, year int) (int64, error) {
	const q = `SELECT last_sequence FROM order_counters WHERE organization_id = $1 AND year = $2`
	var seq int64
	err := tx.QueryRow(ctx, q, orgID, year).Scan(&seq)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("order_number_repo: peek: %w", Classify(err))
	}
	return seq, nil
}
