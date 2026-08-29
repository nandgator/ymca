# A1 — OpenFGA Authorization Model

The concrete FGA schema. This is a versioned artifact with its own test
suite; no change ships without assertions.

Written in the OpenFGA DSL. Comments mark the decisions each construct
realizes.

---

## A1.1 Reading this schema

Four conventions used throughout:

```txt
ONE LINE PER DEFINE
    The OpenFGA DSL has no line continuation. A relation wrapped
    across lines for readability is a syntax error, not a style
    choice. A1.2 was written wrapped and did not parse until
    2026-08-29; see A1.8 rule 7 for what now prevents a repeat.

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

    # Internal administration of this tenant, by its own admins or
    # by break-glass. NOT the cross-tenant authority verb of the
    # same sense in 05.1.5 — that one is granted on `affiliation`
    # and reaches nothing. Named apart so the two cannot be
    # confused by a later edit. ADR-102
    define administered_by: admin or jit_grantee


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

    define admin: [principal] or admin from auth_parent or administered_by from tenant

    define member: [principal] or member from auth_parent

    define staff: [principal]

    # Per-permission propagation. ADR-014
    # member_read reaches descendants; unit_close does not.
    define member_read: [principal] or member_read from auth_parent or admin
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

    # The two reaching verbs. Recorded, never graph-bearing: they
    # are grantable so that holding them is auditable, and they
    # resolve to nothing. Real administration or data access goes
    # through cross_tenant_grant — scoped, reasoned, expiring.
    # Their inertness is enforced by the A1.7 assertions, not by
    # anyone remembering this comment. ADR-102
    define may_administer: [principal]
    define may_read_member_data: [principal]

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

type membership_plan
  relations
    define tenant: [tenant]
    # The plan link, in the direction authorization can travel.
    # OpenFGA traverses forward only, so the fact PostgreSQL stores
    # as membership.plan_id is written here as the plan naming its
    # memberships. One tuple per membership, written at admission.
    # ADR-107
    define covered_member: [membership#covered]

type subscription
  relations
    define membership: [membership]
    define beneficiary: covered from membership

type entitlement_bundle
  relations
    define tenant: [tenant]
    # Every route by which a person holds this bundle. Both are
    # real and they coexist within one tenant, on one person:
    # a category whose fee includes the pool grants by plan, a
    # monthly sports add-on grants by subscription. ADR-107
    define via_plan: [membership_plan]
    define via_subscription: [subscription]
    define beneficiary: [person] or beneficiary from via_subscription or covered_member from via_plan


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

    define manager: [principal] or admin from auth_parent
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
# CONSUMPTION — entitlement only. Records, obligations and
# absences are business state. ADR-096, ADR-097
# ─────────────────────────────────────────────────────────────

type consumption_type
  relations
    define tenant: [tenant]
    define auth_parent: [organizational_unit, resource]

    define manager: [principal] or admin from auth_parent
    define entitled: [entitlement_bundle#beneficiary]

    # Self-service recording follows entitlement, exactly as
    # resource use does. Recording FOR someone else is staff work.
    define may_record: entitled or manager
    define may_record_for_other: [principal] or manager
    define may_correct: [principal] or manager
    define may_read: [principal] or manager
    define may_close_period: manager


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
path to a member record. This is what invariant 2 in 8.12 tests, and it holds
because of the shape of the model rather than because of discipline.

---

## A1.4 Per-permission propagation

ADR-014 required that propagation be declared per permission. In the DSL this
is the difference between:

```dsl
# propagates through the DAG
define member_read: [principal] or member_read from auth_parent or admin

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

| Absent                        | Why                                                                                                                 | Lives in          |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------- | ----------------- |
| Membership suspension         | business state, changes for financial reasons                                                                       | PostgreSQL        |
| Booking, stay, allocation     | business state with time periods                                                                                    | PostgreSQL        |
| Term windows                  | temporal; would need clock-driven tuple rewrites                                                                    | PostgreSQL        |
| Clearance validity            | same                                                                                                                | PostgreSQL        |
| Restriction contents          | only readability is gated here                                                                                      | PostgreSQL        |
| Policy values                 | resolved and evaluated by the domain                                                                                | PostgreSQL        |
| Enrollment                    | business fact with price and state                                                                                  | PostgreSQL        |
| Which plan a membership is on | the _fact_ is business state; `membership_plan.covered_member` carries only the direction authorization must travel | PostgreSQL        |
| Consumption records           | business state; one per person per day                                                                              | PostgreSQL        |
| Obligations                   | derived at billing time, not authority                                                                              | PostgreSQL        |
| Expected absence              | business state; relieves billing, not entitlement                                                                   | PostgreSQL        |
| Invoice contents              | business state                                                                                                      | PostgreSQL        |
| Any PII                       | ADR-030                                                                                                             | nowhere in tuples |
| `but not`                     | negative scoping prohibited                                                                                         | ADR-017           |

The unifying rule: **if it changes for business reasons, it is not
authority.**

---

## A1.6 Tuple examples

Written `user relation object` throughout — the order OpenFGA itself uses.
Earlier drafts of this section mixed that with `object relation user`, which
is how a fixture ends up written backwards.

```txt
# Alice is a member of YMCA Bombay
principal:alice member tenant:bombay

# Alice is Branch Secretary at Procter
role_definition:branch-secretary definition role_assignment:ra-001
principal:alice                  subject    role_assignment:ra-001
organizational_unit:bombay/procter scope    role_assignment:ra-001

# The pool has two authorization parents (DAG)
organizational_unit:bombay/procter auth_parent resource:bombay/procter-pool
organizational_unit:bombay/pe-dept auth_parent resource:bombay/procter-pool

# ── The two entitlement routes, both real. ADR-107 ──

# BY SUBSCRIPTION — a monthly add-on bought on top of a category.
# Note the direction: the BUNDLE names its subscription. The reverse
# is not expressible, because OpenFGA traverses forward only.
person:alice          holder           membership:m-123
membership:m-123      membership       subscription:sub-77
subscription:sub-77   via_subscription entitlement_bundle:pool-access
entitlement_bundle:pool-access#beneficiary entitled resource:bombay/procter-pool

# BY PLAN — a category whose fee includes the facility. One tuple
# per membership, one per plan-bundle. Amending what the category
# includes moves every member on it and touches no member's tuples.
membership:m-123#covered        covered_member membership_plan:bombay/metropolitan
membership_plan:bombay/metropolitan via_plan   entitlement_bundle:gym-access
entitlement_bundle:gym-access#beneficiary entitled resource:bombay/procter-gym

# Alice now holds both routes at once, which is the ordinary case
# wherever a base category is sold alongside facility add-ons.

# National body holds every authority verb over Bombay.
# This is the strong form of invariant 2: not a body with one
# verb, but a body with all five, still reaching nothing.
principal:natl-sec may_set_policy        affiliation:india-bombay
principal:natl-sec may_review_compliance affiliation:india-bombay
principal:natl-sec may_sanction          affiliation:india-bombay
principal:natl-sec may_administer        affiliation:india-bombay
principal:natl-sec may_read_member_data  affiliation:india-bombay
tenant:india       from_tenant           affiliation:india-bombay
tenant:bombay      to_tenant             affiliation:india-bombay

# Break-glass, on the ELEVATED principal, expiry in PostgreSQL
principal:bob-elevated jit_grantee tenant:bombay
```

**Subjects are typed, and the type is not decoration.** `person:alice` and
`principal:alice` are different subjects reaching different relations.
Entitlement flows to a **person** — a membership covers a human, not a login.
Authority is held by a **principal**, because the same human acting personally
and acting with elevated authority must be distinguishable (ADR-106). A1.7
spells the type out on every line for this reason.

---

## A1.7 Assertions

The model's test suite. These run in CI; a failure blocks the change.

Every line is `user relation object`, and the user's type is part of it —
`person:alice` and `principal:alice` are different subjects (A1.6). The suite
that runs these lives at `backend/fga/assertions.yaml`; A1.8 rule 7 makes the
two impossible to drift apart.

### Must ALLOW

```txt
person:alice             may_use              resource:bombay/procter-pool
person:alice             may_record           consumption_type:bombay/dinner
principal:warden         may_record_for_other consumption_type:bombay/dinner
principal:alice          member_read          organizational_unit:bombay/procter
principal:alice          member_read          organizational_unit:bombay/procter/sub-unit
principal:pe-head        may_close            resource:bombay/procter-pool
principal:procter-admin  may_close            resource:bombay/procter-pool
principal:natl-sec       may_sanction         affiliation:india-bombay
principal:natl-sec       may_administer       affiliation:india-bombay
principal:bob-elevated   administered_by      tenant:bombay
```

### Must ALLOW — the two entitlement routes (ADR-107)

```txt
person:alice             may_use              resource:bombay/procter-gym
person:alice             may_use              resource:bombay/procter-pool
```

The first reaches the gym through the plan alone and the second reaches the
pool through a subscription alone, on the same person. If either stops
resolving, one of the two routes has been lost.

### Must ALLOW — proof the fixture is not empty

```txt
principal:bombay-secretary member_read        organizational_unit:bombay/procter
principal:bombay-finance   viewer             invoice:bombay/inv-1
person:priya               may_use            resource:pune/central-pool
```

These exist only to keep the DENY assertions below honest. A negative test
against a fixture that grants nothing passes for the wrong reason — which is
exactly the weakness found in 8.12 test 2. Each one is the positive twin of a
DENY: the same relation, on the same object, for a subject who _should_ reach
it.

### Must DENY — the invariants

```txt
# Invariant 2 — the central claim of the design.
# natl-sec holds all five verbs (A1.6) and reaches nothing.
principal:natl-sec     member_read          organizational_unit:bombay/procter
principal:natl-sec     may_use              resource:bombay/procter-pool
principal:natl-sec     viewer               invoice:bombay/inv-1
principal:natl-sec     admin                organizational_unit:bombay/procter
principal:natl-sec     administered_by      tenant:bombay
principal:natl-sec     member               tenant:bombay
principal:natl-sec     finance_reader       tenant:bombay
principal:natl-sec     safeguarding_reader  tenant:bombay
principal:natl-sec     may_record           consumption_type:bombay/dinner

# Invariant 9 — platform admin reaches no tenant object
principal:platform-op  member_read          organizational_unit:bombay/procter
principal:platform-op  viewer               invoice:bombay/inv-1
principal:platform-op  may_use              resource:bombay/procter-pool

# Invariant 1 — no cross-tenant leakage
person:alice           may_use              resource:pune/central-pool
principal:alice        member_read          organizational_unit:pune/central
person:alice           may_record           consumption_type:pune/dinner

# Entitlement does not confer recording for others
principal:alice        may_record_for_other consumption_type:bombay/dinner

# One route does not confer the other's bundle. ADR-107
person:dilip           may_use              resource:bombay/procter-gym
person:dilip           may_use              resource:bombay/procter-pool

# Per-permission propagation (ADR-014)
principal:bombay-secretary unit_close       organizational_unit:bombay/procter

# Personal principal holds no elevated authority
principal:bob-personal administered_by      tenant:bombay
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
7  A1.2 is the source of `backend/fga/model.fga`, and A1.7 of the
   assertions actually run. A test fails if the model differs by
   one character, or if any assertion listed in A1.7 is missing
   from the suite or carries a different expectation.
```

Rule 5 is the enforcement point for ADR-018. A type with no tenant path is
either a platform-plane object or a defect, and the schema must say which.

Rule 7 exists because rules 1 to 6 were all obeyed and the model still did
not parse. A1.2 was wrapped across lines for readability, A1.6 was missing
two tuples without which A1.7's first assertion could not resolve, and
`entitlement_bundle.via_plan` was declared and read by nothing — none of
which a human reader had caught in review. Prose cannot enforce prose. The
check runs in CI with the assertions.
