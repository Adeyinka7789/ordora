package observe

import (
	"net/http/httptest"
	"testing"
	"time"
)

// Empty DSN must disable Sentry entirely: init is a no-op and reporting
// never panics, blocks, or sends anything.
func TestDisabledByDefault(t *testing.T) {
	if err := Init("", "test"); err != nil {
		t.Fatalf("Init with empty DSN: %v", err)
	}
	req := httptest.NewRequest("GET", "/orders", nil)
	ReportPanic(req, "req-1", "boom") // must be a silent no-op
	Flush()                           // must not hang
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
