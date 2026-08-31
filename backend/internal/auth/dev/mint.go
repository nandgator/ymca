//go:build dev

package dev

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
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

	h := base64.RawURLEncoding.EncodeToString([]byte(fixedHeader))
	p := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := h + "." + p

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + sig, nil
}
