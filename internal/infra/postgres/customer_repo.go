package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// CustomerRepo persists customers. Every method requires a TenantScope; all
// queries run inside WithTenant so RLS enforces isolation at the DB level.
type CustomerRepo struct {
	db *DB
}

func NewCustomerRepo(db *DB) *CustomerRepo { return &CustomerRepo{db: db} }

// -----------------------------------------------------------------------------
// Create
// -----------------------------------------------------------------------------

func (r *CustomerRepo) Create(ctx context.Context, scope tenant.TenantScope, c *customer.Customer) error {
	if c.OrganizationID != scope.OrgID {
		return fmt.Errorf("customer_repo: org mismatch")
	}
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		return r.createTx(ctx, tx, c)
	})
}

// CreateTx is the transaction-aware variant, used when the caller is already
// inside a WithTenant block (e.g. public order submission creating a customer
// and an order together).
func (r *CustomerRepo) CreateTx(ctx context.Context, tx pgx.Tx, c *customer.Customer) error {
	return r.createTx(ctx, tx, c)
}

func (r *CustomerRepo) createTx(ctx context.Context, tx pgx.Tx, c *customer.Customer) error {
	const q = `
		INSERT INTO customers (id, organization_id, name, email, phone, address, notes, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := tx.Exec(ctx, q,
		c.ID, c.OrganizationID, c.Name,
		nullIfEmpty(c.Email), nullIfEmpty(c.Phone), nullIfEmpty(c.Address), nullIfEmpty(c.Notes),
		c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("customer_repo: create: %w", Classify(err))
	}
	return nil
}

// -----------------------------------------------------------------------------
// Read
// -----------------------------------------------------------------------------

func (r *CustomerRepo) GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*customer.Customer, error) {
	var c *customer.Customer
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		var e error
		c, e = r.getByIDTx(ctx, tx, scope, id)
		return e
	})
	return c, err
}

func (r *CustomerRepo) getByIDTx(ctx context.Context, tx pgx.Tx, scope tenant.TenantScope, id uuid.UUID) (*customer.Customer, error) {
	const q = `
		SELECT id, organization_id, name, COALESCE(email,''), COALESCE(phone,''),
		       COALESCE(address,''), COALESCE(notes,''), created_at, updated_at
		FROM customers
		WHERE id = $1
	`
	return scanCustomer(tx.QueryRow(ctx, q, id))
}

// -----------------------------------------------------------------------------
// Update
// -----------------------------------------------------------------------------

func (r *CustomerRepo) Update(ctx context.Context, scope tenant.TenantScope, c *customer.Customer) error {
	if c.OrganizationID != scope.OrgID {
		return fmt.Errorf("customer_repo: org mismatch")
	}
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			UPDATE customers
			SET name = $2, email = $3, phone = $4, address = $5, notes = $6, updated_at = $7
			WHERE id = $1
		`
		ct, err := tx.Exec(ctx, q,
			c.ID, c.Name, nullIfEmpty(c.Email), nullIfEmpty(c.Phone),
			nullIfEmpty(c.Address), nullIfEmpty(c.Notes), c.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("customer_repo: update: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return customer.ErrNotFound
		}
		return nil
	})
}

// -----------------------------------------------------------------------------
// Delete
// -----------------------------------------------------------------------------

// Delete removes a customer. Returns customer.ErrNotFound if the row does not
// belong to the tenant. Fails with a foreign-key error if the customer has
// orders — that's a business decision the app layer surfaces as "cannot delete
// a customer with orders".
func (r *CustomerRepo) Delete(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx, `DELETE FROM customers WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("customer_repo: delete: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return customer.ErrNotFound
		}
		return nil
	})
}

// -----------------------------------------------------------------------------
// List with search, filter, pagination
// -----------------------------------------------------------------------------

// ListOptions controls filtering and pagination.
type ListOptions struct {
	Query  string // free-text: matches name, email, or phone (ILIKE)
	Limit  int    // default 20, max 100
	Offset int    // >= 0
}

// ListResult carries the page and the total count for pagination UI.
type ListResult struct {
	Customers []*customer.Customer
	Total     int
	Limit     int
	Offset    int
}

// List returns a page of customers matching the options, plus a total count.
//
// Search strategy: we use ILIKE '%q%' on name, email, and phone. This is fast
// because of the trigram index on name (migration 0009). For small-org scale
// (thousands of customers), this is more than sufficient. If a tenant ever
// grows to hundreds of thousands, we'd move to tsvector or a dedicated search.
func (r *CustomerRepo) List(ctx context.Context, scope tenant.TenantScope, opts ListOptions) (*ListResult, error) {
	if opts.Limit <= 0 || opts.Limit > 100 {
		opts.Limit = 20
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	q := strings.TrimSpace(opts.Query)

	var out *ListResult
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		// Build the WHERE clause. Two variants so the query planner can use
		// the right index depending on whether we're filtering.
		var where string
		var args []any
		if q == "" {
			where = "TRUE"
		} else {
			where = `(name ILIKE $1 OR COALESCE(email,'') ILIKE $1 OR COALESCE(phone,'') ILIKE $1)`
			args = append(args, "%"+q+"%")
		}

		// Count total matching rows (for pagination).
		var total int
		countSQL := fmt.Sprintf(`SELECT count(*) FROM customers WHERE %s`, where)
		if err := tx.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
			return fmt.Errorf("customer_repo: count: %w", Classify(err))
		}

		// Fetch page.
		pageArgs := append([]any{}, args...)
		pageArgs = append(pageArgs, opts.Limit, opts.Offset)
		listSQL := fmt.Sprintf(`
			SELECT id, organization_id, name, COALESCE(email,''), COALESCE(phone,''),
			       COALESCE(address,''), COALESCE(notes,''), created_at, updated_at
			FROM customers
			WHERE %s
			ORDER BY name ASC
			LIMIT $%d OFFSET $%d
		`, where, len(args)+1, len(args)+2)

		rows, err := tx.Query(ctx, listSQL, pageArgs...)
		if err != nil {
			return fmt.Errorf("customer_repo: list: %w", Classify(err))
		}
		defer rows.Close()

		var customers []*customer.Customer
		for rows.Next() {
			c, err := scanCustomerFromRows(rows)
			if err != nil {
				return err
			}
			customers = append(customers, c)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("customer_repo: rows: %w", err)
		}

		out = &ListResult{
			Customers: customers,
			Total:     total,
			Limit:     opts.Limit,
			Offset:    opts.Offset,
		}
		return nil
	})
	return out, err
}

// -----------------------------------------------------------------------------
// Scanners
// -----------------------------------------------------------------------------

// pgx.Row and pgx.Rows both expose Scan, so we define a tiny interface.
type scannable interface {
	Scan(dest ...any) error
}

func scanCustomer(row scannable) (*customer.Customer, error) {
	var (
		id        uuid.UUID
		orgID     uuid.UUID
		name      string
		email     string
		phone     string
		address   string
		notes     string
		createdAt time.Time
		updatedAt time.Time
	)
	if err := row.Scan(&id, &orgID, &name, &email, &phone, &address, &notes, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, customer.ErrNotFound
		}
		return nil, fmt.Errorf("customer_repo: scan: %w", err)
	}
	return &customer.Customer{
		ID:             id,
		OrganizationID: orgID,
		Name:           name,
		Email:          email,
		Phone:          phone,
		Address:        address,
		Notes:          notes,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}, nil
}

func scanCustomerFromRows(rows pgx.Rows) (*customer.Customer, error) {
	return scanCustomer(rows)
}
