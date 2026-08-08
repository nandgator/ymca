// Package postgres implements the internal/app port interfaces against
// Postgres via pgx. Each file here is a thin adapter — all business rules
// live in internal/domain and internal/app; nothing here should contain a
// decision, only a translation between Go structs and SQL rows.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	pgxuuid "github.com/vgarvardt/pgx-google-uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Open creates a connection pool. connString is a standard Postgres URL,
// e.g. "postgres://user:pass@host:5432/dbname?sslmode=disable".
//
// pgx/v5 has no built-in codec for google/uuid.UUID (the type every
// domain/app struct in this codebase uses for IDs) — without registering
// one, every uuid.UUID field and every uuid.UUID/[]uuid.UUID query
// parameter in this package would fail to scan or encode at runtime, not
// compile time, which makes it an easy thing to miss until it's live. We
// register it once here, on every pooled connection, so nothing downstream
// has to think about it again.
func Open(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("parsing database url: %w", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		pgxuuid.Register(conn.TypeMap())
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return pool, nil
}
