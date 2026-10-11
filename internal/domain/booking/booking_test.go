package booking

import "testing"

func TestStatusMachine(t *testing.T) {
	ok := map[Status][]Status{
		StatusRequested: {StatusConfirmed, StatusCancelled},
		StatusConfirmed: {StatusCompleted, StatusCancelled, StatusNoShow},
	}
	for from, tos := range ok {
		for _, to := range tos {
			if !from.CanTransitionTo(to) {
				t.Errorf("%s -> %s must be allowed", from, to)
			}
		}
	}
	denied := [][2]Status{
		{StatusRequested, StatusCompleted},
		{StatusRequested, StatusNoShow},
		{StatusConfirmed, StatusRequested},
		{StatusCompleted, StatusCancelled},
		{StatusCancelled, StatusRequested},
		{StatusNoShow, StatusConfirmed},
		{StatusRequested, "BOGUS"},
		{"BOGUS", StatusConfirmed},
	}
	for _, tt := range denied {
		if tt[0].CanTransitionTo(tt[1]) {
			t.Errorf("%s -> %s must be denied", tt[0], tt[1])
		}
	}
}

func TestBlocksSlot(t *testing.T) {
	for _, s := range []Status{StatusRequested, StatusConfirmed} {
		if !s.BlocksSlot() {
			t.Errorf("%s must block its slot", s)
		}
	}
	for _, s := range []Status{StatusCompleted, StatusCancelled, StatusNoShow} {
		if s.BlocksSlot() {
			t.Errorf("%s must free its slot", s)
		}
	}
	for _, s := range []Status{StatusRequested, StatusConfirmed, StatusCompleted, StatusCancelled, StatusNoShow} {
		if s.IsTerminal() != (s == StatusCompleted || s == StatusCancelled || s == StatusNoShow) {
			t.Errorf("%s terminal mismatch", s)
		}
		if !s.IsValid() {
			t.Errorf("%s must be valid", s)
		}
		if s.Label() == string(s) && s != StatusNoShow {
			t.Errorf("%s must have a friendly label", s)
		}
	}
}
