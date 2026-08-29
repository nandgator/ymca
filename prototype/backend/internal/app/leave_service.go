package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/ymca/mess-backend/internal/domain"
)

type LeaveService struct {
	Members MemberRepo
	Leaves  LeaveRepo
}

// RegisterLeave has no approval step by design (see CONTEXT.md) — a member
// registering a leave is sufficient. The only rejection is an overlap with
// a period they've already registered.
func (s LeaveService) RegisterLeave(ctx context.Context, actor domain.Actor, memberID uuid.UUID, start, end domain.Day) (domain.Leave, error) {
	if actor.Role != domain.RoleMember || actor.ID != memberID {
		return domain.Leave{}, domain.ErrForbidden
	}

	member, err := s.Members.Get(ctx, memberID)
	if err != nil {
		return domain.Leave{}, fmt.Errorf("loading member: %w", err)
	}

	candidate, err := domain.NewLeave(member.ID, member.HostelID, start, end)
	if err != nil {
		return domain.Leave{}, err
	}

	existing, err := s.Leaves.ListForMember(ctx, member.ID)
	if err != nil {
		return domain.Leave{}, fmt.Errorf("loading existing leaves: %w", err)
	}
	for _, l := range existing {
		if candidate.Overlaps(l) {
			return domain.Leave{}, domain.ErrLeaveOverlaps
		}
	}

	if err := s.Leaves.Create(ctx, candidate); err != nil {
		return domain.Leave{}, fmt.Errorf("saving leave: %w", err)
	}
	return candidate, nil
}

func (s LeaveService) ListMine(ctx context.Context, actor domain.Actor) ([]domain.Leave, error) {
	if actor.Role != domain.RoleMember {
		return nil, domain.ErrForbidden
	}
	return s.Leaves.ListForMember(ctx, actor.ID)
}
