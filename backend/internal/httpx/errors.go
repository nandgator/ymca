// errors.go is A3.4's error shape, verbatim, and the code vocabulary this
// step uses (D7).
package httpx

import "net/http"

// Code is A3.4's stable, machine-readable error code.
type Code string

const (
	CodeInvalidRequest  Code = "invalid_request"
	CodeUnauthenticated Code = "unauthenticated"
	CodeTokenExpired    Code = "token_expired"
	CodeTenantMismatch  Code = "tenant_mismatch"
	CodeForbidden       Code = "forbidden"
	CodeNotFound        Code = "not_found"
	// CodeIdempotencyKeyReused is A3.6's 409: the same key arrived with a
	// different body. Replaying the first response would silently discard
	// the second request, which is that header's own failure mode inverted.
	CodeIdempotencyKeyReused Code = "idempotency_key_reused"
	// CodeMembershipNumberTaken and CodeAlreadyActiveMember are admission's
	// two 409s (A3.7). They are separate codes rather than one `conflict`
	// because A3.4 writes the message for a developer — a client must not
	// parse it — and the two call for different client behaviour: choose
	// another number, versus this person is already a member and the
	// existing membership is the thing to show.
	CodeMembershipNumberTaken Code = "membership_number_taken"
	CodeAlreadyActiveMember   Code = "already_active_member"
	CodeInternal              Code = "internal"

	// A3.10 and A3.11. Each is a state the caller can act on, which is the
	// reason none of them is a bare `conflict`.
	CodeAlreadyRecorded        Code = "already_recorded"
	CodeRecordAlreadyCorrected Code = "record_already_corrected"
	CodeWindowClosed           Code = "consumption_window_closed"
	CodePeriodClosed           Code = "period_closed"
	CodePeriodAlreadyClosed    Code = "period_already_closed"
	CodeObligationOverlaps     Code = "obligation_overlaps"
	CodeOptionCodeTaken        Code = "option_code_taken"
	CodeRateOverlaps           Code = "rate_overlaps"
	CodeNoRate                 Code = "no_rate"
	CodeCurrencyMismatch       Code = "currency_mismatch"
	CodeInvoiceWrongState      Code = "invoice_wrong_state"
	CodeInvoiceEmpty           Code = "invoice_empty"
	CodePaymentExceedsBalance  Code = "payment_exceeds_balance"
	CodeCreditExceedsInvoice   Code = "credit_exceeds_invoice"
	CodeRecordModeNotAllowed   Code = "record_mode_not_allowed"
	CodeMembershipWrongState   Code = "membership_wrong_state"
)

// httpStatus is D7's HTTP status for each code. tenant_mismatch and
// forbidden are both 403 — ADR-105: refusing a tenant the caller named
// themselves discloses nothing, so it does not fall back to 401.
var httpStatus = map[Code]int{
	CodeInvalidRequest:        http.StatusBadRequest,
	CodeUnauthenticated:       http.StatusUnauthorized,
	CodeTokenExpired:          http.StatusUnauthorized,
	CodeTenantMismatch:        http.StatusForbidden,
	CodeForbidden:             http.StatusForbidden,
	CodeNotFound:              http.StatusNotFound,
	CodeIdempotencyKeyReused:  http.StatusConflict,
	CodeMembershipNumberTaken: http.StatusConflict,
	CodeAlreadyActiveMember:   http.StatusConflict,
	CodeInternal:              http.StatusInternalServerError,

	CodeAlreadyRecorded:        http.StatusConflict,
	CodeRecordAlreadyCorrected: http.StatusConflict,
	CodeWindowClosed:           http.StatusConflict,
	CodePeriodClosed:           http.StatusConflict,
	CodePeriodAlreadyClosed:    http.StatusConflict,
	CodeObligationOverlaps:     http.StatusConflict,
	CodeOptionCodeTaken:        http.StatusConflict,
	CodeRateOverlaps:           http.StatusConflict,
	CodeNoRate:                 http.StatusConflict,
	CodeCurrencyMismatch:       http.StatusConflict,
	CodeInvoiceWrongState:      http.StatusConflict,
	CodeInvoiceEmpty:           http.StatusConflict,
	CodePaymentExceedsBalance:  http.StatusConflict,
	CodeCreditExceedsInvoice:   http.StatusConflict,
	CodeRecordModeNotAllowed:   http.StatusUnprocessableEntity,
	CodeMembershipWrongState:   http.StatusConflict,
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteError writes A3.4's shape:
//
//	{ "error": { "code": "tenant_mismatch", "message": "..." } }
//
// message is for a developer (A3.4): no SQL, no stack, no identifier the
// caller could not already name.
func WriteError(w http.ResponseWriter, code Code, message string) {
	status, ok := httpStatus[code]
	if !ok {
		status = http.StatusInternalServerError
	}
	WriteJSON(w, status, errorBody{Error: errorDetail{Code: string(code), Message: message}})
}
