// Package authz implements 6.1's four-step authorization check — tenant
// ancestry, the effective role assignments, the OpenFGA graph, and what
// remains of PostgreSQL validity (8.1) — and audits every DENY inside the
// tenant transaction that produced it (D8).
//
// The ordering is load-bearing (ADR-109). Steps 2 and 3 are not
// interchangeable: role assignments are resolved BEFORE the graph is asked,
// and supplied to it as contextual tuples, because Check returns a boolean
// rather than a path. Reversing them reintroduces the defect that made 8.2's
// term-window and clearance limbs unwritable — a validity step running after
// the graph cannot tell whether an ALLOW came through the lapsed role or the
// direct grant beside it.
package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/audit"
	"github.com/nandgator/ymca/backend/internal/auth"
	"github.com/nandgator/ymca/backend/internal/db"
)

// Object is the resource a permission is checked against.
type Object struct {
	// Type and ID together form the OpenFGA object "<type>:<id>".
	Type string
	ID   string
	// TenantID is the tenant this object itself belongs to — step 1 of 6.1
	// compares it against RequestTenantID. In this slice's one caller
	// (GET /me) the object IS the tenant, so the two are equal by
	// construction. There is no generic "resolve an object's tenant
	// ancestor" resolver yet; a future endpoint checking some other object
	// type must supply the object's real tenant here itself.
	TenantID string
}

// Request is one instance of 6.1's check(principal, permission, resource,
// context).
type Request struct {
	Principal auth.Principal
	// RequestTenantID is the tenant named by the request path (A3.1) — the
	// tenant the caller says they are acting in.
	RequestTenantID string
	Object          Object
	// Relation is the OpenFGA relation checked on Object — e.g. "admin",
	// "may_use". 8.1 calls "<object type>.<relation>" the permission name,
	// and Permission() below is that name: step 2 matches role_permission
	// rows against it, so Object.Type and Relation together decide which
	// assignments can possibly apply.
	Relation string
	// AlsoAsPerson, when set, is a person id the graph is ALSO asked about,
	// as "person:<id>", and either subject suffices (A3.10).
	//
	// Entitlement is a property of a person — a membership covers a human
	// (ADR-107) — while `manager` and the role-grantable permissions are
	// properties of a principal. A relation like consumption_type.may_record
	// is reachable both ways, so a check naming one subject denies either
	// the member or the owner. The DENY is audited once, when both refuse.
	AlsoAsPerson string
	// Action and RequestID are audit context for a DENY (8.5, D9).
	Action    string
	RequestID string
}

// ErrTenantMismatch is step 1 of 6.1: the object named does not belong to
// the tenant the request names. D7 maps this to 403 tenant_mismatch,
// distinct from an ordinary graph or validity DENY (403 forbidden) —
// ADR-105's rationale is that refusing a tenant the caller named
// themselves discloses nothing, so it gets its own code rather than being
// folded into a generic denial.
var ErrTenantMismatch = errors.New("authz: object's tenant does not match the request tenant")

// severityDeny is 8.5's severity for an ordinary authorization DENY. None
// of 8.5's HIGH-severity categories (break-glass, restriction change, ...)
// apply to a plain permission check, so this step uses INFO uniformly. A
// future check that reaches one of those categories will need a way to say
// so, which this shape does not yet provide.
const severityDeny = "INFO"

// Permission is 8.1's permission name for this request,
// "<object type>.<relation>" — the form role_permission stores and
// restriction_kind_permission maps.
func (r Request) Permission() string { return r.Object.Type + "." + r.Relation }

// Check runs all four steps of 6.1 and audits every DENY inside the same
// tenant transaction that produced it (D8).
func Check(ctx context.Context, database *db.DB, fga *FGA, req Request) (bool, error) {
	// Step 1 — tenant ancestry. Rejected before any graph query (ADR-018),
	// not folded into steps 2/3's transaction because a mismatch here
	// means the request names a different tenant than the object it is
	// asking about — auditing it uses the request's own tenant, not the
	// object's.
	if req.Object.TenantID != req.RequestTenantID {
		if err := database.InTenantTx(ctx, req.RequestTenantID, func(tx pgx.Tx) error {
			return writeDeny(ctx, tx, req, "tenant_ancestor_mismatch")
		}); err != nil {
			return false, fmt.Errorf("authz: audit tenant mismatch: %w", err)
		}
		return false, ErrTenantMismatch
	}

	var allowed bool
	err := database.InTenantTx(ctx, req.RequestTenantID, func(tx pgx.Tx) error {
		user := "principal:" + req.Principal.ID
		object := req.Object.Type + ":" + req.Object.ID

		// Step 2 — the effective role assignments for this permission
		// (roles.go). Everything temporal is inside that query. An empty
		// result is not a denial: the principal may still reach the
		// permission directly, or through entitlement, and the graph is
		// what decides.
		contextual, err := roleTuples(ctx, tx, req.Principal.ID, req.Permission())
		if err != nil {
			return err
		}

		// Step 3 — the OpenFGA graph, against the pinned model id (fga.go),
		// given step 2's tuples and no others.
		users := []string{user}
		if req.AlsoAsPerson != "" {
			// The person first: the common caller of an entitlement check
			// is a member, and the principal-only path is the exception.
			users = []string{"person:" + req.AlsoAsPerson, user}
		}
		ok, allowedBy := false, ""
		for _, u := range users {
			var err error
			ok, err = fga.Check(ctx, u, req.Relation, object, contextual)
			if err != nil {
				return fmt.Errorf("authz: graph step: %w", err)
			}
			if ok {
				allowedBy = u
				break
			}
		}
		if !ok {
			return writeDeny(ctx, tx, req, "graph")
		}

		// Step 3b — an ALLOW that came through a PERSON on an entitlement
		// permission came through a membership, and a membership has a state
		// and a term the graph does not hold (ADR-118, A3.13). Only the person
		// route is checked: a principal's own authority — manager, a role —
		// does not depend on being a member, and a person's standing as an
		// invoice's payer is not an entitlement at all.
		if allowedBy == "person:"+req.AlsoAsPerson && entitlementPermissions[req.Permission()] {
			valid, err := hasValidMembership(ctx, tx, req.RequestTenantID, req.AlsoAsPerson)
			if err != nil {
				return fmt.Errorf("authz: membership term: %w", err)
			}
			if !valid {
				return writeDeny(ctx, tx, req, "membership_not_valid")
			}
		}

		// Step 4 — what remains (validity.go): principal and person status,
		// and any restriction withholding this specific permission.
		result, err := checkValidity(ctx, tx, req.Principal.ID, req.Permission())
		if err != nil {
			return fmt.Errorf("authz: validity step: %w", err)
		}
		if !result.Valid {
			return writeDeny(ctx, tx, req, result.Reason)
		}

		allowed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return allowed, nil
}

func writeDeny(ctx context.Context, tx pgx.Tx, req Request, reason string) error {
	return audit.WriteDeny(ctx, tx, req.RequestTenantID, audit.DenyEvent{
		ActorPrincipalID: req.Principal.ID,
		Action:           req.Action,
		ObjectType:       req.Object.Type,
		ObjectID:         req.Object.ID,
		Severity:         severityDeny,
		Reason:           reason,
		RequestID:        req.RequestID,
	})
}

// entitlementPermissions are the permissions a person holds by being covered
// by a membership (ADR-107), and so the only ones whose ALLOW depends on the
// membership still being in force.
var entitlementPermissions = map[string]bool{
	"consumption_type.may_record": true,
	"resource.may_use":            true,
	"resource.may_book":           true,
}

// hasValidMembership is the decision-time term check: an ACTIVE membership in
// this tenant whose term covers today, where the tenant is (ADR-119). In PostgreSQL, with no sweeper — 8.7
// forbids one noticing that a term ended, and A1.5 puts term windows here.
func hasValidMembership(ctx context.Context, tx pgx.Tx, tenantID, personID string) (bool, error) {
	var valid bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM membership
		     WHERE tenant_id = $1 AND person_id = $2::uuid AND state = 'ACTIVE'
		       AND (valid_from IS NULL OR valid_from <= tenant_today($1::uuid))
		       AND (valid_until IS NULL OR valid_until >= tenant_today($1::uuid)))
	`, tenantID, personID).Scan(&valid)
	return valid, err
}
