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
	MarkReversedTx(ctx context.Context, tx pgx.Tx, originalID, reversalID uuid.UUID) error
}

// PaymentReader is the read side.
type PaymentReader interface {
	ListForOrder(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) ([]*payment.Payment, error)
	GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*payment.Payment, error)
	GetByIDTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*payment.Payment, error)
}

// PaymentService orchestrates recording payments against orders.
type PaymentService struct {
	db        TxRunner
	payments  PaymentWriter
	paymentR  PaymentReader
	orders    OrderReader
	customers CustomerReader
	audit     AuditWriter
	outbox    OutboxWriter
	ids       IDGen
	now       func() time.Time
}

type PaymentServiceDeps struct {
	DB          TxRunner
	Payments    PaymentWriter
	PaymentRead PaymentReader
	Orders      OrderReader
	Customers   CustomerReader
	Audit       AuditWriter
	Outbox      OutboxWriter
	IDs         IDGen
	Now         func() time.Time
}

func NewPaymentService(d PaymentServiceDeps) *PaymentService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &PaymentService{
		db:        d.DB,
		payments:  d.Payments,
		paymentR:  d.PaymentRead,
		orders:    d.Orders,
		customers: d.Customers,
		audit:     d.Audit,
		outbox:    d.Outbox,
		ids:       d.IDs,
		now:       d.Now,
	}
}

// -----------------------------------------------------------------------------
// Input / errors
// -----------------------------------------------------------------------------

type RecordPaymentInput struct {
	OrderID   uuid.UUID
	Amount    int64
	Method    payment.Method
	Reference string
	PaidAt    time.Time
	Notes     string
}

var (
	ErrPaymentAmountRequired  = errors.New("payment service: amount is required")
	ErrPaymentMethodInvalid   = errors.New("payment service: invalid payment method")
	ErrOrderNotFound          = errors.New("payment service: order not found")
	ErrOverpaymentNotAllowed  = errors.New("payment service: amount exceeds outstanding balance")
	ErrOrderIsCancelled       = errors.New("payment service: order is cancelled")
	ErrPaidAtInFuture         = errors.New("payment service: payment date cannot be in the future")
	ErrCannotReverseReversal  = errors.New("payment service: cannot reverse a reversal")
	ErrPaymentAlreadyReversed = errors.New("payment service: payment is already reversed")
	ErrReasonRequired         = errors.New("payment service: reason is required")
)

// -----------------------------------------------------------------------------
// RecordPayment
// -----------------------------------------------------------------------------

func (s *PaymentService) RecordPayment(ctx context.Context, scope tenant.TenantScope, in RecordPaymentInput) (*order.Order, error) {
	if err := scope.RequireWrite(); err != nil {
		return nil, err
	}
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
		// Row lock: concurrent RecordPayment calls serialize here, so
		// the balance check below can't pass twice for the same funds.
		o, err := s.orders.GetByIDForUpdate(ctx, tx, scope, in.OrderID)
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

		if s.outbox != nil && s.customers != nil {
			cust, err := s.customers.GetByID(ctx, scope, o.CustomerID)
			if err == nil && cust.Email != "" {
				if err := s.outbox.EnqueueTx(ctx, tx, scope.OrgID, "payment.recorded", map[string]any{
					"PaymentID":     p.ID.String(),
					"OrderID":       o.ID.String(),
					"OrderNumber":   o.Number,
					"AmountMinor":   p.Amount.Amount(),
					"Currency":      p.Amount.Currency(),
					"PaidMinor":     reloaded.Paid.Amount(),
					"TotalMinor":    reloaded.Total.Amount(),
					"BalanceMinor":  reloaded.Balance().Amount(),
					"Method":        string(p.Method),
					"CustomerName":  cust.Name,
					"CustomerEmail": cust.Email,
					"PortalURL":     "",
				}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// -----------------------------------------------------------------------------
// ReversePayment
// -----------------------------------------------------------------------------

type ReverseInput struct {
	PaymentID  uuid.UUID
	Reason     string
	ReversedAt time.Time
}

func (s *PaymentService) ReversePayment(ctx context.Context, scope tenant.TenantScope, in ReverseInput) (*order.Order, error) {
	if err := scope.RequireWrite(); err != nil {
		return nil, err
	}
	if in.Reason == "" {
		return nil, ErrReasonRequired
	}
	reversedAt := in.ReversedAt
	if reversedAt.IsZero() {
		reversedAt = s.now()
	}

	var updated *order.Order
	err := s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		orig, err := s.paymentR.GetByIDTx(ctx, tx, in.PaymentID)
		if err != nil {
			return err
		}
		if orig.IsReversal() {
			return ErrCannotReverseReversal
		}
		if orig.IsReversed() {
			return ErrPaymentAlreadyReversed
		}

		rev, err := payment.New(
			s.ids.New(), scope.OrgID, orig.OrderID,
			orig.Amount, orig.Method,
			"REVERSAL", reversedAt, "Reversed: "+in.Reason,
			scope.UserID, s.now(),
		)
		if err != nil {
			return err
		}
		rev.Reverses = &orig.ID

		if err := s.payments.CreateTx(ctx, tx, rev); err != nil {
			return err
		}
		if err := s.payments.MarkReversedTx(ctx, tx, orig.ID, rev.ID); err != nil {
			return err
		}

		reloaded, err := s.orders.GetByID(ctx, scope, orig.OrderID)
		if err != nil {
			return err
		}
		updated = reloaded

		if s.audit != nil {
			if err := s.audit.RecordTx(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID,
				ActorUserID:    scope.UserID,
				Action:         "payment.reversed",
				EntityType:     "PAYMENT",
				EntityID:       orig.ID,
				After: mustJSON(map[string]any{
					"reversal_id":  rev.ID.String(),
					"amount_minor": orig.Amount.Amount(),
					"reason":       in.Reason,
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

// -----------------------------------------------------------------------------
// Read helpers
// -----------------------------------------------------------------------------

func (s *PaymentService) ListForOrder(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) ([]*payment.Payment, error) {
	return s.paymentR.ListForOrder(ctx, scope, orderID)
}

func (s *PaymentService) GetPayment(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*payment.Payment, error) {
	return s.paymentR.GetByID(ctx, scope, id)
}
