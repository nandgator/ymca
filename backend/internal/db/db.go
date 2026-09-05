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

// Pool exposes the underlying pool for the tables that carry no tenant_id
// and so need no InTenantTx:
//
//	person, principal, guardianship, restriction   8.2's RLS exemptions,
//	                                               deliberately global
//	tenant                                         has no tenant_id to be
//	                                               policed by; it IS the
//	                                               tenant (A2.2)
//	authorization_outbox                           spans tenants by
//	                                               construction (A2.1)
//	platform_audit_event                           platform plane; no
//	                                               tenant exists to name
//	                                               (ADR-112)
//
// `authorization_outbox` is the one a reader might not expect. It has no
// tenant_id and therefore no policy, because the dispatcher drains the whole
// table: a dispatcher that had to name a tenant could not do its job. That is
// why internal/outbox takes a *pgxpool.Pool rather than a *DB.
//
// Every other query must go through InTenantTx instead of this. A new caller
// reaching for Pool() for anything not listed above is almost certainly a
// defect.
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

// InTx runs fn inside a transaction that names NO tenant.
//
// It exists for the platform plane (ADR-111, ADR-113) and for the tables
// Pool() lists above — the ones with no tenant_id and therefore no policy.
// Provisioning a tenant is the motivating case: tenant, person, principal
// and authorization_outbox are all unpoliced, and there is no tenant
// context to run inside because the tenant is what is being created.
//
// It is NOT a way around InTenantTx, and it cannot quietly become one. A
// query it runs against any policied table raises
//
//	ERROR: invalid input syntax for type uuid: ""
//
// because the policy reads current_setting('app.tenant_id') with missing_ok
// defaulted to false and finds the empty string. That is loud and
// fail-closed, exactly as A2.1 intends, and it must not be softened — in
// particular do not "fix" it by setting app.tenant_id to a zero uuid or by
// catching that error, either of which would turn a refusal into a silently
// empty result.
//
// A reviewer seeing a new caller of InTx should ask which of Pool()'s
// tables it touches. If the answer is none of them, it is a defect.
func (d *DB) InTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // no-op once committed

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit tx: %w", err)
	}
	return nil
}
