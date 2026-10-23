//go:build integration && dev

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/nandgator/ymca/backend/internal/db"
)

// The claim: a registered person can be given a login exactly once, the command
// repairs by being run again, and a subject can never be moved to another
// person. Needs only PostgreSQL.
func TestBootstrapPrincipal(t *testing.T) {
	ctx := context.Background()
	pool, err := db.Open(ctx, mustEnvVar(t, "YMCA_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	const subA, subB = "bootstrap-principal-test-a", "bootstrap-principal-test-b"
	var personA, personB, personGone string
	for _, p := range []struct {
		dst    *string
		name   string
		status string
	}{{&personA, "Principal Test A", "ACTIVE"}, {&personB, "Principal Test B", "ACTIVE"},
		{&personGone, "Principal Test Gone", "DECEASED"}} {
		if err := pool.Pool().QueryRow(ctx, `INSERT INTO person (display_name, status)
			VALUES ($1, $2) RETURNING id::text`, p.name, p.status).Scan(p.dst); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		bg := context.Background()
		if _, err := pool.Pool().Exec(bg, `DELETE FROM principal WHERE idp_subject = ANY($1)`,
			[]string{subA, subB}); err != nil {
			t.Errorf("cleanup could not delete the principals: %v", err)
		}
		if _, err := pool.Pool().Exec(bg, `DELETE FROM person WHERE id = ANY($1::uuid[])`,
			[]string{personA, personB, personGone}); err != nil {
			t.Errorf("cleanup could not delete the people: %v", err)
		}
		var left int
		if err := pool.Pool().QueryRow(bg, `SELECT count(*) FROM principal WHERE idp_subject = ANY($1)`,
			[]string{subA, subB}).Scan(&left); err != nil || left != 0 {
			t.Errorf("cleanup left %d principals behind (err %v)", left, err)
		}
	})

	id1, created, err := bootstrapPrincipal(ctx, pool, personA, subA)
	if err != nil || !created {
		t.Fatalf("first run: id %q created %v err %v", id1, created, err)
	}
	var kind, status, bound string
	if err := pool.Pool().QueryRow(ctx, `SELECT kind, status, person_id::text FROM principal WHERE id = $1::uuid`,
		id1).Scan(&kind, &status, &bound); err != nil {
		t.Fatal(err)
	}
	if kind != "PERSONAL" || status != "ACTIVE" || bound != personA {
		t.Errorf("principal is %s/%s/%s, want PERSONAL/ACTIVE/%s", kind, status, bound, personA)
	}

	id2, created, err := bootstrapPrincipal(ctx, pool, personA, subA)
	if err != nil || created || id2 != id1 {
		t.Errorf("second run: id %q (want %q) created %v err %v", id2, id1, created, err)
	}

	if _, _, err := bootstrapPrincipal(ctx, pool, personB, subA); !errors.Is(err, errSubjectBound) {
		t.Errorf("rebinding a subject: err %v, want errSubjectBound", err)
	}
	var owner string
	if err := pool.Pool().QueryRow(ctx, `SELECT person_id::text FROM principal WHERE idp_subject = $1`,
		subA).Scan(&owner); err != nil || owner != personA {
		t.Errorf("subject now belongs to %q (err %v), want it left with %s", owner, err, personA)
	}

	if _, _, err := bootstrapPrincipal(ctx, pool, personGone, subB); err == nil {
		t.Error("a DECEASED person was given a login")
	}
	if _, _, err := bootstrapPrincipal(ctx, pool, "00000000-0000-0000-0000-000000000000", subB); err == nil {
		t.Error("a person who does not exist was given a login")
	}
	var n int
	if err := pool.Pool().QueryRow(ctx, `SELECT count(*) FROM principal WHERE idp_subject = $1`, subB).Scan(&n); err != nil || n != 0 {
		t.Errorf("refused runs left %d principals for %s (err %v)", n, subB, err)
	}
}
