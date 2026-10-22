package consumption

import (
	"fmt"
	"time"
)

// parseDay reads A3.10's calendar day. Dates are parsed here and passed to
// PostgreSQL as time.Time, so that a malformed one is this package's
// ErrBadPeriod rather than a 22007 that falls through to a 500.
func parseDay(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q is not a YYYY-MM-DD date", ErrBadPeriod, s)
	}
	return t, nil
}

// dayRange turns A3.10's inclusive [from, to] into the half-open
// [from, to+1) that every range column stores. A nil to is open-ended.
func dayRange(from string, to *string) (lo time.Time, hi *time.Time, err error) {
	lo, err = parseDay(from)
	if err != nil {
		return time.Time{}, nil, err
	}
	if to == nil {
		return lo, nil, nil
	}
	end, err := parseDay(*to)
	if err != nil {
		return time.Time{}, nil, err
	}
	if end.Before(lo) {
		return time.Time{}, nil, fmt.Errorf("%w: to is before from", ErrBadPeriod)
	}
	next := end.AddDate(0, 0, 1)
	return lo, &next, nil
}
