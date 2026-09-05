# A2 — Data Model

The PostgreSQL schema sketch. Implementation-ready in shape; indexes and
partitioning are indicative rather than tuned.

Conventions: `id uuid primary key default gen_random_uuid()`,
`timestamptz` throughout stored UTC, `tstzrange` for periods, soft states
rather than deletes.

---

## A2.1 Conventions

Tables here are grouped by bounded context, which is not a valid creation
order — `guardianship` precedes the `verification` it references, `membership`
precedes `decision_record`, `booking` precedes `invoice`. The migration
orders them topologically.

### Row level security

```sql
ALTER TABLE <t> ENABLE ROW LEVEL SECURITY;
ALTER TABLE <t> FORCE  ROW LEVEL SECURITY;   -- see below
CREATE POLICY tenant_isolation ON <t>
  USING (tenant_id = current_setting('app.tenant_id')::uuid);
```

`FORCE` is not optional and its absence is silent. Without it the table's
**owner** — whoever ran the migration — is exempt from the policy and reads
every tenant's rows. Nothing fails; isolation simply is not there.

`ENABLE` and `FORCE` together are still not enough, because RLS does not
apply to a superuser or to any role holding `BYPASSRLS`. **The application
must connect as neither.** Two roles, and they are not interchangeable
(ADR-108):

```txt
migration role   superuser or table owner; needs DDL; exempt from RLS
application role no superuser, no BYPASSRLS, DML only; subject to RLS
```

A deployment that runs the API as its migration role has a schema full of
policies and no isolation. This is the design's central claim, so it is worth
stating that it rests on a role grant rather than on the schema alone.

### Every connection names its tenant

`current_setting('app.tenant_id')` is written without the `missing_ok`
argument deliberately. A connection that has not set it **raises** rather than
returning zero rows: a query that forgot its tenant is a defect, and failing
loudly is better than returning an empty result the caller reads as "none".

The cost is that every pooled connection must set the value before touching a
tenant-scoped table, including after the pool resets it.

```txt
EXEMPT FROM RLS — deliberately global (08.2)
    person, principal, guardianship, restriction
    Protected by application-level relationship checks.

NO POLICY, because the table carries no tenant_id
    entitlement_grant, membership_dependent, membership_suspension,
    subscription, price_component, charge, charge_component, party,
    policy_definition, policy_binding, consumption_record_option,
    authorization_outbox, role_permission, role_required_clearance,
    office_conferred_role

    These are tenant-scoped through a parent row, not by a column, so
    the policy above cannot be written for them. `charge` and
    `charge_component` are the ones that matter: an invoice id alone
    reaches its contents. Known gap, 11.2.

    The three role tables reach their tenant through role_definition
    or office, both of which carry tenant_id and are themselves
    policed. Reading them still requires having reached the parent.

PLATFORM-DEFINED, no tenant_id by design
    grantable_permission — the ADR-110 set; permissions are
    system-defined and immutable (ADR-076)
    restriction_kind_permission — the meaning of a restriction kind
    cannot vary by tenant, because a restriction may be platform-wide
    (05.9.9)

    Both are readable by every tenant connection and writable by
    neither: INSERT, UPDATE and DELETE are revoked from the
    application role in migration 0002.
```

Three tables — `verification`, `clearance`, `audit_event` — have a **nullable**
`tenant_id` for deliberately global rows. Under the policy above `NULL = <uuid>`
is NULL, so those global rows are invisible to every tenant connection. Reading
them needs a path this schema does not provide. Known gap, 11.2.

Required extensions:

```sql
CREATE EXTENSION IF NOT EXISTS btree_gist;   -- allocation exclusion
CREATE EXTENSION IF NOT EXISTS pgcrypto;     -- gen_random_uuid
```

`pgcrypto` is redundant on PostgreSQL 13 and later, which provide
`gen_random_uuid()` in core. Kept for older targets.

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

-- Referenced by organizational_unit and resource. Physical containment
-- only: it carries no authorization meaning whatever. ADR-095 keeps
-- organizational, physical and authorization containment separate, and
-- this is the physical one.
CREATE TABLE location (
    id           uuid PRIMARY KEY,
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

## A2.7 Governance and roles

The role tables are what 6.1 step 2 queries. Nothing here is projected into
OpenFGA: the graph learns of an assignment only as a contextual tuple, for one
permission, at the moment of a check (ADR-109).

```sql
-- A tenant-configurable bundle of system-defined permissions.
-- ADR-011, ADR-022, ADR-076
CREATE TABLE role_definition (
    id            uuid PRIMARY KEY,
    tenant_id     uuid NOT NULL REFERENCES tenant,
    code          text NOT NULL,
    name          text NOT NULL,
    term_policy   text NOT NULL,   -- PERMANENT|OPTIONAL_TERM|MANDATORY_TERM
                                   -- ADR-069
    youth_facing  boolean NOT NULL DEFAULT false,   -- 05.9.3
    status        text NOT NULL,   -- ACTIVE|RETIRED
    UNIQUE (tenant_id, code)
);
-- Cloned from a template, never linked to one. ADR-079

-- The grantable set of ADR-110, as a table so that a foreign key can point
-- at it. A1.2 remains authoritative: a permission belongs here exactly when
-- `role_assignment#holder` is in its type restriction, and a test fails if
-- the two sets ever differ (A1.8 rule 10). Platform-defined — permissions
-- are system-defined and immutable (ADR-076) — so no tenant_id.
CREATE TABLE grantable_permission (
    permission text PRIMARY KEY
);

-- The bundle. The foreign key IS ADR-110: a role cannot be given a
-- permission the model would refuse to resolve for it. Declarative rather
-- than a trigger, so a bulk load cannot slip past it.
CREATE TABLE role_permission (
    role_definition_id uuid NOT NULL REFERENCES role_definition,
    permission         text NOT NULL REFERENCES grantable_permission,
    PRIMARY KEY (role_definition_id, permission)
);

-- ADR-087. An assignment whose definition requires a clearance the person
-- does not currently hold is inert, exactly as an expired term is.
CREATE TABLE role_required_clearance (
    role_definition_id uuid NOT NULL REFERENCES role_definition,
    verification_type  text NOT NULL,   -- BACKGROUND_CHECK|REGISTRY_SCREENING
    PRIMARY KEY (role_definition_id, verification_type)
);

CREATE TABLE role_assignment (
    id                   uuid PRIMARY KEY,
    tenant_id            uuid NOT NULL REFERENCES tenant,
    role_definition_id   uuid NOT NULL REFERENCES role_definition,
    subject_principal_id uuid NOT NULL REFERENCES principal,
    -- ADR-011: an assignment always carries a scope. scope_type must be the
    -- object type the permission names, so that the contextual tuple lands
    -- on an object that declares the relation. Reach beyond the scope is the
    -- graph's business, per-permission. ADR-014
    scope_type           text NOT NULL,   -- tenant|organizational_unit|
                                          -- resource|programme|consumption_type
    scope_id             uuid NOT NULL,
    valid_from           timestamptz NOT NULL DEFAULT now(),
    valid_until          timestamptz,
    holding_type         text NOT NULL DEFAULT 'SUBSTANTIVE',
                                          -- SUBSTANTIVE|ACTING. ADR-070
    substantive_assignment_id uuid REFERENCES role_assignment,
    via_office_holding_id uuid REFERENCES office_holding,
    granted_by_principal_id uuid NOT NULL REFERENCES principal,
    decision_record_id   uuid REFERENCES decision_record,
    revoked_at           timestamptz,
    CONSTRAINT acting_names_its_substantive CHECK (
        (holding_type = 'ACTING') = (substantive_assignment_id IS NOT NULL)),
    CONSTRAINT window_ordered CHECK (
        valid_until IS NULL OR valid_until > valid_from)
);
-- MANDATORY_TERM requires valid_until. Enforced by trigger rather than
-- CHECK: the policy lives on the referenced role_definition row, which a
-- CHECK constraint cannot reach. ADR-069

-- The office -> role arrow of 05.6.3, which had no table behind it.
-- Appointing to an office materializes one role_assignment per conferred
-- definition, carrying via_office_holding_id; vacating revokes them
-- together, which is what 05.6.4 means by "atomically". ADR-071
CREATE TABLE office_conferred_role (
    office_id          uuid NOT NULL REFERENCES office,
    role_definition_id uuid NOT NULL REFERENCES role_definition,
    PRIMARY KEY (office_id, role_definition_id)
);

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
    id          uuid PRIMARY KEY,
    invoice_id  uuid NOT NULL REFERENCES invoice,
    source_type text NOT NULL,   -- MEMBERSHIP|SUBSCRIPTION|ENROLLMENT|
                                 -- BOOKING|STAY|CONSUMPTION|DEPOSIT|
                                 -- DUES|ADJUSTMENT
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

-- What a restriction kind actually withholds. Without this, `kind` was a
-- label mapping to nothing and 6.1's restriction limb checked nothing at
-- all. Platform-defined, not tenant-configurable: a restriction imposed in
-- one association may be platform-wide (05.9.9), so its meaning cannot vary
-- by tenant. 8.8 requires a denial to name what it withholds, which is why
-- this is a mapping rather than a blanket deny.
CREATE TABLE restriction_kind_permission (
    kind       text NOT NULL,   -- as enumerated in 05.9.4
    permission text NOT NULL,   -- '<object_type>.<relation>'
    PRIMARY KEY (kind, permission)
);
```

### What each restriction kind withholds

Two of the four kinds act on the role path and are applied in 6.1 step 2 —
they suppress assignments rather than permissions, so they need no row here:

```txt
NO_ROLE_ASSIGNMENT        every assignment is ineffective
NO_YOUTH_FACING_ROLES     assignments whose definition is youth_facing
```

The other two withhold named permissions, and are applied in step 4:

```txt
NO_RESOURCE_ACCESS        resource.may_use, resource.may_book,
                          consumption_type.may_record
SUSPENDED_PENDING_REVIEW  the same, and every assignment as well
```

`SUSPENDED_PENDING_REVIEW` appears in both lists deliberately: standing a
person down means neither acting in a role nor using the facilities, and
05.9.4 is explicit that it carries no narrative to distinguish the two.

---

## A2.9 Consumption

```sql
CREATE TABLE consumption_type (
    id                uuid PRIMARY KEY,
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
    id                  uuid PRIMARY KEY,
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
    id                  uuid PRIMARY KEY,
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
    id                  uuid PRIMARY KEY,
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
    id                    uuid PRIMARY KEY,
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
CREATE INDEX ON expected_absence USING gist (subject_person_id, period);
```

---

## A2.10 Infrastructure

```sql
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

-- A3.6 requires an Idempotency-Key on every POST that creates money or a
-- consumption record, and requires the result to be stored so a repeat
-- returns the original response. A2 defined no table for it until R9; the
-- contract asked for storage the schema never provided.
--
-- The row is written INSIDE the transaction that does the work, carrying the
-- response. That is what makes it correct rather than merely helpful: effect
-- and record commit together or not at all, so there is no in-flight state to
-- reason about and a rolled-back attempt leaves nothing to replay. Two
-- concurrent identical requests both do the work; the second loses the
-- primary key race, rolls back, reads the winner's row and replays it.
CREATE TABLE idempotency_key (
    tenant_id      uuid NOT NULL REFERENCES tenant,
    key            text NOT NULL,          -- client-generated
    endpoint       text NOT NULL,          -- method + route pattern
    -- A key replayed against a different body is a client defect, not a hit.
    -- Returning the first response would silently discard the second request;
    -- this makes it a 409 instead. 8.8: the denial is specific.
    request_digest text NOT NULL,          -- sha256 of the request body
    principal_id   uuid NOT NULL REFERENCES principal,
    status_code    int NOT NULL,
    -- jsonb, not text: the database validates what it stores. The cost is
    -- that a replay is semantically the original response rather than
    -- byte-identical, because jsonb normalizes whitespace and key order.
    -- A3.6 states that consequence rather than leaving it to be discovered.
    response_body  jsonb NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, endpoint, key)
);

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

## A2.11 Where the invariants live

| Invariant                                               | Enforced by                                                        |
| ------------------------------------------------------- | ------------------------------------------------------------------ |
| No overlapping allocation                               | `EXCLUDE USING gist` on `allocation`                               |
| One active membership per tenant                        | partial unique index                                               |
| One elevated principal per person                       | partial unique index                                               |
| Cross-tenant grants expire                              | `NOT NULL` on `expires_at`                                         |
| One authority verb per pair                             | unique constraint                                                  |
| Party is exactly one kind                               | `CHECK (num_nonnulls(...) = 1)`                                    |
| Guardian ≠ minor                                        | `CHECK`                                                            |
| Affiliation not self-referential                        | `CHECK`                                                            |
| Tenant isolation                                        | RLS, four documented exemptions                                    |
| No cycles in the DAG                                    | recursive CTE, pre-commit                                          |
| One obligation per person per type at a time            | `EXCLUDE USING gist` on `consumption_obligation`                   |
| One live consumption record per person per type per day | partial unique index                                               |
| A row is never both dispatched and voided               | `CHECK` on `authorization_outbox`                                  |
| Gapless invoice numbering                               | counter row locked in the issuing txn. ADR-103                     |
| Tenant isolation survives the table owner               | `FORCE ROW LEVEL SECURITY`. ADR-108                                |
| Tenant isolation survives the connecting role           | application role has no superuser, no `BYPASSRLS`. ADR-108         |
| A query that forgot its tenant fails                    | `current_setting` without `missing_ok` raises                      |
| `audit_event` is append-only                            | `UPDATE` and `DELETE` revoked from the app role                    |
| An ACTING assignment names its substantive one          | `CHECK (holding_type = 'ACTING') = (substantive IS NOT NULL)`      |
| A MANDATORY_TERM assignment has an end date             | trigger; the policy lives on `role_definition`. ADR-069            |
| A role confers only role-grantable permissions          | `role_permission.permission` FK to `grantable_permission`. ADR-110 |
| An expired term cannot authorize                        | never supplied to the graph. ADR-109, not a constraint             |

Everything above is enforced by the database rather than by application
code, because application code can be bypassed by a code path that does not
exist yet.

---

## A2.12 Indicative indexing

```sql
CREATE INDEX ON membership (tenant_id, person_id, state);
CREATE INDEX ON allocation USING gist (resource_id, period);
CREATE INDEX ON booking (tenant_id, resource_id, state);
CREATE INDEX ON authorization_edge (child_type, child_id);
CREATE INDEX ON authorization_edge (parent_type, parent_id);
CREATE INDEX ON verification (subject_type, subject_id, type, status);
CREATE INDEX ON restriction (person_id) WHERE lifted_at IS NULL;
CREATE INDEX ON audit_event (tenant_id, occurred_at DESC);
CREATE INDEX ON idempotency_key (created_at);   -- for expiry sweeps
CREATE INDEX ON consumption_record (tenant_id, subject_person_id, occurred_on);

-- 6.1 step 2 runs on every check. This is the index it must hit.
CREATE INDEX ON role_assignment (subject_principal_id, tenant_id)
  WHERE revoked_at IS NULL;
CREATE INDEX ON role_permission (permission);
CREATE INDEX ON role_assignment (substantive_assignment_id)
  WHERE substantive_assignment_id IS NOT NULL;
CREATE INDEX ON role_assignment (via_office_holding_id)
  WHERE via_office_holding_id IS NOT NULL;
```

`role_assignment (subject_principal_id, ...)` is the one index in this list
whose absence is a latency regression on **every authorized request**, not
just on a report. 10 sets a p99 for `check()`; step 2 is now inside it.

Partitioning candidates: `audit_event` monthly, `allocation` by tenant at
scale, `authorization_outbox` with aggressive archival of dispatched rows.
