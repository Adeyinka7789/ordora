// Package observe owns error monitoring (Sentry). It is intentionally
// tiny: init once at startup, flush on shutdown, report panics from the
// Recover middleware. An empty DSN disables everything and the app runs
// exactly as before (local dev default).
package observe

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"
)

var enabled atomic.Bool

// Init configures Sentry. A blank dsn is a no-op (disabled).
func Init(dsn, environment string) error {
	if dsn == "" {
		return nil
	}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      environment,
		AttachStacktrace: true,
	}); err != nil {
		return err
	}
	enabled.Store(true)
	return nil
}

// Flush delivers buffered events. Call on shutdown.
func Flush() {
	if enabled.Load() {
		sentry.Flush(2 * time.Second)
	}
}

// ReportPanic sends a recovered panic to Sentry with request context.
// No-op when Sentry is disabled. Never raises.
func ReportPanic(r *http.Request, reqID string, rec any) {
	if !enabled.Load() {
		return
	}
	defer func() { _ = recover() }()
	hub := sentry.CurrentHub().Clone()
	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetRequest(r)
		if reqID != "" {
			scope.SetTag("req_id", reqID)
		}
		hub.CaptureException(asError(rec))
	})
}

func asError(rec any) error {
	if err, ok := rec.(error); ok {
		return err
	}
	return fmt.Errorf("panic: %v", rec)
}

// SafeGo runs fn in a goroutine, reporting a panic to Sentry (and logs)
// instead of silently killing the process's background work. Use for the
// outbox relay and notification worker.
func SafeGo(name string, fn func()) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("background: panic",
					"worker", name,
					"panic", rec,
					"stack", string(debug.Stack()),
				)
				if !enabled.Load() {
					return
				}
				hub := sentry.CurrentHub().Clone()
				hub.WithScope(func(scope *sentry.Scope) {
					scope.SetTag("worker", name)
					hub.CaptureException(asError(rec))
				})
			}
		}()
		fn()
	}()
}
