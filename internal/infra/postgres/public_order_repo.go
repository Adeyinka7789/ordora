package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/app"
)

// PublicOrderRepo implements app.PublicOrderDB. All its operations go through
// SECURITY DEFINER functions so they can run without a tenant scope.
type PublicOrderRepo struct {
	db  *DB
	ids app.IDGen
}

func NewPublicOrderRepo(db *DB, ids app.IDGen) *PublicOrderRepo {
	return &PublicOrderRepo{db: db, ids: ids}
}

// LookupOrgBySlug resolves a business slug to (id, name, currency).
// Columns are named explicitly so a stale function shape fails loudly in
// logs instead of silently shifting values.
func (r *PublicOrderRepo) LookupOrgBySlug(ctx context.Context, slug string) (*app.PublicOrg, error) {
	var out *app.PublicOrg
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `SELECT org_id, org_name, currency, email, slug FROM lookup_public_org($1)`
		var o app.PublicOrg
		if err := tx.QueryRow(ctx, q, slug).Scan(&o.ID, &o.Name, &o.Currency, &o.Email, &o.Slug); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return app.ErrPublicOrgNotFound
			}
			return fmt.Errorf("public_order_repo: lookup: %w", err)
		}
		out = &o
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, app.ErrPublicOrgNotFound
	}
	return out, nil
}

// ListProductsBySlug returns the shop's active catalog for the public
// intake form. Unknown slugs yield an empty list.
func (r *PublicOrderRepo) ListProductsBySlug(ctx context.Context, slug string) ([]app.PublicProduct, error) {
	var out []app.PublicProduct
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT product_id, product_name, product_description, unit_price_minor, currency
			FROM get_public_products($1)
		`
		rows, err := tx.Query(ctx, q, slug)
		if err != nil {
			return fmt.Errorf("public_order_repo: products: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var p app.PublicProduct
			if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.UnitPriceMinor, &p.Currency); err != nil {
				return fmt.Errorf("public_order_repo: scan product: %w", err)
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// CreatePublicOrder runs the SECURITY DEFINER function that creates the
// customer (if new) and the order atomically.
func (r *PublicOrderRepo) CreatePublicOrder(ctx context.Context, in app.PublicOrderInput) (*app.PublicOrderResult, error) {
	var out *app.PublicOrderResult
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT * FROM create_public_order(
				$1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11
			)
		`
		orderID := r.ids.New()
		customerID := r.ids.New()
		auditID := r.ids.New()

		items, err := marshalPublicItems(r.ids, in.Items)
		if err != nil {
			return err
		}

		var res app.PublicOrderResult
		if err := tx.QueryRow(ctx, q,
			in.Slug, in.CustomerName, in.CustomerEmail, in.CustomerPhone,
			in.Description, in.ExpectedDate, in.BudgetMinor,
			items,
			orderID, customerID, auditID,
		).Scan(&res.OrderID, &res.OrderNumber, &res.OrgName, &res.OrgPhone, &res.TotalMinor, &res.Currency); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return app.ErrPublicOrgNotFound
			}
			return fmt.Errorf("public_order_repo: create: %w", err)
		}
		res.CustomerName = in.CustomerName
		out = &res
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out == nil || out.OrderID == uuid.Nil {
		return nil, app.ErrPublicOrgNotFound
	}
	return out, nil
}

// marshalPublicItems encodes product lines for create_public_order:
// [{id, product_id, qty}] with qty as an exact 3-decimal numeric string.
// Item ids are generated here so the function stays free of extension
// dependencies.
func marshalPublicItems(ids app.IDGen, items []app.PublicOrderItemInput) (string, error) {
	type line struct {
		ID        string `json:"id"`
		ProductID string `json:"product_id"`
		Qty       string `json:"qty"`
	}
	out := make([]line, 0, len(items))
	for _, it := range items {
		out = append(out, line{
			ID:        ids.New().String(),
			ProductID: it.ProductID.String(),
			Qty:       fmt.Sprintf("%.3f", float64(it.QuantityScaled)/1000.0),
		})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("public_order_repo: marshal items: %w", err)
	}
	return string(raw), nil
}
