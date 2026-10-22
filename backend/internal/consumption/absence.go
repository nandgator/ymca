package consumption

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PaymentReliefDays is the length at which an absence stops being billed:
// the reference tenant's SHORT/LONG leave threshold (05.10.6). It is the
// platform default until 05.7's policy engine can carry
// ABSENCE_PAYMENT_RELIEF per tenant (A3.10, 11.2). The flags it decides are
// snapshotted onto the row, so changing this rebills nothing already declared
// (ADR-099).
const PaymentReliefDays = 7

// Absence is a declared future non-use (05.10.6).
type Absence struct {
	ID                 string `json:"id"`
	SubjectPersonID    string `json:"subject_person_id"`
	From               string `json:"from"`
	To                 string `json:"to"`
	DeclaredByPersonID string `json:"declared_by_person_id"`
	RelievesRecording  bool   `json:"relieves_recording"`
	RelievesPayment    bool   `json:"relieves_payment"`
	Withdrawn          bool   `json:"withdrawn"`
}

// NewAbsence is what a caller supplies. From and To are inclusive days.
type NewAbsence struct {
	SubjectPersonID    string
	DeclaredByPersonID string
	From, To           string
}

// DeclareAbsence records an absence and snapshots its relief flags.
func DeclareAbsence(ctx context.Context, tx pgx.Tx, tenantID string, in NewAbsence) (Absence, error) {
	lo, hi, err := dayRange(in.From, &in.To)
	if err != nil {
		return Absence{}, err
	}
	days := int(hi.Sub(lo).Hours() / 24)

	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM person WHERE id = $1::uuid)`,
		in.SubjectPersonID).Scan(&exists); err != nil {
		return Absence{}, fmt.Errorf("consumption: read subject: %w", err)
	}
	if !exists {
		return Absence{}, ErrNotFound
	}

	a := Absence{
		SubjectPersonID:    in.SubjectPersonID,
		From:               in.From,
		To:                 in.To,
		DeclaredByPersonID: in.DeclaredByPersonID,
		RelievesRecording:  true,
		RelievesPayment:    days >= PaymentReliefDays,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO expected_absence
		    (id, tenant_id, subject_person_id, period, declared_by_person_id,
		     relieves_recording, relieves_payment)
		VALUES (gen_random_uuid(), $1, $2::uuid,
		        tstzrange(($3::date)::timestamp AT TIME ZONE 'UTC',
		                  ($4::date)::timestamp AT TIME ZONE 'UTC'),
		        $5::uuid, $6, $7)
		RETURNING id::text
	`, tenantID, in.SubjectPersonID, lo, hi, in.DeclaredByPersonID,
		a.RelievesRecording, a.RelievesPayment).Scan(&a.ID)
	if err != nil {
		return Absence{}, fmt.Errorf("consumption: declare absence: %w", err)
	}
	return a, nil
}

// WithdrawAbsence sets withdrawn_at. Only the declarer, or an administrator,
// may withdraw; anyone else is told the absence does not exist (A3.3).
// Withdrawing twice is not an error: the second call finds what the first
// left and says so.
func WithdrawAbsence(
	ctx context.Context, tx pgx.Tx, tenantID, absenceID, actorPersonID string, admin bool,
) (Absence, error) {
	var a Absence
	var withdrawnAt *string
	err := tx.QueryRow(ctx, `
		SELECT id::text, subject_person_id::text,
		       to_char(lower(period) AT TIME ZONE 'UTC', 'YYYY-MM-DD'),
		       to_char((upper(period) AT TIME ZONE 'UTC') - interval '1 day', 'YYYY-MM-DD'),
		       declared_by_person_id::text, relieves_recording, relieves_payment,
		       withdrawn_at::text
		  FROM expected_absence
		 WHERE id = $1::uuid AND tenant_id = $2
		   FOR UPDATE
	`, absenceID, tenantID).Scan(&a.ID, &a.SubjectPersonID, &a.From, &a.To,
		&a.DeclaredByPersonID, &a.RelievesRecording, &a.RelievesPayment, &withdrawnAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Absence{}, ErrNotFound
	}
	if err != nil {
		return Absence{}, fmt.Errorf("consumption: read absence: %w", err)
	}
	if !admin && a.DeclaredByPersonID != actorPersonID {
		return Absence{}, ErrNotFound
	}
	if withdrawnAt == nil {
		if _, err := tx.Exec(ctx,
			`UPDATE expected_absence SET withdrawn_at = now() WHERE id = $1::uuid`,
			absenceID); err != nil {
			return Absence{}, fmt.Errorf("consumption: withdraw absence: %w", err)
		}
	}
	a.Withdrawn = true
	return a, nil
}

// AbsenceDeclarer returns the person who declared an absence, so a handler
// can tell whether the caller is that person before deciding whether to ask
// the graph if they are an administrator instead.
func AbsenceDeclarer(ctx context.Context, tx pgx.Tx, tenantID, absenceID string) (string, error) {
	var declarer string
	err := tx.QueryRow(ctx,
		`SELECT declared_by_person_id::text FROM expected_absence
		  WHERE id = $1::uuid AND tenant_id = $2`,
		absenceID, tenantID).Scan(&declarer)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("consumption: read absence: %w", err)
	}
	return declarer, nil
}
