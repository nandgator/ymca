// validity.go is step 4 of 6.1: everything still capable of withholding a
// permission once the graph has said yes.
//
// It is smaller than the step 3 it replaces. Term windows and clearance
// validity moved to step 2 (roles.go), where they belong: they qualify a
// role assignment, and applying them here — after a boolean Check that
// cannot say which path produced the ALLOW — was never able to give the
// right answer for a principal holding both a lapsed role and a direct
// grant. ADR-109 records why that ordering was the defect.
//
// What remains genuinely applies to the principal rather than to one path
// into the permission.
package authz

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// validityResult is step 4's verdict. Valid is false exactly when some limb
// below denies; Reason names which one, for the audit context.
type validityResult struct {
	Valid  bool
	Reason string
}

// checkValidity runs every limb of step 4 in order and stops at the first
// denial. D5's requirement that each limb be separately named still holds —
// it is what kept the unbuilt ones visible until they could be built.
func checkValidity(ctx context.Context, tx pgx.Tx, principalID, permission string) (validityResult, error) {
	if result, err := checkPrincipalAndPersonStatus(ctx, tx, principalID); err != nil || !result.Valid {
		return result, err
	}
	return checkRestriction(ctx, tx, principalID, permission)
}

// checkPrincipalAndPersonStatus is principal.status = 'ACTIVE' and the
// owning person.status = 'ACTIVE' (D5). Both tables are exempt from
// row-level security (8.2, global by design), so this reads correctly
// regardless of app.tenant_id — but it still runs inside the caller's tenant
// transaction, so a DENY can be audited atomically with the read that
// produced it.
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

// checkRestriction is 6.1's restriction limb, and it is now real.
//
// It asks whether any in-force restriction on this principal's person
// withholds THIS permission, via restriction_kind_permission (A2.8). That
// mapping is what 8.8 requires: a denial must say what it withholds, and
// before the table existed `restriction.kind` mapped to nothing, so the limb
// checked nothing at all.
//
// Two of 05.9.4's four kinds are absent from the mapping on purpose. They act
// on the role path and are applied in step 2 — a person barred from holding
// roles is not barred from using the pool their membership entitles them to,
// and conflating the two would deny far more than the restriction says.
//
// A restriction with a NULL tenant_id is platform-wide (05.9.9) and applies
// in every tenant; one naming a tenant applies only there.
func checkRestriction(ctx context.Context, tx pgx.Tx, principalID, permission string) (validityResult, error) {
	var restricted bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM restriction r
			JOIN principal p ON p.person_id = r.person_id
			JOIN restriction_kind_permission rkp ON rkp.kind = r.kind
			WHERE p.id = $1::uuid
			  AND rkp.permission = $2
			  AND r.lifted_at IS NULL
			  AND (r.expires_at IS NULL OR r.expires_at > now())
			  AND (r.tenant_id IS NULL
			       OR r.tenant_id = current_setting('app.tenant_id')::uuid)
		)
	`, principalID, permission).Scan(&restricted)
	if err != nil {
		return validityResult{}, fmt.Errorf("load restriction: %w", err)
	}
	if restricted {
		// 8.8: specific, and it names the permission rather than the
		// restriction — the existence and contents of a restriction are not
		// disclosed to the person it denies.
		return validityResult{Reason: "restricted:" + permission}, nil
	}
	return validityResult{Valid: true}, nil
}
