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
	"github.com/Adeyinka7789/ordora/internal/infra/email"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

// NotificationWorker polls notification_jobs and sends emails.
type NotificationWorker struct {
	db       *postgres.DB
	renderer *email.Renderer
	mailer   email.Mailer
	interval time.Duration
	batch    int
	maxTries int
}

// NotificationWorkerConfig configures the worker.
type NotificationWorkerConfig struct {
	Interval time.Duration
	Batch    int
	MaxTries int
}

func NewNotificationWorker(db *postgres.DB, renderer *email.Renderer, mailer email.Mailer, cfg NotificationWorkerConfig) *NotificationWorker {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.Batch <= 0 {
		cfg.Batch = 20
	}
	if cfg.MaxTries <= 0 {
		cfg.MaxTries = 8
	}
	return &NotificationWorker{
		db:       db,
		renderer: renderer,
		mailer:   mailer,
		interval: cfg.Interval,
		batch:    cfg.Batch,
		maxTries: cfg.MaxTries,
	}
}

// Run blocks, polling forever, until ctx is cancelled.
func (w *NotificationWorker) Run(ctx context.Context) {
	slog.Info("notification worker: started", "interval", w.interval.String())

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("notification worker: stopped")
			return
		case <-ticker.C:
			if err := w.tick(ctx); err != nil {
				slog.Error("notification worker: tick failed", "err", err)
			}
		}
	}
}

func (w *NotificationWorker) tick(ctx context.Context) error {
	jobs, err := w.claim(ctx)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		w.process(ctx, j)
	}
	return nil
}

type claimJob struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	Kind           string
	Channel        string
	Recipient      string
	Payload        []byte
	Attempts       int
}

func (w *NotificationWorker) claim(ctx context.Context) ([]claimJob, error) {
	var jobs []claimJob
	err := w.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			WITH c AS (
				SELECT id FROM notification_jobs
				WHERE status = 'PENDING'
				  AND next_attempt_at <= now()
				ORDER BY next_attempt_at ASC
				LIMIT $1
				FOR UPDATE SKIP LOCKED
			)
			UPDATE notification_jobs j
			SET next_attempt_at = now() + interval '5 minutes'
			FROM c
			WHERE j.id = c.id
			RETURNING j.id, j.organization_id, j.kind, j.channel, j.recipient, j.payload, j.attempts
		`
		rows, err := tx.Query(ctx, q, w.batch)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var j claimJob
			if err := rows.Scan(&j.ID, &j.OrganizationID, &j.Kind, &j.Channel, &j.Recipient, &j.Payload, &j.Attempts); err != nil {
				return err
			}
			jobs = append(jobs, j)
		}
		return rows.Err()
	})
	return jobs, err
}

func (w *NotificationWorker) process(ctx context.Context, j claimJob) {
	subject := subjectFor(j.Kind)
	if subject == "" {
		w.deadLetter(ctx, j.ID, fmt.Sprintf("unknown kind %q", j.Kind))
		return
	}
	templateName := templateFor(j.Kind)
	if templateName == "" {
		w.deadLetter(ctx, j.ID, fmt.Sprintf("no template for kind %q", j.Kind))
		return
	}

	data, err := decodePayload(j.Payload)
	if err != nil {
		w.deadLetter(ctx, j.ID, "invalid payload: "+err.Error())
		return
	}

	msg, err := w.renderer.Render(templateName, subject, j.Recipient, data)
	if err != nil {
		w.deadLetter(ctx, j.ID, "render: "+err.Error())
		return
	}

	if err := w.mailer.Send(ctx, msg); err != nil {
		w.retryOrDead(ctx, j.ID, j.Attempts, err)
		return
	}

	_ = w.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			UPDATE notification_jobs
			SET status = 'SENT', sent_at = now(), attempts = attempts + 1, last_error = NULL
			WHERE id = $1
		`
		_, err := tx.Exec(ctx, q, j.ID)
		return err
	})
	slog.Info("notification worker: sent", "kind", j.Kind, "to", j.Recipient)
}

func (w *NotificationWorker) retryOrDead(ctx context.Context, id uuid.UUID, attempts int, cause error) {
	next := attempts + 1
	if next >= w.maxTries {
		w.deadLetter(ctx, id, cause.Error())
		return
	}
	backoff := backoffFor(next)
	_ = w.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			UPDATE notification_jobs
			SET attempts = $2,
			    last_error = $3,
			    next_attempt_at = now() + $4::interval
			WHERE id = $1
		`
		_, err := tx.Exec(ctx, q, id, next, cause.Error(), fmt.Sprintf("%d seconds", int(backoff.Seconds())))
		return err
	})
	slog.Warn("notification worker: retry scheduled",
		"id", id, "attempt", next, "backoff", backoff.String(), "err", cause.Error())
}

func (w *NotificationWorker) deadLetter(ctx context.Context, id uuid.UUID, reason string) {
	_ = w.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			UPDATE notification_jobs
			SET status = 'DEAD', last_error = $2, attempts = attempts + 1
			WHERE id = $1
		`
		_, err := tx.Exec(ctx, q, id, reason)
		return err
	})
	slog.Error("notification worker: dead-lettered", "id", id, "reason", reason)
}

func backoffFor(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 1 * time.Minute
	case 2:
		return 5 * time.Minute
	case 3:
		return 30 * time.Minute
	case 4:
		return 2 * time.Hour
	case 5:
		return 12 * time.Hour
	default:
		return 24 * time.Hour
	}
}

func subjectFor(kind string) string {
	switch notify.Kind(kind) {
	case notify.KindWelcomeVerify:
		return "Verify your Ordora email"
	case notify.KindPasswordReset:
		return "Reset your Ordora password"
	case notify.KindOrderCreatedCust:
		return "Your order is confirmed"
	case notify.KindOrderCreatedOwner:
		return "New order created"
	case notify.KindPaymentReceived:
		return "Payment received"
	case notify.KindStatusChanged:
		return "Order status updated"
	case notify.KindOrderReady:
		return "Your order is ready"
	case notify.KindIntakeReceived:
		return "New order request"
	}
	return ""
}

func templateFor(kind string) string {
	switch notify.Kind(kind) {
	case notify.KindWelcomeVerify:
		return "welcome_verify"
	case notify.KindPasswordReset:
		return "password_reset"
	case notify.KindOrderCreatedCust:
		return "order_created_customer"
	case notify.KindOrderCreatedOwner:
		return "order_created_owner"
	case notify.KindPaymentReceived:
		return "payment_received"
	case notify.KindStatusChanged:
		return "status_changed"
	case notify.KindOrderReady:
		return "order_ready"
	case notify.KindIntakeReceived:
		return "intake_received"
	}
	return ""
}

func decodePayload(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for key, val := range m {
		if strings.HasSuffix(key, "Minor") {
			if f, ok := val.(float64); ok {
				m[strings.TrimSuffix(key, "Minor")+"MinorFormatted"] = formatMinor(int64(f))
			}
		}
	}
	if to, ok := m["To"].(string); ok {
		m["ToLabel"] = statusLabel(to)
	}
	return m, nil
}

func statusLabel(s string) string {
	switch s {
	case "NEW":
		return "New"
	case "CONFIRMED":
		return "Confirmed"
	case "IN_PROGRESS":
		return "In progress"
	case "READY":
		return "Ready"
	case "OUT_FOR_DELIVERY":
		return "Out for delivery"
	case "DELIVERED":
		return "Delivered"
	case "COMPLETED":
		return "Completed"
	case "CANCELLED":
		return "Cancelled"
	}
	return s
}

func formatMinor(minor int64) string {
	neg := ""
	if minor < 0 {
		neg = "-"
		minor = -minor
	}
	return fmt.Sprintf("%s%d.%02d", neg, minor/100, minor%100)
}
