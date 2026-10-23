package finance

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// CreditNote is a correction to an issued invoice (ADR-120). It reduces what
// the invoice asks for; it never edits the invoice.
type CreditNote struct {
	ID               string `json:"id"`
	Number           string `json:"number"`
	Amount           string `json:"amount"`
	Currency         string `json:"currency"`
	Reason           string `json:"reason"`
	IssuedAt         string `json:"issued_at"`
	IssuedByPersonID string `json:"issued_by_person_id"`
}

// NewCreditNote is what staff supply to correct an invoice.
type NewCreditNote struct {
	Amount   string
	Reason   string
	IssuedBy string // the issuing person
}

const creditNoteColumns = `
	id::text, number, amount::text, currency, reason,
	to_char(issued_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	issued_by_person_id::text`

// IssueCreditNote credits an ISSUED or PAID invoice by an amount no greater
// than what remains of its total after earlier credit notes, and moves the
// invoice to the state that follows from the new figures (ADR-120):
//
//	nothing left to ask for, nothing received   VOIDED
//	received covers what is now asked           PAID
//	otherwise                                   unchanged
//
// A credit note after payment can leave more received than is asked for. The
// invoice's balance is then negative, which is a refund due; refunds are
// payments in reverse and have no endpoint (11.2).
//
// The invoice row is locked first, as in RecordPayment, so a credit and a
// payment cannot each fit a balance that only one of them can have. The number
// is drawn last, as in Issue (ADR-103): every refusal above costs no number.
func IssueCreditNote(
	ctx context.Context, tx pgx.Tx, tenantID, invoiceID string, in NewCreditNote,
) (CreditNote, error) {
	if !amountPattern.MatchString(in.Amount) || in.Amount == "0" || in.Amount == "0.00" || in.Amount == "0.0" {
		return CreditNote{}, fmt.Errorf("%w: amount must be a positive decimal with at most two places", ErrInvalid)
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" || len(reason) > 500 {
		return CreditNote{}, fmt.Errorf("%w: reason is required and at most 500 characters", ErrInvalid)
	}

	var state, currency, total string
	err := tx.QueryRow(ctx, `
		SELECT state, currency, COALESCE(total_amount, 0)::text
		  FROM invoice WHERE id = $1::uuid AND tenant_id = $2 FOR UPDATE
	`, invoiceID, tenantID).Scan(&state, &currency, &total)
	if errors.Is(err, pgx.ErrNoRows) {
		return CreditNote{}, ErrNotFound
	}
	if err != nil {
		return CreditNote{}, fmt.Errorf("finance: lock invoice: %w", err)
	}
	if state != "ISSUED" && state != "PAID" {
		return CreditNote{}, ErrInvoiceState
	}

	var fits bool
	var asked, received string
	if err := tx.QueryRow(ctx, `
		SELECT $2::numeric <= $3::numeric - COALESCE(sum(c.amount), 0),
		       ($3::numeric - COALESCE(sum(c.amount), 0) - $2::numeric)::text,
		       (SELECT COALESCE(sum(amount), 0) FROM payment WHERE invoice_id = $1::uuid)::text
		  FROM credit_note c WHERE c.invoice_id = $1::uuid
	`, invoiceID, in.Amount, total).Scan(&fits, &asked, &received); err != nil {
		return CreditNote{}, fmt.Errorf("finance: read credited total: %w", err)
	}
	if !fits {
		return CreditNote{}, ErrExceedsInvoice
	}

	var series string
	if err := tx.QueryRow(ctx,
		`SELECT financial_year(tenant_today($1::uuid))`, tenantID).Scan(&series); err != nil {
		return CreditNote{}, fmt.Errorf("finance: financial year: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO invoice_number_series (tenant_id, series, kind, prefix)
		VALUES ($1, $2, 'CREDIT_NOTE', 'CN')
		ON CONFLICT (tenant_id, series, kind) DO NOTHING
	`, tenantID, series); err != nil {
		return CreditNote{}, fmt.Errorf("finance: ensure series: %w", err)
	}
	var number string
	if err := tx.QueryRow(ctx, `
		UPDATE invoice_number_series
		   SET next_value = next_value + 1
		 WHERE tenant_id = $1 AND series = $2 AND kind = 'CREDIT_NOTE'
		RETURNING prefix || '/' || series || '/' || lpad((next_value - 1)::text, 6, '0')
	`, tenantID, series).Scan(&number); err != nil {
		return CreditNote{}, fmt.Errorf("finance: allocate number: %w", err)
	}

	var cn CreditNote
	if err := tx.QueryRow(ctx, `
		INSERT INTO credit_note (tenant_id, invoice_id, number, amount, currency, reason, issued_by_person_id)
		VALUES ($1, $2::uuid, $3, $4::numeric, $5, $6, $7::uuid)
		RETURNING `+creditNoteColumns,
		tenantID, invoiceID, number, in.Amount, currency, reason, in.IssuedBy).
		Scan(&cn.ID, &cn.Number, &cn.Amount, &cn.Currency, &cn.Reason, &cn.IssuedAt, &cn.IssuedByPersonID); err != nil {
		return CreditNote{}, fmt.Errorf("finance: insert credit note: %w", err)
	}

	// asked is what the invoice now asks for; received is what it has been paid.
	var next string
	if err := tx.QueryRow(ctx, `
		SELECT CASE WHEN $1::numeric = 0 AND $2::numeric = 0 THEN 'VOIDED'
		            WHEN $2::numeric >= $1::numeric THEN 'PAID'
		            ELSE $3 END`, asked, received, state).Scan(&next); err != nil {
		return CreditNote{}, fmt.Errorf("finance: next state: %w", err)
	}
	if next != state {
		if _, err := tx.Exec(ctx,
			`UPDATE invoice SET state = $2 WHERE id = $1::uuid`, invoiceID, next); err != nil {
			return CreditNote{}, fmt.Errorf("finance: move invoice to %s: %w", next, err)
		}
	}
	return cn, nil
}
