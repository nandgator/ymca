package consumption

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Obligation is a standing expectation of use (05.10.4): one row per person
// per type, never one per day.
type Obligation struct {
	ID              string  `json:"id"`
	TypeID          string  `json:"consumption_type_id"`
	SubjectPersonID string  `json:"subject_person_id"`
	SourceType      string  `json:"source_type"`
	SourceID        string  `json:"source_id"`
	EffectiveFrom   string  `json:"effective_from"`
	EffectiveUntil  *string `json:"effective_until"`
}

// NewObligation is what a caller supplies. Only a MEMBERSHIP source has an
// endpoint (A3.10); STAY and SUBSCRIPTION arrive with the contexts that own
// them.
type NewObligation struct {
	SubjectPersonID string
	MembershipID    string
	EffectiveFrom   string
	EffectiveUntil  *string
}

// CreateObligation obligates a member to a type.
//
// The membership must be ACTIVE, in this tenant, and held by the named
// person. A holder-only rule: dependents are unreachable (11.2), so the
// holder is the only person a membership can obligate today.
func CreateObligation(
	ctx context.Context, tx pgx.Tx, tenantID, typeID string, in NewObligation,
) (Obligation, error) {
	lo, hi, err := dayRange(in.EffectiveFrom, in.EffectiveUntil)
	if err != nil {
		return Obligation{}, err
	}

	var obligates bool
	err = tx.QueryRow(ctx,
		`SELECT obligates FROM consumption_type WHERE id = $1::uuid AND tenant_id = $2`,
		typeID, tenantID).Scan(&obligates)
	if errors.Is(err, pgx.ErrNoRows) {
		return Obligation{}, ErrNotFound
	}
	if err != nil {
		return Obligation{}, fmt.Errorf("consumption: read type: %w", err)
	}
	if !obligates {
		return Obligation{}, ErrNotObligating
	}

	var held bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM membership
		     WHERE id = $1::uuid AND tenant_id = $2
		       AND person_id = $3::uuid AND state = 'ACTIVE')
	`, in.MembershipID, tenantID, in.SubjectPersonID).Scan(&held)
	if err != nil {
		return Obligation{}, fmt.Errorf("consumption: read membership: %w", err)
	}
	if !held {
		return Obligation{}, ErrNotAMember
	}

	var o Obligation
	err = tx.QueryRow(ctx, `
		INSERT INTO consumption_obligation
		    (id, tenant_id, consumption_type_id, subject_person_id, effective,
		     source_type, source_id)
		VALUES (gen_random_uuid(), $1, $2::uuid, $3::uuid,
		        tstzrange(($4::date)::timestamp AT TIME ZONE 'UTC',
		                  ($5::date)::timestamp AT TIME ZONE 'UTC'),
		        'MEMBERSHIP', $6::uuid)
		RETURNING id::text, consumption_type_id::text, subject_person_id::text,
		          source_type, source_id::text
	`, tenantID, typeID, in.SubjectPersonID, lo, hi, in.MembershipID).
		Scan(&o.ID, &o.TypeID, &o.SubjectPersonID, &o.SourceType, &o.SourceID)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == exclusionViolation {
		return Obligation{}, ErrObligationOverlaps
	}
	if err != nil {
		return Obligation{}, fmt.Errorf("consumption: create obligation: %w", err)
	}
	o.EffectiveFrom = in.EffectiveFrom
	o.EffectiveUntil = in.EffectiveUntil
	return o, nil
}

// EndObligationsFor stops every obligation sourced from one relationship after
// today (UTC), which is how an obligation ends: "created and ended by the
// source relationship, never by a clock" (05.10.4). Today's own unit is kept —
// a member terminated at noon has still been owed dinner that evening.
//
// Obligations already ended earlier are left alone. It returns how many it
// shortened.
func EndObligationsFor(
	ctx context.Context, tx pgx.Tx, tenantID, sourceType, sourceID string,
) (int64, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE consumption_obligation
		   SET effective = tstzrange(
		           lower(effective),
		           (date_trunc('day', now() AT TIME ZONE 'UTC') + interval '1 day') AT TIME ZONE 'UTC')
		 WHERE tenant_id = $1 AND source_type = $2 AND source_id = $3::uuid
		   AND (upper_inf(effective)
		        OR upper(effective) > (date_trunc('day', now() AT TIME ZONE 'UTC') + interval '1 day') AT TIME ZONE 'UTC')
		   AND lower(effective) < (date_trunc('day', now() AT TIME ZONE 'UTC') + interval '1 day') AT TIME ZONE 'UTC'
	`, tenantID, sourceType, sourceID)
	if err != nil {
		return 0, fmt.Errorf("consumption: end obligations: %w", err)
	}
	// An obligation that had not begun cannot be shortened to an empty range,
	// and it must not begin after the relationship is over: it is removed.
	gone, err := tx.Exec(ctx, `
		DELETE FROM consumption_obligation
		 WHERE tenant_id = $1 AND source_type = $2 AND source_id = $3::uuid
		   AND lower(effective) >= (date_trunc('day', now() AT TIME ZONE 'UTC') + interval '1 day') AT TIME ZONE 'UTC'
	`, tenantID, sourceType, sourceID)
	if err != nil {
		return 0, fmt.Errorf("consumption: remove unstarted obligations: %w", err)
	}
	return tag.RowsAffected() + gone.RowsAffected(), nil
}
