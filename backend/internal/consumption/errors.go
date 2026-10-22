package consumption

import "errors"

// The refusals consumption can make beyond ErrNotFound (consumption.go).
// Each maps to one of A3.4's codes in cmd/api/handlers.go; the comment says
// which, because a code added here and not there is a 500.
var (
	// invalid_request: the type exists and is not taking records.
	ErrInactiveType = errors.New("consumption: that consumption type is not active")

	// record_mode_not_allowed (422).
	ErrRecordModeNotAllowed = errors.New("consumption: that consumption type does not take this kind of recording")

	// invalid_request: the day is more than a day ahead of UTC.
	ErrFutureDate = errors.New("consumption: occurred_on is in the future")

	// invalid_request: an option code the type does not offer.
	ErrUnknownOption = errors.New("consumption: an option code is unknown or withdrawn for this type")

	// invalid_request: the request asks for something that cannot be done to
	// this record (voiding what is already void).
	ErrAlreadyVoided = errors.New("consumption: that record is already void")

	// invalid_request: obligations belong to types that obligate (ADR-097).
	ErrNotObligating = errors.New("consumption: that consumption type does not obligate")

	// invalid_request: the dates do not make a range.
	ErrBadPeriod = errors.New("consumption: the period is empty or reversed")

	// 409 already_recorded: a live record exists; the recourse is a correction.
	ErrAlreadyRecorded = errors.New("consumption: a live record already exists for that person, type and day")

	// 409 record_already_corrected: a record is superseded once.
	ErrAlreadyCorrected = errors.New("consumption: that record has already been corrected")

	// 409 consumption_window_closed.
	ErrWindowClosed = errors.New("consumption: the correction window for that record has closed")

	// 409 period_closed: the day lies in a period already handed to Finance.
	ErrPeriodClosed = errors.New("consumption: that day lies in a period that has been closed")

	// 409 period_already_closed: the exclusion constraint of migration 0006.
	ErrPeriodAlreadyClosed = errors.New("consumption: that period overlaps one already closed")

	// 409 obligation_overlaps.
	ErrObligationOverlaps = errors.New("consumption: an obligation of that type already covers those days")

	// invalid_request: not an ACTIVE membership held by that person.
	ErrNotAMember = errors.New("consumption: that person holds no active membership in this tenant")
)

// The SQLSTATEs and constraint names this package maps. The NAMES are a
// contract with the schema (migrations 0001 and 0006): a rename compiles,
// vets clean, and turns a refusal the caller can act on into a 500.
const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
	exclusionViolation  = "23P01"

	constraintLiveRecord      = "one_live_record_per_subject_type_day"
	constraintSupersededOnce  = "consumption_record_superseded_once"
	constraintPeriodCloseOnce = "consumption_period_close_once"
)
