package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/config"
	"github.com/nandgator/ymca/backend/internal/db"
)

func init() {
	commands["bootstrap-principal"] = bootstrapPrincipalCommand
}

// errSubjectBound is returned when the identity-provider subject already
// identifies a principal belonging to a different person. One subject is one
// principal (A2.3), so the command refuses rather than rebinding it.
var errSubjectBound = errors.New("bootstrap: idp subject already identifies a principal of another person")

// bootstrapPrincipalCommand implements `api bootstrap-principal <person-id>
// <idp-subject>`: it gives an already-registered person a login.
//
// POST /people creates a person and nothing creates a principal, because
// idp_subject comes from the identity provider (ADR-106) and that adapter does
// not exist yet. Until it does, this is the one way a member who was admitted
// can sign in. Like bootstrap-operator it is NOT behind the dev build tag and
// writes no credential: the provider supplies those, and the subject given
// here is what must match. It writes no authorization tuple either — a
// PERSONAL principal holds nothing of its own; what it may do flows from the
// person's memberships and role assignments.
func bootstrapPrincipalCommand(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: api bootstrap-principal <person-id> <idp-subject>")
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
	principalID, created, err := bootstrapPrincipal(ctx, pool, args[0], args[1])
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(os.Stdout, "created principal %s for person %s (subject %s)\n", principalID, args[0], args[1])
	} else {
		fmt.Fprintf(os.Stdout, "principal %s already binds subject %s to person %s; nothing to do\n",
			principalID, args[1], args[0])
	}
	return nil
}

// bootstrapPrincipal creates a PERSONAL principal for an ACTIVE person, or
// finds the one that already binds this subject to this person. Safe to run
// twice. A subject bound to anyone else is refused, never rebound: moving a
// login between people would silently hand over everything the first one held.
func bootstrapPrincipal(
	ctx context.Context, pool *db.DB, personID, subject string,
) (principalID string, created bool, err error) {
	if personID == "" || subject == "" {
		return "", false, errors.New("bootstrap: person id and subject are required")
	}
	err = pool.InTx(ctx, func(tx pgx.Tx) error {
		var status string
		scanErr := tx.QueryRow(ctx, `SELECT status FROM person WHERE id = $1::uuid`, personID).Scan(&status)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return fmt.Errorf("bootstrap: no such person %s", personID)
		}
		if scanErr != nil {
			return scanErr
		}
		if status != "ACTIVE" {
			return fmt.Errorf("bootstrap: person %s is %s, not ACTIVE", personID, status)
		}
		var boundTo string
		scanErr = tx.QueryRow(ctx,
			`SELECT id::text, person_id::text FROM principal WHERE idp_subject = $1`, subject).
			Scan(&principalID, &boundTo)
		if scanErr == nil {
			if boundTo != personID {
				return errSubjectBound
			}
			return nil
		}
		if !errors.Is(scanErr, pgx.ErrNoRows) {
			return scanErr
		}
		created = true
		return tx.QueryRow(ctx, `
			INSERT INTO principal (id, person_id, idp_subject, kind, status)
			VALUES (gen_random_uuid(), $1::uuid, $2, 'PERSONAL', 'ACTIVE') RETURNING id::text`,
			personID, subject).Scan(&principalID)
	})
	if err != nil {
		return "", false, err
	}
	return principalID, created, nil
}
