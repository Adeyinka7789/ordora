package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/app"
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
			&o.CustomerAddress,
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

// ListPayments returns the payment lines for one order (receipt view).
// Uses the SECURITY DEFINER function from migration 0032. Only
// filenames/counts of proof attachments are exposed; file downloads
// stay behind staff auth.
func (r *PortalRepo) ListPayments(ctx context.Context, orderID uuid.UUID) ([]portal.Payment, error) {
	var out []portal.Payment
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `SELECT * FROM get_public_order_payments($1)`
		rows, err := tx.Query(ctx, q, orderID)
		if err != nil {
			return fmt.Errorf("portal_repo: payments: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var p portal.Payment
			if err := rows.Scan(&p.Method, &p.Reference, &p.PaidAt,
				&p.AmountMinor, &p.Currency, &p.Notes,
				&p.IsReversed, &p.IsReversal, &p.ProofCount, &p.ProofNames,
				&p.ProofIDs); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// GetPortalAttachment resolves one proof file through the portal token.
// All authorization (token validity, org match, order/payment ownership,
// purpose allowlist) happens inside the SECURITY DEFINER function.
func (r *PortalRepo) GetPortalAttachment(ctx context.Context, tokenHash []byte, attachmentID uuid.UUID) (*app.PortalAttachment, error) {
	var out *app.PortalAttachment
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `SELECT * FROM get_portal_attachment($1, $2)`
		var a app.PortalAttachment
		if err := tx.QueryRow(ctx, q, tokenHash, attachmentID).Scan(
			&a.ID, &a.OrderID, &a.OrgID, &a.Filename,
			&a.MimeType, &a.SizeBytes, &a.StorageKey,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrPortalTokenNotFound
			}
			return fmt.Errorf("portal_repo: attachment: %w", err)
		}
		out = &a
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

// GetPublicProductImage resolves one catalog image. The function only
// returns rows for images on active, visible products of non-suspended
// orgs; anything else is a 404 with no existence signal.
func (r *PortalRepo) GetPublicProductImage(ctx context.Context, attachmentID uuid.UUID) (*app.PublicProductImage, error) {
	var out *app.PublicProductImage
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `SELECT * FROM get_public_product_image($1)`
		var img app.PublicProductImage
		if err := tx.QueryRow(ctx, q, attachmentID).Scan(
			&img.StorageKey, &img.MimeType, &img.Filename, &img.SizeBytes,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrPortalTokenNotFound
			}
			return fmt.Errorf("portal_repo: product image: %w", err)
		}
		out = &img
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
				&it.SubtotalMinor, &it.Currency, &it.Position,
				&it.Material, &it.ImageRef); err != nil {
				return err
			}
			out = append(out, it)
		}
		return rows.Err()
	})
	return out, err
}
