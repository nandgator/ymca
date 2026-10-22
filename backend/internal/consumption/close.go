package consumption

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// VariantCount and OptionCount are the vocabulary 05.10.8 says Consumption
// reports to Finance: units and names, never amounts it computed. Option
// amounts are the catalogue's price as it stood at close.
type VariantCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

type OptionCount struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	ChargeKind string `json:"charge_kind"`
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
	Count      int    `json:"count"`
}

// Summary is the published ConsumptionPeriodClosed (05.10.8), and is what the
// close row stores: Finance reads this, not a recomputation.
type Summary struct {
	Recurrence     *string        `json:"recurrence"`
	ObligatedUnits int            `json:"obligated_units"`
	RelievedUnits  int            `json:"relieved_units"`
	RecordedUnits  int            `json:"recorded_units"`
	Variants       []VariantCount `json:"variants"`
	Options        []OptionCount  `json:"options"`
}

// Closed is one person's closed period.
type Closed struct {
	ID              string  `json:"id"`
	TypeID          string  `json:"consumption_type_id"`
	TypeName        string  `json:"-"`
	SubjectPersonID string  `json:"subject_person_id"`
	From            string  `json:"from"`
	To              string  `json:"to"`
	ObligatedUnits  int     `json:"obligated_units"`
	Summary         Summary `json:"summary"`
	// InvoiceID is filled by the handler after Finance has composed the
	// charge; Consumption never learns of it (05.10.8).
	InvoiceID *string `json:"invoice_id"`
}

// headPredicate selects one person's live heads of one type in [$3, $4): not
// voided and not superseded. The same predicate counts units, variants and
// options, so the three cannot disagree about which records exist.
const headPredicate = `
	r.consumption_type_id = $1::uuid AND r.subject_person_id = $2::uuid
	  AND r.occurred_on >= $3::date AND r.occurred_on < $4::date
	  AND NOT r.voided
	  AND NOT EXISTS (SELECT 1 FROM consumption_record s WHERE s.supersedes_id = r.id)`

// ClosePeriod closes [from, to] for every person the period concerns: anyone
// with an obligation overlapping it or a live record inside it.
//
// All or nothing, in the caller's transaction. Exactly-once per person-period
// is the exclusion constraint on consumption_period_close, not anything in
// this function (05.10.12): a second close that overlaps raises 23P01 however
// the requests interleave.
func ClosePeriod(
	ctx context.Context, tx pgx.Tx, tenantID, typeID, closedByPersonID, from, to string,
) ([]Closed, error) {
	lo, hi, err := dayRange(from, &to)
	if err != nil {
		return nil, err
	}

	var typeName string
	var recurrence *string
	var obligates bool
	err = tx.QueryRow(ctx,
		`SELECT name, obligates, recurrence FROM consumption_type
		  WHERE id = $1::uuid AND tenant_id = $2`,
		typeID, tenantID).Scan(&typeName, &obligates, &recurrence)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("consumption: read type: %w", err)
	}

	subjects, err := periodSubjects(ctx, tx, typeID, lo.Format("2006-01-02"), hi.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	sort.Strings(subjects) // lock order: two closes must not deadlock

	var out []Closed
	for _, subject := range subjects {
		if err := lockSubject(ctx, tx, typeID, subject); err != nil {
			return nil, err
		}
		c := Closed{
			TypeID: typeID, TypeName: typeName, SubjectPersonID: subject,
			From: from, To: to,
		}
		c.Summary, err = summarize(ctx, tx, typeID, subject,
			lo.Format("2006-01-02"), hi.Format("2006-01-02"), obligates, recurrence)
		if err != nil {
			return nil, err
		}
		c.ObligatedUnits = c.Summary.ObligatedUnits

		body, err := json.Marshal(c.Summary)
		if err != nil {
			return nil, fmt.Errorf("consumption: encode summary: %w", err)
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO consumption_period_close
			    (id, tenant_id, consumption_type_id, subject_person_id, period,
			     closed_by_person_id, obligated_units, summary)
			VALUES (gen_random_uuid(), $1, $2::uuid, $3::uuid,
			        daterange($4::date, $5::date, '[)'), $6::uuid, $7, $8)
			RETURNING id::text
		`, tenantID, typeID, subject, lo, hi, closedByPersonID,
			c.ObligatedUnits, body).Scan(&c.ID)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == exclusionViolation {
			return nil, ErrPeriodAlreadyClosed
		}
		if err != nil {
			return nil, fmt.Errorf("consumption: write close: %w", err)
		}
		out = append(out, c)
	}
	return out, nil
}

func periodSubjects(ctx context.Context, tx pgx.Tx, typeID, lo, hi string) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT subject_person_id::text FROM consumption_obligation
		 WHERE consumption_type_id = $1::uuid
		   AND effective && tstzrange(($2::date)::timestamp AT TIME ZONE 'UTC',
		                              ($3::date)::timestamp AT TIME ZONE 'UTC')
		UNION
		SELECT r.subject_person_id::text FROM consumption_record r
		 WHERE r.consumption_type_id = $1::uuid
		   AND r.occurred_on >= $2::date AND r.occurred_on < $3::date
		   AND NOT r.voided
		   AND NOT EXISTS (SELECT 1 FROM consumption_record s WHERE s.supersedes_id = r.id)
	`, typeID, lo, hi)
	if err != nil {
		return nil, fmt.Errorf("consumption: period subjects: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// summarize derives one person's units and vocabulary for [lo, hi).
//
// expected = recurrence x (effective ∩ period) - periods relieved (05.10.4).
// The days are generated here and tested against the obligation and the
// absences at query time — nothing is materialized, so a stalled job cannot
// under-bill (05.10.4, 8.7).
func summarize(
	ctx context.Context, tx pgx.Tx, typeID, subject, lo, hi string,
	obligates bool, recurrence *string,
) (Summary, error) {
	s := Summary{Recurrence: recurrence, Variants: []VariantCount{}, Options: []OptionCount{}}

	if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM consumption_record r WHERE `+headPredicate,
		typeID, subject, lo, hi).Scan(&s.RecordedUnits); err != nil {
		return Summary{}, fmt.Errorf("consumption: count records: %w", err)
	}

	if obligates && recurrence != nil {
		var days, billableDays, weeks, billableWeeks int
		err := tx.QueryRow(ctx, `
			WITH days AS (
			    SELECT d::date AS day
			      FROM generate_series($3::date::timestamp, ($4::date - 1)::timestamp,
			                           interval '1 day') d
			     WHERE EXISTS (
			         SELECT 1 FROM consumption_obligation o
			          WHERE o.consumption_type_id = $1::uuid
			            AND o.subject_person_id = $2::uuid
			            AND o.effective @> (d::date::timestamp AT TIME ZONE 'UTC'))
			), flagged AS (
			    SELECT day,
			           EXISTS (
			               SELECT 1 FROM expected_absence a
			                WHERE a.subject_person_id = $2::uuid
			                  AND a.withdrawn_at IS NULL
			                  AND a.relieves_payment
			                  AND a.period @> (day::timestamp AT TIME ZONE 'UTC')) AS relieved
			      FROM days
			)
			SELECT count(*)::int,
			       (count(*) FILTER (WHERE NOT relieved))::int,
			       count(DISTINCT date_trunc('week', day))::int,
			       (count(DISTINCT date_trunc('week', day)) FILTER (WHERE NOT relieved))::int
			  FROM flagged
		`, typeID, subject, lo, hi).Scan(&days, &billableDays, &weeks, &billableWeeks)
		if err != nil {
			return Summary{}, fmt.Errorf("consumption: count obligated days: %w", err)
		}
		switch *recurrence {
		case "DAILY":
			s.ObligatedUnits, s.RelievedUnits = billableDays, days-billableDays
		case "WEEKLY":
			s.ObligatedUnits, s.RelievedUnits = billableWeeks, weeks-billableWeeks
		case "PER_OCCURRENCE":
			// Nothing is standing: the units are what was recorded.
			s.ObligatedUnits = s.RecordedUnits
		}
	}

	vrows, err := tx.Query(ctx, `
		SELECT r.variant_code, count(*)::int FROM consumption_record r
		 WHERE `+headPredicate+`
		   AND r.variant_code IS NOT NULL
		 GROUP BY r.variant_code ORDER BY r.variant_code`, typeID, subject, lo, hi)
	if err != nil {
		return Summary{}, fmt.Errorf("consumption: count variants: %w", err)
	}
	for vrows.Next() {
		var v VariantCount
		if err := vrows.Scan(&v.Code, &v.Count); err != nil {
			vrows.Close()
			return Summary{}, err
		}
		s.Variants = append(s.Variants, v)
	}
	vrows.Close()
	if err := vrows.Err(); err != nil {
		return Summary{}, err
	}

	orows, err := tx.Query(ctx, `
		SELECT o.code, o.name, o.charge_kind, o.amount::text, o.currency, count(*)::int
		  FROM consumption_record r
		  JOIN consumption_record_option cro ON cro.consumption_record_id = r.id
		  JOIN consumption_option o ON o.id = cro.consumption_option_id
		 WHERE `+headPredicate+`
		 GROUP BY o.id ORDER BY o.code`, typeID, subject, lo, hi)
	if err != nil {
		return Summary{}, fmt.Errorf("consumption: count options: %w", err)
	}
	for orows.Next() {
		var o OptionCount
		if err := orows.Scan(&o.Code, &o.Name, &o.ChargeKind, &o.Amount, &o.Currency, &o.Count); err != nil {
			orows.Close()
			return Summary{}, err
		}
		s.Options = append(s.Options, o)
	}
	orows.Close()
	return s, orows.Err()
}
