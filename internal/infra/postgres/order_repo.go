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
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// OrderRepo persists orders and their line items. Orders are tenant-scoped.
// Every method takes a TenantScope and runs inside WithTenant.
type OrderRepo struct {
	db *DB
}

func NewOrderRepo(db *DB) *OrderRepo { return &OrderRepo{db: db} }

// -----------------------------------------------------------------------------
// Create
// -----------------------------------------------------------------------------

// Create inserts the order and all its items in one transaction.
//
// The caller is expected to have allocated an order number already (via
// OrderNumberRepo.AllocateTx) inside the same transaction. Create assumes the
// order.Number field is already set.
func (r *OrderRepo) CreateTx(ctx context.Context, tx pgx.Tx, o *order.Order) error {
	if o.OrganizationID == uuid.Nil {
		return fmt.Errorf("order_repo: missing org")
	}
	if err := o.Validate(); err != nil {
		return err
	}

	const insertOrder = `
		INSERT INTO orders (
			id, organization_id, customer_id, order_number,
			public_token_hash, title, description,
			status, currency,
			subtotal_minor, discount_minor, tax_minor, total_minor, amount_paid_minor,
			expected_completion, delivered_at,
			created_by, created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
	`
	_, err := tx.Exec(ctx, insertOrder,
		o.ID, o.OrganizationID, o.CustomerID, o.Number, o.PublicTokenHash, o.Title, nullIfEmpty(o.Description),
		string(o.Status), o.Currency,
		o.Subtotal.Amount(), o.Discount.Amount(), o.Tax.Amount(), o.Total.Amount(), o.Paid.Amount(),
		o.ExpectedCompletion, o.DeliveredAt,
		o.CreatedBy, o.CreatedAt, o.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("order_repo: insert order: %w", Classify(err))
	}

	for _, it := range o.Items {
		if err := r.insertItemTx(ctx, tx, o.OrganizationID, o.ID, it); err != nil {
			return err
		}
	}
	return nil
}

func (r *OrderRepo) insertItemTx(ctx context.Context, tx pgx.Tx, orgID, orderID uuid.UUID, it *order.Item) error {
	const q = `
		INSERT INTO order_items (id, organization_id, order_id, description, quantity, unit_price_minor, subtotal_minor, position, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`
	_, err := tx.Exec(ctx, q,
		it.ID, orgID, orderID, it.Description,
		quantityToNumeric(it.Quantity), it.UnitPrice.Amount(), it.Subtotal.Amount(),
		it.Position, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("order_repo: insert item: %w", Classify(err))
	}
	return nil
}

// -----------------------------------------------------------------------------
// Read
// -----------------------------------------------------------------------------

// GetByID loads an order and its items.
func (r *OrderRepo) GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*order.Order, error) {
	var o *order.Order
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		var e error
		o, e = r.getByIDTx(ctx, tx, scope, id)
		return e
	})
	return o, err
}

func (r *OrderRepo) getByIDTx(ctx context.Context, tx pgx.Tx, scope tenant.TenantScope, id uuid.UUID) (*order.Order, error) {
	const q = `
		SELECT
			id, organization_id, customer_id, order_number,
			public_token_hash, title, COALESCE(description,''),
			status, currency,
			subtotal_minor, discount_minor, tax_minor, total_minor, amount_paid_minor,
			expected_completion, delivered_at,
			created_by, created_at, updated_at
		FROM orders
		WHERE id = $1
	`
	o, err := scanOrderRow(tx.QueryRow(ctx, q, id))
	if err != nil {
		return nil, err
	}
	items, err := r.loadItemsTx(ctx, tx, o.ID, o.Currency)
	if err != nil {
		return nil, err
	}
	o.Items = items
	return o, nil
}

// GetByIDForUpdate loads the order inside the caller's transaction with a
// row lock. Concurrent writers block until the holder commits or rolls
// back — this closes the check-then-write race in payment recording.
func (r *OrderRepo) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, scope tenant.TenantScope, id uuid.UUID) (*order.Order, error) {
	const q = `
		SELECT
			id, organization_id, customer_id, order_number,
			public_token_hash, title, COALESCE(description,''),
			status, currency,
			subtotal_minor, discount_minor, tax_minor, total_minor, amount_paid_minor,
			expected_completion, delivered_at,
			created_by, created_at, updated_at
		FROM orders
		WHERE id = $1
		FOR UPDATE
	`
	o, err := scanOrderRow(tx.QueryRow(ctx, q, id))
	if err != nil {
		return nil, err
	}
	items, err := r.loadItemsTx(ctx, tx, o.ID, o.Currency)
	if err != nil {
		return nil, err
	}
	o.Items = items
	return o, nil
}

func (r *OrderRepo) loadItemsTx(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, currency string) ([]*order.Item, error) {
	const q = `
		SELECT id, description, quantity, unit_price_minor, subtotal_minor, position
		FROM order_items
		WHERE order_id = $1
		ORDER BY position ASC, id ASC
	`
	rows, err := tx.Query(ctx, q, orderID)
	if err != nil {
		return nil, fmt.Errorf("order_repo: load items: %w", Classify(err))
	}
	defer rows.Close()

	var items []*order.Item
	for rows.Next() {
		var (
			id            uuid.UUID
			desc          string
			qtyNumeric    float64
			unitPrice     int64
			subtotalMinor int64
			position      int
		)
		if err := rows.Scan(&id, &desc, &qtyNumeric, &unitPrice, &subtotalMinor, &position); err != nil {
			return nil, fmt.Errorf("order_repo: scan item: %w", err)
		}
		up, _ := money.New(unitPrice, currency)
		st, _ := money.New(subtotalMinor, currency)
		items = append(items, &order.Item{
			ID:          id,
			Description: desc,
			Quantity:    numericToQuantity(qtyNumeric),
			UnitPrice:   up,
			Subtotal:    st,
			Position:    position,
		})
	}
	return items, rows.Err()
}

// -----------------------------------------------------------------------------
// Update
// -----------------------------------------------------------------------------

// UpdateTx replaces the order row and rebuilds the item set.
//
// Strategy for items: delete all existing items and re-insert from the domain
// object. This is simplest and correct — items have no independent identity
// outside the order aggregate, so "replace all" is the right semantic. The
// cost is a few extra writes; orders are small and this is not a hot path.
func (r *OrderRepo) UpdateTx(ctx context.Context, tx pgx.Tx, o *order.Order) error {
	if err := o.Validate(); err != nil {
		return err
	}

	const updateOrder = `
		UPDATE orders SET
			title = $2, description = $3, status = $4, currency = $5,
			subtotal_minor = $6, discount_minor = $7, tax_minor = $8,
			total_minor = $9, amount_paid_minor = $10,
			expected_completion = $11, delivered_at = $12,
			updated_at = $13
		WHERE id = $1
	`
	ct, err := tx.Exec(ctx, updateOrder,
		o.ID, o.Title, nullIfEmpty(o.Description), string(o.Status), o.Currency,
		o.Subtotal.Amount(), o.Discount.Amount(), o.Tax.Amount(),
		o.Total.Amount(), o.Paid.Amount(),
		o.ExpectedCompletion, o.DeliveredAt,
		o.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("order_repo: update order: %w", Classify(err))
	}
	if ct.RowsAffected() == 0 {
		return order.ErrNotFound
	}

	// Delete existing items, then re-insert.
	if _, err := tx.Exec(ctx, `DELETE FROM order_items WHERE order_id = $1`, o.ID); err != nil {
		return fmt.Errorf("order_repo: delete items: %w", Classify(err))
	}
	for _, it := range o.Items {
		if err := r.insertItemTx(ctx, tx, o.OrganizationID, o.ID, it); err != nil {
			return err
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// Status change (optimized path — does not touch items)
// -----------------------------------------------------------------------------

// ChangeStatusTx updates only the status and updated_at columns, then writes
// an audit row. This is the fast path for the status-change button.
//
// The domain change (state machine validation) is applied by the caller before
// calling this. This method only persists.
func (r *OrderRepo) ChangeStatusTx(ctx context.Context, tx pgx.Tx, o *order.Order, from, to order.Status, actorUserID uuid.UUID, now time.Time) error {
	const q = `UPDATE orders SET status = $2, delivered_at = $3, updated_at = $4 WHERE id = $1`
	ct, err := tx.Exec(ctx, q, o.ID, string(to), o.DeliveredAt, now)
	if err != nil {
		return fmt.Errorf("order_repo: change status: %w", Classify(err))
	}
	if ct.RowsAffected() == 0 {
		return order.ErrNotFound
	}

	// Audit entry.
	const auditQ = `
		INSERT INTO audit_logs (id, organization_id, actor_user_id, action, entity_type, entity_id, before, after, created_at)
		VALUES ($1, $2, $3, 'order.status_changed', 'ORDER', $4, $5, $6, $7)
	`
	before := fmt.Sprintf(`{"status":%q}`, string(from))
	after := fmt.Sprintf(`{"status":%q}`, string(to))
	if _, err := tx.Exec(ctx, auditQ,
		uuid.New(), o.OrganizationID, actorUserID, o.ID, before, after, now,
	); err != nil {
		return fmt.Errorf("order_repo: audit status change: %w", Classify(err))
	}
	return nil
}

// -----------------------------------------------------------------------------
// List with filters and pagination
// -----------------------------------------------------------------------------

// OrderListOptions controls filtering and pagination for the orders list.
type OrderListOptions struct {
	Query      string       // free-text: order number, customer name, title
	Status     order.Status // empty = all
	CustomerID uuid.UUID    // nil UUID = all
	DueBefore  *time.Time   // orders with expected_completion <= this
	Limit      int
	Offset     int
}

// OrderListRow is one row of the orders list: the order plus display fields
// joined from customers.
type OrderListRow struct {
	Order        *order.Order
	CustomerName string
}

// OrderListResult carries the page and total count.
type OrderListResult struct {
	Rows   []OrderListRow
	Total  int
	Limit  int
	Offset int
}

func (r *OrderRepo) List(ctx context.Context, scope tenant.TenantScope, opts OrderListOptions) (*OrderListResult, error) {
	if opts.Limit <= 0 || opts.Limit > 100 {
		opts.Limit = 20
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}

	var out *OrderListResult
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		where, args := buildOrderListWhere(opts)

		var total int
		countSQL := fmt.Sprintf(`SELECT count(*) FROM orders o JOIN customers c ON c.id = o.customer_id WHERE %s`, where)
		if err := tx.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
			return fmt.Errorf("order_repo: count: %w", Classify(err))
		}

		pageArgs := append([]any{}, args...)
		pageArgs = append(pageArgs, opts.Limit, opts.Offset)
		listSQL := fmt.Sprintf(`
			SELECT
				o.id, o.organization_id, o.customer_id, o.order_number, o.title, COALESCE(o.description,''),
				o.status, o.currency,
				o.subtotal_minor, o.discount_minor, o.tax_minor, o.total_minor, o.amount_paid_minor,
				o.expected_completion, o.delivered_at,
				o.created_by, o.created_at, o.updated_at,
				c.name AS customer_name
			FROM orders o
			JOIN customers c ON c.id = o.customer_id
			WHERE %s
			ORDER BY o.created_at DESC
			LIMIT $%d OFFSET $%d
		`, where, len(args)+1, len(args)+2)

		rows, err := tx.Query(ctx, listSQL, pageArgs...)
		if err != nil {
			return fmt.Errorf("order_repo: list: %w", Classify(err))
		}
		defer rows.Close()

		var outRows []OrderListRow
		for rows.Next() {
			o, customerName, err := scanOrderListRow(rows)
			if err != nil {
				return err
			}
			outRows = append(outRows, OrderListRow{Order: o, CustomerName: customerName})
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("order_repo: rows: %w", err)
		}

		out = &OrderListResult{
			Rows:   outRows,
			Total:  total,
			Limit:  opts.Limit,
			Offset: opts.Offset,
		}
		return nil
	})
	return out, err
}

// buildOrderListWhere returns the WHERE clause and positional args.
// All user input goes through args; only our own structure is interpolated.
func buildOrderListWhere(opts OrderListOptions) (string, []any) {
	var clauses []string
	var args []any

	q := strings.TrimSpace(opts.Query)
	if q != "" {
		args = append(args, "%"+q+"%")
		n := len(args)
		clauses = append(clauses, fmt.Sprintf(
			`(o.order_number ILIKE $%d OR o.title ILIKE $%d OR c.name ILIKE $%d)`,
			n, n, n,
		))
	}
	if opts.Status != "" {
		args = append(args, string(opts.Status))
		clauses = append(clauses, fmt.Sprintf(`o.status = $%d`, len(args)))
	}
	if opts.CustomerID != uuid.Nil {
		args = append(args, opts.CustomerID)
		clauses = append(clauses, fmt.Sprintf(`o.customer_id = $%d`, len(args)))
	}
	if opts.DueBefore != nil {
		args = append(args, *opts.DueBefore)
		clauses = append(clauses, fmt.Sprintf(`o.expected_completion <= $%d`, len(args)))
	}
	if len(clauses) == 0 {
		return "TRUE", args
	}
	return strings.Join(clauses, " AND "), args
}

// -----------------------------------------------------------------------------
// Scanners
// -----------------------------------------------------------------------------

func scanOrderRow(row scannable) (*order.Order, error) {
	var (
		id          uuid.UUID
		orgID       uuid.UUID
		customerID  uuid.UUID
		number      string
		tokenHash   []byte
		title       string
		description string
		statusStr   string
		currency    string
		subtotal    int64
		discount    int64
		tax         int64
		total       int64
		paid        int64
		expected    *time.Time
		delivered   *time.Time
		createdBy   uuid.UUID
		createdAt   time.Time
		updatedAt   time.Time
	)
	if err := row.Scan(&id, &orgID, &customerID, &number, &tokenHash, &title, &description,
		&statusStr, &currency, &subtotal, &discount, &tax, &total, &paid,
		&expected, &delivered, &createdBy, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, order.ErrNotFound
		}
		return nil, fmt.Errorf("order_repo: scan: %w", err)
	}

	sub, _ := money.New(subtotal, currency)
	dis, _ := money.New(discount, currency)
	tx, _ := money.New(tax, currency)
	tot, _ := money.New(total, currency)
	pd, _ := money.New(paid, currency)

	return &order.Order{
		ID:                 id,
		OrganizationID:     orgID,
		CustomerID:         customerID,
		Number:             number,
		PublicTokenHash:    tokenHash,
		Title:              title,
		Description:        description,
		Status:             order.Status(statusStr),
		Currency:           currency,
		Subtotal:           sub,
		Discount:           dis,
		Tax:                tx,
		Total:              tot,
		Paid:               pd,
		ExpectedCompletion: expected,
		DeliveredAt:        delivered,
		CreatedBy:          createdBy,
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}, nil
}

func scanOrderListRow(rows pgx.Rows) (*order.Order, string, error) {
	var (
		id           uuid.UUID
		orgID        uuid.UUID
		customerID   uuid.UUID
		number       string
		title        string
		description  string
		statusStr    string
		currency     string
		subtotal     int64
		discount     int64
		tax          int64
		total        int64
		paid         int64
		expected     *time.Time
		delivered    *time.Time
		createdBy    uuid.UUID
		createdAt    time.Time
		updatedAt    time.Time
		customerName string
	)
	if err := rows.Scan(&id, &orgID, &customerID, &number, &title, &description,
		&statusStr, &currency, &subtotal, &discount, &tax, &total, &paid,
		&expected, &delivered, &createdBy, &createdAt, &updatedAt,
		&customerName); err != nil {
		return nil, "", fmt.Errorf("order_repo: scan list: %w", err)
	}

	sub, _ := money.New(subtotal, currency)
	dis, _ := money.New(discount, currency)
	tx, _ := money.New(tax, currency)
	tot, _ := money.New(total, currency)
	pd, _ := money.New(paid, currency)

	return &order.Order{
		ID:                 id,
		OrganizationID:     orgID,
		CustomerID:         customerID,
		Number:             number,
		Title:              title,
		Description:        description,
		Status:             order.Status(statusStr),
		Currency:           currency,
		Subtotal:           sub,
		Discount:           dis,
		Tax:                tx,
		Total:              tot,
		Paid:               pd,
		ExpectedCompletion: expected,
		DeliveredAt:        delivered,
		CreatedBy:          createdBy,
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}, customerName, nil
}

// -----------------------------------------------------------------------------
// Quantity conversion
//
// order_items.quantity is NUMERIC(12,3) in the DB. We translate to/from
// int64 scaled by QuantityScale (1000) so the domain never sees floats.
// -----------------------------------------------------------------------------

func quantityToNumeric(q int64) float64 {
	return float64(q) / float64(order.QuantityScale)
}

func numericToQuantity(n float64) int64 {
	return int64(n*float64(order.QuantityScale) + 0.5)
}
