package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/ymca/mess-backend/internal/domain"
)

// AdminService is deliberately narrow: CentralAdmin can create hostels and
// secretary accounts, and list hostel names — nothing else. It has no
// access to EntryRepo, LeaveRepo, or a hostel's policy/roster, because that
// separation is a domain rule, not just a UI choice (see CONTEXT.md).
type AdminService struct {
	Hostels     HostelRepo
	Secretaries SecretaryRepo
}

type NewHostelInput struct {
	Name   string
	Policy domain.HostelPolicy // Secretary can tune this later; admin just sets a sane starting point
}

func (s AdminService) CreateHostel(ctx context.Context, actor domain.Actor, in NewHostelInput) (domain.Hostel, error) {
	if actor.Role != domain.RoleCentralAdmin {
		return domain.Hostel{}, domain.ErrForbidden
	}
	h := domain.Hostel{ID: uuid.New(), Name: in.Name, Policy: in.Policy}
	if err := s.Hostels.Create(ctx, h); err != nil {
		return domain.Hostel{}, fmt.Errorf("creating hostel: %w", err)
	}
	return h, nil
}

func (s AdminService) ListHostels(ctx context.Context, actor domain.Actor) ([]domain.Hostel, error) {
	if actor.Role != domain.RoleCentralAdmin {
		return nil, domain.ErrForbidden
	}
	return s.Hostels.List(ctx)
}

type NewSecretaryInput struct {
	HostelID uuid.UUID
	StaffID  string
	Name     string
	Email    *string
	Mobile   *string
}

func (s AdminService) CreateSecretary(ctx context.Context, actor domain.Actor, in NewSecretaryInput) (domain.Secretary, error) {
	if actor.Role != domain.RoleCentralAdmin {
		return domain.Secretary{}, domain.ErrForbidden
	}
	if in.Email == nil && in.Mobile == nil {
		return domain.Secretary{}, fmt.Errorf("secretary needs at least one of email or mobile for OTP login")
	}
	sec := domain.Secretary{
		ID:       uuid.New(),
		HostelID: in.HostelID,
		StaffID:  in.StaffID,
		Name:     in.Name,
		Email:    in.Email,
		Mobile:   in.Mobile,
	}
	if err := s.Secretaries.Create(ctx, sec); err != nil {
		return domain.Secretary{}, fmt.Errorf("creating secretary: %w", err)
	}
	return sec, nil
}
