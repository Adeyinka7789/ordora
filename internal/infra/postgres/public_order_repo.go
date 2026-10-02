package postgres

import (
	"context"
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
func (r *PublicOrderRepo) LookupOrgBySlug(ctx context.Context, slug string) (*app.PublicOrg, error) {
	var out *app.PublicOrg
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `SELECT * FROM lookup_public_org($1)`
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

// CreatePublicOrder runs the SECURITY DEFINER function that creates the
// customer (if new) and the order atomically.
func (r *PublicOrderRepo) CreatePublicOrder(ctx context.Context, in app.PublicOrderInput) (*app.PublicOrderResult, error) {
	var out *app.PublicOrderResult
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT * FROM create_public_order(
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
			)
		`
		orderID := r.ids.New()
		customerID := r.ids.New()
		auditID := r.ids.New()

		var res app.PublicOrderResult
		if err := tx.QueryRow(ctx, q,
			in.Slug, in.CustomerName, in.CustomerEmail, in.CustomerPhone,
			in.Description, in.ExpectedDate, in.BudgetMinor,
			orderID, customerID, auditID,
		).Scan(&res.OrderID, &res.OrderNumber, &res.OrgName, &res.OrgPhone); err != nil {
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
