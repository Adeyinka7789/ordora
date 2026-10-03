package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// SearchRepo provides global search across orders, customers, and products.
type SearchRepo struct {
	db *DB
}

func NewSearchRepo(db *DB) *SearchRepo { return &SearchRepo{db: db} }

// OrderHit is one row from the orders search.
type OrderHit struct {
	ID           uuid.UUID
	OrderNumber  string
	Title        string
	Status       string
	CustomerName string
}

// CustomerHit is one row from the customers search.
type CustomerHit struct {
	ID    uuid.UUID
	Name  string
	Email string
	Phone string
}

// ProductHit is one row from the products search.
type ProductHit struct {
	ID       uuid.UUID
	Name     string
	SKU      string
	Currency string
	Price    int64
}

// SearchOrders searches by order number, title, or customer name.
func (r *SearchRepo) SearchOrders(ctx context.Context, scope tenant.TenantScope, q string, limit int) ([]OrderHit, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	var out []OrderHit
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const sql = `
			SELECT o.id, o.order_number, o.title, o.status::text, c.name
			FROM orders o
			JOIN customers c ON c.id = o.customer_id
			WHERE o.order_number ILIKE $1
			   OR o.title ILIKE $1
			   OR c.name ILIKE $1
			ORDER BY o.created_at DESC
			LIMIT $2
		`
		rows, err := tx.Query(ctx, sql, "%"+q+"%", limit)
		if err != nil {
			return fmt.Errorf("search_repo: orders: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var h OrderHit
			if err := rows.Scan(&h.ID, &h.OrderNumber, &h.Title, &h.Status, &h.CustomerName); err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}

// SearchCustomers searches by name, email, or phone.
func (r *SearchRepo) SearchCustomers(ctx context.Context, scope tenant.TenantScope, q string, limit int) ([]CustomerHit, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	var out []CustomerHit
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const sql = `
			SELECT id, name, COALESCE(email::text,''), COALESCE(phone,'')
			FROM customers
			WHERE name ILIKE $1
			   OR COALESCE(email::text,'') ILIKE $1
			   OR COALESCE(phone,'') ILIKE $1
			ORDER BY name ASC
			LIMIT $2
		`
		rows, err := tx.Query(ctx, sql, "%"+q+"%", limit)
		if err != nil {
			return fmt.Errorf("search_repo: customers: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var h CustomerHit
			if err := rows.Scan(&h.ID, &h.Name, &h.Email, &h.Phone); err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}

// SearchProducts searches by name or SKU.
func (r *SearchRepo) SearchProducts(ctx context.Context, scope tenant.TenantScope, q string, limit int) ([]ProductHit, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	var out []ProductHit
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const sql = `
			SELECT id, name, COALESCE(sku,''), currency::text, unit_price_minor
			FROM products
			WHERE active = true
			  AND (name ILIKE $1 OR COALESCE(sku,'') ILIKE $1)
			ORDER BY name ASC
			LIMIT $2
		`
		rows, err := tx.Query(ctx, sql, "%"+q+"%", limit)
		if err != nil {
			return fmt.Errorf("search_repo: products: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var h ProductHit
			if err := rows.Scan(&h.ID, &h.Name, &h.SKU, &h.Currency, &h.Price); err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}
