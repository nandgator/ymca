package domain

import (
	"time"

	"github.com/google/uuid"
)

type MealType string

const (
	MealBreakfast MealType = "BREAKFAST"
	MealDinner    MealType = "DINNER"
)

// Entry is (member, date, meal_type) — an after-the-fact record of what a
// member had. Submitted/editable same-day until midnight in the hostel's
// local time, then locked forever (immutable history).
type Entry struct {
	ID         uuid.UUID
	MemberID   uuid.UUID
	HostelID   uuid.UUID
	Date       Day // calendar date, no time component
	MealType   MealType
	CreatedAt  time.Time
	UpdatedAt  time.Time

	// Only set (and only meaningful) when MealType == MealBreakfast.
	OptionalItemIDs []uuid.UUID

	// Only set (and only meaningful) when MealType == MealDinner.
	// Decided fresh per entry — this is NOT a standing member preference.
	NonVeg bool
}

// Day is a calendar date with no time-of-day, so "same day" comparisons
// can't be fooled by timezones or hour components.
type Day struct {
	Year  int
	Month time.Month
	Day   int
}

func DayOf(t time.Time) Day {
	y, m, d := t.Date()
	return Day{Year: y, Month: m, Day: d}
}

func (d Day) Time() time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

func (d Day) Before(other Day) bool { return d.Time().Before(other.Time()) }
func (d Day) Equal(other Day) bool  { return d == other }

// NewEntry validates and constructs an Entry. now is injected so this stays
// testable without wall-clock dependence.
func NewEntry(memberID, hostelID uuid.UUID, date Day, mealType MealType, optionalItemIDs []uuid.UUID, nonVeg bool, policy HostelPolicy, now time.Time) (Entry, error) {
	if mealType != MealBreakfast && mealType != MealDinner {
		return Entry{}, ErrInvalidMealType
	}
	// Entries are same-day only: a member logs today's meal today. Any other
	// date (past or future) is either already locked or not yet reachable.
	if !date.Equal(DayOf(now)) {
		return Entry{}, ErrEntryLocked
	}
	if IsLocked(date, now) {
		return Entry{}, ErrEntryLocked
	}

	if mealType == MealDinner && len(optionalItemIDs) > 0 {
		return Entry{}, ErrOptionalItemOnDinner
	}
	if mealType == MealBreakfast {
		for _, id := range optionalItemIDs {
			if _, ok := policy.FindOptionalItem(id); !ok {
				return Entry{}, ErrUnknownOptionalItem
			}
		}
	}

	return Entry{
		ID:              uuid.New(),
		MemberID:        memberID,
		HostelID:        hostelID,
		Date:            date,
		MealType:        mealType,
		OptionalItemIDs: optionalItemIDs,
		NonVeg:          mealType == MealDinner && nonVeg,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

// IsLocked reports whether this entry's day can no longer be edited.
func IsLocked(date Day, now time.Time) bool {
	return now.After(date.Time().Add(24 * time.Hour))
}
