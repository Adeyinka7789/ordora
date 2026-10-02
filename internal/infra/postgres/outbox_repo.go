package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OutboxRepo writes events to the transactional outbox table.
//
// The flow: a business transaction (order created, payment recorded, etc.)
// writes an outbox row inside the SAME database transaction as the business
// change. The outbox relay picks up the row within a few seconds, creates
// notification_jobs, and the notification worker sends the email.
//
// This guarantees at-least-once delivery: if the business transaction
// commits, the outbox row is there and will eventually be dispatched.
type OutboxRepo struct {
	db *DB
}

func NewOutboxRepo(db *DB) *OutboxRepo { return &OutboxRepo{db: db} }

// EnqueueTx writes one event to the outbox inside an existing transaction.
func (r *OutboxRepo) EnqueueTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, eventName string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("outbox: marshal: %w", err)
	}
	const q = `
		INSERT INTO outbox (id, organization_id, event_name, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err = tx.Exec(ctx, q, uuid.New(), orgID, eventName, body, time.Now())
	if err != nil {
		return fmt.Errorf("outbox: insert: %w", Classify(err))
	}
	return nil
}

// Enqueue writes one event to the outbox in its own transaction. Used when
// the caller is not already inside a business transaction (e.g. public
// intake submissions that run through a SECURITY DEFINER function).
func (r *OutboxRepo) Enqueue(ctx context.Context, orgID uuid.UUID, eventName string, payload any) error {
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		return r.EnqueueTx(ctx, tx, orgID, eventName, payload)
	})
}
