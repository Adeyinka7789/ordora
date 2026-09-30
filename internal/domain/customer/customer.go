package customer

import (
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Customer belongs to exactly one organization. It is a lightweight entity:
// its purpose is to be the anchor for orders and payments.
type Customer struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	Name           string
	Email          string // lowercased, may be empty
	Phone          string // digits + separators, may be empty
	Address        string // free-form, may be empty
	Notes          string // free-form, may be empty
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// New constructs a new Customer. Email, Phone, Address, and Notes are optional
// but must be valid if provided. Name is required.
//
// We intentionally do not use the value-object pattern for email here (as we
// did for users). Customer email is contact information, not an identity. It
// may be missing, duplicated, or change. Only the user's email is an identity.
func New(id, orgID uuid.UUID, name, email, phone, address, notes string, now time.Time) (*Customer, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameRequired
	}
	if len(name) > 200 {
		return nil, ErrNameTooLong
	}

	email = normalizeEmail(email)
	if email != "" {
		if err := validateEmail(email); err != nil {
			return nil, err
		}
	}

	phone = normalizePhone(phone)
	if len(phone) > 40 {
		return nil, ErrPhoneTooLong
	}

	if len(address) > 500 {
		return nil, ErrAddressTooLong
	}
	if len(notes) > 5000 {
		return nil, ErrNotesTooLong
	}

	return &Customer{
		ID:             id,
		OrganizationID: orgID,
		Name:           name,
		Email:          email,
		Phone:          phone,
		Address:        address,
		Notes:          notes,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// Update replaces the mutable fields. Validation is identical to New.
func (c *Customer) Update(name, email, phone, address, notes string, now time.Time) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNameRequired
	}
	if len(name) > 200 {
		return ErrNameTooLong
	}

	email = normalizeEmail(email)
	if email != "" {
		if err := validateEmail(email); err != nil {
			return err
		}
	}

	phone = normalizePhone(phone)
	if len(phone) > 40 {
		return ErrPhoneTooLong
	}

	if len(address) > 500 {
		return ErrAddressTooLong
	}
	if len(notes) > 5000 {
		return ErrNotesTooLong
	}

	c.Name = name
	c.Email = email
	c.Phone = phone
	c.Address = address
	c.Notes = notes
	c.UpdatedAt = now
	return nil
}

// HasEmail reports whether the customer has a usable email address.
func (c *Customer) HasEmail() bool { return c.Email != "" }

// HasPhone reports whether the customer has a usable phone number.
func (c *Customer) HasPhone() bool { return c.Phone != "" }

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func normalizeEmail(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}

// normalizePhone trims whitespace and normalizes internal whitespace to single
// spaces. It does not attempt to strip formatting characters (dashes, parens),
// because African phone numbers vary widely and users often want them preserved
// for readability (e.g. "+234 803 123 4567").
func normalizePhone(s string) string {
	s = strings.TrimSpace(s)
	// Collapse internal whitespace.
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !space && b.Len() > 0 {
				b.WriteRune(' ')
				space = true
			}
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

func validateEmail(s string) error {
	if len(s) > 254 {
		return ErrEmailTooLong
	}
	at := strings.LastIndex(s, "@")
	if at <= 0 || at == len(s)-1 {
		return ErrEmailInvalid
	}
	domain := s[at+1:]
	if !strings.Contains(domain, ".") {
		return ErrEmailInvalid
	}
	if strings.Contains(domain, "..") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return ErrEmailInvalid
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return ErrEmailInvalid
		}
	}
	return nil
}
