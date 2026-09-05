-- 0002_roles — role definition, assignment, and what withholds them.
--
-- Source of truth: docs/system-design/A2_data_model.md A2.7 and A2.8, and
-- ADR-109, ADR-110, ADR-069, ADR-070, ADR-087.
--
-- These are the tables 6.1 step 2 queries. Nothing here is projected into
-- OpenFGA. A role assignment reaches the graph only as a contextual tuple,
-- built for one permission at the moment of a check, and only while it is
-- effective (ADR-109). There is therefore no outbox row, no fence and no
-- dispatcher in this file: an expired assignment needs no removing because
-- nothing was ever written.
--
-- Forward-only. No Down section, deliberately — see 0001 and 07.4.
--
-- ── DIFFERENCES FROM A2, and A2 has been changed to match ──────────────────
--
-- E1  A2.11 called for a trigger keeping `role_permission` inside the
--     grantable set of ADR-110. This file uses a FOREIGN KEY to a seeded
--     `grantable_permission` table instead. A foreign key states the rule
--     declaratively, cannot be skipped by a bulk load, and needs no
--     maintenance. A2.11 now says foreign key.
--
-- E2  `grantable_permission` is not in A2. It is the ADR-110 set made into a
--     table so that E1 can reference it. A2.7 now defines it.
--
-- The MANDATORY_TERM rule stays a trigger: it reads `term_policy` from the
-- referenced `role_definition` row, which a CHECK constraint cannot do.

-- +goose Up

-- ─────────────────────────────────────────────────────────────
-- THE GRANTABLE SET (ADR-110)
--
-- A permission is role-conferrable if and only if `role_assignment#holder`
-- appears in its type restriction in A1.2. That is the authoritative
-- statement; this table is the same set in a form a foreign key can point
-- at, and `TestGrantableSetMatchesModel` fails if the two ever disagree.
--
-- Platform-defined, not tenant-configurable: permissions are system-defined
-- and immutable (ADR-076), so this carries no tenant_id and no RLS policy.
-- ─────────────────────────────────────────────────────────────

CREATE TABLE grantable_permission (
    permission text PRIMARY KEY
);

INSERT INTO grantable_permission (permission) VALUES
    ('organizational_unit.member_read'),
    ('organizational_unit.unit_close'),
    ('resource.may_close'),
    ('programme.may_manage'),
    ('consumption_type.may_record_for_other'),
    ('consumption_type.may_correct'),
    ('consumption_type.may_read'),
    ('consumption_type.may_close_period'),
    ('tenant.finance_reader'),
    ('tenant.safeguarding_reader'),
    ('tenant.may_approve_membership');

-- ─────────────────────────────────────────────────────────────
-- ROLE DEFINITION (ADR-011, ADR-022, ADR-069, ADR-076, ADR-079)
-- ─────────────────────────────────────────────────────────────

CREATE TABLE role_definition (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenant,
    code         text NOT NULL,
    name         text NOT NULL,
    term_policy  text NOT NULL
        CHECK (term_policy IN ('PERMANENT', 'OPTIONAL_TERM', 'MANDATORY_TERM')),
    -- 05.9.3. A restriction of kind NO_YOUTH_FACING_ROLES suppresses every
    -- assignment whose definition carries this, in 6.1 step 2.
    youth_facing boolean NOT NULL DEFAULT false,
    status       text NOT NULL CHECK (status IN ('ACTIVE', 'RETIRED')),
    UNIQUE (tenant_id, code)
);

-- The bundle. E1: the foreign key is ADR-110 enforced declaratively — a role
-- cannot be given a permission the model would refuse to resolve for it.
CREATE TABLE role_permission (
    role_definition_id uuid NOT NULL REFERENCES role_definition ON DELETE CASCADE,
    permission         text NOT NULL REFERENCES grantable_permission,
    PRIMARY KEY (role_definition_id, permission)
);

-- ADR-087. An assignment whose definition requires a clearance the person
-- does not currently hold is inert, exactly as an expired term is — and by
-- the same mechanism, since both are resolved in step 2's WHERE clause.
CREATE TABLE role_required_clearance (
    role_definition_id uuid NOT NULL REFERENCES role_definition ON DELETE CASCADE,
    verification_type  text NOT NULL,
    PRIMARY KEY (role_definition_id, verification_type)
);

-- ─────────────────────────────────────────────────────────────
-- ROLE ASSIGNMENT (ADR-011, ADR-070)
-- ─────────────────────────────────────────────────────────────

CREATE TABLE role_assignment (
    id                        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                 uuid NOT NULL REFERENCES tenant,
    role_definition_id        uuid NOT NULL REFERENCES role_definition,
    subject_principal_id      uuid NOT NULL REFERENCES principal,
    -- ADR-011: an assignment always carries a scope. Step 2 emits a
    -- contextual tuple only for permissions whose object type equals
    -- scope_type, so a definition bundling unit-scoped and tenant-scoped
    -- permissions is legitimate — each reaches only where it can land.
    scope_type                text NOT NULL CHECK (scope_type IN (
        'tenant', 'organizational_unit', 'resource',
        'programme', 'consumption_type')),
    scope_id                  uuid NOT NULL,
    valid_from                timestamptz NOT NULL DEFAULT now(),
    valid_until               timestamptz,
    holding_type              text NOT NULL DEFAULT 'SUBSTANTIVE'
        CHECK (holding_type IN ('SUBSTANTIVE', 'ACTING')),
    substantive_assignment_id uuid REFERENCES role_assignment,
    via_office_holding_id     uuid REFERENCES office_holding,
    granted_by_principal_id   uuid NOT NULL REFERENCES principal,
    decision_record_id        uuid REFERENCES decision_record,
    revoked_at                timestamptz,
    CONSTRAINT acting_names_its_substantive CHECK (
        (holding_type = 'ACTING') = (substantive_assignment_id IS NOT NULL)),
    CONSTRAINT window_ordered CHECK (
        valid_until IS NULL OR valid_until > valid_from)
);

-- ADR-069. MANDATORY_TERM means an end date is not optional. The policy
-- lives on the referenced role_definition row, which is why this is a
-- trigger and not a CHECK: a CHECK cannot read another table.
-- +goose StatementBegin
CREATE FUNCTION role_assignment_term_policy() RETURNS trigger AS $$
DECLARE
    policy text;
BEGIN
    SELECT term_policy INTO policy
    FROM role_definition WHERE id = NEW.role_definition_id;

    IF policy = 'MANDATORY_TERM' AND NEW.valid_until IS NULL THEN
        RAISE EXCEPTION
            'role_assignment %: definition % is MANDATORY_TERM and requires valid_until (ADR-069)',
            NEW.id, NEW.role_definition_id;
    END IF;
    RETURN NEW;
END $$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER role_assignment_term_policy
    BEFORE INSERT OR UPDATE ON role_assignment
    FOR EACH ROW EXECUTE FUNCTION role_assignment_term_policy();

-- The office -> role arrow of 05.6.3, which had no table behind it.
-- Appointing materializes one role_assignment per conferred definition
-- carrying via_office_holding_id; vacating ends them together, which is what
-- 05.6.4 means by "atomically" — and needs no OpenFGA write, because there is
-- no tuple to remove (ADR-109).
CREATE TABLE office_conferred_role (
    office_id          uuid NOT NULL REFERENCES office ON DELETE CASCADE,
    role_definition_id uuid NOT NULL REFERENCES role_definition,
    PRIMARY KEY (office_id, role_definition_id)
);

-- ─────────────────────────────────────────────────────────────
-- WHAT A RESTRICTION WITHHOLDS (A2.8, 05.9.4, 8.8)
--
-- Before this table `restriction.kind` mapped to nothing and 6.1's
-- restriction limb checked nothing at all.
--
-- Platform-defined: a restriction may be platform-wide (05.9.9), so its
-- meaning cannot vary by tenant. No tenant_id, no RLS policy, and the
-- application role gets SELECT only — the grant at the foot of this file
-- revokes the rest.
--
-- Only two of 05.9.4's four kinds appear. NO_ROLE_ASSIGNMENT and
-- NO_YOUTH_FACING_ROLES act on the role path in step 2 and suppress
-- assignments rather than permissions, so they need no rows here. That is
-- why this table is smaller than the gap suggested.
-- ─────────────────────────────────────────────────────────────

CREATE TABLE restriction_kind_permission (
    kind       text NOT NULL,
    permission text NOT NULL,
    PRIMARY KEY (kind, permission)
);

INSERT INTO restriction_kind_permission (kind, permission) VALUES
    ('NO_RESOURCE_ACCESS',       'resource.may_use'),
    ('NO_RESOURCE_ACCESS',       'resource.may_book'),
    ('NO_RESOURCE_ACCESS',       'consumption_type.may_record'),
    -- Standing a person down means neither acting in a role nor using the
    -- facilities. The role half is applied in step 2; these are the rest.
    ('SUSPENDED_PENDING_REVIEW', 'resource.may_use'),
    ('SUSPENDED_PENDING_REVIEW', 'resource.may_book'),
    ('SUSPENDED_PENDING_REVIEW', 'consumption_type.may_record');

-- ─────────────────────────────────────────────────────────────
-- INDEXES (A2.12)
-- ─────────────────────────────────────────────────────────────

-- Step 2 runs on every authorized request. 10 sets a p99 for check() and
-- step 2 is now inside it, so this index is a latency requirement.
CREATE INDEX role_assignment_subject
    ON role_assignment (subject_principal_id, tenant_id)
    WHERE revoked_at IS NULL;
CREATE INDEX role_permission_permission
    ON role_permission (permission);
CREATE INDEX role_assignment_substantive
    ON role_assignment (substantive_assignment_id)
    WHERE substantive_assignment_id IS NOT NULL;
CREATE INDEX role_assignment_via_office
    ON role_assignment (via_office_holding_id)
    WHERE via_office_holding_id IS NOT NULL;

-- ─────────────────────────────────────────────────────────────
-- ROW LEVEL SECURITY (A2.1, ADR-108)
--
-- Only the two tables carrying tenant_id. role_permission,
-- role_required_clearance and office_conferred_role reach their tenant
-- through role_definition or office, both of which are policed here.
-- ─────────────────────────────────────────────────────────────

-- +goose StatementBegin
DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['role_definition', 'role_assignment']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I'
            ' USING (tenant_id = current_setting(''app.tenant_id'')::uuid)', t);
    END LOOP;
END $$;
-- +goose StatementEnd

-- ALTER DEFAULT PRIVILEGES in 0001 already granted DML on these tables to
-- ymca_app. The two platform-defined tables are reference data the tenant
-- plane reads and must never write.
REVOKE INSERT, UPDATE, DELETE ON grantable_permission FROM ymca_app;
REVOKE INSERT, UPDATE, DELETE ON restriction_kind_permission FROM ymca_app;
