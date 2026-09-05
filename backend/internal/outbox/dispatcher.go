// dispatcher.go is ADR-101 obligation 3: claim a pending row, render the
// fact into a tuple against the CURRENT model, write it, and mark the row
// dispatched — holding the row lock across the OpenFGA write for the whole
// of it.
//
// Holding a row lock across a network call is normally a thing to avoid, and
// here it is the requirement. Releasing it before the write reopens the
// window a revocation can slip through: the revocation would void nothing
// (the row is already claimed and no longer pending) and delete a tuple that
// this dispatcher then writes. SKIP LOCKED confines the cost to one row, and
// a statement timeout bounds it.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Tuple is one OpenFGA relationship, as a Renderer produces it.
type Tuple struct {
	User     string
	Relation string
	Object   string
}

// Renderer turns a stored fact into the tuples the CURRENT model requires.
// It is the half of ADR-101 that keeps model versions out of stored rows: a
// model change edits a renderer, never the undispatched rows.
type Renderer func(payload json.RawMessage) ([]Tuple, error)

// Writer is the subset of OpenFGA the dispatcher needs, narrowed so the
// dispatcher's ordering can be tested without a live store.
type Writer interface {
	WriteTuples(ctx context.Context, tuples []Tuple) error
}

// ErrNoRenderer means a row names an event type nothing knows how to
// project. It is deliberately fatal for that row rather than skipped: a
// silently unprojected grant is authority that never arrives, and ADR-107
// warns that failure is invisible from the outside.
var ErrNoRenderer = errors.New("outbox: no renderer for event type")

// Dispatcher drains authorization_outbox.
type Dispatcher struct {
	pool      *pgxpool.Pool
	writer    Writer
	renderers map[string]Renderer
	logger    *slog.Logger

	// Interval is how long to wait when a pass finds nothing. A pass that
	// found work polls again immediately.
	Interval time.Duration
	// Batch bounds how many rows one pass claims.
	Batch int
}

// New builds a Dispatcher. The pool is used directly rather than through
// InTenantTx because authorization_outbox carries no tenant_id and so has no
// RLS policy (A2.1) — it spans tenants by construction, and a dispatcher
// that had to name one could not drain the table.
func New(pool *pgxpool.Pool, writer Writer, renderers map[string]Renderer, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		pool:      pool,
		writer:    writer,
		renderers: renderers,
		logger:    logger,
		Interval:  time.Second,
		Batch:     50,
	}
}

// Run drains the table until ctx is cancelled. 7.4 makes a stalled
// dispatcher an operational alert rather than a blocking failure, so a failed
// pass is logged and retried rather than returned.
func (d *Dispatcher) Run(ctx context.Context) {
	for {
		dispatched, err := d.Once(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			d.logger.ErrorContext(ctx, "outbox: dispatch pass failed", "error", err)
		}
		if dispatched > 0 && ctx.Err() == nil {
			continue // there may be more
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(d.Interval):
		}
	}
}

// Once runs a single pass and reports how many rows it dispatched.
func (d *Dispatcher) Once(ctx context.Context) (int, error) {
	var dispatched int
	for i := 0; i < d.Batch; i++ {
		done, err := d.dispatchOne(ctx)
		if err != nil {
			return dispatched, err
		}
		if !done {
			return dispatched, nil
		}
		dispatched++
	}
	return dispatched, nil
}

// dispatchOne claims at most one row and projects it. Everything from the
// claim to the mark happens in one transaction, so the row lock is held
// across the OpenFGA write.
func (d *Dispatcher) dispatchOne(ctx context.Context) (bool, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("outbox: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var (
		id        int64
		eventType string
		payload   json.RawMessage
	)
	err = tx.QueryRow(ctx, `
		SELECT id, event_type, payload
		  FROM authorization_outbox
		 WHERE dispatched_at IS NULL AND voided_at IS NULL
		 ORDER BY created_at, id
		   FOR UPDATE SKIP LOCKED
		 LIMIT 1
	`).Scan(&id, &eventType, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("outbox: claim row: %w", err)
	}

	renderer, ok := d.renderers[eventType]
	if !ok {
		return false, d.fail(ctx, tx, id, fmt.Errorf("%w: %q", ErrNoRenderer, eventType))
	}

	tuples, err := renderer(payload)
	if err != nil {
		return false, d.fail(ctx, tx, id, fmt.Errorf("render %s: %w", eventType, err))
	}

	// The network call, with the row lock still held. A revocation for this
	// fact is blocked behind that lock until this transaction ends, so it
	// cannot delete a tuple this write is about to create.
	if err := d.writer.WriteTuples(ctx, tuples); err != nil {
		return false, d.fail(ctx, tx, id, fmt.Errorf("write tuples for %s: %w", eventType, err))
	}

	if _, err := tx.Exec(ctx,
		`UPDATE authorization_outbox SET dispatched_at = now() WHERE id = $1`, id); err != nil {
		return false, fmt.Errorf("outbox: mark dispatched: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("outbox: commit dispatch: %w", err)
	}
	return true, nil
}

// fail records the attempt against the row and commits that record, so a
// permanently broken row is visible rather than retried in a tight loop
// forever with nothing to show for it. The row stays pending: 8.3's alert on
// undispatched rows older than a threshold is what surfaces it.
//
// It deliberately commits the attempt even though the dispatch failed. The
// alternative — rolling back — loses the error and the count, and the next
// pass claims the same row with no record that it has ever been tried.
func (d *Dispatcher) fail(ctx context.Context, tx pgx.Tx, id int64, cause error) error {
	if _, err := tx.Exec(ctx, `
		UPDATE authorization_outbox
		   SET attempts = attempts + 1, last_error = $2
		 WHERE id = $1
	`, id, cause.Error()); err != nil {
		return fmt.Errorf("outbox: record attempt: %w (original: %v)", err, cause)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("outbox: commit attempt: %w (original: %v)", err, cause)
	}
	return cause
}
