// Package contact loads the site's public contact details from an editable
// JSON file (contact.json at the repo root). Owners change emails, phones,
// address, website and the WhatsApp number there — no code changes, no
// redeploy of templates. A missing file yields quiet defaults (everything
// hidden); an invalid file is an error so typos fail fast at startup.
package contact

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"unicode"
)

// Info is the public contact surface shown in the utility top bar and the
// floating WhatsApp button (landing page + app).
type Info struct {
	Emails          []string `json:"emails"`
	Phones          []string `json:"phones"`
	Address         string   `json:"address"`
	Website         string   `json:"website"`
	WebsiteURL      string   `json:"website_url"`
	WhatsAppNumber  string   `json:"whatsapp_number"`
	WhatsAppMessage string   `json:"whatsapp_message"`

	// Derived (not in the file).
	WhatsAppDigits string `json:"-"`
	WhatsAppURL    string `json:"-"`
}

// Defaults returns the zero-visible Info: every field empty, so templates
// render nothing rather than placeholders.
func Defaults() *Info { return &Info{} }

// Load reads path. If the file does not exist, quiet defaults are returned
// with a nil error (first-run friendly). Invalid JSON is an error.
func Load(path string) (*Info, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Defaults(), nil
		}
		return nil, fmt.Errorf("contact: read %s: %w", path, err)
	}
	c := Defaults()
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, fmt.Errorf("contact: parse %s: %w", path, err)
	}
	c.normalize()
	return c, nil
}

func (c *Info) normalize() {
	c.WhatsAppDigits = Digits(c.WhatsAppNumber)
	v := url.Values{}
	v.Set("text", strings.TrimSpace(c.WhatsAppMessage))
	if v.Get("text") == "" {
		v.Set("text", "Hello! I found you through your website.")
	}
	if c.WhatsAppDigits == "" {
		c.WhatsAppURL = ""
		return
	}
	c.WhatsAppURL = "https://wa.me/" + c.WhatsAppDigits + "?" + v.Encode()
}

// HasAny reports whether there is anything worth showing in the utility bar.
func (c *Info) HasAny() bool {
	if c == nil {
		return false
	}
	return len(c.Emails) > 0 || len(c.Phones) > 0 ||
		strings.TrimSpace(c.Address) != "" || strings.TrimSpace(c.Website) != ""
}

// Digits strips everything but 0-9, producing the wa.me-compatible number
// (international format, no leading +).
func Digits(phone string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, phone)
}
