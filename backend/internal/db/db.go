// Package db owns the PostgreSQL connection pool and the one pattern every
// tenant-plane query must use to reach it: a transaction that names its
// tenant before touching a single row (D4, A2.1, ADR-108).
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps the pool. The zero value is not usable — construct with Open.
type DB struct {
	pool *pgxpool.Pool
}

// Open establishes the pool. It does not itself set app.tenant_id — that
// happens per transaction, never on the shared connection, because the pool
// hands a connection back to an unrelated request once it is returned (D4).
func Open(ctx context.Context, connString string) (*DB, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("db: open pool: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Close releases every connection in the pool.
func (d *DB) Close() {
	d.pool.Close()
}

// Health confirms the database is reachable.
func (d *DB) Health(ctx context.Context) error {
	if err := d.pool.Ping(ctx); err != nil {
		return fmt.Errorf("db: health check: %w", err)
	}
	return nil
}

// Pool exposes the underlying pool for the handful of tables 8.2 exempts
// from row-level security — person, principal, guardianship, restriction —
// which carry no tenant_id and so need no InTenantTx. Every other query
// must go through InTenantTx instead of this.
func (d *DB) Pool() *pgxpool.Pool {
	return d.pool
}

// InTenantTx runs fn inside a transaction that has named its tenant, per
// D4:
//
//	BEGIN
//	SELECT set_config('app.tenant_id', $1, true)   -- true = local to the tx
//	... fn ...
//	COMMIT
//
// SET LOCAL cannot take a parameter, hence set_config. The policy
// (0001_init.sql, A2.1) reads app.tenant_id with current_setting's
// missing_ok defaulted to false, so a query that reaches a tenant-scoped
// table without this wrapper raises rather than silently returning zero
// rows. That is deliberate fail-closed behaviour and must not be softened —
// for example by catching and swallowing the "unset app.tenant_id" error.
//
// app.tenant_id is never set outside a transaction: the pool hands the
// underlying connection to the next, unrelated request once this one
// returns it, and a value set outside a transaction would leak forward.
func (d *DB) InTenantTx(ctx context.Context, tenantID string, fn func(pgx.Tx) error) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // no-op once committed; reports nothing on success

	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		return fmt.Errorf("db: set tenant context: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit tx: %w", err)
	}
	return nil
}
