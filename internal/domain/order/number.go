package order

import (
	"fmt"
	"regexp"
	"time"
)

// Order numbers are formatted as: ORD-YYYY-NNNNNN
//
//   ORD     — fixed prefix, makes them recognizable in inboxes and on receipts
//   YYYY    — the year the order was created (in the org's timezone)
//   NNNNNN  — zero-padded counter, scoped to (org, year)
//
// The counter is assigned by the database (per-org, per-year sequence) to
// guarantee uniqueness under concurrency. This package only formats and
// validates the number, it does not generate it.
//
// Six digits allows 999,999 orders per org per year. If a tenant ever exceeds
// that, the sequence widens automatically (the format does not hard-code width
// on read, only on write).

var orderNumberRE = regexp.MustCompile(`^ORD-\d{4}-\d{6}$`)

// FormatOrderNumber constructs an order number from a year and sequence.
func FormatOrderNumber(year int, seq int64) string {
	return fmt.Sprintf("ORD-%04d-%06d", year, seq)
}

// YearOf returns the year embedded in an order number.
func YearOf(number string) (int, error) {
	if !orderNumberRE.MatchString(number) {
		return 0, fmt.Errorf("order: bad order number format %q", number)
	}
	var y int
	if _, err := fmt.Sscanf(number[4:8], "%d", &y); err != nil {
		return 0, err
	}
	return y, nil
}

// IsValidOrderNumber reports whether s looks like a well-formed order number.
func IsValidOrderNumber(s string) bool {
	return orderNumberRE.MatchString(s)
}

// CurrentYear returns the year in the given timezone, for use when generating
// the "YYYY" segment.
func CurrentYear(t time.Time, timezone string) int {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return t.UTC().Year()
	}
	return t.In(loc).Year()
}
