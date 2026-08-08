package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ymca/mess-backend/internal/auth"
	"github.com/ymca/mess-backend/internal/domain"
)

var (
	ErrUnknownLoginID   = errors.New("auth: no account with that ID")
	ErrNoSuchChannel    = errors.New("auth: no email/mobile on file for that channel")
	ErrInvalidOrExpired = errors.New("auth: code is invalid or has expired")
)

type OTPChannel string

const (
	ChannelEmail OTPChannel = "EMAIL"
	ChannelSMS   OTPChannel = "SMS"
)

const otpValidity = 5 * time.Minute
const sessionValidity = 30 * 24 * time.Hour

type AuthService struct {
	Members       MemberRepo
	Secretaries   SecretaryRepo
	CentralAdmins CentralAdminRepo
	OTPs          OTPStore
	Sessions      SessionStore
	Sender        OTPSender
	Now           func() time.Time
}

// subject bundles what every role needs after ID lookup, regardless of
// which repo it came from, so the rest of this file doesn't need three
// near-identical code paths.
type subject struct {
	role     domain.Role
	id       domain.Actor // reused as a carrier for id + hostelID only, role set below
	email    *string
	mobile   *string
}

func (s AuthService) resolveSubject(ctx context.Context, role domain.Role, loginID string) (subject, error) {
	switch role {
	case domain.RoleMember:
		m, err := s.Members.GetByMemberID(ctx, loginID)
		if err != nil {
			return subject{}, ErrUnknownLoginID
		}
		hostelID := m.HostelID
		return subject{role: role, id: domain.Actor{Role: role, ID: m.ID, HostelID: &hostelID}, email: m.Email, mobile: m.Mobile}, nil
	case domain.RoleSecretary:
		sec, err := s.Secretaries.GetByStaffID(ctx, loginID)
		if err != nil {
			return subject{}, ErrUnknownLoginID
		}
		hostelID := sec.HostelID
		return subject{role: role, id: domain.Actor{Role: role, ID: sec.ID, HostelID: &hostelID}, email: sec.Email, mobile: sec.Mobile}, nil
	case domain.RoleCentralAdmin:
		ca, err := s.CentralAdmins.GetByStaffID(ctx, loginID)
		if err != nil {
			return subject{}, ErrUnknownLoginID
		}
		return subject{role: role, id: domain.Actor{Role: role, ID: ca.ID, HostelID: nil}, email: ca.Email, mobile: ca.Mobile}, nil
	default:
		return subject{}, ErrUnknownLoginID
	}
}

// RequestOTP looks up the account by its offline-provisioned login ID and
// sends a fresh 6-digit code to the requested channel. It deliberately does
// not reveal *which* part failed (unknown ID vs missing channel) beyond
// what's returned here — callers should show a generic "check your
// email/phone" message either way, standard OTP-flow practice.
func (s AuthService) RequestOTP(ctx context.Context, role domain.Role, loginID string, channel OTPChannel) error {
	subj, err := s.resolveSubject(ctx, role, loginID)
	if err != nil {
		return err
	}

	var destination string
	switch channel {
	case ChannelEmail:
		if subj.email == nil {
			return ErrNoSuchChannel
		}
		destination = *subj.email
	case ChannelSMS:
		if subj.mobile == nil {
			return ErrNoSuchChannel
		}
		destination = *subj.mobile
	default:
		return ErrNoSuchChannel
	}

	code, err := auth.GenerateOTP()
	if err != nil {
		return fmt.Errorf("generating otp: %w", err)
	}

	expiresAt := s.now().Add(otpValidity).Unix()
	if err := s.OTPs.Issue(ctx, subj.role, subj.id.ID, string(channel), destination, auth.HashSecret(code), expiresAt); err != nil {
		return fmt.Errorf("storing otp: %w", err)
	}

	switch channel {
	case ChannelEmail:
		return s.Sender.SendEmail(ctx, destination, code)
	case ChannelSMS:
		return s.Sender.SendSMS(ctx, destination, code)
	}
	return nil
}

// VerifyOTP checks the code and, on success, issues a bearer session token.
// The raw token is returned exactly once here — only its hash is persisted.
func (s AuthService) VerifyOTP(ctx context.Context, role domain.Role, loginID, code string) (string, domain.Actor, error) {
	subj, err := s.resolveSubject(ctx, role, loginID)
	if err != nil {
		return "", domain.Actor{}, err
	}

	ok, err := s.OTPs.VerifyAndConsume(ctx, subj.role, subj.id.ID, auth.HashSecret(code))
	if err != nil {
		return "", domain.Actor{}, fmt.Errorf("verifying otp: %w", err)
	}
	if !ok {
		return "", domain.Actor{}, ErrInvalidOrExpired
	}

	token, err := auth.GenerateSessionToken()
	if err != nil {
		return "", domain.Actor{}, fmt.Errorf("generating session token: %w", err)
	}
	expiresAt := s.now().Add(sessionValidity).Unix()
	if err := s.Sessions.Create(ctx, subj.id, auth.HashSecret(token), expiresAt); err != nil {
		return "", domain.Actor{}, fmt.Errorf("storing session: %w", err)
	}

	return token, subj.id, nil
}

func (s AuthService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}
