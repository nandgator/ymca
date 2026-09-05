// Package outbox projects authorization facts into OpenFGA, fenced against
// the synchronous revocations that would otherwise race them (ADR-101, 8.3).
//
// The asymmetry it serves: a GRANT may lag by a dispatch interval, because a
// new member simply cannot use a facility for a few hundred milliseconds. A
// REVOCATION may not lag at all, and is written synchronously before its
// transaction commits. Those two rules alone are not enough — idempotency is
// not ordering — and the gap between them is the failure 6.4 said could not
// happen:
//
//	grant commits an outbox row
//	dispatcher stalls
//	revocation deletes a tuple that does not exist yet, a no-op
//	revocation commits, reported successful
//	dispatcher recovers and applies the stale row
//	the tuple exists again, and no synchronous path runs a second time
//
// ADR-101 closes it with a fence key naming the fact, and two locks. Both are
// required and they close different races — the first draft of this fix had
// only the row lock and was wrong.
//
// # What is NOT here
//
// Role assignments. ADR-109 resolves those per check and never stores a
// tuple for them, so they never enter this table and cannot be resurrected
// by it. What remains fenced is everything the graph really holds:
// entitlement, membership coverage, affiliation authority.
//
// This table projects authorization facts only. It is not the inter-context
// event bus of 05.0.8, which does not exist (REVIEW.md B6).
package outbox

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Fence names a fact at domain level: the subject, the relation and the
// object as the domain understands them, never as a particular version of
// the FGA model spells them.
//
// That indirection is the point of ADR-101's "the producer names the fact,
// the dispatcher renders the tuple". A payload holding a rendered tuple
// would freeze a model version into stored rows, and a model change would
// then need a migration over undispatched rows.
type Fence struct {
	Subject  string
	Relation string
	Object   string
}

// Key is the fence key the advisory lock is taken on.
func (f Fence) Key() string { return f.Subject + "\x1f" + f.Relation + "\x1f" + f.Object }

// Zero reports whether the fence is unset. The table's fence_all_or_none
// constraint permits either all three columns or none: a fact that nothing
// can revoke needs no fence.
func (f Fence) Zero() bool { return f.Subject == "" && f.Relation == "" && f.Object == "" }

// Fact is one authorization fact awaiting projection.
type Fact struct {
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       any
	Fence         Fence
}

// lockFence takes ADR-101 obligation 2's advisory lock, held to the end of
// the caller's transaction.
//
// hashtext collides: two unrelated fence keys can share a lock. That costs
// serialization between facts that never needed it and costs nothing in
// correctness, which is the right side to err on — the alternative is a lock
// table with rows to clean up.
func lockFence(ctx context.Context, tx pgx.Tx, fence Fence) error {
	if fence.Zero() {
		return nil
	}
	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, fence.Key()); err != nil {
		return fmt.Errorf("outbox: lock fence: %w", err)
	}
	return nil
}

// Enqueue records a fact for projection, inside the caller's transaction and
// under the fence lock (ADR-101 obligations 1 and 2).
//
// It must be called in the same transaction as the business mutation it
// projects. A row committed without its mutation grants authority for
// something that did not happen; a mutation committed without its row grants
// nothing and reports success, which ADR-107 warns is silent — the member
// simply has no entitlements and no error says so.
func Enqueue(ctx context.Context, tx pgx.Tx, fact Fact) error {
	if err := lockFence(ctx, tx, fact.Fence); err != nil {
		return err
	}

	payload, err := json.Marshal(fact.Payload)
	if err != nil {
		return fmt.Errorf("outbox: marshal payload: %w", err)
	}

	var subject, relation, object *string
	if !fact.Fence.Zero() {
		subject, relation, object = &fact.Fence.Subject, &fact.Fence.Relation, &fact.Fence.Object
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO authorization_outbox
		    (aggregate_type, aggregate_id, event_type, payload,
		     fence_subject, fence_relation, fence_object)
		VALUES ($1, $2::uuid, $3, $4, $5, $6, $7)
	`, fact.AggregateType, fact.AggregateID, fact.EventType, payload,
		subject, relation, object); err != nil {
		return fmt.Errorf("outbox: enqueue %s: %w", fact.EventType, err)
	}
	return nil
}

// Void is the revocation half of the fence (ADR-101, 8.3's revocation path).
//
// Called inside the revoking transaction, BEFORE the synchronous tuple
// delete, it takes the same advisory lock Enqueue takes and marks every
// pending row for the fact as voided. A grant that began before this
// revocation and commits after it is blocked on that lock until this
// transaction ends, so it cannot slip a row past the void — which is the
// race the row lock alone does not close.
//
// Voided rows are retained rather than deleted. A row that had to be fenced
// is evidence a race reached production, and belongs in the record.
func Void(ctx context.Context, tx pgx.Tx, fence Fence) (int64, error) {
	if fence.Zero() {
		return 0, fmt.Errorf("outbox: cannot void an unset fence")
	}
	if err := lockFence(ctx, tx, fence); err != nil {
		return 0, err
	}

	tag, err := tx.Exec(ctx, `
		UPDATE authorization_outbox
		   SET voided_at = now()
		 WHERE fence_subject = $1 AND fence_relation = $2 AND fence_object = $3
		   AND dispatched_at IS NULL
		   AND voided_at IS NULL
	`, fence.Subject, fence.Relation, fence.Object)
	if err != nil {
		return 0, fmt.Errorf("outbox: void pending rows: %w", err)
	}
	return tag.RowsAffected(), nil
}
