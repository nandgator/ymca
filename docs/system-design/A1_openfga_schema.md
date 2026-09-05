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

    # Role-grantable. `role_assignment#holder` stands in the type
    # restriction of every permission a tenant may confer through a
    # role and nowhere else, so which permissions are delegable is
    # read off this file rather than out of prose. ADR-109, ADR-110
    define finance_reader: [principal, role_assignment#holder] or admin
    define safeguarding_reader: [principal, role_assignment#holder]
    define may_approve_membership: [principal, role_assignment#holder] or admin

    # person is global and carries no tenant_id (A2.1): nothing about
    # the row itself says which tenant may create one. This relation
    # is the "application-level relationship check" A2.1 promised and
    # never had — registering a person is an act over this tenant, not
    # over the (tenant-less) row. Role-grantable, same as the three
    # above. ADR-114, ADR-110
    define may_register_person: [principal, role_assignment#holder] or admin

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
    define member_read: [principal, role_assignment#holder] or member_read from auth_parent or admin
    define unit_close: [principal, role_assignment#holder] or admin


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
    # The subject, and the userset every role-grantable permission
    # names. Definition, scope, term window and clearance are
    # PostgreSQL facts (A2.7): they are resolved BEFORE the graph is
    # asked, and an assignment that is not effective is never
    # supplied as a contextual tuple. None of that is expressible
    # here, so none of it is declared here — an earlier draft
    # declared `definition` and `scope` and nothing read either.
    # ADR-069, ADR-070, ADR-087, ADR-109
    define subject: [principal]
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
    define may_close: [role_assignment#holder] or manager


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
    define may_manage: [role_assignment#holder] or manager

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
    define may_record_for_other: [principal, role_assignment#holder] or manager
    define may_correct: [principal, role_assignment#holder] or manager
    define may_read: [principal, role_assignment#holder] or manager
    define may_close_period: [role_assignment#holder] or manager


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

# ── The role path. SUPPLIED PER CHECK, NEVER STORED. ADR-109 ──
#
# Alice is Branch Secretary at Procter, and that role confers
# member_read. Both lines below are contextual tuples: PostgreSQL
# resolves them for the permission being checked, with the term
# window, the clearance precondition and any ACTING cover already
# applied in the query (A2.7). An assignment that is not effective
# is simply never supplied, so ADR-070's "inert the moment it
# expires" holds by construction rather than by a sweeper.
#
# Which definition and which scope are PostgreSQL facts and appear
# nowhere in the graph. The second line's object is the assignment's
# scope; the permission reaches descendants only where the relation
# itself propagates (ADR-014).
principal:alice               subject     role_assignment:ra-001
role_assignment:ra-001#holder member_read organizational_unit:bombay/procter

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

### Must ALLOW — the platform plane (ADR-005)

```txt
principal:platform-op    may_provision_tenant platform:main
```

The platform singleton is `platform:main`. There is exactly one: the platform
plane has no multi-tenancy of its own, so there is nothing to namespace it
against.

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

### Must ALLOW — the role path (ADR-109)

```txt
principal:alice            member_read        organizational_unit:bombay/procter
principal:alice            member_read        organizational_unit:bombay/procter/sub-unit
```

Both are already listed above and are repeated here for what they now prove.
Alice holds no direct `member_read` tuple. She is Branch Secretary at Procter,
and that entire path arrives as **contextual tuples** — resolved from
PostgreSQL for the permission being checked, with the term window and the
clearance precondition already applied in the query. The second line adds the
propagation hop: role → scope → `auth_parent`, which is the whole mechanism in
one assertion.

Verified against real drift, not merely observed to pass: removing the two
contextual tuples fails exactly these two assertions and no others.

### Must ALLOW — `may_register_person`, direct and via role (ADR-114)

```txt
principal:bombay-admin    may_register_person  tenant:bombay
```

The tenant's own admin reaches it through `or admin`, the same shape as
`may_approve_membership`. It also resolves through a role, exactly as
`finance_reader` does — supplied as a contextual tuple, never stored
(ADR-109, A1.8 rule 8):

```txt
principal:frontdesk       may_register_person  tenant:bombay
```

Front desk staff who take applications but do not admit members are the
ordinary case this route exists for (05.3.4).

### Must REFUSE — the grantable set (ADR-110)

Not denials. The model must reject these writes outright.

```txt
role_assignment:ra-666#holder owner    tenant:bombay
role_assignment:ra-666#holder admin    tenant:bombay
role_assignment:ra-666#holder member   tenant:bombay
role_assignment:ra-666#holder may_use  resource:bombay/procter-pool
```

Ownership and administration are the privilege-escalation route ADR-078
forbids, and `admin` propagates through the entire DAG. Membership and
entitlement reach a person through a membership, never through a job. None of
the four relations names `role_assignment#holder` in its type restriction, so
OpenFGA refuses the tuple rather than accepting one that happens not to
resolve.

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

# Invariant 9, the other direction — a tenant admin reaches no platform
# object. Proved separately from the line above: that one shows the
# platform plane cannot read tenant data, this one shows a tenant
# cannot reach platform lifecycle authority. Neither implies the other.
principal:bombay-admin may_provision_tenant platform:main

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

# may_register_person is not membership. ADR-114
principal:alice        may_register_person  tenant:bombay
```

The first block is the one that matters. If `natl-sec` ever resolves to
`member_read`, the design's central claim has failed and the change must be
rejected regardless of what else it enables.

`principal:alice`'s plain membership does not reach `may_register_person`:
holding the row a tenant grants for showing up (`member`) is not the same as
holding the relation a tenant grants for a job (`role_assignment#holder` or
`admin`). Entitlement and membership are excluded from the grantable set for
the same reason (ADR-110); this is that same boundary checked on the newest
relation.

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
8  A role assignment is never a stored tuple. The suite refuses
   to write one, and supplies the role path as contextual tuples
   exactly as `internal/authz` does. ADR-109.
9  A permission is role-grantable if and only if
   `role_assignment#holder` is in its type restriction here. The
   suite proves the complement: every relation in its `forbidden`
   block must be REFUSED by the model, not merely denied. ADR-110.
10 Migration 0002 seeds that same set into `grantable_permission`,
   so a foreign key can enforce it. A test fails if the two sets
   differ in either direction. ADR-110, A2.7.
```

Rule 5 is the enforcement point for ADR-018. A type with no tenant path is
either a platform-plane object or a defect, and the schema must say which.
`platform` is the type this exempts — the only one, and no longer a
hypothetical: `POST /platform/tenants` reaches `platform:main` for real
(ADR-113), so a defect in this exemption would now be a live one rather than
an unused branch of the rule.

Rules 8 and 9 exist for the same reason as rule 7, one round later. Rule 8 is
what keeps ADR-070 true: a stored role tuple outlives the term that justified
it, and removing it then needs a sweeper — the exact dependency the design
refuses. Rule 9 is what keeps ADR-078 true: the grantable set is a real
security boundary, and a boundary that lives in a comment is not one.

**Rule 10 exists because the set is now stated twice** — once here as a type
restriction, once in migration 0002 as seed data a foreign key points at. Two
statements of one set drift, and this pair drifts silently in the dangerous
direction: a permission seeded but not modelled becomes grantable in
PostgreSQL and unresolvable in OpenFGA, so a role confers something that
never works and nothing raises. The test reads both artifacts on a checkout,
before either has been applied anywhere.

**Rule 9's negative form is deliberate.** Asserting that a forbidden role
tuple produces a DENY would be weaker than it looks: a DENY means the tuple
was accepted and merely failed to resolve, which one careless edit turns into
an ALLOW. Requiring the write to be _refused_ means the model itself will not
hold the tuple at all.

Rule 7 exists because rules 1 to 6 were all obeyed and the model still did
not parse. A1.2 was wrapped across lines for readability, A1.6 was missing
two tuples without which A1.7's first assertion could not resolve, and
`entitlement_bundle.via_plan` was declared and read by nothing — none of
which a human reader had caught in review. Prose cannot enforce prose. The
check runs in CI with the assertions.
