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
	CodeInternal             Code = "internal"
)

// httpStatus is D7's HTTP status for each code. tenant_mismatch and
// forbidden are both 403 — ADR-105: refusing a tenant the caller named
// themselves discloses nothing, so it does not fall back to 401.
var httpStatus = map[Code]int{
	CodeInvalidRequest:       http.StatusBadRequest,
	CodeUnauthenticated:      http.StatusUnauthorized,
	CodeTokenExpired:         http.StatusUnauthorized,
	CodeTenantMismatch:       http.StatusForbidden,
	CodeForbidden:            http.StatusForbidden,
	CodeNotFound:             http.StatusNotFound,
	CodeIdempotencyKeyReused: http.StatusConflict,
	CodeInternal:             http.StatusInternalServerError,
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
