package app

import (
	"testing"
	"time"

	"github.com/google/uuid"
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
	slots := AvailableSlots(day, loc, now, slotTestSettings(), nil, nil, nil, 60, nil)
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
	slots := AvailableSlots(day, loc, now, slotTestSettings(), nil, nil, nil, 60, nil)
	if len(slots) == 0 || slotLabels(slots, loc)[0] != "10:30" {
		t.Fatalf("notice must trim early slots: %v", slotLabels(slots, loc))
	}
	// Beyond the 30-day window: nothing.
	far := day.AddDate(0, 0, 31)
	if got := AvailableSlots(far, loc, now, slotTestSettings(), nil, nil, nil, 60, nil); len(got) != 0 {
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
	labels := slotLabels(AvailableSlots(day, loc, now, st, nil, nil, busy, 60, nil), loc)
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
	labels = slotLabels(AvailableSlots(day, loc, now, st, nil, nil, busy, 60, nil), loc)
	for _, l := range labels {
		if l == "11:00" {
			t.Errorf("buffer-after must block 11:00: %v", labels)
		}
	}

	// Blackout closes the whole day.
	black := map[string]bool{"2026-10-12": true}
	if got := AvailableSlots(day, loc, now, st, nil, black, nil, 60, nil); len(got) != 0 {
		t.Errorf("blackout day must be empty, got %v", slotLabels(got, loc))
	}

	// Sunday closed by default.
	sun := time.Date(2026, 10, 11, 0, 0, 0, 0, loc)
	if got := AvailableSlots(sun, loc, now, st, nil, nil, nil, 60, nil); len(got) != 0 {
		t.Errorf("Sunday must be closed by default, got %v", slotLabels(got, loc))
	}

	// Custom hours shrink the grid (10:00-12:00 → 10:00,10:30,11:00).
	hours := []BookingHour{{Dow: 1, OpenMin: 600, CloseMin: 720, IsOpen: true}}
	labels = slotLabels(AvailableSlots(day, loc, now, st, hours, nil, nil, 60, nil), loc)
	if len(labels) != 3 || labels[0] != "10:00" || labels[2] != "11:00" {
		t.Errorf("custom hours grid wrong: %v", labels)
	}
}

func uuidPtr(s string) *uuid.UUID {
	id := uuid.MustParse(s)
	return &id
}

func TestAvailableSlots_TimezoneBoundary(t *testing.T) {
	// 23:30 UTC Sunday == 00:30 Monday Lagos: the Lagos Monday grid must
	// not leak Sunday-night slots, and vice versa.
	lagos := BookingTimezone("Africa/Lagos")
	monday := slotMonday(lagos)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, lagos)
	slots := AvailableSlots(monday, lagos, now, slotTestSettings(), nil, nil, nil, 60, nil)
	for _, s := range slots {
		if s.Start.In(lagos).Format("2006-01-02") != "2026-10-12" {
			t.Errorf("slot leaks across the day boundary: %v", s.Start)
		}
		if s.Start.Location() != lagos {
			t.Errorf("slot must carry the org location")
		}
	}
}

func TestAvailableSlots_StaffLanes(t *testing.T) {
	loc := BookingTimezone("Africa/Lagos")
	day := slotMonday(loc)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, loc)
	st := slotTestSettings()
	ada := uuidPtr("11111111-1111-1111-8111-111111111111")
	bola := uuidPtr("22222222-2222-2222-8222-222222222222")
	at := func(hh, mm int) time.Time {
		return time.Date(2026, 10, 12, hh, mm, 0, 0, loc)
	}
	busy := []BusyBlock{
		{Start: at(10, 0), End: at(11, 0), StaffUserID: ada}, // Ada busy
		{Start: at(14, 0), End: at(15, 0)},                   // shared demand
	}
	has := func(labels []string, want string) bool {
		for _, l := range labels {
			if l == want {
				return true
			}
		}
		return false
	}

	// Ada's lane: her own 10:00 blocks her, Bola's absence doesn't matter,
	// but the unassigned 14:00 blocks everyone.
	adaLabels := slotLabels(AvailableSlots(day, loc, now, st, nil, nil, busy, 60, ada), loc)
	if has(adaLabels, "10:00") || has(adaLabels, "10:30") {
		t.Errorf("Ada must not see her own booked slots: %v", adaLabels)
	}
	if !has(adaLabels, "11:00") {
		t.Errorf("Ada must see 11:00 (Bola is free, irrelevant): %v", adaLabels)
	}
	if has(adaLabels, "14:00") || has(adaLabels, "14:30") {
		t.Errorf("unassigned demand must block named picks: %v", adaLabels)
	}

	// Bola's lane: Ada's 10:00 does not block him.
	bolaLabels := slotLabels(AvailableSlots(day, loc, now, st, nil, nil, busy, 60, bola), loc)
	if !has(bolaLabels, "10:00") {
		t.Errorf("Bola must see 10:00 despite Ada's booking: %v", bolaLabels)
	}
	if has(bolaLabels, "14:00") {
		t.Errorf("unassigned demand must block Bola too: %v", bolaLabels)
	}

	// Anyone: conservative org-wide view blocks on any demand.
	anyLabels := slotLabels(AvailableSlots(day, loc, now, st, nil, nil, busy, 60, nil), loc)
	for _, gone := range []string{"10:00", "10:30", "14:00", "14:30"} {
		if has(anyLabels, gone) {
			t.Errorf("anyone view must hide %s: %v", gone, anyLabels)
		}
	}
	if !has(anyLabels, "11:00") {
		t.Errorf("anyone view must show 11:00: %v", anyLabels)
	}
}

func TestAvailableSlots_ServiceDuration(t *testing.T) {
	loc := BookingTimezone("Africa/Lagos")
	day := slotMonday(loc)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, loc)
	st := slotTestSettings()

	// 120-minute service: last start 15:00 (ends at close 17:00).
	slots := AvailableSlots(day, loc, now, st, nil, nil, nil, 120, nil)
	if len(slots) == 0 {
		t.Fatal("expected slots for 120min service")
	}
	labels := slotLabels(slots, loc)
	if labels[len(labels)-1] != "15:00" {
		t.Errorf("120min last start must be 15:00: %v", labels)
	}
	for i, s := range slots {
		if s.End.Sub(s.Start) != 2*time.Hour {
			t.Errorf("slot %d must be 120min", i)
		}
	}
}
