//go:build integration && dev

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openfga/go-sdk/client"

	"github.com/nandgator/ymca/backend/internal/auth"
	authdev "github.com/nandgator/ymca/backend/internal/auth/dev"
	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/config"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/httpx"
	"github.com/nandgator/ymca/backend/internal/outbox"
)

// The claim under test is ADR-113's, and it is not "the endpoint returns
// 201". It is that a tenant provisioned through the platform plane is
// USABLE — that its owner can immediately act inside it. A tenant created
// without its owner tuple would return 201 just the same, exist in the
// database just the same, and be administrable by nobody, with no error
// anywhere saying so. That silence is the failure this test exists to
// catch, so the assertion has to be a successful tenant-plane write by the
// owner, not the shape of the provisioning response.
func TestPlatformProvisioning_EndToEnd(t *testing.T) {
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
	writer, err := outbox.NewFGAWriter(cfg)
	if err != nil {
		t.Fatalf("NewFGAWriter: %v", err)
	}

	const (
		opPerson      = "eeeeeeee-0000-0000-0000-000000000001"
		opPrincipal   = "eeeeeeee-0000-0000-0000-000000000002"
		opSubject     = "platform-test-operator"
		bystanderPers = "eeeeeeee-0000-0000-0000-000000000003"
		bystanderPrin = "eeeeeeee-0000-0000-0000-000000000004"
		bystanderSub  = "platform-test-bystander"
		hostTenant    = "eeeeeeee-0000-0000-0000-000000000005"
		tenantPers    = "eeeeeeee-0000-0000-0000-000000000006"
		tenantPrin    = "eeeeeeee-0000-0000-0000-000000000007"
		tenantSub     = "platform-test-tenant-bound"

		newOwnerSubject = "platform-test-new-owner"
	)

	seed := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Pool().Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seed(`INSERT INTO person (id, display_name, status) VALUES ($1,'Platform Operator','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, opPerson)
	seed(`INSERT INTO principal (id, person_id, idp_subject, kind, status)
	      VALUES ($1,$2,$3,'STAFF','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		opPrincipal, opPerson, opSubject)
	// A platform principal holding no operator tuple: the negative twin.
	seed(`INSERT INTO person (id, display_name, status) VALUES ($1,'Platform Bystander','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, bystanderPers)
	seed(`INSERT INTO principal (id, person_id, idp_subject, kind, status)
	      VALUES ($1,$2,$3,'STAFF','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		bystanderPrin, bystanderPers, bystanderSub)
	// An ordinary tenant-bound principal, for the two plane tests.
	seed(`INSERT INTO tenant (id, legal_name, display_name, jurisdiction, status)
	      VALUES ($1,'Platform Test Host','Platform Test Host','IN','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, hostTenant)
	seed(`INSERT INTO person (id, display_name, status) VALUES ($1,'Tenant Person','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, tenantPers)
	seed(`INSERT INTO principal (id, person_id, idp_subject, kind, status)
	      VALUES ($1,$2,$3,'STAFF','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		tenantPrin, tenantPers, tenantSub)

	fgaRaw, err := client.NewSdkClient(&client.ClientConfiguration{
		ApiUrl: cfg.FGAAPIURL, StoreId: cfg.FGAStoreID, AuthorizationModelId: cfg.FGAModelID,
	})
	if err != nil {
		t.Fatalf("raw fga client: %v", err)
	}

	// The operator's authority is a direct tuple. How a platform operator
	// comes to hold `operator` is out of this slice — there is no endpoint
	// that grants it, deliberately (11.2).
	opTuple := client.ClientTupleKey{
		User:     "principal:" + opPrincipal,
		Relation: "operator",
		Object:   "platform:" + authz.PlatformObjectID,
	}
	if _, err := fgaRaw.Write(ctx).Body(client.ClientWriteRequest{
		Writes: []client.ClientTupleKey{opTuple},
	}).Execute(); err != nil {
		t.Fatalf("write operator tuple: %v", err)
	}

	var (
		newTenantID, newOwnerPrincipal, newOwnerPerson, bundleID string
		writtenTuples                                            []outbox.Tuple
	)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = fgaRaw.Write(bg).Body(client.ClientWriteRequest{
			Deletes: []client.ClientTupleKeyWithoutCondition{{
				User: opTuple.User, Relation: opTuple.Relation, Object: opTuple.Object,
			}},
		}).Execute()
		_ = writer.DeleteTuples(bg, writtenTuples)

		// Scoped by id throughout. `go test` runs packages in parallel, and
		// an unscoped DELETE here once clobbered another package's rows.
		if newTenantID != "" {
			_, _ = pool.Pool().Exec(bg,
				`DELETE FROM authorization_outbox WHERE aggregate_id = $1::uuid`, newTenantID)
		}
		if bundleID != "" {
			_, _ = pool.Pool().Exec(bg,
				`DELETE FROM authorization_outbox WHERE aggregate_id = $1::uuid`, bundleID)
			_, _ = pool.Pool().Exec(bg,
				`DELETE FROM entitlement_bundle WHERE id = $1::uuid`, bundleID)
		}
		for _, id := range []string{opPrincipal, bystanderPrin, tenantPrin, newOwnerPrincipal} {
			if id != "" {
				_, _ = pool.Pool().Exec(bg, `DELETE FROM principal WHERE id = $1::uuid`, id)
			}
		}
		for _, id := range []string{opPerson, bystanderPers, tenantPers, newOwnerPerson} {
			if id != "" {
				_, _ = pool.Pool().Exec(bg, `DELETE FROM person WHERE id = $1::uuid`, id)
			}
		}
		for _, id := range []string{hostTenant, newTenantID} {
			if id != "" {
				_, _ = pool.Pool().Exec(bg, `DELETE FROM tenant WHERE id = $1::uuid`, id)
			}
		}
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dispatcher := outbox.New(pool.Pool(), writer, outboxRenderers(), logger)

	authenticator, err := auth.Open("dev", auth.Deps{DB: pool, DevAuthSecret: cfg.DevAuthSecret})
	if err != nil {
		t.Fatalf("auth.Open(dev): %v", err)
	}

	iat, exp := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)
	platformToken, err := authdev.MintPlatform([]byte(cfg.DevAuthSecret), opSubject, iat, exp)
	if err != nil {
		t.Fatalf("mint platform token: %v", err)
	}
	bystanderToken, err := authdev.MintPlatform([]byte(cfg.DevAuthSecret), bystanderSub, iat, exp)
	if err != nil {
		t.Fatalf("mint bystander token: %v", err)
	}
	tenantToken, err := authdev.Mint([]byte(cfg.DevAuthSecret), tenantSub, hostTenant, iat, exp)
	if err != nil {
		t.Fatalf("mint tenant token: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/platform/tenants",
		httpx.PlatformOnly(pool.Pool(), logger)(handleCreateTenant(pool, fga, logger)))
	mux.Handle("POST /api/v1/t/{tenant}/entitlement-bundles",
		httpx.TenantMatch(pool, logger)(handleCreateBundle(pool, fga, logger)))

	srv := httptest.NewServer(httpx.Chain(mux,
		httpx.Recover(logger), httpx.RequestID(), httpx.Logging(logger),
		httpx.Authenticate(authenticator, logger)))
	t.Cleanup(srv.Close)

	do := func(t *testing.T, token, method, path string, body any, want int) map[string]any {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&buf).Encode(body); err != nil {
				t.Fatalf("encode: %v", err)
			}
		}
		req, err := http.NewRequest(method, srv.URL+path, &buf)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s %s = %d, want %d; body %s", method, path, resp.StatusCode, want, raw)
		}
		if len(raw) == 0 {
			return nil
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		return out
	}

	// ── ADR-111: the two gates are mutually exclusive ─────────────────

	t.Run("a tenant credential is refused at a platform route", func(t *testing.T) {
		do(t, tenantToken, "POST", "/api/v1/platform/tenants",
			map[string]any{"legal_name": "X", "display_name": "X", "jurisdiction": "IN",
				"owner": map[string]any{"idp_subject": "never-created"}},
			http.StatusForbidden)
	})

	t.Run("a platform credential is refused at a tenant route", func(t *testing.T) {
		do(t, platformToken, "POST", "/api/v1/t/"+hostTenant+"/entitlement-bundles",
			map[string]any{"name": "never created"}, http.StatusForbidden)
	})

	// ── The negative twin, before the positive ────────────────────────

	t.Run("a platform principal without operator may not provision", func(t *testing.T) {
		do(t, bystanderToken, "POST", "/api/v1/platform/tenants",
			map[string]any{"legal_name": "Y", "display_name": "Y", "jurisdiction": "IN",
				"owner": map[string]any{"idp_subject": "never-created-either"}},
			http.StatusForbidden)
	})

	// ── Provisioning ──────────────────────────────────────────────────

	t.Run("the operator provisions a tenant and its owner", func(t *testing.T) {
		out := do(t, platformToken, "POST", "/api/v1/platform/tenants",
			map[string]any{
				"legal_name": "Bombay YMCA (test)", "display_name": "Bombay (test)",
				"jurisdiction": "IN",
				"owner": map[string]any{
					"display_name": "First Secretary", "idp_subject": newOwnerSubject,
				},
			}, http.StatusCreated)

		newTenantID, _ = out["id"].(string)
		if newTenantID == "" {
			t.Fatalf("no tenant id in %v", out)
		}
		owner, _ := out["owner"].(map[string]any)
		newOwnerPrincipal, _ = owner["principal_id"].(string)
		newOwnerPerson, _ = owner["person_id"].(string)
		if newOwnerPrincipal == "" || newOwnerPerson == "" {
			t.Fatalf("provisioning returned no owner: %v", out)
		}
		writtenTuples = append(writtenTuples, outbox.Tuple{
			User: "principal:" + newOwnerPrincipal, Relation: "owner",
			Object: "tenant:" + newTenantID,
		})
	})

	// The grant may lag (ADR-101). That it lags is asserted, not assumed.
	t.Run("the owner grant is pending until the dispatcher runs", func(t *testing.T) {
		var pending int
		if err := pool.Pool().QueryRow(ctx, `
			SELECT count(*) FROM authorization_outbox
			 WHERE aggregate_id = $1::uuid
			   AND dispatched_at IS NULL AND voided_at IS NULL`, newTenantID).Scan(&pending); err != nil {
			t.Fatalf("count pending: %v", err)
		}
		if pending != 1 {
			t.Fatalf("%d pending rows for the new tenant, want 1 (the owner tuple)", pending)
		}
	})

	t.Run("the dispatcher projects the owner tuple", func(t *testing.T) {
		if _, err := dispatcher.Once(ctx); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		var pending int
		if err := pool.Pool().QueryRow(ctx, `
			SELECT count(*) FROM authorization_outbox
			 WHERE aggregate_id = $1::uuid
			   AND dispatched_at IS NULL AND voided_at IS NULL`, newTenantID).Scan(&pending); err != nil {
			t.Fatalf("count pending: %v", err)
		}
		if pending != 0 {
			t.Fatalf("%d rows still pending for the new tenant after dispatch", pending)
		}
	})

	// ── The payoff: ADR-113's actual claim ────────────────────────────

	// If this fails, the tenant was born inert: it exists, provisioning
	// reported success, and nobody can administer it. Note the permission
	// path being exercised — the owner holds `owner`, config endpoints check
	// `admin`, and A1.2's `admin: [principal] or owner` is what joins them.
	t.Run("the new owner can immediately act in the new tenant", func(t *testing.T) {
		ownerToken, err := authdev.Mint([]byte(cfg.DevAuthSecret), newOwnerSubject,
			newTenantID, iat, exp)
		if err != nil {
			t.Fatalf("mint owner token: %v", err)
		}

		out := do(t, ownerToken, "POST", "/api/v1/t/"+newTenantID+"/entitlement-bundles",
			map[string]any{"name": "Mess access"}, http.StatusCreated)
		bundleID, _ = out["id"].(string)
		if bundleID == "" {
			t.Fatalf("no bundle id in %v", out)
		}
		writtenTuples = append(writtenTuples, outbox.Tuple{
			User: "tenant:" + newTenantID, Relation: "tenant",
			Object: "entitlement_bundle:" + bundleID,
		})
		if _, err := dispatcher.Once(ctx); err != nil {
			t.Fatalf("dispatch bundle: %v", err)
		}
	})
}
