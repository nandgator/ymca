package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ymca/mess-backend/internal/app"
	"github.com/ymca/mess-backend/internal/domain"
)

func mountSecretaryRoutes(r chi.Router, d Deps) {
	r.Post("/members", handleAddMember(d))
	r.Get("/members", handleListRoster(d))
	r.Get("/policy", handleGetPolicy(d))
	r.Put("/policy", handleUpdatePolicy(d))
	r.Post("/optional-items", handleAddOptionalItem(d))
	r.Delete("/optional-items/{itemID}", handleDeactivateOptionalItem(d))
	r.Get("/entries", handleHostelEntries(d))
	r.Get("/billing", handleRunBilling(d))
}

type addMemberRequest struct {
	MemberID string  `json:"member_id"`
	Name     string  `json:"name"`
	Email    *string `json:"email,omitempty"`
	Mobile   *string `json:"mobile,omitempty"`
}

type memberDTO struct {
	ID       string  `json:"id"`
	MemberID string  `json:"member_id"`
	Name     string  `json:"name"`
	Email    *string `json:"email,omitempty"`
	Mobile   *string `json:"mobile,omitempty"`
}

func toMemberDTO(m domain.Member) memberDTO {
	return memberDTO{ID: m.ID.String(), MemberID: m.MemberID, Name: m.Name, Email: m.Email, Mobile: m.Mobile}
}

func handleAddMember(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		var req addMemberRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid request body"})
			return
		}
		m, err := d.Secretary.AddMember(r.Context(), actor, app.NewMemberInput{
			MemberID: req.MemberID, Name: req.Name, Email: req.Email, Mobile: req.Mobile,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toMemberDTO(m))
	}
}

func handleListRoster(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		members, err := d.Secretary.ListRoster(r.Context(), actor)
		if err != nil {
			writeError(w, err)
			return
		}
		out := make([]memberDTO, 0, len(members))
		for _, m := range members {
			out = append(out, toMemberDTO(m))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type optionalItemDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PricePaise  int64  `json:"price_paise"`
	PriceINR    string `json:"price_inr"`
}

type policyDTO struct {
	FlatMonthlyFeePaise    int64             `json:"flat_monthly_fee_paise"`
	NonVegSurchargePaise   int64             `json:"non_veg_surcharge_paise"`
	DailyDeductionPaise    int64             `json:"daily_deduction_paise"`
	LongLeaveThresholdDays int               `json:"long_leave_threshold_days"`
	MenuDays               [7]string         `json:"menu_days"` // index 0 = Monday
	OptionalItems          []optionalItemDTO `json:"optional_items"`
}

func toPolicyDTO(p domain.HostelPolicy) policyDTO {
	items := make([]optionalItemDTO, 0, len(p.OptionalItems))
	for _, it := range p.OptionalItems {
		items = append(items, optionalItemDTO{ID: it.ID.String(), Name: it.Name, PricePaise: it.PriceINR, PriceINR: domain.FormatINR(it.PriceINR)})
	}
	return policyDTO{
		FlatMonthlyFeePaise:    p.FlatMonthlyFeeINR,
		NonVegSurchargePaise:   p.NonVegSurchargeINR,
		DailyDeductionPaise:    p.DailyDeductionINR,
		LongLeaveThresholdDays: p.LongLeaveThresholdDays,
		MenuDays:               p.Menu.Days,
		OptionalItems:          items,
	}
}

func handleGetPolicy(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		policy, err := d.Secretary.GetPolicy(r.Context(), actor)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toPolicyDTO(policy))
	}
}

type updatePolicyRequest struct {
	FlatMonthlyFeePaise    int64     `json:"flat_monthly_fee_paise"`
	NonVegSurchargePaise   int64     `json:"non_veg_surcharge_paise"`
	DailyDeductionPaise    int64     `json:"daily_deduction_paise"`
	LongLeaveThresholdDays int       `json:"long_leave_threshold_days"`
	MenuDays               [7]string `json:"menu_days"`
}

func handleUpdatePolicy(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		var req updatePolicyRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid request body"})
			return
		}

		// Optional items aren't part of this payload (managed via their own
		// endpoints) — preserve whatever's currently on file rather than
		// wiping the catalog on every policy edit.
		current, err := d.Secretary.GetPolicy(r.Context(), actor)
		if err != nil {
			writeError(w, err)
			return
		}

		policy := domain.HostelPolicy{
			FlatMonthlyFeeINR:      req.FlatMonthlyFeePaise,
			NonVegSurchargeINR:     req.NonVegSurchargePaise,
			DailyDeductionINR:      req.DailyDeductionPaise,
			LongLeaveThresholdDays: req.LongLeaveThresholdDays,
			Menu:                   domain.MenuCycle{Days: req.MenuDays},
			OptionalItems:          current.OptionalItems,
		}
		if err := d.Secretary.UpdatePolicy(r.Context(), actor, policy); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toPolicyDTO(policy))
	}
}

type addOptionalItemRequest struct {
	Name       string `json:"name"`
	PricePaise int64  `json:"price_paise"`
}

func handleAddOptionalItem(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		var req addOptionalItemRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid request body"})
			return
		}
		item, err := d.Secretary.AddOptionalItem(r.Context(), actor, req.Name, req.PricePaise)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, optionalItemDTO{ID: item.ID.String(), Name: item.Name, PricePaise: item.PriceINR, PriceINR: domain.FormatINR(item.PriceINR)})
	}
}

func handleDeactivateOptionalItem(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		id, err := uuid.Parse(chi.URLParam(r, "itemID"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid item id"})
			return
		}
		if err := d.Secretary.DeactivateOptionalItem(r.Context(), actor, id); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusNoContent, nil)
	}
}

// handleHostelEntries supports either ?date=YYYY-MM-DD (the daily kitchen
// roll) or ?year=&month= (the full-month view) — exactly one must be given.
func handleHostelEntries(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		if actor.HostelID == nil {
			writeError(w, domain.ErrForbidden)
			return
		}

		if dateStr := r.URL.Query().Get("date"); dateStr != "" {
			date, err := parseDay(dateStr)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
				return
			}
			entries, err := d.Entries.ListForHostelDate(r.Context(), actor, *actor.HostelID, date)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, mapEntryDTOs(entries))
			return
		}

		year, month, err := yearMonthFromQuery(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
			return
		}
		entries, err := d.Entries.ListForHostelMonth(r.Context(), actor, *actor.HostelID, year, month)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, mapEntryDTOs(entries))
	}
}

func mapEntryDTOs(entries []domain.Entry) []entryDTO {
	out := make([]entryDTO, 0, len(entries))
	for _, e := range entries {
		out = append(out, toEntryDTO(e))
	}
	return out
}

func handleRunBilling(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		if actor.HostelID == nil {
			writeError(w, domain.ErrForbidden)
			return
		}
		year, month, err := yearMonthFromQuery(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
			return
		}
		bills, err := d.Billing.RunHostelBilling(r.Context(), actor, *actor.HostelID, year, month)
		if err != nil {
			writeError(w, err)
			return
		}
		out := make([]billDTO, 0, len(bills))
		for _, b := range bills {
			out = append(out, toBillDTO(b))
		}
		writeJSON(w, http.StatusOK, out)
	}
}
