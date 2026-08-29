# A1 — OpenFGA Authorization Model

The concrete FGA schema. This is a versioned artifact with its own test
suite; no change ships without assertions.

Written in the OpenFGA DSL. Comments mark the decisions each construct
realizes.

---

## A1.1 Reading this schema

Three conventions used throughout:

```txt
OBJECT IDS ARE NAMESPACED
    organizational_unit:bombay/procter
    resource:bombay/procter-pool
    Namespacing is defence in depth. The tenant relation is what
    actually enforces isolation; the prefix makes accidents visible.

NO NEGATIVE RELATIONS
    OpenFGA supports `but not`. This model does not use it (ADR-017).
    Any occurrence in a future change is a defect.

VALIDITY IS NOT HERE
    Terms, clearances and restrictions are checked against PostgreSQL
    at decision time (08.1). Their absence from this schema is
    deliberate, not an omission.
```

---

## A1.2 The model

```dsl
model
  schema 1.1

# ─────────────────────────────────────────────────────────────
# PLATFORM PLANE
# Separate from the tenant plane. Neither implies the other.
# ADR-005
# ─────────────────────────────────────────────────────────────

type platform
  relations
    define operator: [principal]
    define security_admin: [principal]
    define support: [principal]
    define auditor: [principal]

    # Platform authority over tenants is limited to lifecycle
    # and configuration. It confers NO tenant-data access.
    define may_provision_tenant: operator
    define may_suspend_tenant: operator or security_admin
    define may_read_platform_audit: auditor or security_admin


# ─────────────────────────────────────────────────────────────
# SUBJECTS
# ─────────────────────────────────────────────────────────────

type person
  relations
    define principal: [principal]
    # The only Person -> Person relation in the model. ADR-042
    define guardian: [person]

type principal
  relations
    define person: [person]


# ─────────────────────────────────────────────────────────────
# TENANT — the root of every authorization path
# ADR-004, ADR-018
# ─────────────────────────────────────────────────────────────

type tenant
  relations
    define owner: [principal]
    define admin: [principal] or owner
    define member: [principal] or admin

    # Break-glass and JIT access land here, on the ELEVATED
    # principal only, always with an expiry enforced in
    # PostgreSQL. ADR-066, ADR-067
    define jit_grantee: [principal]

    define finance_reader: [principal] or admin
    define safeguarding_reader: [principal]
    define may_administer: admin or jit_grantee


# ─────────────────────────────────────────────────────────────
# ORGANIZATIONAL UNIT
# auth_parent is the DAG edge. It is NOT derived from
# org_parent — see 05.1.3. ADR-016, ADR-095
# ─────────────────────────────────────────────────────────────

type organizational_unit
  relations
    define tenant: [tenant]

    # Multiple parents permitted. Union over all paths.
    define auth_parent: [organizational_unit, tenant]

    define admin: [principal]
                  or admin from auth_parent
                  or may_administer from tenant

    define member: [principal]
                   or member from auth_parent

    define staff: [principal]

    # Per-permission propagation. ADR-014
    # member_read reaches descendants; unit_close does not.
    define member_read: [principal]
                        or member_read from auth_parent
                        or admin
    define unit_close: [principal] or admin


# ─────────────────────────────────────────────────────────────
# AFFILIATION — a first-class object, not a relation on tenant.
#
# This is what makes sanction authority structurally incapable
# of reaching tenant data. ADR-009, ADR-073, ADR-074
# ─────────────────────────────────────────────────────────────

type affiliation
  relations
    define from_tenant: [tenant]
    define to_tenant: [tenant]

    # Granted ON the affiliation. There is no path from any of
    # these to to_tenant's members, resources or finances.
    define may_set_policy: [principal]
    define may_review_compliance: [principal]
    define may_sanction: [principal]

    define viewer: admin from from_tenant or admin from to_tenant


# ─────────────────────────────────────────────────────────────
# CROSS-TENANT GRANT
# The single sanctioned exception to Principle 1. Always
# scoped to one object, always expiring. ADR-019
# ─────────────────────────────────────────────────────────────

type cross_tenant_grant
  relations
    define grantor_tenant: [tenant]
    define grantee: [principal, organizational_unit#staff]
    define scope_object: [organizational_unit, resource, programme]
    define active: grantee


# ─────────────────────────────────────────────────────────────
# ROLES
# Definitions may be global; assignments are always scoped.
# ADR-011, ADR-022, ADR-076, ADR-079
# ─────────────────────────────────────────────────────────────

type role_definition
  relations
    define tenant: [tenant]
    define editor: admin from tenant

type role_assignment
  relations
    define definition: [role_definition]
    define subject: [principal]
    define scope: [organizational_unit, resource, programme, tenant]
    # Effectiveness — term window, clearance — is resolved in
    # PostgreSQL. ADR-069, ADR-070, ADR-087
    define holder: subject


# ─────────────────────────────────────────────────────────────
# OFFICE — constitutional position. Confers roles; is not one.
# ADR-071, ADR-072
# ─────────────────────────────────────────────────────────────

type office
  relations
    define tenant: [tenant]
    define org_unit: [organizational_unit]
    define holder: [principal]
    define confers: [role_definition]
    define appointer: admin from tenant

type committee
  relations
    define tenant: [tenant]
    define org_unit: [organizational_unit]
    define chair: [principal]
    define member: [principal] or chair or holder from ex_officio_office
    define ex_officio_office: [office]


# ─────────────────────────────────────────────────────────────
# MEMBERSHIP AND ENTITLEMENT
# The membership record itself lives in PostgreSQL. What lives
# here is who it covers and what it grants. ADR-024, ADR-057
# ─────────────────────────────────────────────────────────────

type membership
  relations
    define tenant: [tenant]
    define holder: [person]
    define dependent: [person]
    define covered: holder or dependent
    define plan: [membership_plan]

type membership_plan
  relations
    define tenant: [tenant]
    define grants_bundle: [entitlement_bundle]

type subscription
  relations
    define membership: [membership]
    define beneficiary: covered from membership
    define grants_bundle: [entitlement_bundle]

type entitlement_bundle
  relations
    define tenant: [tenant]
    # Every route by which a person holds this bundle.
    define via_plan: [membership_plan#grants_bundle]
    define via_subscription: [subscription]
    define beneficiary: [person] or beneficiary from via_subscription


# ─────────────────────────────────────────────────────────────
# RESOURCE
# Bookings, stays and allocations are NOT here. They are
# business state with periods. ADR-024, ADR-058
# ─────────────────────────────────────────────────────────────

type resource
  relations
    define tenant: [tenant]
    define org_unit: [organizational_unit]
    define auth_parent: [organizational_unit, resource]

    define manager: [principal]
                    or admin from auth_parent
    define entitled: [entitlement_bundle#beneficiary]

    define may_use: entitled or manager
    define may_book: entitled or manager
    define may_close: manager


# ─────────────────────────────────────────────────────────────
# PROGRAMME
# ─────────────────────────────────────────────────────────────

type programme
  relations
    define tenant: [tenant]
    define org_unit: [organizational_unit]
    define manager: [principal] or admin from org_unit
    define instructor: [principal]
    define entitled: [entitlement_bundle#beneficiary]
    define open_to_public: [person:*]

    define may_enrol: entitled or open_to_public
    define may_manage: manager

type offering
  relations
    define programme: [programme]
    define may_manage: may_manage from programme
    define may_enrol: may_enrol from programme


# ─────────────────────────────────────────────────────────────
# FINANCE — permissions are ordinary roles. Contents are not
# tuples. ADR-082
# ─────────────────────────────────────────────────────────────

type invoice
  relations
    define tenant: [tenant]
    define payer: [person, organizational_unit]
    define viewer: payer or finance_reader from tenant


# ─────────────────────────────────────────────────────────────
# SAFEGUARDING — only the restriction is here, and only so
# that reading it can be gated. ADR-089
# ─────────────────────────────────────────────────────────────

type restriction
  relations
    define subject: [person]
    define tenant: [tenant]
    define viewer: subject or safeguarding_reader from tenant


# ─────────────────────────────────────────────────────────────
# POLICY
# ─────────────────────────────────────────────────────────────

type policy_definition
  relations
    define owner_tenant: [tenant]
    define editor: admin from owner_tenant
    define binder: [principal]
```

---

## A1.3 Why `affiliation` is a type

The most important structural choice in the schema, and the least obvious.

The natural instinct is to put sanction authority on `tenant`:

```dsl
type tenant
  relations
    define may_sanction: [principal]     # ← WRONG
```

That places the relation on the object being sanctioned. From there, any
future relation added to `tenant` — a convenience `viewer`, a reporting
`reader` — risks becoming reachable from `may_sanction` through a path
nobody intended.

Placing it on `affiliation` makes the guarantee structural:

```txt
principal ──may_sanction──▶ affiliation ──to_tenant──▶ tenant

There is no relation FROM affiliation INTO tenant's members,
resources or finances. The path terminates.
```

A national body may hold every authority verb and still have no expressible
path to a member record. This is what invariant 2 in 8.11 tests, and it holds
because of the shape of the model rather than because of discipline.

---

## A1.4 Per-permission propagation

ADR-014 required that propagation be declared per permission. In the DSL this
is the difference between:

```dsl
# propagates through the DAG
define member_read: [principal]
                    or member_read from auth_parent
                    or admin

# does not propagate — local scope only
define unit_close: [principal] or admin
```

An association-level secretary holding `member_read` at the tenant reaches
every chapter's member records. The same secretary holding `unit_close`
reaches nothing below their own node.

Each permission's propagation is a deliberate line in this file, reviewable
in a diff. There is no global propagation switch.

---

## A1.5 What is deliberately absent

| Absent | Why | Lives in |
|---|---|---|
| Membership suspension | business state, changes for financial reasons | PostgreSQL |
| Booking, stay, allocation | business state with time periods | PostgreSQL |
| Term windows | temporal; would need clock-driven tuple rewrites | PostgreSQL |
| Clearance validity | same | PostgreSQL |
| Restriction contents | only readability is gated here | PostgreSQL |
| Policy values | resolved and evaluated by the domain | PostgreSQL |
| Enrollment | business fact with price and state | PostgreSQL |
| Invoice contents | business state | PostgreSQL |
| Any PII | ADR-030 | nowhere in tuples |
| `but not` | negative scoping prohibited | ADR-017 |

The unifying rule: **if it changes for business reasons, it is not
authority.**

---

## A1.6 Tuple examples

```txt
# Alice is a member of YMCA Bombay
principal:alice#... member    tenant:bombay

# Alice is Branch Secretary at Procter
role_assignment:ra-001 definition   role_definition:branch-secretary
role_assignment:ra-001 subject      principal:alice
role_assignment:ra-001 scope        organizational_unit:bombay/procter

# The pool has two authorization parents (DAG)
resource:bombay/procter-pool auth_parent organizational_unit:bombay/procter
resource:bombay/procter-pool auth_parent organizational_unit:bombay/pe-dept

# Alice's pool subscription entitles her
subscription:sub-77 membership      membership:m-123
subscription:sub-77 grants_bundle   entitlement_bundle:pool-access
resource:bombay/procter-pool entitled entitlement_bundle:pool-access#beneficiary

# National body may sanction — and only sanction
principal:natl-sec may_sanction affiliation:india-bombay
affiliation:india-bombay from_tenant tenant:india
affiliation:india-bombay to_tenant   tenant:bombay

# Break-glass, on the ELEVATED principal, expiry in PostgreSQL
principal:bob-elevated jit_grantee tenant:bombay
```

---

## A1.7 Assertions

The model's test suite. These run in CI; a failure blocks the change.

### Must ALLOW

```txt
alice          may_use        resource:bombay/procter-pool
alice          member_read    organizational_unit:bombay/procter
alice          member_read    organizational_unit:bombay/procter/sub-unit
pe-head        may_close      resource:bombay/procter-pool
procter-admin  may_close      resource:bombay/procter-pool
natl-sec       may_sanction   affiliation:india-bombay
bob-elevated   may_administer tenant:bombay
```

### Must DENY — the invariants

```txt
# Invariant 2 — the central claim of the design
natl-sec       member_read    organizational_unit:bombay/procter
natl-sec       may_use        resource:bombay/procter-pool
natl-sec       viewer         invoice:bombay/inv-1
natl-sec       may_administer tenant:bombay

# Invariant 9 — platform admin reaches no tenant object
platform-op    member_read    organizational_unit:bombay/procter
platform-op    viewer         invoice:bombay/inv-1
platform-op    may_use        resource:bombay/procter-pool

# Invariant 1 — no cross-tenant leakage
alice          may_use        resource:pune/central-pool
alice          member_read    organizational_unit:pune/central

# Per-permission propagation (ADR-014)
bombay-secretary  unit_close  organizational_unit:bombay/procter

# Personal principal holds no elevated authority
bob-personal   may_administer tenant:bombay
```

The first block is the one that matters. If `natl-sec` ever resolves to
`member_read`, the design's central claim has failed and the change must be
rejected regardless of what else it enables.

---

## A1.8 Model change discipline

```txt
1  Every new relation states which ADR it realizes.
2  Every new permission declares propagation explicitly.
3  No `but not`. Ever.
4  Every change adds assertions, including negative ones.
5  Every new type declares a path to `tenant`, or documents
   in this file why it is platform-plane.
6  Migrations are forward-only; tuple rewrites are batched
   and reversible.
```

Rule 5 is the enforcement point for ADR-018. A type with no tenant path is
either a platform-plane object or a defect, and the schema must say which.
