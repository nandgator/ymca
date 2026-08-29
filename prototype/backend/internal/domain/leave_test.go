package domain

import (
	"testing"

	"github.com/google/uuid"
)

func TestLeave_DurationDays_IsInclusive(t *testing.T) {
	l, err := NewLeave(uuid.New(), uuid.New(), day(2026, 8, 1), day(2026, 8, 1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l.DurationDays() != 1 {
		t.Fatalf("single-day leave should have duration 1, got %d", l.DurationDays())
	}

	l2, _ := NewLeave(uuid.New(), uuid.New(), day(2026, 8, 1), day(2026, 8, 7))
	if l2.DurationDays() != 7 {
		t.Fatalf("Aug 1-7 inclusive should be 7 days, got %d", l2.DurationDays())
	}
}

func TestLeave_TypeClassification(t *testing.T) {
	threshold := 7
	shortLeave, _ := NewLeave(uuid.New(), uuid.New(), day(2026, 8, 1), day(2026, 8, 6)) // 6 days
	longLeave, _ := NewLeave(uuid.New(), uuid.New(), day(2026, 8, 1), day(2026, 8, 7))  // exactly 7 days -> boundary

	if shortLeave.Type(threshold) != LeaveShort {
		t.Fatalf("6-day leave with threshold 7 should be SHORT")
	}
	if longLeave.Type(threshold) != LeaveLong {
		t.Fatalf("exactly-threshold (7-day) leave should be LONG (>=, not >)")
	}
}

func TestNewLeave_RejectsEndBeforeStart(t *testing.T) {
	_, err := NewLeave(uuid.New(), uuid.New(), day(2026, 8, 10), day(2026, 8, 5))
	if err != ErrLeaveDatesInvalid {
		t.Fatalf("expected ErrLeaveDatesInvalid, got %v", err)
	}
}

func TestLeave_Overlaps(t *testing.T) {
	a, _ := NewLeave(uuid.New(), uuid.New(), day(2026, 8, 1), day(2026, 8, 10))
	b, _ := NewLeave(uuid.New(), uuid.New(), day(2026, 8, 10), day(2026, 8, 15)) // touches at Aug 10
	c, _ := NewLeave(uuid.New(), uuid.New(), day(2026, 8, 11), day(2026, 8, 15)) // no overlap

	if !a.Overlaps(b) {
		t.Fatalf("leave periods sharing a boundary day should count as overlapping")
	}
	if a.Overlaps(c) {
		t.Fatalf("leave periods with a gap should not overlap")
	}
}

func TestLeave_Covers(t *testing.T) {
	l, _ := NewLeave(uuid.New(), uuid.New(), day(2026, 8, 5), day(2026, 8, 10))

	if !l.Covers(day(2026, 8, 5)) || !l.Covers(day(2026, 8, 10)) {
		t.Fatalf("Covers should be inclusive of both boundary dates")
	}
	if !l.Covers(day(2026, 8, 7)) {
		t.Fatalf("Covers should be true for a date strictly inside the range")
	}
	if l.Covers(day(2026, 8, 4)) || l.Covers(day(2026, 8, 11)) {
		t.Fatalf("Covers should be false just outside the range")
	}
}
