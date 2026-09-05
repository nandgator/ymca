-- 0003_idempotency — the store A3.6 requires and A2 did not define.
--
-- Source of truth: docs/system-design/A2_data_model.md A2.10, and A3.6.
--
-- A3.6 has required an Idempotency-Key on every POST that creates money or a
-- consumption record since the contract was written, and required the result
-- to be stored so a repeat returns the original. Nothing stored it. A warden
-- on hostel wifi retries, and without this a retried meal is a second meal.
--
-- The row is written inside the transaction that does the work. Effect and
-- record commit together or not at all, so there is no in-flight state and a
-- rolled-back attempt leaves nothing to replay.
--
-- Forward-only. No Down section — see 0001 and 07.4.

-- +goose Up

CREATE TABLE idempotency_key (
    tenant_id      uuid NOT NULL REFERENCES tenant,
    key            text NOT NULL,
    endpoint       text NOT NULL,
    -- A key replayed against a different body is a client defect. Comparing
    -- the digest turns that into a 409 rather than silently discarding the
    -- second request by replaying the first one's response.
    request_digest text NOT NULL,
    principal_id   uuid NOT NULL REFERENCES principal,
    status_code    int NOT NULL,
    response_body  jsonb NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, endpoint, key)
);

-- For an expiry sweep. Keys are not kept forever; nothing sweeps them yet
-- and that is recorded in 11.2 rather than implied by this index.
CREATE INDEX idempotency_key_created ON idempotency_key (created_at);

-- +goose StatementBegin
DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['idempotency_key']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I'
            ' USING (tenant_id = current_setting(''app.tenant_id'')::uuid)', t);
    END LOOP;
END $$;
-- +goose StatementEnd
