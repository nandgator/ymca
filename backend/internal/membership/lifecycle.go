// lifecycle.go is A3.13: suspending, reinstating and terminating a membership.
//
// This is the first code to run 8.3's REVOCATION path. ADR-101's fence, Void
// and DeleteTuples were built and tested in isolation, and nothing in the
// application revoked anything: admission wrote three grants and no endpoint
// could take one back, so coverage outlived the membership that justified it.
//
// The path, in the order 8.3 gives it and that matters:
//
//	lock the fence key             (Void takes it first)
//	mark the state in PostgreSQL
//	void any pending row for the fact
//	delete the tuple from OpenFGA  synchronously, BEFORE the commit
//	a failed delete rolls everything back and is reported
//
// The failure mode is "revocation did not happen and you were told", never
// "appeared to succeed but authority persisted". A commit that fails AFTER a
// successful delete leaves the member under-covered, which is the safe side and
// which a retry heals.
package membership

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nandgator/ymca/backend/internal/outbox"
)

// ErrWrongState is 409 membership_wrong_state: 05.3's state machine has no such
// transition from where the membership is.
var ErrWrongState = errors.New("membership: that transition is not allowed from the membership's current state")

// ErrInvalidReason is invalid_request: not one of 05.3's four categories.
var ErrInvalidReason = errors.New("membership: reason must be ARREARS, CONDUCT, SAFEGUARDING or ADMINISTRATIVE")

// ErrRevocationFailed means the tuple could not be deleted and so NOTHING was
// changed: the caller's transaction rolls back. It is reported as a 500 that
// says so, because "the revocation did not happen" is the one thing a caller
// must never have to infer.
var ErrRevocationFailed = errors.New("membership: the revocation did not happen and nothing was changed")

// Revoker is the synchronous half of the revocation path: remove tuples from
// the authorization store, and fail if that did not happen. *outbox.FGAWriter
// satisfies it; a test supplies one that fails, which is the only way to see
// the rollback.
type Revoker interface {
	DeleteTuples(ctx context.Context, tuples []outbox.Tuple) error
}

var suspensionReasons = map[string]bool{
	"ARREARS": true, "CONDUCT": true, "SAFEGUARDING": true, "ADMINISTRATIVE": true,
}

// constraintOneOpenSuspension is migration 0008's index. Its name is a contract
// with this file, as migration 0005's are with internal/organization.
const constraintOneOpenSuspension = "membership_one_open_suspension"

const membershipColumns = `
	id::text, person_id::text, plan_id::text, org_unit_id::text, number, state,
	to_char(admitted_at, 'YYYY-MM-DD"T"HH24:MI:SSOF:00'),
	to_char(valid_from, 'YYYY-MM-DD'), to_char(valid_until, 'YYYY-MM-DD')`

func scanMembership(row pgx.Row) (Membership, error) {
	var m Membership
	err := row.Scan(&m.ID, &m.PersonID, &m.PlanID, &m.OrgUnitID, &m.Number, &m.State,
		&m.AdmittedAt, &m.ValidFrom, &m.ValidUntil)
	return m, err
}

// coveredFence is the fact a suspension lifts: the membership's coverage of its
// plan (ADR-107). It is the same fence admission enqueued the grant under, which
// is what lets Void find a grant that has not been dispatched yet.
func coveredFence(membershipID, planID string) outbox.Fence {
	return outbox.Fence{
		Subject:  "membership:" + membershipID + "#covered",
		Relation: "covered_member",
		Object:   "membership_plan:" + planID,
	}
}

func lockedState(ctx context.Context, tx pgx.Tx, tenantID, id string) (state, planID string, err error) {
	err = tx.QueryRow(ctx, `
		SELECT state, plan_id::text FROM membership
		 WHERE id = $1::uuid AND tenant_id = $2 FOR UPDATE`, id, tenantID).Scan(&state, &planID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("membership: read state: %w", err)
	}
	return state, planID, nil
}

// revokeCoverage is the synchronous delete, after the state is marked and the
// pending rows voided — both of which the caller did FIRST, because Void takes
// the fence's advisory lock and the lock must precede the state change.
func revokeCoverage(ctx context.Context, tx pgx.Tx, rev Revoker, membershipID, planID string) error {
	fence := coveredFence(membershipID, planID)
	if err := rev.DeleteTuples(ctx, []outbox.Tuple{{
		User: fence.Subject, Relation: fence.Relation, Object: fence.Object,
	}}); err != nil {
		// Returned, not logged and swallowed: the caller's transaction rolls
		// back and the response says the revocation did not happen.
		return fmt.Errorf("%w: %v", ErrRevocationFailed, err)
	}
	return nil
}

// Suspend lifts a membership's coverage while leaving the membership and its
// holder in place. actorPersonID is who issued it (ADR-047: never "system").
func Suspend(
	ctx context.Context, tx pgx.Tx, tenantID, id, reason, actorPersonID string, rev Revoker,
) (Membership, error) {
	if !suspensionReasons[reason] {
		return Membership{}, ErrInvalidReason
	}
	state, planID, err := lockedState(ctx, tx, tenantID, id)
	if err != nil {
		return Membership{}, err
	}
	if state != "ACTIVE" {
		return Membership{}, ErrWrongState
	}

	// Void takes the fence's advisory lock as its first act, so the lock
	// precedes every write below: a grant racing this revocation waits on it
	// and finds its row voided rather than slipping past.
	if _, err := outbox.Void(ctx, tx, coveredFence(id, planID)); err != nil {
		return Membership{}, err
	}
	m, err := scanMembership(tx.QueryRow(ctx,
		`UPDATE membership SET state = 'SUSPENDED' WHERE id = $1::uuid RETURNING `+membershipColumns, id))
	if err != nil {
		return Membership{}, fmt.Errorf("membership: mark suspended: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO membership_suspension
		    (id, membership_id, reason_category, issued_by, effective_from)
		VALUES (gen_random_uuid(), $1::uuid, $2, $3::uuid, now())
	`, id, reason, actorPersonID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == constraintOneOpenSuspension {
			return Membership{}, ErrWrongState
		}
		return Membership{}, fmt.Errorf("membership: record suspension: %w", err)
	}
	if err := revokeCoverage(ctx, tx, rev, id, planID); err != nil {
		return Membership{}, err
	}
	return m, nil
}

// Reinstate lifts a suspension and re-enqueues the coverage grant. Grants may
// lag (8.3): the member can record again after the next dispatch.
//
// It can collide with ADR-043. A suspension frees the person's ACTIVE slot, so
// they may have been admitted again meanwhile, and reinstating the old
// membership would then make two.
func Reinstate(ctx context.Context, tx pgx.Tx, tenantID, id string) (Membership, error) {
	state, planID, err := lockedState(ctx, tx, tenantID, id)
	if err != nil {
		return Membership{}, err
	}
	if state != "SUSPENDED" && state != "LAPSED" {
		return Membership{}, ErrWrongState
	}
	m, err := scanMembership(tx.QueryRow(ctx,
		`UPDATE membership SET state = 'ACTIVE' WHERE id = $1::uuid RETURNING `+membershipColumns, id))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "one_active_membership_per_tenant" {
			return Membership{}, ErrAlreadyActiveMember
		}
		return Membership{}, fmt.Errorf("membership: mark active: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE membership_suspension SET lifted_at = now()
		  WHERE membership_id = $1::uuid AND lifted_at IS NULL`, id); err != nil {
		return Membership{}, fmt.Errorf("membership: lift suspension: %w", err)
	}
	fence := coveredFence(id, planID)
	if err := outbox.Enqueue(ctx, tx, outbox.Fact{
		AggregateType: "membership",
		AggregateID:   id,
		EventType:     EventMembershipCovered,
		Payload:       map[string]string{"membership_id": id, "plan_id": planID},
		Fence:         fence,
	}); err != nil {
		return Membership{}, err
	}
	return m, nil
}

// Terminate ends a membership for good. Its coverage is revoked exactly as a
// suspension's is, and the state is terminal: there is no way back, and a
// returning member is admitted afresh.
//
// Ending the member's obligations is the caller's, because they belong to
// another context (A3.13); the membership id is what it needs.
func Terminate(
	ctx context.Context, tx pgx.Tx, tenantID, id string, rev Revoker,
) (Membership, error) {
	state, planID, err := lockedState(ctx, tx, tenantID, id)
	if err != nil {
		return Membership{}, err
	}
	if state == "TERMINATED" {
		return Membership{}, ErrWrongState
	}
	if _, err := outbox.Void(ctx, tx, coveredFence(id, planID)); err != nil {
		return Membership{}, err
	}
	m, err := scanMembership(tx.QueryRow(ctx,
		`UPDATE membership SET state = 'TERMINATED' WHERE id = $1::uuid RETURNING `+membershipColumns, id))
	if err != nil {
		return Membership{}, fmt.Errorf("membership: mark terminated: %w", err)
	}
	// A suspension still open when the membership ends is closed with it, so
	// no row says the member is suspended in a terminated membership.
	if _, err := tx.Exec(ctx,
		`UPDATE membership_suspension SET lifted_at = now()
		  WHERE membership_id = $1::uuid AND lifted_at IS NULL`, id); err != nil {
		return Membership{}, fmt.Errorf("membership: close suspension: %w", err)
	}
	if err := revokeCoverage(ctx, tx, rev, id, planID); err != nil {
		return Membership{}, err
	}
	return m, nil
}
