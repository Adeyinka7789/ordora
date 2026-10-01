package order

import "errors"

// Status is the lifecycle state of an order.
type Status string

const (
	StatusNew            Status = "NEW"
	StatusConfirmed      Status = "CONFIRMED"
	StatusInProgress     Status = "IN_PROGRESS"
	StatusReady          Status = "READY"
	StatusOutForDelivery Status = "OUT_FOR_DELIVERY"
	StatusDelivered      Status = "DELIVERED"
	StatusCompleted      Status = "COMPLETED"
	StatusCancelled      Status = "CANCELLED"
)

// ErrInvalidTransition is returned when a status change is not allowed by the
// state machine.
var ErrInvalidTransition = errors.New("order: invalid status transition")

// valid lists every recognized status.
var valid = map[Status]bool{
	StatusNew:            true,
	StatusConfirmed:      true,
	StatusInProgress:     true,
	StatusReady:          true,
	StatusOutForDelivery: true,
	StatusDelivered:      true,
	StatusCompleted:      true,
	StatusCancelled:      true,
}

// IsValid reports whether s is a recognized status.
func (s Status) IsValid() bool { return valid[s] }

// IsTerminal reports whether s is a final state (no outgoing transitions).
func (s Status) IsTerminal() bool {
	return s == StatusCompleted || s == StatusCancelled
}

// allowedTransitions encodes the state machine.
//
// The intent:
//
//	NEW              — order captured, not yet committed
//	CONFIRMED        — customer and business agreed
//	IN_PROGRESS      — work has started
//	READY            — finished, awaiting pickup/delivery
//	OUT_FOR_DELIVERY — in transit
//	DELIVERED        — handed off to customer
//	COMPLETED        — closed successfully
//	CANCELLED        — closed unsuccessfully
//
// Cancellation is allowed from every non-terminal state. The domain does not
// enforce a cancellation window; that is a business rule decided later.
var allowedTransitions = map[Status][]Status{
	StatusNew:            {StatusConfirmed, StatusCancelled},
	StatusConfirmed:      {StatusInProgress, StatusCancelled},
	StatusInProgress:     {StatusReady, StatusCancelled},
	StatusReady:          {StatusOutForDelivery, StatusDelivered, StatusCancelled},
	StatusOutForDelivery: {StatusDelivered, StatusCancelled},
	StatusDelivered:      {StatusCompleted},
	StatusCompleted:      {},
	StatusCancelled:      {},
}

// CanTransitionTo reports whether a transition from s to next is allowed.
func (s Status) CanTransitionTo(next Status) bool {
	if !s.IsValid() || !next.IsValid() {
		return false
	}
	for _, ok := range allowedTransitions[s] {
		if ok == next {
			return true
		}
	}
	return false
}

// NextStatuses returns the valid next statuses from s, in a stable order.
// Useful for rendering a status dropdown.
func (s Status) NextStatuses() []Status {
	return append([]Status(nil), allowedTransitions[s]...)
}

// Label returns a human-readable label for the status.
func (s Status) Label() string {
	switch s {
	case StatusNew:
		return "New"
	case StatusConfirmed:
		return "Confirmed"
	case StatusInProgress:
		return "In progress"
	case StatusReady:
		return "Ready"
	case StatusOutForDelivery:
		return "Out for delivery"
	case StatusDelivered:
		return "Delivered"
	case StatusCompleted:
		return "Completed"
	case StatusCancelled:
		return "Cancelled"
	}
	return string(s)
}
