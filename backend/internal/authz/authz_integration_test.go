//go:build integration

package authz_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/openfga/go-sdk/client"

	"github.com/nandgator/ymca/backend/internal/auth"
	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/config"
	"github.com/nandgator/ymca/backend/internal/db"
)

// This proves D5's authz.Check against the real cluster: a real tuple
// produces ALLOW, its absence produces DENY, and the DENY is audited
// (D8) — all through the actual OpenFGA and PostgreSQL this step depends
// on, not through fakes of either.
//
// Requires YMCA_DATABASE_URL, YMCA_FGA_API_URL, YMCA_FGA_STORE_ID and
// YMCA_FGA_MODEL_ID — the same variables cmd/api itself requires, plus the
// store id `go run ./cmd/fga apply` prints.
func TestCheck_AllowAndDenyAgainstRealStore(t *testing.T) {
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
		tenantID    = "33333333-3333-3333-3333-333333333333"
		personID    = "44444444-4444-4444-4444-444444444444"
		principalID = "55555555-5555-5555-5555-555555555555"
	)

	if _, err := database.Pool().Exec(ctx, `
		INSERT INTO tenant (id, legal_name, display_name, jurisdiction, status)
		VALUES ($1, 'Check Test', 'Check Test', 'US', 'ACTIVE')
		ON CONFLICT (id) DO NOTHING
	`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := database.Pool().Exec(ctx, `
		INSERT INTO person (id, display_name, status) VALUES ($1, 'Check Test Person', 'ACTIVE')
		ON CONFLICT (id) DO NOTHING
	`, personID); err != nil {
		t.Fatalf("seed person: %v", err)
	}
	if _, err := database.Pool().Exec(ctx, `
		INSERT INTO principal (id, person_id, idp_subject, kind, status)
		VALUES ($1, $2, 'authz-integration-test-subject', 'PERSONAL', 'ACTIVE')
		ON CONFLICT (id) DO NOTHING
	`, principalID, personID); err != nil {
		t.Fatalf("seed principal: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		// audit_event is append-only in privilege (0001_init.sql revokes
		// UPDATE/DELETE from ymca_app) — the test's audit rows outlive the
		// test deliberately, matching 8.5. Only the fixture rows that
		// *can* be deleted are cleaned up here.
		_, _ = database.Pool().Exec(bg, `DELETE FROM principal WHERE id = $1`, principalID)
		_, _ = database.Pool().Exec(bg, `DELETE FROM person WHERE id = $1`, personID)
		_, _ = database.Pool().Exec(bg, `DELETE FROM tenant WHERE id = $1`, tenantID)
	})

	fgaRaw, err := client.NewSdkClient(&client.ClientConfiguration{
		ApiUrl:               cfg.FGAAPIURL,
		StoreId:              cfg.FGAStoreID,
		AuthorizationModelId: cfg.FGAModelID,
	})
	if err != nil {
		t.Fatalf("raw fga client: %v", err)
	}
	if _, err := fgaRaw.Write(ctx).Body(client.ClientWriteRequest{
		Writes: []client.ClientTupleKey{
			{User: "principal:" + principalID, Relation: "member", Object: "tenant:" + tenantID},
		},
	}).Execute(); err != nil {
		t.Fatalf("write tuple: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fgaRaw.Write(context.Background()).Body(client.ClientWriteRequest{
			Deletes: []client.ClientTupleKeyWithoutCondition{
				{User: "principal:" + principalID, Relation: "member", Object: "tenant:" + tenantID},
			},
		}).Execute()
	})

	principal := auth.Principal{ID: principalID, PersonID: personID, Kind: auth.KindPersonal, TenantID: tenantID}

	t.Run("ALLOW", func(t *testing.T) {
		allowed, err := authz.Check(ctx, database, fga, authz.Request{
			Principal:       principal,
			RequestTenantID: tenantID,
			Object:          authz.Object{Type: "tenant", ID: tenantID, TenantID: tenantID},
			Relation:        "member",
			Action:          "tenant.member",
			RequestID:       "test-allow",
		})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if !allowed {
			t.Fatal("expected ALLOW for the granted relation, got DENY")
		}
	})

	t.Run("DENY_is_audited", func(t *testing.T) {
		// audit_event is append-only, so rows from every previous run of
		// this test are still there. Counting rows for the action alone
		// would pass once and fail on every re-run; the assertion is
		// therefore scoped to a request id unique to this run — which is
		// also what the request id is for in production (8.5, A3.4).
		denyRequestID := fmt.Sprintf("test-deny-%d", time.Now().UnixNano())

		allowed, err := authz.Check(ctx, database, fga, authz.Request{
			Principal:       principal,
			RequestTenantID: tenantID,
			Object:          authz.Object{Type: "tenant", ID: tenantID, TenantID: tenantID},
			Relation:        "admin", // not granted
			Action:          "tenant.admin",
			RequestID:       denyRequestID,
		})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if allowed {
			t.Fatal("expected DENY for the ungranted relation, got ALLOW")
		}

		// audit_event carries the same tenant_isolation RLS policy as any
		// other tenant-scoped table, so reading it back also goes through
		// InTenantTx.
		var count int
		var reason string
		err = database.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT count(*), coalesce(min(context->>'reason'), '')
				FROM audit_event
				WHERE tenant_id = $1
				  AND action = 'tenant.admin'
				  AND outcome = 'DENIED'
				  AND actor_principal_id = $2
				  AND context->>'request_id' = $3
			`, tenantID, principalID, denyRequestID).Scan(&count, &reason)
		})
		if err != nil {
			t.Fatalf("query audit_event: %v", err)
		}
		if count != 1 {
			t.Fatalf("audit_event rows for the DENY = %d, want 1", count)
		}
		// The DENY came from step 2, so the recorded reason must say so —
		// a row that merely exists proves less than one that names which
		// check produced it.
		if reason != "graph" {
			t.Fatalf("audit context reason = %q, want \"graph\"", reason)
		}
	})
}

func mustEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("%s must be set to run integration tests", key)
	}
	return v
}
