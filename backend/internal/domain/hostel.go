package domain

import "github.com/google/uuid"

// Hostel is a physical mess site. Every rule that could plausibly differ
// between sites lives on its HostelPolicy, not as a package-level constant.
type Hostel struct {
	ID     uuid.UUID
	Name   string
	Policy HostelPolicy
}

// OptionalItem is an a-la-carte breakfast add-on with its own price.
// Priced and named per hostel — "boiled egg" at Hostel A can cost a
// different amount than at Hostel B, and the catalog itself can differ.
type OptionalItem struct {
	ID       uuid.UUID
	Name     string // e.g. "Boiled Egg", "Omelette", "Tea", "Coffee", "Milk", "Sandwich"
	PriceINR int64  // stored in paise to avoid float rounding; see Money helpers
}

// HostelPolicy is the full set of hostel-specific rules a Secretary configures.
type HostelPolicy struct {
	FlatMonthlyFeeINR      int64          // paise; baseline fee, assumes veg dinner every night
	NonVegSurchargeINR     int64          // paise; added per day a DINNER entry is logged non-veg
	DailyDeductionINR      int64          // paise; subtracted per LONG leave day
	LongLeaveThresholdDays int            // Leave.DurationDays() >= this => LONG, else SHORT
	OptionalItems          []OptionalItem // this hostel's a-la-carte breakfast catalog
	Menu                   MenuCycle      // 7-day repeating breakfast menu (informational, not billed)
}

// MenuCycle is the fixed weekly breakfast menu. It repeats every 7 days and
// carries no price — it's the included baseline, unlike OptionalItems.
type MenuCycle struct {
	// Days[0] = Monday ... Days[6] = Sunday
	Days [7]string
}

func (p HostelPolicy) FindOptionalItem(id uuid.UUID) (OptionalItem, bool) {
	for _, it := range p.OptionalItems {
		if it.ID == id {
			return it, true
		}
	}
	return OptionalItem{}, false
}
