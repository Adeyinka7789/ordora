package handlers

import (
	"testing"
	"time"
)

// TestParseMoneyMinorAcceptsCommas locks the Nigerian input style: grouping
// commas, spaces and pasted currency symbols must parse on every money form
// (all of them funnel through this function).
func TestParseMoneyMinorAcceptsCommas(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"1500.00", 150000},
		{"1500", 150000},
		{"50,000", 5000000},
		{"50,000.00", 5000000},
		{"50,000.50", 5000050},
		{"1,234,567.89", 123456789},
		{"50 000", 5000000},
		{"₦50,000", 5000000},
		{"NGN 50,000.25", 5000025},
		{"0", 0},
		{"0.05", 5},
	}
	for _, tc := range cases {
		got, err := parseMoneyMinor(tc.in)
		if err != nil {
			t.Errorf("parseMoneyMinor(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseMoneyMinor(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}

	for _, in := range []string{"", "   ", ",", "abc", "12.34.56", "-50", "₦"} {
		if _, err := parseMoneyMinor(in); err == nil {
			t.Errorf("parseMoneyMinor(%q): expected error, got nil", in)
		}
	}
}

// TestParseQuantityAcceptsCommas mirrors the money behavior for quantities.
func TestParseQuantityAcceptsCommas(t *testing.T) {
	got, err := parseQuantity("1,000")
	if err != nil || got != 1000*1000 {
		t.Errorf("parseQuantity(1,000) = %d, %v; want 1000000, nil", got, err)
	}
	got, err = parseQuantity("2.5")
	if err != nil || got != 2500 {
		t.Errorf("parseQuantity(2.5) = %d, %v; want 2500, nil", got, err)
	}
	for _, in := range []string{"", "0", "-2", "abc"} {
		if _, err := parseQuantity(in); err == nil {
			t.Errorf("parseQuantity(%q): expected error, got nil", in)
		}
	}
}

// TestParseDateInput checks day-month-year first (the displayed format) with
// ISO fallback (native date inputs), plus rejection of anything else.
func TestParseDateInput(t *testing.T) {
	want := time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC)
	for _, in := range []string{"05-10-2026", "5-10-2026", "05-10-2026 ", "2026-10-05"} {
		got, err := parseDateInput(in)
		if err != nil {
			t.Errorf("parseDateInput(%q): unexpected error: %v", in, err)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("parseDateInput(%q) = %v, want %v", in, got, want)
		}
	}
	// Ambiguous-looking input must resolve day-first: 05-10 is 5 October.
	got, err := parseDateInput("05-10-2026")
	if err != nil || got.Month() != time.October || got.Day() != 5 {
		t.Errorf("day-first ordering broken: %v, %v", got, err)
	}
	for _, in := range []string{"", "2026/10/05", "10-05-2026x", "32-01-2026", "05-13-2026", "October 5"} {
		if _, err := parseDateInput(in); err == nil {
			t.Errorf("parseDateInput(%q): expected error, got nil", in)
		}
	}
}
