package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AdminLookupRepo provides targeted search across the platform.
// Unlike the data browser, this returns *specific* rows with joins, so
// the admin sees a coherent view of an order or a customer.
type AdminLookupRepo struct {
	adminDB *DB
}

func NewAdminLookupRepo(adminDB *DB) *AdminLookupRepo {
	return &AdminLookupRepo{adminDB: adminDB}
}

// OrderLookup is a full view of one order for support.
type OrderLookup struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	OrgName        string
	OrgSlug        string
	OrderNumber    string
	Title          string
	Status         string
	Currency       string
	SubtotalMinor  int64
	DiscountMinor  int64
	TaxMinor       int64
	TotalMinor     int64
	PaidMinor      int64
	CustomerID     uuid.UUID
	CustomerName   string
	CustomerEmail  string
	CustomerPhone  string
	ExpectedDate   *time.Time
	CreatedAt      time.Time
	Items          []OrderLookupItem
	Payments       []OrderLookupPayment
}

type OrderLookupItem struct {
	Description    string
	Quantity       float64
	UnitPriceMinor int64
	SubtotalMinor  int64
}

type OrderLookupPayment struct {
	ID          uuid.UUID
	AmountMinor int64
	Method      string
	Reference   string
	PaidAt      time.Time
	CreatedAt   time.Time
}

// LookupOrderByNumber finds one order by its order_number.
func (r *AdminLookupRepo) LookupOrderByNumber(ctx context.Context, number string) (*OrderLookup, error) {
	var out *OrderLookup
	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT o.id, o.organization_id, org.name, org.slug::text,
			       o.order_number, o.title, o.status::text, o.currency::text,
			       o.subtotal_minor, o.discount_minor, o.tax_minor, o.total_minor, o.amount_paid_minor,
			       c.id, c.name, COALESCE(c.email::text,''), COALESCE(c.phone,''),
			       o.expected_completion, o.created_at
			FROM orders o
			JOIN organizations org ON org.id = o.organization_id
			JOIN customers c ON c.id = o.customer_id
			WHERE o.order_number = $1
		`
		var ol OrderLookup
		if err := tx.QueryRow(ctx, q, number).Scan(
			&ol.ID, &ol.OrganizationID, &ol.OrgName, &ol.OrgSlug,
			&ol.OrderNumber, &ol.Title, &ol.Status, &ol.Currency,
			&ol.SubtotalMinor, &ol.DiscountMinor, &ol.TaxMinor, &ol.TotalMinor, &ol.PaidMinor,
			&ol.CustomerID, &ol.CustomerName, &ol.CustomerEmail, &ol.CustomerPhone,
			&ol.ExpectedDate, &ol.CreatedAt,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}

		// Items.
		const iQ = `
			SELECT description, quantity, unit_price_minor, subtotal_minor
			FROM order_items
			WHERE order_id = $1
			ORDER BY position ASC, id ASC
		`
		irs, err := tx.Query(ctx, iQ, ol.ID)
		if err != nil {
			return err
		}
		for irs.Next() {
			var it OrderLookupItem
			if err := irs.Scan(&it.Description, &it.Quantity, &it.UnitPriceMinor, &it.SubtotalMinor); err != nil {
				irs.Close()
				return err
			}
			ol.Items = append(ol.Items, it)
		}
		irs.Close()

		// Payments.
		const pQ = `
			SELECT id, amount_minor, method::text, COALESCE(reference,''), paid_at, created_at
			FROM payments
			WHERE order_id = $1
			ORDER BY paid_at DESC, created_at DESC
		`
		prs, err := tx.Query(ctx, pQ, ol.ID)
		if err != nil {
			return err
		}
		for prs.Next() {
			var p OrderLookupPayment
			if err := prs.Scan(&p.ID, &p.AmountMinor, &p.Method, &p.Reference, &p.PaidAt, &p.CreatedAt); err != nil {
				prs.Close()
				return err
			}
			ol.Payments = append(ol.Payments, p)
		}
		prs.Close()

		out = &ol
		return nil
	})
	return out, err
}

// CustomerLookup is a full view of one customer.
type CustomerLookup struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	OrgName        string
	OrgSlug        string
	Currency       string
	Name           string
	Email          string
	Phone          string
	Address        string
	Notes          string
	CreatedAt      time.Time
	Orders         []CustomerLookupOrder
	TotalBilled    int64
	TotalPaid      int64
}

type CustomerLookupOrder struct {
	ID          uuid.UUID
	OrderNumber string
	Title       string
	Status      string
	TotalMinor  int64
	PaidMinor   int64
	CreatedAt   time.Time
}

// LookupCustomersByEmail finds customers matching an email (case-insensitive).
func (r *AdminLookupRepo) LookupCustomersByEmail(ctx context.Context, email string) ([]*CustomerLookup, error) {
	return r.lookupCustomers(ctx, "c.email::text ILIKE $1", "%"+email+"%")
}

// LookupCustomersByPhone finds customers matching a phone.
func (r *AdminLookupRepo) LookupCustomersByPhone(ctx context.Context, phone string) ([]*CustomerLookup, error) {
	return r.lookupCustomers(ctx, "COALESCE(c.phone,'') ILIKE $1", "%"+phone+"%")
}

func (r *AdminLookupRepo) lookupCustomers(ctx context.Context, where, arg string) ([]*CustomerLookup, error) {
	var out []*CustomerLookup
	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		q := `
			SELECT c.id, c.organization_id, o.name, o.slug::text, o.currency::text,
			       c.name, COALESCE(c.email::text,''), COALESCE(c.phone,''),
			       COALESCE(c.address,''), COALESCE(c.notes,''), c.created_at
			FROM customers c
			JOIN organizations o ON o.id = c.organization_id
			WHERE ` + where + `
			ORDER BY c.created_at DESC
			LIMIT 25
		`
		rows, err := tx.Query(ctx, q, arg)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c CustomerLookup
			if err := rows.Scan(&c.ID, &c.OrganizationID, &c.OrgName, &c.OrgSlug, &c.Currency,
				&c.Name, &c.Email, &c.Phone, &c.Address, &c.Notes, &c.CreatedAt); err != nil {
				return err
			}
			out = append(out, &c)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		// Load recent orders for all customers in one query (newest 20
		// per customer), then group in Go. Replaces one query per
		// customer (N+1).
		ids := make([]uuid.UUID, 0, len(out))
		for _, c := range out {
			ids = append(ids, c.ID)
		}
		const oQ = `
			SELECT customer_id, id, order_number, title, status::text,
			       total_minor, amount_paid_minor, created_at
			FROM (
				SELECT customer_id, id, order_number, title, status,
				       total_minor, amount_paid_minor, created_at,
				       ROW_NUMBER() OVER (PARTITION BY customer_id ORDER BY created_at DESC) AS rn
				FROM orders
				WHERE customer_id = ANY($1)
			) ranked
			WHERE rn <= 20
			ORDER BY customer_id, created_at DESC
		`
		ors, err := tx.Query(ctx, oQ, ids)
		if err != nil {
			return err
		}
		defer ors.Close()
		byCustomer := make(map[uuid.UUID]*CustomerLookup, len(out))
		for _, c := range out {
			byCustomer[c.ID] = c
		}
		for ors.Next() {
			var customerID uuid.UUID
			var co CustomerLookupOrder
			if err := ors.Scan(&customerID, &co.ID, &co.OrderNumber, &co.Title, &co.Status, &co.TotalMinor, &co.PaidMinor, &co.CreatedAt); err != nil {
				return err
			}
			if c, ok := byCustomer[customerID]; ok {
				c.Orders = append(c.Orders, co)
				c.TotalBilled += co.TotalMinor
				c.TotalPaid += co.PaidMinor
			}
		}
		return ors.Err()
	})
	return out, err
}

// ResendNotificationJob clones a failed notification_jobs row into a fresh
// PENDING row so the worker picks it up again.
func (r *AdminLookupRepo) ResendNotificationJob(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	var newID uuid.UUID
	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			INSERT INTO notification_jobs (id, organization_id, kind, channel, recipient, payload, idempotency_key, status, next_attempt_at)
			SELECT gen_random_uuid(), organization_id, kind, channel, recipient, payload,
			       idempotency_key || '-resend-' || extract(epoch from now())::text,
			       'PENDING', now()
			FROM notification_jobs
			WHERE id = $1
			RETURNING id
		`
		if err := tx.QueryRow(ctx, q, id).Scan(&newID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		return nil
	})
	return newID, err
}

// NotificationJobRow is a summary of one job.
type NotificationJobRow struct {
	ID        uuid.UUID
	Kind      string
	Recipient string
	Status    string
	Attempts  int
	LastError string
	NextAt    time.Time
	CreatedAt time.Time
	SentAt    *time.Time
}

// RecentNotificationJobs returns the last N jobs, newest first.
func (r *AdminLookupRepo) RecentNotificationJobs(ctx context.Context, limit int) ([]NotificationJobRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []NotificationJobRow
	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT id, kind, recipient, status, attempts, COALESCE(last_error,''),
			       next_attempt_at, created_at, sent_at
			FROM notification_jobs
			ORDER BY created_at DESC
			LIMIT $1
		`
		rows, err := tx.Query(ctx, q, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var j NotificationJobRow
			if err := rows.Scan(&j.ID, &j.Kind, &j.Recipient, &j.Status, &j.Attempts,
				&j.LastError, &j.NextAt, &j.CreatedAt, &j.SentAt); err != nil {
				return err
			}
			out = append(out, j)
		}
		return rows.Err()
	})
	return out, err
}
