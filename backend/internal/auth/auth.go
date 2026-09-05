// Package auth is the authentication port of ADR-106: it turns a bearer
// credential into a Principal. The port is the whole of the abstraction —
// a deployment picks one concrete implementation (Cognito, Supabase,
// Appwrite) and wires it in behind Authenticator; this slice ships only the
// development implementation, in the dev subpackage, gated so it cannot
// reach a deployment build (D2, see registry.go).
package auth

import (
	"context"
	"errors"

	"github.com/nandgator/ymca/backend/internal/db"
)

// Kind is 05.2.3's three principal kinds, the same three the principal
// table in 0001_init.sql constrains and ADR-106 and A3.2 name. Exactly one
// per principal, never a combination.
type Kind string

const (
	KindPersonal Kind = "PERSONAL"
	KindStaff    Kind = "STAFF"
	KindElevated Kind = "ELEVATED"
)

// Plane is which of A3.1's two planes a credential is for (ADR-111).
//
// The polarity is the load-bearing part, not the presence of the field.
// PlaneTenant is the ZERO VALUE, deliberately: a Principal whose Plane was
// never assigned — a partially-filled struct literal, a test fixture, an
// Authenticator implementation that forgets to set it — must come out
// ordinary and tenant-bound, never holding platform authority nobody
// granted it.
//
// This is ADR-106's argument for the `dev` build tag applied a second time:
// a forgotten build flag must yield a binary WITHOUT the development
// authenticator, and a forgotten plane must yield a principal WITHOUT
// platform authority. The accidental case is the safe one. Do not "tidy"
// this by giving PlaneTenant an explicit string value — that would make the
// zero value neither plane, and every construction site would then have to
// remember, which is the property this design refuses to depend on.
type Plane string

const (
	PlaneTenant   Plane = ""
	PlanePlatform Plane = "PLATFORM"
)

// Principal is who is making the request, resolved from a credential by an
// Authenticator. A2.3's principal.id is Principal.ID; the subject of every
// OpenFGA tuple and check is "principal:" + Principal.ID.
type Principal struct {
	ID       string
	PersonID string
	Kind     Kind
	// TenantID is the tenant the credential itself names (A3.2). httpx
	// compares it against the tenant in the request path before routing
	// (ADR-105) — that comparison, not this field alone, is what enforces
	// A3.1. Empty for a platform credential, which names no tenant.
	TenantID string
	// Plane is A3.1's plane (ADR-111). A request satisfies exactly one of
	// httpx.TenantMatch and httpx.PlatformOnly, never both and never
	// neither.
	Plane Plane
}

// ErrUnauthenticated is returned by an Authenticator for any credential it
// will not accept, deliberately without distinguishing why. A missing
// token, a bad signature, an unknown subject and a suspended principal are
// all the same answer to the caller (D3; 8.8's non-disclosure rule).
var ErrUnauthenticated = errors.New("auth: unauthenticated")

// ErrTokenExpired is the one distinguishable failure. A3.4 gives it its own
// error code because a client can react to it differently — by refreshing —
// from an outright invalid credential.
var ErrTokenExpired = errors.New("auth: token expired")

// Authenticator is the port (ADR-106):
//
//	Authenticate(token) → (principal_id, principal_kind, tenant_id, expires_at)
//
// expires_at is not part of this signature: expiry is checked once, at
// verification time, and never carried forward for a caller to re-check
// later (8.7 — decision-time enforcement, never a sweeper).
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (Principal, error)
}

// Deps is what an Authenticator implementation may need to construct
// itself. Not every field applies to every provider — Cognito, Supabase or
// Appwrite (ADR-106) would use their own credentials and never look at
// DevAuthSecret.
type Deps struct {
	DB            *db.DB
	DevAuthSecret string
}
