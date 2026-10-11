package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// enqueueRetry writes one outbox event outside a business transaction,
// retrying transient failures a few times before giving up with a loud
// log. Callers here (public intake, booking submit/reschedule) commit
// their business change first and notify after, so a lost event would
// otherwise vanish silently — the previous `_ = Enqueue` pattern.
//
// This is recoverable-via-logs, not transactional: after the retries the
// business record stands and the notification is gone, but the error is
// now visible to ops (with order context) instead of discarded. The
// worker's own retry/backoff still covers every send-side failure once
// the event is stored.
func enqueueRetry(ctx context.Context, outbox OutboxWriter, orgID uuid.UUID, event string, payload map[string]any, logCtx ...any) {
	if outbox == nil {
		return
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
			}
		}
		if err = outbox.Enqueue(ctx, orgID, event, payload); err == nil {
			return
		}
	}
	args := append([]any{"event", event, "err", err}, logCtx...)
	slog.Error("outbox: event lost after retries; business change stands without its email", args...)
}
