package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func testPolicy() HostelPolicy {
	eggID := uuid.New()
	teaID := uuid.New()
	return HostelPolicy{
		FlatMonthlyFeeINR:      600000, // ₹6000.00
		NonVegSurchargeINR:     5000,   // ₹50.00
		DailyDeductionINR:      15000,  // ₹150.00/day
		LongLeaveThresholdDays: 7,
		OptionalItems: []OptionalItem{
			{ID: eggID, Name: "Boiled Egg", PriceINR: 1000},
			{ID: teaID, Name: "Tea", PriceINR: 500},
		},
	}
}

func day(y int, m int, d int) Day { return Day{Year: y, Month: time.Month(m), Day: d} }

func TestComputeBill_FlatFeeOnly_NoEntriesNoLeave(t *testing.T) {
	policy := testPolicy()
	memberID := uuid.New()

	bill := ComputeBill(memberID, 2026, 8, policy, nil, nil)

	if bill.TotalINR != policy.FlatMonthlyFeeINR {
		t.Fatalf("expected bill to equal flat fee with no entries/leave, got %d want %d", bill.TotalINR, policy.FlatMonthlyFeeINR)
	}
}

func TestComputeBill_LongLeaveDeductsOnlyDaysWithinBilledMonth(t *testing.T) {
	policy := testPolicy()
	memberID := uuid.New()

	// A 10-day leave straddling July 28 -> Aug 6. Threshold is 7, so it's LONG.
	// Only the Aug days (6 of them: Aug 1-6) should count toward August's bill.
	leave, err := NewLeave(memberID, uuid.New(), day(2026, 7, 28), day(2026, 8, 6))
	if err != nil {
		t.Fatalf("unexpected error constructing leave: %v", err)
	}
	if leave.Type(policy.LongLeaveThresholdDays) != LeaveLong {
		t.Fatalf("expected 10-day leave with threshold 7 to be LONG")
	}

	bill := ComputeBill(memberID, 2026, 8, policy, nil, []Leave{leave})

	wantDeduction := int64(6) * policy.DailyDeductionINR
	wantTotal := policy.FlatMonthlyFeeINR - wantDeduction
	if bill.TotalINR != wantTotal {
		t.Fatalf("got total %d, want %d (flat fee %d minus %d days deduction)", bill.TotalINR, wantTotal, policy.FlatMonthlyFeeINR, 6)
	}
}

func TestComputeBill_ShortLeaveDoesNotDeduct(t *testing.T) {
	policy := testPolicy()
	memberID := uuid.New()

	// 3-day leave, threshold is 7 -> SHORT -> no deduction, still billed as present.
	leave, err := NewLeave(memberID, uuid.New(), day(2026, 8, 10), day(2026, 8, 12))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if leave.Type(policy.LongLeaveThresholdDays) != LeaveShort {
		t.Fatalf("expected 3-day leave with threshold 7 to be SHORT")
	}

	bill := ComputeBill(memberID, 2026, 8, policy, nil, []Leave{leave})

	if bill.TotalINR != policy.FlatMonthlyFeeINR {
		t.Fatalf("short leave must not change the bill, got %d want %d", bill.TotalINR, policy.FlatMonthlyFeeINR)
	}
}

func TestComputeBill_BreakfastOptionalItemsSum(t *testing.T) {
	policy := testPolicy()
	memberID := uuid.New()
	hostelID := uuid.New()
	eggID := policy.OptionalItems[0].ID
	teaID := policy.OptionalItems[1].ID

	entries := []Entry{
		{MemberID: memberID, HostelID: hostelID, Date: day(2026, 8, 1), MealType: MealBreakfast, OptionalItemIDs: []uuid.UUID{eggID, teaID}},
		{MemberID: memberID, HostelID: hostelID, Date: day(2026, 8, 2), MealType: MealBreakfast, OptionalItemIDs: []uuid.UUID{teaID}},
	}

	bill := ComputeBill(memberID, 2026, 8, policy, entries, nil)

	wantAddOn := int64(1000+500) + int64(500)
	wantTotal := policy.FlatMonthlyFeeINR + wantAddOn
	if bill.TotalINR != wantTotal {
		t.Fatalf("got %d want %d", bill.TotalINR, wantTotal)
	}
}

func TestComputeBill_NonVegDinnerSurcharge(t *testing.T) {
	policy := testPolicy()
	memberID := uuid.New()
	hostelID := uuid.New()

	entries := []Entry{
		{MemberID: memberID, HostelID: hostelID, Date: day(2026, 8, 1), MealType: MealDinner, NonVeg: true},
		{MemberID: memberID, HostelID: hostelID, Date: day(2026, 8, 2), MealType: MealDinner, NonVeg: false},
		{MemberID: memberID, HostelID: hostelID, Date: day(2026, 8, 3), MealType: MealDinner, NonVeg: true},
	}

	bill := ComputeBill(memberID, 2026, 8, policy, entries, nil)

	wantTotal := policy.FlatMonthlyFeeINR + int64(2)*policy.NonVegSurchargeINR
	if bill.TotalINR != wantTotal {
		t.Fatalf("got %d want %d (2 non-veg days x surcharge)", bill.TotalINR, wantTotal)
	}
}

func TestComputeBill_EntriesOutsideBilledMonthAreIgnored(t *testing.T) {
	policy := testPolicy()
	memberID := uuid.New()
	hostelID := uuid.New()
	eggID := policy.OptionalItems[0].ID

	entries := []Entry{
		// July entry - must not leak into August's bill.
		{MemberID: memberID, HostelID: hostelID, Date: day(2026, 7, 31), MealType: MealBreakfast, OptionalItemIDs: []uuid.UUID{eggID}},
	}

	bill := ComputeBill(memberID, 2026, 8, policy, entries, nil)

	if bill.TotalINR != policy.FlatMonthlyFeeINR {
		t.Fatalf("entry from a different month must not affect this month's bill, got %d want %d", bill.TotalINR, policy.FlatMonthlyFeeINR)
	}
}

func TestComputeBill_CombinedScenario(t *testing.T) {
	policy := testPolicy()
	memberID := uuid.New()
	hostelID := uuid.New()
	eggID := policy.OptionalItems[0].ID

	leave, _ := NewLeave(memberID, hostelID, day(2026, 8, 15), day(2026, 8, 22)) // 8 days -> LONG
	entries := []Entry{
		{MemberID: memberID, HostelID: hostelID, Date: day(2026, 8, 1), MealType: MealBreakfast, OptionalItemIDs: []uuid.UUID{eggID}},
		{MemberID: memberID, HostelID: hostelID, Date: day(2026, 8, 1), MealType: MealDinner, NonVeg: true},
	}

	bill := ComputeBill(memberID, 2026, 8, policy, entries, []Leave{leave})

	wantTotal := policy.FlatMonthlyFeeINR -
		int64(8)*policy.DailyDeductionINR +
		int64(1000) +
		policy.NonVegSurchargeINR

	if bill.TotalINR != wantTotal {
		t.Fatalf("got %d want %d", bill.TotalINR, wantTotal)
	}
}
