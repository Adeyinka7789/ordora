// Package portal defines the read projection for the public customer order
// portal. It is a pure data struct — no persistence logic, no business rules.
package portal

import (
	"time"

	"github.com/google/uuid"
)

// Order is the raw projection returned by the public-token lookup function.
type Order struct {
	OrderID            uuid.UUID
	OrganizationID     uuid.UUID
	OrderNumber        string
	Title              string
	Description        string
	Status             string
	Currency           string
	SubtotalMinor      int64
	DiscountMinor      int64
	TaxMinor           int64
	TotalMinor         int64
	AmountPaidMinor    int64
	ExpectedCompletion *time.Time
	DeliveredAt        *time.Time
	CreatedAt          time.Time

	CustomerName    string
	CustomerEmail   string
	CustomerPhone   string
	CustomerAddress string

	OrgName     string
	OrgSlug     string
	OrgEmail    string
	OrgPhone    string
	OrgAddress  string
	OrgCurrency string
	OrgTimezone string
}

// Item is one line item in a public order view. Material/ImageRef are
// snapshots taken at order time — later catalog edits don't change them.
type Item struct {
	Description    string
	Quantity       float64
	UnitPriceMinor int64
	SubtotalMinor  int64
	Currency       string
	Position       int
	Material       string
	ImageRef       string // product cover attachment id at order time (may be "")
}

// Payment is one payment line for the public receipt. ProofNames carries
// the filenames of payment-proof attachments (staff-side uploads);
// ProofIDs carries the matching attachment ids (same order) so the portal
// can link token-bound downloads.
type Payment struct {
	Method      string
	Reference   string
	PaidAt      time.Time
	AmountMinor int64
	Currency    string
	Notes       string
	IsReversed  bool
	IsReversal  bool
	ProofCount  int
	ProofNames  string
	ProofIDs    string
}
