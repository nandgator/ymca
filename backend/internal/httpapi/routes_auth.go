package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ymca/mess-backend/internal/app"
	"github.com/ymca/mess-backend/internal/domain"
)

func mountAuthRoutes(r chi.Router, d Deps) {
	r.Post("/otp/request", handleRequestOTP(d))
	r.Post("/otp/verify", handleVerifyOTP(d))
}

type requestOTPRequest struct {
	Role    string `json:"role"`     // MEMBER | SECRETARY | CENTRAL_ADMIN
	LoginID string `json:"login_id"` // MemberID or StaffID, as provisioned offline
	Channel string `json:"channel"`  // EMAIL | SMS
}

func handleRequestOTP(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req requestOTPRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid request body"})
			return
		}

		role, err := parseRole(req.Role)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
			return
		}

		if err := d.Auth.RequestOTP(r.Context(), role, req.LoginID, app.OTPChannel(req.Channel)); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "otp_sent"})
	}
}

type verifyOTPRequest struct {
	Role    string `json:"role"`
	LoginID string `json:"login_id"`
	Code    string `json:"code"`
}

type verifyOTPResponse struct {
	Token    string  `json:"token"`
	Role     string  `json:"role"`
	ActorID  string  `json:"actor_id"`
	HostelID *string `json:"hostel_id,omitempty"`
}

func handleVerifyOTP(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req verifyOTPRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid request body"})
			return
		}

		role, err := parseRole(req.Role)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
			return
		}

		token, actor, err := d.Auth.VerifyOTP(r.Context(), role, req.LoginID, req.Code)
		if err != nil {
			writeError(w, err)
			return
		}

		resp := verifyOTPResponse{Token: token, Role: string(actor.Role), ActorID: actor.ID.String()}
		if actor.HostelID != nil {
			hid := actor.HostelID.String()
			resp.HostelID = &hid
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func parseRole(s string) (domain.Role, error) {
	switch domain.Role(s) {
	case domain.RoleMember, domain.RoleSecretary, domain.RoleCentralAdmin:
		return domain.Role(s), nil
	default:
		return "", errors.New("role must be one of MEMBER, SECRETARY, CENTRAL_ADMIN")
	}
}
