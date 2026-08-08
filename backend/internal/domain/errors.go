package domain

import "errors"

var (
	ErrEntryLocked          = errors.New("entry: day is locked, cannot submit or edit after midnight")
	ErrEntryOnLeaveDay      = errors.New("entry: member is on registered leave for this date, entry not required")
	ErrInvalidMealType      = errors.New("entry: meal type must be BREAKFAST or DINNER")
	ErrUnknownOptionalItem  = errors.New("entry: optional item is not in this hostel's price list")
	ErrOptionalItemOnDinner = errors.New("entry: optional items only apply to breakfast entries")
	ErrLeaveDatesInvalid    = errors.New("leave: end_date must not be before start_date")
	ErrLeaveOverlaps        = errors.New("leave: overlaps an existing leave period for this member")
	ErrNotInHostel          = errors.New("access: actor is not scoped to this hostel")
	ErrForbidden            = errors.New("access: action not permitted for this role")
)
