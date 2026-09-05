//go:build integration && dev

package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/openfga/go-sdk/client"

	"github.com/nandgator/ymca/backend/internal/auth"
	authdev "github.com/nandgator/ymca/backend/internal/auth/dev"
	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/config"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/httpx"
)

// TestGetMe_EndToEnd exercises D6's whole path with a minted token: a real
// dev JWT, real HS256 verification, a real principal lookup, and five real
// authz.Check calls against the live OpenFGA and PostgreSQL — the same
// wiring main.go's serve() builds, reassembled here around an
// httptest.Server instead of a real listener. Requires the same
// environment as cmd/api itself, plus YMCA_DEV_AUTH_SECRET.
func TestGetMe_EndToEnd(t *testing.T) {
	ctx := context.Background()

	cfg := config.Config{
		DatabaseURL:   mustEnvVar(t, "YMCA_DATABASE_URL"),
		FGAAPIURL:     mustEnvVar(t, "YMCA_FGA_API_URL"),
		FGAStoreID:    mustEnvVar(t, "YMCA_FGA_STORE_ID"),
		FGAModelID:    mustEnvVar(t, "YMCA_FGA_MODEL_ID"),
		DevAuthSecret: mustEnvVar(t, "YMCA_DEV_AUTH_SECRET"),
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(pool.Close)

	fga, err := authz.NewFGA(cfg)
	if err != nil {
		t.Fatalf("authz.NewFGA: %v", err)
	}

	const (
		tenantID    = "66666666-6666-6666-6666-666666666666"
		personID    = "77777777-7777-7777-7777-777777777777"
		principalID = "88888888-8888-8888-8888-888888888888"
		idpSubject  = "me-integration-test-subject"
		roleDefID   = "99999999-9999-9999-9999-999999999991"
		assignID    = "99999999-9999-9999-9999-999999999992"
	)

	if _, err := pool.Pool().Exec(ctx, `
		INSERT INTO tenant (id, legal_name, display_name, jurisdiction, status)
		VALUES ($1, 'Me Test', 'Me Test', 'US', 'ACTIVE') ON CONFLICT (id) DO NOTHING
	`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := pool.Pool().Exec(ctx, `
		INSERT INTO person (id, display_name, status) VALUES ($1, 'Me Test Person', 'ACTIVE')
		ON CONFLICT (id) DO NOTHING
	`, personID); err != nil {
		t.Fatalf("seed person: %v", err)
	}
	if _, err := pool.Pool().Exec(ctx, `
		INSERT INTO principal (id, person_id, idp_subject, kind, status)
		VALUES ($1, $2, $3, 'PERSONAL', 'ACTIVE') ON CONFLICT (id) DO NOTHING
	`, principalID, personID, idpSubject); err != nil {
		t.Fatalf("seed principal: %v", err)
	}

	// A role conferring may_approve_membership, held within its term. This is
	// the end-to-end claim 8.4 rests on: the client gates its shell on /me,
	// and a permission held through a role must be indistinguishable there
	// from one held directly. No tuple is written for it — step 2 resolves it
	// per check (ADR-109).
	if err := pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO role_definition (id, tenant_id, code, name, term_policy, status)
			VALUES ($1, $2, 'me-test-approver', 'Me Test Approver', 'OPTIONAL_TERM', 'ACTIVE')
			ON CONFLICT (id) DO NOTHING`, roleDefID, tenantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO role_permission (role_definition_id, permission)
			VALUES ($1, 'tenant.may_approve_membership') ON CONFLICT DO NOTHING`,
			roleDefID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO role_assignment
			  (id, tenant_id, role_definition_id, subject_principal_id,
			   scope_type, scope_id, valid_until, granted_by_principal_id)
			VALUES ($1, $2, $3, $4, 'tenant', $2, now() + interval '1 day', $4)
			ON CONFLICT (id) DO NOTHING`, assignID, tenantID, roleDefID, principalID)
		return err
	}); err != nil {
		t.Fatalf("seed role: %v", err)
	}

	t.Cleanup(func() {
		bg := context.Background()
		_ = pool.InTenantTx(bg, tenantID, func(tx pgx.Tx) error {
			_, _ = tx.Exec(bg, `DELETE FROM role_assignment WHERE id = $1`, assignID)
			_, _ = tx.Exec(bg, `DELETE FROM role_permission WHERE role_definition_id = $1`, roleDefID)
			_, _ = tx.Exec(bg, `DELETE FROM role_definition WHERE id = $1`, roleDefID)
			return nil
		})
		_, _ = pool.Pool().Exec(bg, `DELETE FROM principal WHERE id = $1`, principalID)
		_, _ = pool.Pool().Exec(bg, `DELETE FROM person WHERE id = $1`, personID)
		_, _ = pool.Pool().Exec(bg, `DELETE FROM tenant WHERE id = $1`, tenantID)
	})

	fgaRaw, err := client.NewSdkClient(&client.ClientConfiguration{
		ApiUrl: cfg.FGAAPIURL, StoreId: cfg.FGAStoreID, AuthorizationModelId: cfg.FGAModelID,
	})
	if err != nil {
		t.Fatalf("raw fga client: %v", err)
	}
	// Grant member and finance_reader; leave admin and safeguarding_reader
	// ungranted, so the response must contain exactly the first two.
	writes := []client.ClientTupleKey{
		{User: "principal:" + principalID, Relation: "member", Object: "tenant:" + tenantID},
		{User: "principal:" + principalID, Relation: "finance_reader", Object: "tenant:" + tenantID},
	}
	if _, err := fgaRaw.Write(ctx).Body(client.ClientWriteRequest{Writes: writes}).Execute(); err != nil {
		t.Fatalf("write tuples: %v", err)
	}
	t.Cleanup(func() {
		deletes := make([]client.ClientTupleKeyWithoutCondition, len(writes))
		for i, w := range writes {
			deletes[i] = client.ClientTupleKeyWithoutCondition{User: w.User, Relation: w.Relation, Object: w.Object}
		}
		_, _ = fgaRaw.Write(context.Background()).Body(client.ClientWriteRequest{Deletes: deletes}).Execute()
	})

	authenticator, err := auth.Open("dev", auth.Deps{DB: pool, DevAuthSecret: cfg.DevAuthSecret})
	if err != nil {
		t.Fatalf("auth.Open(dev): %v", err)
	}

	token, err := authdev.Mint([]byte(cfg.DevAuthSecret), idpSubject, tenantID,
		time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// The same route wiring as main.go's serve(): TenantMatch applied
	// per-route so it runs after ServeMux has resolved {tenant}, then the
	// standard middleware chain around the mux.
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/t/{tenant}/me",
		httpx.TenantMatch(pool, logger)(http.HandlerFunc(handleMe(pool, fga, logger))))
	handler := httpx.Chain(mux,
		httpx.Recover(logger),
		httpx.RequestID(),
		httpx.Logging(logger),
		httpx.Authenticate(authenticator, logger),
	)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/t/"+tenantID+"/me", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body = %s", resp.StatusCode, body)
	}

	var got meResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.PrincipalID != principalID || got.PersonID != personID || got.TenantID != tenantID || got.Kind != "PERSONAL" {
		t.Fatalf("response = %+v, unexpected identity fields", got)
	}
	// member and finance_reader are held by direct tuple;
	// may_approve_membership only through the role assignment seeded above.
	// admin and safeguarding_reader are held by neither route.
	wantPerms := map[string]bool{
		"tenant:member":                 true,
		"tenant:finance_reader":         true,
		"tenant:may_approve_membership": true,
	}
	if len(got.Permissions) != len(wantPerms) {
		t.Fatalf("permissions = %v, want exactly %v", got.Permissions, wantPerms)
	}
	for _, p := range got.Permissions {
		if !wantPerms[p] {
			t.Fatalf("unexpected permission %q in %v", p, got.Permissions)
		}
	}
}

func mustEnvVar(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("%s must be set to run integration tests", key)
	}
	return v
}
