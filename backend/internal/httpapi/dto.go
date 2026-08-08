package httpapi

import (
	"fmt"
	"time"

	"github.com/ymca/mess-backend/internal/domain"
)

const dateLayout = "2006-01-02"

func parseDay(s string) (domain.Day, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return domain.Day{}, fmt.Errorf("invalid date %q, expected YYYY-MM-DD: %w", s, err)
	}
	return domain.DayOf(t), nil
}

func formatDay(d domain.Day) string {
	return d.Time().Format(dateLayout)
}

// billLineDTO / billDTO mirror domain.BillLine / domain.Bill for JSON
// output, formatting money as both raw paise (for exact client-side math)
// and a display string (for showing without reimplementing FormatINR).
type billLineDTO struct {
	Label       string `json:"label"`
	AmountPaise int64  `json:"amount_paise"`
	AmountINR   string `json:"amount_inr"`
}

type billDTO struct {
	MemberID  string        `json:"member_id"`
	Year      int           `json:"year"`
	Month     int           `json:"month"`
	Lines     []billLineDTO `json:"lines"`
	TotalPaise int64        `json:"total_paise"`
	TotalINR  string        `json:"total_inr"`
}

func toBillDTO(b domain.Bill) billDTO {
	lines := make([]billLineDTO, 0, len(b.Lines))
	for _, l := range b.Lines {
		lines = append(lines, billLineDTO{Label: l.Label, AmountPaise: l.AmountINR, AmountINR: domain.FormatINR(l.AmountINR)})
	}
	return billDTO{
		MemberID:   b.MemberID.String(),
		Year:       b.Year,
		Month:      b.Month,
		Lines:      lines,
		TotalPaise: b.TotalINR,
		TotalINR:   domain.FormatINR(b.TotalINR),
	}
}

type entryDTO struct {
	ID              string   `json:"id"`
	MemberID        string   `json:"member_id"`
	Date            string   `json:"date"`
	MealType        string   `json:"meal_type"`
	OptionalItemIDs []string `json:"optional_item_ids,omitempty"`
	NonVeg          bool     `json:"non_veg,omitempty"`
}

func toEntryDTO(e domain.Entry) entryDTO {
	items := make([]string, 0, len(e.OptionalItemIDs))
	for _, id := range e.OptionalItemIDs {
		items = append(items, id.String())
	}
	return entryDTO{
		ID:              e.ID.String(),
		MemberID:        e.MemberID.String(),
		Date:            formatDay(e.Date),
		MealType:        string(e.MealType),
		OptionalItemIDs: items,
		NonVeg:          e.NonVeg,
	}
}

type leaveDTO struct {
	ID        string `json:"id"`
	MemberID  string `json:"member_id"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Type      string `json:"type"` // SHORT or LONG — computed against the hostel's current threshold at read time
}

func toLeaveDTO(l domain.Leave, thresholdDays int) leaveDTO {
	return leaveDTO{
		ID:        l.ID.String(),
		MemberID:  l.MemberID.String(),
		StartDate: formatDay(l.StartDate),
		EndDate:   formatDay(l.EndDate),
		Type:      string(l.Type(thresholdDays)),
	}
}
