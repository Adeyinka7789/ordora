package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/audit"
	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/payment"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// PaymentWriter is the write side of the payment repo.
type PaymentWriter interface {
	CreateTx(ctx context.Context, tx pgx.Tx, p *payment.Payment) error
}

// PaymentReader is the read side.
type PaymentReader interface {
	ListForOrder(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) ([]*payment.Payment, error)
	GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*payment.Payment, error)
}

// PaymentService orchestrates recording payments against orders.
type PaymentService struct {
	db       TxRunner
	payments PaymentWriter
	paymentR PaymentReader
	orders   OrderReader
	audit    AuditWriter
	ids      IDGen
	now      func() time.Time
}

type PaymentServiceDeps struct {
	DB          TxRunner
	Payments    PaymentWriter
	PaymentRead PaymentReader
	Orders      OrderReader
	Audit       AuditWriter
	IDs         IDGen
	Now         func() time.Time
}

func NewPaymentService(d PaymentServiceDeps) *PaymentService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &PaymentService{
		db:       d.DB,
		payments: d.Payments,
		paymentR: d.PaymentRead,
		orders:   d.Orders,
		audit:    d.Audit,
		ids:      d.IDs,
		now:      d.Now,
	}
}

// -----------------------------------------------------------------------------
// Input / errors
// -----------------------------------------------------------------------------

// RecordPaymentInput is the request shape for recording a payment.
type RecordPaymentInput struct {
	OrderID   uuid.UUID
	Amount    int64 // minor units, must be > 0
	Method    payment.Method
	Reference string
	PaidAt    time.Time
	Notes     string
}

var (
	ErrPaymentAmountRequired = errors.New("payment service: amount is required")
	ErrPaymentMethodInvalid  = errors.New("payment service: invalid payment method")
	ErrOrderNotFound         = errors.New("payment service: order not found")
	ErrOverpaymentNotAllowed = errors.New("payment service: amount exceeds outstanding balance")
	ErrOrderIsCancelled      = errors.New("payment service: order is cancelled")
	ErrPaidAtInFuture        = errors.New("payment service: payment date cannot be in the future")
)

// -----------------------------------------------------------------------------
// RecordPayment
// -----------------------------------------------------------------------------

// RecordPayment records a payment against an order.
//
// Rules:
//   - Amount must be > 0.
//   - Method must be valid.
//   - Paid date must not be in the future.
//   - Order must belong to the caller's tenant.
//   - Order must not be cancelled.
//   - Amount cannot exceed the order's outstanding balance.
//
// The whole operation runs in one transaction. The DB trigger
// trg_payments_refresh_order updates orders.amount_paid_minor automatically.
func (s *PaymentService) RecordPayment(ctx context.Context, scope tenant.TenantScope, in RecordPaymentInput) (*order.Order, error) {
	if in.Amount <= 0 {
		return nil, ErrPaymentAmountRequired
	}
	if !in.Method.IsValid() {
		return nil, ErrPaymentMethodInvalid
	}
	if in.PaidAt.After(s.now().Add(time.Minute)) {
		return nil, ErrPaidAtInFuture
	}

	var updated *order.Order
	err := s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		o, err := s.orders.GetByID(ctx, scope, in.OrderID)
		if err != nil {
			if errors.Is(err, order.ErrNotFound) {
				return ErrOrderNotFound
			}
			return err
		}

		if o.Status == order.StatusCancelled {
			return ErrOrderIsCancelled
		}

		balance := o.Balance()
		if in.Amount > balance.Amount() {
			return ErrOverpaymentNotAllowed
		}

		amount, err := money.New(in.Amount, o.Currency)
		if err != nil {
			return err
		}
		p, err := payment.New(
			s.ids.New(), scope.OrgID, o.ID,
			amount, in.Method,
			in.Reference, in.PaidAt, in.Notes,
			scope.UserID, s.now(),
		)
		if err != nil {
			return err
		}

		if err := s.payments.CreateTx(ctx, tx, p); err != nil {
			return err
		}

		reloaded, err := s.orders.GetByID(ctx, scope, o.ID)
		if err != nil {
			return err
		}
		updated = reloaded

		if s.audit != nil {
			if err := s.audit.RecordTx(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID,
				ActorUserID:    scope.UserID,
				Action:         "payment.recorded",
				EntityType:     "PAYMENT",
				EntityID:       p.ID,
				After: mustJSON(map[string]any{
					"order_id":     p.OrderID.String(),
					"amount_minor": p.Amount.Amount(),
					"method":       string(p.Method),
				}),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ListForOrder returns the payment history for an order.
func (s *PaymentService) ListForOrder(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) ([]*payment.Payment, error) {
	return s.paymentR.ListForOrder(ctx, scope, orderID)
}
