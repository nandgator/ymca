//go:build integration

package consumption

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The constraint NAMES in errors.go are a contract with the schema. Renaming
// one in a migration compiles, vets clean, and turns a 409 the caller can act
// on into a 500 — and the integration tests would not notice, because the
// race that reaches the backstop is the one nothing provokes. This fails the
// moment either side moves.
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
		constraintLiveRecord,      // a unique index on consumption_record
		constraintSupersededOnce,  // a unique index on consumption_record
		constraintPeriodCloseOnce, // an exclusion constraint
	} {
		var found bool
		err := pool.QueryRow(context.Background(), `
			SELECT EXISTS (SELECT 1 FROM pg_class WHERE relname = $1 AND relkind = 'i')
			    OR EXISTS (SELECT 1 FROM pg_constraint WHERE conname = $1)`, name).Scan(&found)
		if err != nil {
			t.Fatalf("look up %s: %v", name, err)
		}
		if !found {
			t.Errorf("errors.go maps %q and the schema has no index or constraint of that name", name)
		}
	}
}
