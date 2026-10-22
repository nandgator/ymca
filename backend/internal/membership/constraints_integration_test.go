//go:build integration

package membership

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The constraint NAMES this package maps are a contract with the schema; see
// internal/consumption's test of the same name for why a rename is silent.
func TestConstraintNamesExistInTheSchema(t *testing.T) {
	url := os.Getenv("YMCA_DATABASE_URL")
	if url == "" {
		t.Fatal("YMCA_DATABASE_URL must be set to run integration tests")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	for _, name := range []string{
		constraintOneOpenSuspension,
		"one_active_membership_per_tenant", // Reinstate maps it
		"membership_state_known",
	} {
		var found bool
		if err := pool.QueryRow(context.Background(), `
			SELECT EXISTS (SELECT 1 FROM pg_class WHERE relname = $1 AND relkind = 'i')
			    OR EXISTS (SELECT 1 FROM pg_constraint WHERE conname = $1)`, name).Scan(&found); err != nil {
			t.Fatalf("look up %s: %v", name, err)
		}
		if !found {
			t.Errorf("the schema has no index or constraint named %q", name)
		}
	}
}
