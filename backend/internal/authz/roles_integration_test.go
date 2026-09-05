//go:build integration

package authz_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/openfga/go-sdk/client"

	"github.com/nandgator/ymca/backend/internal/auth"
	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/config"
	"github.com/nandgator/ymca/backend/internal/db"
)

// TestRoleAssignment_AgainstRealStore proves ADR-109 against the live
// cluster: a role-derived permission resolves through contextual tuples that
// were never written to OpenFGA, and stops resolving the moment the term
// window closes, with no sweeper having run.
//
// This is the test 8.2 could not have: its term-window limb was a no-op
// because the check's shape made it unwritable. Every sub-test here is a
// claim the design record makes and nothing previously verified.
func TestRoleAssignment_AgainstRealStore(t *testing.T) {
	ctx := context.Background()

	cfg := config.Config{
		DatabaseURL: mustEnv(t, "YMCA_DATABASE_URL"),
		FGAAPIURL:   mustEnv(t, "YMCA_FGA_API_URL"),
		FGAStoreID:  mustEnv(t, "YMCA_FGA_STORE_ID"),
		FGAModelID:  mustEnv(t, "YMCA_FGA_MODEL_ID"),
	}

	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(database.Close)

	fga, err := authz.NewFGA(cfg)
	if err != nil {
		t.Fatalf("authz.NewFGA: %v", err)
	}

	const (
		tenantID    = "aaaaaaaa-0000-0000-0000-000000000001"
		personID    = "aaaaaaaa-0000-0000-0000-000000000002"
		principalID = "aaaaaaaa-0000-0000-0000-000000000003"
		granterID   = "aaaaaaaa-0000-0000-0000-000000000004"
		granterPers = "aaaaaaaa-0000-0000-0000-000000000005"
		unitID      = "aaaaaaaa-0000-0000-0000-000000000006"
		roleDefID   = "aaaaaaaa-0000-0000-0000-000000000007"
		assignID    = "aaaaaaaa-0000-0000-0000-000000000008"

		permission = "organizational_unit.member_read"
		relation   = "member_read"
	)

	// person, principal and tenant are RLS-exempt or seeded before any
	// tenant context exists, so they go through the pool directly. Everything
	// tenant-scoped below goes through InTenantTx — the policy uses its
	// USING expression as the INSERT check too, so a seed without a tenant
	// context raises rather than silently inserting.
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := database.Pool().Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	exec(`INSERT INTO tenant (id, legal_name, display_name, jurisdiction, status)
	      VALUES ($1,'Role Test','Role Test','IN','ACTIVE') ON CONFLICT (id) DO NOTHING`, tenantID)
	exec(`INSERT INTO person (id, display_name, status) VALUES ($1,'Role Subject','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, personID)
	exec(`INSERT INTO person (id, display_name, status) VALUES ($1,'Role Granter','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, granterPers)
	exec(`INSERT INTO principal (id, person_id, idp_subject, kind, status)
	      VALUES ($1,$2,'role-integration-subject','STAFF','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		principalID, personID)
	exec(`INSERT INTO principal (id, person_id, idp_subject, kind, status)
	      VALUES ($1,$2,'role-integration-granter','STAFF','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		granterID, granterPers)

	inTenant := func(sql string, args ...any) {
		t.Helper()
		err := database.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
		if err != nil {
			t.Fatalf("seed (tenant tx): %v", err)
		}
	}
	inTenant(`INSERT INTO organizational_unit (id, tenant_id, type, name, status)
	          VALUES ($1,$2,'CHAPTER','Role Test Unit','ACTIVE') ON CONFLICT (id) DO NOTHING`,
		unitID, tenantID)
	inTenant(`INSERT INTO role_definition (id, tenant_id, code, name, term_policy, status)
	          VALUES ($1,$2,'role-test-secretary','Role Test Secretary','OPTIONAL_TERM','ACTIVE')
	          ON CONFLICT (id) DO NOTHING`, roleDefID, tenantID)
	inTenant(`INSERT INTO role_permission (role_definition_id, permission)
	          VALUES ($1,$2) ON CONFLICT DO NOTHING`, roleDefID, permission)

	t.Cleanup(func() {
		bg := context.Background()
		_ = database.InTenantTx(bg, tenantID, func(tx pgx.Tx) error {
			for _, q := range []string{
				`DELETE FROM role_assignment WHERE tenant_id = $1`,
				`DELETE FROM role_permission WHERE role_definition_id = '` + roleDefID + `'`,
				`DELETE FROM role_required_clearance WHERE role_definition_id = '` + roleDefID + `'`,
				`DELETE FROM role_definition WHERE tenant_id = $1`,
				`DELETE FROM organizational_unit WHERE tenant_id = $1`,
			} {
				_, _ = tx.Exec(bg, q, tenantID)
			}
			return nil
		})
		for _, id := range []string{principalID, granterID} {
			_, _ = database.Pool().Exec(bg, `DELETE FROM principal WHERE id = $1`, id)
		}
		for _, id := range []string{personID, granterPers} {
			_, _ = database.Pool().Exec(bg, `DELETE FROM person WHERE id = $1`, id)
		}
		_, _ = database.Pool().Exec(bg, `DELETE FROM tenant WHERE id = $1`, tenantID)
	})

	// The unit must be reachable from the tenant, or step 1's ancestry and
	// the model's tenant relation have nothing to resolve.
	fgaRaw, err := client.NewSdkClient(&client.ClientConfiguration{
		ApiUrl:               cfg.FGAAPIURL,
		StoreId:              cfg.FGAStoreID,
		AuthorizationModelId: cfg.FGAModelID,
	})
	if err != nil {
		t.Fatalf("raw fga client: %v", err)
	}
	tenantTuple := client.ClientTupleKey{
		User: "tenant:" + tenantID, Relation: "tenant", Object: "organizational_unit:" + unitID,
	}
	if _, err := fgaRaw.Write(ctx).Body(client.ClientWriteRequest{
		Writes: []client.ClientTupleKey{tenantTuple},
	}).Execute(); err != nil {
		t.Fatalf("write tenant tuple: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fgaRaw.Write(context.Background()).Body(client.ClientWriteRequest{
			Deletes: []client.ClientTupleKeyWithoutCondition{{
				User: tenantTuple.User, Relation: tenantTuple.Relation, Object: tenantTuple.Object,
			}},
		}).Execute()
	})

	principal := auth.Principal{
		ID: principalID, PersonID: personID, Kind: auth.KindStaff, TenantID: tenantID,
	}
	request := func(requestID string) authz.Request {
		return authz.Request{
			Principal:       principal,
			RequestTenantID: tenantID,
			Object:          authz.Object{Type: "organizational_unit", ID: unitID, TenantID: tenantID},
			Relation:        relation,
			Action:          permission,
			RequestID:       requestID,
		}
	}

	// assign replaces the single assignment under test with one whose window
	// and shape the sub-test chooses.
	assign := func(validFrom, validUntil any) {
		t.Helper()
		inTenant(`DELETE FROM role_assignment WHERE tenant_id = $1`, tenantID)
		inTenant(`INSERT INTO role_assignment
		    (id, tenant_id, role_definition_id, subject_principal_id,
		     scope_type, scope_id, valid_from, valid_until, granted_by_principal_id)
		    VALUES ($1,$2,$3,$4,'organizational_unit',$5,$6,$7,$8)`,
			assignID, tenantID, roleDefID, principalID, unitID,
			validFrom, validUntil, granterID)
	}

	past := time.Now().Add(-48 * time.Hour)
	future := time.Now().Add(48 * time.Hour)

	t.Run("a current assignment ALLOWs", func(t *testing.T) {
		assign(past, future)
		allowed, err := authz.Check(ctx, database, fga, request("role-allow"))
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if !allowed {
			t.Fatal("a current role assignment did not ALLOW")
		}
	})

	// The claim that makes ADR-109 worth its cost. Nothing was deleted from
	// OpenFGA between this and the sub-test above, and no sweeper ran — the
	// only difference is a column in PostgreSQL.
	t.Run("an EXPIRED assignment DENIES", func(t *testing.T) {
		assign(past, time.Now().Add(-1*time.Hour))
		requestID := fmt.Sprintf("role-expired-%d", time.Now().UnixNano())

		allowed, err := authz.Check(ctx, database, fga, request(requestID))
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if allowed {
			t.Fatal("an expired role assignment still authorized — ADR-070 is not holding")
		}
		assertDenyAudited(t, ctx, database, tenantID, principalID, permission, requestID, "graph")
	})

	t.Run("an assignment not yet begun DENIES", func(t *testing.T) {
		assign(future, nil)
		allowed, err := authz.Check(ctx, database, fga, request("role-not-yet"))
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if allowed {
			t.Fatal("an assignment whose valid_from is in the future authorized")
		}
	})

	t.Run("a revoked assignment DENIES", func(t *testing.T) {
		assign(past, future)
		inTenant(`UPDATE role_assignment SET revoked_at = now() WHERE id = $1`, assignID)
		allowed, err := authz.Check(ctx, database, fga, request("role-revoked"))
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if allowed {
			t.Fatal("a revoked assignment still authorized")
		}
	})

	// ADR-087. The assignment is current; only the clearance is missing.
	t.Run("a missing required clearance DENIES", func(t *testing.T) {
		assign(past, future)
		inTenant(`INSERT INTO role_required_clearance (role_definition_id, verification_type)
		          VALUES ($1,'BACKGROUND_CHECK') ON CONFLICT DO NOTHING`, roleDefID)
		t.Cleanup(func() {
			inTenant(`DELETE FROM role_required_clearance WHERE role_definition_id = $1`, roleDefID)
		})

		allowed, err := authz.Check(ctx, database, fga, request("role-no-clearance"))
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if allowed {
			t.Fatal("an assignment requiring an absent clearance still authorized")
		}
	})

	// 05.9.4. The role path is suppressed by the restriction kinds that act
	// on it, which is why they need no restriction_kind_permission row.
	t.Run("NO_ROLE_ASSIGNMENT suppresses the role", func(t *testing.T) {
		assign(past, future)
		var restrictionID string
		err := database.Pool().QueryRow(ctx, `
			INSERT INTO restriction (id, person_id, tenant_id, kind, imposed_by_person_id)
			VALUES (gen_random_uuid(), $1, $2, 'NO_ROLE_ASSIGNMENT', $3) RETURNING id::text
		`, personID, tenantID, granterPers).Scan(&restrictionID)
		if err != nil {
			t.Fatalf("seed restriction: %v", err)
		}
		t.Cleanup(func() {
			_, _ = database.Pool().Exec(context.Background(),
				`DELETE FROM restriction WHERE id = $1::uuid`, restrictionID)
		})

		allowed, err := authz.Check(ctx, database, fga, request("role-restricted"))
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if allowed {
			t.Fatal("a principal under NO_ROLE_ASSIGNMENT still held their role")
		}
	})

	// A1.8 rule 8, proved at runtime rather than in the fixture: the ALLOW
	// above came entirely from contextual tuples. If any role tuple had been
	// written to the store, this same check without them would still pass.
	t.Run("no role tuple was ever stored", func(t *testing.T) {
		assign(past, future)

		allowed, err := fga.Check(ctx,
			"principal:"+principalID, relation, "organizational_unit:"+unitID, nil)
		if err != nil {
			t.Fatalf("Check without contextual tuples: %v", err)
		}
		if allowed {
			t.Fatal("the permission resolved WITHOUT contextual tuples — " +
				"a role tuple is in the store, which ADR-109 forbids")
		}

		withTuples, err := authz.Check(ctx, database, fga, request("role-contextual-only"))
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if !withTuples {
			t.Fatal("the same permission did not resolve WITH contextual tuples")
		}
	})
}

// assertDenyAudited scopes its assertion to a request id unique to the run,
// because audit_event is append-only by privilege: counting rows for an
// action alone passes on a fresh database and fails on every re-run.
func assertDenyAudited(t *testing.T, ctx context.Context, database *db.DB,
	tenantID, principalID, action, requestID, wantReason string,
) {
	t.Helper()

	var count int
	var reason string
	err := database.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT count(*), coalesce(min(context->>'reason'), '')
			FROM audit_event
			WHERE tenant_id = $1
			  AND action = $2
			  AND outcome = 'DENIED'
			  AND actor_principal_id = $3
			  AND context->>'request_id' = $4
		`, tenantID, action, principalID, requestID).Scan(&count, &reason)
	})
	if err != nil {
		t.Fatalf("query audit_event: %v", err)
	}
	if count != 1 {
		t.Fatalf("audit_event rows for the DENY = %d, want 1", count)
	}
	if reason != wantReason {
		t.Fatalf("audit context reason = %q, want %q", reason, wantReason)
	}
}
