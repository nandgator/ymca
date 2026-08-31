//go:build integration

package db_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/db"
)

// requireTestDatabaseURL is shared by every integration test in this
// package. Failing loudly when the variable is absent, rather than
// t.Skip-ing, matches the spec's own rule for the registry tests: a test
// that passes because it never ran is worse than no test.
func requireTestDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("YMCA_DATABASE_URL")
	if url == "" {
		t.Fatal("YMCA_DATABASE_URL must be set to run integration tests (role ymca_api)")
	}
	return url
}

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(context.Background(), requireTestDatabaseURL(t))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(d.Close)
	if err := d.Health(context.Background()); err != nil {
		t.Fatalf("db.Health: %v", err)
	}
	return d
}

// TestInTenantTx_TenantIsolation is D4's central claim, proved against the
// real cluster: a row inserted for one tenant is invisible to a second
// tenant's transaction, even though both are read through the same pool.
func TestInTenantTx_TenantIsolation(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	tenantA := "11111111-1111-1111-1111-111111111111"
	tenantB := "22222222-2222-2222-2222-222222222222"

	// tenant itself carries no tenant_id and no RLS policy (8.2) — it is
	// the root of the boundary, not a member of it.
	if _, err := d.Pool().Exec(ctx, `
		INSERT INTO tenant (id, legal_name, display_name, jurisdiction, status)
		VALUES ($1, 'Test A', 'Test A', 'US', 'ACTIVE'), ($2, 'Test B', 'Test B', 'US', 'ACTIVE')
		ON CONFLICT (id) DO NOTHING
	`, tenantA, tenantB); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}
	t.Cleanup(func() {
		_, _ = d.Pool().Exec(context.Background(), `DELETE FROM tenant WHERE id = ANY($1)`,
			[]string{tenantA, tenantB})
	})

	if err := d.InTenantTx(ctx, tenantA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO location (tenant_id, name) VALUES ($1, 'A-location')`, tenantA)
		return err
	}); err != nil {
		t.Fatalf("insert A-location: %v", err)
	}
	if err := d.InTenantTx(ctx, tenantB, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO location (tenant_id, name) VALUES ($1, 'B-location')`, tenantB)
		return err
	}); err != nil {
		t.Fatalf("insert B-location: %v", err)
	}
	t.Cleanup(func() {
		_ = d.InTenantTx(context.Background(), tenantA, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), `DELETE FROM location WHERE tenant_id = $1`, tenantA)
			return err
		})
		_ = d.InTenantTx(context.Background(), tenantB, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), `DELETE FROM location WHERE tenant_id = $1`, tenantB)
			return err
		})
	})

	// From tenant A's transaction, A's row is visible and B's is not —
	// even though the WHERE clause below does not mention tenant at all.
	err := d.InTenantTx(ctx, tenantA, func(tx pgx.Tx) error {
		var aCount, bCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM location WHERE name = 'A-location'`).Scan(&aCount); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM location WHERE name = 'B-location'`).Scan(&bCount); err != nil {
			return err
		}
		if aCount != 1 {
			t.Fatalf("A's own transaction sees %d rows named A-location, want 1", aCount)
		}
		if bCount != 0 {
			t.Fatalf("A's transaction sees %d rows named B-location, want 0 — tenant isolation is broken", bCount)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("InTenantTx(A): %v", err)
	}
}

// TestQueryOutsideInTenantTx_Raises is A2.1's fail-closed guarantee: a
// query that reaches a tenant-scoped table without ever calling
// db.InTenantTx errors — because current_setting('app.tenant_id') is called
// without missing_ok — rather than silently returning zero rows.
func TestQueryOutsideInTenantTx_Raises(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	var name string
	err := d.Pool().QueryRow(ctx, `SELECT name FROM location LIMIT 1`).Scan(&name)
	if err == nil {
		t.Fatal("query outside InTenantTx succeeded; want an error from the unset app.tenant_id policy")
	}
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("query returned zero rows instead of raising: %v (fail-closed behaviour was softened into zero rows)", err)
	}
}
