package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/config"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/outbox"
)

func init() {
	commands["bootstrap-operator"] = bootstrapOperatorCommand
}

// bootstrapOperatorCommand implements `api bootstrap-operator <idp-subject>
// <display-name>`: it makes the first platform operator, the one act that
// cannot go through the API because the API is what an operator uses.
//
// Unlike mint-token it is NOT behind the dev build tag. A deployment needs its
// first operator, and "how does anyone ever provision a tenant" must have an
// answer in a build that cannot mint a credential. It writes no credential: the
// identity provider supplies those, and the subject given here is what must
// match.
func bootstrapOperatorCommand(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: api bootstrap-operator <idp-subject> <display-name>")
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
		return fmt.Errorf("open authorization writer: %w", err)
	}
	principalID, created, err := bootstrapOperator(ctx, pool, writer, args[0], args[1])
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(os.Stdout, "created platform operator %s (principal %s)\n", args[0], principalID)
	} else {
		fmt.Fprintf(os.Stdout, "platform operator %s already exists (principal %s); authority re-asserted\n",
			args[0], principalID)
	}
	return nil
}

// tupleWriter is the part of the OpenFGA writer bootstrap needs.
type tupleWriter interface {
	WriteTuples(ctx context.Context, tuples []outbox.Tuple) error
}

// bootstrapOperator creates a person and a STAFF principal for the subject —
// or finds the existing principal — and writes `principal:<id> operator
// platform:main`. Safe to run twice: the rows are found rather than
// duplicated and WriteTuples tolerates redelivery, so a run that died between
// the database and OpenFGA is repaired by running it again.
//
// The tuple is written directly and not through the outbox because the outbox
// is tenant-scoped infrastructure and there is no tenant yet; this is the one
// authorization fact written outside it, and it is why the command exists as
// a command rather than an endpoint.
func bootstrapOperator(
	ctx context.Context, pool *db.DB, writer tupleWriter, subject, displayName string,
) (principalID string, created bool, err error) {
	if subject == "" || displayName == "" {
		return "", false, errors.New("bootstrap: subject and display name are required")
	}
	err = pool.InTx(ctx, func(tx pgx.Tx) error {
		scanErr := tx.QueryRow(ctx,
			`SELECT id::text FROM principal WHERE idp_subject = $1`, subject).Scan(&principalID)
		if scanErr == nil {
			return nil
		}
		if !errors.Is(scanErr, pgx.ErrNoRows) {
			return scanErr
		}
		var personID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO person (id, display_name, status)
			VALUES (gen_random_uuid(), $1, 'ACTIVE') RETURNING id::text`,
			displayName).Scan(&personID); err != nil {
			return err
		}
		created = true
		return tx.QueryRow(ctx, `
			INSERT INTO principal (id, person_id, idp_subject, kind, status)
			VALUES (gen_random_uuid(), $1::uuid, $2, 'STAFF', 'ACTIVE') RETURNING id::text`,
			personID, subject).Scan(&principalID)
	})
	if err != nil {
		return "", false, fmt.Errorf("bootstrap: write operator rows: %w", err)
	}
	if err := writer.WriteTuples(ctx, []outbox.Tuple{{
		User: "principal:" + principalID, Relation: "operator", Object: "platform:main",
	}}); err != nil {
		return "", false, fmt.Errorf("bootstrap: write operator tuple: %w", err)
	}
	return principalID, created, nil
}
