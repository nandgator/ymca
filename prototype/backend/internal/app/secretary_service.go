package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/ymca/mess-backend/internal/domain"
)

type SecretaryService struct {
	Hostels HostelRepo
	Members MemberRepo
}

type NewMemberInput struct {
	MemberID string
	Name     string
	Email    *string
	Mobile   *string
}

// AddMember provisions a member offline, on the Secretary's behalf — this
// is how anyone ever gets into the system, since nothing self-registers.
func (s SecretaryService) AddMember(ctx context.Context, actor domain.Actor, in NewMemberInput) (domain.Member, error) {
	if actor.Role != domain.RoleSecretary || actor.HostelID == nil {
		return domain.Member{}, domain.ErrForbidden
	}
	if in.Email == nil && in.Mobile == nil {
		return domain.Member{}, fmt.Errorf("member needs at least one of email or mobile for OTP login")
	}

	m := domain.Member{
		ID:       uuid.New(),
		HostelID: *actor.HostelID,
		MemberID: in.MemberID,
		Name:     in.Name,
		Email:    in.Email,
		Mobile:   in.Mobile,
	}
	if err := s.Members.Create(ctx, m); err != nil {
		return domain.Member{}, fmt.Errorf("creating member: %w", err)
	}
	return m, nil
}

func (s SecretaryService) ListRoster(ctx context.Context, actor domain.Actor) ([]domain.Member, error) {
	if actor.Role != domain.RoleSecretary || actor.HostelID == nil {
		return nil, domain.ErrForbidden
	}
	return s.Members.ListByHostel(ctx, *actor.HostelID)
}

func (s SecretaryService) GetPolicy(ctx context.Context, actor domain.Actor) (domain.HostelPolicy, error) {
	if actor.Role != domain.RoleSecretary || actor.HostelID == nil {
		return domain.HostelPolicy{}, domain.ErrForbidden
	}
	hostel, err := s.Hostels.Get(ctx, *actor.HostelID)
	if err != nil {
		return domain.HostelPolicy{}, fmt.Errorf("loading policy: %w", err)
	}
	return hostel.Policy, nil
}

// UpdatePolicy replaces the whole policy (fee, surcharge, deduction,
// threshold, menu) in one call — the Secretary owns all of it, per
// CONTEXT.md's "everything varies per hostel" rule. Optional items are
// managed separately (AddOptionalItem/DeactivateOptionalItem) since
// they're an open-ended list, not a fixed set of fields.
func (s SecretaryService) UpdatePolicy(ctx context.Context, actor domain.Actor, policy domain.HostelPolicy) error {
	if actor.Role != domain.RoleSecretary || actor.HostelID == nil {
		return domain.ErrForbidden
	}
	if err := s.Hostels.UpdatePolicy(ctx, *actor.HostelID, policy); err != nil {
		return fmt.Errorf("updating policy: %w", err)
	}
	return nil
}

func (s SecretaryService) AddOptionalItem(ctx context.Context, actor domain.Actor, name string, priceINR int64) (domain.OptionalItem, error) {
	if actor.Role != domain.RoleSecretary || actor.HostelID == nil {
		return domain.OptionalItem{}, domain.ErrForbidden
	}
	item := domain.OptionalItem{ID: uuid.New(), Name: name, PriceINR: priceINR}
	if err := s.Hostels.AddOptionalItem(ctx, *actor.HostelID, item); err != nil {
		return domain.OptionalItem{}, fmt.Errorf("adding optional item: %w", err)
	}
	return item, nil
}

func (s SecretaryService) DeactivateOptionalItem(ctx context.Context, actor domain.Actor, itemID uuid.UUID) error {
	if actor.Role != domain.RoleSecretary || actor.HostelID == nil {
		return domain.ErrForbidden
	}
	if err := s.Hostels.DeactivateOptionalItem(ctx, *actor.HostelID, itemID); err != nil {
		return fmt.Errorf("deactivating optional item: %w", err)
	}
	return nil
}
