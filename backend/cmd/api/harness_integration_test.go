//go:build integration && dev

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	"github.com/nandgator/ymca/backend/internal/outbox"
)

// harness is the shared setup for the A3.10 and A3.11 tests: one tenant, its
// administrator, the REAL routes (registerRoutes, not a hand-picked few), a
// dispatcher, and a cleanup that proves it cleaned.
//
// The older integration tests each build their own server and register the
// routes they touch. That is how a route can exist in main and be tested
// nowhere. Nothing here lists a route.
type harness struct {
	t          *testing.T
	ctx        context.Context
	cfg        config.Config
	pool       *db.DB
	fga        *authz.FGA
	writer     *outbox.FGAWriter
	fgaRaw     *client.OpenFgaClient
	dispatcher *outbox.Dispatcher
	srv        *httptest.Server

	tenantID    string
	adminPerson string
	adminPrinc  string
	adminTok    string
	persons     []string
	principals  []string
	granted     []outbox.Tuple
}

// newHarness provisions a tenant whose id is fixed per test, so a run that
// crashed leaves something the next run purges rather than something that
// accumulates. n distinguishes tests; ids are e0000000-...-0000000000nn.
func newHarness(t *testing.T, n int) *harness {
	t.Helper()
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
	fga, err := authz.NewFGA(cfg)
	if err != nil {
		t.Fatalf("authz.NewFGA: %v", err)
	}
	writer, err := outbox.NewFGAWriter(cfg)
	if err != nil {
		t.Fatalf("NewFGAWriter: %v", err)
	}
	raw, err := client.NewSdkClient(&client.ClientConfiguration{
		ApiUrl: cfg.FGAAPIURL, StoreId: cfg.FGAStoreID, AuthorizationModelId: cfg.FGAModelID,
	})
	if err != nil {
		t.Fatalf("raw fga client: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	h := &harness{
		t: t, ctx: ctx, cfg: cfg, pool: pool, fga: fga, writer: writer, fgaRaw: raw,
		tenantID:    fmt.Sprintf("e0000000-0000-0000-0000-0000000000%02x", n),
		adminPerson: fmt.Sprintf("e0000000-0000-0000-0001-0000000000%02x", n),
		adminPrinc:  fmt.Sprintf("e0000000-0000-0000-0002-0000000000%02x", n),
	}
	h.dispatcher = outbox.New(pool.Pool(), writer, outboxRenderers(), logger)

	authenticator, err := auth.Open("dev", auth.Deps{DB: pool, DevAuthSecret: cfg.DevAuthSecret})
	if err != nil {
		t.Fatalf("auth.Open(dev): %v", err)
	}
	mux := http.NewServeMux()
	registerRoutes(mux, pool, fga, logger)
	h.srv = httptest.NewServer(httpx.Chain(mux,
		httpx.Recover(logger), httpx.RequestID(), httpx.Logging(logger),
		httpx.Authenticate(authenticator, logger)))

	// Cleanup is registered BEFORE seeding, so a seed that dies halfway is
	// still cleaned, and runs once up front to purge a crashed predecessor.
	h.purge()
	t.Cleanup(func() {
		h.srv.Close()
		h.purge()
		pool.Close()
	})

	h.seed(`INSERT INTO tenant (id, legal_name, display_name, jurisdiction, status)
	        VALUES ($1,'Harness','Harness','IN','ACTIVE')`, h.tenantID)
	h.adminTok = h.newLogin(h.adminPerson, h.adminPrinc, "harness-admin-"+h.tenantID, "STAFF", "Harness Admin")
	h.grant("principal:"+h.adminPrinc, "admin", "tenant:"+h.tenantID)
	return h
}

func (h *harness) seed(sql string, args ...any) {
	h.t.Helper()
	if _, err := h.pool.Pool().Exec(h.ctx, sql, args...); err != nil {
		h.t.Fatalf("seed: %v", err)
	}
}

// newLogin creates a person and a principal for it and returns a token.
func (h *harness) newLogin(person, princ, subject, kind, name string) string {
	h.t.Helper()
	h.seed(`INSERT INTO person (id, display_name, status) VALUES ($1,$2,'ACTIVE')`, person, name)
	h.seed(`INSERT INTO principal (id, person_id, idp_subject, kind, status)
	        VALUES ($1,$2,$3,$4,'ACTIVE')`, princ, person, subject, kind)
	h.persons = append(h.persons, person)
	h.principals = append(h.principals, princ)
	tok, err := authdev.Mint([]byte(h.cfg.DevAuthSecret), subject, h.tenantID,
		time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	if err != nil {
		h.t.Fatalf("mint: %v", err)
	}
	return tok
}

// grant writes a tuple directly. Reserved for what a test is NOT about: how
// somebody came to hold authority. It is deleted again on purge.
func (h *harness) grant(user, relation, object string) {
	h.t.Helper()
	if _, err := h.fgaRaw.Write(h.ctx).Body(client.ClientWriteRequest{
		Writes: []client.ClientTupleKey{{User: user, Relation: relation, Object: object}},
	}).Execute(); err != nil {
		h.t.Fatalf("grant %s %s %s: %v", user, relation, object, err)
	}
	h.granted = append(h.granted, outbox.Tuple{User: user, Relation: relation, Object: object})
}

// dispatch drains the outbox. Grants lag (8.3), so anything that needs a new
// tuple to resolve calls this first.
func (h *harness) dispatch() {
	h.t.Helper()
	for i := 0; i < 20; i++ {
		n, err := h.dispatcher.Once(h.ctx)
		if err != nil {
			h.t.Fatalf("dispatch: %v", err)
		}
		if n == 0 {
			return
		}
	}
	h.t.Fatal("dispatcher never drained")
}

// call makes a request and returns status and decoded body (nil if empty).
func (h *harness) call(tok, method, path string, body any, headers ...string) (int, map[string]any) {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			h.t.Fatalf("encode: %v", err)
		}
	}
	req, err := http.NewRequest(method, h.srv.URL+"/api/v1/t/"+h.tenantID+path, &buf)
	if err != nil {
		h.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			h.t.Fatalf("decode %s: %v", raw, err)
		}
	}
	return resp.StatusCode, out
}

// must is call with a status assertion.
func (h *harness) must(tok, method, path string, body any, want int, headers ...string) map[string]any {
	h.t.Helper()
	status, out := h.call(tok, method, path, body, headers...)
	if status != want {
		h.t.Fatalf("%s %s = %d, want %d; body %v", method, path, status, want, out)
	}
	return out
}

// refused asserts a status AND an error code. Two failure modes that share a
// status must be told apart by something only one of them produces.
func (h *harness) refused(tok, method, path string, body any, wantStatus int, wantCode string, headers ...string) {
	h.t.Helper()
	status, out := h.call(tok, method, path, body, headers...)
	if status != wantStatus {
		h.t.Fatalf("%s %s = %d, want %d (%s); body %v", method, path, status, wantStatus, wantCode, out)
	}
	e, _ := out["error"].(map[string]any)
	if got, _ := e["code"].(string); got != wantCode {
		h.t.Fatalf("%s %s: error code %q, want %q; body %v", method, path, got, wantCode, out)
	}
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// purge removes everything this harness's tenant owns, deletes the tuples it
// projected, and then PROVES nothing is left pending. A cleanup that cannot
// fail the test cannot tell you it did not work (HANDOFF §3).
func (h *harness) purge() {
	bg := context.Background()
	var aggregates []string
	_ = h.pool.InTenantTx(bg, h.tenantID, func(tx pgx.Tx) error {
		for _, table := range []string{
			"consumption_type", "membership", "membership_plan",
			"entitlement_bundle", "invoice", "organizational_unit",
		} {
			rows, err := tx.Query(bg, `SELECT id::text FROM `+table+` WHERE tenant_id = $1`, h.tenantID)
			if err != nil {
				continue
			}
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					aggregates = append(aggregates, id)
				}
			}
			rows.Close()
		}
		return nil
	})

	// The tuples the dispatcher projected: reconstructed from the outbox's own
	// fence columns, so nothing has to be remembered by the test.
	var tuples []outbox.Tuple
	if len(aggregates) > 0 {
		rows, err := h.pool.Pool().Query(bg, `
			SELECT fence_subject, fence_relation, fence_object FROM authorization_outbox
			 WHERE aggregate_id = ANY($1::uuid[]) AND dispatched_at IS NOT NULL
			   AND fence_subject IS NOT NULL`, aggregates)
		if err == nil {
			for rows.Next() {
				var t outbox.Tuple
				if rows.Scan(&t.User, &t.Relation, &t.Object) == nil {
					tuples = append(tuples, t)
				}
			}
			rows.Close()
		}
	}
	tuples = append(tuples, h.granted...)
	if len(tuples) > 0 {
		_ = h.writer.DeleteTuples(bg, tuples)
	}
	h.granted = nil
	if len(aggregates) > 0 {
		_, _ = h.pool.Pool().Exec(bg,
			`DELETE FROM authorization_outbox WHERE aggregate_id = ANY($1::uuid[])`, aggregates)
	}

	_ = h.pool.InTenantTx(bg, h.tenantID, func(tx pgx.Tx) error {
		for _, q := range []string{
			`DELETE FROM consumption_record_option WHERE consumption_record_id IN
			   (SELECT id FROM consumption_record WHERE tenant_id = $1)`,
			`DELETE FROM consumption_record WHERE tenant_id = $1`,
			`DELETE FROM consumption_period_close WHERE tenant_id = $1`,
			`DELETE FROM consumption_obligation WHERE tenant_id = $1`,
			`DELETE FROM expected_absence WHERE tenant_id = $1`,
			`DELETE FROM consumption_option WHERE tenant_id = $1`,
			`DELETE FROM consumption_rate WHERE tenant_id = $1`,
			`DELETE FROM payment WHERE tenant_id = $1`,
			`DELETE FROM charge_component WHERE charge_id IN
			   (SELECT c.id FROM charge c JOIN invoice i ON i.id = c.invoice_id WHERE i.tenant_id = $1)`,
			`DELETE FROM charge WHERE invoice_id IN (SELECT id FROM invoice WHERE tenant_id = $1)`,
			`DELETE FROM invoice WHERE tenant_id = $1`,
			`DELETE FROM invoice_number_series WHERE tenant_id = $1`,
			`DELETE FROM idempotency_key WHERE tenant_id = $1`,
			`DELETE FROM membership WHERE tenant_id = $1`,
			`DELETE FROM membership_plan WHERE tenant_id = $1`,
			`DELETE FROM consumption_type WHERE tenant_id = $1`,
			`DELETE FROM entitlement_bundle WHERE tenant_id = $1`,
		} {
			_, _ = tx.Exec(bg, q, h.tenantID)
		}
		return nil
	})
	for _, p := range h.persons {
		_, _ = h.pool.Pool().Exec(bg, `DELETE FROM party WHERE person_id = $1::uuid`, p)
	}
	_, _ = h.pool.Pool().Exec(bg, `DELETE FROM party WHERE organization_id = $1::uuid`, h.tenantID)
	for _, id := range h.principals {
		_, _ = h.pool.Pool().Exec(bg, `DELETE FROM principal WHERE id = $1::uuid`, id)
	}
	for _, id := range h.persons {
		_, _ = h.pool.Pool().Exec(bg, `DELETE FROM person WHERE id = $1::uuid`, id)
	}
	_, _ = h.pool.Pool().Exec(bg, `DELETE FROM tenant WHERE id = $1::uuid`, h.tenantID)

	// The proof. Payload-scoped on purpose: this is a VERIFICATION that
	// catches a fact the aggregate-scoped delete missed, not a delete.
	var left int
	if err := h.pool.Pool().QueryRow(bg, `
		SELECT count(*) FROM authorization_outbox
		 WHERE dispatched_at IS NULL AND voided_at IS NULL
		   AND (payload->>'tenant_id' = $1 OR aggregate_id = ANY($2::uuid[]))`,
		h.tenantID, aggregates).Scan(&left); err == nil && left != 0 {
		h.t.Errorf("cleanup left %d pending outbox rows for tenant %s", left, h.tenantID)
	}
}

// count runs a single-integer query inside the tenant's transaction. The
// policied tables raise on a bare pool connection (A2.1's fail-closed
// behaviour), which is the point of them.
func (h *harness) count(sql string, args ...any) int {
	h.t.Helper()
	var n int
	err := h.pool.InTenantTx(h.ctx, h.tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(h.ctx, sql, args...).Scan(&n)
	})
	if err != nil {
		h.t.Fatalf("count: %v", err)
	}
	return n
}
