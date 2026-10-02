package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/portal"
)

// ErrPortalTokenNotFound is returned when a token hash matches no active order.
var ErrPortalTokenNotFound = errors.New("portal: token not found")

// PortalRepo reads a single order by public token via the SECURITY DEFINER
// function created in migration 0014. Not tenant-scoped.
type PortalRepo struct {
	db *DB
}

func NewPortalRepo(db *DB) *PortalRepo { return &PortalRepo{db: db} }

// GetByTokenHash reads one order by SHA-256 token hash.
func (r *PortalRepo) GetByTokenHash(ctx context.Context, tokenHash []byte) (*portal.Order, error) {
	var out *portal.Order
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `SELECT * FROM get_order_by_public_token($1)`
		row := tx.QueryRow(ctx, q, tokenHash)
		var o portal.Order
		if err := row.Scan(
			&o.OrderID, &o.OrganizationID, &o.OrderNumber, &o.Title,
			&o.Description, &o.Status, &o.Currency,
			&o.SubtotalMinor, &o.DiscountMinor, &o.TaxMinor,
			&o.TotalMinor, &o.AmountPaidMinor,
			&o.ExpectedCompletion, &o.DeliveredAt, &o.CreatedAt,
			&o.CustomerName, &o.CustomerEmail, &o.CustomerPhone,
			&o.OrgName, &o.OrgSlug, &o.OrgEmail, &o.OrgPhone,
			&o.OrgAddress, &o.OrgCurrency, &o.OrgTimezone,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrPortalTokenNotFound
			}
			return fmt.Errorf("portal_repo: scan: %w", err)
		}
		out = &o
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, ErrPortalTokenNotFound
	}
	return out, nil
}

// ListItems returns the line items for one order.
// ListItems returns the line items for one order.
func (r *PortalRepo) ListItems(ctx context.Context, orderID uuid.UUID) ([]portal.Item, error) {
	var out []portal.Item
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `SELECT * FROM get_public_order_items($1)`
		rows, err := tx.Query(ctx, q, orderID)
		if err != nil {
			return fmt.Errorf("portal_repo: items: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var it portal.Item
			if err := rows.Scan(&it.Description, &it.Quantity, &it.UnitPriceMinor,
				&it.SubtotalMinor, &it.Currency, &it.Position); err != nil {
				return err
			}
			out = append(out, it)
		}
		return rows.Err()
	})
	return out, err
}
