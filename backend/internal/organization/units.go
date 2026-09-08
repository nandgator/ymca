// units.go creates and reads organizational units, and the authorization
// edges that are NOT derived from them (05.1.3 invariant 4, ADR-016).
//
// The distinction this file exists to keep is that a unit's organizational
// parent and its authorization parent are different things that happen to
// agree most of the time. `org_parent_id` says who administers the unit as a
// matter of org chart; the `authorization_edge` row says where authority
// propagates from. Creating a unit with an org parent must never imply the
// edge — 05.1.3 calls that the invariant implementers will most want to
// relax, and relaxing it collapses the whole context back into a tree.
//
// So the endpoint asks for the authorization parent explicitly and writes it
// explicitly, in the same transaction as the unit row. That is the workflow
// "offering" the matching edge, which 05.1.3 permits; it is not the model
// deriving it, which 05.1.3 forbids.
package organization

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nandgator/ymca/backend/internal/outbox"
)

// Event types this package publishes for a unit. Two, not one, because they
// are two facts: that the unit belongs to a tenant (ADR-018 — no permission
// resolves without a tenant in the path) and that authority reaches it from
// a particular parent. They are fenced separately for the same reason —
// revoking an edge must not void the tenant edge that makes the unit
// resolvable at all.
const (
	EventUnitCreated    = "OrganizationalUnitCreated"
	EventUnitAuthParent = "AuthorizationEdgeCreated"
)

// unitTypes is 05.1.2's closed list. One typed concept, not six (ADR-094):
// the type is a label on a uniform thing, so it is validated here rather
// than becoming six tables or six code paths.
var unitTypes = map[string]bool{
	"CHAPTER": true, "DEPARTMENT": true, "CENTRE": true,
	"REGION": true, "INSTITUTE": true, "PROJECT": true,
}

// parentTypes is A1.2's type restriction on organizational_unit.auth_parent,
// [organizational_unit, tenant], spelled again in Go so that a bad parent is
// refused with a domain error rather than by the trigger — which would be
// correct but would report it as a foreign endpoint.
var parentTypes = map[string]bool{
	"organizational_unit": true, "tenant": true,
}

// Parent is one authorization parent of a unit: the child end of an
// authorization_edge, named by type because A1.2 permits two.
type Parent struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Object renders the parent as an OpenFGA object.
func (p Parent) Object() string { return p.Type + ":" + p.ID }

// Unit is an organizational unit (A2.2, 05.1.2).
//
// AuthParents is a list even though nothing in this slice creates a second
// one. ADR-016 permits many and the read side must not quietly assume one —
// a caller shown a single parent would draw a tree and be wrong about the
// system as designed rather than about the data it happens to hold.
type Unit struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	OrgParentID *string  `json:"org_parent_id"`
	Status      string   `json:"status"`
	AuthParents []Parent `json:"auth_parents"`
}

// NewUnit is what a caller must supply to create one.
type NewUnit struct {
	Type string
	Name string
	// OrgParentID is the ORGANIZATIONAL parent and carries no authorization
	// meaning (05.1.3). It is not defaulted from Parent and must not be.
	OrgParentID *string
	// Parent is the authorization parent, and it is required. A unit with
	// no edge is unreachable: every permission on it resolves through
	// `admin from auth_parent` or `member from auth_parent`, so a unit
	// created without one would return 201 and be administrable by nobody —
	// the same silent shape ADR-113 describes for a tenant with no owner.
	Parent Parent
	// CreatedBy is the acting principal, for authorization_edge.created_by.
	CreatedBy string
}

// CreateUnit writes the unit and its first authorization edge in one
// transaction, and queues both authorization facts.
//
// The cycle, depth and same-tenant invariants are NOT checked here. They are
// enforced by migration 0005's trigger (ADR-115) so that they hold for every
// writer of the table rather than for this one; what this function does is
// translate the trigger's refusal into a domain error. Re-deriving the walk
// here would give two implementations of one invariant and a way for them to
// disagree.
func CreateUnit(ctx context.Context, tx pgx.Tx, tenantID string, in NewUnit) (Unit, error) {
	if !unitTypes[in.Type] {
		return Unit{}, fmt.Errorf("%w: %q; 05.1.2 gives CHAPTER, DEPARTMENT, "+
			"CENTRE, REGION, INSTITUTE, PROJECT", ErrInvalidUnitType, in.Type)
	}
	if !parentTypes[in.Parent.Type] {
		return Unit{}, fmt.Errorf("%w: %q; A1.2 restricts auth_parent to "+
			"organizational_unit and tenant", ErrInvalidParentType, in.Parent.Type)
	}

	u := Unit{AuthParents: []Parent{in.Parent}}
	err := tx.QueryRow(ctx, `
		INSERT INTO organizational_unit (id, tenant_id, type, name, org_parent_id, status)
		VALUES (gen_random_uuid(), $1, $2, $3, $4::uuid, 'ACTIVE')
		RETURNING id::text, type, name, org_parent_id::text, status
	`, tenantID, in.Type, in.Name, in.OrgParentID).
		Scan(&u.ID, &u.Type, &u.Name, &u.OrgParentID, &u.Status)
	if err != nil {
		// A bad org_parent_id is a foreign key violation on a value the
		// caller supplied, not a server fault. RLS means another tenant's
		// unit is not visible to the constraint either.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return Unit{}, ErrNotFound
		}
		return Unit{}, fmt.Errorf("organization: create unit: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO authorization_edge
		    (id, tenant_id, child_type, child_id, parent_type, parent_id, created_by)
		VALUES (gen_random_uuid(), $1, 'organizational_unit', $2::uuid, $3, $4::uuid, $5::uuid)
	`, tenantID, u.ID, in.Parent.Type, in.Parent.ID, in.CreatedBy); err != nil {
		return Unit{}, edgeError(err)
	}

	// The tenant edge first. ADR-018 makes it the precondition for the unit
	// resolving at all, and the order these are enqueued in is the order the
	// dispatcher applies them.
	if err := outbox.Enqueue(ctx, tx, outbox.Fact{
		AggregateType: "organizational_unit",
		AggregateID:   u.ID,
		EventType:     EventUnitCreated,
		Payload:       map[string]string{"tenant_id": tenantID, "unit_id": u.ID},
		Fence: outbox.Fence{
			Subject:  "tenant:" + tenantID,
			Relation: "tenant",
			Object:   "organizational_unit:" + u.ID,
		},
	}); err != nil {
		return Unit{}, err
	}

	if err := outbox.Enqueue(ctx, tx, outbox.Fact{
		AggregateType: "organizational_unit",
		AggregateID:   u.ID,
		EventType:     EventUnitAuthParent,
		Payload: map[string]string{
			"unit_id":     u.ID,
			"parent_type": in.Parent.Type,
			"parent_id":   in.Parent.ID,
		},
		Fence: outbox.Fence{
			Subject:  in.Parent.Object(),
			Relation: "auth_parent",
			Object:   "organizational_unit:" + u.ID,
		},
	}); err != nil {
		return Unit{}, err
	}

	return u, nil
}

// edgeError turns migration 0005's refusal into this package's vocabulary,
// by constraint name rather than by message text. The message is written for
// a log and may be reworded; the constraint name is the contract, and the
// integration test fails if a rename ever breaks it.
func edgeError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != checkViolation {
		return fmt.Errorf("organization: create authorization edge: %w", err)
	}
	switch pgErr.ConstraintName {
	case "authorization_edge_acyclic":
		return ErrEdgeCycle
	case "authorization_edge_max_depth":
		return ErrEdgeTooDeep
	case "authorization_edge_same_tenant", "authorization_edge_supported_types":
		return ErrEdgeForeignEndpoint
	default:
		// A new constraint on this table that nothing here knows about. A
		// 500 is the honest answer: silently folding it into one of the
		// three above would report the wrong invariant.
		return fmt.Errorf("organization: create authorization edge: %w", err)
	}
}

// GetUnit reads one unit and every authorization parent it has.
//
// RLS is what scopes this, not the tenant_id predicate: the predicate is
// there so a reader can see the scope without knowing the policy exists.
// A unit in another tenant is simply not present, which is A3.3's 404 and is
// deliberately indistinguishable from one the caller may not know about.
func GetUnit(ctx context.Context, tx pgx.Tx, tenantID, unitID string) (Unit, error) {
	var u Unit
	err := tx.QueryRow(ctx, `
		SELECT id::text, type, name, org_parent_id::text, status
		  FROM organizational_unit
		 WHERE tenant_id = $1 AND id = $2::uuid
	`, tenantID, unitID).Scan(&u.ID, &u.Type, &u.Name, &u.OrgParentID, &u.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Unit{}, ErrNotFound
	}
	if err != nil {
		return Unit{}, fmt.Errorf("organization: get unit: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT parent_type, parent_id::text
		  FROM authorization_edge
		 WHERE tenant_id = $1 AND child_type = 'organizational_unit' AND child_id = $2::uuid
		 ORDER BY created_at, id
	`, tenantID, unitID)
	if err != nil {
		return Unit{}, fmt.Errorf("organization: get unit parents: %w", err)
	}
	defer rows.Close()

	u.AuthParents = []Parent{}
	for rows.Next() {
		var p Parent
		if err := rows.Scan(&p.Type, &p.ID); err != nil {
			return Unit{}, fmt.Errorf("organization: scan unit parent: %w", err)
		}
		u.AuthParents = append(u.AuthParents, p)
	}
	return u, rows.Err()
}
