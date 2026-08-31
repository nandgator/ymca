//go:build dev

// Package dev is the development implementation of the auth.Authenticator
// port (ADR-106). It exists to unblock work above the auth boundary before
// a real identity provider is wired in, and it must never reach a
// deployment build (D2): this file, and every other file in this package,
// carries the dev build tag, so a build without -tags dev does not compile
// this package in at all — registry.go in the parent package cannot even
// name it.
//
// It verifies a hand-rolled HS256 JWT (D3) with no algorithm agility: the
// header must decode to exactly {"alg":"HS256","typ":"JWT"}, so alg=none
// and alg-confusion attacks are structurally inexpressible rather than
// merely rejected by a check that could be gotten wrong.
package dev

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/auth"
)

func init() {
	auth.Register("dev", open)
}

// minSecretBytes is D3's floor: an HS256 key shorter than this is brute
// forceable and is refused at start-up, not discovered at request time.
const minSecretBytes = 32

// querier is the minimum internal/db.DB this package depends on, narrowed
// so unknown-subject and suspended-principal are true unit tests: a fake
// satisfying this needs no live Postgres. *pgxpool.Pool (and so *db.DB,
// via its Pool method) satisfies it already — nothing production-shaped is
// added by naming it.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type authenticator struct {
	db     querier
	secret []byte
}

// open adapts auth.Deps to this implementation. Registered against the
// name "dev" — see init above.
func open(deps auth.Deps) (auth.Authenticator, error) {
	if deps.DB == nil {
		return nil, errors.New("dev auth: no database configured")
	}
	if len(deps.DevAuthSecret) < minSecretBytes {
		return nil, fmt.Errorf("dev auth: YMCA_DEV_AUTH_SECRET must be at least %d bytes", minSecretBytes)
	}
	return &authenticator{db: deps.DB.Pool(), secret: []byte(deps.DevAuthSecret)}, nil
}

// header is D3's fixed JWT header — the only shape verify accepts.
type header struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

// claims is D3's fixed claim set: sub (the IdP subject), tenant (uuid),
// iat, exp. Nothing else is read or trusted.
type claims struct {
	Sub    string `json:"sub"`
	Tenant string `json:"tenant"`
	IAT    int64  `json:"iat"`
	EXP    int64  `json:"exp"`
}

var (
	// errMalformed covers every verification failure except expiry: bad
	// structure, bad header, bad signature, tampering, not-yet-valid.
	// Authenticate collapses all of them to auth.ErrUnauthenticated —
	// distinguishing them to the caller would be exactly the disclosure
	// 8.8 forbids.
	errMalformed = errors.New("dev auth: malformed token")
	errExpired   = errors.New("dev auth: token expired")
)

// Authenticate implements auth.Authenticator.
func (a *authenticator) Authenticate(ctx context.Context, token string) (auth.Principal, error) {
	c, err := verify(token, a.secret, time.Now())
	if err != nil {
		if errors.Is(err, errExpired) {
			return auth.Principal{}, auth.ErrTokenExpired
		}
		return auth.Principal{}, auth.ErrUnauthenticated
	}

	// D3: sub resolves against principal.idp_subject to give principal.id,
	// person_id and kind. principal is exempt from row-level security
	// (8.2) — it is global, not tenant-scoped — so this reads through the
	// pool directly rather than through db.InTenantTx.
	var id, personID, kind, status string
	err = a.db.QueryRow(ctx,
		`SELECT id, person_id, kind, status FROM principal WHERE idp_subject = $1`,
		c.Sub,
	).Scan(&id, &personID, &kind, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// D3: an unknown subject and a suspended principal are the
			// same answer to the caller.
			return auth.Principal{}, auth.ErrUnauthenticated
		}
		return auth.Principal{}, fmt.Errorf("dev auth: resolve principal: %w", err)
	}
	if status != "ACTIVE" {
		return auth.Principal{}, auth.ErrUnauthenticated
	}

	return auth.Principal{
		ID:       id,
		PersonID: personID,
		Kind:     auth.Kind(kind),
		TenantID: c.Tenant,
	}, nil
}

// verify checks the structure, header, signature and time window of a dev
// token and returns its claims. The signature is checked before the
// payload is ever decoded into claims, so a tampered payload is caught by
// the signature mismatch rather than by trusting altered content long
// enough to reject it on some other ground.
func verify(token string, secret []byte, now time.Time) (claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims{}, errMalformed
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claims{}, errMalformed
	}
	var h header
	hdec := json.NewDecoder(bytes.NewReader(headerBytes))
	hdec.DisallowUnknownFields()
	if err := hdec.Decode(&h); err != nil {
		return claims{}, errMalformed
	}
	// D3: the header must be exactly {"alg":"HS256","typ":"JWT"}, not
	// merely contain those values somewhere among others. No algorithm
	// agility: this is the only alg this package will ever accept.
	if h.Alg != "HS256" || h.Typ != "JWT" {
		return claims{}, errMalformed
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return claims{}, errMalformed
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return claims{}, errMalformed
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims{}, errMalformed
	}
	var c claims
	pdec := json.NewDecoder(bytes.NewReader(payloadBytes))
	pdec.DisallowUnknownFields()
	if err := pdec.Decode(&c); err != nil {
		return claims{}, errMalformed
	}
	if c.Sub == "" || c.Tenant == "" {
		return claims{}, errMalformed
	}

	if now.Before(time.Unix(c.IAT, 0)) {
		return claims{}, errMalformed // not yet valid
	}
	if !now.Before(time.Unix(c.EXP, 0)) {
		return claims{}, errExpired
	}
	return c, nil
}
