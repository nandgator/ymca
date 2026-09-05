// Package membership owns entitlement bundles, plans, and admission — the
// chain that decides what a member may actually do (05.3, ADR-107).
//
// Transport-neutral by intent (REVIEW.md B7). Every function here takes a
// transaction and returns a value; nothing knows whether an HTTP handler or
// a CLI called it.
//
// The FGA side of each write goes through the outbox rather than a direct
// tuple write, because these are grants and 8.3 says grants may lag while
// revocations may not. The fence key names the fact so that a later
// revocation can void a pending row for it (ADR-101).
package membership

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/outbox"
	"github.com/nandgator/ymca/backend/internal/page"
)

// Event types this package publishes. Each has a renderer registered with
// the dispatcher; an event with no renderer fails its row loudly rather than
// being skipped, so adding one here without the other is caught immediately.
const (
	EventBundleCreated  = "EntitlementBundleCreated"
	EventBundleEntitles = "EntitlementBundleEntitles"
	EventPlanCreated    = "MembershipPlanCreated"
	EventPlanGrants     = "MembershipPlanGrantsBundle"
)

// Bundle is an entitlement bundle (A2.4).
type Bundle struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CreateBundle records a bundle and queues its tenant edge.
//
// The tenant tuple is not decoration: A1.2 gives entitlement_bundle a tenant
// relation, and ADR-018 rejects any check whose object has no resolvable
// tenant ancestor. A bundle without it is unreachable by construction.
func CreateBundle(ctx context.Context, tx pgx.Tx, tenantID, name string) (Bundle, error) {
	var b Bundle
	err := tx.QueryRow(ctx, `
		INSERT INTO entitlement_bundle (id, tenant_id, name)
		VALUES (gen_random_uuid(), $1, $2)
		RETURNING id::text, name
	`, tenantID, name).Scan(&b.ID, &b.Name)
	if err != nil {
		return Bundle{}, fmt.Errorf("membership: create bundle: %w", err)
	}

	if err := outbox.Enqueue(ctx, tx, outbox.Fact{
		AggregateType: "entitlement_bundle",
		AggregateID:   b.ID,
		EventType:     EventBundleCreated,
		Payload:       map[string]string{"tenant_id": tenantID, "bundle_id": b.ID},
		Fence: outbox.Fence{
			Subject:  "tenant:" + tenantID,
			Relation: "tenant",
			Object:   "entitlement_bundle:" + b.ID,
		},
	}); err != nil {
		return Bundle{}, err
	}
	return b, nil
}

// ListBundles pages bundles under the tenant scope. ADR-104: the scope is
// checked once by the caller, and these rows come back by keyset SQL under
// RLS — there is no per-row check and no ListObjects.
func ListBundles(ctx context.Context, tx pgx.Tx, tenantID string, p page.Params) ([]Bundle, error) {
	// The sort key is (name, id): name for a stable human order, id to break
	// ties, since name is not unique. A keyset pager over a non-unique sort
	// key alone silently skips rows that share a value.
	var afterName, afterID string
	if len(p.Key) == 2 {
		afterName, afterID = p.Key[0], p.Key[1]
	}

	rows, err := tx.Query(ctx, `
		SELECT id::text, name
		  FROM entitlement_bundle
		 WHERE tenant_id = $1
		   AND ($2 = '' OR (name, id::text) > ($2, $3))
		 ORDER BY name, id
		 LIMIT $4
	`, tenantID, afterName, afterID, p.Fetch())
	if err != nil {
		return nil, fmt.Errorf("membership: list bundles: %w", err)
	}
	defer rows.Close()

	var out []Bundle
	for rows.Next() {
		var b Bundle
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			return nil, fmt.Errorf("membership: scan bundle: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// BundleKey is the sort key ListBundles orders by, and the one its cursor
// must encode. They are defined together so a change to one is a change to
// the other — a cursor built from a different key than the query sorts by
// makes pages overlap or skip, silently.
func BundleKey(b Bundle) []string { return []string{b.Name, b.ID} }

// EntitledObject is what a bundle entitles its beneficiaries to reach.
// A1.2 declares `entitled` on resource, programme and consumption_type;
// nothing else can carry the relation, so nothing else is accepted.
var entitledObjectTypes = map[string]bool{
	"resource":         true,
	"programme":        true,
	"consumption_type": true,
}

// Entitle records that a bundle entitles an object.
//
// The tuple is `entitlement_bundle:<id>#beneficiary entitled <object>` — a
// userset, not the bundle itself. A1.2's `entitled` names
// [entitlement_bundle#beneficiary], so writing the bare bundle would be a
// type violation, and the difference is the whole point: what reaches the
// resource is every PERSON the bundle benefits, not the bundle.
func Entitle(ctx context.Context, tx pgx.Tx, tenantID, bundleID, objectType, objectID string) error {
	if !entitledObjectTypes[objectType] {
		return fmt.Errorf("%w: %q; A1.2 declares `entitled` only on "+
			"resource, programme and consumption_type", ErrInvalidEntitledType, objectType)
	}

	// Both rows must belong to the tenant. RLS already guarantees it for the
	// reads below; this exists so a caller naming another tenant's object id
	// gets a clean refusal rather than a foreign key error.
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM entitlement_bundle WHERE id = $1::uuid AND tenant_id = $2::uuid)
	`, bundleID, tenantID).Scan(&exists); err != nil {
		return fmt.Errorf("membership: check bundle: %w", err)
	}
	if !exists {
		return ErrNotFound
	}

	object := objectType + ":" + objectID
	return outbox.Enqueue(ctx, tx, outbox.Fact{
		AggregateType: "entitlement_bundle",
		AggregateID:   bundleID,
		EventType:     EventBundleEntitles,
		Payload: map[string]string{
			"bundle_id": bundleID, "object_type": objectType, "object_id": objectID,
		},
		Fence: outbox.Fence{
			Subject:  "entitlement_bundle:" + bundleID + "#beneficiary",
			Relation: "entitled",
			Object:   object,
		},
	})
}

// Plan is a membership plan (A2.4, 05.3.2).
type Plan struct {
	ID          string  `json:"id"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Acquisition string  `json:"acquisition"`
	Duration    string  `json:"duration"`
	BundleID    *string `json:"entitlement_bundle_id"`
	Status      string  `json:"status"`
}

// CreatePlan records a plan and, when it grants a bundle, queues the
// `via_plan` edge (ADR-107).
//
// The direction is the load-bearing part. OpenFGA traverses forward from the
// object, so the BUNDLE names the plan that confers it. A plan naming the
// bundles it grants reads naturally and resolves to nothing — which is
// exactly what the original model did, and it shipped through a full review.
func CreatePlan(ctx context.Context, tx pgx.Tx, tenantID string, p Plan) (Plan, error) {
	var out Plan
	err := tx.QueryRow(ctx, `
		INSERT INTO membership_plan
		    (id, tenant_id, code, name, acquisition, duration,
		     entitlement_bundle_id, status)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6::uuid, 'OPEN')
		RETURNING id::text, code, name, acquisition, duration,
		          entitlement_bundle_id::text, status
	`, tenantID, p.Code, p.Name, p.Acquisition, p.Duration, p.BundleID).
		Scan(&out.ID, &out.Code, &out.Name, &out.Acquisition, &out.Duration,
			&out.BundleID, &out.Status)
	if err != nil {
		return Plan{}, fmt.Errorf("membership: create plan: %w", err)
	}

	if err := outbox.Enqueue(ctx, tx, outbox.Fact{
		AggregateType: "membership_plan",
		AggregateID:   out.ID,
		EventType:     EventPlanCreated,
		Payload:       map[string]string{"tenant_id": tenantID, "plan_id": out.ID},
		Fence: outbox.Fence{
			Subject:  "tenant:" + tenantID,
			Relation: "tenant",
			Object:   "membership_plan:" + out.ID,
		},
	}); err != nil {
		return Plan{}, err
	}

	if out.BundleID != nil {
		if err := outbox.Enqueue(ctx, tx, outbox.Fact{
			AggregateType: "membership_plan",
			AggregateID:   out.ID,
			EventType:     EventPlanGrants,
			Payload:       map[string]string{"plan_id": out.ID, "bundle_id": *out.BundleID},
			Fence: outbox.Fence{
				Subject:  "membership_plan:" + out.ID,
				Relation: "via_plan",
				Object:   "entitlement_bundle:" + *out.BundleID,
			},
		}); err != nil {
			return Plan{}, err
		}
	}
	return out, nil
}

// ListPlans pages plans under the tenant scope, ordered by the code a tenant
// gave them — which, unlike name, A2.4 makes unique per tenant, so the
// cursor needs no tiebreak.
func ListPlans(ctx context.Context, tx pgx.Tx, tenantID string, p page.Params) ([]Plan, error) {
	var afterCode string
	if len(p.Key) == 1 {
		afterCode = p.Key[0]
	}

	rows, err := tx.Query(ctx, `
		SELECT id::text, code, name, acquisition, duration,
		       entitlement_bundle_id::text, status
		  FROM membership_plan
		 WHERE tenant_id = $1
		   AND ($2 = '' OR code > $2)
		 ORDER BY code
		 LIMIT $3
	`, tenantID, afterCode, p.Fetch())
	if err != nil {
		return nil, fmt.Errorf("membership: list plans: %w", err)
	}
	defer rows.Close()

	var out []Plan
	for rows.Next() {
		var pl Plan
		if err := rows.Scan(&pl.ID, &pl.Code, &pl.Name, &pl.Acquisition,
			&pl.Duration, &pl.BundleID, &pl.Status); err != nil {
			return nil, fmt.Errorf("membership: scan plan: %w", err)
		}
		out = append(out, pl)
	}
	return out, rows.Err()
}

// PlanKey is the sort key ListPlans orders by.
func PlanKey(p Plan) []string { return []string{p.Code} }
