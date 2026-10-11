package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/notify"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

// OutboxRelay reads the outbox table and creates notification_jobs.
type OutboxRelay struct {
	db       *postgres.DB
	interval time.Duration
	batch    int
}

type OutboxRelayConfig struct {
	Interval time.Duration
	Batch    int
}

func NewOutboxRelay(db *postgres.DB, cfg OutboxRelayConfig) *OutboxRelay {
	if cfg.Interval <= 0 {
		cfg.Interval = 2 * time.Second
	}
	if cfg.Batch <= 0 {
		cfg.Batch = 100
	}
	return &OutboxRelay{db: db, interval: cfg.Interval, batch: cfg.Batch}
}

// Run blocks until ctx is cancelled.
func (r *OutboxRelay) Run(ctx context.Context) {
	slog.Info("outbox relay: started", "interval", r.interval.String())
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("outbox relay: stopped")
			return
		case <-ticker.C:
			if err := r.tick(ctx); err != nil {
				slog.Error("outbox relay: tick failed", "err", err)
			}
		}
	}
}

type outboxRow struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	EventName      string
	Payload        []byte
}

func (r *OutboxRelay) tick(ctx context.Context) error {
	var rows []outboxRow
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		// Cross-tenant read through a SECURITY DEFINER function: the
		// worker role has no tenant scope, and RLS would hide every row
		// (see 0061_worker_rls_functions.sql).
		const q = `
			SELECT id, org_id, event, payload FROM claim_outbox_batch($1)
		`
		rs, err := tx.Query(ctx, q, r.batch)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var o outboxRow
			if err := rs.Scan(&o.ID, &o.OrganizationID, &o.EventName, &o.Payload); err != nil {
				return err
			}
			rows = append(rows, o)
		}
		if err := rs.Err(); err != nil {
			return err
		}

		for _, o := range rows {
			if err := r.dispatch(ctx, tx, o); err != nil {
				slog.Warn("outbox relay: dispatch failed",
					"event", o.EventName, "id", o.ID, "err", err)
				// Continue: don't fail the whole batch for one event.
				// We'll record last_error but still mark dispatched so we
				// don't spin on a bad payload forever.
				_, _ = tx.Exec(ctx, `SELECT ack_outbox($1, $2)`, o.ID, err.Error())
			}
			if _, err := tx.Exec(ctx,
				`SELECT ack_outbox($1, '')`, o.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// dispatch turns one outbox event into zero or more notification_jobs.
func (r *OutboxRelay) dispatch(ctx context.Context, tx pgx.Tx, o outboxRow) error {
	switch o.EventName {
	case notify.EventUserRegistered:
		return r.dispatchUserRegistered(ctx, tx, o)
	case notify.EventOrderCreated:
		return r.dispatchOrderCreated(ctx, tx, o)
	case notify.EventPaymentRecorded:
		return r.dispatchPaymentRecorded(ctx, tx, o)
	case notify.EventOrderStatusChanged:
		return r.dispatchStatusChanged(ctx, tx, o)
	case notify.EventOrderReady:
		return r.dispatchOrderReady(ctx, tx, o)
	case notify.EventPublicIntakeReceived:
		return r.dispatchIntakeReceived(ctx, tx, o)
	case notify.EventBookingSubmitted:
		return r.dispatchBookingSubmitted(ctx, tx, o)
	case notify.EventBookingRescheduled:
		return r.dispatchBookingRescheduled(ctx, tx, o)
	default:
		slog.Warn("outbox relay: unknown event", "event", o.EventName)
		return nil
	}
}

func (r *OutboxRelay) dispatchUserRegistered(ctx context.Context, tx pgx.Tx, o outboxRow) error {
	var p notify.UserRegistered
	if err := json.Unmarshal(o.Payload, &p); err != nil {
		return err
	}
	return r.enqueue(ctx, tx, o.OrganizationID, notify.KindWelcomeVerify, p.Email, p, idemKey(o, p.Email))
}

func (r *OutboxRelay) dispatchOrderCreated(ctx context.Context, tx pgx.Tx, o outboxRow) error {
	var p notify.OrderCreated
	if err := json.Unmarshal(o.Payload, &p); err != nil {
		return err
	}
	// Customer notification (if email present).
	if p.CustomerEmail != "" {
		// Give the template access to minor-unit formatted fields.
		data := orderCreatedData(p)
		if err := r.enqueue(ctx, tx, o.OrganizationID, notify.KindOrderCreatedCust, p.CustomerEmail, data, idemKey(o, p.CustomerEmail)); err != nil {
			return err
		}
	}
	// Owner notification.
	if p.OrgEmail != "" {
		data := orderCreatedData(p)
		if err := r.enqueue(ctx, tx, o.OrganizationID, notify.KindOrderCreatedOwner, p.OrgEmail, data, idemKey(o, p.OrgEmail)); err != nil {
			return err
		}
	}
	return nil
}

func (r *OutboxRelay) dispatchPaymentRecorded(ctx context.Context, tx pgx.Tx, o outboxRow) error {
	var p notify.PaymentRecorded
	if err := json.Unmarshal(o.Payload, &p); err != nil {
		return err
	}
	if p.CustomerEmail == "" {
		return nil
	}
	return r.enqueue(ctx, tx, o.OrganizationID, notify.KindPaymentReceived, p.CustomerEmail, p, idemKey(o, p.CustomerEmail))
}

func (r *OutboxRelay) dispatchStatusChanged(ctx context.Context, tx pgx.Tx, o outboxRow) error {
	var p notify.OrderStatusChanged
	if err := json.Unmarshal(o.Payload, &p); err != nil {
		return err
	}
	if p.CustomerEmail == "" {
		return nil
	}
	// READY gets its own dedicated email; skip the generic status one.
	if p.To == "READY" {
		return r.enqueue(ctx, tx, o.OrganizationID, notify.KindOrderReady, p.CustomerEmail, p, idemKey(o, p.CustomerEmail))
	}
	return r.enqueue(ctx, tx, o.OrganizationID, notify.KindStatusChanged, p.CustomerEmail, p, idemKey(o, p.CustomerEmail))
}

func (r *OutboxRelay) dispatchOrderReady(ctx context.Context, tx pgx.Tx, o outboxRow) error {
	var p notify.OrderReady
	if err := json.Unmarshal(o.Payload, &p); err != nil {
		return err
	}
	if p.CustomerEmail == "" {
		return nil
	}
	return r.enqueue(ctx, tx, o.OrganizationID, notify.KindOrderReady, p.CustomerEmail, p, idemKey(o, p.CustomerEmail))
}

func (r *OutboxRelay) dispatchIntakeReceived(ctx context.Context, tx pgx.Tx, o outboxRow) error {
	var p notify.PublicIntakeReceived
	if err := json.Unmarshal(o.Payload, &p); err != nil {
		return err
	}
	if p.OrgEmail == "" {
		return nil
	}
	return r.enqueue(ctx, tx, o.OrganizationID, notify.KindIntakeReceived, p.OrgEmail, p, idemKey(o, p.OrgEmail))
}

// dispatchBookingSubmitted mails the owner about a new visit request,
// reusing the intake-received template (same shape on purpose).
// Confirm/cancel/complete/no-show need no booking-level mail: the linked
// order's own status transitions already mail the customer, and a second
// mail per action would be a duplicate.
func (r *OutboxRelay) dispatchBookingSubmitted(ctx context.Context, tx pgx.Tx, o outboxRow) error {
	var p notify.BookingSubmitted
	if err := json.Unmarshal(o.Payload, &p); err != nil {
		return err
	}
	if p.OrgEmail == "" {
		return nil
	}
	owner := notify.PublicIntakeReceived{
		OrderID: p.OrderID, OrderNumber: p.OrderNumber,
		CustomerName: p.CustomerName, CustomerEmail: p.CustomerEmail,
		CustomerPhone: p.CustomerPhone, Description: p.Description,
		OrgName: p.OrgName, OrgEmail: p.OrgEmail, AdminURL: p.AdminURL,
	}
	return r.enqueue(ctx, tx, o.OrganizationID, notify.KindIntakeReceived, p.OrgEmail, owner, idemKey(o, p.OrgEmail))
}

// dispatchBookingRescheduled mails the customer their new slot. Reschedules
// change no order status, so no other flow covers this mail.
func (r *OutboxRelay) dispatchBookingRescheduled(ctx context.Context, tx pgx.Tx, o outboxRow) error {
	var p notify.BookingRescheduled
	if err := json.Unmarshal(o.Payload, &p); err != nil {
		return err
	}
	if p.CustomerEmail == "" {
		return nil
	}
	return r.enqueue(ctx, tx, o.OrganizationID, notify.KindBookingRescheduled, p.CustomerEmail, p, idemKey(o, p.CustomerEmail))
}

// enqueue inserts one notification_jobs row. The idempotency_key prevents
// duplicates if the same event gets dispatched twice.
func (r *OutboxRelay) enqueue(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, kind notify.Kind, recipient string, payload any, idem string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("relay: marshal: %w", err)
	}
	const q = `SELECT enqueue_notification($1, $2, 'EMAIL', $3, $4::jsonb, $5)`
	_, err = tx.Exec(ctx, q, orgID, string(kind), recipient, string(body), idem)
	if err != nil {
		return fmt.Errorf("relay: enqueue: %w", err)
	}
	return nil
}

// idemKey builds a deterministic key per (event, aggregate, recipient).
func idemKey(o outboxRow, recipient string) string {
	return strings.Join([]string{o.EventName, o.ID.String(), recipient}, ":")
}

// orderCreatedData adds a computed title for the template.
func orderCreatedData(p notify.OrderCreated) map[string]any {
	m := map[string]any{
		"OrderNumber":  p.OrderNumber,
		"Title":        p.Title,
		"TotalMinor":   p.TotalMinor,
		"Currency":     p.Currency,
		"CustomerName": p.CustomerName,
		"OrgName":      p.OrgName,
		"PortalURL":    p.PortalURL,
		"ExpectedDate": p.ExpectedDate,
	}
	return m
}
