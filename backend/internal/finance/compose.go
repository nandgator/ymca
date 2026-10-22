package finance

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/outbox"
)

// OptionLine is one option used in the period, as Consumption reports it.
type OptionLine struct {
	Code       string
	Name       string
	ChargeKind string
	Amount     string
	Currency   string
	Count      int
}

// ConsumptionClose is Consumption's published ConsumptionPeriodClosed
// (05.10.8), restated in this package's own terms so that neither imports the
// other.
type ConsumptionClose struct {
	CloseID         string
	TypeID          string
	TypeName        string
	SubjectPersonID string
	From, To        string // inclusive days
	ObligatedUnits  int
	Options         []OptionLine
}

type component struct {
	kind     string
	amount   string
	currency string
}

// ComposeConsumption turns a closed period into one charge on the payer's
// open draft invoice, and returns that invoice's id — or nil when there is
// nothing to bill (05.10.8, A3.11).
//
// It runs in the closing transaction. A person with units and no rate rolls
// the whole close back; see ErrNoRate.
func ComposeConsumption(
	ctx context.Context, tx pgx.Tx, tenantID string, c ConsumptionClose,
) (*string, error) {
	var parts []component

	if c.ObligatedUnits > 0 {
		var amount, currency string
		err := tx.QueryRow(ctx, `
			SELECT ($4::int * amount)::text, currency
			  FROM consumption_rate
			 WHERE tenant_id = $1 AND consumption_type_id = $2::uuid
			   AND valid_during @> $3::date
		`, tenantID, c.TypeID, c.From, c.ObligatedUnits).Scan(&amount, &currency)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoRate
		}
		if err != nil {
			return nil, fmt.Errorf("finance: read rate: %w", err)
		}
		parts = append(parts, component{"USAGE_FEE", amount, currency})
	}
	for _, o := range c.Options {
		var amount string
		if err := tx.QueryRow(ctx, `SELECT ($1::int * $2::numeric)::text`,
			o.Count, o.Amount).Scan(&amount); err != nil {
			return nil, fmt.Errorf("finance: price option: %w", err)
		}
		parts = append(parts, component{o.ChargeKind, amount, o.Currency})
	}
	if len(parts) == 0 {
		return nil, nil
	}
	currency := parts[0].currency
	for _, p := range parts {
		if p.currency != currency {
			return nil, ErrCurrencyMismatch
		}
	}

	payer, err := personParty(ctx, tx, c.SubjectPersonID)
	if err != nil {
		return nil, err
	}
	payee, err := organizationParty(ctx, tx, tenantID)
	if err != nil {
		return nil, err
	}
	invoiceID, err := openDraft(ctx, tx, tenantID, payer, payee, currency, c.SubjectPersonID)
	if err != nil {
		return nil, err
	}

	var chargeID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO charge (id, invoice_id, source_type, source_id, description)
		VALUES (gen_random_uuid(), $1::uuid, 'CONSUMPTION', $2::uuid, $3)
		RETURNING id::text
	`, invoiceID, c.CloseID,
		fmt.Sprintf("%s, %s to %s", c.TypeName, c.From, c.To)).Scan(&chargeID); err != nil {
		return nil, fmt.Errorf("finance: insert charge: %w", err)
	}
	for _, p := range parts {
		if _, err := tx.Exec(ctx, `
			INSERT INTO charge_component (id, charge_id, kind, recurrence, amount, currency)
			VALUES (gen_random_uuid(), $1::uuid, $2, 'ONE_TIME', $3::numeric, $4)
		`, chargeID, p.kind, p.amount, p.currency); err != nil {
			return nil, fmt.Errorf("finance: insert component: %w", err)
		}
	}
	return &invoiceID, nil
}

// personParty returns the person's party, creating it on first need. A person
// is a party only because something needs to be billed to them.
func personParty(ctx context.Context, tx pgx.Tx, personID string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		WITH ins AS (
		    INSERT INTO party (id, kind, person_id)
		    VALUES (gen_random_uuid(), 'PERSON', $1::uuid)
		    ON CONFLICT (person_id) WHERE person_id IS NOT NULL DO NOTHING
		    RETURNING id)
		SELECT id::text FROM ins
		UNION ALL
		SELECT id::text FROM party WHERE person_id = $1::uuid
		LIMIT 1
	`, personID).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("finance: person party: %w", err)
	}
	return id, nil
}

// organizationParty is the tenant as payee. Its organization_id is the
// tenant id: A2.8 leaves organization_id unreferenced, and the tenant is the
// only organization this slice bills on behalf of.
func organizationParty(ctx context.Context, tx pgx.Tx, tenantID string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		WITH ins AS (
		    INSERT INTO party (id, kind, organization_id)
		    VALUES (gen_random_uuid(), 'ORGANIZATION', $1::uuid)
		    ON CONFLICT (organization_id) WHERE organization_id IS NOT NULL DO NOTHING
		    RETURNING id)
		SELECT id::text FROM ins
		UNION ALL
		SELECT id::text FROM party WHERE organization_id = $1::uuid
		LIMIT 1
	`, tenantID).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("finance: organization party: %w", err)
	}
	return id, nil
}

// openDraft finds the payer's DRAFT invoice in this currency, locked, or
// starts one.
//
// The partial unique index of migration 0007 is what makes "the" draft
// well-defined under concurrency, and the lock is what keeps it a draft: an
// issue running at the same moment locks the same row, so a charge is never
// added to an invoice that has just stopped being editable (A2.8: issued
// invoices are immutable). Under READ COMMITTED the locked read re-checks its
// WHERE clause against the row the issue committed, so it no longer matches
// and the loop starts a fresh draft instead.
//
// A new draft publishes its two authorization facts. They are grants, so they
// may lag a dispatch interval (8.3); a payer cannot see a draft anyway.
func openDraft(
	ctx context.Context, tx pgx.Tx, tenantID, payer, payee, currency, personID string,
) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		var id string
		err := tx.QueryRow(ctx, `
			SELECT id::text FROM invoice
			 WHERE tenant_id = $1 AND payer_party_id = $2::uuid
			   AND currency = $3 AND state = 'DRAFT'
			   FOR UPDATE
		`, tenantID, payer, currency).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("finance: find draft: %w", err)
		}

		err = tx.QueryRow(ctx, `
			INSERT INTO invoice
			    (id, tenant_id, payer_party_id, payee_party_id, currency, state)
			VALUES (gen_random_uuid(), $1, $2::uuid, $3::uuid, $4, 'DRAFT')
			ON CONFLICT (tenant_id, payer_party_id, currency) WHERE state = 'DRAFT'
			DO NOTHING
			RETURNING id::text
		`, tenantID, payer, payee, currency).Scan(&id)
		if err == nil {
			return id, publishInvoice(ctx, tx, tenantID, id, personID)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("finance: start draft: %w", err)
		}
		// Somebody else's draft won the index; read it on the next pass.
	}
	return "", errors.New("finance: could not settle on a draft invoice")
}

func publishInvoice(ctx context.Context, tx pgx.Tx, tenantID, invoiceID, personID string) error {
	if err := outbox.Enqueue(ctx, tx, outbox.Fact{
		AggregateType: "invoice",
		AggregateID:   invoiceID,
		EventType:     EventInvoiceCreated,
		Payload:       map[string]string{"tenant_id": tenantID, "invoice_id": invoiceID},
		Fence: outbox.Fence{
			Subject:  "tenant:" + tenantID,
			Relation: "tenant",
			Object:   "invoice:" + invoiceID,
		},
	}); err != nil {
		return err
	}
	return outbox.Enqueue(ctx, tx, outbox.Fact{
		AggregateType: "invoice",
		AggregateID:   invoiceID,
		EventType:     EventInvoicePayerSet,
		Payload:       map[string]string{"invoice_id": invoiceID, "person_id": personID},
		Fence: outbox.Fence{
			Subject:  "person:" + personID,
			Relation: "payer",
			Object:   "invoice:" + invoiceID,
		},
	})
}
