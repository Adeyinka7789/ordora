// Package group defines order groups: aso-ebi / bulk collections where
// one style is made for many members (e.g. a wedding party). A group is
// a named bucket of orders with an optional occasion date; per-member
// measurements, items, payments and balances all live on the member
// orders themselves.
package group

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Group is a named collection of orders for one occasion.
// Deep Aso-ebi: the group is the master order. PriceMinor/Currency is the
// fixed per-head price (0 = tailor quotes). Fabric is shown on the join
// form (e.g. "Wine Lace"). TemplateID is the default measurement template
// guests fill. JoinToken/ManageToken hashes back the public join link
// (/g/{raw}) and the secret bride link (/g/{raw}/manage?key={raw}).
// Paid/Collected are manual ticks only — no money moves on site.
type Group struct {
	ID              uuid.UUID
	OrganizationID  uuid.UUID
	Name            string
	OccasionDate    *time.Time
	Notes           string
	PriceMinor      int64
	Currency        string
	Fabric          string
	TemplateID      *uuid.UUID
	JoinEnabled     bool
	JoinSlug        string
	JoinTokenHash   []byte
	ManageTokenHash []byte
	CreatedBy       uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Errors.
var (
	ErrNameRequired = errors.New("group: name is required")
	ErrNameTooLong  = errors.New("group: name is too long (max 120)")
	ErrNotesTooLong = errors.New("group: notes are too long (max 2000)")
	ErrNotFound     = errors.New("group: not found")
)

// New constructs a group. Occasion may be nil (no fixed party date).
func New(id, orgID uuid.UUID, name string, occasion *time.Time, notes string, createdBy uuid.UUID, now time.Time) (*Group, error) {
	return NewFull(id, orgID, name, occasion, notes, 0, "NGN", "", nil, createdBy, now)
}

// NewFull constructs a group with deep aso-ebi master fields.
func NewFull(id, orgID uuid.UUID, name string, occasion *time.Time, notes string, priceMinor int64, currency, fabric string, templateID *uuid.UUID, createdBy uuid.UUID, now time.Time) (*Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameRequired
	}
	if len(name) > 120 {
		return nil, ErrNameTooLong
	}
	notes = strings.TrimSpace(notes)
	if len(notes) > 2000 {
		return nil, ErrNotesTooLong
	}
	if priceMinor < 0 {
		priceMinor = 0
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "NGN"
	}
	fabric = strings.TrimSpace(fabric)
	if len(fabric) > 200 {
		return nil, ErrNotesTooLong
	}
	return &Group{
		ID:             id,
		OrganizationID: orgID,
		Name:           name,
		OccasionDate:   occasion,
		Notes:          notes,
		PriceMinor:     priceMinor,
		Currency:       currency,
		Fabric:         fabric,
		TemplateID:     templateID,
		JoinEnabled:    true,
		JoinSlug:       "",
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// DaysToOccasion reports whole days until the occasion from now (negative
// when past). Nil occasion returns ok=false.
func (g *Group) DaysToOccasion(now time.Time) (days int, ok bool) {
	if g.OccasionDate == nil {
		return 0, false
	}
	y1, m1, d1 := now.Date()
	y2, m2, d2 := g.OccasionDate.Date()
	return int(time.Date(y2, m2, d2, 0, 0, 0, 0, time.UTC).Sub(
		time.Date(y1, m1, d1, 0, 0, 0, 0, time.UTC),
	).Hours() / 24), true
}

// GroupMember is one member order of a group, with customer + money.
// MemberPaid is the bride's manual tick (cash collected offline — no
// gateway). PaidMinor is the tailor's ledger amount. Collected means the
// garment was picked up.
type GroupMember struct {
	OrderID     uuid.UUID
	OrderNumber string
	CustomerID  uuid.UUID
	Customer    string
	Phone       string
	Title       string
	Status      string
	Currency    string
	TotalMinor  int64
	PaidMinor   int64
	MemberPaid  bool
	Collected   bool
	Expected    *time.Time
}

// BalanceMinor is TotalMinor - PaidMinor.
func (m GroupMember) BalanceMinor() int64 { return m.TotalMinor - m.PaidMinor }

// GroupRow is one group for list pages, with rolled-up money.
type GroupRow struct {
	ID           uuid.UUID
	Name         string
	OccasionDate *time.Time
	MemberCount  int
	TotalMinor   int64
	PaidMinor    int64
	Currency     string
	CreatedAt    time.Time
}

// BalanceMinor is TotalMinor - PaidMinor.
func (r GroupRow) BalanceMinor() int64 { return r.TotalMinor - r.PaidMinor }

// GroupDetail is a group plus its members and rolled-up money.
type GroupDetail struct {
	Group        *Group
	Members      []GroupMember
	MemberCount  int
	TotalMinor   int64
	PaidMinor    int64
	BalanceMinor int64
	Currency     string
}
