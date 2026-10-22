-- 0007_finance — what composing, issuing and paying an invoice need that A2.8
-- did not give them.
--
-- Source of truth: docs/system-design/A2_data_model.md A2.8, A3.11, 05.8.
--
-- F1  An invoice has no creation time, so a list of them had no order to
--     page by (ADR-104 pages by keyset). `created_at` is that order.
--
-- F2  Close composes onto "the payer's open draft", and two closes running
--     together must not each start one. A partial UNIQUE index makes that a
--     rule the database holds: one DRAFT per payer per currency per tenant.
--     Its name is a contract with Go (internal/finance).
--
-- F3  `party` is generic (ADR-082) and carries no uniqueness, so creating one
--     "on first need" would make a second party for the same person under
--     concurrency. One per person and one per organization.
--
-- F4  State and amount constraints A2.8 states in prose only.
--
-- Forward-only. No Down section — see 0001 and 07.4.

-- +goose Up

ALTER TABLE invoice ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX invoice_tenant_created ON invoice (tenant_id, created_at DESC, id DESC);

CREATE UNIQUE INDEX one_draft_per_payer_currency
    ON invoice (tenant_id, payer_party_id, currency) WHERE state = 'DRAFT';

CREATE UNIQUE INDEX party_one_per_person
    ON party (person_id) WHERE person_id IS NOT NULL;
CREATE UNIQUE INDEX party_one_per_organization
    ON party (organization_id) WHERE organization_id IS NOT NULL;

ALTER TABLE invoice
    ADD CONSTRAINT invoice_state_known CHECK (state IN ('DRAFT', 'ISSUED', 'PAID')),
    ADD CONSTRAINT invoice_issued_has_number
        CHECK (state = 'DRAFT' OR (number IS NOT NULL AND total_amount IS NOT NULL));
ALTER TABLE payment
    ADD CONSTRAINT payment_amount_positive CHECK (amount > 0);

-- The two lookups close and the payer's own list make.
CREATE INDEX charge_invoice ON charge (invoice_id);
CREATE INDEX charge_component_charge ON charge_component (charge_id);
CREATE INDEX payment_invoice ON payment (invoice_id);
