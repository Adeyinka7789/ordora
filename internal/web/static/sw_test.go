package static

import (
	"os"
	"strings"
	"testing"
)

// TestServiceWorker_NeverCachesHTML guards the shared-device leak fix:
// authenticated HTML must never be written to (or read from) the cache.
// Only static assets and the /offline fallback may be cached.
func TestServiceWorker_NeverCachesHTML(t *testing.T) {
	raw, err := os.ReadFile("sw.js")
	if err != nil {
		t.Fatalf("read sw.js: %v", err)
	}
	js := string(raw)

	// The retired v1 page cache must stay gone as a *write target*.
	// ("ordora-pages" still appears in the activate handler that purges
	// v1 caches — that deletion path must stay.)
	if strings.Contains(js, "PAGE_CACHE") {
		t.Error("sw.js must not contain PAGE_CACHE (authenticated HTML cache)")
	}
	if strings.Contains(js, "ordora-pages-${VERSION}") || strings.Contains(js, "`ordora-pages-`") {
		t.Error("sw.js must not define a page cache (authenticated HTML cache)")
	}

	// Navigations must go to network with an offline fallback, never the cache.
	if !strings.Contains(js, "caches.match('/offline')") {
		t.Error("sw.js navigations must fall back to the pre-cached /offline page")
	}
	if !strings.Contains(js, "ordora-static-") {
		t.Error("sw.js must keep the versioned static-asset cache")
	}
}
