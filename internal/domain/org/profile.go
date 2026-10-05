package org

import (
	"strings"
	"time"
)

// Other is the select value that unlocks the free-text input. Stored values
// never equal Other — the handler resolves it to the typed text first.
const Other = "Other"

// MaxCustomProfileLength caps free-text "Other" entries.
const MaxCustomProfileLength = 120

// BusinessCategories is the trade taxonomy offered at registration.
func BusinessCategories() []string {
	return []string{
		"Tailoring & Fashion",
		"Bakery & Confectionery",
		"Printing & Branding",
		"Furniture & Carpentry",
		"Repair & Maintenance",
		"Beauty & Salon",
		"Food & Catering",
		"Photography & Media",
		Other,
	}
}

// BusinessTypes is the operation-shape taxonomy offered at registration.
func BusinessTypes() []string {
	return []string{
		"Retail",
		"Wholesale",
		"Services",
		"Manufacturing",
		Other,
	}
}

// TeamSizes offered at registration.
func TeamSizes() []string {
	return []string{
		"Just me",
		"2–5",
		"6–20",
		"21–50",
		"50+",
	}
}

// ReferralSources offered at registration.
func ReferralSources() []string {
	return []string{
		"Google search",
		"Social media",
		"Friend or colleague",
		"WhatsApp",
		"Advertisement",
		Other,
	}
}

// inList reports whether v is one of the allowed options.
func inList(options []string, v string) bool {
	for _, o := range options {
		if o == v {
			return true
		}
	}
	return false
}

// ResolveOther maps a select value plus its free-text companion to the stored
// value: anything but Other stores as-is; Other stores the trimmed custom
// text (which must be non-empty).
func ResolveOther(value, custom string) string {
	if value != Other {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(custom)
}

// validProfileValue accepts a taxonomy option or custom free text (the
// resolved "Other" value). The raw Other sentinel itself is never valid.
func validProfileValue(options []string, v string) bool {
	if v == "" || v == Other {
		return false
	}
	if inList(options, v) {
		return true
	}
	return len(v) <= MaxCustomProfileLength
}

// SetProfile records the registration-time business profile for analytics.
// Category and phone are required; type, team size, referral and address are
// optional. Values must come from the taxonomies (ResolveOther first).
func (o *Organization) SetProfile(businessType, category, teamSize, referral, phone, address string, now time.Time) error {
	category = strings.TrimSpace(category)
	if category == "" {
		return ErrProfileCategoryRequired
	}
	if !validProfileValue(BusinessCategories(), category) {
		return ErrProfileCategoryInvalid
	}

	businessType = strings.TrimSpace(businessType)
	if businessType != "" && !validProfileValue(BusinessTypes(), businessType) {
		return ErrProfileTypeInvalid
	}

	teamSize = strings.TrimSpace(teamSize)
	if teamSize != "" && !inList(TeamSizes(), teamSize) {
		return ErrProfileTeamSizeInvalid
	}

	referral = strings.TrimSpace(referral)
	if referral != "" && !validProfileValue(ReferralSources(), referral) {
		return ErrProfileReferralInvalid
	}

	phone = strings.TrimSpace(phone)
	if phone == "" || len(phone) > 40 {
		return ErrProfilePhoneInvalid
	}

	address = strings.TrimSpace(address)
	if len(address) > 500 {
		address = address[:500]
	}

	o.BusinessType = businessType
	o.BusinessCategory = category
	o.TeamSize = teamSize
	o.ReferralSource = referral
	o.Phone = phone
	o.Address = address
	o.UpdatedAt = now
	return nil
}
