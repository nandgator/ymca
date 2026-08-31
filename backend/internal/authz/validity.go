package authz

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// validityResult is step 3 of 6.1: is the relationship the graph found
// currently effective. Valid is false exactly when some limb below denies;
// Reason names which one, for the audit context.
type validityResult struct {
	Valid  bool
	Reason string
}

// checkValidity runs every limb of step 3 in order and stops at the first
// denial. D5 requires each limb to be a named, separate thing rather than
// one combined query, specifically so the limbs that are not built yet are
// visible in the code as unbuilt, not silently forgotten (11.2).
func checkValidity(ctx context.Context, tx pgx.Tx, principalID string) (validityResult, error) {
	limbs := []func(context.Context, pgx.Tx, string) (validityResult, error){
		checkPrincipalAndPersonStatus,
		checkTermWindow,
		checkClearance,
		checkRestriction,
	}
	for _, limb := range limbs {
		result, err := limb(ctx, tx, principalID)
		if err != nil {
			return validityResult{}, err
		}
		if !result.Valid {
			return result, nil
		}
	}
	return validityResult{Valid: true}, nil
}

// checkPrincipalAndPersonStatus is the one limb of step 3 that is built
// today: principal.status = 'ACTIVE' and the owning person.status =
// 'ACTIVE' (D5). Both tables are exempt from row-level security (8.2,
// global by design), so this reads correctly regardless of app.tenant_id —
// but it still runs inside the caller's tenant transaction, so a DENY can
// be audited atomically with the read that produced it.
func checkPrincipalAndPersonStatus(ctx context.Context, tx pgx.Tx, principalID string) (validityResult, error) {
	var principalStatus, personStatus string
	err := tx.QueryRow(ctx, `
		SELECT p.status, pe.status
		FROM principal p
		JOIN person pe ON pe.id = p.person_id
		WHERE p.id = $1
	`, principalID).Scan(&principalStatus, &personStatus)
	if err != nil {
		return validityResult{}, fmt.Errorf("load principal/person status: %w", err)
	}
	if principalStatus != "ACTIVE" {
		return validityResult{Reason: "principal_not_active"}, nil
	}
	if personStatus != "ACTIVE" {
		return validityResult{Reason: "person_not_active"}, nil
	}
	return validityResult{Valid: true}, nil
}

// checkTermWindow is 6.1's term-window limb of step 3: is the relationship
// the graph found still within its granted term.
//
// NOT IMPLEMENTED (D5). A2 defines no role_definition or role_assignment
// table, though the FGA model declares both types (fga/model.fga) — there
// is nothing in PostgreSQL to query yet. This always reports valid, so the
// gap is a named, visible no-op rather than a limb that was silently never
// written. Recorded as a gap in 11.2.
func checkTermWindow(context.Context, pgx.Tx, string) (validityResult, error) {
	return validityResult{Valid: true}, nil
}

// checkClearance is 6.1's clearance-validity limb of step 3.
//
// NOT IMPLEMENTED (D5). Clearance is reachable only through a role
// assignment, which does not exist yet either — see checkTermWindow, same
// gap, same reason. Recorded as a gap in 11.2.
func checkClearance(context.Context, pgx.Tx, string) (validityResult, error) {
	return validityResult{Valid: true}, nil
}

// checkRestriction is 6.1's restriction limb of step 3.
//
// NOT IMPLEMENTED (D5). The restriction table has a kind but no mapping
// from a kind to the permissions it withholds. Denying every check for a
// principal with any in-force restriction would be fail-closed but wrong:
// 8.8 requires denial to be specific, and this cannot yet say what it is
// denying. Recorded as a gap in 11.2.
func checkRestriction(context.Context, pgx.Tx, string) (validityResult, error) {
	return validityResult{Valid: true}, nil
}
