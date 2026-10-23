package membership

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/outbox"
)

// reconcile.go compares what PostgreSQL says about memberships with what
// OpenFGA holds, and optionally repairs it (11.2's C1).
//
// The two stores are written in two steps, and ADR-118 accepts the consequence:
// a commit that fails after a synchronous delete leaves a member UNDER-covered
// until someone retries, and a dispatch that never ran leaves a grant missing.
// The dangerous direction is the other one — coverage in the graph for a
// membership that is no longer ACTIVE, or for a plan it has left — and nothing
// else looks for it.
//
//	MISSING     an ACTIVE membership lacks a tuple its row implies, and no
//	            pending outbox row is about to write it. Repair: enqueue it.
//	UNEXPECTED  the graph covers a membership that is not ACTIVE, or covers it
//	            on a plan other than its own. Repair: revoke it, on the path
//	            8.3 gives revocation (lock, void, delete, roll back on failure).
//
// A grant still waiting in the outbox is not drift: it is the lag ADR-101
// accepts, so a row pending for the fact is reported as nothing.

// Inspector is the read half of the authorization store.
type Inspector interface {
	Has(ctx context.Context, t outbox.Tuple) (bool, error)
	ObjectsOf(ctx context.Context, user, relation, objectType string) ([]string, error)
}

// Drift is one disagreement between the stores.
type Drift struct {
	MembershipID string       `json:"membership_id"`
	Kind         string       `json:"kind"` // MISSING | UNEXPECTED
	Tuple        outbox.Tuple `json:"tuple"`
	Repaired     bool         `json:"repaired"`
}

type expectedFact struct {
	event   string
	payload map[string]string
	fence   outbox.Fence
}

// expectedFacts is what admission wrote for a membership and what a plan move
// or reinstatement keeps true: the tenant edge, the holder, and the coverage of
// the membership's current plan. It reads the same fences those paths do, so a
// membership admitted and dispatched reconciles to nothing.
func expectedFacts(tenantID, membershipID, personID, planID string) []expectedFact {
	return []expectedFact{
		{EventMembershipAdmitted, map[string]string{"tenant_id": tenantID, "membership_id": membershipID},
			outbox.Fence{Subject: "tenant:" + tenantID, Relation: "tenant", Object: "membership:" + membershipID}},
		{EventMembershipHeldBy, map[string]string{"membership_id": membershipID, "person_id": personID},
			outbox.Fence{Subject: "person:" + personID, Relation: "holder", Object: "membership:" + membershipID}},
		{EventMembershipCovered, map[string]string{"membership_id": membershipID, "plan_id": planID},
			coveredFence(membershipID, planID)},
	}
}

func pending(ctx context.Context, tx pgx.Tx, f outbox.Fence) (bool, error) {
	var n int
	err := tx.QueryRow(ctx, `
		SELECT count(*)::int FROM authorization_outbox
		 WHERE fence_subject = $1 AND fence_relation = $2 AND fence_object = $3
		   AND dispatched_at IS NULL AND voided_at IS NULL`,
		f.Subject, f.Relation, f.Object).Scan(&n)
	return n > 0, err
}

// ReconcileTenant checks every membership of one tenant, inside the caller's
// tenant transaction. With repair false it only reports.
func ReconcileTenant(
	ctx context.Context, tx pgx.Tx, tenantID string, store Inspector, rev Revoker, repair bool,
) ([]Drift, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, person_id::text, plan_id::text, state
		  FROM membership WHERE tenant_id = $1 ORDER BY id`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("membership: read memberships to reconcile: %w", err)
	}
	type row struct{ id, person, plan, state string }
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.person, &r.plan, &r.state); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []Drift
	for _, m := range all {
		// UNEXPECTED: every plan this membership covers in the graph, against
		// the one it is entitled to.
		covers, err := store.ObjectsOf(ctx, "membership:"+m.id+"#covered", "covered_member", "membership_plan")
		if err != nil {
			return nil, err
		}
		for _, object := range covers {
			if m.state == "ACTIVE" && object == "membership_plan:"+m.plan {
				continue
			}
			d := Drift{MembershipID: m.id, Kind: "UNEXPECTED", Tuple: outbox.Tuple{
				User: "membership:" + m.id + "#covered", Relation: "covered_member", Object: object}}
			if repair {
				fence := outbox.Fence{Subject: d.Tuple.User, Relation: d.Tuple.Relation, Object: d.Tuple.Object}
				if _, err := outbox.Void(ctx, tx, fence); err != nil {
					return nil, err
				}
				if err := rev.DeleteTuples(ctx, []outbox.Tuple{d.Tuple}); err != nil {
					return nil, fmt.Errorf("%w: %v", ErrRevocationFailed, err)
				}
				d.Repaired = true
			}
			out = append(out, d)
		}

		// MISSING: only an ACTIVE membership is owed its tuples.
		if m.state != "ACTIVE" {
			continue
		}
		for _, f := range expectedFacts(tenantID, m.id, m.person, m.plan) {
			t := outbox.Tuple{User: f.fence.Subject, Relation: f.fence.Relation, Object: f.fence.Object}
			has, err := store.Has(ctx, t)
			if err != nil {
				return nil, err
			}
			if has {
				continue
			}
			inFlight, err := pending(ctx, tx, f.fence)
			if err != nil {
				return nil, err
			}
			if inFlight {
				continue
			}
			d := Drift{MembershipID: m.id, Kind: "MISSING", Tuple: t}
			if repair {
				if err := outbox.Enqueue(ctx, tx, outbox.Fact{
					AggregateType: "membership", AggregateID: m.id,
					EventType: f.event, Payload: f.payload, Fence: f.fence,
				}); err != nil {
					return nil, err
				}
				d.Repaired = true
			}
			out = append(out, d)
		}
	}
	return out, nil
}
