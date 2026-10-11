package booking

import "errors"

// Status is the lifecycle state of an appointment. The linked order keeps
// its own status (NEW while requested, CONFIRMED once the visit is
// confirmed); the two advance together through the staff actions.
type Status string

const (
	StatusRequested Status = "REQUESTED"
	StatusConfirmed Status = "CONFIRMED"
	StatusCompleted Status = "COMPLETED"
	StatusCancelled Status = "CANCELLED"
	StatusNoShow    Status = "NO_SHOW"
)

// ErrInvalidTransition is returned when a status change is not allowed by
// the state machine.
var ErrInvalidTransition = errors.New("booking: invalid status transition")

// valid lists every recognized status.
var valid = map[Status]bool{
	StatusRequested: true,
	StatusConfirmed: true,
	StatusCompleted: true,
	StatusCancelled: true,
	StatusNoShow:    true,
}

// IsValid reports whether s is a recognized status.
func (s Status) IsValid() bool { return valid[s] }

// IsTerminal reports whether s is a final state (no outgoing transitions).
func (s Status) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusCancelled, StatusNoShow:
		return true
	}
	return false
}

// BlocksSlot reports whether a booking in state s reserves its time slot.
// Only live appointments block: cancelled and no-show visits free the slot,
// completed ones are in the past.
func (s Status) BlocksSlot() bool {
	return s == StatusRequested || s == StatusConfirmed
}

// allowedTransitions encodes the state machine.
//
//	REQUESTED — customer asked for a slot, staff hasn't reviewed
//	CONFIRMED — staff accepted the visit
//	COMPLETED — visit happened
//	CANCELLED — called off (by either side); frees the slot
//	NO_SHOW   — customer never arrived; frees the slot
var allowedTransitions = map[Status][]Status{
	StatusRequested: {StatusConfirmed, StatusCancelled},
	StatusConfirmed: {StatusCompleted, StatusCancelled, StatusNoShow},
	StatusCompleted: {},
	StatusCancelled: {},
	StatusNoShow:    {},
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

// Label returns a human-readable label for the status.
func (s Status) Label() string {
	switch s {
	case StatusRequested:
		return "Requested"
	case StatusConfirmed:
		return "Confirmed"
	case StatusCompleted:
		return "Completed"
	case StatusCancelled:
		return "Cancelled"
	case StatusNoShow:
		return "No show"
	}
	return string(s)
}
