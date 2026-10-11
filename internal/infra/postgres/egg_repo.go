package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// EggRepo persists poultry_settings and daily_crate_counters plus the
// seller-panel reads. Tenant methods take a scope and open their own
// WithTenant block (same convention as the customer/order repos); the
// anonymous intake config goes through a SECURITY DEFINER function.
type EggRepo struct {
	db *DB
}

func NewEggRepo(db *DB) *EggRepo { return &EggRepo{db: db} }

// GetSettings returns the org's egg settings, or defaults when the seller
// never saved any (a missing row is normal, not an error).
func (r *EggRepo) GetSettings(ctx context.Context, scope tenant.TenantScope) (*app.PoultrySettings, error) {
	var out *app.PoultrySettings
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT organization_id, max_crates_per_day, accepting_orders,
			       COALESCE(bank_name,''), COALESCE(account_number,''),
			       COALESCE(account_name,''), COALESCE(whatsapp_number,''),
			       updated_at
			FROM poultry_settings
			WHERE organization_id = $1
		`
		var s app.PoultrySettings
		var orgID uuid.UUID
		if err := tx.QueryRow(ctx, q, scope.OrgID).Scan(
			&orgID, &s.MaxCratesPerDay, &s.AcceptingOrders,
			&s.BankName, &s.AccountNumber, &s.AccountName, &s.Whatsapp,
			&s.UpdatedAt,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// No row yet: seller keeps the defaults until first save.
				s = app.DefaultPoultrySettings(scope.OrgID)
				out = &s
				return nil
			}
			return fmt.Errorf("egg_repo: get settings: %w", Classify(err))
		}
		s.OrgID = orgID
		out = &s
		return nil
	})
	return out, err
}

// UpsertSettings saves the org's egg settings (owner-only upstream).
func (r *EggRepo) UpsertSettings(ctx context.Context, scope tenant.TenantScope, s *app.PoultrySettings) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			INSERT INTO poultry_settings
			    (organization_id, max_crates_per_day, accepting_orders,
			     bank_name, account_number, account_name, whatsapp_number, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (organization_id)
			DO UPDATE SET max_crates_per_day = EXCLUDED.max_crates_per_day,
			              accepting_orders = EXCLUDED.accepting_orders,
			              bank_name = EXCLUDED.bank_name,
			              account_number = EXCLUDED.account_number,
			              account_name = EXCLUDED.account_name,
			              whatsapp_number = EXCLUDED.whatsapp_number,
			              updated_at = EXCLUDED.updated_at
		`
		_, err := tx.Exec(ctx, q,
			scope.OrgID, s.MaxCratesPerDay, s.AcceptingOrders,
			s.BankName, s.AccountNumber, s.AccountName, s.Whatsapp, s.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("egg_repo: upsert settings: %w", Classify(err))
		}
		return nil
	})
}

// SetAccepting flips the open/close toggle, creating the settings row with
// defaults when the seller never saved settings.
func (r *EggRepo) SetAccepting(ctx context.Context, scope tenant.TenantScope, accepting bool, now time.Time) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			INSERT INTO poultry_settings AS s
			    (organization_id, max_crates_per_day, accepting_orders, updated_at)
			VALUES ($1, 100, $2, $3)
			ON CONFLICT (organization_id)
			DO UPDATE SET accepting_orders = EXCLUDED.accepting_orders,
			              updated_at = EXCLUDED.updated_at
		`
		_, err := tx.Exec(ctx, q, scope.OrgID, accepting, now)
		if err != nil {
			return fmt.Errorf("egg_repo: set accepting: %w", Classify(err))
		}
		return nil
	})
}

// GetSold returns crates already committed against the cap for one delivery
// day (0 when no counter row exists yet).
func (r *EggRepo) GetSold(ctx context.Context, scope tenant.TenantScope, day time.Time) (float64, error) {
	var sold float64
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `SELECT COALESCE(sold, 0)::float8 FROM daily_crate_counters WHERE organization_id = $1 AND day = $2::date`
		if err := tx.QueryRow(ctx, q, scope.OrgID, day.Format("2006-01-02")).Scan(&sold); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				sold = 0
				return nil
			}
			return fmt.Errorf("egg_repo: get sold: %w", Classify(err))
		}
		return nil
	})
	return sold, err
}

// ReleaseCap gives back capacity when an order is rejected/cancelled.
// Floors at zero; a missing counter row is a no-op.
func (r *EggRepo) ReleaseCap(ctx context.Context, scope tenant.TenantScope, day time.Time, qty float64) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			UPDATE daily_crate_counters
			SET sold = GREATEST(0, sold - $3::numeric), updated_at = now()
			WHERE organization_id = $1 AND day = $2::date
		`
		_, err := tx.Exec(ctx, q, scope.OrgID, day.Format("2006-01-02"), fmt.Sprintf("%.3f", qty))
		if err != nil {
			return fmt.Errorf("egg_repo: release cap: %w", Classify(err))
		}
		return nil
	})
}

// OrderCrates returns the total crate quantity and delivery date of one
// order (for cap release on reject). Qty sums order_items (scale-3 aware
// callers pass quantities in whole crates).
func (r *EggRepo) OrderCrates(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) (qty float64, day time.Time, err error) {
	err = r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT COALESCE((SELECT SUM(quantity)::float8 FROM order_items WHERE order_id = $2), 0),
			       o.expected_completion
			FROM orders o
			WHERE o.organization_id = $1 AND o.id = $2
		`
		var dayVal *time.Time
		if e := tx.QueryRow(ctx, q, scope.OrgID, orderID).Scan(&qty, &dayVal); e != nil {
			return fmt.Errorf("egg_repo: order crates: %w", Classify(e))
		}
		if dayVal != nil {
			day = *dayVal
		}
		return nil
	})
	return qty, day, err
}

// OrderSummary is the small read the confirm/reject actions need.
func (r *EggRepo) OrderSummary(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) (*app.EggOrderSummary, error) {
	var out *app.EggOrderSummary
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT o.id, o.order_number, o.status::text, o.total_minor,
			       o.amount_paid_minor, o.currency::text, o.expected_completion,
			       o.customer_id, o.created_at
			FROM orders o
			WHERE o.organization_id = $1 AND o.id = $2
		`
		var s app.EggOrderSummary
		var status string
		var dayVal *time.Time
		if err := tx.QueryRow(ctx, q, scope.OrgID, orderID).Scan(
			&s.ID, &s.Number, &status, &s.TotalMinor, &s.PaidMinor,
			&s.Currency, &dayVal, &s.CustomerID, &s.CreatedAt,
		); err != nil {
			return fmt.Errorf("egg_repo: order summary: %w", Classify(err))
		}
		s.Status = status
		s.ExpectedDate = dayVal
		out = &s
		return nil
	})
	return out, err
}

// ListPanelOrders lists one delivery day's orders with customer, totals and
// the intake receipt (first payment_proof attachment), newest first.
func (r *EggRepo) ListPanelOrders(ctx context.Context, scope tenant.TenantScope, day time.Time) ([]app.EggPanelOrder, error) {
	var out []app.EggPanelOrder
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT o.id, o.order_number, o.status::text, o.total_minor,
			       o.amount_paid_minor, o.currency::text, o.created_at,
			       c.name, COALESCE(c.phone,''),
			       COALESCE((SELECT SUM(quantity)::float8 FROM order_items i WHERE i.order_id = o.id), 0),
			       r.id
			FROM orders o
			JOIN customers c ON c.id = o.customer_id
			LEFT JOIN LATERAL (
				SELECT a.id FROM attachments a
				WHERE a.organization_id = o.organization_id
				  AND a.entity_type = 'ORDER' AND a.entity_id = o.id
				  AND a.purpose = 'payment_proof'
				ORDER BY a.created_at ASC
				LIMIT 1
			) r ON true
			WHERE o.organization_id = $1 AND o.expected_completion = $2::date
			ORDER BY o.created_at DESC
		`
		rows, err := tx.Query(ctx, q, scope.OrgID, day.Format("2006-01-02"))
		if err != nil {
			return fmt.Errorf("egg_repo: panel list: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var o app.EggPanelOrder
			var receiptID *uuid.UUID
			if err := rows.Scan(&o.ID, &o.Number, &o.Status, &o.TotalMinor,
				&o.PaidMinor, &o.Currency, &o.CreatedAt,
				&o.CustomerName, &o.CustomerPhone, &o.QtyCrates, &receiptID); err != nil {
				return fmt.Errorf("egg_repo: panel scan: %w", err)
			}
			o.ReceiptID = receiptID
			out = append(out, o)
		}
		return rows.Err()
	})
	return out, err
}

var _ app.EggStore = (*EggRepo)(nil)
