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

// The claim: the configuration endpoints, the dispatcher and ADR-107 together
// make a member able to record a meal. Before this chain existed, a member
// admitted through the API held no entitlements and may_record denied
// forever — correctly, and invisibly.
//
// Nothing here writes a tuple directly. Every tuple under test arrives
// because a handler enqueued a fact and the dispatcher rendered it.
func TestConfigurationChain_EndToEnd(t *testing.T) {
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
		tenantID     = "dddddddd-0000-0000-0000-000000000001"
		adminPerson  = "dddddddd-0000-0000-0000-000000000002"
		adminPrinc   = "dddddddd-0000-0000-0000-000000000003"
		memberPers   = "dddddddd-0000-0000-0000-000000000004"
		memberPrinc  = "dddddddd-0000-0000-0000-000000000005"
		membershipID = "dddddddd-0000-0000-0000-000000000006"
		adminSubject = "config-test-admin"
	)

	seed := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Pool().Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seed(`INSERT INTO tenant (id, legal_name, display_name, jurisdiction, status)
	      VALUES ($1,'Config Test','Config Test','IN','ACTIVE') ON CONFLICT (id) DO NOTHING`, tenantID)
	seed(`INSERT INTO person (id, display_name, status) VALUES ($1,'Config Admin','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, adminPerson)
	seed(`INSERT INTO person (id, display_name, status) VALUES ($1,'Config Member','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, memberPers)
	seed(`INSERT INTO principal (id, person_id, idp_subject, kind, status)
	      VALUES ($1,$2,$3,'STAFF','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		adminPrinc, adminPerson, adminSubject)
	seed(`INSERT INTO principal (id, person_id, idp_subject, kind, status)
	      VALUES ($1,$2,'config-test-member','PERSONAL','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		memberPrinc, memberPers)

	fgaRaw, err := client.NewSdkClient(&client.ClientConfiguration{
		ApiUrl: cfg.FGAAPIURL, StoreId: cfg.FGAStoreID, AuthorizationModelId: cfg.FGAModelID,
	})
	if err != nil {
		t.Fatalf("raw fga client: %v", err)
	}

	// The admin's authority is a direct tuple: this test is about the
	// configuration chain, not about how the configurer got their authority.
	adminTuple := client.ClientTupleKey{
		User: "principal:" + adminPrinc, Relation: "admin", Object: "tenant:" + tenantID,
	}
	if _, err := fgaRaw.Write(ctx).Body(client.ClientWriteRequest{
		Writes: []client.ClientTupleKey{adminTuple},
	}).Execute(); err != nil {
		t.Fatalf("write admin tuple: %v", err)
	}

	var (
		writtenTuples            []outbox.Tuple
		bundleID, typeID, planID string
	)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = fgaRaw.Write(bg).Body(client.ClientWriteRequest{
			Deletes: []client.ClientTupleKeyWithoutCondition{{
				User: adminTuple.User, Relation: adminTuple.Relation, Object: adminTuple.Object,
			}},
		}).Execute()
		_ = writer.DeleteTuples(bg, writtenTuples)
		_ = pool.InTenantTx(bg, tenantID, func(tx pgx.Tx) error {
			for _, q := range []string{
				// Placeholder; the outbox delete runs separately below,
				// scoped by aggregate_id.
				`SELECT $1::uuid`,
				`DELETE FROM membership WHERE tenant_id = $1`,
				`DELETE FROM membership_plan WHERE tenant_id = $1`,
				`DELETE FROM consumption_type WHERE tenant_id = $1`,
				`DELETE FROM entitlement_bundle WHERE tenant_id = $1`,
			} {
				_, _ = tx.Exec(bg, q, tenantID)
			}
			return nil
		})
		// Scoped by aggregate_id, which every fact carries. An earlier
		// version scoped by payload->>'tenant_id' and silently missed two of
		// the five events, whose payloads have no tenant_id -- so a failed
		// run left a pending row and the NEXT run counted six. Found by
		// re-running, which is the only thing that finds it.
		for _, id := range []string{bundleID, typeID, planID} {
			if id != "" {
				_, _ = pool.Pool().Exec(bg,
					`DELETE FROM authorization_outbox WHERE aggregate_id = $1::uuid`, id)
			}
		}
		for _, id := range []string{adminPrinc, memberPrinc} {
			_, _ = pool.Pool().Exec(bg, `DELETE FROM principal WHERE id = $1`, id)
		}
		for _, id := range []string{adminPerson, memberPers} {
			_, _ = pool.Pool().Exec(bg, `DELETE FROM person WHERE id = $1`, id)
		}
		_, _ = pool.Pool().Exec(bg, `DELETE FROM tenant WHERE id = $1`, tenantID)
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dispatcher := outbox.New(pool.Pool(), writer, outboxRenderers(), logger)

	authenticator, err := auth.Open("dev", auth.Deps{DB: pool, DevAuthSecret: cfg.DevAuthSecret})
	if err != nil {
		t.Fatalf("auth.Open(dev): %v", err)
	}
	token, err := authdev.Mint([]byte(cfg.DevAuthSecret), adminSubject, tenantID,
		time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	mux := http.NewServeMux()
	route := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, httpx.TenantMatch(pool, logger)(h))
	}
	route("POST /api/v1/t/{tenant}/entitlement-bundles", handleCreateBundle(pool, fga, logger))
	route("GET /api/v1/t/{tenant}/entitlement-bundles", handleListBundles(pool, fga, logger))
	route("POST /api/v1/t/{tenant}/entitlement-bundles/{bundle}/entitles", handleEntitle(pool, fga, logger))
	route("POST /api/v1/t/{tenant}/membership-plans", handleCreatePlan(pool, fga, logger))
	route("POST /api/v1/t/{tenant}/consumption-types", handleCreateConsumptionType(pool, fga, logger))

	srv := httptest.NewServer(httpx.Chain(mux,
		httpx.Recover(logger), httpx.RequestID(), httpx.Logging(logger),
		httpx.Authenticate(authenticator, logger)))
	t.Cleanup(srv.Close)

	do := func(t *testing.T, method, path string, body any, want int) map[string]any {
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

	// ── Build the chain entirely through the API ──────────────────────
	bundle := do(t, "POST", "/api/v1/t/"+tenantID+"/entitlement-bundles",
		map[string]any{"name": "Mess access"}, http.StatusCreated)
	bundleID, _ = bundle["id"].(string)
	if bundleID == "" {
		t.Fatalf("no bundle id in %v", bundle)
	}

	ctype := do(t, "POST", "/api/v1/t/"+tenantID+"/consumption-types",
		map[string]any{"name": "Dinner", "obligates": true, "recurrence": "DAILY",
			"record_mode": "EITHER"}, http.StatusCreated)
	typeID, _ = ctype["id"].(string)

	do(t, "POST", "/api/v1/t/"+tenantID+"/entitlement-bundles/"+bundleID+"/entitles",
		map[string]any{"object_type": "consumption_type", "object_id": typeID},
		http.StatusNoContent)

	plan := do(t, "POST", "/api/v1/t/"+tenantID+"/membership-plans",
		map[string]any{"code": "resident", "name": "Hostel Resident",
			"acquisition": "PURCHASED", "duration": "ANNUAL",
			"entitlement_bundle_id": bundleID}, http.StatusCreated)
	planID, _ = plan["id"].(string)

	writtenTuples = []outbox.Tuple{
		{User: "tenant:" + tenantID, Relation: "tenant", Object: "entitlement_bundle:" + bundleID},
		{User: "tenant:" + tenantID, Relation: "tenant", Object: "consumption_type:" + typeID},
		{User: "tenant:" + tenantID, Relation: "tenant", Object: "membership_plan:" + planID},
		{User: "entitlement_bundle:" + bundleID + "#beneficiary", Relation: "entitled",
			Object: "consumption_type:" + typeID},
		{User: "membership_plan:" + planID, Relation: "via_plan",
			Object: "entitlement_bundle:" + bundleID},
	}

	// Nothing has reached OpenFGA yet: these are grants, and 8.3 says grants
	// may lag. That they lag is a property worth asserting, not assuming.
	t.Run("grants are pending until the dispatcher runs", func(t *testing.T) {
		var pending int
		if err := pool.Pool().QueryRow(ctx, `
			SELECT count(*) FROM authorization_outbox
			 WHERE dispatched_at IS NULL AND voided_at IS NULL`).Scan(&pending); err != nil {
			t.Fatalf("count pending: %v", err)
		}
		if pending != 5 {
			t.Fatalf("%d pending outbox rows, want 5 (bundle, type, plan tenant edges, entitles, via_plan)", pending)
		}
	})

	t.Run("the dispatcher projects every fact", func(t *testing.T) {
		n, err := dispatcher.Once(ctx)
		if err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if n != 5 {
			t.Fatalf("dispatched %d rows, want 5", n)
		}
	})

	// ── The payoff ────────────────────────────────────────────────────
	// A membership on the plan, and the ADR-107 tuple admission will write.
	// Written directly here because admission is the next commit; what this
	// test proves is that the CONFIGURATION the chain built is what makes
	// that tuple mean something.
	t.Run("a member on the plan may record", func(t *testing.T) {
		if err := pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO membership (id, tenant_id, person_id, plan_id, number, state, admitted_at)
				VALUES ($1,$2,$3,$4::uuid,'CFG-1','ACTIVE',now())
				ON CONFLICT (id) DO NOTHING`, membershipID, tenantID, memberPers, planID)
			return err
		}); err != nil {
			t.Fatalf("seed membership: %v", err)
		}

		memberTuples := []outbox.Tuple{
			{User: "tenant:" + tenantID, Relation: "tenant", Object: "membership:" + membershipID},
			{User: "person:" + memberPers, Relation: "holder", Object: "membership:" + membershipID},
			// ADR-107: one covered_member tuple per membership, at admission.
			{User: "membership:" + membershipID + "#covered", Relation: "covered_member",
				Object: "membership_plan:" + planID},
		}
		if err := writer.WriteTuples(ctx, memberTuples); err != nil {
			t.Fatalf("write membership tuples: %v", err)
		}
		writtenTuples = append(writtenTuples, memberTuples...)

		allowed, err := fga.Check(ctx, "person:"+memberPers, "may_record",
			"consumption_type:"+typeID, nil)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if !allowed {
			t.Fatal("a member on a plan whose bundle entitles the type may NOT record — " +
				"the chain bundle -> entitled -> via_plan -> covered_member is broken")
		}
	})

	// The negative twin. Without it the ALLOW above could pass for the wrong
	// reason — a model where everyone may record would satisfy it too.
	t.Run("a person with no membership may not record", func(t *testing.T) {
		allowed, err := fga.Check(ctx, "person:"+adminPerson, "may_record",
			"consumption_type:"+typeID, nil)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if allowed {
			t.Fatal("a person on no plan may record; the entitlement path grants everyone")
		}
	})

	t.Run("the list reports what was created", func(t *testing.T) {
		got := do(t, "GET", "/api/v1/t/"+tenantID+"/entitlement-bundles?limit=10", nil, http.StatusOK)
		items, _ := got["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("listed %d bundles, want 1", len(items))
		}
		if got["next_cursor"] != "" {
			t.Fatalf("next_cursor = %v on a short page, want empty", got["next_cursor"])
		}
	})

	t.Run("a malformed cursor is refused, not ignored", func(t *testing.T) {
		do(t, "GET", "/api/v1/t/"+tenantID+"/entitlement-bundles?cursor=nonsense%21",
			nil, http.StatusBadRequest)
	})

	t.Run("an object type that cannot be entitled is refused", func(t *testing.T) {
		do(t, "POST", "/api/v1/t/"+tenantID+"/entitlement-bundles/"+bundleID+"/entitles",
			map[string]any{"object_type": "tenant", "object_id": tenantID},
			http.StatusBadRequest)
	})

	// 6.1 runs before anything else. A caller with no tenant admin gets 403
	// and no row is created.
	t.Run("a caller without admin is refused", func(t *testing.T) {
		memberToken, err := authdev.Mint([]byte(cfg.DevAuthSecret), "config-test-member",
			tenantID, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("mint member token: %v", err)
		}
		var buf bytes.Buffer
		_ = json.NewEncoder(&buf).Encode(map[string]any{"name": "Sneaky"})
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/t/"+tenantID+"/entitlement-bundles", &buf)
		req.Header.Set("Authorization", "Bearer "+memberToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 403; body %s", resp.StatusCode, body)
		}
	})
}
