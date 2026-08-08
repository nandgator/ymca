package auth

import (
	"context"
	"log"
)

// ConsoleSender implements app.OTPSender by logging the code instead of
// actually sending it. Wired in for local dev (docker-compose) so the whole
// stack runs without SES/SNS/Twilio credentials. Swap for a real provider
// in production wiring (see cmd/api/main.go) — the interface is the seam.
type ConsoleSender struct{}

func (ConsoleSender) SendEmail(ctx context.Context, toEmail, code string) error {
	log.Printf("[otp:email] to=%s code=%s (ConsoleSender — wire a real provider for production)", toEmail, code)
	return nil
}

func (ConsoleSender) SendSMS(ctx context.Context, toMobile, code string) error {
	log.Printf("[otp:sms] to=%s code=%s (ConsoleSender — wire a real provider for production)", toMobile, code)
	return nil
}
