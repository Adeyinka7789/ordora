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
	"github.com/Adeyinka7789/ordora/internal/domain/payment"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// PaymentRepo persists payments. Tenant-scoped.
//
// Payments are append-only: this repo has no Update or Delete. Corrections
// use InsertReversal (to be added later).
type PaymentRepo struct {
	db *DB
}

func NewPaymentRepo(db *DB) *PaymentRepo { return &PaymentRepo{db: db} }

// -----------------------------------------------------------------------------
// Create
// -----------------------------------------------------------------------------

// CreateTx inserts a payment inside an existing transaction. The orders
// trigger (trg_payments_refresh_order) updates orders.amount_paid_minor
// automatically.
func (r *PaymentRepo) CreateTx(ctx context.Context, tx pgx.Tx, p *payment.Payment) error {
	const q = `
		INSERT INTO payments
			(id, organization_id, order_id, amount_minor, currency, method, reference, paid_at, notes,
			 reverses, reversed_by, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	`
	_, err := tx.Exec(ctx, q,
		p.ID, p.OrganizationID, p.OrderID,
		p.Amount.Amount(), p.Amount.Currency(),
		string(p.Method),
		nullIfEmpty(p.Reference),
		p.PaidAt,
		nullIfEmpty(p.Notes),
		p.Reverses, p.ReversedBy,
		p.CreatedBy, p.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("payment_repo: create: %w", Classify(err))
	}
	return nil
}

// Create wraps CreateTx in its own transaction. Prefer CreateTx when the
// caller is already inside a transaction.
func (r *PaymentRepo) Create(ctx context.Context, scope tenant.TenantScope, p *payment.Payment) error {
	if p.OrganizationID != scope.OrgID {
		return fmt.Errorf("payment_repo: org mismatch")
	}
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		return r.CreateTx(ctx, tx, p)
	})
}

// -----------------------------------------------------------------------------
// Read
// -----------------------------------------------------------------------------

// ListForOrder returns all payments for an order, newest paid_at first.
// Includes reversals (which contribute 0 to the balance).
func (r *PaymentRepo) ListForOrder(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) ([]*payment.Payment, error) {
	var out []*payment.Payment
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT id, organization_id, order_id, amount_minor, currency, method,
			       COALESCE(reference,''), paid_at, COALESCE(notes,''),
			       reverses, reversed_by, created_by, created_at
			FROM payments
			WHERE order_id = $1
			ORDER BY paid_at DESC, created_at DESC
		`
		rows, err := tx.Query(ctx, q, orderID)
		if err != nil {
			return fmt.Errorf("payment_repo: list: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanPayment(rows)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// GetByID loads one payment.
func (r *PaymentRepo) GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*payment.Payment, error) {
	var p *payment.Payment
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT id, organization_id, order_id, amount_minor, currency, method,
			       COALESCE(reference,''), paid_at, COALESCE(notes,''),
			       reverses, reversed_by, created_by, created_at
			FROM payments
			WHERE id = $1
		`
		var e error
		p, e = scanPayment(tx.QueryRow(ctx, q, id))
		return e
	})
	return p, err
}

// SumActiveForOrder returns the total of non-reversed, non-reversal payments
// for an order. This is the authoritative figure that should equal
// orders.amount_paid_minor.
//
// Used by tests and by a future "reconcile" job.
func (r *PaymentRepo) SumActiveForOrder(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID, currency string) (money.Money, error) {
	var minor int64
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT COALESCE(SUM(amount_minor), 0)
			FROM payments
			WHERE order_id = $1
			  AND reverses IS NULL
			  AND reversed_by IS NULL
		`
		return tx.QueryRow(ctx, q, orderID).Scan(&minor)
	})
	if err != nil {
		return money.Money{}, err
	}
	return money.New(minor, currency)
}

// GetByIDTx loads a payment inside an existing transaction.
func (r *PaymentRepo) GetByIDTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*payment.Payment, error) {
	const q = `
		SELECT id, organization_id, order_id, amount_minor, currency, method,
		       COALESCE(reference,''), paid_at, COALESCE(notes,''),
		       reverses, reversed_by, created_by, created_at
		FROM payments
		WHERE id = $1
	`
	return scanPayment(tx.QueryRow(ctx, q, id))
}

// MarkReversedTx sets the reversed_by field on a payment. Used when a
// reversal is inserted; the original gains a pointer to the reversal row.
//
// This is the ONLY mutation allowed on a payment row.
func (r *PaymentRepo) MarkReversedTx(ctx context.Context, tx pgx.Tx, originalID, reversalID uuid.UUID) error {
	const q = `UPDATE payments SET reversed_by = $2 WHERE id = $1 AND reversed_by IS NULL`
	ct, err := tx.Exec(ctx, q, originalID, reversalID)
	if err != nil {
		return fmt.Errorf("payment_repo: mark reversed: %w", Classify(err))
	}
	if ct.RowsAffected() == 0 {
		// Either the payment doesn't exist, or it's already reversed.
		return payment.ErrNotFound
	}
	return nil
}

// -----------------------------------------------------------------------------
// Ledger: cross-order listing with filters and pagination
// -----------------------------------------------------------------------------

// LedgerOptions controls filtering and pagination for the ledger view.
type LedgerOptions struct {
	Query        string // free-text: order number, customer name, reference
	Method       string // payment method, empty = all
	PaidFrom     *time.Time
	PaidBefore   *time.Time // exclusive upper bound on paid_at
	HideReversed bool       // exclude reversed payments and reversals
	Limit        int
	Offset       int
}

// LedgerRow is one row of the ledger: the payment plus display fields
// joined from orders and customers.
type LedgerRow struct {
	Payment      *payment.Payment
	OrderID      uuid.UUID
	OrderNumber  string
	CustomerName string
}

// LedgerResult carries the page and total count.
type LedgerResult struct {
	Rows   []LedgerRow
	Total  int
	Limit  int
	Offset int
}

// LedgerMethodTotal aggregates one payment method.
type LedgerMethodTotal struct {
	Method     payment.Method
	Count      int
	TotalMinor int64
}

// LedgerTotals aggregates active (non-reversed, non-reversal) payments
// matching the same filters.
type LedgerTotals struct {
	Count      int
	TotalMinor int64
	ByMethod   []LedgerMethodTotal
}

// Ledger returns a page of payments across all orders, newest first.
func (r *PaymentRepo) Ledger(ctx context.Context, scope tenant.TenantScope, opts LedgerOptions) (*LedgerResult, error) {
	if opts.Limit <= 0 || opts.Limit > 5000 {
		opts.Limit = 20
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}

	var out *LedgerResult
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		where, args := buildLedgerWhere(opts)

		var total int
		countSQL := fmt.Sprintf(`
			SELECT count(*)
			FROM payments p
			JOIN orders o ON o.id = p.order_id
			JOIN customers c ON c.id = o.customer_id
			WHERE %s`, where)
		if err := tx.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
			return fmt.Errorf("payment_repo: ledger count: %w", Classify(err))
		}

		pageArgs := append([]any{}, args...)
		pageArgs = append(pageArgs, opts.Limit, opts.Offset)
		listSQL := fmt.Sprintf(`
			SELECT
				p.id, p.organization_id, p.order_id, p.amount_minor, p.currency, p.method,
				COALESCE(p.reference,''), p.paid_at, COALESCE(p.notes,''),
				p.reverses, p.reversed_by, p.created_by, p.created_at,
				o.id, o.order_number, c.name
			FROM payments p
			JOIN orders o ON o.id = p.order_id
			JOIN customers c ON c.id = o.customer_id
			WHERE %s
			ORDER BY p.paid_at DESC, p.created_at DESC
			LIMIT $%d OFFSET $%d
		`, where, len(args)+1, len(args)+2)

		rows, err := tx.Query(ctx, listSQL, pageArgs...)
		if err != nil {
			return fmt.Errorf("payment_repo: ledger list: %w", Classify(err))
		}
		defer rows.Close()

		var outRows []LedgerRow
		for rows.Next() {
			row, err := scanLedgerRow(rows)
			if err != nil {
				return err
			}
			outRows = append(outRows, row)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("payment_repo: ledger rows: %w", err)
		}

		out = &LedgerResult{Rows: outRows, Total: total, Limit: opts.Limit, Offset: opts.Offset}
		return nil
	})
	return out, err
}

// LedgerTotals aggregates active payments matching the same filters.
func (r *PaymentRepo) LedgerTotals(ctx context.Context, scope tenant.TenantScope, opts LedgerOptions) (*LedgerTotals, error) {
	tot := &LedgerTotals{}
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		where, args := buildLedgerWhere(opts)
		activeWhere := where + ` AND p.reverses IS NULL AND p.reversed_by IS NULL`

		grandSQL := fmt.Sprintf(`
			SELECT count(*), COALESCE(SUM(p.amount_minor), 0)
			FROM payments p
			JOIN orders o ON o.id = p.order_id
			JOIN customers c ON c.id = o.customer_id
			WHERE %s`, activeWhere)
		if err := tx.QueryRow(ctx, grandSQL, args...).Scan(&tot.Count, &tot.TotalMinor); err != nil {
			return fmt.Errorf("payment_repo: ledger totals: %w", Classify(err))
		}

		byMethodSQL := fmt.Sprintf(`
			SELECT p.method, count(*), COALESCE(SUM(p.amount_minor), 0)
			FROM payments p
			JOIN orders o ON o.id = p.order_id
			JOIN customers c ON c.id = o.customer_id
			WHERE %s
			GROUP BY p.method
			ORDER BY SUM(p.amount_minor) DESC`, activeWhere)
		rows, err := tx.Query(ctx, byMethodSQL, args...)
		if err != nil {
			return fmt.Errorf("payment_repo: ledger by-method: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var m LedgerMethodTotal
			var methodStr string
			if err := rows.Scan(&methodStr, &m.Count, &m.TotalMinor); err != nil {
				return fmt.Errorf("payment_repo: ledger by-method scan: %w", err)
			}
			m.Method = payment.Method(methodStr)
			tot.ByMethod = append(tot.ByMethod, m)
		}
		return rows.Err()
	})
	return tot, err
}

// buildLedgerWhere returns the WHERE clause and positional args.
// All user input goes through args; only our own structure is interpolated.
func buildLedgerWhere(opts LedgerOptions) (string, []any) {
	clauses := []string{"TRUE"}
	var args []any

	q := strings.TrimSpace(opts.Query)
	if q != "" {
		args = append(args, "%"+q+"%")
		n := len(args)
		clauses = append(clauses, fmt.Sprintf(
			`(o.order_number ILIKE $%d OR c.name ILIKE $%d OR COALESCE(p.reference,'') ILIKE $%d)`,
			n, n, n,
		))
	}
	if opts.Method != "" {
		args = append(args, opts.Method)
		clauses = append(clauses, fmt.Sprintf(`p.method = $%d`, len(args)))
	}
	if opts.PaidFrom != nil {
		args = append(args, *opts.PaidFrom)
		clauses = append(clauses, fmt.Sprintf(`p.paid_at >= $%d`, len(args)))
	}
	if opts.PaidBefore != nil {
		args = append(args, *opts.PaidBefore)
		clauses = append(clauses, fmt.Sprintf(`p.paid_at < $%d`, len(args)))
	}
	if opts.HideReversed {
		clauses = append(clauses, `p.reverses IS NULL AND p.reversed_by IS NULL`)
	}
	return strings.Join(clauses, " AND "), args
}

func scanLedgerRow(row scannable) (LedgerRow, error) {
	var (
		id, orgID, orderID   uuid.UUID
		amountMinor          int64
		currency, methodStr  string
		reference            string
		paidAt               time.Time
		notes                string
		reverses, reversedBy *uuid.UUID
		createdBy            uuid.UUID
		createdAt            time.Time
		joinedOrderID        uuid.UUID
		orderNumber          string
		customerName         string
	)
	if err := row.Scan(&id, &orgID, &orderID, &amountMinor, &currency, &methodStr,
		&reference, &paidAt, &notes, &reverses, &reversedBy, &createdBy, &createdAt,
		&joinedOrderID, &orderNumber, &customerName); err != nil {
		return LedgerRow{}, fmt.Errorf("payment_repo: scan ledger: %w", err)
	}
	amt, err := money.New(amountMinor, currency)
	if err != nil {
		return LedgerRow{}, fmt.Errorf("payment_repo: bad amount: %w", err)
	}
	return LedgerRow{
		Payment: &payment.Payment{
			ID:             id,
			OrganizationID: orgID,
			OrderID:        orderID,
			Amount:         amt,
			Method:         payment.Method(methodStr),
			Reference:      reference,
			PaidAt:         paidAt,
			Notes:          notes,
			Reverses:       reverses,
			ReversedBy:     reversedBy,
			CreatedBy:      createdBy,
			CreatedAt:      createdAt,
		},
		OrderID:      joinedOrderID,
		OrderNumber:  orderNumber,
		CustomerName: customerName,
	}, nil
}

// -----------------------------------------------------------------------------
// Scanner
// -----------------------------------------------------------------------------

func scanPayment(row scannable) (*payment.Payment, error) {
	var (
		id          uuid.UUID
		orgID       uuid.UUID
		orderID     uuid.UUID
		amountMinor int64
		currency    string
		methodStr   string
		reference   string
		paidAt      time.Time
		notes       string
		reverses    *uuid.UUID
		reversedBy  *uuid.UUID
		createdBy   uuid.UUID
		createdAt   time.Time
	)
	if err := row.Scan(&id, &orgID, &orderID, &amountMinor, &currency, &methodStr,
		&reference, &paidAt, &notes, &reverses, &reversedBy, &createdBy, &createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, payment.ErrNotFound
		}
		return nil, fmt.Errorf("payment_repo: scan: %w", err)
	}
	amt, err := money.New(amountMinor, currency)
	if err != nil {
		return nil, fmt.Errorf("payment_repo: bad amount: %w", err)
	}
	return &payment.Payment{
		ID:             id,
		OrganizationID: orgID,
		OrderID:        orderID,
		Amount:         amt,
		Method:         payment.Method(methodStr),
		Reference:      reference,
		PaidAt:         paidAt,
		Notes:          notes,
		Reverses:       reverses,
		ReversedBy:     reversedBy,
		CreatedBy:      createdBy,
		CreatedAt:      createdAt,
	}, nil
}
