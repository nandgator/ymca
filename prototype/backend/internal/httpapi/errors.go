package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/ymca/mess-backend/internal/app"
	"github.com/ymca/mess-backend/internal/domain"
)

type errorBody struct {
	Error string `json:"error"`
}

// writeError maps known domain/app sentinel errors to the right HTTP
// status and a stable JSON body. Anything unrecognized becomes a 500 with
// a generic message — the real error is logged server-side, never leaked
// to the client (it may contain a raw DB error string).
func writeError(w http.ResponseWriter, err error) {
	status, message := classify(err)
	if status == http.StatusInternalServerError {
		log.Printf("internal error: %v", err)
		message = "internal server error"
	}
	writeJSON(w, status, errorBody{Error: message})
}

func classify(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrNotInHostel):
		return http.StatusForbidden, err.Error()
	case errors.Is(err, app.ErrInvalidOrExpired):
		return http.StatusUnauthorized, err.Error()
	case errors.Is(err, app.ErrUnknownLoginID), errors.Is(err, app.ErrNoSuchChannel):
		// Deliberately vague to the client — see AuthService.RequestOTP's
		// doc comment on why we don't distinguish these over the wire.
		return http.StatusBadRequest, "unable to process that login"
	case errors.Is(err, domain.ErrEntryLocked),
		errors.Is(err, domain.ErrEntryOnLeaveDay),
		errors.Is(err, domain.ErrInvalidMealType),
		errors.Is(err, domain.ErrUnknownOptionalItem),
		errors.Is(err, domain.ErrOptionalItemOnDinner),
		errors.Is(err, domain.ErrLeaveDatesInvalid):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, domain.ErrLeaveOverlaps):
		return http.StatusConflict, err.Error()
	default:
		return http.StatusInternalServerError, "internal server error"
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("writing json response: %v", err)
	}
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
