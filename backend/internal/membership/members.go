// members.go is ADR-104's read model for a unit's members: check the scope
// once, then page the rows by keyset SQL under RLS. There is no per-row
// check and no ListObjects call, and 8.11 forbids both.
package membership

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/page"
)

// UnitMember is one membership in a unit's list.
//
// It carries the person's id but not their name. ADR-104 authorizes the
// SCOPE, so every row in the page is visible to a caller who cleared
// `member_read` on the unit — but "may list this unit's members" is not
// "may read a person's record", and joining `person` here would make the
// weaker permission carry the stronger one's data.
type UnitMember struct {
	MembershipID string  `json:"membership_id"`
	PersonID     string  `json:"person_id"`
	Number       string  `json:"number"`
	State        string  `json:"state"`
	AdmittedAt   *string `json:"admitted_at"`
}

// ListUnitMembers pages the memberships whose org_unit_id is EXACTLY the
// unit named, never its descendants.
//
// This is ADR-104's permitted under-report, and it looks like a bug, so:
// `organizational_unit.member_read` DOES reach descendants in the graph
// (A1.4), which means a caller authorized at a parent lists a child's
// members by naming the child. What they never see is a child's members
// folded into the parent's page. Authority propagates down the DAG; the
// membership rows do not fold up it.
//
// Folding them in would need either a recursive query per page — unbounded
// by page size, which is what a keyset pager exists to bound — or a
// denormalized closure table this design does not carry.
//
// A membership with a NULL org_unit_id is an association-level membership
// and appears under no unit's list at all. `org_unit_id = $2` does not match
// NULL, so that follows from the SQL rather than from a special case, which
// is why it is stated here: nothing in the query looks like the decision it
// is. Open in 11.2.
func ListUnitMembers(
	ctx context.Context, tx pgx.Tx, tenantID, unitID string, p page.Params,
) ([]UnitMember, error) {
	// number is UNIQUE (tenant_id, number) by A2.4, so it needs no tiebreak
	// — unlike a bundle's name, which does.
	var afterNumber string
	if len(p.Key) == 1 {
		afterNumber = p.Key[0]
	}

	rows, err := tx.Query(ctx, `
		SELECT id::text, person_id::text, number, state,
		       to_char(admitted_at, 'YYYY-MM-DD"T"HH24:MI:SSOF:00')
		  FROM membership
		 WHERE tenant_id = $1
		   AND org_unit_id = $2::uuid
		   AND ($3 = '' OR number > $3)
		 ORDER BY number
		 LIMIT $4
	`, tenantID, unitID, afterNumber, p.Fetch())
	if err != nil {
		return nil, fmt.Errorf("membership: list unit members: %w", err)
	}
	defer rows.Close()

	var out []UnitMember
	for rows.Next() {
		var m UnitMember
		if err := rows.Scan(&m.MembershipID, &m.PersonID, &m.Number,
			&m.State, &m.AdmittedAt); err != nil {
			return nil, fmt.Errorf("membership: scan unit member: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UnitMemberKey is the sort key ListUnitMembers orders by.
func UnitMemberKey(m UnitMember) []string { return []string{m.Number} }

// IsActiveMember reports whether a person holds an ACTIVE membership in the
// tenant. It is a PostgreSQL fact, not a graph check, and it exists for the
// one caller whose authority is "I am a member": declaring one's own absence
// (A3.10), where no relation on any object means that.
func IsActiveMember(ctx context.Context, tx pgx.Tx, tenantID, personID string) (bool, error) {
	var held bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM membership
		     WHERE tenant_id = $1 AND person_id = $2::uuid AND state = 'ACTIVE')
	`, tenantID, personID).Scan(&held)
	if err != nil {
		return false, fmt.Errorf("membership: read active membership: %w", err)
	}
	return held, nil
}

// DirectoryEntry is one membership as the front desk sees it: with the
// person's name, which UnitMember deliberately lacks.
type DirectoryEntry struct {
	MembershipID string  `json:"membership_id"`
	PersonID     string  `json:"person_id"`
	DisplayName  string  `json:"display_name"`
	Number       string  `json:"number"`
	State        string  `json:"state"`
	PlanID       string  `json:"plan_id"`
	OrgUnitID    *string `json:"org_unit_id"`
}

// ListDirectory pages every membership in the tenant with the holder's name,
// optionally narrowed by q — a prefix of the number or a fragment of the name.
//
// UnitMember carries no name because `member_read` on a unit must not carry a
// person's record. This list is gated on `may_approve_membership`, the
// authority that took the application and so already knows the name, which is
// why the join is permitted here and was refused there (A3.12).
//
// The person row is global (A2.1) and is read only THROUGH a membership of
// this tenant, so the tenant sees names of its own members and no others.
func ListDirectory(
	ctx context.Context, tx pgx.Tx, tenantID, q string, p page.Params,
) ([]DirectoryEntry, error) {
	var afterNumber string
	if len(p.Key) == 1 {
		afterNumber = p.Key[0]
	}
	rows, err := tx.Query(ctx, `
		SELECT m.id::text, m.person_id::text, pe.display_name, m.number, m.state,
		       m.plan_id::text, m.org_unit_id::text
		  FROM membership m JOIN person pe ON pe.id = m.person_id
		 WHERE m.tenant_id = $1
		   AND ($2::text = '' OR m.number > $2::text)
		   AND ($3::text = '' OR m.number ILIKE $3::text || '%'
		                      OR pe.display_name ILIKE '%' || $3::text || '%')
		 ORDER BY m.number
		 LIMIT $4
	`, tenantID, afterNumber, q, p.Fetch())
	if err != nil {
		return nil, fmt.Errorf("membership: list directory: %w", err)
	}
	defer rows.Close()

	var out []DirectoryEntry
	for rows.Next() {
		var e DirectoryEntry
		if err := rows.Scan(&e.MembershipID, &e.PersonID, &e.DisplayName, &e.Number,
			&e.State, &e.PlanID, &e.OrgUnitID); err != nil {
			return nil, fmt.Errorf("membership: scan directory entry: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DirectoryKey is the sort key ListDirectory orders by.
func DirectoryKey(e DirectoryEntry) []string { return []string{e.Number} }
