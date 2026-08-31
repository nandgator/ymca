// Package authz implements 6.1's three-step authorization check — tenant
// ancestry, the OpenFGA graph, and PostgreSQL validity (8.1) — and audits
// every DENY inside the tenant transaction that produced it (D8).
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
	// "may_use". 8.1 calls "<object type>.<relation>" the permission name.
	Relation string
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

// Check runs all three steps of 6.1 and audits every DENY inside the same
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

		// Step 2 — the OpenFGA graph, against the pinned model id (fga.go).
		ok, err := fga.Check(ctx, user, req.Relation, object)
		if err != nil {
			return fmt.Errorf("authz: graph step: %w", err)
		}
		if !ok {
			return writeDeny(ctx, tx, req, "graph")
		}

		// Step 3 — PostgreSQL validity (validity.go). Each limb is named
		// there; three of the four are documented gaps, not silent ones.
		result, err := checkValidity(ctx, tx, req.Principal.ID)
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
