package postgres

import (
	"context"
	"errors"
	"fmt"
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
