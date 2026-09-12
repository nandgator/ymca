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
	"strings"
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

// The claim: admission through the API is what makes a person a member who
// may actually do something, and the three facts it publishes are the whole
// of that. ADR-107's covered_member tuple is three lines and its absence is
// silent — a membership without it holds no entitlements, every may_record
// check denies correctly, and nothing anywhere reports why. So the assertion
// that matters is an ALLOW that the same person did not have a moment
// earlier, not a count of rows.
//
// Nothing here writes a tuple for the membership. The configuration chain is
// built through the API, admission is performed through the API, and every
// tuple under test arrives because a handler enqueued a fact and the
// dispatcher rendered it. The one direct tuple write is the admin's own
// authority, which this test is not about.
func TestAdmission_EndToEnd(t *testing.T) {
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
		tenantID     = "ffffffff-0000-0000-0000-000000000001"
		adminPerson  = "ffffffff-0000-0000-0000-000000000002"
		adminPrinc   = "ffffffff-0000-0000-0000-000000000003"
		memberPerson = "ffffffff-0000-0000-0000-000000000004"
		memberPrinc  = "ffffffff-0000-0000-0000-000000000005"
		otherPerson  = "ffffffff-0000-0000-0000-000000000006"
		closedPlanID = "ffffffff-0000-0000-0000-000000000007"
		absentPlanID = "ffffffff-0000-0000-0000-0000000000ff"

		adminSubject  = "admission-test-admin"
		memberSubject = "admission-test-member"
	)

	seed := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Pool().Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seed(`INSERT INTO tenant (id, legal_name, display_name, jurisdiction, status)
	      VALUES ($1,'Admission Test','Admission Test','IN','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		tenantID)
	seed(`INSERT INTO person (id, display_name, status) VALUES ($1,'Admission Admin','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, adminPerson)
	seed(`INSERT INTO person (id, display_name, status) VALUES ($1,'Admission Member','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, memberPerson)
	seed(`INSERT INTO person (id, display_name, status) VALUES ($1,'Admission Other','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, otherPerson)
	seed(`INSERT INTO principal (id, person_id, idp_subject, kind, status)
	      VALUES ($1,$2,$3,'STAFF','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		adminPrinc, adminPerson, adminSubject)
	seed(`INSERT INTO principal (id, person_id, idp_subject, kind, status)
	      VALUES ($1,$2,$3,'PERSONAL','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		memberPrinc, memberPerson, memberSubject)

	fgaRaw, err := client.NewSdkClient(&client.ClientConfiguration{
		ApiUrl: cfg.FGAAPIURL, StoreId: cfg.FGAStoreID, AuthorizationModelId: cfg.FGAModelID,
	})
	if err != nil {
		t.Fatalf("raw fga client: %v", err)
	}

	// The admin's authority is a direct tuple: how the configurer got their
	// authority is the platform test's subject, not this one's. `admin`
	// reaches may_approve_membership through A1.2's `or admin`, which is
	// why this one tuple is enough to both configure and admit.
	adminTuple := client.ClientTupleKey{
		User: "principal:" + adminPrinc, Relation: "admin", Object: "tenant:" + tenantID,
	}
	if _, err := fgaRaw.Write(ctx).Body(client.ClientWriteRequest{
		Writes: []client.ClientTupleKey{adminTuple},
	}).Execute(); err != nil {
		t.Fatalf("write admin tuple: %v", err)
	}

	var bundleID, typeID, planID string

	// Cleanup reads the aggregate ids back out of the database rather than
	// trusting the variables above, and renders the tuples to delete from
	// the outbox's own fence columns. Both are session 6's lesson: a subtest
	// that was supposed to be refused and was not leaves a membership whose
	// id nothing in this function holds, and a cleanup scoped by a payload
	// field misses every fact whose payload lacks it.
	t.Cleanup(func() {
		bg := context.Background()

		// `membership` is policied, so this read goes through InTenantTx.
		// Reaching for Pool() here raises A2.1's fail-closed error instead
		// of returning rows — and an ignored error at this point leaves the
		// membership ids out of the list below, which is exactly how the
		// first version of this cleanup left three pending outbox rows
		// behind while reporting nothing.
		aggregates := []string{bundleID, typeID, planID, closedPlanID}
		if err := pool.InTenantTx(bg, tenantID, func(tx pgx.Tx) error {
			rows, err := tx.Query(bg,
				`SELECT id::text FROM membership WHERE tenant_id = $1`, tenantID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					return err
				}
				aggregates = append(aggregates, id)
			}
			return rows.Err()
		}); err != nil {
			t.Errorf("cleanup: collect membership ids: %v", err)
		}

		// The tuples to delete are read from the outbox's own fence columns
		// rather than rebuilt in the test. Every fact admission publishes
		// fences the tuple it renders, so this is the same list — and it
		// covers a membership no variable here holds, which is what a
		// subtest that was supposed to be refused and was not produces.
		var tuples []outbox.Tuple
		fenceRows, err := pool.Pool().Query(bg, `
			SELECT fence_subject, fence_relation, fence_object
			  FROM authorization_outbox
			 WHERE aggregate_id = ANY($1::uuid[])
			   AND fence_subject IS NOT NULL`, aggregates)
		if err != nil {
			t.Errorf("cleanup: read fences: %v", err)
		} else {
			for fenceRows.Next() {
				var tuple outbox.Tuple
				if err := fenceRows.Scan(&tuple.User, &tuple.Relation, &tuple.Object); err != nil {
					t.Errorf("cleanup: scan fence: %v", err)
					break
				}
				tuples = append(tuples, tuple)
			}
			fenceRows.Close()
		}
		_ = writer.DeleteTuples(bg, tuples)
		_, _ = fgaRaw.Write(bg).Body(client.ClientWriteRequest{
			Deletes: []client.ClientTupleKeyWithoutCondition{{
				User: adminTuple.User, Relation: adminTuple.Relation, Object: adminTuple.Object,
			}},
		}).Execute()

		_ = pool.InTenantTx(bg, tenantID, func(tx pgx.Tx) error {
			for _, q := range []string{
				`DELETE FROM idempotency_key WHERE tenant_id = $1`,
				`DELETE FROM membership WHERE tenant_id = $1`,
				`DELETE FROM membership_plan WHERE tenant_id = $1`,
				`DELETE FROM consumption_type WHERE tenant_id = $1`,
				`DELETE FROM entitlement_bundle WHERE tenant_id = $1`,
			} {
				_, _ = tx.Exec(bg, q, tenantID)
			}
			return nil
		})
		_, _ = pool.Pool().Exec(bg,
			`DELETE FROM authorization_outbox WHERE aggregate_id = ANY($1::uuid[])`, aggregates)

		// The cleanup checks itself. A leak here is not a cosmetic problem:
		// the dispatcher drains this table globally, so a row left pending
		// is another package's failing assertion next run — and the only
		// thing that finds it is looking.
		var left int
		if err := pool.Pool().QueryRow(bg, `
			SELECT count(*) FROM authorization_outbox
			 WHERE aggregate_id = ANY($1::uuid[])`, aggregates).Scan(&left); err != nil {
			t.Errorf("cleanup: count remaining outbox rows: %v", err)
		} else if left != 0 {
			t.Errorf("cleanup left %d outbox rows for this test's aggregates", left)
		}

		for _, id := range []string{adminPrinc, memberPrinc} {
			_, _ = pool.Pool().Exec(bg, `DELETE FROM principal WHERE id = $1`, id)
		}
		for _, id := range []string{adminPerson, memberPerson, otherPerson} {
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
	mintToken := func(subject string) string {
		t.Helper()
		token, err := authdev.Mint([]byte(cfg.DevAuthSecret), subject, tenantID,
			time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("mint token for %s: %v", subject, err)
		}
		return token
	}
	adminToken, memberToken := mintToken(adminSubject), mintToken(memberSubject)

	mux := http.NewServeMux()
	route := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, httpx.TenantMatch(pool, logger)(h))
	}
	route("POST /api/v1/t/{tenant}/entitlement-bundles", handleCreateBundle(pool, fga, logger))
	route("POST /api/v1/t/{tenant}/entitlement-bundles/{bundle}/entitles", handleEntitle(pool, fga, logger))
	route("POST /api/v1/t/{tenant}/membership-plans", handleCreatePlan(pool, fga, logger))
	route("POST /api/v1/t/{tenant}/consumption-types", handleCreateConsumptionType(pool, fga, logger))
	route("POST /api/v1/t/{tenant}/memberships", handleAdmitMember(pool, fga, logger))

	srv := httptest.NewServer(httpx.Chain(mux,
		httpx.Recover(logger), httpx.RequestID(), httpx.Logging(logger),
		httpx.Authenticate(authenticator, logger)))
	t.Cleanup(srv.Close)

	base := srv.URL + "/api/v1/t/" + tenantID

	// post sends a body with an optional Idempotency-Key. An empty key sends
	// no header at all, which is itself a case under test.
	post := func(t *testing.T, token, path, key string, body any, want int) map[string]any {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&buf).Encode(body); err != nil {
				t.Fatalf("encode: %v", err)
			}
		}
		req, err := http.NewRequest("POST", base+path, &buf)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set(httpx.IdempotencyHeader, key)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("POST %s = %d, want %d; body %s", path, resp.StatusCode, want, raw)
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
	errorCode := func(t *testing.T, body map[string]any) string {
		t.Helper()
		detail, _ := body["error"].(map[string]any)
		code, _ := detail["code"].(string)
		return code
	}

	// ── The configuration admission needs, all through the API ────────
	bundleID, _ = post(t, adminToken, "/entitlement-bundles", "",
		map[string]any{"name": "Mess access"}, http.StatusCreated)["id"].(string)
	typeID, _ = post(t, adminToken, "/consumption-types", "",
		map[string]any{"name": "Dinner", "obligates": true, "recurrence": "DAILY",
			"record_mode": "EITHER"}, http.StatusCreated)["id"].(string)
	post(t, adminToken, "/entitlement-bundles/"+bundleID+"/entitles", "",
		map[string]any{"object_type": "consumption_type", "object_id": typeID},
		http.StatusNoContent)
	planID, _ = post(t, adminToken, "/membership-plans", "",
		map[string]any{"code": "resident", "name": "Hostel Resident",
			"acquisition": "PURCHASED", "duration": "ANNUAL",
			"entitlement_bundle_id": bundleID}, http.StatusCreated)["id"].(string)
	if bundleID == "" || typeID == "" || planID == "" {
		t.Fatalf("configuration incomplete: bundle %q type %q plan %q", bundleID, typeID, planID)
	}
	if _, err := dispatcher.Once(ctx); err != nil {
		t.Fatalf("dispatch configuration: %v", err)
	}

	// The state before admission, asserted rather than assumed. Without it
	// the ALLOW below could be true for a reason that has nothing to do with
	// the membership.
	t.Run("a person with no membership may not record", func(t *testing.T) {
		allowed, err := fga.Check(ctx, "person:"+memberPerson, "may_record",
			"consumption_type:"+typeID, nil)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if allowed {
			t.Fatal("a person on no plan may already record; the entitlement path " +
				"grants everyone and every ALLOW below proves nothing")
		}
	})

	admitBody := map[string]any{
		"person_id":  memberPerson,
		"plan_id":    planID,
		"number":     "ADM-1",
		"valid_from": "2026-04-01",
	}
	const admitKey = "admission-test-key-1"

	var membershipID string
	t.Run("admission records an active membership", func(t *testing.T) {
		got := post(t, adminToken, "/memberships", admitKey, admitBody, http.StatusCreated)
		membershipID, _ = got["id"].(string)
		if membershipID == "" {
			t.Fatalf("no membership id in %v", got)
		}
		if got["state"] != "ACTIVE" {
			t.Fatalf("state = %v, want ACTIVE", got["state"])
		}
		if got["admitted_at"] == "" || got["admitted_at"] == nil {
			t.Fatalf("admitted_at = %v, want a timestamp: 05.3.5 makes it the "+
				"date the membership began", got["admitted_at"])
		}
		if got["valid_from"] != "2026-04-01" {
			t.Fatalf("valid_from = %v, want the date supplied; admission passes the "+
				"caller's term through and derives nothing", got["valid_from"])
		}
		// NULL is a real answer, not a missing one: an association-level
		// membership belongs to no unit (A2.4, ADR-104).
		if got["org_unit_id"] != nil {
			t.Fatalf("org_unit_id = %v, want null for an association-level membership",
				got["org_unit_id"])
		}
	})

	pendingHere := func(t *testing.T) int {
		t.Helper()
		var n int
		if err := pool.Pool().QueryRow(ctx, `
			SELECT count(*) FROM authorization_outbox
			 WHERE aggregate_id = $1::uuid
			   AND dispatched_at IS NULL AND voided_at IS NULL`, membershipID).Scan(&n); err != nil {
			t.Fatalf("count pending: %v", err)
		}
		return n
	}

	// Scoped to this membership's aggregate id, never a table-wide count:
	// the dispatcher drains authorization_outbox globally by design, so a
	// count over the whole table is an assertion about every other test's
	// leftovers.
	t.Run("the three facts are pending until the dispatcher runs", func(t *testing.T) {
		if n := pendingHere(t); n != 3 {
			t.Fatalf("%d pending facts for this membership, want 3 "+
				"(tenant edge, holder, covered_member)", n)
		}
		// A grant that lagged is ADR-101's asymmetry working, not a race.
		allowed, err := fga.Check(ctx, "person:"+memberPerson, "may_record",
			"consumption_type:"+typeID, nil)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if allowed {
			t.Fatal("the member may record before the dispatcher ran; a grant " +
				"reached OpenFGA synchronously and ADR-101's fence has nothing to guard")
		}
	})

	t.Run("the admitted member may record", func(t *testing.T) {
		if _, err := dispatcher.Once(ctx); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if n := pendingHere(t); n != 0 {
			t.Fatalf("%d of this membership's facts are still pending, want 0", n)
		}

		allowed, err := fga.Check(ctx, "person:"+memberPerson, "may_record",
			"consumption_type:"+typeID, nil)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if !allowed {
			t.Fatal("the admitted member may NOT record: the chain " +
				"holder -> covered -> covered_member -> via_plan -> entitled is broken. " +
				"ADR-107's tuple is the link that says nothing when it is missing")
		}
	})

	// ── A3.6 ──────────────────────────────────────────────────────────
	t.Run("a retry with the same key replays the original response", func(t *testing.T) {
		got := post(t, adminToken, "/memberships", admitKey, admitBody, http.StatusCreated)
		if got["id"] != membershipID {
			t.Fatalf("replay returned membership %v, want the original %v — a second "+
				"admission happened under a key that should have replayed",
				got["id"], membershipID)
		}
		var n int
		if err := pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM membership WHERE tenant_id = $1 AND number = 'ADM-1'`,
				tenantID).Scan(&n)
		}); err != nil {
			t.Fatalf("count memberships: %v", err)
		}
		if n != 1 {
			t.Fatalf("%d memberships numbered ADM-1, want 1", n)
		}
		// Every fact for this membership, not the PENDING ones: a replay
		// that repeated the work would make six, while a row left pending
		// means the dispatch failed — a different fault, which the subtest
		// above already owns. Counting pending here would let that fault
		// fail this assertion too, and send the reader after the wrong
		// thing.
		var facts int
		if err := pool.Pool().QueryRow(ctx,
			`SELECT count(*) FROM authorization_outbox WHERE aggregate_id = $1::uuid`,
			membershipID).Scan(&facts); err != nil {
			t.Fatalf("count facts: %v", err)
		}
		if facts != 3 {
			t.Fatalf("%d authorization facts for this membership, want 3: a replay "+
				"must repeat the response, not the work", facts)
		}
	})

	t.Run("the same key with a different body is refused", func(t *testing.T) {
		different := map[string]any{
			"person_id": otherPerson, "plan_id": planID, "number": "ADM-2",
		}
		got := post(t, adminToken, "/memberships", admitKey, different, http.StatusConflict)
		if code := errorCode(t, got); code != "idempotency_key_reused" {
			t.Fatalf("code = %q, want idempotency_key_reused; replaying the first "+
				"response here would silently discard this request", code)
		}
	})

	t.Run("admission without an idempotency key is refused", func(t *testing.T) {
		got := post(t, adminToken, "/memberships", "",
			map[string]any{"person_id": otherPerson, "plan_id": planID, "number": "ADM-3"},
			http.StatusBadRequest)
		if code := errorCode(t, got); code != "invalid_request" {
			t.Fatalf("code = %q, want invalid_request", code)
		}
		// The code alone cannot pin this refusal to its own reason — every
		// other malformed request answers invalid_request too. Removing the
		// requirement on purpose left this case answering 400 anyway, from a
		// zero-valued key reaching PostgreSQL as ''::uuid, so the message is
		// asserted as well. A client must not parse it; a drift test may.
		detail, _ := got["error"].(map[string]any)
		message, _ := detail["message"].(string)
		if !strings.Contains(message, httpx.IdempotencyHeader) {
			t.Fatalf("message %q does not name %s: this case passes for the wrong "+
				"reason unless the refusal is the missing header",
				message, httpx.IdempotencyHeader)
		}
	})

	// ── The two collisions, which are different answers ───────────────
	t.Run("a number already in use is refused as a number collision", func(t *testing.T) {
		got := post(t, adminToken, "/memberships", "admission-test-key-dup",
			map[string]any{"person_id": otherPerson, "plan_id": planID, "number": "ADM-1"},
			http.StatusConflict)
		if code := errorCode(t, got); code != "membership_number_taken" {
			t.Fatalf("code = %q, want membership_number_taken: the number is the "+
				"caller's own and the answer is to choose another", code)
		}
	})

	t.Run("a second active membership for one person is refused as such", func(t *testing.T) {
		got := post(t, adminToken, "/memberships", "admission-test-key-second",
			map[string]any{"person_id": memberPerson, "plan_id": planID, "number": "ADM-4"},
			http.StatusConflict)
		if code := errorCode(t, got); code != "already_active_member" {
			t.Fatalf("code = %q, want already_active_member: one_active_membership_per_tenant "+
				"is a different answer from a number collision and a client acts on it "+
				"differently", code)
		}
	})

	// A3.6's other half: the failed attempt above rolled back, key row and
	// all, so the SAME key is free to carry different work. If the key had
	// survived its own failure this would answer 409 instead — a client
	// locked out by a request that never happened.
	t.Run("a rolled-back attempt leaves nothing to replay", func(t *testing.T) {
		got := post(t, adminToken, "/memberships", "admission-test-key-dup",
			map[string]any{"person_id": otherPerson, "plan_id": planID, "number": "ADM-5"},
			http.StatusCreated)
		if got["id"] == "" || got["id"] == nil {
			t.Fatalf("no membership id in %v", got)
		}
	})

	// ── Refusals about the plan ───────────────────────────────────────
	t.Run("a plan that no longer accepts applicants is refused", func(t *testing.T) {
		// Seeded straight at the table, deliberately: no endpoint can set a
		// plan's status (B8, plan lifecycle unreachable), and a guard that
		// cannot be made to fail is indistinguishable from one that cannot
		// fire. 05.3.2 calls CLOSED_TO_NEW the common real state.
		if err := pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO membership_plan
				    (id, tenant_id, code, name, acquisition, duration, status)
				VALUES ($1,$2,'closed','Closed To New','PURCHASED','ANNUAL','CLOSED_TO_NEW')
				ON CONFLICT (id) DO NOTHING`, closedPlanID, tenantID)
			return err
		}); err != nil {
			t.Fatalf("seed closed plan: %v", err)
		}
		got := post(t, adminToken, "/memberships", "admission-test-key-closed",
			map[string]any{"person_id": otherPerson, "plan_id": closedPlanID, "number": "ADM-6"},
			http.StatusBadRequest)
		if code := errorCode(t, got); code != "invalid_request" {
			t.Fatalf("code = %q, want invalid_request", code)
		}
	})

	t.Run("a plan this tenant does not have reads as absent", func(t *testing.T) {
		post(t, adminToken, "/memberships", "admission-test-key-absent",
			map[string]any{"person_id": otherPerson, "plan_id": absentPlanID, "number": "ADM-7"},
			http.StatusNotFound)
	})

	t.Run("an identifier that is not a uuid is a bad request, not a server fault", func(t *testing.T) {
		got := post(t, adminToken, "/memberships", "admission-test-key-malformed",
			map[string]any{"person_id": "not-a-uuid", "plan_id": planID, "number": "ADM-8"},
			http.StatusBadRequest)
		if code := errorCode(t, got); code != "invalid_request" {
			t.Fatalf("code = %q, want invalid_request", code)
		}
	})

	// ── The gate ──────────────────────────────────────────────────────
	// The one case that fails if the authorization check is deleted from the
	// handler. Every other case here uses the admin, who is allowed — which
	// is exactly how session 5's PlatformOnly drift test came to pass when
	// it should have failed.
	t.Run("a caller without may_approve_membership may not admit", func(t *testing.T) {
		got := post(t, memberToken, "/memberships", "admission-test-key-forbidden",
			map[string]any{"person_id": otherPerson, "plan_id": planID, "number": "ADM-9"},
			http.StatusForbidden)
		if code := errorCode(t, got); code != "forbidden" {
			t.Fatalf("code = %q, want forbidden", code)
		}
		var n int
		if err := pool.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM membership WHERE tenant_id = $1 AND number = 'ADM-9'`,
				tenantID).Scan(&n)
		}); err != nil {
			t.Fatalf("count memberships: %v", err)
		}
		if n != 0 {
			t.Fatal("a refused admission wrote a membership row; 6.1 runs before the work")
		}
	})
}
