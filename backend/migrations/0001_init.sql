-- 0001_init — the full A2 schema.
--
-- Source of truth: docs/system-design/A2_data_model.md, sections A2.2 to A2.10.
-- This file is that document in dependency order. Every table, column,
-- constraint and index in A2 appears here, and nothing appears here that A2
-- does not name. The differences are formatting, listed below.
--
-- Forward-only (07_deployment_view.md §7.4). This file declares no Down
-- section, and that is deliberate: `goose down` will refuse to run rather
-- than half-reverse a schema. To reset a development database, drop it and
-- migrate again — see backend/README.md.
-- (The annotation is spelled out nowhere in this comment block on purpose:
--  goose parses its directives out of comments, anywhere in the file.)
--
-- ── DIFFERENCES FROM A2, all recorded there ────────────────────────────────
--
-- D1  Table order is topological, not A2's presentation order. A2 groups by
--     bounded context, which puts `verification` (A2.8) after `guardianship`
--     (A2.3) that references it, `decision_record` (A2.7) after `membership`
--     (A2.4), and `invoice` (A2.8) after `booking` (A2.5). A2.1 states that
--     the migration reorders.
--
-- D2  `DEFAULT gen_random_uuid()` is applied to every `id`. A2's conventions
--     preamble states this; A2's table bodies omit it.
--
-- D3  Indexes that A2.12 leaves unnamed are named explicitly, so that a later
--     migration can refer to them.
--
-- Three things this migration does that A2 did not originally say, and now
-- does — `location` (A2.2), `FORCE ROW LEVEL SECURITY` and the `ymca_app`
-- role (A2.1, ADR-108). They are no longer deviations.
--
-- ── KNOWN GAPS, recorded in A2.1 and 11.2 ──────────────────────────────────
--
-- H1  Twelve tables carry no `tenant_id` and therefore get no RLS policy.
--     `charge` and `charge_component` are the ones that matter: an invoice id
--     alone reaches its contents.
--
-- H2  `current_setting('app.tenant_id')` without `missing_ok` raises rather
--     than returning NULL when the setting is absent. A2.1 verbatim, and
--     deliberate: a query that forgot its tenant is a defect. Every pooled
--     connection must SET it before touching a tenant-scoped table.
--
-- H3  `verification`, `clearance` and `audit_event` have a nullable
--     `tenant_id` for deliberately global rows, which the policy makes
--     invisible to every tenant connection.
--
-- H4  No cycle check on `authorization_edge`. A2.2 specifies "recursive CTE,
--     pre-commit, depth bounded by policy, default 12" — application code,
--     not schema. Until it exists the DAG is a graph.

-- +goose Up

-- ─────────────────────────────────────────────────────────────
-- EXTENSIONS (A2.1)
-- ─────────────────────────────────────────────────────────────

-- Required by the EXCLUDE constraints on `allocation` and
-- `consumption_obligation`, which mix uuid `=` with range `&&`.
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- A2.1 names this for gen_random_uuid(). PostgreSQL has provided that
-- function in core since 13, and this cluster is 18 — so the extension is
-- redundant here. Created anyway, because A2.1 says so and because a
-- deployment target older than 13 would need it.
CREATE EXTENSION IF NOT EXISTS pgcrypto;


-- ─────────────────────────────────────────────────────────────
-- ORGANIZATION (A2.2)
-- ─────────────────────────────────────────────────────────────

CREATE TABLE tenant (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    legal_name      text NOT NULL,
    display_name    text NOT NULL,
    jurisdiction    text NOT NULL,       -- drives tax, screening, residency
    status          text NOT NULL,       -- ACTIVE|SUSPENDED|ARCHIVED
    created_at      timestamptz NOT NULL DEFAULT now()
    -- NO parent_id. Relationships to other tenants exist only
    -- as affiliation and authority_grant rows. Principle 2.
);

-- A2.2. Physical containment only; no authorization meaning (ADR-095).
CREATE TABLE location (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    uuid NOT NULL REFERENCES tenant,
    name         text NOT NULL,
    address_line text,
    locality     text,
    region       text,
    postal_code  text,
    country_code char(2),
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE organizational_unit (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenant,   -- immutable
    type            text NOT NULL,       -- CHAPTER|DEPARTMENT|CENTRE|
                                         -- REGION|INSTITUTE|PROJECT
    name            text NOT NULL,
    org_parent_id   uuid REFERENCES organizational_unit,
    location_id     uuid REFERENCES location,
    status          text NOT NULL
);

-- The DAG. NOT derived from org_parent_id. ADR-016, 05.1.3
CREATE TABLE authorization_edge (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenant,
    child_type      text NOT NULL,
    child_id        uuid NOT NULL,
    parent_type     text NOT NULL,
    parent_id       uuid NOT NULL,
    created_by      uuid NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (child_type, child_id, parent_type, parent_id)
);
-- Cycle detection runs before commit, in a recursive CTE.
-- Depth bounded by policy, default 12. See H4 — not yet written.

CREATE TABLE affiliation (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    from_tenant_id  uuid NOT NULL REFERENCES tenant,
    to_tenant_id    uuid NOT NULL REFERENCES tenant,
    state           text NOT NULL,
    since           date,
    last_reviewed_at timestamptz,
    CHECK (from_tenant_id <> to_tenant_id)
);

CREATE TABLE authority_grant (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    from_tenant_id  uuid NOT NULL REFERENCES tenant,
    to_tenant_id    uuid NOT NULL REFERENCES tenant,
    verb            text NOT NULL,       -- one of five; no ALL value
    granted_by      uuid NOT NULL,
    granted_at      timestamptz NOT NULL DEFAULT now(),
    basis           text,
    expires_at      timestamptz,
    UNIQUE (from_tenant_id, to_tenant_id, verb)
);

CREATE TABLE cross_tenant_grant (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    from_tenant_id  uuid NOT NULL REFERENCES tenant,
    subject_type    text NOT NULL,
    subject_id      uuid NOT NULL,
    scope_object_type text NOT NULL,
    scope_object_id uuid NOT NULL,
    permissions     text[] NOT NULL,     -- explicit; no wildcards
    reason          text NOT NULL,
    approved_by     uuid NOT NULL,
    granted_at      timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz NOT NULL,   -- mandatory. ADR-019
    revoked_at      timestamptz,
    CHECK (array_length(permissions, 1) > 0)
);


-- ─────────────────────────────────────────────────────────────
-- IDENTITY (A2.3), part one
-- `guardianship` needs `verification` (A2.8) and follows it below.
-- ─────────────────────────────────────────────────────────────

-- Global. No tenant_id. See 05.2.2.
CREATE TABLE person (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    given_name        text,
    family_name       text,
    display_name      text,
    date_of_birth     date,
    sex               text,          -- optional; accommodation only
    preferred_language text,
    status            text NOT NULL, -- ACTIVE|DECEASED|MERGED|SCRUBBED
    merged_into_id    uuid REFERENCES person,
    created_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE principal (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id     uuid NOT NULL REFERENCES person,
    idp_subject   text NOT NULL UNIQUE,
    kind          text NOT NULL,     -- PERSONAL|STAFF|ELEVATED
    label         text,
    status        text NOT NULL
);
CREATE UNIQUE INDEX one_elevated_per_person
  ON principal (person_id) WHERE kind = 'ELEVATED';

CREATE TABLE consent_grant (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenant,
    subject_person_id   uuid NOT NULL REFERENCES person,
    granted_by_person_id uuid NOT NULL REFERENCES person,
    purpose             text NOT NULL,   -- enumerated, never free text
    granted_at          timestamptz NOT NULL,
    expires_at          timestamptz,
    withdrawn_at        timestamptz,     -- withdrawal preserves the row
    evidence_reference  text
);


-- ─────────────────────────────────────────────────────────────
-- GOVERNANCE (A2.7), hoisted
-- `decision_record` is referenced by membership_application, membership,
-- office_holding, policy_definition, restriction, expected_absence and stay.
-- `office` and `office_holding` stay below, with the rest of A2.7.
-- ─────────────────────────────────────────────────────────────

CREATE TABLE decision_record (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenant,
    deciding_body_type text NOT NULL,
    deciding_body_id   uuid,
    decision_type      text NOT NULL,
    subject_type       text NOT NULL,
    subject_id         uuid NOT NULL,
    outcome            text NOT NULL,
    decided_at         timestamptz NOT NULL,
    recorded_by_person_id uuid NOT NULL REFERENCES person,
    participants       uuid[],
    quorum_met         boolean,
    reference          text
    -- No deliberation, no minutes, no reasons. 05.6.6
);


-- ─────────────────────────────────────────────────────────────
-- SAFEGUARDING (A2.8), hoisted
-- `verification` is referenced by guardianship (A2.3) and stay (A2.5).
-- ─────────────────────────────────────────────────────────────

CREATE TABLE verification (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid REFERENCES tenant,   -- nullable; some global
    subject_type        text NOT NULL,
    subject_id          uuid NOT NULL,
    type                text NOT NULL,
    status              text NOT NULL,
    provider_code       text,
    evidence_reference  text,   -- OPAQUE ONLY. Never content. ADR-086
    verified_by_person_id uuid REFERENCES person,
    verified_at         timestamptz,
    expires_at          timestamptz
);

CREATE TABLE clearance (
    verification_id   uuid PRIMARY KEY REFERENCES verification,
    person_id         uuid NOT NULL REFERENCES person,
    tenant_id         uuid REFERENCES tenant,
    scope_of_check    text,
    valid_from        date,
    valid_until       date,
    next_check_due_at date,
    supersedes_id     uuid REFERENCES verification
);

-- Global; may be platform-wide. The one cross-tenant
-- safeguarding flow. 05.9.9
CREATE TABLE restriction (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id            uuid NOT NULL REFERENCES person,
    tenant_id            uuid REFERENCES tenant,   -- null = platform-wide
    kind                 text NOT NULL,
    imposed_by_person_id uuid NOT NULL REFERENCES person,
    imposed_at           timestamptz NOT NULL DEFAULT now(),
    decision_record_id   uuid REFERENCES decision_record,
    external_case_ref    text,   -- opaque pointer. No narrative. ADR-089
    expires_at           timestamptz,
    lifted_at            timestamptz,
    lifted_by_person_id  uuid REFERENCES person
);


-- ─────────────────────────────────────────────────────────────
-- IDENTITY (A2.3), part two
-- ─────────────────────────────────────────────────────────────

CREATE TABLE guardianship (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    guardian_person_id uuid NOT NULL REFERENCES person,
    minor_person_id   uuid NOT NULL REFERENCES person,
    relationship_type text NOT NULL,
    verification_id   uuid NOT NULL REFERENCES verification,
    established_at    timestamptz NOT NULL,
    ends_at           timestamptz,
    revoked_at        timestamptz,
    CHECK (guardian_person_id <> minor_person_id)
);


-- ─────────────────────────────────────────────────────────────
-- FINANCE (A2.8)
-- `invoice` is referenced by booking, stay and enrollment.
-- ─────────────────────────────────────────────────────────────

CREATE TABLE party (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind            text NOT NULL,   -- PERSON|ORGANIZATION
    person_id       uuid REFERENCES person,
    organization_id uuid,
    tax_identifier  text,
    CHECK (num_nonnulls(person_id, organization_id) = 1)
);
-- Generic parties. ADR-082

CREATE TABLE invoice (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenant,
    payer_party_id uuid NOT NULL REFERENCES party,
    payee_party_id uuid NOT NULL REFERENCES party,
    number         text,             -- gapless per tenant per year
    currency       char(3) NOT NULL,
    issued_at      timestamptz,
    due_at         timestamptz,
    state          text NOT NULL,
    total_amount   numeric(14,2),
    tax_snapshot   jsonb,            -- SNAPSHOT
    UNIQUE (tenant_id, number)
);
-- Issued invoices are immutable. Corrections are credit notes.

CREATE TABLE invoice_number_series (
    tenant_id  uuid NOT NULL REFERENCES tenant,
    series     text NOT NULL,   -- financial year, e.g. '2026-27'
    kind       text NOT NULL,   -- LIVE|IMPORTED. ADR-103
    prefix     text,
    next_value bigint NOT NULL DEFAULT 1,
    PRIMARY KEY (tenant_id, series, kind)
);
-- Allocated by UPDATE ... RETURNING inside the issuing transaction,
-- as late in it as possible. A sequence cannot be used: nextval is
-- non-transactional, so a rollback would leave a gap. ADR-103

CREATE TABLE charge (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id  uuid NOT NULL REFERENCES invoice,
    source_type text NOT NULL,   -- MEMBERSHIP|SUBSCRIPTION|ENROLLMENT|
                                 -- BOOKING|STAY|CONSUMPTION|DEPOSIT|
                                 -- DUES|ADJUSTMENT
    source_id   uuid,
    description text
);

CREATE TABLE charge_component (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    charge_id        uuid NOT NULL REFERENCES charge,
    kind             text NOT NULL,   -- ENTRANCE_FEE|SUBSCRIPTION_FEE|
                                      -- USAGE_FEE|DEPOSIT|LATE_FEE|
                                      -- DISCOUNT|TAX
    recurrence       text NOT NULL,
    amount           numeric(14,2) NOT NULL,  -- signed
    currency         char(3) NOT NULL,
    tax_treatment_id uuid
);

CREATE TABLE payment (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenant,
    invoice_id         uuid NOT NULL REFERENCES invoice,
    party_id           uuid REFERENCES party,
    method             text NOT NULL,   -- CARD|UPI|BANK_TRANSFER|
                                        -- CASH|CHEQUE|PROVIDER_HOSTED
    amount             numeric(14,2) NOT NULL,
    currency           char(3) NOT NULL,
    state              text NOT NULL,
    provider_reference text,            -- opaque
    received_at        timestamptz,
    recorded_by        uuid             -- required for offline methods
);


-- ─────────────────────────────────────────────────────────────
-- MEMBERSHIP (A2.4)
-- ─────────────────────────────────────────────────────────────

CREATE TABLE entitlement_bundle (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenant,
    name      text NOT NULL
);

CREATE TABLE entitlement_grant (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bundle_id    uuid NOT NULL REFERENCES entitlement_bundle,
    target_type  text NOT NULL,   -- RESOURCE_TYPE|RESOURCE|
                                  -- PROGRAMME|ORG_UNIT
    target_id    uuid NOT NULL,
    access_level text NOT NULL    -- USE|BOOK|PRIORITY_BOOK
);

CREATE TABLE governance_profile (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             uuid NOT NULL REFERENCES tenant,
    may_vote              boolean NOT NULL DEFAULT false,
    may_stand_for_election boolean NOT NULL DEFAULT false,
    may_hold_office       boolean NOT NULL DEFAULT false,
    eligibility_policy_id uuid
);

CREATE TABLE membership_plan (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             uuid NOT NULL REFERENCES tenant,
    code                  text NOT NULL,
    name                  text NOT NULL,
    acquisition           text NOT NULL,  -- PURCHASED|CONFERRED
    duration              text NOT NULL,  -- ANNUAL|FIXED_TERM|LIFETIME
    duration_months       int,
    max_dependents        int NOT NULL DEFAULT 0,
    eligibility_policy_id uuid,
    entitlement_bundle_id uuid REFERENCES entitlement_bundle,
    governance_profile_id uuid REFERENCES governance_profile,
    status                text NOT NULL,  -- DRAFT|OPEN|CLOSED_TO_NEW|RETIRED
    superseded_by_id      uuid REFERENCES membership_plan,
    UNIQUE (tenant_id, code)
);
-- Plans are versioned, never edited in place. 05.3.2

CREATE TABLE membership_application (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenant,
    person_id          uuid NOT NULL REFERENCES person,
    plan_id            uuid NOT NULL REFERENCES membership_plan,
    submitted_at       timestamptz,
    state              text NOT NULL,
    decided_by_record_id uuid REFERENCES decision_record,
    decided_at         timestamptz,
    rejection_recorded boolean NOT NULL DEFAULT false
    -- No reason column, by design. 05.3.4
);

CREATE TABLE membership (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             uuid NOT NULL REFERENCES tenant,
    person_id             uuid NOT NULL REFERENCES person,
    plan_id               uuid NOT NULL REFERENCES membership_plan,
    number                text NOT NULL,
    state                 text NOT NULL,
    admitted_at           timestamptz,
    valid_from            date,
    valid_until           date,           -- null for LIFETIME
    application_id        uuid REFERENCES membership_application,
    conferred_by_record_id uuid REFERENCES decision_record,
    UNIQUE (tenant_id, number)
);
CREATE UNIQUE INDEX one_active_membership_per_tenant
  ON membership (tenant_id, person_id) WHERE state = 'ACTIVE';

CREATE TABLE membership_dependent (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    membership_id uuid NOT NULL REFERENCES membership,
    person_id     uuid NOT NULL REFERENCES person,
    relationship  text NOT NULL,
    added_at      timestamptz NOT NULL DEFAULT now(),
    removed_at    timestamptz
);

-- Explicit, never derived. ADR-047
CREATE TABLE membership_suspension (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    membership_id   uuid NOT NULL REFERENCES membership,
    reason_category text NOT NULL,   -- ARREARS|CONDUCT|SAFEGUARDING|
                                     -- ADMINISTRATIVE
    issued_by       uuid NOT NULL,
    issued_at       timestamptz NOT NULL DEFAULT now(),
    effective_from  timestamptz NOT NULL,
    lifted_at       timestamptz
);

CREATE TABLE subscription (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    membership_id         uuid NOT NULL REFERENCES membership,
    entitlement_bundle_id uuid NOT NULL REFERENCES entitlement_bundle,
    state                 text NOT NULL,
    valid_from            date,
    valid_until           date
);


-- ─────────────────────────────────────────────────────────────
-- RESOURCE AND CONSUMPTION SUBSTRATE (A2.5)
-- ─────────────────────────────────────────────────────────────

CREATE TABLE resource_type (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenant,
    name                text NOT NULL,
    archetype           text NOT NULL,  -- BOOKABLE_SLOT|ALLOCATABLE_UNIT|
                                        -- WALK_IN_ONLY|NON_CONSUMABLE
    default_access_mode text
);

CREATE TABLE resource (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenant,
    resource_type_id   uuid NOT NULL REFERENCES resource_type,
    org_unit_id        uuid REFERENCES organizational_unit,
    location_id        uuid REFERENCES location,
    parent_resource_id uuid REFERENCES resource,   -- bed within room
    name               text NOT NULL,
    capacity           int,
    allocation_unit    text,            -- BED|ROOM, for accommodation
    status             text NOT NULL
);

CREATE TABLE access_window (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            uuid NOT NULL REFERENCES tenant,
    resource_id          uuid NOT NULL REFERENCES resource,
    schedule_rule        text NOT NULL,
    starts_time          time NOT NULL,
    ends_time            time NOT NULL,
    access_mode          text NOT NULL,  -- WALK_IN|BOOKED
    capacity             int,            -- enforced only for BOOKED
    entitlement_required uuid,
    valid_from           date,
    valid_until          date
);

-- The exclusivity substrate. Not an aggregate root. ADR-059
CREATE TABLE allocation (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenant,
    resource_id   uuid NOT NULL REFERENCES resource,
    period        tstzrange NOT NULL,
    consumer_type text NOT NULL,   -- BOOKING|STAY|OCCURRENCE|MAINTENANCE
    consumer_id   uuid NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),

    EXCLUDE USING gist (
        resource_id WITH =,
        period      WITH &&
    )
);
-- Three writers, one constraint. Cannot be bypassed by any
-- code path, including ones that do not exist yet.

CREATE TABLE booking (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id              uuid NOT NULL REFERENCES tenant,
    resource_id            uuid NOT NULL REFERENCES resource,
    access_window_id       uuid REFERENCES access_window,
    actor_person_id        uuid NOT NULL REFERENCES person,
    beneficiary_person_id  uuid NOT NULL REFERENCES person,
    allocation_id          uuid REFERENCES allocation,
    invoice_id             uuid REFERENCES invoice,
    state                  text NOT NULL,
    cancellation_policy    jsonb NOT NULL,   -- SNAPSHOT, not a reference
    created_at             timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE stay (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               uuid NOT NULL REFERENCES tenant,
    resource_id             uuid NOT NULL REFERENCES resource,
    person_id               uuid NOT NULL REFERENCES person,
    booked_by_person_id     uuid REFERENCES person,
    allocation_id           uuid REFERENCES allocation,
    verification_id         uuid REFERENCES verification,
    approval_record_id      uuid REFERENCES decision_record,
    house_rules_accepted_at timestamptz,
    deposit_charge_id       uuid,
    invoice_id              uuid REFERENCES invoice,
    state                   text NOT NULL,
    expected_check_out      timestamptz,
    actual_check_out        timestamptz
);
-- Independent state machines. No shared supertype. ADR-058


-- ─────────────────────────────────────────────────────────────
-- PROGRAMME (A2.6)
-- ─────────────────────────────────────────────────────────────

CREATE TABLE programme (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenant,
    org_unit_id uuid REFERENCES organizational_unit,
    name        text NOT NULL,
    category    text,
    youth_facing boolean NOT NULL DEFAULT false,
    status      text NOT NULL
);

CREATE TABLE offering (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             uuid NOT NULL REFERENCES tenant,
    programme_id          uuid NOT NULL REFERENCES programme,
    code                  text,
    name                  text NOT NULL,
    enrollment_opens_at   timestamptz,
    enrollment_closes_at  timestamptz,
    starts_on             date,
    ends_on               date,
    capacity              int,
    min_enrollment        int,
    eligibility_policy_id uuid,
    status                text NOT NULL
);

CREATE TABLE price_component (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    offering_id  uuid NOT NULL REFERENCES offering,
    audience     text NOT NULL,
    kind         text NOT NULL,
    amount       numeric(14,2),
    currency     char(3),
    valid_from   timestamptz,
    valid_until  timestamptz,
    availability text NOT NULL DEFAULT 'AVAILABLE'   -- or UNAVAILABLE
);
-- UNAVAILABLE is distinct from absent. 05.4.5

CREATE TABLE enrollment (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            uuid NOT NULL REFERENCES tenant,
    offering_id          uuid NOT NULL REFERENCES offering,
    person_id            uuid NOT NULL REFERENCES person,
    enrolled_by_person_id uuid REFERENCES person,
    consent_grant_id     uuid REFERENCES consent_grant,
    price_snapshot       jsonb NOT NULL,   -- SNAPSHOT
    invoice_id           uuid REFERENCES invoice,
    state                text NOT NULL,
    enrolled_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE occurrence (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            uuid NOT NULL REFERENCES tenant,
    offering_id          uuid NOT NULL REFERENCES offering,
    starts_at            timestamptz NOT NULL,
    ends_at              timestamptz NOT NULL,
    allocation_id        uuid REFERENCES allocation,
    instructor_person_id uuid REFERENCES person,
    status               text NOT NULL,
    superseded_by_id     uuid REFERENCES occurrence
);
-- Rescheduling creates a new row. 05.4.7

CREATE TABLE attendance (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL REFERENCES tenant,
    occurrence_id uuid NOT NULL REFERENCES occurrence,
    person_id     uuid NOT NULL REFERENCES person,
    state         text NOT NULL,
    recorded_by   uuid,
    recorded_at   timestamptz NOT NULL DEFAULT now()
);


-- ─────────────────────────────────────────────────────────────
-- GOVERNANCE (A2.7), remainder
-- ─────────────────────────────────────────────────────────────

CREATE TABLE office (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             uuid NOT NULL REFERENCES tenant,
    org_unit_id           uuid REFERENCES organizational_unit,
    title                 text NOT NULL,
    term_months           int,
    max_consecutive_terms int,
    eligibility_policy_id uuid,
    seat_count            int NOT NULL DEFAULT 1,
    status                text NOT NULL
);

CREATE TABLE office_holding (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id              uuid NOT NULL REFERENCES tenant,
    office_id              uuid NOT NULL REFERENCES office,
    person_id              uuid NOT NULL REFERENCES person,
    seat_ordinal           int NOT NULL DEFAULT 1,
    valid_from             timestamptz NOT NULL,
    valid_until            timestamptz NOT NULL,   -- MANDATORY_TERM
    holding_type           text NOT NULL,          -- SUBSTANTIVE|ACTING
    substantive_holding_id uuid REFERENCES office_holding,
    decision_record_id     uuid REFERENCES decision_record,
    vacated_at             timestamptz,
    vacation_reason        text
);
-- Expiry evaluated at decision time, never by sweeper. ADR-070


-- ─────────────────────────────────────────────────────────────
-- POLICY (A2.8)
-- ─────────────────────────────────────────────────────────────

CREATE TABLE policy_definition (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_type_code   text NOT NULL,
    owner_org_id       uuid NOT NULL,
    level              text NOT NULL,   -- MANDATORY|DEFAULT|LOCAL
    value              jsonb NOT NULL,
    effective_from     timestamptz,
    effective_until    timestamptz,
    decision_record_id uuid REFERENCES decision_record,
    status             text NOT NULL
);

CREATE TABLE policy_binding (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_definition_id uuid NOT NULL REFERENCES policy_definition,
    target_type          text NOT NULL,
    target_id            uuid NOT NULL,
    propagates           boolean NOT NULL DEFAULT true
);


-- ─────────────────────────────────────────────────────────────
-- CONSUMPTION (A2.9)
-- ─────────────────────────────────────────────────────────────

CREATE TABLE consumption_type (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL REFERENCES tenant,
    name              text NOT NULL,
    resource_id       uuid REFERENCES resource,   -- nullable
    obligates         boolean NOT NULL DEFAULT false,  -- opt-in. ADR-097
    recurrence        text,          -- DAILY|WEEKLY|PER_OCCURRENCE
    record_mode       text NOT NULL, -- SELF|STAFF|EITHER
    correction_window interval,      -- null = unbounded
    status            text NOT NULL,
    CHECK (NOT obligates OR recurrence IS NOT NULL)
);

-- One row per person per type. NOT one per period. ADR-097
CREATE TABLE consumption_obligation (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenant,
    consumption_type_id uuid NOT NULL REFERENCES consumption_type,
    subject_person_id   uuid NOT NULL REFERENCES person,
    effective           tstzrange NOT NULL,
    source_type         text NOT NULL,   -- STAY|MEMBERSHIP|SUBSCRIPTION
    source_id           uuid NOT NULL,
    EXCLUDE USING gist (
        consumption_type_id WITH =,
        subject_person_id   WITH =,
        effective           WITH &&
    )
);
-- One obligation of a given type per person at a time.

CREATE TABLE consumption_option (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenant,
    consumption_type_id uuid NOT NULL REFERENCES consumption_type,
    code                text NOT NULL,
    name                text NOT NULL,
    charge_kind         text NOT NULL,          -- feeds ChargeComponent
    amount              numeric(14,2) NOT NULL,
    currency            char(3) NOT NULL,
    status              text NOT NULL,          -- AVAILABLE|WITHDRAWN
    UNIQUE (consumption_type_id, code)
);

-- Immutable. Corrections supersede. ADR-098
CREATE TABLE consumption_record (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenant,
    consumption_type_id uuid NOT NULL REFERENCES consumption_type,
    subject_person_id   uuid NOT NULL REFERENCES person,
    actor_person_id     uuid NOT NULL REFERENCES person,
    occurred_on         date NOT NULL,
    obligation_id       uuid REFERENCES consumption_obligation,
    allocation_id       uuid REFERENCES allocation,   -- realization. ADR-096
    variant_code        text,
    supersedes_id       uuid REFERENCES consumption_record,
    recorded_at         timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX one_live_record_per_subject_type_day
  ON consumption_record (consumption_type_id, subject_person_id, occurred_on)
  WHERE supersedes_id IS NULL;

CREATE TABLE consumption_record_option (
    consumption_record_id uuid NOT NULL REFERENCES consumption_record,
    consumption_option_id uuid NOT NULL REFERENCES consumption_option,
    PRIMARY KEY (consumption_record_id, consumption_option_id)
);

-- Relief flags snapshotted at declaration. ADR-099
CREATE TABLE expected_absence (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             uuid NOT NULL REFERENCES tenant,
    subject_person_id     uuid NOT NULL REFERENCES person,
    period                tstzrange NOT NULL,
    declared_by_person_id uuid NOT NULL REFERENCES person,
    relieves_recording    boolean NOT NULL,
    relieves_payment      boolean NOT NULL,
    decision_record_id    uuid REFERENCES decision_record,
    declared_at           timestamptz NOT NULL DEFAULT now(),
    withdrawn_at          timestamptz
);
CREATE INDEX expected_absence_subject_period
  ON expected_absence USING gist (subject_person_id, period);


-- ─────────────────────────────────────────────────────────────
-- INFRASTRUCTURE (A2.10)
-- ─────────────────────────────────────────────────────────────

CREATE TABLE authorization_outbox (
    id             bigserial PRIMARY KEY,
    aggregate_type text NOT NULL,
    aggregate_id   uuid NOT NULL,
    event_type     text NOT NULL,
    payload        jsonb NOT NULL,
    fence_subject  text,          -- the fact this row projects, at domain
    fence_relation text,          -- level. The dispatcher renders the
    fence_object   text,          -- tuple. ADR-101
    created_at     timestamptz NOT NULL DEFAULT now(),
    dispatched_at  timestamptz,
    voided_at      timestamptz,   -- a revocation overtook this row. ADR-101
    attempts       int NOT NULL DEFAULT 0,
    last_error     text,
    CONSTRAINT fence_all_or_none CHECK (
        num_nonnulls(fence_subject, fence_relation, fence_object) IN (0, 3)),
    CONSTRAINT voided_not_dispatched CHECK (
        voided_at IS NULL OR dispatched_at IS NULL)
);
CREATE INDEX outbox_pending
  ON authorization_outbox (created_at)
  WHERE dispatched_at IS NULL AND voided_at IS NULL;
-- Rows pending beyond threshold are an operational alert:
-- a stalled dispatcher means authority is not being granted.
CREATE INDEX outbox_fence
  ON authorization_outbox (fence_subject, fence_relation, fence_object)
  WHERE dispatched_at IS NULL AND voided_at IS NULL;
-- Revocation voids pending rows for its fence key under
-- pg_advisory_xact_lock, before deleting the tuple. ADR-101.
-- This table projects authorization facts only; it is not the
-- inter-context event bus of 05.0.8.

CREATE TABLE audit_event (
    id                    bigserial PRIMARY KEY,
    tenant_id             uuid,      -- null for platform-plane
    actor_principal_id    uuid,
    delegated_identity_id uuid,      -- impersonation. ADR-068
    action                text NOT NULL,
    object_type           text,
    object_id             uuid,
    outcome               text NOT NULL,
    severity              text NOT NULL,
    context               jsonb,
    occurred_at           timestamptz NOT NULL DEFAULT now()
);
-- Append-only. No UPDATE, no DELETE. Outlives SCRUBBED persons,
-- which is why it holds identifiers rather than names.
-- A2.10 marks this partitioned monthly on occurred_at. It is not
-- partitioned here: partitioning is a scale decision and C3 records
-- that there are no scale figures yet. Doing it now would fix a
-- partition key before anyone knows the retention policy.


-- ─────────────────────────────────────────────────────────────
-- INDICATIVE INDEXING (A2.12)
-- "indicative rather than tuned" — A2 preamble.
-- ─────────────────────────────────────────────────────────────

CREATE INDEX membership_tenant_person_state
  ON membership (tenant_id, person_id, state);
CREATE INDEX allocation_resource_period
  ON allocation USING gist (resource_id, period);
CREATE INDEX booking_tenant_resource_state
  ON booking (tenant_id, resource_id, state);
CREATE INDEX authorization_edge_child
  ON authorization_edge (child_type, child_id);
CREATE INDEX authorization_edge_parent
  ON authorization_edge (parent_type, parent_id);
CREATE INDEX verification_subject
  ON verification (subject_type, subject_id, type, status);
CREATE INDEX restriction_person_live
  ON restriction (person_id) WHERE lifted_at IS NULL;
CREATE INDEX audit_event_tenant_time
  ON audit_event (tenant_id, occurred_at DESC);
CREATE INDEX consumption_record_tenant_subject_day
  ON consumption_record (tenant_id, subject_person_id, occurred_on);


-- ─────────────────────────────────────────────────────────────
-- ROW LEVEL SECURITY (A2.1)
--
-- Applied to every table carrying tenant_id, with four exemptions.
-- The list is spelled out rather than derived from the catalogue, so
-- that adding a tenant-scoped table without an RLS decision shows up
-- as an omission in a diff rather than being silently swept in.
--
-- EXEMPT, deliberately global (08.2, A2.1):
--     person, principal, guardianship  — no tenant_id at all
--     restriction                      — has tenant_id, still exempt;
--                                        the one cross-tenant
--                                        safeguarding flow, 05.9.9
--
-- NO POLICY because the table carries no tenant_id (see H1):
--     entitlement_grant, membership_dependent, membership_suspension,
--     subscription, price_component, charge, charge_component, party,
--     policy_definition, policy_binding, consumption_record_option,
--     authorization_outbox
-- ─────────────────────────────────────────────────────────────

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'location',
        'organizational_unit',
        'authorization_edge',
        'consent_grant',
        'decision_record',
        'verification',
        'clearance',
        'invoice',
        'invoice_number_series',
        'payment',
        'entitlement_bundle',
        'governance_profile',
        'membership_plan',
        'membership_application',
        'membership',
        'resource_type',
        'resource',
        'access_window',
        'allocation',
        'booking',
        'stay',
        'programme',
        'offering',
        'enrollment',
        'occurrence',
        'attendance',
        'office',
        'office_holding',
        'consumption_type',
        'consumption_obligation',
        'consumption_option',
        'consumption_record',
        'expected_absence',
        'audit_event'
    ]
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        -- A2.1, ADR-108. Without FORCE the table owner — whoever ran this
        -- migration — reads and writes every tenant's rows, silently.
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY tenant_isolation ON %I'
            ' USING (tenant_id = current_setting(''app.tenant_id'')::uuid)', t);
    END LOOP;
END $$;
-- +goose StatementEnd


-- ─────────────────────────────────────────────────────────────
-- APPLICATION ROLE (A2.1, ADR-108)
--
-- RLS does not apply to superusers, and it does not apply to roles with
-- BYPASSRLS. If the API connects as the cluster superuser then every
-- policy above is decoration and tenant isolation — the design's central
-- claim — is untested in every environment including production.
--
-- `ymca_app` is NOLOGIN and holds only DML. The login role is created
-- out of band and granted membership in it, so that no password appears
-- in a migration file. See backend/README.md and ADR-108.
-- ─────────────────────────────────────────────────────────────

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ymca_app') THEN
        CREATE ROLE ymca_app NOLOGIN;
    END IF;
END $$;
-- +goose StatementEnd

GRANT USAGE ON SCHEMA public TO ymca_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO ymca_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO ymca_app;

-- audit_event is append-only (A2.10). No UPDATE, no DELETE — enforced by
-- privilege, not by convention.
REVOKE UPDATE, DELETE ON audit_event FROM ymca_app;

-- Later migrations create tables after this grant runs. Default privileges
-- cover them, for objects created by the role that runs migrations.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO ymca_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO ymca_app;
