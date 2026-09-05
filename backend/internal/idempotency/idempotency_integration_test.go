//go:build integration

package idempotency_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/idempotency"
)

// A3.6 against the real database. The claims under test are the ones that
// cannot be checked with a fake pgx.Tx, because all three are properties of
// PostgreSQL rather than of this package: that the effect and the key commit
// atomically, that a rollback leaves nothing to replay, and that two
// concurrent identical requests produce one row and one effect.
func TestIdempotency_AgainstRealDatabase(t *testing.T) {
	ctx := context.Background()

	database, err := db.Open(ctx, mustEnv(t, "YMCA_DATABASE_URL"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(database.Close)

	const (
		tenantID    = "bbbbbbbb-0000-0000-0000-000000000001"
		personID    = "bbbbbbbb-0000-0000-0000-000000000002"
		principalID = "bbbbbbbb-0000-0000-0000-000000000003"
		endpoint    = "POST /t/{t}/consumption-types/{ct}/records"
	)

	seed := func(sql string, args ...any) {
		t.Helper()
		if _, err := database.Pool().Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seed(`INSERT INTO tenant (id, legal_name, display_name, jurisdiction, status)
	      VALUES ($1,'Idem Test','Idem Test','IN','ACTIVE') ON CONFLICT (id) DO NOTHING`, tenantID)
	seed(`INSERT INTO person (id, display_name, status) VALUES ($1,'Idem Person','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, personID)
	seed(`INSERT INTO principal (id, person_id, idp_subject, kind, status)
	      VALUES ($1,$2,'idem-integration-subject','STAFF','ACTIVE')
	      ON CONFLICT (id) DO NOTHING`, principalID, personID)

	t.Cleanup(func() {
		bg := context.Background()
		_ = database.InTenantTx(bg, tenantID, func(tx pgx.Tx) error {
			_, _ = tx.Exec(bg, `DELETE FROM idempotency_key WHERE tenant_id = $1`, tenantID)
			_, _ = tx.Exec(bg, `DELETE FROM location WHERE tenant_id = $1`, tenantID)
			return nil
		})
		_, _ = database.Pool().Exec(bg, `DELETE FROM principal WHERE id = $1`, principalID)
		_, _ = database.Pool().Exec(bg, `DELETE FROM person WHERE id = $1`, personID)
		_, _ = database.Pool().Exec(bg, `DELETE FROM tenant WHERE id = $1`, tenantID)
	})

	body := []byte(`{"meal":"dinner"}`)
	key := func(k string) idempotency.Key {
		return idempotency.Key{
			TenantID:      tenantID,
			Endpoint:      endpoint,
			Key:           k,
			PrincipalID:   principalID,
			RequestDigest: idempotency.Digest(body),
		}
	}

	// The "effect" is a `location` row per run, so the test counts how many
	// times the operation actually committed rather than trusting that it did
	// not. A scratch table would have been clearer and is not available: the
	// application role holds no DDL, which is ADR-108 working as intended.
	// `location` is tenant-scoped and RLS-policed, so every count below also
	// passes through InTenantTx.
	effects := func(tag string) int {
		t.Helper()
		var n int
		err := database.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM location WHERE tenant_id = $1 AND name = $2`,
				tenantID, tag).Scan(&n)
		})
		if err != nil {
			t.Fatalf("count effects: %v", err)
		}
		return n
	}

	op := func(tag string, status int) func(context.Context, pgx.Tx) (idempotency.Result, error) {
		return func(ctx context.Context, tx pgx.Tx) (idempotency.Result, error) {
			if _, err := tx.Exec(ctx,
				`INSERT INTO location (id, tenant_id, name) VALUES (gen_random_uuid(), $1, $2)`,
				tenantID, tag); err != nil {
				return idempotency.Result{}, err
			}
			return idempotency.Result{
				StatusCode: status,
				Body:       json.RawMessage(`{"tag":"` + tag + `"}`),
			}, nil
		}
	}

	// run is the shape a handler uses: look first, run if absent.
	run := func(k idempotency.Key, tag string) (idempotency.Result, error) {
		var out idempotency.Result
		err := database.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
			stored, found, err := idempotency.Lookup(ctx, tx, k)
			if err != nil {
				return err
			}
			if found {
				out = stored
				return nil
			}
			out, err = idempotency.Do(ctx, tx, k, op(tag, 201))
			return err
		})
		return out, err
	}

	t.Run("first call runs, second replays", func(t *testing.T) {
		first, err := run(key("k-replay"), "replay")
		if err != nil {
			t.Fatalf("first: %v", err)
		}
		if first.Replayed || first.StatusCode != 201 {
			t.Fatalf("first = %+v, want a fresh 201", first)
		}

		second, err := run(key("k-replay"), "replay")
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		if !second.Replayed {
			t.Fatal("second call did not replay")
		}
		if second.StatusCode != first.StatusCode {
			t.Fatalf("replay status = %d, want %d", second.StatusCode, first.StatusCode)
		}
		// Semantic, not byte, equality: response_body is jsonb and PostgreSQL
		// normalizes whitespace and key order (A3.6). Asserting bytes here
		// would pin an implementation detail of the database and fail for a
		// reason that has nothing to do with idempotency.
		var wantBody, gotBody map[string]any
		if err := json.Unmarshal(first.Body, &wantBody); err != nil {
			t.Fatalf("decode first body: %v", err)
		}
		if err := json.Unmarshal(second.Body, &gotBody); err != nil {
			t.Fatalf("decode replayed body: %v", err)
		}
		if !reflect.DeepEqual(wantBody, gotBody) {
			t.Fatalf("replayed body = %v, want %v", gotBody, wantBody)
		}
		if n := effects("replay"); n != 1 {
			t.Fatalf("the operation ran %d times, want 1", n)
		}
	})

	// The property that makes the in-transaction design correct. If the key
	// were recorded outside the work's transaction, this retry would replay a
	// response for an effect that was rolled back.
	t.Run("a rolled-back attempt leaves nothing to replay", func(t *testing.T) {
		k := key("k-rollback")
		wantErr := errors.New("the operation failed")

		err := database.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
			_, err := idempotency.Do(ctx, tx, k, func(ctx context.Context, tx pgx.Tx) (idempotency.Result, error) {
				if _, err := tx.Exec(ctx,
					`INSERT INTO location (id, tenant_id, name) VALUES (gen_random_uuid(), $1, 'rollback')`,
					tenantID); err != nil {
					return idempotency.Result{}, err
				}
				return idempotency.Result{}, wantErr
			})
			return err
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("err = %v, want the operation's own error", err)
		}
		if n := effects("rollback"); n != 0 {
			t.Fatalf("a failed operation left %d effects behind", n)
		}

		// The retry must run for real, not replay.
		result, err := run(k, "rollback")
		if err != nil {
			t.Fatalf("retry: %v", err)
		}
		if result.Replayed {
			t.Fatal("the retry replayed a response for work that was rolled back")
		}
		if n := effects("rollback"); n != 1 {
			t.Fatalf("after retry the operation ran %d times, want 1", n)
		}
	})

	t.Run("the same key with a different body is refused", func(t *testing.T) {
		if _, err := run(key("k-digest"), "digest"); err != nil {
			t.Fatalf("first: %v", err)
		}

		different := key("k-digest")
		different.RequestDigest = idempotency.Digest([]byte(`{"meal":"breakfast"}`))

		_, err := run(different, "digest")
		if !errors.Is(err, idempotency.ErrKeyReused) {
			t.Fatalf("err = %v, want ErrKeyReused", err)
		}
		if n := effects("digest"); n != 1 {
			t.Fatalf("the operation ran %d times, want 1", n)
		}
	})

	// Two identical requests genuinely at once. Both run the operation; one
	// loses the primary key race and must not leave its effect behind.
	t.Run("concurrent identical requests produce one effect", func(t *testing.T) {
		k := key("k-concurrent")

		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			errs    []error
			retried bool
		)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := run(k, "concurrent")
				mu.Lock()
				defer mu.Unlock()
				if errors.Is(err, idempotency.ErrConcurrent) {
					retried = true
					return
				}
				if err != nil {
					errs = append(errs, err)
				}
			}()
		}
		wg.Wait()

		for _, err := range errs {
			t.Errorf("unexpected error: %v", err)
		}
		if n := effects("concurrent"); n != 1 {
			t.Fatalf("the operation committed %d times, want 1 (loser retried: %v)", n, retried)
		}

		// Whichever lost, a later attempt must replay rather than run again.
		result, err := run(k, "concurrent")
		if err != nil {
			t.Fatalf("after the race: %v", err)
		}
		if !result.Replayed {
			t.Fatal("the post-race attempt did not replay")
		}
		if n := effects("concurrent"); n != 1 {
			t.Fatalf("the operation committed %d times after replay, want 1", n)
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
