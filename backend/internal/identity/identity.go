// Package identity owns people and the principals that act for them — the
// one part of the model that is deliberately global (05.2.2, A2.3).
//
// Transport-neutral by intent (REVIEW.md B7): every function takes a
// transaction and returns a value.
//
// # Why a global row is created through a tenant-scoped endpoint
//
// `person` carries no `tenant_id` and is one of A2.1's four RLS exemptions.
// One human is one row across every tenant, which is the whole reason the
// table is shaped this way — and it means the row itself can state no
// opinion about who may create it. A2.1 promised "application-level
// relationship checks" and, until ADR-114, there were none: the table had no
// permission guarding it anywhere.
//
// ADR-114 puts that permission on the TENANT — `tenant.may_register_person`
// — because registering somebody is an act performed over a tenant by
// somebody acting for it, typically whoever takes an application at the
// front desk (05.3.4). The row it produces belongs to no one.
//
// Nothing here publishes an authorization fact. A1.2's `person` type carries
// `principal` and `guardian` and no tenant relation; a person is reachable
// in the graph through the memberships and principals that name them, never
// on their own. Registering somebody grants nothing, which is exactly what
// makes the permission safe to hold at a front desk.
package identity

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Person is a human in the movement (A2.3, 05.2.2).
//
// Every name field but DisplayName is a pointer, because A2.3 makes them
// nullable and a real register holds people known by one name. Flattening
// them to "" would make "not recorded" and "recorded as empty"
// indistinguishable in a table whose whole job is identifying somebody.
type Person struct {
	ID                string  `json:"id"`
	GivenName         *string `json:"given_name"`
	FamilyName        *string `json:"family_name"`
	DisplayName       string  `json:"display_name"`
	DateOfBirth       *string `json:"date_of_birth"`
	PreferredLanguage *string `json:"preferred_language"`
	Status            string  `json:"status"`
}

// NewPerson is what a caller must supply to register one.
type NewPerson struct {
	GivenName         *string
	FamilyName        *string
	DisplayName       string
	DateOfBirth       *string // ISO 8601 date; the database parses it
	PreferredLanguage *string
}

// RegisterPerson creates a person row, status ACTIVE.
//
// It creates no principal. `principal.idp_subject` comes from the identity
// provider, and somebody registered at a front desk has not authenticated
// anywhere — a principal invented here would be a credential nobody can use
// occupying a UNIQUE index the real one will later need.
//
// It does not look for duplicates either. 11.2 carries that: the heuristic
// exists to be designed, and 11.1 requires it never disclose that a person
// is already known to another tenant.
func RegisterPerson(ctx context.Context, tx pgx.Tx, in NewPerson) (Person, error) {
	var p Person
	err := tx.QueryRow(ctx, `
		INSERT INTO person
		    (id, given_name, family_name, display_name,
		     date_of_birth, preferred_language, status)
		VALUES (gen_random_uuid(), $1, $2, $3, $4::date, $5, 'ACTIVE')
		RETURNING id::text, given_name, family_name, display_name,
		          to_char(date_of_birth, 'YYYY-MM-DD'), preferred_language, status
	`, in.GivenName, in.FamilyName, in.DisplayName, in.DateOfBirth, in.PreferredLanguage).
		Scan(&p.ID, &p.GivenName, &p.FamilyName, &p.DisplayName,
			&p.DateOfBirth, &p.PreferredLanguage, &p.Status)
	if err != nil {
		return Person{}, fmt.Errorf("identity: register person: %w", err)
	}
	return p, nil
}
