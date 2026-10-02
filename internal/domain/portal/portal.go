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

	CustomerName  string
	CustomerEmail string
	CustomerPhone string

	OrgName     string
	OrgSlug     string
	OrgEmail    string
	OrgPhone    string
	OrgAddress  string
	OrgCurrency string
	OrgTimezone string
}

// Item is one line item in a public order view.
type Item struct {
	Description    string
	Quantity       float64
	UnitPriceMinor int64
	SubtotalMinor  int64
	Currency       string
	Position       int
}
