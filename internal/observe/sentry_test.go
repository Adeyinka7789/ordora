package observe

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Empty DSN must disable Sentry entirely: init is a no-op and reporting
// never panics, blocks, or sends anything.
func TestDisabledByDefault(t *testing.T) {
	if err := Init(Options{DSN: "", Environment: "test"}); err != nil {
		t.Fatalf("Init with empty DSN: %v", err)
	}
	req := httptest.NewRequest("GET", "/orders", nil)
	ReportPanic(req, "req-1", "boom") // must be a silent no-op
	Flush()                           // must not hang
	if Verify() {
		t.Error("Verify must be false when disabled")
	}
}

// Traced must pass requests through untouched when Sentry is disabled.
func TestTracedPassthroughWhenDisabled(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	})
	req := httptest.NewRequest("GET", "/orders", nil)
	rec := httptest.NewRecorder()
	Traced(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot || rec.Body.String() != "ok" {
		t.Fatalf("traced passthrough broken: %d %q", rec.Code, rec.Body.String())
	}
}

// The Sentry log bridge must never swallow stdout logging, raise, or send
// when disabled.
func TestSentryHandlerPassthrough(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := SentryHandler(base)
	ctx := context.Background()
	if !h.Enabled(ctx, slog.LevelError) {
		t.Error("Enabled must follow the wrapped handler")
	}
	if err := h.Handle(ctx, slog.NewRecord(time.Now(), slog.LevelError, "boom", 0)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Error("wrapped handler must still receive the record")
	}
	// WithAttrs/WithGroup must preserve the bridge (and stdout flow).
	h2 := h.WithAttrs([]slog.Attr{slog.String("k", "v")}).WithGroup("g")
	if err := h2.Handle(ctx, slog.NewRecord(time.Now(), slog.LevelWarn, "warn-1", 0)); err != nil {
		t.Fatalf("Handle with attrs/group: %v", err)
	}
	if !strings.Contains(buf.String(), "warn-1") {
		t.Error("attrs/group bridge must still log to stdout")
	}
}

// SafeGo must survive a panicking goroutine (the test binary crashing
// is the failure mode).
func TestSafeGoRecovers(t *testing.T) {
	done := make(chan struct{})
	SafeGo("test", func() {
		close(done)
		panic("boom")
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine did not run")
	}
	time.Sleep(100 * time.Millisecond) // let the deferred recover run
}
