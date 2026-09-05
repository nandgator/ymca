// roles.go is step 2 of 6.1 (ADR-109): resolve the role assignments that
// confer one permission on one principal, right now, and render them as the
// contextual tuples the graph step will be given.
//
// Everything temporal lives in this query's WHERE clause. That placement is
// the whole mechanism: an assignment outside its term, missing a required
// clearance, covered by an ACTING holding, or suppressed by a restriction is
// never returned, so no tuple for it reaches OpenFGA and no path through it
// exists to be found. ADR-070's "inert the moment it expires" is true here by
// construction rather than by a sweeper — and, crucially, without asking the
// graph a question it cannot answer, since Check returns a boolean and not a
// path.
package authz

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ContextualTuple is one tuple supplied to a single Check and never stored.
type ContextualTuple struct {
	User     string
	Relation string
	Object   string
}

// effectiveAssignments is the step 2 query.
//
// The clauses, in the order they appear:
//
//	rp.permission          only assignments conferring THIS permission
//	ra.scope_type          only where the scope can carry the relation —
//	                       a definition may bundle tenant- and unit-scoped
//	                       permissions, and each reaches only where it lands
//	revoked_at / status     revocation and retirement are immediate
//	valid_from / valid_until the term window                     ADR-069/070
//	NOT EXISTS acting      a substantive assignment is inert while an ACTING
//	                       one covers it, without being deleted       05.6.4
//	NOT EXISTS clearance   every required clearance must be currently valid
//	                       for the subject's person                    ADR-087
//	NOT EXISTS restriction the two restriction kinds that act on the role
//	                       path rather than on a named permission      05.9.4
//
// The restriction limb is deliberately here and not in step 4.
// NO_ROLE_ASSIGNMENT and SUSPENDED_PENDING_REVIEW suppress every assignment;
// NO_YOUTH_FACING_ROLES suppresses only those whose definition is
// youth_facing. None of them names a permission, so none of them can be
// expressed as a restriction_kind_permission row.
const effectiveAssignments = `
SELECT ra.id::text, ra.scope_id::text
FROM role_assignment ra
JOIN role_definition rd ON rd.id = ra.role_definition_id
JOIN role_permission rp ON rp.role_definition_id = rd.id
WHERE ra.subject_principal_id = $1::uuid
  AND rp.permission = $2
  AND ra.scope_type = $3
  AND ra.revoked_at IS NULL
  AND rd.status = 'ACTIVE'
  AND ra.valid_from <= now()
  AND (ra.valid_until IS NULL OR ra.valid_until > now())
  AND NOT EXISTS (
      SELECT 1 FROM role_assignment cover
      WHERE cover.substantive_assignment_id = ra.id
        AND cover.revoked_at IS NULL
        AND cover.valid_from <= now()
        AND (cover.valid_until IS NULL OR cover.valid_until > now())
  )
  AND NOT EXISTS (
      SELECT 1 FROM role_required_clearance rrc
      WHERE rrc.role_definition_id = rd.id
        AND NOT EXISTS (
            SELECT 1
            FROM clearance c
            JOIN verification v ON v.id = c.verification_id
            JOIN principal p ON p.id = ra.subject_principal_id
            WHERE c.person_id = p.person_id
              AND v.type = rrc.verification_type
              AND v.status = 'VERIFIED'
              AND (c.valid_from IS NULL OR c.valid_from <= current_date)
              AND (c.valid_until IS NULL OR c.valid_until >= current_date)
        )
  )
  AND NOT EXISTS (
      SELECT 1
      FROM restriction r
      JOIN principal p ON p.id = ra.subject_principal_id
      WHERE r.person_id = p.person_id
        AND r.lifted_at IS NULL
        AND (r.expires_at IS NULL OR r.expires_at > now())
        AND (r.tenant_id IS NULL OR r.tenant_id = ra.tenant_id)
        AND (
              r.kind IN ('NO_ROLE_ASSIGNMENT', 'SUSPENDED_PENDING_REVIEW')
           OR (r.kind = 'NO_YOUTH_FACING_ROLES' AND rd.youth_facing)
        )
  )
`

// objectTypeOf splits "<object_type>.<relation>" — 8.1's permission name.
func objectTypeOf(permission string) (objectType, relation string, err error) {
	objectType, relation, found := strings.Cut(permission, ".")
	if !found || objectType == "" || relation == "" {
		return "", "", fmt.Errorf(
			"authz: %q is not a permission name of the form <object_type>.<relation> (8.1)",
			permission)
	}
	return objectType, relation, nil
}

// roleTuples runs step 2 and renders the result.
//
// Two tuples per assignment, because the graph reaches the principal through
// the assignment's holder userset rather than directly — which is what makes
// the role path distinguishable from a direct grant in A1.2, and what lets
// ADR-110's grantable set be expressed as a type restriction at all.
func roleTuples(ctx context.Context, tx pgx.Tx, principalID, permission string) ([]ContextualTuple, error) {
	objectType, relation, err := objectTypeOf(permission)
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, effectiveAssignments, principalID, permission, objectType)
	if err != nil {
		return nil, fmt.Errorf("authz: role step: %w", err)
	}
	defer rows.Close()

	var tuples []ContextualTuple
	for rows.Next() {
		var assignmentID, scopeID string
		if err := rows.Scan(&assignmentID, &scopeID); err != nil {
			return nil, fmt.Errorf("authz: role step scan: %w", err)
		}
		assignment := "role_assignment:" + assignmentID
		tuples = append(tuples,
			ContextualTuple{
				User:     "principal:" + principalID,
				Relation: "subject",
				Object:   assignment,
			},
			ContextualTuple{
				User:     assignment + "#holder",
				Relation: relation,
				Object:   objectType + ":" + scopeID,
			},
		)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("authz: role step: %w", err)
	}
	return tuples, nil
}
