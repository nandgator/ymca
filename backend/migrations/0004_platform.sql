-- 0004_platform — the platform plane's audit table, the permission that
-- guards a global person row, and the unit a membership belongs to.
--
-- Source of truth: docs/system-design/A2_data_model.md A2.1, A2.4, A2.10,
-- A2.12; ADR-104, ADR-105, ADR-112, ADR-113, ADR-114.
--
-- Three unrelated-looking changes, one cause: 8.3.5 tried to build
-- POST /platform/tenants and GET /t/{t}/units/{unit}/members against the
-- record, and found the record could not answer. A platform-plane DENY had
-- nowhere to go, POST /t/{t}/people had no permission to check, and
-- ADR-104's members query named a column no table had.
--
-- Forward-only. No Down section — see 0001 and 07.4.

-- +goose Up

-- ─────────────────────────────────────────────────────────────
-- PLATFORM-PLANE AUDIT (A2.10, ADR-112)
--
-- No tenant_id, because there is no tenant: a platform-lifecycle decision
-- names none. No RLS, because there is nothing to isolate BY. Isolation
-- here is the GRANT, which is ADR-108's principle — isolation rests on a
-- role, not only on the schema — applied a second time.
--
-- A second policy on audit_event keyed on a session GUC such as
-- current_setting('app.platform') was rejected: app.* is a placeholder GUC
-- that any role may set, so a tenant-path bug setting it would read every
-- platform row.
-- ─────────────────────────────────────────────────────────────

CREATE TABLE platform_audit_event (
    id                 bigserial PRIMARY KEY,
    actor_principal_id uuid,
    action             text NOT NULL,
    object_type        text,
    object_id          uuid,
    outcome            text NOT NULL,
    severity           text NOT NULL,
    context            jsonb,
    occurred_at        timestamptz NOT NULL DEFAULT now()
);

-- This REVOKE is mandatory, not belt-and-braces. 0001 ends with
-- ALTER DEFAULT PRIVILEGES ... GRANT SELECT, INSERT, UPDATE, DELETE ON
-- TABLES TO ymca_app, so the table above arrives with ymca_app already
-- holding all four. Without the REVOKE the application could read back
-- every platform decision, which is exactly what ADR-112 says it must not
-- be able to do.
--
-- Nothing is revoked on the backing sequence: bigserial needs USAGE on it
-- for the INSERT below to work, and default privileges already grant that.
REVOKE ALL ON platform_audit_event FROM ymca_app;
GRANT INSERT ON platform_audit_event TO ymca_app;

-- ─────────────────────────────────────────────────────────────
-- audit_event.tenant_id — NOT NULL, and deliberately no foreign key
-- ─────────────────────────────────────────────────────────────

-- A2's own comment used to read `tenant_id uuid, -- null for platform-plane`.
-- That was never true. audit_event carries FORCE ROW LEVEL SECURITY with
-- USING (tenant_id = current_setting('app.tenant_id')::uuid); a policy with
-- no WITH CHECK reuses its USING expression for INSERT, and NULL = <uuid> is
-- NULL rather than true, so the row is refused:
--
--   ERROR: new row violates row-level security policy for table "audit_event"
--
-- There was never a working path for a null-tenant audit row. The platform
-- plane has its own table above. Verified against the live cluster before
-- this migration was written, not assumed.
ALTER TABLE audit_event ALTER COLUMN tenant_id SET NOT NULL;

-- NOT NULL, but NO foreign key to tenant, and that is deliberate.
--
-- The ADR-105 tenant-mismatch DENY records the tenant the caller NAMED, and
-- a caller probing for tenants they cannot reach names one that does not
-- exist. A foreign key would refuse precisely the row that evidences the
-- probe — and httpx.TenantMatch only LOGS a failed audit write before
-- returning its 403, so the most security-relevant DENY in the system would
-- disappear while the request still looked correctly handled.
--
-- This is the same reason the columns beside it hold bare identifiers with
-- no references: the record outlives what it names (A2.10, "Outlives
-- SCRUBBED persons").

-- ─────────────────────────────────────────────────────────────
-- ADR-114 — may_register_person joins the grantable set
-- ─────────────────────────────────────────────────────────────

-- A1.2 gives tenant.may_register_person a [role_assignment#holder] type
-- restriction, which by ADR-110 IS the declaration that a role may confer
-- it. The set is stated twice — there, and here as seed data a foreign key
-- points at — so fga/grantable_test.go reads both artifacts and fails in
-- either direction. It is failing right now for want of this row.
INSERT INTO grantable_permission (permission) VALUES
    ('tenant.may_register_person');

-- ─────────────────────────────────────────────────────────────
-- ADR-104 — the unit a membership belongs to (A2.4, A2.12)
-- ─────────────────────────────────────────────────────────────

-- ADR-104 spells the members list out as check(member_read, unit) then
-- SELECT ... WHERE unit_id = $1, and no table carried the column. Nullable:
-- NULL is an association-level membership, which belongs to no unit and so
-- appears under no unit's list. That is ADR-104's permitted under-report —
-- the mechanism may under-report and must never over-report.
--
-- NOT NULL was rejected: it would force every tenant to own a unit before
-- admitting anyone, pushing a root unit into tenant provisioning, and 05.3
-- nowhere requires a membership to sit in a unit.
ALTER TABLE membership
    ADD COLUMN org_unit_id uuid REFERENCES organizational_unit;

CREATE INDEX ON membership (tenant_id, org_unit_id);
