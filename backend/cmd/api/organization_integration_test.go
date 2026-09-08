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

// The claim: a tenant admin can register a person, build a unit hierarchy
// whose authority actually propagates down the DAG, and list one unit's
// members without ever seeing another's.
//
// The load-bearing assertion is the SECOND unit. Creating it requires `admin`
// on the FIRST, and the only path to that is `admin from auth_parent` through
// the edge the first request wrote — so if the edge were missing, or written
// in the wrong direction, or never dispatched, this test fails while a test
// that only created top-level units would pass. That is the ADR-107 failure
// shape (a fact written the natural way round and resolving to nothing) in
// the place it would next occur.
func TestOrganization_PeopleAndUnits(t *testing.T) {
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
		tenantID     = "cccccccc-0000-0000-0000-000000000001"
		adminPerson  = "cccccccc-0000-0000-0000-000000000002"
		adminPrinc   = "cccccccc-0000-0000-0000-000000000003"
		plainPerson  = "cccccccc-0000-0000-0000-000000000004"
		plainPrinc   = "cccccccc-0000-0000-0000-000000000005"
		planID       = "cccccccc-0000-0000-0000-000000000006"
		adminSubject = "org-test-admin"
		plainSubject = "org-test-plain"
	)

	seed := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Pool().Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seed(`INSERT INTO tenant (id, legal_name, display_name, jurisdiction, status)
	      VALUES ($1,'Org Test','Org Test','IN','ACTIVE') ON CONFLICT (id) DO NOTHING`, tenantID)
	seed(`INSERT INTO person (id, display_name, status) VALUES ($1,'Org Admin','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, adminPerson)
	seed(`INSERT INTO person (id, display_name, status) VALUES ($1,'Org Plain','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, plainPerson)
	seed(`INSERT INTO principal (id, person_id, idp_subject, kind, status)
	      VALUES ($1,$2,$3,'STAFF','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		adminPrinc, adminPerson, adminSubject)
	seed(`INSERT INTO principal (id, person_id, idp_subject, kind, status)
	      VALUES ($1,$2,$3,'PERSONAL','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		plainPrinc, plainPerson, plainSubject)

	fgaRaw, err := client.NewSdkClient(&client.ClientConfiguration{
		ApiUrl: cfg.FGAAPIURL, StoreId: cfg.FGAStoreID, AuthorizationModelId: cfg.FGAModelID,
	})
	if err != nil {
		t.Fatalf("raw fga client: %v", err)
	}

	// How the admin got their authority is not what this test is about.
	adminTuple := client.ClientTupleKey{
		User: "principal:" + adminPrinc, Relation: "admin", Object: "tenant:" + tenantID,
	}
	if _, err := fgaRaw.Write(ctx).Body(client.ClientWriteRequest{
		Writes: []client.ClientTupleKey{adminTuple},
	}).Execute(); err != nil {
		t.Fatalf("write admin tuple: %v", err)
	}

	var (
		writtenTuples               []outbox.Tuple
		chapterID, deptID, personID string
	)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = fgaRaw.Write(bg).Body(client.ClientWriteRequest{
			Deletes: []client.ClientTupleKeyWithoutCondition{{
				User: adminTuple.User, Relation: adminTuple.Relation, Object: adminTuple.Object,
			}},
		}).Execute()
		_ = writer.DeleteTuples(bg, writtenTuples)
		// Every unit of this tenant, not just the two this test names.
		//
		// Scoped by aggregate_id, because the AuthorizationEdgeCreated
		// payload carries no tenant_id and scoping by that leaves the edge
		// facts behind. And swept by query rather than from variables,
		// because a subtest that was SUPPOSED to be refused and was not
		// creates a unit whose id nothing here holds — which is exactly what
		// happened while drift-testing these gates, and the rows it leaked
		// then failed the configuration test two runs later.
		var unitIDs []string
		_ = pool.InTenantTx(bg, tenantID, func(tx pgx.Tx) error {
			rows, err := tx.Query(bg,
				`SELECT id FROM organizational_unit WHERE tenant_id = $1`, tenantID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					return err
				}
				unitIDs = append(unitIDs, id)
			}
			return rows.Err()
		})
		if len(unitIDs) > 0 {
			_, _ = pool.Pool().Exec(bg,
				`DELETE FROM authorization_outbox WHERE aggregate_id = ANY($1::uuid[])`, unitIDs)
		}
		_ = pool.InTenantTx(bg, tenantID, func(tx pgx.Tx) error {
			for _, q := range []string{
				`DELETE FROM membership WHERE tenant_id = $1`,
				`DELETE FROM membership_plan WHERE tenant_id = $1`,
				`DELETE FROM authorization_edge WHERE tenant_id = $1`,
				`DELETE FROM organizational_unit WHERE tenant_id = $1`,
			} {
				_, _ = tx.Exec(bg, q, tenantID)
			}
			return nil
		})

		for _, id := range []string{adminPrinc, plainPrinc} {
			_, _ = pool.Pool().Exec(bg, `DELETE FROM principal WHERE id = $1`, id)
		}
		for _, id := range []string{adminPerson, plainPerson} {
			_, _ = pool.Pool().Exec(bg, `DELETE FROM person WHERE id = $1`, id)
		}
		if personID != "" {
			_, _ = pool.Pool().Exec(bg, `DELETE FROM person WHERE id = $1::uuid`, personID)
		}
		_, _ = pool.Pool().Exec(bg, `DELETE FROM tenant WHERE id = $1`, tenantID)
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dispatcher := outbox.New(pool.Pool(), writer, outboxRenderers(), logger)

	authenticator, err := auth.Open("dev", auth.Deps{DB: pool, DevAuthSecret: cfg.DevAuthSecret})
	if err != nil {
		t.Fatalf("auth.Open(dev): %v", err)
	}
	tokenFor := func(subject string) string {
		t.Helper()
		token, err := authdev.Mint([]byte(cfg.DevAuthSecret), subject, tenantID,
			time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("mint token for %s: %v", subject, err)
		}
		return token
	}
	adminToken, plainToken := tokenFor(adminSubject), tokenFor(plainSubject)

	mux := http.NewServeMux()
	route := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, httpx.TenantMatch(pool, logger)(h))
	}
	route("POST /api/v1/t/{tenant}/people", handleRegisterPerson(pool, fga, logger))
	route("POST /api/v1/t/{tenant}/units", handleCreateUnit(pool, fga, logger))
	route("GET /api/v1/t/{tenant}/units/{unit}", handleGetUnit(pool, fga, logger))
	route("GET /api/v1/t/{tenant}/units/{unit}/members", handleListUnitMembers(pool, fga, logger))

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
	base := "/api/v1/t/" + tenantID

	// ── ADR-114: a person is registered under a tenant permission ─────
	t.Run("a person is registered through a tenant permission", func(t *testing.T) {
		got := do(t, adminToken, "POST", base+"/people",
			map[string]any{"display_name": "Anjali R", "given_name": "Anjali"},
			http.StatusCreated)
		personID, _ = got["id"].(string)
		if personID == "" {
			t.Fatalf("no person id in %v", got)
		}
		if got["status"] != "ACTIVE" {
			t.Fatalf("status = %v, want ACTIVE", got["status"])
		}
		// family_name was not sent, and A2.3 makes it nullable. Coming back
		// as "" rather than null would make "not recorded" and "recorded as
		// empty" indistinguishable in the register.
		if got["family_name"] != nil {
			t.Fatalf("family_name = %#v, want null for a name that was not given", got["family_name"])
		}
	})

	// Registering publishes nothing. If a tuple ever appears here, the
	// permission stops being safe to hand a front desk.
	t.Run("registering a person publishes no authorization fact", func(t *testing.T) {
		var facts int
		// Scoped to this person rather than counted over the whole table:
		// the dispatcher drains globally, so a table-wide count is really an
		// assertion about every other test's leftovers (the -p 1 hazard).
		if err := pool.Pool().QueryRow(ctx, `
			SELECT count(*) FROM authorization_outbox WHERE aggregate_id = $1::uuid`,
			personID).Scan(&facts); err != nil {
			t.Fatalf("count facts: %v", err)
		}
		if facts != 0 {
			t.Fatalf("%d authorization facts published for a registered person, want 0: "+
				"registering grants nothing, which is what makes ADR-114 safe at a front desk", facts)
		}
	})

	// ── The unit, and the edge that is not derived from anything ──────
	t.Run("a top-level unit is created with its authorization edge", func(t *testing.T) {
		got := do(t, adminToken, "POST", base+"/units", map[string]any{
			"type": "CHAPTER", "name": "Bombay",
			"parent": map[string]any{"type": "tenant", "id": tenantID},
		}, http.StatusCreated)
		chapterID, _ = got["id"].(string)
		if chapterID == "" {
			t.Fatalf("no unit id in %v", got)
		}
		parents, _ := got["auth_parents"].([]any)
		if len(parents) != 1 {
			t.Fatalf("auth_parents = %v, want exactly the one that was named", parents)
		}

		// Two facts, not one: the tenant edge (ADR-018) and the DAG edge.
		assertPendingFacts(t, pool, chapterID, 2)
		if _, err := dispatcher.Once(ctx); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		assertPendingFacts(t, pool, chapterID, 0)
		writtenTuples = append(writtenTuples,
			outbox.Tuple{User: "tenant:" + tenantID, Relation: "tenant",
				Object: "organizational_unit:" + chapterID},
			outbox.Tuple{User: "tenant:" + tenantID, Relation: "auth_parent",
				Object: "organizational_unit:" + chapterID})
	})

	// The one that would catch a missing or reversed edge.
	t.Run("authority propagates down the edge to a nested unit", func(t *testing.T) {
		got := do(t, adminToken, "POST", base+"/units", map[string]any{
			"type": "DEPARTMENT", "name": "Physical Education",
			"parent": map[string]any{"type": "organizational_unit", "id": chapterID},
		}, http.StatusCreated)
		deptID, _ = got["id"].(string)
		if deptID == "" {
			t.Fatalf("no unit id in %v", got)
		}
		assertPendingFacts(t, pool, deptID, 2)
		if _, err := dispatcher.Once(ctx); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		assertPendingFacts(t, pool, deptID, 0)
		writtenTuples = append(writtenTuples,
			outbox.Tuple{User: "tenant:" + tenantID, Relation: "tenant",
				Object: "organizational_unit:" + deptID},
			outbox.Tuple{User: "organizational_unit:" + chapterID, Relation: "auth_parent",
				Object: "organizational_unit:" + deptID})
	})

	t.Run("a unit reads back with the parent it was given", func(t *testing.T) {
		got := do(t, adminToken, "GET", base+"/units/"+deptID, nil, http.StatusOK)
		parents, _ := got["auth_parents"].([]any)
		if len(parents) != 1 {
			t.Fatalf("auth_parents = %v, want one", parents)
		}
		p, _ := parents[0].(map[string]any)
		if p["type"] != "organizational_unit" || p["id"] != chapterID {
			t.Fatalf("auth_parent = %v, want the chapter", p)
		}
		// org_parent_id was never sent. 05.1.3 keeps the three containments
		// apart, so creating the edge must not have back-filled it.
		if got["org_parent_id"] != nil {
			t.Fatalf("org_parent_id = %v; the authorization edge derived an "+
				"ORGANIZATIONAL parent, which 05.1.3 forbids", got["org_parent_id"])
		}
	})

	// ── ADR-104's list: the scope is checked, never the row ───────────
	t.Run("a unit's member list holds its own memberships and no others", func(t *testing.T) {
		// Three people, not one: one_active_membership_per_tenant is a
		// partial unique index on (tenant_id, person_id) WHERE state =
		// 'ACTIVE', so a person holds at most one live membership here.
		seedMembership := func(number, personID string, unit *string) {
			t.Helper()
			if err := pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO membership
					    (tenant_id, person_id, plan_id, number, state, admitted_at, org_unit_id)
					VALUES ($1,$2::uuid,$3::uuid,$4,'ACTIVE',now(),$5::uuid)`,
					tenantID, personID, planID, number, unit)
				return err
			}); err != nil {
				t.Fatalf("seed membership %s: %v", number, err)
			}
		}
		if err := pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO membership_plan (id, tenant_id, code, name, acquisition, duration, status)
				VALUES ($1,$2,'org-test','Org Test','PURCHASED','ANNUAL','OPEN')
				ON CONFLICT (id) DO NOTHING`, planID, tenantID)
			return err
		}); err != nil {
			t.Fatalf("seed plan: %v", err)
		}
		seedMembership("ORG-CHAPTER", plainPerson, &chapterID)
		seedMembership("ORG-DEPT", adminPerson, &deptID)
		// The person registered through the API at the top of this test,
		// admitted at association level: org_unit_id NULL, which A2.4 and
		// ADR-104 make invisible to every unit list (11.2).
		seedMembership("ORG-ASSOCIATION", personID, nil)

		got := do(t, adminToken, "GET", base+"/units/"+chapterID+"/members", nil, http.StatusOK)
		items, _ := got["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("the chapter listed %d memberships, want 1: a descendant's "+
				"members folded up into the parent's page, or the association-level "+
				"membership appeared under a unit. Items: %v", len(items), items)
		}
		first, _ := items[0].(map[string]any)
		if first["number"] != "ORG-CHAPTER" {
			t.Fatalf("listed %v, want the chapter's own membership", first["number"])
		}

		// The other half of ADR-104's asymmetry: authority DOES reach the
		// child, so naming the child lists it. If this were forbidden the
		// under-report would be an over-restriction instead.
		got = do(t, adminToken, "GET", base+"/units/"+deptID+"/members", nil, http.StatusOK)
		items, _ = got["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("the department listed %d memberships, want 1; a caller "+
				"authorized at the parent must reach a child by naming it", len(items))
		}
	})

	// ── Refusals ──────────────────────────────────────────────────────
	t.Run("a caller without may_register_person is refused", func(t *testing.T) {
		do(t, plainToken, "POST", base+"/people",
			map[string]any{"display_name": "Not Allowed"}, http.StatusForbidden)
	})

	t.Run("a caller without admin on the parent may not create a unit", func(t *testing.T) {
		do(t, plainToken, "POST", base+"/units", map[string]any{
			"type": "CHAPTER", "name": "Sneaky",
			"parent": map[string]any{"type": "tenant", "id": tenantID},
		}, http.StatusForbidden)
	})

	// Without these two, removing the authorizeUnit call from either GET
	// handler leaves this file green: every other case uses the tenant
	// admin, who is allowed. That is the shape session 5 found when
	// deleting the PlatformOnly middleware did not fail a single test.
	t.Run("a caller who is not a member of the unit may not read it", func(t *testing.T) {
		do(t, plainToken, "GET", base+"/units/"+chapterID, nil, http.StatusForbidden)
	})

	t.Run("a caller without member_read may not list the unit's members", func(t *testing.T) {
		do(t, plainToken, "GET", base+"/units/"+chapterID+"/members", nil, http.StatusForbidden)
	})

	// A3.3: absent and not-permitted must be indistinguishable. The check
	// runs against a unit with no tuples, so it denies before any row is
	// read — which is what makes the two cases the same response.
	t.Run("a unit that does not exist is refused, not reported absent", func(t *testing.T) {
		do(t, adminToken, "GET", base+"/units/cccccccc-0000-0000-0000-0000000000ff",
			nil, http.StatusForbidden)
	})

	t.Run("a unit with no authorization parent is refused", func(t *testing.T) {
		got := do(t, adminToken, "POST", base+"/units",
			map[string]any{"type": "CHAPTER", "name": "Orphan"}, http.StatusBadRequest)
		detail, _ := got["error"].(map[string]any)
		if detail["code"] != "invalid_request" {
			t.Fatalf("error code = %v, want invalid_request", detail["code"])
		}
	})

	t.Run("a unit type outside 05.1.2's six is refused", func(t *testing.T) {
		do(t, adminToken, "POST", base+"/units", map[string]any{
			"type": "FRANCHISE", "name": "Nope",
			"parent": map[string]any{"type": "tenant", "id": tenantID},
		}, http.StatusBadRequest)
	})

	// ADR-105 step 1: naming another tenant as the parent is its own code,
	// because that DENY is the one evidencing a probe for tenants the
	// caller cannot reach.
	t.Run("naming another tenant as parent is a tenant mismatch", func(t *testing.T) {
		got := do(t, adminToken, "POST", base+"/units", map[string]any{
			"type": "CHAPTER", "name": "Elsewhere",
			"parent": map[string]any{"type": "tenant", "id": "cccccccc-0000-0000-0000-0000000000ee"},
		}, http.StatusForbidden)
		detail, _ := got["error"].(map[string]any)
		if detail["code"] != "tenant_mismatch" {
			t.Fatalf("error code = %v, want tenant_mismatch", detail["code"])
		}
	})
}

// assertPendingFacts counts the undispatched facts for ONE aggregate.
//
// Scoped rather than table-wide on purpose. The dispatcher drains the whole
// authorization_outbox by design, so a count over the table is an assertion
// about what every other package left behind — which is the -p 1 hazard
// stated as a test rather than as a flag.
func assertPendingFacts(t *testing.T, pool *db.DB, aggregateID string, want int) {
	t.Helper()
	var got int
	if err := pool.Pool().QueryRow(context.Background(), `
		SELECT count(*) FROM authorization_outbox
		 WHERE aggregate_id = $1::uuid AND dispatched_at IS NULL AND voided_at IS NULL`,
		aggregateID).Scan(&got); err != nil {
		t.Fatalf("count pending facts: %v", err)
	}
	if got != want {
		t.Fatalf("%d pending facts for %s, want %d", got, aggregateID, want)
	}
}
