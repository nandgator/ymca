package httpx_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/auth"
	"github.com/nandgator/ymca/backend/internal/httpx"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeAuthenticator struct {
	principal auth.Principal
	err       error
}

func (f fakeAuthenticator) Authenticate(context.Context, string) (auth.Principal, error) {
	return f.principal, f.err
}

func decodeError(t *testing.T, body []byte) (code, message string) {
	t.Helper()
	var v struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return v.Error.Code, v.Error.Message
}

func TestAuthenticate_NoBearer(t *testing.T) {
	mw := httpx.Authenticate(fakeAuthenticator{}, testLogger())
	called := false
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/t/x/me", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler ran without a bearer token")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	code, _ := decodeError(t, rec.Body.Bytes())
	if code != "unauthenticated" {
		t.Fatalf("code = %q, want unauthenticated", code)
	}
}

func TestAuthenticate_MalformedHeader(t *testing.T) {
	mw := httpx.Authenticate(fakeAuthenticator{}, testLogger())
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not run")
	}))

	for _, hdr := range []string{"Basic dXNlcjpwYXNz", "Bearer", "Bearer   "} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/t/x/me", nil)
		req.Header.Set("Authorization", hdr)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("Authorization=%q: status = %d, want 401", hdr, rec.Code)
		}
		code, _ := decodeError(t, rec.Body.Bytes())
		if code != "unauthenticated" {
			t.Fatalf("Authorization=%q: code = %q, want unauthenticated", hdr, code)
		}
	}
}

func TestAuthenticate_TokenExpired(t *testing.T) {
	mw := httpx.Authenticate(fakeAuthenticator{err: auth.ErrTokenExpired}, testLogger())
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not run")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/t/x/me", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	code, _ := decodeError(t, rec.Body.Bytes())
	if code != "token_expired" {
		t.Fatalf("code = %q, want token_expired", code)
	}
}

func TestAuthenticate_ValidToken(t *testing.T) {
	want := auth.Principal{ID: "p1", PersonID: "person1", Kind: auth.KindPersonal, TenantID: "tenant-1"}
	mw := httpx.Authenticate(fakeAuthenticator{principal: want}, testLogger())

	var got auth.Principal
	var ok bool
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok = httpx.PrincipalFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/t/x/me", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !ok || got != want {
		t.Fatalf("principal in context = %+v (ok=%v), want %+v", got, ok, want)
	}
}

// fakeTxRunner stands in for *db.DB so TenantMatch's mismatch path — which
// writes an audit row inside a tenant transaction — is testable without a
// live Postgres. It records the call and skips fn, which would otherwise
// need a real pgx.Tx.
type fakeTxRunner struct {
	calledWithTenant string
	called           bool
}

func (f *fakeTxRunner) InTenantTx(_ context.Context, tenantID string, _ func(pgx.Tx) error) error {
	f.called = true
	f.calledWithTenant = tenantID
	return nil
}

func TestTenantMatch_Mismatch(t *testing.T) {
	runner := &fakeTxRunner{}
	mw := httpx.TenantMatch(runner, testLogger())
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not run on a tenant mismatch")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/t/path-tenant/me", nil)
	req.SetPathValue("tenant", "path-tenant")
	principal := auth.Principal{ID: "p1", TenantID: "token-tenant"}
	req = req.WithContext(contextWithPrincipal(req.Context(), principal))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	code, _ := decodeError(t, rec.Body.Bytes())
	if code != "tenant_mismatch" {
		t.Fatalf("code = %q, want tenant_mismatch", code)
	}
	if !runner.called || runner.calledWithTenant != "path-tenant" {
		t.Fatalf("expected the mismatch to be audited against the path tenant; runner = %+v", runner)
	}
}

func TestTenantMatch_Match(t *testing.T) {
	runner := &fakeTxRunner{}
	mw := httpx.TenantMatch(runner, testLogger())
	called := false
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/t/tenant-1/me", nil)
	req.SetPathValue("tenant", "tenant-1")
	principal := auth.Principal{ID: "p1", TenantID: "tenant-1"}
	req = req.WithContext(contextWithPrincipal(req.Context(), principal))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("handler did not run on a matching tenant")
	}
	if runner.called {
		t.Fatal("a matching tenant must not be audited")
	}
}

// contextWithPrincipal round-trips through Authenticate's own middleware so
// this test package never needs an internal (unexported) helper from
// httpx — it observes exactly what a real request would carry.
func contextWithPrincipal(ctx context.Context, p auth.Principal) context.Context {
	var out context.Context
	mw := httpx.Authenticate(fakeAuthenticator{principal: p}, testLogger())
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { out = r.Context() }))

	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer x")
	h.ServeHTTP(httptest.NewRecorder(), req)
	return out
}

func TestRecover_PanicReturns500WithNoStackInBody(t *testing.T) {
	mw := httpx.Recover(testLogger())
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom: something exploded at internal/foo.go:42")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic escaped Recover: %v", r)
			}
		}()
		h.ServeHTTP(rec, req)
	}()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	code, _ := decodeError(t, rec.Body.Bytes())
	if code != "internal" {
		t.Fatalf("code = %q, want internal", code)
	}
	body := rec.Body.String()
	for _, leak := range []string{"boom", "foo.go", "goroutine"} {
		if containsFold(body, leak) {
			t.Fatalf("response body leaks panic detail %q: %s", leak, body)
		}
	}
}

func containsFold(s, substr string) bool {
	return len(s) >= len(substr) && indexFold(s, substr) >= 0
}

func indexFold(s, substr string) int {
	// Simple case-sensitive search is sufficient here: the panic strings
	// used in this test are lowercase and would appear verbatim if leaked.
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
