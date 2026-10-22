package finance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/page"
)

// Component is one signed amount on a charge.
type Component struct {
	Kind     string `json:"kind"`
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// Charge is one thing being billed for (ADR-100's CONSUMPTION among them).
type Charge struct {
	ID          string      `json:"id"`
	SourceType  string      `json:"source_type"`
	SourceID    *string     `json:"source_id"`
	Description *string     `json:"description"`
	Components  []Component `json:"components"`
}

// Payment is money received against an invoice.
type Payment struct {
	ID                string  `json:"id"`
	Method            string  `json:"method"`
	Amount            string  `json:"amount"`
	Currency          string  `json:"currency"`
	State             string  `json:"state"`
	ProviderReference *string `json:"provider_reference"`
	ReceivedAt        string  `json:"received_at"`
	RecordedBy        *string `json:"recorded_by"`
}

// Invoice is a bill. Charges, Payments and Balance appear on the detail read
// only.
type Invoice struct {
	ID            string    `json:"id"`
	Number        *string   `json:"number"`
	State         string    `json:"state"`
	Currency      string    `json:"currency"`
	PayerPersonID *string   `json:"payer_person_id"`
	IssuedAt      *string   `json:"issued_at"`
	DueAt         *string   `json:"due_at"`
	TotalAmount   *string   `json:"total_amount"`
	CreatedAt     string    `json:"created_at"`
	Balance       *string   `json:"balance,omitempty"`
	Charges       []Charge  `json:"charges,omitempty"`
	Payments      []Payment `json:"payments,omitempty"`
}

const invoiceColumns = `
	i.id::text, i.number, i.state, i.currency, p.person_id::text,
	to_char(i.issued_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	to_char(i.due_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	i.total_amount::text,
	to_char(i.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US')`

func scanInvoice(row pgx.Row) (Invoice, error) {
	var inv Invoice
	err := row.Scan(&inv.ID, &inv.Number, &inv.State, &inv.Currency,
		&inv.PayerPersonID, &inv.IssuedAt, &inv.DueAt, &inv.TotalAmount, &inv.CreatedAt)
	return inv, err
}

// Get reads one invoice with its charges, payments and balance.
func Get(ctx context.Context, tx pgx.Tx, tenantID, invoiceID string) (Invoice, error) {
	inv, err := scanInvoice(tx.QueryRow(ctx, `
		SELECT `+invoiceColumns+`
		  FROM invoice i JOIN party p ON p.id = i.payer_party_id
		 WHERE i.id = $1::uuid AND i.tenant_id = $2
	`, invoiceID, tenantID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, ErrNotFound
	}
	if err != nil {
		return Invoice{}, fmt.Errorf("finance: read invoice: %w", err)
	}

	crow, err := tx.Query(ctx, `
		SELECT c.id::text, c.source_type, c.source_id::text, c.description,
		       cc.kind, cc.amount::text, cc.currency
		  FROM charge c JOIN charge_component cc ON cc.charge_id = c.id
		 WHERE c.invoice_id = $1::uuid
		 ORDER BY c.id, cc.kind, cc.id
	`, invoiceID)
	if err != nil {
		return Invoice{}, fmt.Errorf("finance: read charges: %w", err)
	}
	inv.Charges = []Charge{}
	for crow.Next() {
		var (
			ch Charge
			co Component
		)
		if err := crow.Scan(&ch.ID, &ch.SourceType, &ch.SourceID, &ch.Description,
			&co.Kind, &co.Amount, &co.Currency); err != nil {
			crow.Close()
			return Invoice{}, err
		}
		if n := len(inv.Charges); n > 0 && inv.Charges[n-1].ID == ch.ID {
			inv.Charges[n-1].Components = append(inv.Charges[n-1].Components, co)
		} else {
			ch.Components = []Component{co}
			inv.Charges = append(inv.Charges, ch)
		}
	}
	crow.Close()
	if err := crow.Err(); err != nil {
		return Invoice{}, err
	}

	prow, err := tx.Query(ctx, `
		SELECT id::text, method, amount::text, currency, state, provider_reference,
		       to_char(received_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		       recorded_by::text
		  FROM payment WHERE invoice_id = $1::uuid
		 ORDER BY received_at, id
	`, invoiceID)
	if err != nil {
		return Invoice{}, fmt.Errorf("finance: read payments: %w", err)
	}
	inv.Payments = []Payment{}
	for prow.Next() {
		var pm Payment
		if err := prow.Scan(&pm.ID, &pm.Method, &pm.Amount, &pm.Currency, &pm.State,
			&pm.ProviderReference, &pm.ReceivedAt, &pm.RecordedBy); err != nil {
			prow.Close()
			return Invoice{}, err
		}
		inv.Payments = append(inv.Payments, pm)
	}
	prow.Close()
	if err := prow.Err(); err != nil {
		return Invoice{}, err
	}

	if inv.TotalAmount != nil {
		var balance string
		if err := tx.QueryRow(ctx, `
			SELECT ($2::numeric - COALESCE(sum(amount), 0))::text
			  FROM payment WHERE invoice_id = $1::uuid`,
			invoiceID, *inv.TotalAmount).Scan(&balance); err != nil {
			return Invoice{}, fmt.Errorf("finance: balance: %w", err)
		}
		inv.Balance = &balance
	}
	return inv, nil
}

// List pages every invoice in the tenant, newest first (ADR-104).
func List(ctx context.Context, tx pgx.Tx, tenantID string, p page.Params) ([]Invoice, error) {
	return list(ctx, tx, `i.tenant_id = $1`, []any{tenantID}, p)
}

// ListOwn pages the invoices billed to one person, drafts excluded: a payer
// is not shown a bill nobody has issued (A3.11).
func ListOwn(
	ctx context.Context, tx pgx.Tx, tenantID, personID string, p page.Params,
) ([]Invoice, error) {
	return list(ctx, tx, `i.tenant_id = $1 AND p.person_id = $2::uuid AND i.state <> 'DRAFT'`,
		[]any{tenantID, personID}, p)
}

func list(
	ctx context.Context, tx pgx.Tx, where string, args []any, p page.Params,
) ([]Invoice, error) {
	var afterAt, afterID string
	if len(p.Key) == 2 {
		afterAt, afterID = p.Key[0], p.Key[1]
	}
	n := len(args)
	args = append(args, afterAt, afterID, p.Fetch())
	q := fmt.Sprintf(`
		SELECT `+invoiceColumns+`
		  FROM invoice i JOIN party p ON p.id = i.payer_party_id
		 WHERE `+where+`
		   AND ($%[1]d::text = '' OR (i.created_at AT TIME ZONE 'UTC', i.id) < ($%[1]d::text::timestamp, $%[2]d::text::uuid))
		 ORDER BY i.created_at DESC, i.id DESC
		 LIMIT $%[3]d`, n+1, n+2, n+3)
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("finance: list invoices: %w", err)
	}
	defer rows.Close()
	var out []Invoice
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, fmt.Errorf("finance: scan invoice: %w", err)
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// InvoiceKey is the sort key List and ListOwn order by.
func InvoiceKey(i Invoice) []string { return []string{i.CreatedAt, i.ID} }

// Issue allocates the invoice's number and makes it immutable (ADR-103).
//
// The order inside is the mechanism. The invoice row is locked first, so two
// issues of one invoice serialise and the loser finds it ISSUED. The total is
// computed before the number is drawn, and the number is drawn LAST — the
// UPDATE of the counter is the final write before the invoice row's own — so
// that the counter row's lock is held for as little of the transaction as
// possible, and so that any failure before it costs no number. A sequence
// could not do this: nextval is non-transactional, and a rollback would leave
// a gap a tax authority asks about.
func Issue(
	ctx context.Context, tx pgx.Tx, tenantID, invoiceID string, dueAt *time.Time,
) (Invoice, error) {
	var state string
	err := tx.QueryRow(ctx,
		`SELECT state FROM invoice WHERE id = $1::uuid AND tenant_id = $2 FOR UPDATE`,
		invoiceID, tenantID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, ErrNotFound
	}
	if err != nil {
		return Invoice{}, fmt.Errorf("finance: lock invoice: %w", err)
	}
	if state != "DRAFT" {
		return Invoice{}, ErrInvoiceState
	}

	var total string
	var components int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(sum(cc.amount), 0)::text, count(cc.id)::int
		  FROM charge c JOIN charge_component cc ON cc.charge_id = c.id
		 WHERE c.invoice_id = $1::uuid
	`, invoiceID).Scan(&total, &components); err != nil {
		return Invoice{}, fmt.Errorf("finance: total invoice: %w", err)
	}
	if components == 0 {
		return Invoice{}, ErrInvoiceEmpty
	}

	// The financial year starts in April for every tenant (A3.11, 11.2).
	var series string
	if err := tx.QueryRow(ctx, `
		WITH t AS (SELECT now() AT TIME ZONE 'UTC' AS at),
		     fy AS (SELECT CASE WHEN extract(month FROM at) >= 4
		                        THEN extract(year FROM at)::int
		                        ELSE extract(year FROM at)::int - 1 END AS y FROM t)
		SELECT y::text || '-' || lpad(((y + 1) % 100)::text, 2, '0') FROM fy
	`).Scan(&series); err != nil {
		return Invoice{}, fmt.Errorf("finance: financial year: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO invoice_number_series (tenant_id, series, kind, prefix)
		VALUES ($1, $2, 'LIVE', 'INV')
		ON CONFLICT (tenant_id, series, kind) DO NOTHING
	`, tenantID, series); err != nil {
		return Invoice{}, fmt.Errorf("finance: ensure series: %w", err)
	}

	// ADR-103: as late as possible. Everything above can fail and cost no
	// number; nothing below is a decision.
	var number string
	if err := tx.QueryRow(ctx, `
		UPDATE invoice_number_series
		   SET next_value = next_value + 1
		 WHERE tenant_id = $1 AND series = $2 AND kind = 'LIVE'
		RETURNING prefix || '/' || series || '/' || lpad((next_value - 1)::text, 6, '0')
	`, tenantID, series).Scan(&number); err != nil {
		return Invoice{}, fmt.Errorf("finance: allocate number: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE invoice
		   SET state = 'ISSUED', number = $2, total_amount = $3::numeric,
		       issued_at = now(), due_at = COALESCE($4::timestamptz, now() + interval '30 days')
		 WHERE id = $1::uuid
	`, invoiceID, number, total, dueAt); err != nil {
		return Invoice{}, fmt.Errorf("finance: issue invoice: %w", err)
	}
	return Get(ctx, tx, tenantID, invoiceID)
}
