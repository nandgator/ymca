package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ymca/mess-backend/internal/app"
	"github.com/ymca/mess-backend/internal/domain"
)

func mountAdminRoutes(r chi.Router, d Deps) {
	r.Post("/hostels", handleCreateHostel(d))
	r.Get("/hostels", handleListHostels(d))
	r.Post("/secretaries", handleCreateSecretary(d))
}

type createHostelRequest struct {
	Name                   string    `json:"name"`
	FlatMonthlyFeePaise    int64     `json:"flat_monthly_fee_paise"`
	NonVegSurchargePaise   int64     `json:"non_veg_surcharge_paise"`
	DailyDeductionPaise    int64     `json:"daily_deduction_paise"`
	LongLeaveThresholdDays int       `json:"long_leave_threshold_days"`
	MenuDays               [7]string `json:"menu_days"`
}

type hostelDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func handleCreateHostel(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		var req createHostelRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid request body"})
			return
		}

		hostel, err := d.Admin.CreateHostel(r.Context(), actor, app.NewHostelInput{
			Name: req.Name,
			Policy: domain.HostelPolicy{
				FlatMonthlyFeeINR:      req.FlatMonthlyFeePaise,
				NonVegSurchargeINR:     req.NonVegSurchargePaise,
				DailyDeductionINR:      req.DailyDeductionPaise,
				LongLeaveThresholdDays: req.LongLeaveThresholdDays,
				Menu:                   domain.MenuCycle{Days: req.MenuDays},
			},
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, hostelDTO{ID: hostel.ID.String(), Name: hostel.Name})
	}
}

func handleListHostels(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		hostels, err := d.Admin.ListHostels(r.Context(), actor)
		if err != nil {
			writeError(w, err)
			return
		}
		out := make([]hostelDTO, 0, len(hostels))
		for _, h := range hostels {
			out = append(out, hostelDTO{ID: h.ID.String(), Name: h.Name})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type createSecretaryRequest struct {
	HostelID string  `json:"hostel_id"`
	StaffID  string  `json:"staff_id"`
	Name     string  `json:"name"`
	Email    *string `json:"email,omitempty"`
	Mobile   *string `json:"mobile,omitempty"`
}

type secretaryDTO struct {
	ID       string `json:"id"`
	HostelID string `json:"hostel_id"`
	StaffID  string `json:"staff_id"`
	Name     string `json:"name"`
}

func handleCreateSecretary(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		var req createSecretaryRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid request body"})
			return
		}
		hostelID, err := uuid.Parse(req.HostelID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid hostel_id"})
			return
		}

		sec, err := d.Admin.CreateSecretary(r.Context(), actor, app.NewSecretaryInput{
			HostelID: hostelID, StaffID: req.StaffID, Name: req.Name, Email: req.Email, Mobile: req.Mobile,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, secretaryDTO{ID: sec.ID.String(), HostelID: sec.HostelID.String(), StaffID: sec.StaffID, Name: sec.Name})
	}
}
