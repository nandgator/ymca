package domain

import "github.com/google/uuid"

type Role string

const (
	RoleMember       Role = "MEMBER"
	RoleSecretary    Role = "SECRETARY"
	RoleCentralAdmin Role = "CENTRAL_ADMIN"
)

// Member belongs to exactly one Hostel. Never self-registered — provisioned
// offline by a Secretary or CentralAdmin with a MemberID plus a verified
// email and/or mobile number for OTP login.
type Member struct {
	ID       uuid.UUID
	HostelID uuid.UUID
	MemberID string // human-facing login ID, e.g. "YMCA-2026-0143"
	Name     string
	Email    *string
	Mobile   *string
}

// Secretary is staff scoped to exactly one Hostel: full control of that
// hostel's roster, menu, pricing, leave records, and month-end billing.
// Cannot see or act on any other hostel.
type Secretary struct {
	ID       uuid.UUID
	HostelID uuid.UUID
	StaffID  string
	Name     string
	Email    *string
	Mobile   *string
}

// CentralAdmin manages Hostels and Secretary accounts only — no visibility
// into rosters, entries, menus, or billing of any hostel.
type CentralAdmin struct {
	ID      uuid.UUID
	StaffID string
	Name    string
	Email   *string
	Mobile  *string
}

// Actor is the authenticated identity attached to a request after OTP login.
type Actor struct {
	Role     Role
	ID       uuid.UUID
	HostelID *uuid.UUID // nil for CentralAdmin; set for Member/Secretary
}

func (a Actor) CanActOnHostel(hostelID uuid.UUID) bool {
	if a.Role == RoleCentralAdmin {
		return false // by design — central admin never touches hostel-scoped data
	}
	return a.HostelID != nil && *a.HostelID == hostelID
}
