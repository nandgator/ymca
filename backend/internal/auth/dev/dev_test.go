//go:build dev

package dev

import (
	"context"
	"encoding/base64"
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
