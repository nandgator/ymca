package domain

import "github.com/google/uuid"

// BillLine itemizes one component of a bill, for display/audit purposes.
type BillLine struct {
	Label      string
	AmountINR  int64 // signed: negative for deductions
}

// Bill is the fully computed month-end statement for one member.
type Bill struct {
	MemberID  uuid.UUID
	Year      int
	Month     int // 1-12
	Lines     []BillLine
	TotalINR  int64
}

// ComputeBill implements the formula recorded in CONTEXT.md:
//
//	bill = FlatMonthlyFee
//	     − (LONG leave days × DailyDeductionRate)
//	     + Σ(optional item prices across BREAKFAST entries)
//	     + (non_veg DINNER entries × NonVegSurcharge)
//
// entries and leaves must already be filtered to the member and calendar
// month being billed — this function does no date-range filtering itself,
// so it stays a pure calculation over exactly the data that applies.
func ComputeBill(memberID uuid.UUID, year, month int, policy HostelPolicy, entries []Entry, leaves []Leave) Bill {
	lines := []BillLine{
		{Label: "Flat monthly fee", AmountINR: policy.FlatMonthlyFeeINR},
	}

	longLeaveDays := countLongLeaveDays(leaves, policy.LongLeaveThresholdDays, year, month)
	if longLeaveDays > 0 {
		deduction := int64(longLeaveDays) * policy.DailyDeductionINR
		lines = append(lines, BillLine{
			Label:     "Long leave deduction (" + itoa(longLeaveDays) + " days)",
			AmountINR: -deduction,
		})
	}

	var optionalItemsTotal int64
	var nonVegDays int
	for _, e := range entries {
		if e.Date.Year != year || int(e.Date.Month) != month {
			continue
		}
		switch e.MealType {
		case MealBreakfast:
			for _, itemID := range e.OptionalItemIDs {
				if item, ok := policy.FindOptionalItem(itemID); ok {
					optionalItemsTotal += item.PriceINR
				}
			}
		case MealDinner:
			if e.NonVeg {
				nonVegDays++
			}
		}
	}

	if optionalItemsTotal > 0 {
		lines = append(lines, BillLine{Label: "Breakfast optional items", AmountINR: optionalItemsTotal})
	}
	if nonVegDays > 0 {
		surcharge := int64(nonVegDays) * policy.NonVegSurchargeINR
		lines = append(lines, BillLine{
			Label:     "Non-veg dinner surcharge (" + itoa(nonVegDays) + " days)",
			AmountINR: surcharge,
		})
	}

	var total int64
	for _, l := range lines {
		total += l.AmountINR
	}

	return Bill{MemberID: memberID, Year: year, Month: month, Lines: lines, TotalINR: total}
}

func countLongLeaveDays(leaves []Leave, threshold, year, month int) int {
	total := 0
	for _, l := range leaves {
		if l.Type(threshold) != LeaveLong {
			continue
		}
		for d := l.StartDate; !d.Time().After(l.EndDate.Time()); d = nextDay(d) {
			if d.Year == year && int(d.Month) == month {
				total++
			}
		}
	}
	return total
}

func nextDay(d Day) Day {
	return DayOf(d.Time().AddDate(0, 0, 1))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
