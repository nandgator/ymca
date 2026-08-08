package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/ymca/mess-backend/internal/domain"
)

type BillingService struct {
	Hostels HostelRepo
	Members MemberRepo
	Entries EntryRepo
	Leaves  LeaveRepo
}

// ComputeMemberBill is usable both by the member (their own bill) and by
// the Secretary of their hostel (anyone on the roster).
func (s BillingService) ComputeMemberBill(ctx context.Context, actor domain.Actor, memberID uuid.UUID, year, month int) (domain.Bill, error) {
	member, err := s.Members.Get(ctx, memberID)
	if err != nil {
		return domain.Bill{}, fmt.Errorf("loading member: %w", err)
	}

	isSelf := actor.Role == domain.RoleMember && actor.ID == memberID
	isTheirSecretary := actor.Role == domain.RoleSecretary && actor.CanActOnHostel(member.HostelID)
	if !isSelf && !isTheirSecretary {
		return domain.Bill{}, domain.ErrForbidden
	}

	hostel, err := s.Hostels.Get(ctx, member.HostelID)
	if err != nil {
		return domain.Bill{}, fmt.Errorf("loading hostel policy: %w", err)
	}
	entries, err := s.Entries.ListForMemberMonth(ctx, memberID, year, month)
	if err != nil {
		return domain.Bill{}, fmt.Errorf("loading entries: %w", err)
	}
	leaves, err := s.Leaves.ListForMemberMonth(ctx, memberID, year, month)
	if err != nil {
		return domain.Bill{}, fmt.Errorf("loading leaves: %w", err)
	}

	return domain.ComputeBill(memberID, year, month, hostel.Policy, entries, leaves), nil
}

// RunHostelBilling computes every member's bill for the month — the
// Secretary's month-end replacement for manually tallying the notebook.
func (s BillingService) RunHostelBilling(ctx context.Context, actor domain.Actor, hostelID uuid.UUID, year, month int) ([]domain.Bill, error) {
	if actor.Role != domain.RoleSecretary || !actor.CanActOnHostel(hostelID) {
		return nil, domain.ErrForbidden
	}

	hostel, err := s.Hostels.Get(ctx, hostelID)
	if err != nil {
		return nil, fmt.Errorf("loading hostel policy: %w", err)
	}
	members, err := s.Members.ListByHostel(ctx, hostelID)
	if err != nil {
		return nil, fmt.Errorf("loading roster: %w", err)
	}
	entries, err := s.Entries.ListForHostelMonth(ctx, hostelID, year, month)
	if err != nil {
		return nil, fmt.Errorf("loading entries: %w", err)
	}
	leaves, err := s.Leaves.ListForHostelMonth(ctx, hostelID, year, month)
	if err != nil {
		return nil, fmt.Errorf("loading leaves: %w", err)
	}

	entriesByMember := map[uuid.UUID][]domain.Entry{}
	for _, e := range entries {
		entriesByMember[e.MemberID] = append(entriesByMember[e.MemberID], e)
	}
	leavesByMember := map[uuid.UUID][]domain.Leave{}
	for _, l := range leaves {
		leavesByMember[l.MemberID] = append(leavesByMember[l.MemberID], l)
	}

	bills := make([]domain.Bill, 0, len(members))
	for _, m := range members {
		bills = append(bills, domain.ComputeBill(m.ID, year, month, hostel.Policy, entriesByMember[m.ID], leavesByMember[m.ID]))
	}
	return bills, nil
}
