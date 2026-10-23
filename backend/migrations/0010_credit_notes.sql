-- 0010_credit_notes — how an issued invoice is corrected.
--
-- Source of truth: docs/system-design/A2_data_model.md A2.8, 05.8.5, ADR-120.
--
-- K1  An issued invoice is immutable and "corrections are credit notes"
--     (A2.8), and no credit note existed. A wrong invoice had no correction
--     path, and a correction inside a closed period is refused by design.
--     A credit note is its own document with its own gapless number, drawn
--     from `invoice_number_series` under kind CREDIT_NOTE, so it never shares a
--     counter with the invoices it corrects.
--
-- K2  An invoice credited in full with nothing received has no amount left to
--     collect and was never paid: 05.8.5's VOIDED. The state CHECK of 0007 gains
--     it; nothing else gains it, and the transition is made only by a credit
--     note.
--
-- Forward-only. No Down section — see 0001 and 07.4.

-- +goose Up

CREATE TABLE credit_note (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenant,
    invoice_id          uuid NOT NULL REFERENCES invoice,
    number              text NOT NULL,
    amount              numeric(14,2) NOT NULL CHECK (amount > 0),
    currency            char(3) NOT NULL,
    reason              text NOT NULL CHECK (length(reason) BETWEEN 1 AND 500),
    issued_at           timestamptz NOT NULL DEFAULT now(),
    issued_by_person_id uuid NOT NULL REFERENCES person,
    UNIQUE (tenant_id, number)
);
CREATE INDEX credit_note_invoice ON credit_note (invoice_id);

ALTER TABLE invoice DROP CONSTRAINT invoice_state_known;
ALTER TABLE invoice
    ADD CONSTRAINT invoice_state_known
    CHECK (state IN ('DRAFT', 'ISSUED', 'PAID', 'VOIDED'));

ALTER TABLE credit_note ENABLE ROW LEVEL SECURITY;
ALTER TABLE credit_note FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON credit_note
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
