package app

import (
	"testing"
	"time"
)

func slotTestSettings() BookingSettings {
	return BookingSettings{
		DurationMin: 60, BufBefore: 0, BufAfter: 0,
		MinNoticeHours: 2, MaxAdvanceDays: 30,
	}
}

// Monday 2026-10-12 (matches the egg tests' week).
func slotMonday(loc *time.Location) time.Time {
	return time.Date(2026, 10, 12, 0, 0, 0, 0, loc)
}

func slotLabels(slots []Slot, loc *time.Location) []string {
	var out []string
	for _, s := range slots {
		out = append(out, s.Start.In(loc).Format("15:04"))
	}
	return out
}

func TestAvailableSlots_BasicGrid(t *testing.T) {
	loc := BookingTimezone("Africa/Lagos")
	day := slotMonday(loc)
	// Monday 09:00 open; now = 07:00 (notice satisfied for 09:00+).
	now := time.Date(2026, 10, 12, 7, 0, 0, 0, loc)
	slots := AvailableSlots(day, loc, now, slotTestSettings(), nil, nil, nil, 60)
	// 09:00..16:00 starts (16:00 + 60min = 17:00 close), 30-min grid.
	if len(slots) != 15 {
		t.Fatalf("want 15 slots, got %d (%v)", len(slots), slotLabels(slots, loc))
	}
	got := slotLabels(slots, loc)
	if got[0] != "09:00" || got[len(got)-1] != "16:00" {
		t.Errorf("grid bounds wrong: %v", got)
	}
	for _, s := range slots {
		if s.End.Sub(s.Start) != time.Hour {
			t.Errorf("slot must be 60min: %v", s)
		}
	}
}

func TestAvailableSlots_NoticeAndBounds(t *testing.T) {
	loc := BookingTimezone("Africa/Lagos")
	day := slotMonday(loc)
	// Now 08:30 + 2h notice: 09:00/09:30/10:00 fall away, 10:30 first.
	now := time.Date(2026, 10, 12, 8, 30, 0, 0, loc)
	slots := AvailableSlots(day, loc, now, slotTestSettings(), nil, nil, nil, 60)
	if len(slots) == 0 || slotLabels(slots, loc)[0] != "10:30" {
		t.Fatalf("notice must trim early slots: %v", slotLabels(slots, loc))
	}
	// Beyond the 30-day window: nothing.
	far := day.AddDate(0, 0, 31)
	if got := AvailableSlots(far, loc, now, slotTestSettings(), nil, nil, nil, 60); len(got) != 0 {
		t.Errorf("beyond advance window must be empty, got %v", slotLabels(got, loc))
	}
}

func TestAvailableSlots_BusyBuffersAndBlackout(t *testing.T) {
	loc := BookingTimezone("Africa/Lagos")
	day := slotMonday(loc)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, loc) // Sunday noon: notice fine
	st := slotTestSettings()

	// Live 10:00-11:00 blocks 09:30/10:00/10:30 starts (half-open overlap);
	// 09:00 ends exactly at 10:00 and stays open.
	busy := []BusyBlock{{
		Start: time.Date(2026, 10, 12, 10, 0, 0, 0, loc),
		End:   time.Date(2026, 10, 12, 11, 0, 0, 0, loc),
	}}
	labels := slotLabels(AvailableSlots(day, loc, now, st, nil, nil, busy, 60), loc)
	for _, gone := range []string{"09:30", "10:00", "10:30"} {
		for _, l := range labels {
			if l == gone {
				t.Errorf("slot %s must be blocked by 10:00 booking", gone)
			}
		}
	}
	found900, found1100 := false, false
	for _, l := range labels {
		if l == "09:00" {
			found900 = true
		}
		if l == "11:00" {
			found1100 = true
		}
	}
	if !found900 || !found1100 {
		t.Errorf("adjacent slots must stay open: %v", labels)
	}

	// Buffer-after 30 on the booking pushes 11:00 out (11:00-11:30 window
	// touches 10:00-11:30 buffered).
	busy[0].BufAfter = 30
	labels = slotLabels(AvailableSlots(day, loc, now, st, nil, nil, busy, 60), loc)
	for _, l := range labels {
		if l == "11:00" {
			t.Errorf("buffer-after must block 11:00: %v", labels)
		}
	}

	// Blackout closes the whole day.
	black := map[string]bool{"2026-10-12": true}
	if got := AvailableSlots(day, loc, now, st, nil, black, nil, 60); len(got) != 0 {
		t.Errorf("blackout day must be empty, got %v", slotLabels(got, loc))
	}

	// Sunday closed by default.
	sun := time.Date(2026, 10, 11, 0, 0, 0, 0, loc)
	if got := AvailableSlots(sun, loc, now, st, nil, nil, nil, 60); len(got) != 0 {
		t.Errorf("Sunday must be closed by default, got %v", slotLabels(got, loc))
	}

	// Custom hours shrink the grid (10:00-12:00 → 10:00,10:30,11:00).
	hours := []BookingHour{{Dow: 1, OpenMin: 600, CloseMin: 720, IsOpen: true}}
	labels = slotLabels(AvailableSlots(day, loc, now, st, hours, nil, nil, 60), loc)
	if len(labels) != 3 || labels[0] != "10:00" || labels[2] != "11:00" {
		t.Errorf("custom hours grid wrong: %v", labels)
	}
}

func TestAvailableSlots_TimezoneBoundary(t *testing.T) {
	// 23:30 UTC Sunday == 00:30 Monday Lagos: the Lagos Monday grid must
	// not leak Sunday-night slots, and vice versa.
	lagos := BookingTimezone("Africa/Lagos")
	monday := slotMonday(lagos)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, lagos)
	slots := AvailableSlots(monday, lagos, now, slotTestSettings(), nil, nil, nil, 60)
	for _, s := range slots {
		if s.Start.In(lagos).Format("2006-01-02") != "2026-10-12" {
			t.Errorf("slot leaks across the day boundary: %v", s.Start)
		}
		if s.Start.Location() != lagos {
			t.Errorf("slot must carry the org location")
		}
	}
}
