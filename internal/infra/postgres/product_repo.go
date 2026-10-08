package postgres

import (
	"context"
	"encoding/json"
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
		questions, err := marshalQuestions(p.Questions)
		if err != nil {
			return err
		}
		const q = `
			INSERT INTO products
				(id, organization_id, name, description, sku, unit_price_minor, currency, active, created_by, created_at, updated_at,
				 material, color, short_description, internal_notes, specs, production_days,
				 quote_only, starting_from, hidden, availability, category, questions)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,
			        $12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23::jsonb)
		`
		_, err = tx.Exec(ctx, q,
			p.ID, p.OrganizationID, p.Name,
			nullIfEmpty(p.Description), nullIfEmpty(p.SKU),
			p.UnitPrice.Amount(), p.Currency, p.Active,
			p.CreatedBy, p.CreatedAt, p.UpdatedAt,
			nullIfEmpty(p.Material), nullIfEmpty(p.Color),
			nullIfEmpty(p.ShortDescription), nullIfEmpty(p.InternalNotes),
			nullIfEmpty(p.Specs), nullIfInt(p.ProductionDays),
			p.QuoteOnly, p.StartingFrom, p.Hidden, p.Availability,
			nullIfEmpty(p.Category), questions,
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
		questions, err := marshalQuestions(p.Questions)
		if err != nil {
			return err
		}
		const q = `
			UPDATE products
			   SET name = $2, description = $3, sku = $4,
			       unit_price_minor = $5, active = $6, updated_at = $7,
			       material = $8, color = $9, short_description = $10,
			       internal_notes = $11, specs = $12, production_days = $13,
			       quote_only = $14, starting_from = $15, hidden = $16,
			       availability = $17, category = $18, questions = $19::jsonb
			 WHERE id = $1
		`
		ct, err := tx.Exec(ctx, q,
			p.ID, p.Name, nullIfEmpty(p.Description), nullIfEmpty(p.SKU),
			p.UnitPrice.Amount(), p.Active, p.UpdatedAt,
			nullIfEmpty(p.Material), nullIfEmpty(p.Color),
			nullIfEmpty(p.ShortDescription), nullIfEmpty(p.InternalNotes),
			nullIfEmpty(p.Specs), nullIfInt(p.ProductionDays),
			p.QuoteOnly, p.StartingFrom, p.Hidden, p.Availability,
			nullIfEmpty(p.Category), questions,
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
	return r.setActive(ctx, scope, id, false, now)
}

// Unarchive sets active = true (undo for Archive).
func (r *ProductRepo) Unarchive(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, now time.Time) error {
	return r.setActive(ctx, scope, id, true, now)
}

func (r *ProductRepo) setActive(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, active bool, now time.Time) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `UPDATE products SET active = $2, updated_at = $3 WHERE id = $1`
		ct, err := tx.Exec(ctx, q, id, active, now)
		if err != nil {
			return fmt.Errorf("product_repo: set active: %w", Classify(err))
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
			       unit_price_minor, currency, active, created_by, created_at, updated_at,
			       COALESCE(material,''), COALESCE(color,''), COALESCE(short_description,''),
			       COALESCE(internal_notes,''), COALESCE(specs,''), production_days,
			       COALESCE(quote_only,false), COALESCE(starting_from,false),
			       COALESCE(hidden,false), COALESCE(availability,'in_stock'),
			       COALESCE(category,''), COALESCE(questions,'[]')
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
			       unit_price_minor, currency, active, created_by, created_at, updated_at,
			       COALESCE(material,''), COALESCE(color,''), COALESCE(short_description,''),
			       COALESCE(internal_notes,''), COALESCE(specs,''), production_days,
			       COALESCE(quote_only,false), COALESCE(starting_from,false),
			       COALESCE(hidden,false), COALESCE(availability,'in_stock'),
			       COALESCE(category,''), COALESCE(questions,'[]')
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

		material     string
		color        string
		shortDesc    string
		notes        string
		specs        string
		prodDays     *int
		quoteOnly    bool
		startingFrom bool
		hidden       bool
		availability string
		category     string
		questionsRaw []byte
	)
	if err := row.Scan(&id, &orgID, &name, &description, &sku, &priceMinor, &currency, &active, &createdBy, &createdAt, &updatedAt,
		&material, &color, &shortDesc, &notes, &specs, &prodDays,
		&quoteOnly, &startingFrom, &hidden, &availability, &category, &questionsRaw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, product.ErrNotFound
		}
		return nil, fmt.Errorf("product_repo: scan: %w", err)
	}
	price, err := money.New(priceMinor, currency)
	if err != nil {
		return nil, fmt.Errorf("product_repo: bad price: %w", err)
	}
	questions, err := unmarshalQuestions(questionsRaw)
	if err != nil {
		return nil, fmt.Errorf("product_repo: bad questions: %w", err)
	}
	if availability == "" {
		availability = product.AvailabilityInStock
	}
	p := &product.Product{
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

		Material:         material,
		Color:            color,
		ShortDescription: shortDesc,
		InternalNotes:    notes,
		Specs:            specs,
		QuoteOnly:        quoteOnly,
		StartingFrom:     startingFrom,
		Hidden:           hidden,
		Availability:     availability,
		Category:         category,
		Questions:        questions,
	}
	if prodDays != nil {
		p.ProductionDays = *prodDays
	}
	return p, nil
}

// nullIfInt stores 0 production days as NULL (unset).
func nullIfInt(n int) any {
	if n <= 0 {
		return nil
	}
	return n
}

// marshalQuestions encodes product questions for the JSONB column.
func marshalQuestions(qs []product.ProductQuestion) (string, error) {
	if len(qs) == 0 {
		return "[]", nil
	}
	raw, err := json.Marshal(qs)
	if err != nil {
		return "", fmt.Errorf("product_repo: marshal questions: %w", err)
	}
	return string(raw), nil
}

// unmarshalQuestions decodes the JSONB column; NULL/empty yields nil.
func unmarshalQuestions(raw []byte) ([]product.ProductQuestion, error) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "[]" {
		return nil, nil
	}
	var qs []product.ProductQuestion
	if err := json.Unmarshal(raw, &qs); err != nil {
		return nil, err
	}
	return qs, nil
}
