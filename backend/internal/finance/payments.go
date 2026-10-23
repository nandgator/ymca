package finance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// OfflineMethods are the payment methods a staff member can record by hand.
// CARD and PROVIDER_HOSTED need an adapter that does not exist (05.8.9), and
// accepting them here would record a card payment nobody processed.
var OfflineMethods = map[string]bool{
	"CASH": true, "CHEQUE": true, "BANK_TRANSFER": true, "UPI": true,
}

// NewPayment is what staff supply to record money received.
type NewPayment struct {
	Method            string
	Amount            string
	Currency          string
	ProviderReference *string
	ReceivedAt        *time.Time
	RecordedBy        string // the recording person; required for offline methods
}

// RecordPayment records a payment against an ISSUED invoice and, when it
// brings the balance to zero, marks the invoice PAID. The balance is what the
// invoice asks for after its credit notes (ADR-120).
//
// The invoice row is locked, so two payments cannot both fit the same
// balance: the second reads the first's sum.
func RecordPayment(
	ctx context.Context, tx pgx.Tx, tenantID, invoiceID string, in NewPayment,
) (Payment, error) {
	if !OfflineMethods[in.Method] {
		return Payment{}, fmt.Errorf("%w: method must be one of CASH, CHEQUE, BANK_TRANSFER, UPI", ErrInvalid)
	}
	if !amountPattern.MatchString(in.Amount) || in.Amount == "0" || in.Amount == "0.00" || in.Amount == "0.0" {
		return Payment{}, fmt.Errorf("%w: amount must be a positive decimal with at most two places", ErrInvalid)
	}
	if !currencyPattern.MatchString(in.Currency) {
		return Payment{}, fmt.Errorf("%w: currency must be an ISO 4217 code", ErrInvalid)
	}

	var (
		state, currency, total string
		payerParty             string
	)
	err := tx.QueryRow(ctx, `
		SELECT state, currency, COALESCE(total_amount, 0)::text, payer_party_id::text
		  FROM invoice WHERE id = $1::uuid AND tenant_id = $2 FOR UPDATE
	`, invoiceID, tenantID).Scan(&state, &currency, &total, &payerParty)
	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, ErrNotFound
	}
	if err != nil {
		return Payment{}, fmt.Errorf("finance: lock invoice: %w", err)
	}
	if state != "ISSUED" {
		return Payment{}, ErrInvoiceState
	}
	if in.Currency != currency {
		return Payment{}, ErrCurrencyMismatch
	}

	var fits bool
	var balance string
	if err := tx.QueryRow(ctx, `
		SELECT $2::numeric <= asked - COALESCE(sum(p.amount), 0),
		       (asked - COALESCE(sum(p.amount), 0) - $2::numeric)::text
		  FROM (SELECT $3::numeric - COALESCE((SELECT sum(amount) FROM credit_note
		                                        WHERE invoice_id = $1::uuid), 0) AS asked) a
		  LEFT JOIN payment p ON p.invoice_id = $1::uuid
		 GROUP BY asked
	`, invoiceID, in.Amount, total).Scan(&fits, &balance); err != nil {
		return Payment{}, fmt.Errorf("finance: read balance: %w", err)
	}
	if !fits {
		return Payment{}, ErrExceedsBalance
	}

	var pm Payment
	err = tx.QueryRow(ctx, `
		INSERT INTO payment
		    (id, tenant_id, invoice_id, party_id, method, amount, currency, state,
		     provider_reference, received_at, recorded_by)
		VALUES (gen_random_uuid(), $1, $2::uuid, $3::uuid, $4, $5::numeric, $6,
		        'COMPLETED', $7, COALESCE($8::timestamptz, now()), $9::uuid)
		RETURNING id::text, method, amount::text, currency, state, provider_reference,
		          to_char(received_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		          recorded_by::text
	`, tenantID, invoiceID, payerParty, in.Method, in.Amount, in.Currency,
		in.ProviderReference, in.ReceivedAt, in.RecordedBy).
		Scan(&pm.ID, &pm.Method, &pm.Amount, &pm.Currency, &pm.State,
			&pm.ProviderReference, &pm.ReceivedAt, &pm.RecordedBy)
	if err != nil {
		return Payment{}, fmt.Errorf("finance: record payment: %w", err)
	}

	if balance == "0.00" || balance == "0" {
		if _, err := tx.Exec(ctx,
			`UPDATE invoice SET state = 'PAID' WHERE id = $1::uuid`, invoiceID); err != nil {
			return Payment{}, fmt.Errorf("finance: mark paid: %w", err)
		}
	}
	return pm, nil
}
