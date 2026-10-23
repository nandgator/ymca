package consumption

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nandgator/ymca/backend/internal/audit"
	"github.com/nandgator/ymca/backend/internal/page"
)

// Record is one occurrence of actual use (05.10.5). Immutable: a correction
// is another Record that supersedes it (ADR-098).
type Record struct {
	ID              string   `json:"id"`
	TypeID          string   `json:"consumption_type_id"`
	SubjectPersonID string   `json:"subject_person_id"`
	ActorPersonID   string   `json:"actor_person_id"`
	OccurredOn      string   `json:"occurred_on"`
	ObligationID    *string  `json:"obligation_id"`
	VariantCode     *string  `json:"variant_code"`
	OptionCodes     []string `json:"option_codes"`
	SupersedesID    *string  `json:"supersedes_id"`
	Voided          bool     `json:"voided"`
	RecordedAt      string   `json:"recorded_at"`
}

// NewRecord is what a caller supplies to record a use.
type NewRecord struct {
	TypeID          string
	SubjectPersonID string
	ActorPersonID   string
	OccurredOn      string
	VariantCode     *string
	OptionCodes     []string
}

// recordColumns is the SELECT list every reader of consumption_record uses,
// so that the struct and the query cannot drift apart in two places.
const recordColumns = `
	r.id::text, r.consumption_type_id::text, r.subject_person_id::text,
	r.actor_person_id::text, to_char(r.occurred_on, 'YYYY-MM-DD'),
	r.obligation_id::text, r.variant_code, r.supersedes_id::text, r.voided,
	to_char(r.recorded_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	ARRAY(SELECT o.code
	        FROM consumption_record_option cro
	        JOIN consumption_option o ON o.id = cro.consumption_option_id
	       WHERE cro.consumption_record_id = r.id
	       ORDER BY o.code)`

func scanRecord(row pgx.Row) (Record, error) {
	var r Record
	err := row.Scan(&r.ID, &r.TypeID, &r.SubjectPersonID, &r.ActorPersonID,
		&r.OccurredOn, &r.ObligationID, &r.VariantCode, &r.SupersedesID,
		&r.Voided, &r.RecordedAt, &r.OptionCodes)
	return r, err
}

// lockSubject serialises everything that decides what is true about one
// person's consumption of one type: recording, correcting and closing.
//
// Without it a record could commit between a close reading the period and
// writing its row, and be neither counted in the bill nor refused. It is a
// transaction-scoped advisory lock keyed by (type, subject); the close takes
// them in sorted order so two closes cannot deadlock each other.
func lockSubject(ctx context.Context, tx pgx.Tx, typeID, subjectID string) error {
	_, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"consumption:"+typeID+":"+subjectID)
	if err != nil {
		return fmt.Errorf("consumption: lock subject: %w", err)
	}
	return nil
}

// LockSubject is lockSubject for the one caller outside this package that
// has to prove the lock is taken: the close's own concurrency test.
func LockSubject(ctx context.Context, tx pgx.Tx, typeID, subjectID string) error {
	return lockSubject(ctx, tx, typeID, subjectID)
}

// dayClosed reports whether a day lies in a period already closed for the
// subject and type (migration 0006).
func dayClosed(ctx context.Context, tx pgx.Tx, typeID, subjectID string, day string) (bool, error) {
	var closed bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM consumption_period_close
		     WHERE consumption_type_id = $1::uuid
		       AND subject_person_id = $2::uuid
		       AND period @> $3::date)
	`, typeID, subjectID, day).Scan(&closed)
	if err != nil {
		return false, fmt.Errorf("consumption: read closed periods: %w", err)
	}
	return closed, nil
}

// resolveOptions maps option codes to ids, refusing any the type does not
// offer or has withdrawn. Duplicates collapse: asking for an egg twice in one
// record is one egg, and a primary key on (record, option) says the same.
func resolveOptions(
	ctx context.Context, tx pgx.Tx, tenantID, typeID string, codes []string,
) ([]string, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(codes))
	var distinct []string
	for _, c := range codes {
		if !seen[c] {
			seen[c] = true
			distinct = append(distinct, c)
		}
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text FROM consumption_option
		 WHERE tenant_id = $1 AND consumption_type_id = $2::uuid
		   AND status = 'AVAILABLE' AND code = ANY($3)
	`, tenantID, typeID, distinct)
	if err != nil {
		return nil, fmt.Errorf("consumption: resolve options: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("consumption: scan option: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) != len(distinct) {
		return nil, ErrUnknownOption
	}
	return ids, nil
}

// RecordUse records one occurrence of consumption.
//
// The order of refusals is deliberate: what the caller got wrong about the
// type, then about the day, then what state the day is in. The advisory lock
// comes before the closed-period read and the insert, never after, so that
// "the period is open" is still true when the row commits.
func RecordUse(ctx context.Context, tx pgx.Tx, tenantID string, in NewRecord) (Record, error) {
	var recordMode, status string
	err := tx.QueryRow(ctx,
		`SELECT record_mode, status FROM consumption_type WHERE id = $1::uuid AND tenant_id = $2`,
		in.TypeID, tenantID).Scan(&recordMode, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("consumption: read type: %w", err)
	}
	if status != "ACTIVE" {
		return Record{}, ErrInactiveType
	}

	forSelf := in.SubjectPersonID == in.ActorPersonID
	if (recordMode == "SELF" && !forSelf) || (recordMode == "STAFF" && forSelf) {
		return Record{}, ErrRecordModeNotAllowed
	}

	day, err := parseDay(in.OccurredOn)
	if err != nil {
		return Record{}, err
	}
	// The future is a day after today where the tenant is (ADR-119). The
	// slack this check once carried, because the zone was unknown, is gone.
	var future bool
	if err := tx.QueryRow(ctx,
		`SELECT $1::date > tenant_today($2::uuid)`, day, tenantID).Scan(&future); err != nil {
		return Record{}, fmt.Errorf("consumption: compare day: %w", err)
	}
	if future {
		return Record{}, ErrFutureDate
	}

	var subjectExists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM person WHERE id = $1::uuid)`,
		in.SubjectPersonID).Scan(&subjectExists); err != nil {
		return Record{}, fmt.Errorf("consumption: read subject: %w", err)
	}
	if !subjectExists {
		return Record{}, ErrNotFound
	}

	if err := lockSubject(ctx, tx, in.TypeID, in.SubjectPersonID); err != nil {
		return Record{}, err
	}
	closed, err := dayClosed(ctx, tx, in.TypeID, in.SubjectPersonID, in.OccurredOn)
	if err != nil {
		return Record{}, err
	}
	if closed {
		return Record{}, ErrPeriodClosed
	}

	optionIDs, err := resolveOptions(ctx, tx, tenantID, in.TypeID, in.OptionCodes)
	if err != nil {
		return Record{}, err
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO consumption_record AS r
		    (id, tenant_id, consumption_type_id, subject_person_id,
		     actor_person_id, occurred_on, obligation_id, variant_code)
		VALUES (gen_random_uuid(), $1, $2::uuid, $3::uuid, $4::uuid, $5::date,
		        (SELECT o.id FROM consumption_obligation o
		          WHERE o.consumption_type_id = $2::uuid
		            AND o.subject_person_id = $3::uuid
		            AND o.effective @> (($5::date)::timestamp AT TIME ZONE 'UTC')
		          LIMIT 1),
		        $6)
		RETURNING `+recordColumns,
		tenantID, in.TypeID, in.SubjectPersonID, in.ActorPersonID, day, in.VariantCode)
	rec, err := scanRecord(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation &&
			pgErr.ConstraintName == constraintLiveRecord {
			return Record{}, ErrAlreadyRecorded
		}
		return Record{}, fmt.Errorf("consumption: insert record: %w", err)
	}
	if err := attachOptions(ctx, tx, &rec, optionIDs, in.OptionCodes); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func attachOptions(
	ctx context.Context, tx pgx.Tx, rec *Record, optionIDs []string, codes []string,
) error {
	for _, id := range optionIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO consumption_record_option
			    (consumption_record_id, consumption_option_id)
			VALUES ($1::uuid, $2::uuid)`, rec.ID, id); err != nil {
			return fmt.Errorf("consumption: attach option: %w", err)
		}
	}
	set := make(map[string]bool, len(codes))
	for _, c := range codes {
		set[c] = true
	}
	rec.OptionCodes = make([]string, 0, len(set))
	for c := range set {
		rec.OptionCodes = append(rec.OptionCodes, c)
	}
	sort.Strings(rec.OptionCodes)
	return nil
}

// Correction is what a caller supplies to supersede a record. Void says no
// consumption occurred; otherwise the variant and options replace the
// original's.
type Correction struct {
	Void        bool
	VariantCode *string
	OptionCodes []string
}

// Head is what a handler needs to know about a record BEFORE it may write a
// correction: which type to check permission against, and whether the window
// has closed, since that decides whether a second permission is needed.
type Head struct {
	TypeID string
	Late   bool
}

// PeekHead reads a record's type and lateness without changing anything.
// Late is `occurred_on + correction_window < now()`, a null window meaning
// unbounded (05.10.7). Correct recomputes it under lock; this exists only to
// let the handler ask for the right permission first.
func PeekHead(ctx context.Context, tx pgx.Tx, tenantID, recordID string) (Head, error) {
	var h Head
	err := tx.QueryRow(ctx, `
		SELECT r.consumption_type_id::text,
		       t.correction_window IS NOT NULL AND
		       tenant_day_start(r.tenant_id, r.occurred_on) + t.correction_window < now()
		  FROM consumption_record r
		  JOIN consumption_type t ON t.id = r.consumption_type_id
		 WHERE r.id = $1::uuid AND r.tenant_id = $2
	`, recordID, tenantID).Scan(&h.TypeID, &h.Late)
	if errors.Is(err, pgx.ErrNoRows) {
		return Head{}, ErrNotFound
	}
	if err != nil {
		return Head{}, fmt.Errorf("consumption: peek record: %w", err)
	}
	return h, nil
}

// Actor is who is correcting, for the audit row a late correction writes.
type Actor struct {
	PersonID    string
	PrincipalID string
	RequestID   string
}

// Correct supersedes a record (ADR-098). lateAllowed is the caller's
// `may_correct_late`, already checked: the window is recomputed here under
// lock, and a correction that is late without it is ErrWindowClosed.
func Correct(
	ctx context.Context, tx pgx.Tx, tenantID, recordID string,
	c Correction, actor Actor, lateAllowed bool,
) (Record, error) {
	var (
		typeID, subjectID, day string
		obligationID           *string
		voided, late           bool
	)
	err := tx.QueryRow(ctx, `
		SELECT r.consumption_type_id::text, r.subject_person_id::text,
		       to_char(r.occurred_on, 'YYYY-MM-DD'), r.obligation_id::text, r.voided,
		       t.correction_window IS NOT NULL AND
		       tenant_day_start(r.tenant_id, r.occurred_on) + t.correction_window < now()
		  FROM consumption_record r
		  JOIN consumption_type t ON t.id = r.consumption_type_id
		 WHERE r.id = $1::uuid AND r.tenant_id = $2
		   FOR UPDATE OF r
	`, recordID, tenantID).Scan(&typeID, &subjectID, &day, &obligationID, &voided, &late)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("consumption: read record: %w", err)
	}

	if err := lockSubject(ctx, tx, typeID, subjectID); err != nil {
		return Record{}, err
	}

	var superseded bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM consumption_record WHERE supersedes_id = $1::uuid)`,
		recordID).Scan(&superseded); err != nil {
		return Record{}, fmt.Errorf("consumption: read chain: %w", err)
	}
	if superseded {
		return Record{}, ErrAlreadyCorrected
	}
	if voided && c.Void {
		return Record{}, ErrAlreadyVoided
	}
	closed, err := dayClosed(ctx, tx, typeID, subjectID, day)
	if err != nil {
		return Record{}, err
	}
	if closed {
		return Record{}, ErrPeriodClosed
	}
	if late && !lateAllowed {
		return Record{}, ErrWindowClosed
	}

	variant, codes := c.VariantCode, c.OptionCodes
	if c.Void {
		variant, codes = nil, nil
	}
	optionIDs, err := resolveOptions(ctx, tx, tenantID, typeID, codes)
	if err != nil {
		return Record{}, err
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO consumption_record AS r
		    (id, tenant_id, consumption_type_id, subject_person_id,
		     actor_person_id, occurred_on, obligation_id, variant_code,
		     supersedes_id, voided)
		VALUES (gen_random_uuid(), $1, $2::uuid, $3::uuid, $4::uuid, $5::date,
		        $6::uuid, $7, $8::uuid, $9)
		RETURNING `+recordColumns,
		tenantID, typeID, subjectID, actor.PersonID, day, obligationID,
		variant, recordID, c.Void)
	rec, err := scanRecord(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation &&
			pgErr.ConstraintName == constraintSupersededOnce {
			return Record{}, ErrAlreadyCorrected
		}
		return Record{}, fmt.Errorf("consumption: insert correction: %w", err)
	}
	if err := attachOptions(ctx, tx, &rec, optionIDs, codes); err != nil {
		return Record{}, err
	}

	if late {
		// 05.10.7: beyond the window a correction is audited NOTABLE. The
		// row commits with the correction it describes.
		if err := audit.WriteNotable(ctx, tx, tenantID, audit.NotableEvent{
			ActorPrincipalID: actor.PrincipalID,
			Action:           "consumption_type.may_correct_late",
			ObjectType:       "consumption_record",
			ObjectID:         recordID,
			Detail:           "correction filed after the correction window",
			RequestID:        actor.RequestID,
		}); err != nil {
			return Record{}, err
		}
	}
	return rec, nil
}

// ListFilter narrows ListRecords. History includes superseded and voided
// rows; the default is live heads only.
type ListFilter struct {
	SubjectPersonID string
	From, To        string
	History         bool
}

// headOnly is the predicate for a live head: not voided, not superseded.
const headOnly = `NOT r.voided AND NOT EXISTS (
	SELECT 1 FROM consumption_record s WHERE s.supersedes_id = r.id)`

// ListRecords pages a type's records, newest day first (A3.5, ADR-104).
func ListRecords(
	ctx context.Context, tx pgx.Tx, tenantID, typeID string, f ListFilter, p page.Params,
) ([]Record, error) {
	return listRecords(ctx, tx, `r.tenant_id = $1 AND r.consumption_type_id = $2::uuid`,
		[]any{tenantID, typeID}, f, p)
}

// ListOwn pages one person's own records across every type — GET /me.
func ListOwn(
	ctx context.Context, tx pgx.Tx, tenantID, personID string, p page.Params,
) ([]Record, error) {
	return listRecords(ctx, tx, `r.tenant_id = $1 AND r.subject_person_id = $2::uuid`,
		[]any{tenantID, personID}, ListFilter{}, p)
}

func listRecords(
	ctx context.Context, tx pgx.Tx, where string, args []any, f ListFilter, p page.Params,
) ([]Record, error) {
	var afterDay, afterID string
	if len(p.Key) == 2 {
		afterDay, afterID = p.Key[0], p.Key[1]
	}
	n := len(args)
	q := `SELECT ` + recordColumns + ` FROM consumption_record r WHERE ` + where
	args = append(args, f.SubjectPersonID, f.From, f.To, afterDay, afterID, p.Fetch())
	// Each parameter is cast to text first. Without it PostgreSQL deduces the
	// type from the LAST use (`$n::uuid`), and the comparison with '' that
	// means "not supplied" then fails to parse as a uuid.
	q += fmt.Sprintf(`
		AND ($%[1]d::text = '' OR r.subject_person_id = $%[1]d::text::uuid)
		AND ($%[2]d::text = '' OR r.occurred_on >= $%[2]d::text::date)
		AND ($%[3]d::text = '' OR r.occurred_on <= $%[3]d::text::date)
		AND ($%[4]d::text = '' OR (r.occurred_on, r.id) < ($%[4]d::text::date, $%[5]d::text::uuid))`,
		n+1, n+2, n+3, n+4, n+5)
	if !f.History {
		q += ` AND ` + headOnly
	}
	q += fmt.Sprintf(` ORDER BY r.occurred_on DESC, r.id DESC LIMIT $%d`, n+6)

	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("consumption: list records: %w", err)
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("consumption: scan record: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordKey is the sort key ListRecords and ListOwn order by.
func RecordKey(r Record) []string { return []string{r.OccurredOn, r.ID} }
