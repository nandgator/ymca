// Package app orchestrates the domain layer against infrastructure. It
// defines the interfaces ("ports") that internal/storage/postgres and
// internal/auth implement ("adapters") — the domain package itself stays
// infrastructure-free per the codebase-design deep-module principle:
// small interfaces here, real behaviour behind them, each testable in
// isolation via a fake implementing the same interface.
package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/ymca/mess-backend/internal/domain"
)

type HostelRepo interface {
	Create(ctx context.Context, h domain.Hostel) error
	Get(ctx context.Context, id uuid.UUID) (domain.Hostel, error)
	List(ctx context.Context) ([]domain.Hostel, error)
	UpdatePolicy(ctx context.Context, hostelID uuid.UUID, policy domain.HostelPolicy) error
	AddOptionalItem(ctx context.Context, hostelID uuid.UUID, item domain.OptionalItem) error
	DeactivateOptionalItem(ctx context.Context, hostelID, itemID uuid.UUID) error
}

type MemberRepo interface {
	Create(ctx context.Context, m domain.Member) error
	Get(ctx context.Context, id uuid.UUID) (domain.Member, error)
	GetByMemberID(ctx context.Context, memberID string) (domain.Member, error)
	ListByHostel(ctx context.Context, hostelID uuid.UUID) ([]domain.Member, error)
}

type SecretaryRepo interface {
	Create(ctx context.Context, s domain.Secretary) error
	GetByStaffID(ctx context.Context, staffID string) (domain.Secretary, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Secretary, error)
}

type CentralAdminRepo interface {
	GetByStaffID(ctx context.Context, staffID string) (domain.CentralAdmin, error)
	Get(ctx context.Context, id uuid.UUID) (domain.CentralAdmin, error)
}

type EntryRepo interface {
	Upsert(ctx context.Context, e domain.Entry) error
	Get(ctx context.Context, memberID uuid.UUID, date domain.Day, meal domain.MealType) (domain.Entry, bool, error)
	ListForMemberMonth(ctx context.Context, memberID uuid.UUID, year, month int) ([]domain.Entry, error)
	ListForHostelMonth(ctx context.Context, hostelID uuid.UUID, year, month int) ([]domain.Entry, error)
	ListForHostelDate(ctx context.Context, hostelID uuid.UUID, date domain.Day) ([]domain.Entry, error)
}

type LeaveRepo interface {
	Create(ctx context.Context, l domain.Leave) error
	ListForMember(ctx context.Context, memberID uuid.UUID) ([]domain.Leave, error)
	ListForMemberMonth(ctx context.Context, memberID uuid.UUID, year, month int) ([]domain.Leave, error)
	ListForHostelMonth(ctx context.Context, hostelID uuid.UUID, year, month int) ([]domain.Leave, error)
	CoveringDate(ctx context.Context, memberID uuid.UUID, date domain.Day) (domain.Leave, bool, error)
}

// OTPStore persists issued OTP codes and validates attempts against them.
// Codes are always stored hashed — see internal/auth.
type OTPStore interface {
	Issue(ctx context.Context, subjectType domain.Role, subjectID uuid.UUID, channel string, destination string, codeHash string, expiresAt int64) error
	VerifyAndConsume(ctx context.Context, subjectType domain.Role, subjectID uuid.UUID, codeHash string) (bool, error)
}

// SessionStore persists bearer session tokens issued after a successful OTP verify.
type SessionStore interface {
	Create(ctx context.Context, actor domain.Actor, tokenHash string, expiresAt int64) error
	Lookup(ctx context.Context, tokenHash string) (domain.Actor, bool, error)
	Revoke(ctx context.Context, tokenHash string) error
}

// OTPSender delivers a one-time code over email or SMS. Production wiring
// picks a real provider (SES/SNS/Twilio/etc); local dev uses a console
// logger implementation — see internal/auth/console_sender.go.
type OTPSender interface {
	SendEmail(ctx context.Context, toEmail, code string) error
	SendSMS(ctx context.Context, toMobile, code string) error
}
