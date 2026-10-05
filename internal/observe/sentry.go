// Package observe owns error monitoring (Sentry). It is intentionally
// small: init once at startup, flush on shutdown, report panics from the
// Recover middleware. An empty DSN disables everything and the app runs
// exactly as before (local dev default).
//
// Optional Sentry products are env-gated through Options:
//   - TracesSampleRate enables per-request transactions via Traced.
//     0 (default) means tracing code runs but sends nothing.
//   - EnableLogs forwards slog Warn/Error records to Sentry Logs via
//     SentryHandler. Our sentry-go version predates the EnableLogs
//     client option, so this package implements the equivalent with
//     the core Logger API (no extra dependency).
package observe

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"
)

var enabled atomic.Bool
var logsEnabled atomic.Bool

// Options configures Sentry. A blank DSN is a no-op (disabled).
type Options struct {
	DSN              string
	Environment      string
	TracesSampleRate float64 // 0 disables sending (default); 1.0 captures everything
	EnableLogs       bool    // forward slog Warn/Error records to Sentry Logs
}

// Init configures Sentry. A blank dsn is a no-op (disabled).
func Init(o Options) error {
	if o.DSN == "" {
		return nil
	}
	rate := o.TracesSampleRate
	if rate < 0 || rate > 1 {
		rate = 0
	}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              o.DSN,
		Environment:      o.Environment,
		AttachStacktrace: true,
		TracesSampleRate: rate,
	}); err != nil {
		return err
	}
	enabled.Store(true)
	logsEnabled.Store(o.EnableLogs)
	return nil
}

// Flush delivers buffered events. Call on shutdown.
func Flush() {
	if enabled.Load() {
		sentry.Flush(2 * time.Second)
	}
}

// Verify sends a test message ("It works!" ping) and flushes. It reports
// whether Sentry is enabled; the message itself lands in Issues only when
// the DSN is valid. No-op when Sentry is disabled. Never raises.
func Verify() bool {
	if !enabled.Load() {
		return false
	}
	defer func() { _ = recover() }()
	id := sentry.CaptureMessage("Sentry verify ping from Ordora")
	Flush()
	return id != nil
}

// Traced wraps a handler with one Sentry transaction per request
// (op "http.server"). With TracesSampleRate 0 the span runs unsampled:
// near-zero overhead, nothing sent. Place outermost in the middleware
// chain so panics still finish the transaction via the Recover layer.
func Traced(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !enabled.Load() {
			next.ServeHTTP(w, r)
			return
		}
		txn := sentry.StartTransaction(
			r.Context(),
			r.Method+" "+r.URL.Path,
			sentry.WithOpName("http.server"),
		)
		defer txn.Finish()
		next.ServeHTTP(w, r.WithContext(txn.Context()))
	})
}

// SentryHandler wraps the app's slog handler and forwards Warn+ records
// to Sentry Logs (when EnableLogs is on). Stdout logging is untouched:
// the wrapped handler always runs first, and forwarding never raises.
// Wrap once at startup; the enabled checks run per record, so init order
// (logging before Sentry) doesn't matter.
func SentryHandler(next slog.Handler) slog.Handler {
	return &sentryBridge{next: next}
}

type sentryBridge struct {
	next slog.Handler
}

func (b *sentryBridge) Enabled(ctx context.Context, l slog.Level) bool {
	return b.next.Enabled(ctx, l)
}

func (b *sentryBridge) Handle(ctx context.Context, r slog.Record) error {
	err := b.next.Handle(ctx, r)
	if !enabled.Load() || !logsEnabled.Load() {
		return err
	}
	if r.Level >= slog.LevelError {
		forwardLog(ctx, r, true)
	} else if r.Level >= slog.LevelWarn {
		forwardLog(ctx, r, false)
	}
	return err
}

func (b *sentryBridge) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &sentryBridge{next: b.next.WithAttrs(attrs)}
}

func (b *sentryBridge) WithGroup(name string) slog.Handler {
	return &sentryBridge{next: b.next.WithGroup(name)}
}

func forwardLog(ctx context.Context, r slog.Record, isError bool) {
	defer func() { _ = recover() }()
	if isError {
		sentry.NewLogger(ctx).Error().Emit(r.Message)
	} else {
		sentry.NewLogger(ctx).Warn().Emit(r.Message)
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
