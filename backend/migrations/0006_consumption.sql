-- 0006_consumption — what records, corrections, options, obligations,
-- absences and period close need that A2.9 did not give them.
--
-- Source of truth: docs/system-design/A2_data_model.md A2.9, A3.10, 05.10.
--
-- Every finding here came from building the endpoints, as in 0003 and 0005:
--
-- C1  A correction that withdraws a record cannot be a record. A2.9 has
--     `supersedes_id` and nothing that says "no consumption occurred", so a
--     mistaken record could only ever be changed, never taken back. `voided`
--     is a correcting record that says so; ADR-098's rule that nothing is
--     edited holds, and the chain still ends in a row.
--
-- C2  A record may be superseded once. A2.9's partial unique index covers
--     originals only (`WHERE supersedes_id IS NULL`), so two corrections of
--     the same record would fork the chain and leave two "live" heads.
--
-- C3  Period close is exactly-once per person-period (05.10.12's open item),
--     and an exclusion constraint is the mechanism: a second close of an
--     overlapping period raises 23P01, which no ordering of requests can
--     avoid. It also gives records a closed period to be refused against.
--
-- C4  Finance prices a unit of consumption (05.10.8: "pricing and tax are
--     Finance's") and nothing stored a price. `consumption_rate` is
--     Finance's table; consumption never reads it.
--
-- Forward-only. No Down section — see 0001 and 07.4.

-- +goose Up

-- C1
ALTER TABLE consumption_record
    ADD COLUMN voided boolean NOT NULL DEFAULT false;
ALTER TABLE consumption_record
    ADD CONSTRAINT consumption_record_void_supersedes
    CHECK (NOT voided OR supersedes_id IS NOT NULL);

-- C2. The constraint NAME is a contract with Go (internal/consumption),
-- exactly as migration 0005's are: a rename compiles and turns a 409 into
-- a 500.
CREATE UNIQUE INDEX consumption_record_superseded_once
    ON consumption_record (supersedes_id)
    WHERE supersedes_id IS NOT NULL;

-- C3
CREATE TABLE consumption_period_close (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenant,
    consumption_type_id uuid NOT NULL REFERENCES consumption_type,
    subject_person_id   uuid NOT NULL REFERENCES person,
    -- Whole days, [from, until): dates are UTC midnights (A3.10).
    period              daterange NOT NULL,
    closed_by_person_id uuid NOT NULL REFERENCES person,
    closed_at           timestamptz NOT NULL DEFAULT now(),
    -- The published fact (05.10.8), kept as the row's own account of what
    -- was handed to Finance. It is not recomputed on read.
    obligated_units     int NOT NULL CHECK (obligated_units >= 0),
    summary             jsonb NOT NULL,
    CONSTRAINT consumption_period_close_once
        EXCLUDE USING gist (
            consumption_type_id WITH =,
            subject_person_id   WITH =,
            period              WITH &&
        )
);

-- C4
CREATE TABLE consumption_rate (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenant,
    consumption_type_id uuid NOT NULL REFERENCES consumption_type,
    amount              numeric(14,2) NOT NULL CHECK (amount >= 0),
    currency            char(3) NOT NULL,
    valid_during        daterange NOT NULL,
    CONSTRAINT consumption_rate_no_overlap
        EXCLUDE USING gist (
            consumption_type_id WITH =,
            valid_during        WITH &&
        )
);

-- ADR-110's three places, second of three: the seed. The first is A1.2's
-- type restriction, the third an assertion.
INSERT INTO grantable_permission (permission) VALUES
    ('consumption_type.may_correct_late'),
    ('tenant.finance_manager');

-- +goose StatementBegin
DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['consumption_period_close', 'consumption_rate']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I'
            ' USING (tenant_id = current_setting(''app.tenant_id'')::uuid)', t);
    END LOOP;
END $$;
-- +goose StatementEnd
