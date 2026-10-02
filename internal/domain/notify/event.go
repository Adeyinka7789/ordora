// Package notify defines the shape of outbound notifications: events written
// by the domain, delivered to customers and business owners by the worker.
package notify

import (
	"time"

	"github.com/google/uuid"
)

// Event names. These are the strings that land in the outbox.event_name column.
const (
	EventUserRegistered       = "user.registered"
	EventOrderCreated         = "order.created"
	EventPaymentRecorded      = "payment.recorded"
	EventOrderStatusChanged   = "order.status_changed"
	EventOrderReady           = "order.ready"
	EventPublicIntakeReceived = "public.intake_received"
)

// Kind identifies the specific email template to render. One event can map to
// zero or more notification kinds (e.g. order.created sends to the customer
// and to the owner).
type Kind string

const (
	KindWelcomeVerify     Kind = "WELCOME_VERIFY"
	KindPasswordReset     Kind = "PASSWORD_RESET"
	KindOrderCreatedCust  Kind = "ORDER_CREATED_CUSTOMER"
	KindOrderCreatedOwner Kind = "ORDER_CREATED_OWNER"
	KindPaymentReceived   Kind = "PAYMENT_RECEIVED"
	KindStatusChanged     Kind = "ORDER_STATUS_CHANGED"
	KindOrderReady        Kind = "ORDER_READY"
	KindIntakeReceived    Kind = "PUBLIC_INTAKE_RECEIVED"
)

// -----------------------------------------------------------------------------
// Event payloads
// -----------------------------------------------------------------------------

// UserRegistered is emitted when a new user account is created.
type UserRegistered struct {
	UserID    uuid.UUID
	Email     string
	Name      string
	VerifyURL string // pre-built
	OrgName   string
}

// OrderCreated is emitted when a business creates an order.
// Sent to the customer (if they have an email) and to the owner.
type OrderCreated struct {
	OrderID       uuid.UUID
	OrderNumber   string
	Title         string
	TotalMinor    int64
	Currency      string
	CustomerID    uuid.UUID
	CustomerName  string
	CustomerEmail string
	OrgName       string
	OrgEmail      string
	PortalURL     string // if a token was generated, this is the customer link
	ExpectedDate  *time.Time
}

// PaymentRecorded is emitted when a payment is inserted.
type PaymentRecorded struct {
	PaymentID     uuid.UUID
	OrderID       uuid.UUID
	OrderNumber   string
	AmountMinor   int64
	Currency      string
	PaidMinor     int64
	TotalMinor    int64
	BalanceMinor  int64
	Method        string
	CustomerName  string
	CustomerEmail string
	OrgName       string
	PortalURL     string
}

// OrderStatusChanged is emitted on every legal status transition.
type OrderStatusChanged struct {
	OrderID       uuid.UUID
	OrderNumber   string
	From          string
	To            string
	CustomerName  string
	CustomerEmail string
	OrgName       string
	PortalURL     string
}

// OrderReady is emitted when status becomes READY. Distinct from the generic
// status change because the message is different ("your order is ready").
type OrderReady struct {
	OrderID       uuid.UUID
	OrderNumber   string
	CustomerName  string
	CustomerEmail string
	OrgName       string
	PortalURL     string
}

// PublicIntakeReceived is emitted when a stranger submits the public form.
type PublicIntakeReceived struct {
	OrderID       uuid.UUID
	OrderNumber   string
	CustomerName  string
	CustomerEmail string
	CustomerPhone string
	Description   string
	OrgName       string
	OrgEmail      string // owner recipient
	AdminURL      string // link to admin order detail
}
