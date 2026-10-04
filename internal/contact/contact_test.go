package contact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingGivesQuietDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if c.HasAny() {
		t.Error("defaults should show nothing")
	}
	if c.WhatsAppURL != "" {
		t.Errorf("no number => no WhatsApp URL, got %q", c.WhatsAppURL)
	}
}

func TestLoadParsesAndNormalizes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "contact.json")
	raw := `{
		"emails": ["hello@example.com", "sales@example.com"],
		"phones": ["+234 801 234 5678"],
		"address": "12 Allen Ave, Lagos",
		"website": "example.com",
		"website_url": "https://example.com",
		"whatsapp_number": "+234 801 234 5678",
		"whatsapp_message": "Hello! I need help."
	}`
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !c.HasAny() {
		t.Error("expected visible contact details")
	}
	if c.WhatsAppDigits != "2348012345678" {
		t.Errorf("digits = %q", c.WhatsAppDigits)
	}
	if !strings.HasPrefix(c.WhatsAppURL, "https://wa.me/2348012345678?") {
		t.Errorf("wa url = %q", c.WhatsAppURL)
	}
	if !strings.Contains(c.WhatsAppURL, "text=Hello") {
		t.Errorf("wa message not encoded: %q", c.WhatsAppURL)
	}
}

func TestLoadInvalidJSONErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "contact.json")
	if err := os.WriteFile(p, []byte(`{oops`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Error("invalid JSON should error so typos fail fast")
	}
}

func TestDigits(t *testing.T) {
	if got := Digits("+234 (801) 234-5678"); got != "2348012345678" {
		t.Errorf("digits = %q", got)
	}
}
