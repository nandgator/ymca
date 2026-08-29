// Package auth holds pure, infrastructure-free helpers for OTP codes and
// session tokens: generation and hashing only. There are no passwords
// anywhere in this system (see CONTEXT.md) — this package exists precisely
// because "no passwords" still needs *something* to authenticate with.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// GenerateOTP returns a 6-digit numeric code, e.g. "042917". Uses
// crypto/rand, not math/rand — this is a credential, however short-lived.
func GenerateOTP() (string, error) {
	max := 1000000 // 10^6
	n, err := randomInt(max)
	if err != nil {
		return "", fmt.Errorf("generating otp: %w", err)
	}
	return fmt.Sprintf("%06d", n), nil
}

// GenerateSessionToken returns a 256-bit random token, hex-encoded, to be
// handed to the client as a bearer token. Only its hash is ever stored.
func GenerateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating session token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashSecret returns the hex-encoded SHA-256 of a secret (OTP code or
// session token) for storage — never store either in plaintext.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func randomInt(max int) (int, error) {
	// Rejection sampling over crypto/rand to avoid modulo bias.
	if max <= 0 {
		return 0, fmt.Errorf("max must be positive")
	}
	limit := (1 << 32) - (1<<32)%uint64(max)
	for {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			return 0, err
		}
		v := uint64(b[0])<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3])
		if v < limit {
			return int(v % uint64(max)), nil
		}
	}
}
