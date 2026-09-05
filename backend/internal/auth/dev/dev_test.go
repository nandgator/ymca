//go:build dev

package dev

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/auth"
)

const testSecret = "01234567890123456789012345678901" // 33 bytes, >= minSecretBytes

// fakeRow is a hand-written pgx.Row so unknown-subject and
// suspended-principal are true unit tests: nothing here touches a live
// Postgres. D10 forbids a new dependency, which rules out a mocking
// library — this is the stdlib-only alternative.
type fakeRow struct {
	err                        error
	id, personID, kind, status string
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*string) = r.id
	*dest[1].(*string) = r.personID
	*dest[2].(*string) = r.kind
	*dest[3].(*string) = r.status
	return nil
}

type fakeQuerier struct {
	row fakeRow
}

func (f fakeQuerier) QueryRow(context.Context, string, ...any) pgx.Row {
	return f.row
}

func newAuthenticator(t *testing.T, row fakeRow) *authenticator {
	t.Helper()
	return &authenticator{db: fakeQuerier{row: row}, secret: []byte(testSecret)}
}

func mint(t *testing.T, sub, tenant string, iat, exp time.Time) string {
	t.Helper()
	tok, err := Mint([]byte(testSecret), sub, tenant, iat, exp)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return tok
}

func TestAuthenticate_Expired(t *testing.T) {
	a := newAuthenticator(t, fakeRow{id: "p1", personID: "person1", kind: "PERSONAL", status: "ACTIVE"})
	now := time.Now()
	tok := mint(t, "sub-1", "tenant-1", now.Add(-2*time.Hour), now.Add(-time.Hour))

	_, err := a.Authenticate(context.Background(), tok)
	if !errors.Is(err, auth.ErrTokenExpired) {
		t.Fatalf("err = %v, want auth.ErrTokenExpired", err)
	}
}

func TestAuthenticate_NotYetValid(t *testing.T) {
	a := newAuthenticator(t, fakeRow{id: "p1", personID: "person1", kind: "PERSONAL", status: "ACTIVE"})
	now := time.Now()
	tok := mint(t, "sub-1", "tenant-1", now.Add(time.Hour), now.Add(2*time.Hour))

	_, err := a.Authenticate(context.Background(), tok)
	if !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("err = %v, want auth.ErrUnauthenticated", err)
	}
}

func TestAuthenticate_WrongSecret(t *testing.T) {
	a := newAuthenticator(t, fakeRow{id: "p1", personID: "person1", kind: "PERSONAL", status: "ACTIVE"})
	now := time.Now()
	tok, err := Mint([]byte("different-secret-different-secret"), "sub-1", "tenant-1", now, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	_, err = a.Authenticate(context.Background(), tok)
	if !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("err = %v, want auth.ErrUnauthenticated", err)
	}
}

func TestAuthenticate_TamperedPayload(t *testing.T) {
	a := newAuthenticator(t, fakeRow{id: "p1", personID: "person1", kind: "PERSONAL", status: "ACTIVE"})
	now := time.Now()
	tok := mint(t, "sub-1", "tenant-1", now, now.Add(time.Hour))

	parts := splitToken(t, tok)
	tamperedPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"sub-EVIL","tenant":"tenant-1","iat":0,"exp":9999999999}`))
	tampered := parts[0] + "." + tamperedPayload + "." + parts[2]

	_, err := a.Authenticate(context.Background(), tampered)
	if !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("err = %v, want auth.ErrUnauthenticated", err)
	}
}

func TestAuthenticate_AlgNone(t *testing.T) {
	a := newAuthenticator(t, fakeRow{id: "p1", personID: "person1", kind: "PERSONAL", status: "ACTIVE"})
	now := time.Now()
	tok := mint(t, "sub-1", "tenant-1", now, now.Add(time.Hour))
	parts := splitToken(t, tok)

	noneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	forged := noneHeader + "." + parts[1] + "."

	_, err := a.Authenticate(context.Background(), forged)
	if !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("err = %v, want auth.ErrUnauthenticated", err)
	}
}

func TestAuthenticate_AlgRS256(t *testing.T) {
	a := newAuthenticator(t, fakeRow{id: "p1", personID: "person1", kind: "PERSONAL", status: "ACTIVE"})
	now := time.Now()
	tok := mint(t, "sub-1", "tenant-1", now, now.Add(time.Hour))
	parts := splitToken(t, tok)

	rsHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	forged := rsHeader + "." + parts[1] + "." + parts[2]

	_, err := a.Authenticate(context.Background(), forged)
	if !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("err = %v, want auth.ErrUnauthenticated", err)
	}
}

func TestAuthenticate_UnknownSub(t *testing.T) {
	a := newAuthenticator(t, fakeRow{err: pgx.ErrNoRows})
	now := time.Now()
	tok := mint(t, "sub-nonexistent", "tenant-1", now, now.Add(time.Hour))

	_, err := a.Authenticate(context.Background(), tok)
	if !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("err = %v, want auth.ErrUnauthenticated", err)
	}
}

func TestAuthenticate_SuspendedPrincipal(t *testing.T) {
	a := newAuthenticator(t, fakeRow{id: "p1", personID: "person1", kind: "STAFF", status: "SUSPENDED"})
	now := time.Now()
	tok := mint(t, "sub-1", "tenant-1", now, now.Add(time.Hour))

	_, err := a.Authenticate(context.Background(), tok)
	if !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("err = %v, want auth.ErrUnauthenticated", err)
	}
}

func TestAuthenticate_Valid(t *testing.T) {
	a := newAuthenticator(t, fakeRow{id: "p1", personID: "person1", kind: "STAFF", status: "ACTIVE"})
	now := time.Now()
	tok := mint(t, "sub-1", "tenant-1", now.Add(-time.Minute), now.Add(time.Hour))

	principal, err := a.Authenticate(context.Background(), tok)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if principal.ID != "p1" || principal.PersonID != "person1" || principal.Kind != auth.KindStaff || principal.TenantID != "tenant-1" {
		t.Fatalf("principal = %+v, unexpected", principal)
	}
}

func splitToken(t *testing.T, tok string) []string {
	t.Helper()
	parts := make([]string, 0, 3)
	start := 0
	for i, c := range tok {
		if c == '.' {
			parts = append(parts, tok[start:i])
			start = i + 1
		}
	}
	parts = append(parts, tok[start:])
	if len(parts) != 3 {
		t.Fatalf("token %q does not have 3 parts", tok)
	}
	return parts
}

// ─────────────────────────────────────────────────────────────
// ADR-111 — the plane, and the polarity that makes it safe
// ─────────────────────────────────────────────────────────────

// TestPlanePolarity is the test that catches the constants being flipped.
//
// It asserts the one property ADR-111 actually rests on: the ZERO value of
// auth.Plane is the tenant plane. Everything else in this file could pass
// with PlanePlatform = "" — a Principal nobody finished constructing would
// then hold platform authority, and no other test would notice, because
// every other test builds its principal deliberately. This one asserts the
// accident.
func TestPlanePolarity(t *testing.T) {
	var zero auth.Principal
	if zero.Plane != auth.PlaneTenant {
		t.Fatalf("the zero Principal has plane %q; ADR-111 requires the zero "+
			"value to be the tenant plane", zero.Plane)
	}
	if zero.Plane == auth.PlanePlatform {
		t.Fatal("the zero Principal is a PLATFORM principal — a partially " +
			"constructed credential now holds platform authority nobody granted")
	}
	if auth.PlanePlatform == "" {
		t.Fatal("PlanePlatform is the empty string, so it is also the zero " +
			"value; ADR-111's polarity is inverted")
	}
}

func mintPlatform(t *testing.T, sub string, iat, exp time.Time) string {
	t.Helper()
	tok, err := MintPlatform([]byte(testSecret), sub, iat, exp)
	if err != nil {
		t.Fatalf("mint platform: %v", err)
	}
	return tok
}

func TestAuthenticate_PlatformToken(t *testing.T) {
	a := newAuthenticator(t, fakeRow{id: "p9", personID: "person9", kind: "STAFF", status: "ACTIVE"})
	now := time.Now()
	tok := mintPlatform(t, "platform-op", now.Add(-time.Minute), now.Add(time.Hour))

	principal, err := a.Authenticate(context.Background(), tok)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if principal.Plane != auth.PlanePlatform {
		t.Fatalf("plane = %q, want %q", principal.Plane, auth.PlanePlatform)
	}
	// A platform credential names no tenant. If it ever did, TenantMatch
	// would have something to compare and A3.1's separation would be a
	// matter of routing rather than of the credential.
	if principal.TenantID != "" {
		t.Fatalf("platform principal names tenant %q; it must name none", principal.TenantID)
	}
}

// A tenant token is unchanged by the plane's arrival: it carries no plane
// and still must name a tenant.
func TestAuthenticate_TenantTokenHasTenantPlane(t *testing.T) {
	a := newAuthenticator(t, fakeRow{id: "p1", personID: "person1", kind: "STAFF", status: "ACTIVE"})
	now := time.Now()
	tok := mint(t, "sub-1", "tenant-1", now.Add(-time.Minute), now.Add(time.Hour))

	principal, err := a.Authenticate(context.Background(), tok)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if principal.Plane != auth.PlaneTenant {
		t.Fatalf("plane = %q, want the tenant plane", principal.Plane)
	}
}

// A credential claiming both planes has no meaning under A3.1, so it is
// refused rather than resolved to one of them.
func TestAuthenticate_PlatformTokenNamingATenantIsRefused(t *testing.T) {
	a := newAuthenticator(t, fakeRow{id: "p9", personID: "person9", kind: "STAFF", status: "ACTIVE"})
	now := time.Now()

	tok := signClaims(t, claims{
		Sub:    "platform-op",
		Tenant: "tenant-1",
		Plane:  string(auth.PlanePlatform),
		IAT:    now.Add(-time.Minute).Unix(),
		EXP:    now.Add(time.Hour).Unix(),
	})
	if _, err := a.Authenticate(context.Background(), tok); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

// An unrecognised plane is refused outright rather than falling back to
// either plane — the same no-agility rule the JWT header follows.
func TestAuthenticate_UnknownPlaneIsRefused(t *testing.T) {
	a := newAuthenticator(t, fakeRow{id: "p9", personID: "person9", kind: "STAFF", status: "ACTIVE"})
	now := time.Now()

	tok := signClaims(t, claims{
		Sub:   "platform-op",
		Plane: "SUPERUSER",
		IAT:   now.Add(-time.Minute).Unix(),
		EXP:   now.Add(time.Hour).Unix(),
	})
	if _, err := a.Authenticate(context.Background(), tok); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

// A token with neither plane nor tenant is malformed: the tenant plane is
// the absent case, and a tenant token must name its tenant.
func TestAuthenticate_NoPlaneAndNoTenantIsRefused(t *testing.T) {
	a := newAuthenticator(t, fakeRow{id: "p1", personID: "person1", kind: "STAFF", status: "ACTIVE"})
	now := time.Now()

	tok := signClaims(t, claims{
		Sub: "sub-1",
		IAT: now.Add(-time.Minute).Unix(),
		EXP: now.Add(time.Hour).Unix(),
	})
	if _, err := a.Authenticate(context.Background(), tok); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

// signClaims mints an arbitrary claim set, including ones Mint and
// MintPlatform deliberately refuse to produce. That is the point: these
// tests are about what verify ACCEPTS, and a shape the minters cannot make
// is exactly the shape an attacker would hand-roll.
func signClaims(t *testing.T, c claims) string {
	t.Helper()
	payload, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return sign([]byte(testSecret), payload)
}
