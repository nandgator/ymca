# A2 — Data Model

The PostgreSQL schema sketch. Implementation-ready in shape; indexes and
partitioning are indicative rather than tuned.

Conventions: `id uuid primary key default gen_random_uuid()`,
`timestamptz` throughout stored UTC, `tstzrange` for periods, soft states
rather than deletes.

---

## A2.1 Conventions

```sql
-- Every tenant-scoped table carries tenant_id and enables RLS.
ALTER TABLE <t> ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON <t>
  USING (tenant_id = current_setting('app.tenant_id')::uuid);
```

```txt
EXEMPT FROM RLS — deliberately global (08.2)
    person, principal, guardianship, restriction
    Protected by application-level relationship checks.
```

Required extensions:

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;   -- allocation exclusion
CREATE EXTENSION IF NOT EXISTS pgcrypto;     -- gen_random_uuid
```

---

## A2.2 Organization

```sql
CREATE TABLE tenant (
    id              uuid PRIMARY KEY,
    legal_name      text NOT NULL,
    display_name    text NOT NULL,
    jurisdiction    text NOT NULL,       -- drives tax, screening, residency
    status          text NOT NULL,       -- ACTIVE|SUSPENDED|ARCHIVED
    created_at      timestamptz NOT NULL DEFAULT now()
    -- NO parent_id. Relationships to other tenants exist only
    -- as affiliation and authority_grant rows. Principle 2.
);

CREATE TABLE organizational_unit (
    id              uuid PRIMARY KEY,
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
    id              uuid PRIMARY KEY,
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
-- Depth bounded by policy, default 12.

CREATE TABLE affiliation (
    id              uuid PRIMARY KEY,
    from_tenant_id  uuid NOT NULL REFERENCES tenant,
    to_tenant_id    uuid NOT NULL REFERENCES tenant,
    state           text NOT NULL,
    since           date,
    last_reviewed_at timestamptz,
    CHECK (from_tenant_id <> to_tenant_id)
);

CREATE TABLE authority_grant (
    id              uuid PRIMARY KEY,
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
    id              uuid PRIMARY KEY,
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
```

---

## A2.3 Identity

```sql
-- Global. No tenant_id. See 05.2.2.
CREATE TABLE person (
    id                uuid PRIMARY KEY,
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
    id            uuid PRIMARY KEY,
    person_id     uuid NOT NULL REFERENCES person,
    idp_subject   text NOT NULL UNIQUE,
    kind          text NOT NULL,     -- PERSONAL|STAFF|ELEVATED
    label         text,
    status        text NOT NULL
);
CREATE UNIQUE INDEX one_elevated_per_person
  ON principal (person_id) WHERE kind = 'ELEVATED';

CREATE TABLE guardianship (
    id                uuid PRIMARY KEY,
    guardian_person_id uuid NOT NULL REFERENCES person,
    minor_person_id   uuid NOT NULL REFERENCES person,
    relationship_type text NOT NULL,
    verification_id   uuid NOT NULL REFERENCES verification,
    established_at    timestamptz NOT NULL,
    ends_at           timestamptz,
    revoked_at        timestamptz,
    CHECK (guardian_person_id <> minor_person_id)
);

CREATE TABLE consent_grant (
    id                  uuid PRIMARY KEY,
    tenant_id           uuid NOT NULL REFERENCES tenant,
    subject_person_id   uuid NOT NULL REFERENCES person,
    granted_by_person_id uuid NOT NULL REFERENCES person,
    purpose             text NOT NULL,   -- enumerated, never free text
    granted_at          timestamptz NOT NULL,
    expires_at          timestamptz,
    withdrawn_at        timestamptz,     -- withdrawal preserves the row
    evidence_reference  text
);
```

---

## A2.4 Membership

```sql
CREATE TABLE membership_plan (
    id                    uuid PRIMARY KEY,
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

CREATE TABLE governance_profile (
    id                    uuid PRIMARY KEY,
    tenant_id             uuid NOT NULL REFERENCES tenant,
    may_vote              boolean NOT NULL DEFAULT false,
    may_stand_for_election boolean NOT NULL DEFAULT false,
    may_hold_office       boolean NOT NULL DEFAULT false,
    eligibility_policy_id uuid
);

CREATE TABLE membership_application (
    id                 uuid PRIMARY KEY,
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
    id                    uuid PRIMARY KEY,
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
    id            uuid PRIMARY KEY,
    membership_id uuid NOT NULL REFERENCES membership,
    person_id     uuid NOT NULL REFERENCES person,
    relationship  text NOT NULL,
    added_at      timestamptz NOT NULL DEFAULT now(),
    removed_at    timestamptz
);

-- Explicit, never derived. ADR-047
CREATE TABLE membership_suspension (
    id              uuid PRIMARY KEY,
    membership_id   uuid NOT NULL REFERENCES membership,
    reason_category text NOT NULL,   -- ARREARS|CONDUCT|SAFEGUARDING|
                                     -- ADMINISTRATIVE
    issued_by       uuid NOT NULL,
    issued_at       timestamptz NOT NULL DEFAULT now(),
    effective_from  timestamptz NOT NULL,
    lifted_at       timestamptz
);

CREATE TABLE entitlement_bundle (
    id        uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenant,
    name      text NOT NULL
);

CREATE TABLE entitlement_grant (
    id           uuid PRIMARY KEY,
    bundle_id    uuid NOT NULL REFERENCES entitlement_bundle,
    target_type  text NOT NULL,   -- RESOURCE_TYPE|RESOURCE|
                                  -- PROGRAMME|ORG_UNIT
    target_id    uuid NOT NULL,
    access_level text NOT NULL    -- USE|BOOK|PRIORITY_BOOK
);

CREATE TABLE subscription (
    id                    uuid PRIMARY KEY,
    membership_id         uuid NOT NULL REFERENCES membership,
    entitlement_bundle_id uuid NOT NULL REFERENCES entitlement_bundle,
    state                 text NOT NULL,
    valid_from            date,
    valid_until           date
);
```

---

## A2.5 Resource and consumption

```sql
CREATE TABLE resource_type (
    id                  uuid PRIMARY KEY,
    tenant_id           uuid NOT NULL REFERENCES tenant,
    name                text NOT NULL,
    archetype           text NOT NULL,  -- BOOKABLE_SLOT|ALLOCATABLE_UNIT|
                                        -- WALK_IN_ONLY|NON_CONSUMABLE
    default_access_mode text
);

CREATE TABLE resource (
    id                 uuid PRIMARY KEY,
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
    id                   uuid PRIMARY KEY,
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
    id            uuid PRIMARY KEY,
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
    id                     uuid PRIMARY KEY,
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
    id                      uuid PRIMARY KEY,
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
```

---

## A2.6 Programme

```sql
CREATE TABLE programme (
    id          uuid PRIMARY KEY,
    tenant_id   uuid NOT NULL REFERENCES tenant,
    org_unit_id uuid REFERENCES organizational_unit,
    name        text NOT NULL,
    category    text,
    youth_facing boolean NOT NULL DEFAULT false,
    status      text NOT NULL
);

CREATE TABLE offering (
    id                    uuid PRIMARY KEY,
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
    id           uuid PRIMARY KEY,
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
    id                   uuid PRIMARY KEY,
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
    id                   uuid PRIMARY KEY,
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
    id            uuid PRIMARY KEY,
    tenant_id     uuid NOT NULL REFERENCES tenant,
    occurrence_id uuid NOT NULL REFERENCES occurrence,
    person_id     uuid NOT NULL REFERENCES person,
    state         text NOT NULL,
    recorded_by   uuid,
    recorded_at   timestamptz NOT NULL DEFAULT now()
);
```

---

## A2.7 Governance

```sql
CREATE TABLE office (
    id                    uuid PRIMARY KEY,
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
    id                     uuid PRIMARY KEY,
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

CREATE TABLE decision_record (
    id                 uuid PRIMARY KEY,
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
```

---

## A2.8 Policy, Finance, Safeguarding

```sql
CREATE TABLE policy_definition (
    id                 uuid PRIMARY KEY,
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
    id                   uuid PRIMARY KEY,
    policy_definition_id uuid NOT NULL REFERENCES policy_definition,
    target_type          text NOT NULL,
    target_id            uuid NOT NULL,
    propagates           boolean NOT NULL DEFAULT true
);

CREATE TABLE party (
    id              uuid PRIMARY KEY,
    kind            text NOT NULL,   -- PERSON|ORGANIZATION
    person_id       uuid REFERENCES person,
    organization_id uuid,
    tax_identifier  text,
    CHECK (num_nonnulls(person_id, organization_id) = 1)
);
-- Generic parties. ADR-082

CREATE TABLE invoice (
    id             uuid PRIMARY KEY,
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

CREATE TABLE charge (
    id          uuid PRIMARY KEY,
    invoice_id  uuid NOT NULL REFERENCES invoice,
    source_type text NOT NULL,
    source_id   uuid,
    description text
);

CREATE TABLE charge_component (
    id               uuid PRIMARY KEY,
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
    id                 uuid PRIMARY KEY,
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

CREATE TABLE verification (
    id                  uuid PRIMARY KEY,
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
    id                   uuid PRIMARY KEY,
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
```

---

## A2.9 Infrastructure

```sql
CREATE TABLE authorization_outbox (
    id             bigserial PRIMARY KEY,
    aggregate_type text NOT NULL,
    aggregate_id   uuid NOT NULL,
    event_type     text NOT NULL,
    payload        jsonb NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    dispatched_at  timestamptz,
    attempts       int NOT NULL DEFAULT 0,
    last_error     text
);
CREATE INDEX outbox_pending
  ON authorization_outbox (created_at) WHERE dispatched_at IS NULL;
-- Rows pending beyond threshold are an operational alert:
-- a stalled dispatcher means authority is not being granted.

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
-- Partitioned monthly on occurred_at.
```

---

## A2.10 Where the invariants live

| Invariant | Enforced by |
|---|---|
| No overlapping allocation | `EXCLUDE USING gist` on `allocation` |
| One active membership per tenant | partial unique index |
| One elevated principal per person | partial unique index |
| Cross-tenant grants expire | `NOT NULL` on `expires_at` |
| One authority verb per pair | unique constraint |
| Party is exactly one kind | `CHECK (num_nonnulls(...) = 1)` |
| Guardian ≠ minor | `CHECK` |
| Affiliation not self-referential | `CHECK` |
| Tenant isolation | RLS, four documented exemptions |
| No cycles in the DAG | recursive CTE, pre-commit |
| Gapless invoice numbering | see open item, 05.8.13 |

Everything above is enforced by the database rather than by application
code, because application code can be bypassed by a code path that does not
exist yet.

---

## A2.11 Indicative indexing

```sql
CREATE INDEX ON membership (tenant_id, person_id, state);
CREATE INDEX ON allocation USING gist (resource_id, period);
CREATE INDEX ON booking (tenant_id, resource_id, state);
CREATE INDEX ON authorization_edge (child_type, child_id);
CREATE INDEX ON authorization_edge (parent_type, parent_id);
CREATE INDEX ON verification (subject_type, subject_id, type, status);
CREATE INDEX ON restriction (person_id) WHERE lifted_at IS NULL;
CREATE INDEX ON audit_event (tenant_id, occurred_at DESC);
```

Partitioning candidates: `audit_event` monthly, `allocation` by tenant at
scale, `authorization_outbox` with aggressive archival of dispatched rows.
