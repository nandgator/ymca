package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewEntry_RejectsPastDate(t *testing.T) {
	policy := testPolicy()
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	yesterday := day(2026, 8, 3)

	_, err := NewEntry(uuid.New(), uuid.New(), yesterday, MealBreakfast, nil, false, policy, now)
	if err != ErrEntryLocked {
		t.Fatalf("expected ErrEntryLocked for a backdated entry, got %v", err)
	}
}

func TestNewEntry_RejectsFutureDate(t *testing.T) {
	policy := testPolicy()
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	tomorrow := day(2026, 8, 5)

	_, err := NewEntry(uuid.New(), uuid.New(), tomorrow, MealDinner, nil, false, policy, now)
	if err != ErrEntryLocked {
		t.Fatalf("expected ErrEntryLocked for a future-dated entry (advance declarations are out of scope), got %v", err)
	}
}

func TestNewEntry_AllowsSameDayUntilMidnight(t *testing.T) {
	policy := testPolicy()
	today := day(2026, 8, 4)
	justBeforeMidnight := time.Date(2026, 8, 4, 23, 59, 0, 0, time.UTC)

	e, err := NewEntry(uuid.New(), uuid.New(), today, MealDinner, nil, true, policy, justBeforeMidnight)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !e.NonVeg {
		t.Fatalf("expected non-veg flag to be preserved")
	}
}

func TestNewEntry_LocksExactlyAtMidnightRollover(t *testing.T) {
	policy := testPolicy()
	today := day(2026, 8, 4)
	nextDayStart := time.Date(2026, 8, 5, 0, 0, 1, 0, time.UTC)

	_, err := NewEntry(uuid.New(), uuid.New(), today, MealBreakfast, nil, false, policy, nextDayStart)
	if err != ErrEntryLocked {
		t.Fatalf("expected lock just after midnight, got %v", err)
	}
}

func TestNewEntry_OptionalItemsRejectedOnDinner(t *testing.T) {
	policy := testPolicy()
	today := day(2026, 8, 4)
	now := time.Date(2026, 8, 4, 20, 0, 0, 0, time.UTC)

	_, err := NewEntry(uuid.New(), uuid.New(), today, MealDinner, []uuid.UUID{policy.OptionalItems[0].ID}, false, policy, now)
	if err != ErrOptionalItemOnDinner {
		t.Fatalf("expected ErrOptionalItemOnDinner, got %v", err)
	}
}

func TestNewEntry_UnknownOptionalItemRejected(t *testing.T) {
	policy := testPolicy()
	today := day(2026, 8, 4)
	now := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)

	_, err := NewEntry(uuid.New(), uuid.New(), today, MealBreakfast, []uuid.UUID{uuid.New()}, false, policy, now)
	if err != ErrUnknownOptionalItem {
		t.Fatalf("expected ErrUnknownOptionalItem for an item not in this hostel's price list, got %v", err)
	}
}

func TestNewEntry_ValidBreakfastWithOptionalItems(t *testing.T) {
	policy := testPolicy()
	today := day(2026, 8, 4)
	now := time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)

	e, err := NewEntry(uuid.New(), uuid.New(), today, MealBreakfast, []uuid.UUID{policy.OptionalItems[0].ID, policy.OptionalItems[1].ID}, false, policy, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(e.OptionalItemIDs) != 2 {
		t.Fatalf("expected 2 optional items on entry, got %d", len(e.OptionalItemIDs))
	}
}
