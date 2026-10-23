package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/config"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/membership"
	"github.com/nandgator/ymca/backend/internal/outbox"
)

func init() {
	commands["reconcile-coverage"] = reconcileCoverageCommand
}

// reconcileCoverageCommand implements `api reconcile-coverage [--repair]`: it
// compares every tenant's memberships with the authorization store and prints
// one JSON line per disagreement (membership.ReconcileTenant says what the two
// kinds mean).
//
// Without --repair it changes nothing and exits non-zero if there is drift, so
// it can run from a scheduler and alert. With --repair it grants what is
// missing through the outbox and revokes what should not exist on the
// synchronous path, and exits non-zero only if a repair could not be made.
// It is not behind the dev tag: it is an operator's tool and needs no
// credential.
func reconcileCoverageCommand(args []string) error {
	repair := false
	for _, a := range args {
		if a != "--repair" {
			return fmt.Errorf("usage: api reconcile-coverage [--repair]")
		}
		repair = true
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	writer, err := outbox.NewFGAWriter(cfg)
	if err != nil {
		return fmt.Errorf("open authorization store: %w", err)
	}
	unresolved, checked, err := reconcileAll(ctx, pool, writer, repair, os.Stdout)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "reconciled %d tenants; %d disagreements left\n", checked, unresolved)
	if unresolved > 0 {
		return fmt.Errorf("%d disagreements between PostgreSQL and the authorization store", unresolved)
	}
	return nil
}

type coverageStore interface {
	membership.Inspector
	membership.Revoker
}

// reconcileAll runs ReconcileTenant over every tenant, each in its own tenant
// transaction, writing a JSON line per disagreement to out. It returns how many
// are left unresolved: all of them when not repairing, none when repairing and
// every repair held.
func reconcileAll(
	ctx context.Context, pool *db.DB, store coverageStore, repair bool, out interface{ Write([]byte) (int, error) },
) (unresolved, tenants int, err error) {
	rows, err := pool.Pool().Query(ctx, `SELECT id::text FROM tenant ORDER BY id`)
	if err != nil {
		return 0, 0, fmt.Errorf("list tenants: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	enc := json.NewEncoder(out)
	for _, id := range ids {
		var found []membership.Drift
		if err := pool.InTenantTx(ctx, id, func(tx pgx.Tx) error {
			var err error
			found, err = membership.ReconcileTenant(ctx, tx, id, store, store, repair)
			return err
		}); err != nil {
			return unresolved, tenants, fmt.Errorf("tenant %s: %w", id, err)
		}
		for _, d := range found {
			if err := enc.Encode(struct {
				TenantID string `json:"tenant_id"`
				membership.Drift
			}{id, d}); err != nil {
				return unresolved, tenants, err
			}
			if !d.Repaired {
				unresolved++
			}
		}
		tenants++
	}
	return unresolved, tenants, nil
}
