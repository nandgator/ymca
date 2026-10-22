// admission.go is A3.7's `POST /t/{t}/memberships` in domain form: the act
// that turns a person the tenant has registered into a member of it.
//
// It is the last link in the chain the configuration endpoints built. A
// bundle entitles a consumption type, a plan grants the bundle, and this is
// where a PERSON finally enters that path — through the `covered_member`
// tuple of ADR-107, which is three lines and whose absence says nothing. A
// membership admitted without it simply holds no entitlements: every
// `may_record` check denies, correctly, and no error anywhere reports why.
// That is the reason the integration test asserts the ALLOW rather than
// asserting that three rows were enqueued.
//
// Nothing here computes a term from the plan. `valid_from` and `valid_until`
// are the caller's, unvalidated against `plan.duration` — the same reasoning
// that settled the membership number (A3.7): an association's own convention
// survives, and B5 imports historical memberships without a special case.
// The consequence is that a LIFETIME plan will accept a membership with an
// end date, which nothing reads today. Recorded in 11.2 rather than fixed by
// a derivation the record does not specify.
package membership

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nandgator/ymca/backend/internal/outbox"
)

// Event types admission publishes. Three facts, not one, because each is
// separately revocable and the fence names a fact rather than an operation
// (ADR-101): a suspension lifts coverage while the membership and its holder
// remain, and one fact fencing all three tuples could not express that.
//
// lifecycle.go (A3.13) is what revokes the third of them, and reinstating
// re-enqueues it under the same fence — which is why the event type is shared.
const (
	EventMembershipAdmitted = "MembershipAdmitted"
	EventMembershipHeldBy   = "MembershipHeldBy"
	EventMembershipCovered  = "MembershipCoveredByPlan"
)

// Membership is one person's membership of a tenant (A2.4, 05.3.5).
//
// The dates are strings because A2.4 makes them `date`, not `timestamptz`: a
// term runs in the tenant's own days, and rendering them through a Go
// time.Time would attach an offset the column does not carry.
type Membership struct {
	ID         string  `json:"id"`
	PersonID   string  `json:"person_id"`
	PlanID     string  `json:"plan_id"`
	OrgUnitID  *string `json:"org_unit_id"`
	Number     string  `json:"number"`
	State      string  `json:"state"`
	AdmittedAt string  `json:"admitted_at"`
	ValidFrom  *string `json:"valid_from"`
	ValidUntil *string `json:"valid_until"`
}

// NewMembership is what a caller must supply to admit somebody.
//
// OrgUnitID is a pointer because NULL is a real answer and not a missing
// one: an association-level membership belongs to no unit, and appears under
// no unit's member list (A2.4, ADR-104, and ListUnitMembers' own comment).
type NewMembership struct {
	PersonID   string
	PlanID     string
	OrgUnitID  *string
	Number     string
	ValidFrom  *string // ISO 8601 date; the database parses it
	ValidUntil *string
}

// Admit records a membership in state ACTIVE and queues its three
// authorization facts.
//
// The plan is joined rather than merely referenced, so that its status is
// read and the row written in one statement. 05.3.2 makes `CLOSED_TO_NEW`
// "the common real state — existing life members keep a plan that no longer
// accepts applicants", and a plan in DRAFT is not live yet: only OPEN admits.
// Checking it in a separate SELECT would leave a window in which a plan
// closed between the read and the insert.
func Admit(ctx context.Context, tx pgx.Tx, tenantID string, in NewMembership) (Membership, error) {
	m := Membership{}
	err := tx.QueryRow(ctx, `
		INSERT INTO membership
		    (id, tenant_id, person_id, plan_id, org_unit_id, number,
		     state, admitted_at, valid_from, valid_until)
		SELECT gen_random_uuid(), $1, $2::uuid, p.id, $4::uuid, $5,
		       'ACTIVE', now(), $6::date, $7::date
		  FROM membership_plan p
		 WHERE p.id = $3::uuid
		   AND p.tenant_id = $1
		   AND p.status = 'OPEN'
		RETURNING id::text, person_id::text, plan_id::text, org_unit_id::text,
		          number, state,
		          to_char(admitted_at, 'YYYY-MM-DD"T"HH24:MI:SSOF:00'),
		          to_char(valid_from, 'YYYY-MM-DD'),
		          to_char(valid_until, 'YYYY-MM-DD')
	`, tenantID, in.PersonID, in.PlanID, in.OrgUnitID, in.Number,
		in.ValidFrom, in.ValidUntil).
		Scan(&m.ID, &m.PersonID, &m.PlanID, &m.OrgUnitID, &m.Number, &m.State,
			&m.AdmittedAt, &m.ValidFrom, &m.ValidUntil)
	if err != nil {
		return Membership{}, admitError(ctx, tx, tenantID, in, err)
	}

	// The tenant edge first. ADR-018 makes it the precondition for anything
	// about this membership resolving in the graph, and the order facts are
	// enqueued in is the order the dispatcher applies them.
	if err := outbox.Enqueue(ctx, tx, outbox.Fact{
		AggregateType: "membership",
		AggregateID:   m.ID,
		EventType:     EventMembershipAdmitted,
		Payload:       map[string]string{"tenant_id": tenantID, "membership_id": m.ID},
		Fence: outbox.Fence{
			Subject:  "tenant:" + tenantID,
			Relation: "tenant",
			Object:   "membership:" + m.ID,
		},
	}); err != nil {
		return Membership{}, err
	}

	// The holder. A1.2 gives membership `covered: holder or dependent`, so
	// this tuple is what makes the userset in the next fact non-empty —
	// `membership:<id>#covered` resolves to nobody without it, and the
	// covered_member edge then grants nothing while looking correct.
	if err := outbox.Enqueue(ctx, tx, outbox.Fact{
		AggregateType: "membership",
		AggregateID:   m.ID,
		EventType:     EventMembershipHeldBy,
		Payload:       map[string]string{"membership_id": m.ID, "person_id": m.PersonID},
		Fence: outbox.Fence{
			Subject:  "person:" + m.PersonID,
			Relation: "holder",
			Object:   "membership:" + m.ID,
		},
	}); err != nil {
		return Membership{}, err
	}

	// ADR-107's tuple, one per membership. The PLAN is the object and the
	// membership's `covered` userset is the user: OpenFGA traverses forward
	// from the object, and `entitlement_bundle.beneficiary` reads
	// `covered_member from via_plan`. Written the other way round it reads
	// just as naturally and resolves to nothing, which is exactly what the
	// original model did.
	if err := outbox.Enqueue(ctx, tx, outbox.Fact{
		AggregateType: "membership",
		AggregateID:   m.ID,
		EventType:     EventMembershipCovered,
		Payload:       map[string]string{"membership_id": m.ID, "plan_id": m.PlanID},
		Fence: outbox.Fence{
			Subject:  "membership:" + m.ID + "#covered",
			Relation: "covered_member",
			Object:   "membership_plan:" + m.PlanID,
		},
	}); err != nil {
		return Membership{}, err
	}

	return m, nil
}

// admitError turns PostgreSQL's refusal into this package's vocabulary.
//
// The constraint NAMES are a contract between the schema and this file, in
// the same way migration 0005's are (ADR-115). Renaming one compiles, vets
// clean, and turns a 409 the caller can act on into a 500 that says nothing;
// the integration test fails if either name changes.
func admitError(
	ctx context.Context, tx pgx.Tx, tenantID string, in NewMembership, err error,
) error {
	// No row came back from the INSERT ... SELECT: the plan did not match.
	// Either it is not this tenant's (RLS already hid it, so it reads as
	// absent) or it is not OPEN, and those are different answers to the
	// caller. One query separates them, and it runs only on this path.
	if errors.Is(err, pgx.ErrNoRows) {
		var status string
		switch scanErr := tx.QueryRow(ctx,
			`SELECT status FROM membership_plan WHERE id = $1::uuid AND tenant_id = $2`,
			in.PlanID, tenantID).Scan(&status); {
		case errors.Is(scanErr, pgx.ErrNoRows):
			return ErrNotFound
		case scanErr != nil:
			return fmt.Errorf("membership: read plan status: %w", scanErr)
		default:
			return fmt.Errorf("%w: the plan is %s; 05.3.2 admits only OPEN",
				ErrPlanNotOpen, status)
		}
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return fmt.Errorf("membership: admit: %w", err)
	}
	switch {
	case pgErr.Code == uniqueViolation && pgErr.ConstraintName == "membership_tenant_id_number_key":
		return ErrNumberTaken
	case pgErr.Code == uniqueViolation && pgErr.ConstraintName == "one_active_membership_per_tenant":
		return ErrAlreadyActiveMember
	case pgErr.Code == foreignKeyViolation:
		// The person or the unit does not exist — or, for the unit, is
		// another tenant's and so invisible to the constraint under RLS.
		// A3.3 makes those indistinguishable on purpose.
		return ErrNotFound
	default:
		return fmt.Errorf("membership: admit: %w", err)
	}
}
