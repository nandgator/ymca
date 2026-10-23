package membership

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/outbox"
)

// plans.go is a plan's lifecycle (05.3.2, ADR-121). A plan is never edited once
// people hold it; it is closed to newcomers, replaced by a successor, and
// retired when nobody is left on it.
//
//	OPEN ──close──▶ CLOSED_TO_NEW ──retire──▶ RETIRED
//	  ▲                  │
//	  └───── reopen ─────┘   (only while it has no successor)
//
// Every transition locks the plan row, so two administrators racing read each
// other's result, and admission share-locks the same row, so a plan cannot be
// closed or retired past an admission still in flight.

func lockedPlan(ctx context.Context, tx pgx.Tx, tenantID, id string) (Plan, error) {
	var p Plan
	err := tx.QueryRow(ctx, `
		SELECT `+planColumns+` FROM membership_plan
		 WHERE id = $1::uuid AND tenant_id = $2 FOR UPDATE`, id, tenantID).
		Scan(&p.ID, &p.Code, &p.Name, &p.Acquisition, &p.Duration, &p.BundleID,
			&p.Status, &p.SupersededByID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, ErrNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("membership: lock plan: %w", err)
	}
	return p, nil
}

func setPlanStatus(ctx context.Context, tx pgx.Tx, p Plan, status string) (Plan, error) {
	if _, err := tx.Exec(ctx,
		`UPDATE membership_plan SET status = $2 WHERE id = $1::uuid`, p.ID, status); err != nil {
		return Plan{}, fmt.Errorf("membership: set plan %s: %w", status, err)
	}
	p.Status = status
	return p, nil
}

// ClosePlan stops a plan accepting new members. Existing holders keep it:
// "the common real state" (05.3.2).
func ClosePlan(ctx context.Context, tx pgx.Tx, tenantID, id string) (Plan, error) {
	p, err := lockedPlan(ctx, tx, tenantID, id)
	if err != nil {
		return Plan{}, err
	}
	if p.Status != "OPEN" {
		return Plan{}, ErrPlanWrongState
	}
	return setPlanStatus(ctx, tx, p, "CLOSED_TO_NEW")
}

// ReopenPlan undoes a close. A plan that has a successor stays closed: a
// replaced plan accepting members again would defeat the replacement.
func ReopenPlan(ctx context.Context, tx pgx.Tx, tenantID, id string) (Plan, error) {
	p, err := lockedPlan(ctx, tx, tenantID, id)
	if err != nil {
		return Plan{}, err
	}
	if p.Status != "CLOSED_TO_NEW" || p.SupersededByID != nil {
		return Plan{}, ErrPlanWrongState
	}
	return setPlanStatus(ctx, tx, p, "OPEN")
}

// SupersedePlan names the plan that replaces this one and closes it to new
// members. The successor must be a different plan in the tenant that is OPEN
// and has no successor of its own, so the chain cannot loop. Holders are not
// moved: that is explicit and per membership (MovePlan).
func SupersedePlan(ctx context.Context, tx pgx.Tx, tenantID, id, successorID string) (Plan, error) {
	if successorID == "" || successorID == id {
		return Plan{}, fmt.Errorf("%w: the successor must be a different plan", ErrInvalidPlanTarget)
	}
	// Lock in id order so two supersessions naming each other's plans cannot
	// deadlock.
	locked := map[string]Plan{}
	order := []string{id, successorID}
	if order[1] < order[0] {
		order[0], order[1] = order[1], order[0]
	}
	for _, pid := range order {
		pl, err := lockedPlan(ctx, tx, tenantID, pid)
		if errors.Is(err, ErrNotFound) && pid == successorID {
			return Plan{}, fmt.Errorf("%w: no such plan in this tenant", ErrInvalidPlanTarget)
		}
		if err != nil {
			return Plan{}, err
		}
		locked[pid] = pl
	}
	p, succ := locked[id], locked[successorID]
	if p.SupersededByID != nil || (p.Status != "OPEN" && p.Status != "CLOSED_TO_NEW") {
		return Plan{}, ErrPlanWrongState
	}
	// OPEN is the whole test: a replaced plan is always closed (migration 0011's
	// CHECK), so "not itself replaced" needs no clause of its own, and the
	// chain cannot loop because every plan in it but the last is closed.
	if succ.Status != "OPEN" {
		return Plan{}, fmt.Errorf("%w: the successor must be OPEN", ErrInvalidPlanTarget)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE membership_plan SET status = 'CLOSED_TO_NEW', superseded_by_id = $2::uuid
		 WHERE id = $1::uuid`, id, successorID); err != nil {
		return Plan{}, fmt.Errorf("membership: supersede plan: %w", err)
	}
	p.Status, p.SupersededByID = "CLOSED_TO_NEW", &successorID
	return p, nil
}

// RetirePlan ends a closed plan once nobody holds it. A membership still
// ACTIVE, SUSPENDED or LAPSED on it counts as holding it; a TERMINATED one does
// not.
func RetirePlan(ctx context.Context, tx pgx.Tx, tenantID, id string) (Plan, error) {
	p, err := lockedPlan(ctx, tx, tenantID, id)
	if err != nil {
		return Plan{}, err
	}
	if p.Status != "CLOSED_TO_NEW" {
		return Plan{}, ErrPlanWrongState
	}
	var holders int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::int FROM membership
		 WHERE tenant_id = $1 AND plan_id = $2::uuid AND state <> 'TERMINATED'`,
		tenantID, id).Scan(&holders); err != nil {
		return Plan{}, fmt.Errorf("membership: count holders: %w", err)
	}
	if holders > 0 {
		return Plan{}, ErrPlanHasHolders
	}
	return setPlanStatus(ctx, tx, p, "RETIRED")
}

// MovePlan moves one membership onto another plan, which is how a holder of a
// closed or superseded plan is migrated (05.3.2: "explicit, per-membership,
// auditable"). The new plan must be OPEN.
//
// An ACTIVE membership follows 8.3's revocation path for the plan it leaves
// (lock the fence, void a pending grant, delete the coverage tuple
// synchronously, roll back if the delete fails) and queues the grant for the
// plan it joins. The new coverage may lag a dispatch, so for that moment the
// member is under-covered, never over-covered. A SUSPENDED membership has no
// coverage to move; its plan changes and reinstating grants the new one.
func MovePlan(
	ctx context.Context, tx pgx.Tx, tenantID, id, toPlanID, actorPersonID string, rev Revoker,
) (Membership, error) {
	if toPlanID == "" {
		return Membership{}, fmt.Errorf("%w: plan_id is required", ErrInvalidPlanTarget)
	}
	state, fromPlanID, err := lockedState(ctx, tx, tenantID, id)
	if err != nil {
		return Membership{}, err
	}
	if state != "ACTIVE" && state != "SUSPENDED" {
		return Membership{}, ErrWrongState
	}
	if toPlanID == fromPlanID {
		return Membership{}, fmt.Errorf("%w: the member is already on that plan", ErrInvalidPlanTarget)
	}
	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM membership_plan
		 WHERE id = $1::uuid AND tenant_id = $2 FOR SHARE`, toPlanID, tenantID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Membership{}, fmt.Errorf("%w: no such plan in this tenant", ErrInvalidPlanTarget)
	}
	if err != nil {
		return Membership{}, fmt.Errorf("membership: read target plan: %w", err)
	}
	if status != "OPEN" {
		return Membership{}, fmt.Errorf("%w: the plan is %s", ErrPlanNotOpen, status)
	}

	if state == "ACTIVE" {
		// Void takes the fence's advisory lock first, so the lock precedes the
		// state change, exactly as in Suspend.
		if _, err := outbox.Void(ctx, tx, coveredFence(id, fromPlanID)); err != nil {
			return Membership{}, err
		}
	}
	m, err := scanMembership(tx.QueryRow(ctx,
		`UPDATE membership SET plan_id = $2::uuid WHERE id = $1::uuid RETURNING `+membershipColumns,
		id, toPlanID))
	if err != nil {
		return Membership{}, fmt.Errorf("membership: move plan: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO membership_plan_change
		    (tenant_id, membership_id, from_plan_id, to_plan_id, changed_by_person_id)
		VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5::uuid)`,
		tenantID, id, fromPlanID, toPlanID, actorPersonID); err != nil {
		return Membership{}, fmt.Errorf("membership: record plan change: %w", err)
	}
	if state == "ACTIVE" {
		if err := revokeCoverage(ctx, tx, rev, id, fromPlanID); err != nil {
			return Membership{}, err
		}
		if err := outbox.Enqueue(ctx, tx, outbox.Fact{
			AggregateType: "membership",
			AggregateID:   id,
			EventType:     EventMembershipCovered,
			Payload:       map[string]string{"membership_id": id, "plan_id": toPlanID},
			Fence:         coveredFence(id, toPlanID),
		}); err != nil {
			return Membership{}, err
		}
	}
	return m, nil
}
