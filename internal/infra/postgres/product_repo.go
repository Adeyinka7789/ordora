package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/product"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// ProductRepo persists products. Tenant-scoped.
type ProductRepo struct {
	db *DB
}

func NewProductRepo(db *DB) *ProductRepo { return &ProductRepo{db: db} }

// -----------------------------------------------------------------------------
// Create / Update / Archive
// -----------------------------------------------------------------------------

func (r *ProductRepo) Create(ctx context.Context, scope tenant.TenantScope, p *product.Product) error {
	if p.OrganizationID != scope.OrgID {
		return fmt.Errorf("product_repo: org mismatch")
	}
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			INSERT INTO products
				(id, organization_id, name, description, sku, unit_price_minor, currency, active, created_by, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		`
		_, err := tx.Exec(ctx, q,
			p.ID, p.OrganizationID, p.Name,
			nullIfEmpty(p.Description), nullIfEmpty(p.SKU),
			p.UnitPrice.Amount(), p.Currency, p.Active,
			p.CreatedBy, p.CreatedAt, p.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("product_repo: create: %w", Classify(err))
		}
		return nil
	})
}

func (r *ProductRepo) Update(ctx context.Context, scope tenant.TenantScope, p *product.Product) error {
	if p.OrganizationID != scope.OrgID {
		return fmt.Errorf("product_repo: org mismatch")
	}
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			UPDATE products
			   SET name = $2, description = $3, sku = $4,
			       unit_price_minor = $5, active = $6, updated_at = $7
			 WHERE id = $1
		`
		ct, err := tx.Exec(ctx, q,
			p.ID, p.Name, nullIfEmpty(p.Description), nullIfEmpty(p.SKU),
			p.UnitPrice.Amount(), p.Active, p.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("product_repo: update: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return product.ErrNotFound
		}
		return nil
	})
}

// Archive sets active = false. Soft delete.
func (r *ProductRepo) Archive(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, now time.Time) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `UPDATE products SET active = false, updated_at = $2 WHERE id = $1`
		ct, err := tx.Exec(ctx, q, id, now)
		if err != nil {
			return fmt.Errorf("product_repo: archive: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return product.ErrNotFound
		}
		return nil
	})
}

// -----------------------------------------------------------------------------
// Read
// -----------------------------------------------------------------------------

func (r *ProductRepo) GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*product.Product, error) {
	var p *product.Product
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT id, organization_id, name, COALESCE(description,''), COALESCE(sku,''),
			       unit_price_minor, currency, active, created_by, created_at, updated_at
			FROM products
			WHERE id = $1
		`
		var e error
		p, e = scanProduct(tx.QueryRow(ctx, q, id))
		return e
	})
	return p, err
}

// -----------------------------------------------------------------------------
// List with search + active filter
// -----------------------------------------------------------------------------

type ProductListOptions struct {
	Query           string // matches name or SKU
	IncludeArchived bool   // when false, only active products
	Limit           int
	Offset          int
}

type ProductListResult struct {
	Products []*product.Product
	Total    int
	Limit    int
	Offset   int
}

func (r *ProductRepo) List(ctx context.Context, scope tenant.TenantScope, opts ProductListOptions) (*ProductListResult, error) {
	if opts.Limit <= 0 || opts.Limit > 200 {
		opts.Limit = 50
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}

	var out *ProductListResult
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		where, args := buildProductListWhere(opts)

		var total int
		countSQL := fmt.Sprintf(`SELECT count(*) FROM products WHERE %s`, where)
		if err := tx.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
			return fmt.Errorf("product_repo: count: %w", Classify(err))
		}

		pageArgs := append([]any{}, args...)
		pageArgs = append(pageArgs, opts.Limit, opts.Offset)
		listSQL := fmt.Sprintf(`
			SELECT id, organization_id, name, COALESCE(description,''), COALESCE(sku,''),
			       unit_price_minor, currency, active, created_by, created_at, updated_at
			FROM products
			WHERE %s
			ORDER BY active DESC, name ASC
			LIMIT $%d OFFSET $%d
		`, where, len(args)+1, len(args)+2)

		rows, err := tx.Query(ctx, listSQL, pageArgs...)
		if err != nil {
			return fmt.Errorf("product_repo: list: %w", Classify(err))
		}
		defer rows.Close()

		var list []*product.Product
		for rows.Next() {
			p, err := scanProduct(rows)
			if err != nil {
				return err
			}
			list = append(list, p)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		out = &ProductListResult{
			Products: list,
			Total:    total,
			Limit:    opts.Limit,
			Offset:   opts.Offset,
		}
		return nil
	})
	return out, err
}

func buildProductListWhere(opts ProductListOptions) (string, []any) {
	var clauses []string
	var args []any

	if !opts.IncludeArchived {
		clauses = append(clauses, "active = true")
	}

	q := strings.TrimSpace(opts.Query)
	if q != "" {
		args = append(args, "%"+q+"%")
		n := len(args)
		clauses = append(clauses, fmt.Sprintf("(name ILIKE $%d OR COALESCE(sku,'') ILIKE $%d)", n, n))
	}

	if len(clauses) == 0 {
		return "TRUE", args
	}
	return strings.Join(clauses, " AND "), args
}

// -----------------------------------------------------------------------------
// Scanner
// -----------------------------------------------------------------------------

func scanProduct(row scannable) (*product.Product, error) {
	var (
		id          uuid.UUID
		orgID       uuid.UUID
		name        string
		description string
		sku         string
		priceMinor  int64
		currency    string
		active      bool
		createdBy   uuid.UUID
		createdAt   time.Time
		updatedAt   time.Time
	)
	if err := row.Scan(&id, &orgID, &name, &description, &sku, &priceMinor, &currency, &active, &createdBy, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, product.ErrNotFound
		}
		return nil, fmt.Errorf("product_repo: scan: %w", err)
	}
	price, err := money.New(priceMinor, currency)
	if err != nil {
		return nil, fmt.Errorf("product_repo: bad price: %w", err)
	}
	return &product.Product{
		ID:             id,
		OrganizationID: orgID,
		Name:           name,
		Description:    description,
		SKU:            sku,
		UnitPrice:      price,
		Currency:       currency,
		Active:         active,
		CreatedBy:      createdBy,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}, nil
}
