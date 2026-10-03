package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/cost"
	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// CostRepo persists order costs. Tenant-scoped.
type CostRepo struct {
	db *DB
}

func NewCostRepo(db *DB) *CostRepo { return &CostRepo{db: db} }

// Create inserts a cost.
func (r *CostRepo) Create(ctx context.Context, scope tenant.TenantScope, c *cost.Cost) error {
	if c.OrganizationID != scope.OrgID {
		return fmt.Errorf("cost_repo: org mismatch")
	}
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			INSERT INTO order_costs
				(id, organization_id, order_id, category, description, amount_minor, currency, incurred_on, vendor, notes, created_by, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		`
		_, err := tx.Exec(ctx, q,
			c.ID, c.OrganizationID, c.OrderID, string(c.Category),
			c.Description, c.Amount.Amount(), c.Amount.Currency(),
			c.IncurredOn, nullIfEmpty(c.Vendor), nullIfEmpty(c.Notes),
			c.CreatedBy, c.CreatedAt, c.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("cost_repo: create: %w", Classify(err))
		}
		return nil
	})
}

// GetByID loads one cost.
func (r *CostRepo) GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*cost.Cost, error) {
	var out *cost.Cost
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		c, err := r.getByIDTx(ctx, tx, id)
		if err != nil {
			return err
		}
		out = c
		return nil
	})
	return out, err
}

func (r *CostRepo) getByIDTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*cost.Cost, error) {
	const q = `
		SELECT id, organization_id, order_id, category::text, description,
		       amount_minor, currency::text, incurred_on,
		       COALESCE(vendor,''), COALESCE(notes,''),
		       created_by, created_at, updated_at
		FROM order_costs
		WHERE id = $1
	`
	return scanCost(tx.QueryRow(ctx, q, id))
}

// ListForOrder returns all costs for an order, newest incurred first.
func (r *CostRepo) ListForOrder(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) ([]*cost.Cost, error) {
	var out []*cost.Cost
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT id, organization_id, order_id, category::text, description,
			       amount_minor, currency::text, incurred_on,
			       COALESCE(vendor,''), COALESCE(notes,''),
			       created_by, created_at, updated_at
			FROM order_costs
			WHERE order_id = $1
			ORDER BY incurred_on DESC, created_at DESC
		`
		rows, err := tx.Query(ctx, q, orderID)
		if err != nil {
			return fmt.Errorf("cost_repo: list: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanCost(rows)
			if err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// Update replaces the mutable fields of a cost.
func (r *CostRepo) Update(ctx context.Context, scope tenant.TenantScope, c *cost.Cost) error {
	if c.OrganizationID != scope.OrgID {
		return fmt.Errorf("cost_repo: org mismatch")
	}
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			UPDATE order_costs
			SET category = $2, description = $3, amount_minor = $4, currency = $5,
			    incurred_on = $6, vendor = $7, notes = $8, updated_at = $9
			WHERE id = $1
		`
		ct, err := tx.Exec(ctx, q,
			c.ID, string(c.Category), c.Description,
			c.Amount.Amount(), c.Amount.Currency(),
			c.IncurredOn, nullIfEmpty(c.Vendor), nullIfEmpty(c.Notes),
			c.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("cost_repo: update: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return cost.ErrNotFound
		}
		return nil
	})
}

// Delete removes a cost.
func (r *CostRepo) Delete(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx, `DELETE FROM order_costs WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("cost_repo: delete: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return cost.ErrNotFound
		}
		return nil
	})
}

// CategoryTotals returns the sum of costs per category for an order.
func (r *CostRepo) CategoryTotals(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) (map[cost.Category]int64, error) {
	out := make(map[cost.Category]int64)
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT category::text, COALESCE(SUM(amount_minor), 0)
			FROM order_costs
			WHERE order_id = $1
			GROUP BY category
		`
		rows, err := tx.Query(ctx, q, orderID)
		if err != nil {
			return fmt.Errorf("cost_repo: category totals: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var cat string
			var total int64
			if err := rows.Scan(&cat, &total); err != nil {
				return err
			}
			out[cost.Category(cat)] = total
		}
		return rows.Err()
	})
	return out, err
}

// OrderCostTotals returns the total cost per order for a set of orders.
// Used by the reports page.
func (r *CostRepo) OrderCostTotals(ctx context.Context, scope tenant.TenantScope, from, to time.Time) (int64, error) {
	var total int64
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT COALESCE(SUM(amount_minor), 0)
			FROM order_costs
			WHERE organization_id = $1
			  AND incurred_on >= $2
			  AND incurred_on < $3
		`
		if err := tx.QueryRow(ctx, q, scope.OrgID, from, to).Scan(&total); err != nil {
			return fmt.Errorf("cost_repo: totals: %w", Classify(err))
		}
		return nil
	})
	return total, err
}

// CostRow is one row of the summary used by reports.
type CostRow struct {
	Category   string
	TotalMinor int64
	Count      int64
}

// CategoryBreakdown returns the total per category in a date range.
func (r *CostRepo) CategoryBreakdown(ctx context.Context, scope tenant.TenantScope, from, to time.Time) ([]CostRow, error) {
	var out []CostRow
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT category::text, COALESCE(SUM(amount_minor), 0), COUNT(*)
			FROM order_costs
			WHERE organization_id = $1
			  AND incurred_on >= $2
			  AND incurred_on < $3
			GROUP BY category
			ORDER BY 2 DESC
		`
		rows, err := tx.Query(ctx, q, scope.OrgID, from, to)
		if err != nil {
			return fmt.Errorf("cost_repo: breakdown: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var r CostRow
			if err := rows.Scan(&r.Category, &r.TotalMinor, &r.Count); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// scanCost materializes a row.
func scanCost(row scannable) (*cost.Cost, error) {
	var (
		id          uuid.UUID
		orgID       uuid.UUID
		orderID     uuid.UUID
		cat         string
		description string
		amountMinor int64
		currency    string
		incurredOn  time.Time
		vendor      string
		notes       string
		createdBy   uuid.UUID
		createdAt   time.Time
		updatedAt   time.Time
	)
	if err := row.Scan(&id, &orgID, &orderID, &cat, &description,
		&amountMinor, &currency, &incurredOn,
		&vendor, &notes, &createdBy, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, cost.ErrNotFound
		}
		return nil, fmt.Errorf("cost_repo: scan: %w", err)
	}
	amount, err := money.New(amountMinor, currency)
	if err != nil {
		return nil, fmt.Errorf("cost_repo: bad amount: %w", err)
	}
	return &cost.Cost{
		ID:             id,
		OrganizationID: orgID,
		OrderID:        orderID,
		Category:       cost.Category(cat),
		Description:    description,
		Amount:         amount,
		IncurredOn:     incurredOn,
		Vendor:         vendor,
		Notes:          notes,
		CreatedBy:      createdBy,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}, nil
}
