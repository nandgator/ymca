// Package finance owns invoices, charges, payments and the price of a unit of
// consumption (05.8, A3.11).
//
// It composes from what other contexts publish and never the reverse:
// Consumption reports units and vocabulary, and does not import this package
// (05.10.8). The handler in cmd/api is the one place the two meet, because
// there is no event bus yet (B6).
//
// Transport-neutral (REVIEW.md B7): the operator CLI that will issue invoices
// calls these functions.
package finance

import "errors"

var (
	// ErrNotFound is 404: absent, or not the caller's to know about (A3.3).
	ErrNotFound = errors.New("finance: not found")

	// 409 no_rate: units to bill and nothing to price them with. Refused,
	// never a silent zero: an unpriced close would mark the period closed
	// and bill nothing, and the exclusion constraint would then forbid
	// closing it again.
	ErrNoRate = errors.New("finance: no rate is in force for that consumption type")

	// 409 rate_overlaps.
	ErrRateOverlaps = errors.New("finance: a rate already covers those days")

	// 409 currency_mismatch.
	ErrCurrencyMismatch = errors.New("finance: two currencies where one is required")

	// 409 invoice_wrong_state.
	ErrInvoiceState = errors.New("finance: the invoice is not in the state this needs")

	// 409 invoice_empty.
	ErrInvoiceEmpty = errors.New("finance: the invoice has nothing to issue")

	// 409 payment_exceeds_balance.
	ErrExceedsBalance = errors.New("finance: the payment is more than is owed")

	// 409 credit_exceeds_invoice.
	ErrExceedsInvoice = errors.New("finance: the credit is more than the invoice still asks for")

	// invalid_request: a field is not one this API accepts.
	ErrInvalid = errors.New("finance: invalid request")
)

// SQLSTATEs this package maps.
const exclusionViolation = "23P01"
