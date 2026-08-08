package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ymca/mess-backend/internal/app"
	"github.com/ymca/mess-backend/internal/domain"
)

func mountMemberRoutes(r chi.Router, d Deps) {
	r.Post("/entries", handleSubmitEntry(d))
	r.Get("/entries", handleListMyEntries(d))
	r.Post("/leaves", handleRegisterLeave(d))
	r.Get("/leaves", handleListMyLeaves(d))
	r.Get("/bill", handleMyBill(d))
	r.Get("/policy", handleMyPolicy(d))
}

// handleMyPolicy is read-only: a member needs to see the hostel's optional
// item catalog and weekly menu to fill out an entry form, but has no
// authority to change any of it (that's Secretary-only, see routes_secretary.go).
func handleMyPolicy(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		if actor.Role != domain.RoleMember || actor.HostelID == nil {
			writeError(w, domain.ErrForbidden)
			return
		}
		hostel, err := d.Hostels.Get(r.Context(), *actor.HostelID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toPolicyDTO(hostel.Policy))
	}
}

type submitEntryRequest struct {
	Date            string   `json:"date"` // YYYY-MM-DD, must be today
	MealType        string   `json:"meal_type"`
	OptionalItemIDs []string `json:"optional_item_ids,omitempty"`
	NonVeg          bool     `json:"non_veg,omitempty"`
}

func handleSubmitEntry(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())

		var req submitEntryRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid request body"})
			return
		}
		date, err := parseDay(req.Date)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
			return
		}
		itemIDs := make([]uuid.UUID, 0, len(req.OptionalItemIDs))
		for _, s := range req.OptionalItemIDs {
			id, err := uuid.Parse(s)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid optional_item_id: " + s})
				return
			}
			itemIDs = append(itemIDs, id)
		}

		entry, err := d.Entries.SubmitEntry(r.Context(), actor, app.SubmitEntryInput{
			MemberID:        actor.ID,
			Date:            date,
			MealType:        domain.MealType(req.MealType),
			OptionalItemIDs: itemIDs,
			NonVeg:          req.NonVeg,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toEntryDTO(entry))
	}
}

func handleListMyEntries(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		year, month, err := yearMonthFromQuery(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
			return
		}

		entries, err := d.Entries.ListMine(r.Context(), actor, year, month)
		if err != nil {
			writeError(w, err)
			return
		}
		out := make([]entryDTO, 0, len(entries))
		for _, e := range entries {
			out = append(out, toEntryDTO(e))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

type registerLeaveRequest struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

func handleRegisterLeave(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())

		var req registerLeaveRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid request body"})
			return
		}
		start, err := parseDay(req.StartDate)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
			return
		}
		end, err := parseDay(req.EndDate)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
			return
		}

		leave, err := d.Leaves.RegisterLeave(r.Context(), actor, actor.ID, start, end)
		if err != nil {
			writeError(w, err)
			return
		}

		threshold, err := thresholdFor(r, d, actor)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toLeaveDTO(leave, threshold))
	}
}

func handleListMyLeaves(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())

		leaves, err := d.Leaves.ListMine(r.Context(), actor)
		if err != nil {
			writeError(w, err)
			return
		}
		threshold, err := thresholdFor(r, d, actor)
		if err != nil {
			writeError(w, err)
			return
		}
		out := make([]leaveDTO, 0, len(leaves))
		for _, l := range leaves {
			out = append(out, toLeaveDTO(l, threshold))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func handleMyBill(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		year, month, err := yearMonthFromQuery(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
			return
		}

		bill, err := d.Billing.ComputeMemberBill(r.Context(), actor, actor.ID, year, month)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toBillDTO(bill))
	}
}

// thresholdFor looks up the acting member's hostel policy purely to render
// SHORT/LONG on leave DTOs — a display concern, not a business decision
// (the decision itself already happened in domain.NewLeave/ComputeBill).
func thresholdFor(r *http.Request, d Deps, actor domain.Actor) (int, error) {
	if actor.HostelID == nil {
		return 0, domain.ErrForbidden
	}
	hostel, err := d.Hostels.Get(r.Context(), *actor.HostelID)
	if err != nil {
		return 0, err
	}
	return hostel.Policy.LongLeaveThresholdDays, nil
}

func yearMonthFromQuery(r *http.Request) (int, int, error) {
	year, err := strconv.Atoi(r.URL.Query().Get("year"))
	if err != nil {
		return 0, 0, errBadYearMonth
	}
	month, err := strconv.Atoi(r.URL.Query().Get("month"))
	if err != nil || month < 1 || month > 12 {
		return 0, 0, errBadYearMonth
	}
	return year, month, nil
}

var errBadYearMonth = &queryError{"year and month query params are required, e.g. ?year=2026&month=8"}

type queryError struct{ msg string }

func (e *queryError) Error() string { return e.msg }
