//go:build dev

package dev

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nandgator/ymca/backend/internal/auth"
)

// fixedHeader is D3's only accepted header, byte for byte — minting it any
// other way would produce a token verify itself would reject.
const fixedHeader = `{"alg":"HS256","typ":"JWT"}`

// Mint signs a dev token for sub, naming tenant, valid from iat to exp. It
// is the only way to produce a token this package's Authenticate will
// accept, and it is used both by cmd/api's mint-token subcommand and by
// this package's own tests (D3).
func Mint(secret []byte, sub, tenant string, iat, exp time.Time) (string, error) {
	if sub == "" {
		return "", fmt.Errorf("dev: mint: sub is required")
	}
	if tenant == "" {
		return "", fmt.Errorf("dev: mint: tenant is required")
	}
	if len(secret) < minSecretBytes {
		return "", fmt.Errorf("dev: mint: secret must be at least %d bytes", minSecretBytes)
	}

	payload, err := json.Marshal(claims{Sub: sub, Tenant: tenant, IAT: iat.Unix(), EXP: exp.Unix()})
	if err != nil {
		return "", fmt.Errorf("dev: mint: encode claims: %w", err)
	}

	return sign(secret, payload), nil
}

// MintPlatform signs a platform-plane dev token for sub (ADR-111). It names
// no tenant, because the platform plane names none.
//
// This is a separate function rather than a plane argument to Mint, and that
// is deliberate. Mint still refuses an empty tenant, so no existing caller
// can produce a platform credential by leaving an argument blank or by
// passing a variable that happened to be empty. Minting platform authority
// requires naming this function — the same reasoning as the tag polarity in
// ADR-106 and the zero value in ADR-111: the dangerous thing is never what
// you get by accident.
func MintPlatform(secret []byte, sub string, iat, exp time.Time) (string, error) {
	if sub == "" {
		return "", fmt.Errorf("dev: mint platform: sub is required")
	}
	if len(secret) < minSecretBytes {
		return "", fmt.Errorf("dev: mint platform: secret must be at least %d bytes", minSecretBytes)
	}

	payload, err := json.Marshal(claims{
		Sub:   sub,
		Plane: string(auth.PlanePlatform),
		IAT:   iat.Unix(),
		EXP:   exp.Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("dev: mint platform: encode claims: %w", err)
	}
	return sign(secret, payload), nil
}

// sign is the half of minting that both Mint and MintPlatform share: the
// fixed header, the payload, and the HMAC over them.
func sign(secret []byte, payload []byte) string {
	h := base64.RawURLEncoding.EncodeToString([]byte(fixedHeader))
	p := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := h + "." + p

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
