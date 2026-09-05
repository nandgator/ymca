// platform.go is 6.1 for the platform plane (ADR-005, ADR-111).
//
// It is a THREE-step check where the tenant plane has four, and the missing
// step is missing for a reason rather than by omission — see checkPlatform's
// step 2. That is the same discipline validity.go follows for its unbuilt
// limbs: a step that cannot exist should be visible in the code as absent,
// not silently skipped.
package authz

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/audit"
	"github.com/nandgator/ymca/backend/internal/auth"
	"github.com/nandgator/ymca/backend/internal/db"
)

// PlatformObjectID is the platform singleton's OpenFGA id, giving the object
// "platform:main". It matches the fixture in A1.6/A1.7 and
// backend/fga/assertions.yaml; there is exactly one platform object and
// nothing may invent a second.
const PlatformObjectID = "main"

// PlatformRequest is one platform-plane check. It has no Object: the object
// is always the platform singleton, which is what makes this plane a plane
// rather than a tenant with a special name.
type PlatformRequest struct {
	Principal auth.Principal
	// Relation is the relation checked on platform:main — "may_provision_tenant",
	// "may_suspend_tenant", "may_read_platform_audit".
	Relation string
	// Action and RequestID are audit context for a DENY (8.5, D9).
	Action    string
	RequestID string
}

// Permission is 8.1's permission name for this request.
func (r PlatformRequest) Permission() string { return "platform." + r.Relation }

// CheckPlatform runs the platform plane's authorization check and records
// every DENY in platform_audit_event (ADR-112).
func CheckPlatform(ctx context.Context, database *db.DB, fga *FGA, req PlatformRequest) (bool, error) {
	var allowed bool

	// db.InTx, not InTenantTx: there is no tenant to name. Everything read
	// below lives in a table with no tenant_id and no policy.
	err := database.InTx(ctx, func(tx pgx.Tx) error {
		// Step 1 — the PLANE, standing where tenant ancestry stands on the
		// other plane. httpx.PlatformOnly has already refused a tenant
		// credential at the edge; this repeats the question because a check
		// that trusts its caller to have been routed correctly is one
		// mis-wired route away from authorizing the wrong plane, and
		// ADR-018's lesson is that the containment question is asked by the
		// checker, not by whoever called it.
		if req.Principal.Plane != auth.PlanePlatform {
			return writePlatformDeny(ctx, tx, req, "not_a_platform_principal")
		}

		// Step 2 — THE ROLE STEP DOES NOT EXIST HERE, and its absence is a
		// fact about the model rather than work left undone.
		//
		// ADR-109 resolves role assignments in PostgreSQL and hands them to
		// the graph as contextual tuples. Every role assignment is scoped to
		// a tenant by construction: role_definition.tenant_id is NOT NULL
		// (A2.7), and role_assignment reaches its tenant only through that
		// definition. There is therefore no such thing as a platform-plane
		// role assignment to resolve, and no query here would have anything
		// to return.
		//
		// If the platform ever grows roles, this is where they go, and
		// ADR-110's grantable set will need a platform half. Until then,
		// passing no contextual tuples is the correct input, not a stub.

		// Step 3 — the graph, against the pinned model id.
		ok, err := fga.Check(ctx,
			"principal:"+req.Principal.ID, req.Relation,
			"platform:"+PlatformObjectID, nil)
		if err != nil {
			return fmt.Errorf("authz: platform graph step: %w", err)
		}
		if !ok {
			return writePlatformDeny(ctx, tx, req, "graph")
		}

		// Step 4 — validity, in its platform form (validity.go).
		result, err := checkValidityPlatform(ctx, tx, req.Principal.ID, req.Permission())
		if err != nil {
			return fmt.Errorf("authz: platform validity step: %w", err)
		}
		if !result.Valid {
			return writePlatformDeny(ctx, tx, req, result.Reason)
		}

		allowed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return allowed, nil
}

func writePlatformDeny(ctx context.Context, tx pgx.Tx, req PlatformRequest, reason string) error {
	return audit.WritePlatformDeny(ctx, tx, audit.DenyEvent{
		ActorPrincipalID: req.Principal.ID,
		Action:           req.Action,
		ObjectType:       "platform",
		ObjectID:         "",
		Severity:         severityDeny,
		Reason:           reason,
		RequestID:        req.RequestID,
	})
}
