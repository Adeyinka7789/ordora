package group

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNew_Validation(t *testing.T) {
	now := time.Now()
	if _, err := New(uuid.New(), uuid.New(), "", nil, "", uuid.New(), now); err != ErrNameRequired {
		t.Errorf("empty name: got %v", err)
	}
	if _, err := New(uuid.New(), uuid.New(), "  Ada's Wedding  ", nil, "", uuid.New(), now); err != nil {
		t.Errorf("valid group rejected: %v", err)
	} else {
		_ = now
	}
	long := make([]byte, 121)
	for i := range long {
		long[i] = 'x'
	}
	if _, err := New(uuid.New(), uuid.New(), string(long), nil, "", uuid.New(), now); err != ErrNameTooLong {
		t.Errorf("long name: got %v", err)
	}
}

func TestDaysToOccasion(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	g := &Group{}
	if _, ok := g.DaysToOccasion(now); ok {
		t.Error("nil occasion should report not-ok")
	}
	future := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	past := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	g.OccasionDate = &future
	if d, _ := g.DaysToOccasion(now); d != 10 {
		t.Errorf("expected 10 days, got %d", d)
	}
	g.OccasionDate = &past
	if d, _ := g.DaysToOccasion(now); d != -5 {
		t.Errorf("expected -5 days, got %d", d)
	}
}
