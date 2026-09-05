//go:build integration

package outbox_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/nandgator/ymca/backend/internal/config"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/outbox"
)

// ADR-101 against the real cluster. The claim under test is the one 6.4 said
// could not happen and then could: that a grant in flight cannot resurrect a
// tuple a revocation has already removed.
func TestOutbox_AgainstRealCluster(t *testing.T) {
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

	writer, err := outbox.NewFGAWriter(cfg)
	if err != nil {
		t.Fatalf("NewFGAWriter: %v", err)
	}

	const (
		tenantID = "cccccccc-0000-0000-0000-000000000001"
		unitID   = "cccccccc-0000-0000-0000-000000000002"
	)

	if _, err := database.Pool().Exec(ctx, `
		INSERT INTO tenant (id, legal_name, display_name, jurisdiction, status)
		VALUES ($1,'Outbox Test','Outbox Test','IN','ACTIVE') ON CONFLICT (id) DO NOTHING
	`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = database.Pool().Exec(bg,
			`DELETE FROM authorization_outbox WHERE aggregate_id = $1::uuid`, unitID)
		_, _ = database.Pool().Exec(bg, `DELETE FROM tenant WHERE id = $1`, tenantID)
	})

	// The fact under test: a unit's tenant edge. Chosen because it is a real
	// relation in A1.2 and touches nothing else the suite depends on.
	fence := outbox.Fence{
		Subject:  "tenant:" + tenantID,
		Relation: "tenant",
		Object:   "organizational_unit:" + unitID,
	}
	tuple := outbox.Tuple{User: fence.Subject, Relation: fence.Relation, Object: fence.Object}

	renderers := map[string]outbox.Renderer{
		"UnitCreated": func(payload json.RawMessage) ([]outbox.Tuple, error) {
			var p struct{ TenantID, UnitID string }
			if err := json.Unmarshal(payload, &p); err != nil {
				return nil, err
			}
			// Rendered here, from the fact — never stored pre-rendered.
			return []outbox.Tuple{{
				User:     "tenant:" + p.TenantID,
				Relation: "tenant",
				Object:   "organizational_unit:" + p.UnitID,
			}}, nil
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dispatcher := outbox.New(database.Pool(), writer, renderers, logger)

	fact := outbox.Fact{
		AggregateType: "organizational_unit",
		AggregateID:   unitID,
		EventType:     "UnitCreated",
		Payload:       map[string]string{"TenantID": tenantID, "UnitID": unitID},
		Fence:         fence,
	}

	enqueue := func(t *testing.T, f outbox.Fact) {
		t.Helper()
		tx, err := database.Pool().Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		if err := outbox.Enqueue(ctx, tx, f); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	cleanTuple := func() {
		_ = writer.DeleteTuples(context.Background(), []outbox.Tuple{tuple})
	}
	cleanRows := func() {
		_, _ = database.Pool().Exec(ctx,
			`DELETE FROM authorization_outbox WHERE aggregate_id = $1::uuid`, unitID)
	}
	t.Cleanup(cleanTuple)

	pendingRows := func(t *testing.T) int {
		t.Helper()
		var n int
		if err := database.Pool().QueryRow(ctx, `
			SELECT count(*) FROM authorization_outbox
			 WHERE aggregate_id = $1::uuid AND dispatched_at IS NULL AND voided_at IS NULL
		`, unitID).Scan(&n); err != nil {
			t.Fatalf("count pending: %v", err)
		}
		return n
	}

	t.Run("a queued fact is projected", func(t *testing.T) {
		cleanRows()
		cleanTuple()
		enqueue(t, fact)

		n, err := dispatcher.Once(ctx)
		if err != nil {
			t.Fatalf("Once: %v", err)
		}
		if n != 1 {
			t.Fatalf("dispatched %d rows, want 1", n)
		}
		if pendingRows(t) != 0 {
			t.Fatal("the row is still pending after a successful dispatch")
		}
	})

	// At-least-once delivery (8.9) means a row can be redelivered. If an
	// existing tuple made the write fail, every retry would become a
	// permanent error and the dispatcher would wedge.
	t.Run("redelivery is not an error", func(t *testing.T) {
		cleanRows()
		enqueue(t, fact) // the tuple already exists from the sub-test above

		n, err := dispatcher.Once(ctx)
		if err != nil {
			t.Fatalf("Once on redelivery: %v", err)
		}
		if n != 1 {
			t.Fatalf("dispatched %d rows, want 1", n)
		}
	})

	t.Run("a voided row is never dispatched", func(t *testing.T) {
		cleanRows()
		cleanTuple()
		enqueue(t, fact)

		tx, err := database.Pool().Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		voided, err := outbox.Void(ctx, tx, fence)
		if err != nil {
			t.Fatalf("Void: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit void: %v", err)
		}
		if voided != 1 {
			t.Fatalf("voided %d rows, want 1", voided)
		}

		n, err := dispatcher.Once(ctx)
		if err != nil {
			t.Fatalf("Once: %v", err)
		}
		if n != 0 {
			t.Fatalf("dispatched %d voided rows, want 0", n)
		}
	})

	// THE RACE ADR-101 EXISTS FOR, and the one the row lock alone does not
	// close. A grant transaction that began BEFORE the revocation and commits
	// AFTER it must not slip a row past the void.
	//
	// Without the advisory lock the revocation's UPDATE simply does not see
	// the uncommitted row, commits, deletes a tuple that does not exist yet,
	// and reports success — then the dispatcher applies the survivor and the
	// tuple is back with no synchronous path left to run.
	t.Run("a grant in flight cannot outlive the revocation", func(t *testing.T) {
		cleanRows()
		cleanTuple()

		grantTx, err := database.Pool().Begin(ctx)
		if err != nil {
			t.Fatalf("begin grant: %v", err)
		}
		defer grantTx.Rollback(ctx)

		// The grant takes the fence lock and writes its row, but does not
		// commit yet.
		if err := outbox.Enqueue(ctx, grantTx, fact); err != nil {
			t.Fatalf("Enqueue in flight: %v", err)
		}

		// The revocation starts now and must BLOCK on the fence lock.
		type voidResult struct {
			rows int64
			err  error
		}
		done := make(chan voidResult, 1)
		go func() {
			revokeTx, err := database.Pool().Begin(context.Background())
			if err != nil {
				done <- voidResult{err: err}
				return
			}
			defer revokeTx.Rollback(context.Background())

			rows, err := outbox.Void(context.Background(), revokeTx, fence)
			if err != nil {
				done <- voidResult{err: err}
				return
			}
			if err := revokeTx.Commit(context.Background()); err != nil {
				done <- voidResult{err: err}
				return
			}
			done <- voidResult{rows: rows}
		}()

		// If the lock were not taken, Void would finish immediately having
		// seen nothing. Blocking here is the property under test.
		select {
		case r := <-done:
			t.Fatalf("Void completed while the grant held the fence lock "+
				"(voided %d rows, err %v) — the advisory lock of ADR-101 "+
				"obligation 2 is not being taken", r.rows, r.err)
		case <-time.After(300 * time.Millisecond):
		}

		if err := grantTx.Commit(ctx); err != nil {
			t.Fatalf("commit grant: %v", err)
		}

		select {
		case r := <-done:
			if r.err != nil {
				t.Fatalf("Void: %v", r.err)
			}
			if r.rows != 1 {
				t.Fatalf("the revocation voided %d rows, want 1 — the grant "+
					"that committed during it was not fenced", r.rows)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Void did not complete after the grant committed")
		}

		n, err := dispatcher.Once(ctx)
		if err != nil {
			t.Fatalf("Once: %v", err)
		}
		if n != 0 {
			t.Fatalf("dispatched %d rows after the fence voided them, want 0 — "+
				"this is the resurrection 6.4 says cannot happen", n)
		}
	})

	// A row nothing can project must not be retried silently forever with no
	// record. It stays pending so 8.3's staleness alert surfaces it.
	t.Run("an unrenderable row records its attempt and stays pending", func(t *testing.T) {
		cleanRows()
		unknown := fact
		unknown.EventType = "NoSuchEvent"
		enqueue(t, unknown)

		if _, err := dispatcher.Once(ctx); err == nil {
			t.Fatal("dispatching an unrenderable row returned no error")
		}

		var attempts int
		var lastError *string
		if err := database.Pool().QueryRow(ctx, `
			SELECT attempts, last_error FROM authorization_outbox
			 WHERE aggregate_id = $1::uuid AND event_type = 'NoSuchEvent'
		`, unitID).Scan(&attempts, &lastError); err != nil {
			t.Fatalf("read row: %v", err)
		}
		if attempts != 1 {
			t.Fatalf("attempts = %d, want 1", attempts)
		}
		if lastError == nil || *lastError == "" {
			t.Fatal("last_error was not recorded")
		}
		if pendingRows(t) != 1 {
			t.Fatal("the unrenderable row is no longer pending; the staleness alert would never see it")
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
