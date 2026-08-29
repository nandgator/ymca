package domain

import "github.com/google/uuid"

type LeaveType string

const (
	LeaveShort LeaveType = "SHORT"
	LeaveLong  LeaveType = "LONG"
)

// Leave is a member-self-registered period during which no Entry is
// required for either meal. No approval step exists — registering it is
// sufficient. Its billing effect depends on duration vs the hostel's
// LongLeaveThresholdDays:
//
//   - LONG (duration >= threshold): exempt from entries AND from billing
//     (deducted at HostelPolicy.DailyDeductionINR per day).
//   - SHORT (duration < threshold): exempt from entries only — still
//     billed as if present (baseline veg dinner rate), since the mess may
//     already have provisioned for the member.
type Leave struct {
	ID        uuid.UUID
	MemberID  uuid.UUID
	HostelID  uuid.UUID
	StartDate Day
	EndDate   Day
}

// DurationDays is inclusive of both start and end dates.
func (l Leave) DurationDays() int {
	return int(l.EndDate.Time().Sub(l.StartDate.Time()).Hours()/24) + 1
}

func (l Leave) Type(thresholdDays int) LeaveType {
	if l.DurationDays() >= thresholdDays {
		return LeaveLong
	}
	return LeaveShort
}

// Covers reports whether date d falls within [StartDate, EndDate] inclusive.
func (l Leave) Covers(d Day) bool {
	notBeforeStart := d.Equal(l.StartDate) || l.StartDate.Before(d)
	notAfterEnd := d.Equal(l.EndDate) || d.Before(l.EndDate)
	return notBeforeStart && notAfterEnd
}

// NewLeave validates and constructs a Leave.
func NewLeave(memberID, hostelID uuid.UUID, start, end Day) (Leave, error) {
	if end.Before(start) {
		return Leave{}, ErrLeaveDatesInvalid
	}
	return Leave{
		ID:        uuid.New(),
		MemberID:  memberID,
		HostelID:  hostelID,
		StartDate: start,
		EndDate:   end,
	}, nil
}

// Overlaps reports whether two leave periods for the same member intersect.
func (l Leave) Overlaps(other Leave) bool {
	return !l.EndDate.Before(other.StartDate) && !other.EndDate.Before(l.StartDate)
}
