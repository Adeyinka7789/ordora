package org

import (
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Organization is a tenant. All tenant-owned data ultimately scopes to one
// of these.
type Organization struct {
	ID        uuid.UUID
	Name      string
	Slug      Slug
	LogoKey   string
	Email     string
	Phone     string
	Address   string
	Currency  string // ISO 4217, uppercase, 3 letters
	Timezone  string // IANA, e.g. "Africa/Lagos"
	CreatedAt time.Time
	UpdatedAt time.Time
}

// New constructs a new Organization with sensible defaults.
func New(id uuid.UUID, name string, slug Slug, now time.Time) (*Organization, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameRequired
	}
	if name == "" || len(name) > 120 {
		return nil, ErrNameInvalid
	}
	return &Organization{
		ID:        id,
		Name:      name,
		Slug:      slug,
		Currency:  "NGN",
		Timezone:  "Africa/Lagos",
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// -----------------------------------------------------------------------------
// Slug value object
// -----------------------------------------------------------------------------

// Slug is a URL-safe identifier for an organization.
// Rules: lowercase, alphanumeric and hyphens, 3–60 chars, no leading/trailing
// hyphen, no consecutive hyphens. Unique across organizations.
type Slug struct {
	value string
}

func NewSlug(s string) (Slug, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return Slug{}, ErrSlugRequired
	}
	if len(s) < 3 || len(s) > 60 {
		return Slug{}, ErrSlugLength
	}
	if strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") {
		return Slug{}, ErrSlugInvalid
	}
	if strings.Contains(s, "--") {
		return Slug{}, ErrSlugInvalid
	}
	for _, r := range s {
		if !(unicode.IsLower(r) || unicode.IsDigit(r) || r == '-') {
			return Slug{}, ErrSlugInvalid
		}
	}
	return Slug{value: s}, nil
}

// Slugify converts a free-form name to a candidate slug. It does not check
// uniqueness — the caller does that.
func Slugify(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastHyphen := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastHyphen = false
		case unicode.IsSpace(r) || r == '-' || r == '_':
			if !lastHyphen && b.Len() > 0 {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	return s
}

func (s Slug) String() string { return s.value }
func (s Slug) IsZero() bool   { return s.value == "" }
