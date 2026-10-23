-- 0011_plan_lifecycle — what closing, superseding and retiring a plan, and
-- moving a holder from one plan to another, need that A2.4 did not give them.
--
-- Source of truth: docs/system-design/05.3.2, A2.4, A3.7, ADR-121.
--
-- P1  `membership_plan.status` was free text. 05.3.2 names four values and a
--     successor link; the state machine is now a CHECK, so a typo is a refusal
--     and not a fifth state. A plan cannot succeed itself, and a plan that has
--     a successor is no longer open to new members.
--
-- P2  "Migrating holders: explicit, per-membership, auditable" (05.3.2). The
--     row below is the audit: which membership moved, from which plan to which,
--     who moved it and when.
--
-- Forward-only. No Down section — see 0001 and 07.4.

-- +goose Up

ALTER TABLE membership_plan
    ADD CONSTRAINT membership_plan_status_known
        CHECK (status IN ('DRAFT', 'OPEN', 'CLOSED_TO_NEW', 'RETIRED')),
    ADD CONSTRAINT membership_plan_not_own_successor
        CHECK (superseded_by_id IS NULL OR superseded_by_id <> id),
    ADD CONSTRAINT membership_plan_superseded_is_closed
        CHECK (superseded_by_id IS NULL OR status IN ('CLOSED_TO_NEW', 'RETIRED'));

CREATE TABLE membership_plan_change (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenant,
    membership_id       uuid NOT NULL REFERENCES membership,
    from_plan_id        uuid NOT NULL REFERENCES membership_plan,
    to_plan_id          uuid NOT NULL REFERENCES membership_plan,
    changed_by_person_id uuid NOT NULL REFERENCES person,
    changed_at          timestamptz NOT NULL DEFAULT now(),
    CHECK (from_plan_id <> to_plan_id)
);
CREATE INDEX membership_plan_change_membership ON membership_plan_change (membership_id);

ALTER TABLE membership_plan_change ENABLE ROW LEVEL SECURITY;
ALTER TABLE membership_plan_change FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON membership_plan_change
    USING (tenant_id = current_setting('app.tenant_id')::uuid);
