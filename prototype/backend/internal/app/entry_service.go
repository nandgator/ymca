package app

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ymca/mess-backend/internal/domain"
)

type EntryService struct {
	Hostels HostelRepo
	Members MemberRepo
	Entries EntryRepo
	Leaves  LeaveRepo
	Now     func() time.Time // injected for testability; defaults to time.Now in wiring
}

type SubmitEntryInput struct {
	MemberID        uuid.UUID
	Date            domain.Day
	MealType        domain.MealType
	OptionalItemIDs []uuid.UUID // BREAKFAST only
	NonVeg          bool        // DINNER only
}

// SubmitEntry handles both create and same-day edit — the unique
// (member_id, date, meal_type) constraint plus domain.NewEntry's same-day
// check together make this safe to call repeatedly before midnight and
// impossible to call successfully after.
func (s EntryService) SubmitEntry(ctx context.Context, actor domain.Actor, in SubmitEntryInput) (domain.Entry, error) {
	if actor.Role != domain.RoleMember || actor.ID != in.MemberID {
		return domain.Entry{}, domain.ErrForbidden
	}

	member, err := s.Members.Get(ctx, in.MemberID)
	if err != nil {
		return domain.Entry{}, fmt.Errorf("loading member: %w", err)
	}

	_, onLeave, err := s.Leaves.CoveringDate(ctx, member.ID, in.Date)
	if err != nil {
		return domain.Entry{}, fmt.Errorf("checking leave: %w", err)
	}
	if onLeave {
		return domain.Entry{}, domain.ErrEntryOnLeaveDay
	}

	hostel, err := s.Hostels.Get(ctx, member.HostelID)
	if err != nil {
		return domain.Entry{}, fmt.Errorf("loading hostel policy: %w", err)
	}

	now := s.now()
	entry, err := domain.NewEntry(member.ID, member.HostelID, in.Date, in.MealType, in.OptionalItemIDs, in.NonVeg, hostel.Policy, now)
	if err != nil {
		return domain.Entry{}, err
	}

	if err := s.Entries.Upsert(ctx, entry); err != nil {
		return domain.Entry{}, fmt.Errorf("saving entry: %w", err)
	}
	return entry, nil
}

func (s EntryService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// ListMine returns a member's own entries for a calendar month, for the
// "my history" screen.
func (s EntryService) ListMine(ctx context.Context, actor domain.Actor, year, month int) ([]domain.Entry, error) {
	if actor.Role != domain.RoleMember {
		return nil, domain.ErrForbidden
	}
	return s.Entries.ListForMemberMonth(ctx, actor.ID, year, month)
}

// ListForHostelDate is the Secretary's daily roll — how many members ate
// each meal today, for kitchen/reconciliation purposes.
func (s EntryService) ListForHostelDate(ctx context.Context, actor domain.Actor, hostelID uuid.UUID, date domain.Day) ([]domain.Entry, error) {
	if actor.Role != domain.RoleSecretary || !actor.CanActOnHostel(hostelID) {
		return nil, domain.ErrForbidden
	}
	return s.Entries.ListForHostelDate(ctx, hostelID, date)
}

// ListForHostelMonth is the Secretary's raw-entries month view (distinct
// from the computed month-end bill, which lives in BillingService).
func (s EntryService) ListForHostelMonth(ctx context.Context, actor domain.Actor, hostelID uuid.UUID, year, month int) ([]domain.Entry, error) {
	if actor.Role != domain.RoleSecretary || !actor.CanActOnHostel(hostelID) {
		return nil, domain.ErrForbidden
	}
	return s.Entries.ListForHostelMonth(ctx, hostelID, year, month)
}
