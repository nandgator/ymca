# 09 — Architecture Decisions

The decision log. Each record is traceable to the design exchange that
produced it via its **Q** reference.

**R** references are later review rounds rather than the original
interrogation. R1 is the design review of 2026-08-29, which read the whole
document against the reference tenant's actual requirement and found it
inexpressible (05.10).

Status values: **Accepted** · **Deferred** · **Rejected** · **Open**

---

## Index

| #                   | Decision                                                 | Q          |
| ------------------- | -------------------------------------------------------- | ---------- |
| [ADR-001](#adr-001) | Multi-tenant, centralized yet sovereign                  | 1          |
| [ADR-002](#adr-002) | Hybrid RBAC + ReBAC + ABAC                               | —          |
| [ADR-003](#adr-003) | OpenFGA as the authorization engine                      | 8r         |
| [ADR-004](#adr-004) | Tenancy is an immutable security boundary                | 1          |
| [ADR-005](#adr-005) | Separate platform and tenant authority planes            | 11         |
| [ADR-006](#adr-006) | Organizational relationship ≠ authority                  | 15         |
| [ADR-007](#adr-007) | Authority may be narrower than affiliation               | 16         |
| [ADR-008](#adr-008) | Authority may flow against organizational direction      | 17         |
| [ADR-009](#adr-009) | Five distinct authority verbs                            | 86         |
| [ADR-010](#adr-010) | Tenant membership is a relationship                      | 5          |
| [ADR-011](#adr-011) | Role definitions global, assignments scoped              | 7          |
| [ADR-012](#adr-012) | Scope is a separate construct from containment           | 8          |
| [ADR-013](#adr-013) | Scope assignment propagates to descendants               | 61         |
| [ADR-014](#adr-014) | Propagation is per-permission, not per-role              | 62         |
| [ADR-015](#adr-015) | Scope targets are heterogeneous                          | 63         |
| [ADR-016](#adr-016) | Authorization containment is a DAG                       | 64         |
| [ADR-017](#adr-017) | No scope exclusions                                      | 66         |
| [ADR-018](#adr-018) | Every scope path has a tenant ancestor                   | 67         |
| [ADR-019](#adr-019) | Cross-tenant authority is an explicit grant              | 68         |
| [ADR-020](#adr-020) | Ad-hoc scope collections deferred                        | 65         |
| [ADR-021](#adr-021) | Relationship-derived permissions permitted               | 9          |
| [ADR-022](#adr-022) | Roles are reusable permission bundles                    | 10         |
| [ADR-023](#adr-023) | Authorization vs domain eligibility split                | 13, 49     |
| [ADR-024](#adr-024) | OpenFGA knows only the authorization graph               | 14         |
| [ADR-025](#adr-025) | Postgres owns business state, OpenFGA owns relationships | 28         |
| [ADR-026](#adr-026) | Transactional outbox, not distributed transactions       | 29         |
| [ADR-027](#adr-027) | Graded failure modes on authorization unavailability     | 30         |
| [ADR-028](#adr-028) | Tenant isolation represented twice                       | 31         |
| [ADR-029](#adr-029) | PostgreSQL row-level security                            | 32         |
| [ADR-030](#adr-030) | No PII in authorization tuples                           | —          |
| [ADR-031](#adr-031) | Single OpenFGA store, tenant-namespaced                  | 90         |
| [ADR-032](#adr-032) | JWTs carry identity, not permissions                     | —          |
| [ADR-033](#adr-033) | Policy is a structured domain object                     | 18         |
| [ADR-034](#adr-034) | Mandatory / default / local policy levels                | 4          |
| [ADR-035](#adr-035) | Mandatory policy cannot be overridden                    | 20         |
| [ADR-036](#adr-036) | Inheritance semantics are per policy type                | 21         |
| [ADR-037](#adr-037) | Domain owns policy evaluation                            | 19         |
| [ADR-038](#adr-038) | Person is global, membership tenant-local                | 22         |
| [ADR-039](#adr-039) | Authentication identity is global                        | 23         |
| [ADR-040](#adr-040) | Multiple deliberate principals permitted                 | 24         |
| [ADR-041](#adr-041) | Person relationships are not subtypes                    | 37, 38, 39 |
| [ADR-042](#adr-042) | Guardianship is separate from dependency                 | 96         |
| [ADR-043](#adr-043) | Membership is distinct from enrollment                   | 40         |
| [ADR-044](#adr-044) | Membership belongs to exactly one tenant                 | 41         |
| [ADR-045](#adr-045) | Membership has a primary holder and dependents           | 75         |
| [ADR-046](#adr-046) | Membership admission is an approval workflow             | 76         |
| [ADR-047](#adr-047) | Suspension is explicit, never derived                    | 77         |
| [ADR-048](#adr-048) | No automatic age-band transition                         | 78         |
| [ADR-049](#adr-049) | Verification is a first-class object                     | 79         |
| [ADR-050](#adr-050) | Conferred memberships are distinct from purchased        | 80         |
| [ADR-051](#adr-051) | Governance rights are a separate entitlement axis        | 83         |
| [ADR-052](#adr-052) | Foreign membership recognition deferred                  | 42         |
| [ADR-053](#adr-053) | Programme / offering / enrollment / occurrence           | 43         |
| [ADR-054](#adr-054) | Offerings need not be commercial                         | 44         |
| [ADR-055](#adr-055) | Pricing is a separate value object                       | 45         |
| [ADR-056](#adr-056) | Resource is distinct from programme                      | 46, 47, 48 |
| [ADR-057](#adr-057) | Resource entitlement is a relationship                   | 50         |
| [ADR-058](#adr-058) | Booking and Stay are independent aggregates              | 55         |
| [ADR-059](#adr-059) | Allocation exclusivity enforced in the database          | 55c        |
| [ADR-060](#adr-060) | Access mode is time-scoped, not resource-scoped          | 56         |
| [ADR-061](#adr-061) | Walk-in capacity is not system-enforced                  | 57         |
| [ADR-062](#adr-062) | Waitlist status reserved, feature deferred               | 53, 59     |
| [ADR-063](#adr-063) | Hostel stay requires approval                            | 60         |
| [ADR-064](#adr-064) | Allocation granularity varies by room type               | 60         |
| [ADR-065](#adr-065) | Actor and beneficiary may differ                         | 82         |
| [ADR-066](#adr-066) | JIT privileged access, no standing superusers            | 25, 26     |
| [ADR-067](#adr-067) | Break-glass requires dual control                        | 26         |
| [ADR-068](#adr-068) | Impersonation preserves both principals                  | 27         |
| [ADR-069](#adr-069) | Term policy declared on the role definition              | 87         |
| [ADR-070](#adr-070) | Expiry evaluated at decision time                        | 87         |
| [ADR-071](#adr-071) | Office is distinct from Role                             | 88         |
| [ADR-072](#adr-072) | Office-holding in scope, elections out                   | 88         |
| [ADR-073](#adr-073) | Affiliation is a stateful relationship                   | 84         |
| [ADR-074](#adr-074) | Affiliation state is recorded; sanction is explicit      | 84         |
| [ADR-075](#adr-075) | Non-affiliated associations are first-class tenants      | 85         |
| [ADR-076](#adr-076) | Permissions system-defined, roles tenant-configurable    | 69         |
| [ADR-077](#adr-077) | Resource types declare behavioural archetypes            | 70         |
| [ADR-078](#adr-078) | No privilege escalation through role creation            | 71         |
| [ADR-079](#adr-079) | Role templates are cloned, not linked                    | 72         |
| [ADR-080](#adr-080) | Role edits show blast radius and emit audit              | 73         |
| [ADR-081](#adr-081) | Membership plans are tenant-local                        | 74         |
| [ADR-082](#adr-082) | Financial parties are generic                            | 89         |
| [ADR-083](#adr-083) | Composable charge components                             | 81         |
| [ADR-084](#adr-084) | Payment provider facade                                  | 11r        |
| [ADR-085](#adr-085) | Inter-organizational dues deferred                       | 89         |
| [ADR-086](#adr-086) | Store the verdict, not the evidence                      | 97         |
| [ADR-087](#adr-087) | Clearance is a precondition on role assignment           | 92         |
| [ADR-088](#adr-088) | Safeguarding compliance feeds affiliation standing       | 95         |
| [ADR-089](#adr-089) | Allegation records are out of scope                      | 97         |
| [ADR-090](#adr-090) | Member screening is jurisdiction-gated                   | 98         |
| [ADR-091](#adr-091) | Screening never auto-acts                                | 98         |
| [ADR-092](#adr-092) | Staff membership is tenant policy                        | 24r        |
| [ADR-093](#adr-093) | Retain everything, scrub PII, preserve audit             | 5          |
| [ADR-094](#adr-094) | Organizational units are one typed concept               | 34         |
| [ADR-095](#adr-095) | Org, physical and authorization containment differ       | 35, 36     |
| [ADR-096](#adr-096) | Consumption is realization, distinct from reservation    | R1         |
| [ADR-097](#adr-097) | Obligations are standing, never materialized             | R1         |
| [ADR-098](#adr-098) | Consumption records are corrected, never edited          | R1         |
| [ADR-099](#adr-099) | Absence relief is snapshotted at declaration             | R1         |
| [ADR-100](#adr-100) | Consumption enters the charge vocabulary                 | R1         |
| [ADR-101](#adr-101) | Async projection is fenced against synchronous removal   | R1         |
| [ADR-102](#adr-102) | The reaching verbs are recorded, never graph-bearing     | R1         |
| [ADR-103](#adr-103) | Invoice numbers come from a transactional counter        | R1         |
| [ADR-104](#adr-104) | Lists authorize the scope, not the row                   | R1         |
| [ADR-105](#adr-105) | The tenant is a path segment                             | R1         |
| [ADR-106](#adr-106) | Authentication is a port                                 | R1         |
| [ADR-107](#adr-107) | Entitlement reaches a person by plan or by subscription  | R5         |
| [ADR-108](#adr-108) | Tenant isolation rests on a role, not only on the schema | R5         |

---

## Authorization foundations

### ADR-001

**Multi-tenant, centralized yet sovereign** · Accepted · Q1

**Context.** The platform serves a federated movement of independent
associations. A single-tenant deployment per association would fragment the
movement; a fully centralized system would violate the legal independence of
its members.

**Decision.** One platform, many sovereign tenants. Each tenant owns and
administers its own people, memberships, resources and finances. The platform
operator retains limited, explicit global authority.

**Rationale.** This is not merely an architectural preference — it describes
the actual structure. In the US movement, each of roughly 800 local
associations is a separately incorporated 501(c)(3) with its own board,
budget and charter, while the national office sets direction and policy but
does not control local operations. Obligations flowing upward are narrow:
pay dues, refrain from discrimination, support the mission.

**Consequences.** Tenant isolation must be a genuine security boundary, not
a database partition. Platform administration must not imply tenant-data
access. Every subsequent decision inherits this constraint.

---

### ADR-002

**Hybrid RBAC + ReBAC + ABAC** · Accepted

**Context.** Pure RBAC produces role explosion in systems with hierarchical
organizations and inherited permissions. Pure ReBAC struggles with
contextual state. Pure ABAC becomes unauditable.

**Decision.** Combine all three, each for what it does well.

| Model | Used for                                     |
| ----- | -------------------------------------------- |
| RBAC  | Coarse roles within a scope                  |
| ReBAC | Membership, hierarchy, ownership, delegation |
| ABAC  | Context, state, attribute constraints        |

**Consequences.** The team must understand three models. In exchange, none
of them is stretched past its competence.

---

### ADR-003

**OpenFGA as the authorization engine** · Accepted · Q8 (round 8 preamble)

**Decision.** Use OpenFGA (Zanzibar-style) rather than building an
authorization engine.

**Rationale.** The relationship graph, inheritance, and consistency problems
are solved problems with mature implementations. Building this is a
multi-year detour from the actual product.

**Consequences.** The FGA model becomes a first-class artifact requiring
version control, testing and migration discipline (see A1).

---

### ADR-004

**Tenancy is an immutable security boundary** · Accepted · Q1

**Decision.** Every protected object belongs to a tenant. A request is
conceptually `(subject, tenant, resource, action, context)`. The system never
infers tenant from an arbitrary resource lookup where it can be carried
explicitly.

**Consequences.** Enforced in depth: API → authorization → tenant boundary →
service → database isolation. See ADR-018, ADR-028, ADR-029.

---

### ADR-005

**Separate platform and tenant authority planes** · Accepted · Q11

**Decision.** Two independent authority domains.

```txt
PLATFORM PLANE      what may the operator do to tenants and infrastructure
TENANT PLANE        what may a person do inside this association
```

Neither implies the other. Platform administration confers no tenant-data
access.

**Consequences.** Support and incident response require an explicit
privileged-access mechanism (ADR-066).

---

## The authority graphs

### ADR-006

**Organizational relationship is not authority** · Accepted · Q15

**Context.** The initial model assumed a parent → subsidiary hierarchy.
Research did not support it: the movement is a federation of national
movements and independent local associations, with membership frameworks
described as mutual agreements rather than control.

**Decision.** Model affiliation, policy authority, compliance authority and
data-access authority as **independent graphs**.

```txt
AFFILIATION       India ──affiliated_with──> Bombay
POLICY            India ──may_set_policy───> Bombay
COMPLIANCE        India ──may_sanction────> Bombay
DATA              India ──may_read_members─> Bombay   (not implied)
```

**Consequences.** The most important structural decision in the design. It
makes "parent YMCA" a description rather than a permission.

---

### ADR-007

**Authority may be narrower than affiliation** · Accepted · Q16

**Decision.** A body may hold policy authority over an affiliate without
administrative authority over it. This is the normal case, not an exception.

**Rationale.** Confirmed in the field: national bodies require adherence to
membership standards while local programmes, staffing and operations remain
local decisions.

---

### ADR-008

**Authority may flow against organizational direction** · Accepted · Q17

**Decision.** The authorization graph supports authority delegated upward or
sideways — a local association administering a regional programme, a
specialist body granted authority over peers.

**Consequences.** The scope graph cannot be the organizational tree
(ADR-012), and it must be a DAG (ADR-016).

---

### ADR-009

**Five distinct authority verbs** · Accepted · Q86

**Decision.**

```txt
may_set_policy          publish standards binding on the target
may_review_compliance   audit conformance, request evidence, record findings
may_sanction            suspend affiliation, revoke brand use, impose conditions
may_administer          manage the target's internal affairs
may_read_member_data    access the target's personal data
```

None implies any other.

**Rationale.** Field evidence shows compliance authority existing
independently: a national membership standards committee may act on conduct
that damages the movement, without any administrative reach into the
association. Folding sanction into policy authority would mean anyone who can
publish a standard can suspend a peer.

**Critical invariant.** Sanction authority is scoped to the affiliation
relationship, never to tenant data. A national body may suspend a local's
affiliation without ever reading one member record.

---

## Scope and roles

### ADR-010

**Tenant membership is a relationship** · Accepted · Q5

**Decision.** `alice member_of tenant:bombay` is the domain-level truth, not
`user.tenant_id`. A foreign key may still exist for isolation; it is not the
authorization concept.

---

### ADR-011

**Role definitions global, assignments scoped** · Accepted · Q7

**Decision.** An uninstantiated role definition may be global. An assignment
of a role to a subject must carry a scope.

```txt
Definition:   Branch Secretary → member.read, member.update
Assignment:   Alice → Branch Secretary @ scope: Procter
```

---

### ADR-012

**Scope is a separate construct from domain containment** · Accepted · Q8

**Decision.** Authorization scope is maintained deliberately and may disagree
with both organizational ownership and physical location.

**Rationale.** Required by ADR-008. A pool physically at one branch may be
operated by a department elsewhere; both may need authority over it.

---

### ADR-013

**Scope assignment propagates to descendants** · Accepted · Q61

**Decision.** A role assigned at a scope node reaches that node's descendants
by default.

**Rationale.** The alternative produces hundreds of tuples per assignment and
makes revocation unreliable — you cannot revoke what you cannot enumerate.

---

### ADR-014

**Propagation is per-permission, not per-role** · Accepted · Q62

**Decision.** Each permission within a role declares whether it propagates.

```txt
Secretary @ Bombay
    member.read      propagates to all chapters
    member.update    propagates to all chapters
    resource.close   local scope only
```

**Rationale.** An association-level secretary may legitimately read chapter
member records while having no business closing a chapter's pool. Per-role
propagation forces a choice between too much authority and too many
assignments.

**Consequences.** Role definitions are more complex to author. The FGA model
must express propagation per relation.

---

### ADR-015

**Scope targets are heterogeneous** · Accepted · Q63

**Decision.** Any authorization object may be an assignment target — an
organizational unit, a resource, a programme.

---

### ADR-016

**Authorization containment is a DAG** · Accepted · Q64

**Decision.** A scope node may have multiple authorization parents.
Permissions are the union over all paths.

```txt
Procter Pool
 ├── auth_parent → Procter Chapter      (located there)
 └── auth_parent → PE Department        (operates it)
```

**Consequences.** Cycle detection is mandatory on every edge write. Path
explosion must be bounded. Expensive to reverse later, which is why it was
decided early.

---

### ADR-017

**No scope exclusions** · Accepted · Q66

**Decision.** Negative scoping (`Secretary @ Bombay EXCEPT Bandra`) is
prohibited. Grant the three scopes instead.

**Rationale.** Exclusions make "why can Alice do this?" undecidable by
inspection and are a classic source of authorization defects.

---

### ADR-018

**Every scope path has a tenant ancestor** · Accepted · Q67

**Decision.** Absolute invariant. No permission resolves without a tenant in
the path.

**Consequences.** This is what makes a single shared authorization store safe
(ADR-031). It is testable exhaustively against the model.

---

### ADR-019

**Cross-tenant authority is an explicit grant** · Accepted · Q68

**Decision.** Cross-tenant scope is never containment-derived. It is an
explicit, time-bounded, audited grant.

```txt
regional_body ──grants_authority_to──> org_unit:bombay
    scoped to:  programme:regional-youth-2026
    expires:    2027-03-31
    audited:    yes
```

---

### ADR-020

**Ad-hoc scope collections deferred** · Deferred · Q65

**Decision.** Named sets of unrelated scope nodes ("all pools in Bombay") are
useful but deferred. Noted as a likely v2 feature.

**Risk if built.** A second scoping mechanism that can be abused into a
shadow organizational structure.

---

### ADR-021

**Relationship-derived permissions permitted** · Accepted · Q9

**Decision.** A relationship may confer authority directly without an
intermediate role. `Alice organizer → Event 123` may imply `event.edit`.

**Rationale.** Prevents turning every meaningful relationship into a role.

---

### ADR-022

**Roles are reusable permission bundles** · Accepted · Q10

**Decision.** Roles bundle permissions; relationships establish why the
person holds authority and over what.

---

## Authorization / domain split

### ADR-023

**Authorization vs domain eligibility** · Accepted · Q13, Q49

**Decision.**

```txt
AUTHORIZATION   Alice is entitled to use Procter Pool
DOMAIN RULES    membership suspended / programme full / booking window closed
```

**Rationale.** Recurs at membership level (Q13) and again at resource level
(Q49). Treated as a general law of the architecture, not a local rule.

**Consequences.** Prevents the authorization store from absorbing the entire
business domain.

---

### ADR-024

**OpenFGA knows only the authorization graph** · Accepted · Q14

**Decision.** OpenFGA models person, tenant, organizational unit, resource,
programme, group, role. It does not model invoices, payments, bookings,
beds or plans.

---

### ADR-025

**Postgres owns business state; OpenFGA owns relationships** · Accepted · Q28

**Decision.**

```txt
PostgreSQL   membership.status, valid_until, the business record
OpenFGA      the relationship graph, queryable as edges
```

Neither contains everything. Where a tuple has a backing business record,
Postgres is the origin and the tuple is synchronized from it. Where a tuple
has no business meaning outside authorization (an ad-hoc delegation, a
break-glass grant), OpenFGA is simply the only store.

---

### ADR-026

**Transactional outbox, not distributed transactions** · Accepted · Q29

**Decision.**

```txt
Postgres transaction
 ├── business mutation
 └── authorization outbox row
          ↓ reliable dispatcher
       OpenFGA
```

Security-sensitive transitions — revocation, suspension, role removal —
write to OpenFGA **synchronously** before the operation is externally
effective.

**Rationale.** Two-phase commit across Postgres and OpenFGA is significant
operational cost for little benefit. A small lag on grant is acceptable; lag
on revocation is not.

---

### ADR-027

**Graded failure modes** · Accepted · Q30

**Decision.**

```txt
SECURITY-CRITICAL   fail closed   admin mutations, privileged access,
                                  role assignment
MONEY / HIGH-VALUE  fail closed   payment confirmation, expensive booking
                    or authoritative fallback
LOW-RISK            narrow, named fallback from cached decisions
```

No generic "authorization unavailable = allow" rule exists anywhere.

---

### ADR-028

**Tenant isolation represented twice** · Accepted · Q31

**Decision.** Isolation exists independently in the authorization graph and
in the data layer. An authorization failure alone must not expose another
tenant's rows.

---

### ADR-029

**PostgreSQL row-level security** · Accepted · Q32

**Decision.** RLS as the second tenant boundary, not application code alone.

---

### ADR-030

**No PII in authorization tuples** · Accepted

**Decision.** Opaque identifiers only. No names, emails or personal
attributes in the authorization store.

---

### ADR-031

**Single OpenFGA store, tenant-namespaced** · Accepted · Q90

**Decision.** One shared store. Sovereignty comes from ADR-018 (mandatory
tenant ancestor) and namespaced object identifiers, not physical separation.

**Rationale.** Store-per-tenant makes cross-tenant authority (ADR-019)
impossible to express without syncing tuples between stores; leaves
platform-plane tuples homeless; and multiplies schema migrations by tenant
count, producing drift that is itself a security liability.

**Exception.** Hard data-residency requirements are met by separate regional
platform instances, not by splitting stores within one instance.

---

### ADR-032

**JWTs carry identity, not permissions** · Accepted

**Decision.** Tokens carry subject, issuer, audience, expiry and at most
coarse session context. The permission graph is never encoded in a token.

**Rationale.** Removal from a club, revocation of a role, or a tenant
suspension must take effect immediately. Authorization is evaluated against
current state.

---

## Policy

### ADR-033

**Policy is a structured domain object** · Accepted · Q18

**Decision.** Policies are typed domain objects with known semantics, not
arbitrary executable code stored in the database.

---

### ADR-034

**Mandatory / default / local levels** · Accepted · Q4

---

### ADR-035

**Mandatory policy cannot be overridden** · Accepted · Q20

**Decision.** No local override of mandatory policy. Contest-before-effect
was considered and rejected as scope creep.

---

### ADR-036

**Inheritance semantics are per policy type** · Accepted · Q21

**Decision.** No universal inheritance algorithm. Each policy type declares
its semantics: `constraint` (strictest wins), `override`, `default`, `set`,
`append`.

**Rationale.** Age minimums accumulate monotonically; opening hours do not.

---

### ADR-037

**Domain owns policy evaluation** · Accepted · Q19

**Decision.** `Policy definitions → domain policy evaluation → eligibility
decision`. OpenFGA is not a general-purpose policy engine. Use an existing
policy engine where one fits rather than building from scratch.

---

## Identity and person

### ADR-038

**Person global, membership tenant-local** · Accepted · Q22

---

### ADR-039

**Authentication identity is global** · Accepted · Q23

**Decision.** One IdP subject per human across the platform, not independent
per-tenant identities.

---

### ADR-040

**Multiple deliberate principals permitted** · Accepted · Q24

**Decision.** A person may hold genuinely separate authenticated principals
— personal, staff — not merely aliases.

**Consequences.** Makes the privileged-access model coherent: a support
engineer's elevated principal is distinct from their member identity.

---

### ADR-041

**Person relationships are not subtypes** · Accepted · Q37, Q38, Q39

**Decision.** Member, employee, volunteer, hosteller, instructor are
relationships on a Person. They coexist. The same relationship type may be
held concurrently (employee of two branches).

---

### ADR-042

**Guardianship is separate from membership dependency** · Accepted · Q96

**Decision.** Two distinct relationships that frequently coincide.

```txt
DEPENDENT MEMBERSHIP   commercial: who is covered, who pays
GUARDIANSHIP           legal: who consents, who receives, who may act
```

**Rationale.** They come apart in every direction: a guardian may not be a
member; a dependent (spouse) may need no guardian; the guardian may not be
the membership holder; and guardianship must outlive the membership because
consent is what made past processing lawful.

**Consequences.** The first Person→Person relationship in the model.
Guardianship is global; consent grants are tenant-scoped, evidenced,
revocable, purpose-bound and expiring. Guardianship claims require
verification (ADR-049).

---

## Membership

### ADR-043

**Membership is distinct from enrollment** · Accepted · Q40

**Decision.**

```txt
MEMBERSHIP     the relationship with the association
ENROLLMENT     purchase of or participation in an offering
```

Five programme subscriptions do not make five memberships.

---

### ADR-044

**Membership belongs to exactly one tenant** · Accepted · Q41

**Rationale.** Broad-access plans define privileges across an association's
own branches, departments and centres — broad _within_ one tenant, not
globally portable.

---

### ADR-045

**Primary holder plus dependents** · Accepted · Q75

**Decision.** A membership has one accountable primary holder and zero or
more dependents.

**Rationale.** Required by real catalogues — couple and family memberships
exist. Chosen over multi-holder because it keeps fee accountability
unambiguous and generalizes to family and corporate cases.

---

### ADR-046

**Membership admission is an approval workflow** · Accepted · Q76

**Decision.**

```txt
APPLIED → UNDER_REVIEW → APPROVED → ACTIVE
              ↓                        ↓
          REJECTED         SUSPENDED / LAPSED / TERMINATED
```

**Rationale.** Associations reserve the right to reject applications without
assigning a reason, to require an applicant to appear before a committee, and
to terminate membership for conduct prejudicial to the association.
Membership is admitted, not purchased.

**Consequences.** An applicant is a Person with a pending relationship and
needs limited access before becoming a member. Approval authority is a
scoped, delegable permission resolving to actual office holders (ADR-072).

---

### ADR-047

**Suspension is explicit, never derived** · Accepted · Q77

**Decision.** Arrears do not silently compute into lost entitlement.
Automation detects the condition; an actor issues the suspension; the
suspension is a recorded fact.

**Rationale.** Authorization must not depend on live financial state.
Applied identically to affiliation standing (ADR-074).

---

### ADR-048

**No automatic age-band transition** · Accepted · Q78

**Decision.** Eligibility is checked at application and at renewal. A member
crossing an age threshold mid-term is not silently re-planned or re-priced.

---

### ADR-049

**Verification is a first-class object** · Accepted · Q79

**Decision.**

```txt
Verification
    subject, type, evidence_reference, verified_by,
    verified_at, expires_at, status
```

Used for student status, identity at check-in, guardianship claims,
background clearance and compliance attestation.

**Consequences.** Must support expiry and event-driven re-verification —
field practice re-checks on a fixed cycle, on rehire, after a break in
service, and on transfer into a youth-facing role.

---

### ADR-050

**Conferred memberships are distinct from purchased** · Accepted · Q80

**Decision.** Life membership is conferred by decision against a donation to
the corpus, not bought from a price list. The model distinguishes acquisition
by purchase from acquisition by conferral.

---

### ADR-051

**Governance rights are a separate entitlement axis** · Accepted · Q83

**Decision.**

```txt
Membership
 ├── facility entitlements   which resources and programmes  (fee schedule)
 └── governance rights       vote, stand, hold office        (constitution)
```

Independent axes, acquired and revoked separately.

**Rationale.** Field evidence shows voting membership governed by
constitutional criteria entirely disjoint from facility plans — a member may
hold maximal facility access and no vote.

**Data-protection consequence.** Constitutional eligibility criteria may
reference special-category personal data. The platform ships **generic,
tenant-configured eligibility machinery** and hard-codes no constitutional
rule. Tenants encode their own constitutions and own the resulting
obligations under GDPR and India's DPDP Act.

---

### ADR-052

**Foreign membership recognition deferred** · Deferred · Q42

**Decision.** Not built. But entitlement resolution routes through a resolver
capable of consulting grants originating outside the tenant, so reciprocity
later is a new grant type rather than a re-architecture.

---

## Programme, resource, consumption

### ADR-053

**Programme / offering / enrollment / occurrence** · Accepted · Q43

```txt
PROGRAMME     what we offer            Swimming
OFFERING      commercial instance      Beginner, September 2026
ENROLLMENT    person joined it         Alice
OCCURRENCE    scheduled session        Tuesday 18:00
```

---

### ADR-054

**Offerings need not be commercial** · Accepted · Q44

---

### ADR-055

**Pricing is a separate value object** · Accepted · Q45

**Decision.** An offering carries multiple simultaneous prices — member,
non-member, student, early-bird — not a single price field.

---

### ADR-056

**Resource is distinct from programme** · Accepted · Q46, Q47, Q48

**Decision.** A resource has consumable capacity; a programme is organized.
Programmes may exist with no resource; resources may be consumed with no
programme.

---

### ADR-057

**Resource entitlement is a relationship** · Accepted · Q50

**Decision.**

```txt
Alice ──holds──> PoolSubscription ──grants_access_to──> Pool
```

**Rationale.** Entitlement bundling is entirely tenant-local and has no
global default. One association bundles pool and health club into base
membership; another requires separate subscription for the same facilities.
Modelling entitlement as a relationship keeps this native to the graph rather
than a special-case attribute list.

---

### ADR-058

**Booking and Stay are independent aggregates** · Accepted · Q55

**Decision.** No shared abstract parent. Independent state machines.

---

### ADR-059

**Allocation exclusivity enforced in the database** · Accepted · Q55 (consequence)

**Decision.** Because ADR-058 removes the shared domain parent, the
"no overlapping allocation of the same resource" invariant is enforced once
in PostgreSQL via an exclusion constraint over `(resource_id, time_range)`
on a shared allocation table that both aggregates write into.

**Rationale.** Two independent domain implementations of one invariant will
drift. Placing it where it cannot be bypassed preserves the clean aggregate
separation without paying for it in correctness.

---

### ADR-060

**Access mode is time-scoped** · Accepted · Q56

**Decision.** Walk-in versus booked is a property of a schedule window, not a
column on Resource.

```txt
Pool
 ├── 06:00–09:00   WALK_IN
 └── 09:00–21:00   BOOKED
```

Resolvable as tenant or branch policy via ADR-034.

---

### ADR-061

**Walk-in capacity is not system-enforced** · Accepted · Q57

**Decision.** Walk-in windows enforce entitlement and opening hours only. No
live occupancy tracking.

**Rationale.** The physical mechanism to count occupancy does not exist at
most sites.

**Design note.** Capacity lives on the _window_, so a site that later
installs access control can enable capacity tracking without schema change.

---

### ADR-062

**Waitlist reserved, deferred** · Deferred · Q53, Q59

**Decision.** `WAITLISTED` exists as a status and capacity checks return
"would waitlist" rather than hard rejection. Position, promotion and
notification are not built.

---

### ADR-063

**Hostel stay requires approval** · Accepted · Q60

**Decision.** Unlike an auto-confirming booking, a stay requires
verification and approval before confirmation.

---

### ADR-064

**Allocation granularity varies by room type** · Accepted · Q60

```txt
RoomType.allocation_unit = BED | ROOM
```

---

### ADR-065

**Actor and beneficiary may differ** · Accepted · Q82

**Decision.** The person performing an operation and the person consuming it
may be different.

```txt
actor        Alice   member, entitled, accountable
beneficiary  Rahul   guest, no membership, occupies the room
```

**Rationale.** Members book rooms and halls for guests. The guest must exist
as a Person record — the association needs to know who was in the building.

**Note.** Same actor/subject split as impersonation (ADR-068), applied to
domain operations rather than administrative ones.

---

## Privileged access and governance

### ADR-066

**JIT privileged access, no standing superusers** · Accepted · Q25, Q26

**Decision.**

```txt
Routine support         → explicit tenant approval, SLA-bounded
Security / legal / safety → break-glass by platform security
```

Every grant carries reason, target, scope, duration, and full audit.
Permanent support-admin identities do not exist.

---

### ADR-067

**Break-glass requires dual control** · Accepted · Q26

**Decision.** Break-glass requires a second security officer or independent
auditor to co-sign. The tenant is notified regardless of consent. Hard
expiry and mandatory post-hoc review.

**Rationale.** Prevents platform security from becoming the unilateral
superuser that ADR-005 exists to eliminate.

---

### ADR-068

**Impersonation preserves both principals** · Accepted · Q27

```txt
actor = Bob
delegated_identity = Alice
```

Both recorded on every action. Required for forensic correctness.

---

### ADR-069

**Term policy declared on the role definition** · Accepted · Q87

**Decision.**

```txt
term_policy = PERMANENT | OPTIONAL_TERM | MANDATORY_TERM
```

Constitutional offices are `MANDATORY_TERM`; break-glass and cross-tenant
grants are `MANDATORY_TERM`; ordinary operational roles are `OPTIONAL_TERM`.

**Rationale.** Universal mandatory expiry is defeated in practice by admins
setting the year 2099 — worse than no expiry because it appears controlled.
Restricting terms to governance roles is too narrow, since three
non-governance cases already require expiry.

---

### ADR-070

**Expiry evaluated at decision time** · Accepted · Q87

**Decision.** An expired assignment is inert the moment it expires,
regardless of whether a sweeper has run. An `ACTING` status supports
temporary cover without deleting the substantive assignment.

---

### ADR-071

**Office is distinct from Role** · Accepted · Q88

**Decision.** An Office is a constitutional position with a term and
governance meaning. A Role is a permission bundle. An office may confer a
role.

```txt
Office(President) ──confers──> RoleDefinition(tenant_admin)
```

---

### ADR-072

**Office-holding in scope, elections out** · Accepted · Q88

```txt
IN     Office, office holding, committees, approval routing,
       decision records
OUT    Elections, nominations, ballots, meetings, agendas,
       minutes, quorum
```

**Rationale.** ADR-046 puts board approval in the membership admission path,
so the system must know who the board is. Meeting management is a different
product. The carve-out: record the _decision_, not the process — admission
requires an auditable record of who approved and when.

---

## Affiliation and extensibility

### ADR-073

**Affiliation is a stateful relationship** · Accepted · Q84

```txt
APPLIED | AFFILIATED_IN_GOOD_STANDING | AFFILIATED_IN_ARREARS
        | SUSPENDED | LAPSED | WITHDRAWN
```

---

### ADR-074

**Affiliation state is recorded; sanction is explicit** · Accepted · Q84

**Decision.** Loss of good standing records a fact and _enables_ a sanction.
It revokes nothing by itself. Capability changes only when an authorized
actor invokes `may_sanction` with stated grounds.

**Rationale.** Identical in shape to ADR-047. Automatic enforcement would
mean a state change in one sovereign party silently strips capability from
another — that should require someone to decide it and answer for it.

---

### ADR-075

**Non-affiliated associations are first-class tenants** · Accepted · Q85

**Decision.** A tenant may exist with no parent affiliation. Affiliation is
acquired, not presumed.

**Rationale.** In India roughly 450 local associations are non-affiliated
against 588 affiliated. A model assuming affiliation would exclude nearly
half the addressable movement.

---

### ADR-076

**Permissions system-defined, roles tenant-configurable** · Accepted · Q69

```txt
SYSTEM-DEFINED    permissions, core object types, the FGA model
TENANT-CONFIGURABLE  roles, groups, membership plans, resources,
                     programmes, offerings, policies
```

---

### ADR-077

**Resource types declare behavioural archetypes** · Accepted · Q70

**Decision.** The platform defines archetypes — bookable-slot,
allocatable-unit, walk-in-only, non-consumable. Tenants create and name their
own resource types, each declaring an archetype.

**Rationale.** A closed taxonomy makes every new facility type a platform
release; a fully open one makes behaviour unpredictable and cross-tenant
reporting meaningless. Archetypes fix behaviour while freeing vocabulary.

---

### ADR-078

**No privilege escalation through role creation** · Accepted · Q71

**Decision.** A person may only grant permissions they currently hold, at
scopes where they currently hold them.

---

### ADR-079

**Role templates are cloned, not linked** · Accepted · Q72

**Decision.** The platform may publish role templates; adopting one copies
it. No live link.

**Rationale.** A linked template lets the platform silently alter tenant
authority, violating ADR-001.

---

### ADR-080

**Role edits show blast radius and emit audit** · Accepted · Q73

**Decision.** Changes to a role definition propagate immediately to existing
assignments, but the editor must first be shown who is affected, and the
change emits a high-severity audit event.

**Rationale.** Editing a definition is a silent bulk authorization change
affecting people invisible from the editing screen.

---

### ADR-081

**Membership plans are tenant-local** · Accepted · Q74

**Rationale.** Three associations within one country maintain three mutually
incompatible catalogues — differing in categories, eligibility, duration and
whether facilities are bundled. No global catalogue is possible; the platform
ships plan _machinery_ only.

---

## Finance

### ADR-082

**Financial parties are generic** · Accepted · Q89

```txt
Invoice.payer → Party        where Party = Person | Organization
Invoice.payee → Party
```

**Rationale.** Costs nothing now; a migration later. Member fees, programme
fees, hostel charges and inter-organizational dues become one object with
different party types.

---

### ADR-083

**Composable charge components** · Accepted · Q81

**Decision.** Entrance fee (one-time), membership fee (recurring) and tax are
separate components with different recurrence, not a single amount.

**Rationale.** Associations charge a distinct entrance fee alongside the
subscription, and state tax separately on listed prices.

---

### ADR-084

**Payment provider facade** · Accepted · Q11 (round 11)

**Decision.** Ports and adapters. The domain owns money and records what is
owed; adapters record what a provider did; reconciliation links them. No
provider concept leaks into the domain. Tax is a pluggable per-jurisdiction
calculator.

---

### ADR-085

**Inter-organizational dues deferred** · Deferred · Q89

**Decision.** Not built in v1. Enabled by ADR-082 as a workflow addition
rather than a schema change.

---

## Safeguarding

### ADR-086

**Store the verdict, not the evidence** · Accepted · Q97

**Decision.**

```txt
STORE        Verification(type, status, provider, checked_on, expires_on)
NEVER STORE  criminal records, offence details, report contents,
             registry match data
```

**Rationale.** The attestation is what authorization needs. The underlying
record is special-category data under GDPR and the DPDP Act, has no
authorization use, and holding it makes every tenant a custodian of
criminal-history data. Screening providers already retain the evidence.

---

### ADR-087

**Clearance is a precondition on role assignment** · Accepted · Q92

**Decision.** Role definitions declare required clearances. An expired
clearance renders the assignment inert at decision time, exactly as an
expired term does under ADR-070.

**Rationale.** Screening obligations attach to youth access, which is a
property of the role — and they apply to volunteers as much as staff.

---

### ADR-088

**Safeguarding compliance feeds affiliation standing** · Accepted · Q95

**Decision.** Compliance certification is recorded via ADR-049 and feeds
affiliation state via ADR-073/074 — no parallel mechanism.

**Rationale.** Field evidence matches the mechanism exactly: maintaining
national affiliation requires certifying that screening standards are
followed. The design predicted the shape before the instance was found.

---

### ADR-089

**Allegation records are out of scope** · Accepted · Q97

```txt
IN     Restriction (barred from youth-facing roles)
       Suspension pending review (status, no narrative)
       Opaque case reference
OUT    Allegation details, narratives, witness accounts,
       investigation notes, reporter identity
```

**Rationale.** Authorities are the real system of record; the data concerns
named individuals including minors; retention differs from everything else;
investigation material may attract privilege; specialist tooling exists.
A restriction is an authorization fact — the case behind it is not.

---

### ADR-090

**Member screening is jurisdiction-gated** · Accepted · Q98

**Decision.** Screening of members and participants is supported, disabled by
default, enabled per tenant where a jurisdiction supports it.

**Rationale.** Registry screening presumes registry infrastructure that does
not exist in most jurisdictions, including the largest part of the intended
user base. A platform assuming it would be unimplementable there.

---

### ADR-091

**Screening never auto-acts** · Accepted · Q98

**Decision.** A match creates a review task. A human decides. The decision is
the audit record. No automatic bar, ever.

---

## Miscellaneous

### ADR-092

**Staff membership is tenant policy** · Accepted · Q24 (round 16)

```txt
staff_membership_policy = AUTOMATIC | ELIGIBLE | SEPARATE | NONE
```

**Rationale.** Sources support no global rule and practice plainly varies.
The platform ships the switch, not an answer.

---

### ADR-093

**Retain everything, scrub PII, preserve audit** · Accepted · Q5

**Decision.** Deletion scrubs personal data while preserving the audit trail
and the structural record.

---

### ADR-094

**Organizational units are one typed concept** · Accepted · Q34

```txt
OrganizationalUnit
    type = CHAPTER | DEPARTMENT | CENTRE | REGION | INSTITUTE | PROJECT
```

**Rationale.** Depth and naming vary arbitrarily across the movement — one
region subdivides into zones and sub-regions while its peers do not.
No fixed level structure is possible.

---

### ADR-095

**Organizational, physical and authorization containment differ** · Accepted · Q35, Q36

**Decision.** Three independent relationships. A unit may be administratively
owned by one parent, physically located at another's premises, and
authorization-scoped to both.

---

### ADR-096

**Consumption is realization, distinct from reservation** · Accepted · R1

**Context.** The original nine contexts model reserved use — `Booking`, `Stay`
— and nothing else. `WALK_IN_ONLY` resources enforce nothing and record
nothing. The reference tenant's requirement, a daily mess register, is neither
a booking nor a stay and was therefore inexpressible.

**Decision.** A tenth context records **actual** use.

```txt
Booking, Stay      RESERVED     creates an Allocation
Consumption        ACTUAL       creates no Allocation
```

A record may name the allocation it realizes, which is what distinguishes an
attended booking from a no-show.

**Rationale.** `Allocation` exists solely to carry the exclusion constraint. A
meal excludes nobody. Routing consumption through it would put a GiST exclusion
constraint in the write path of the highest-frequency, lowest-value operation
in the system, to enforce an invariant that does not apply.

---

### ADR-097

**Obligations are standing, never materialized** · Accepted · R1

**Decision.** A `ConsumptionType` declares `obligates`. Where true, one
standing `ConsumptionObligation` row covers the whole relationship; expected
units are derived at billing time.

```txt
expected = recurrence × (effective ∩ billing_period)
         − periods relieved by ExpectedAbsence
```

**Rationale.** The alternative — writing a presumed record per person per day —
requires a nightly job, and 8.7 states that sweepers exist only for
notification and cleanup, never for enforcement. Billing from sweeper-written
presumptions is enforcement, and a stalled job would silently under-bill.

**Consequence.** The per-meal-type absence default the reference tenant
appeared to need does not exist. Dinner obligates; breakfast does not. The
special case dissolves rather than being configured.

**Scope.** Obligation is opt-in and tenant-local. Full-board and pay-per-meal
associations use the same machinery with the flag set differently.

---

### ADR-098

**Consumption records are corrected, never edited** · Accepted · R1

**Decision.** A record is immutable once written. A correcting record
supersedes it via `supersedes_id`. `correction_window` bounds ordinary
corrections; beyond it, `consumption.correct_late` is required and audited at
`NOTABLE`.

**Rationale.** Mirrors the invoice rule already established — issued invoices
are immutable, corrections are credit notes. Mutable consumption history makes
a disputed bill unanswerable, which is the paper register's existing failure
and not one worth importing.

**Consequence.** Enforcement is at decision time against `occurred_on`. There
is no midnight lock job, consistent with ADR-070.

---

### ADR-099

**Absence relief is snapshotted at declaration** · Accepted · R1

**Decision.** `ExpectedAbsence` carries two independent booleans, resolved by
tenant policy at declaration and then frozen on the row.

```txt
relieves_recording    need not submit records
relieves_payment      obligated units are not billed
```

**Rationale.** Applies pattern 8.4. A tenant raising its relief threshold must
not retroactively rebill an absence already declared, exactly as it must not
alter a booked cancellation policy or an enrolled price.

**Consequence.** The reference tenant's LONG and SHORT leave are two flag
combinations rather than an enum, so a tenant relieving payment for every
absence, or none, needs no new type.

**Note.** Absence is not suspension. Entitlement is untouched, so no tuple is
written and no synchronous revocation path is involved.

---

### ADR-100

**Consumption enters the charge vocabulary** · Accepted · R1

**Decision.** `CONSUMPTION` is added to `Charge.source_type`, alongside
`MEMBERSHIP | SUBSCRIPTION | ENROLLMENT | BOOKING | STAY | DEPOSIT | DUES |
ADJUSTMENT`.

**Rationale.** Consumption raises no money and computes no amount. It publishes
`ConsumptionPeriodClosed` carrying units and vocabulary; Finance composes the
charge and applies tax. This is the Published Language pattern of 05.0.4, and
the addition is the only change the new context forces on an existing one.

---

### ADR-101

**Asynchronous projection is fenced against synchronous removal** · Accepted · R1

**Decision.** Wherever a fact is projected asynchronously into a second store
and removed synchronously from it, the removal must be able to cancel a
projection still in flight. Three obligations, which only work together:

```txt
1  the row names the fact       fence key — subject, relation, object,
                                at domain level
2  writers serialize on it      pg_advisory_xact_lock(fence_key) in any
                                transaction that grants or revokes the fact
3  the dispatcher holds its     its row lock spans the OpenFGA write, so a
   lock across that write       void cannot land between write and commit
```

A revocation, holding the advisory lock, voids every pending row carrying its
fence key and then deletes the tuple. The dispatcher claims rows with
`FOR UPDATE SKIP LOCKED` and re-reads `voided_at` inside that lock.

The row names the fact, not the tuple. The granting context already knows that
person X holds role R on unit U; rendering that into the tuple form of the
currently deployed model stays with the dispatcher, so no FGA model version is
frozen into stored payloads and a revocation can compute its own fence key
from the same fact it is revoking.

**Rationale.** 8.3 grants asynchronously and revokes synchronously, and states
that the failure mode is never "revocation appeared to succeed but authority
persisted." Unfenced, it is exactly that:

```txt
grant commits, outbox row pending
dispatcher stalls                      7.4 alerts; it does not block
revoke deletes the tuple               no-op — the tuple does not exist yet
revoke commits, reported successful
dispatcher recovers, applies the row   the tuple exists again
```

No synchronous path runs a second time and nothing else removes it.
Idempotency (8.9) makes redelivery safe, not ordered. The validity checks of
6.1 step 3 cover term windows, clearance and restrictions — not revocation —
so the tuple is the sole source of truth for whether the relation holds.

**Consequence.** Obligation 2 is the one that is easy to omit. Without it a
grant transaction that began before the revocation and commits after it is
invisible to the revocation's update, and its row survives unvoided.

Obligation 3 holds a row lock across a network call. `SKIP LOCKED` confines
that to the one row, and a statement timeout bounds it: on timeout the
transaction rolls back, the row stays pending, and it is retried.

Stated generally because the asymmetry is not specific to authorization
tuples. Any projection whose removal may not lag behind its creation
inherits the rule.

**Note.** `authorization_outbox` projects authorization facts and nothing
else. 05.0.8 also calls the outbox the mechanism by which state changes
propagate between contexts; that bus is not this table and is not yet
modelled. Voiding is sound for a projection and never for a domain event
another context has already consumed.

---

### ADR-102

**The reaching verbs are recorded, never graph-bearing** · Accepted · R1

**Decision.** All five authority verbs of ADR-009 are grantable and stored.
Three of them resolve in the graph; two do not.

```txt
may_set_policy          resolves — acts on the relationship
may_review_compliance   resolves — acts on the relationship
may_sanction            resolves — acts on the relationship

may_administer          recorded, resolves to nothing
may_read_member_data    recorded, resolves to nothing
```

The split is not arbitrary. The first three act _on_ the affiliation; the last
two would act _through_ it, into the target tenant, which is what Principle 1
forbids. A body that must genuinely administer or read goes through
`CrossTenantGrant` — one named object, an approver, a stated reason and a
mandatory expiry (ADR-019).

**Rationale.** The two verbs were previously absent from the schema entirely,
so a grant of either was storable and silently inert. That is the same
end state, reached by omission rather than by decision, and unauditable: a
reader could not tell whether the platform refused the authority or had
forgotten it.

Recording them is worth the row. The governance relationship is real even
where the platform declines to implement it, and the refusal is only
defensible if it is visible.

**Consequence.** 8.12's invariant 2 becomes expressible in its strong form.
Previously a national body could not be granted `may_administer` at all, so
"a body holding every verb but one reaches no member record" tested three
verbs and proved little. `natl-sec` now holds all five in the A1.6 tuples and
is asserted to reach nothing.

Those assertions are what enforces the inertness. A later edit that gives
either verb a path fails CI, which is the guarantee the design asks for
everywhere else — structure and tests rather than a remembered convention.

An `AuthorityGrant` of either verb projects no tuple. Its outbox row carries
no fence key, which ADR-101 permits.

**Note.** `tenant.may_administer` in the schema was an unrelated internal
relation — a tenant's own admins and break-glass — that happened to share a
name with the cross-tenant verb, and fed `organizational_unit.admin`. Anyone
wiring the verb into it, reasonably assuming one meaning, would have handed a
national body admin over every unit in the target tenant. It is now
`administered_by`.

**Assumption.** Whether any national or world body in the movement genuinely
holds member rosters is not established. This decision takes the safest
reading. If rosters do flow upward, `may_read_member_data` needs a narrow real
path and this ADR is revisited — see 05.1.11.

---

### ADR-103

**Invoice numbers come from a transactional counter, not a sequence** · Accepted · R1

**Decision.** Each tenant holds a counter row per series, and issuing an
invoice increments it inside the same transaction that issues the invoice.

```sql
UPDATE invoice_number_series
   SET next_value = next_value + 1
 WHERE tenant_id = $1 AND series = $2 AND kind = 'LIVE'
RETURNING next_value - 1;
```

A series is a financial year. Numbering happens at **issue**, never at draft
creation — `invoice.number` stays null until then.

**Rationale.** A PostgreSQL sequence cannot do this. `nextval` is deliberately
non-transactional, so a rolled-back transaction keeps the number it consumed
and the series acquires a gap. C11 is statutory; a gap is a defect a tenant
must explain to an auditor.

The row lock is the mechanism, not an accident of it. Gaplessness _is_
serialization: two invoices cannot both be next.

**Consequence.** Invoice issuance serializes per tenant per series. At
association scale that is unremarkable, but the `UPDATE` should be the last
statement before commit so the lock is held for as little of the transaction
as possible.

This makes 8.9's entry honest. It previously read "invoice numbering — NOT
idempotent; allocation must be concurrency-safe (see 05.8.13)", pointing at an
open item. The mechanism now exists.

**Note.** Historical import (B5) allocates from a separate `IMPORTED` series so
that loading last year's invoices cannot advance or gap the live counter.
Imported invoices keep their original number text verbatim;
`UNIQUE (tenant_id, number)` then makes a genuine collision a loud failure
rather than a silent overwrite.

---

### ADR-104

**Lists authorize the scope, not the row** · Accepted · R1

**Decision.** A list endpoint names the scope object it lists under. The caller
is checked once against that scope; the rows are then returned by ordinary
tenant-scoped SQL beneath RLS.

```txt
GET  .../units/{unit}/members        check(caller, member_read, unit)
                                     then SELECT ... WHERE unit_id = $1

GET  .../consumption?type=&period=   check(caller, may_read, consumption_type)
                                     then SELECT ... WHERE type_id = $1
```

Rows whose authorization is genuinely independent of their scope must be named
explicitly, and are checked per row over the returned page only — bounded by
the page size, never by the size of the result set.

**Rationale.** The reverse-index question — "which objects may this caller
see?" — is the expensive one in any Zanzibar-style model, and answering it
means reconciling pagination across two stores. Almost no real screen needs
it. "Members of this chapter" and "meals in this period" are lists under a
scope, and the scope is what authority is held over.

**Consequence.** No `ListObjects` on the read path, no permission projection to
keep consistent, and pagination is plain keyset pagination in PostgreSQL. List
latency becomes a database property with an index behind it, rather than a
graph traversal with an unbounded result set.

**The mechanism may under-report; it must never over-report.** A caller
reachable to a row by some path other than the named scope sees fewer rows
than they are entitled to. That is the safe direction, and it is the reason a
list must never be used to prove a negative — "not in the list" does not mean
"does not exist".

---

### ADR-105

**The tenant is a path segment** · Accepted · R1

**Decision.** Every tenant-plane route carries its tenant explicitly:

```txt
/api/v1/t/{tenant}/...
```

The principal's token also names a tenant. The two are compared on every
request and a mismatch is rejected before routing.

**Rationale.** 8.2 makes explicit tenant carriage load-bearing, and ADR-018
rejects any check whose resource has no resolvable tenant ancestor. Both are
enforceable by inspection when the tenant is in the URL, and both depend on
decoding a token when it is not. Audit entries, access logs and support
questions all name what was addressed rather than what was inferred.

**Consequence.** Requesting another tenant is expressible, and therefore
deniable and auditable — which is better than being inexpressible and
therefore untested. Platform-plane routes live under a separate prefix and
carry no tenant, per the two-plane separation.

---

### ADR-106

**Authentication is a port; the slice ships a development implementation** · Accepted · R1

**Decision.** The backend depends on an authentication port that turns a
credential into a principal:

```txt
Authenticator
    Authenticate(token) → (principal_id, principal_kind, tenant_id, expires_at)
```

`principal_kind` is `PERSONAL` or `ELEVATED` (ADR-066, ADR-067) — never both
at once. The slice ships a development implementation; a deployment picks
Cognito, Supabase or Appwrite behind the same port.

**Rationale.** The provider is a deployment choice, not a tenant one, so a
port with one concrete implementation per deployment is the whole of the
abstraction needed. Modelling both principal kinds now matters more than the
provider: they are the subjects of tuples, and retrofitting the split means
reissuing every tuple whose subject is a principal.

**Consequence.** Session lifetime, refresh, MFA on the platform plane, and
ELEVATED issuance remain undesigned. They are the port's implementation
concerns, and none of them changes the shape of the port.

**Note.** The development implementation must be impossible to enable
accidentally: it refuses to start unless explicitly selected, and a build
intended for deployment does not contain it.

---

### ADR-107

**Entitlement reaches a person by plan or by subscription, and both routes are
first-class** · Accepted · R5

**Decision.** `entitlement_bundle` names its sources, and there are two:

```dsl
define via_plan: [membership_plan]
define via_subscription: [subscription]
define beneficiary: [person] or beneficiary from via_subscription or covered_member from via_plan
```

`membership_plan` gains `covered_member: [membership#covered]` — one tuple per
membership, written at admission. `membership.plan`,
`membership_plan.grants_bundle` and `subscription.grants_bundle` are removed:
they expressed the same facts in the direction OpenFGA cannot travel, and
nothing read them.

**Rationale.** Both routes exist in the movement today, and they coexist inside
a single association:

```txt
YMCA Poona       one category; the fee "includes the use of the Swimming
                 pool, Health Club and other indoor Sports activities"
YMCA Bombay      five categories; privileges "as laid down in the Bye-laws"
YMCA Ernakulam   Associate and Life confer by category; Hostel Membership
                 confers "access to the indoor and outdoor games with
                 monthly subscription"
```

Ernakulam settles it: one person, one tenant, both routes at once.

Requiring every entitlement to route through a `subscription` would make Poona
create a synthetic subscription per member for a concept its domain does not
have, and would turn a Bombay bye-law amendment — the plan-level act — into a
tuple rewrite for every member of that category. Under `via_plan` the same
amendment is one tuple.

**Consequence.** Admission writes one `covered_member` tuple per membership.
Selling an add-on writes a `subscription` and one `via_subscription` tuple.
Changing what a category includes writes one `via_plan` tuple and moves every
member on that category, which is the point.

**Note.** The direction is not a matter of taste. OpenFGA traverses forward
from the object, so the bundle must name its sources; the reverse — a plan
naming the bundles it grants — is unreachable no matter how natural it reads.
The original `via_plan: [membership_plan#grants_bundle]` was the natural
reading and resolved to nothing at all. It was declared, never read, and the
model shipped that way through a full review.

**Terminology.** Associations use _subscription_ for the recurring membership
fee itself. This design uses it for an entitlement add-on bought on top of a
membership. The clash is real and will surface in the first conversation with
a real association; 12 records it.

---

### ADR-108

**Tenant isolation rests on a role grant, not only on the schema** · Accepted · R5

**Decision.** Row level security is declared `ENABLE` **and** `FORCE`, and the
application connects as a role that is neither a superuser nor a holder of
`BYPASSRLS`. The migration role and the application role are different roles.

**Rationale.** Three ways for a schema full of policies to isolate nothing,
none of which produce an error:

```txt
no FORCE            the table owner is exempt; the owner is whoever migrated
superuser           RLS does not apply, at all
BYPASSRLS           same
```

Every one of them fails open and silently. `08.2` and `A2.1` treat tenant
isolation as the design's central claim, and until this ADR that claim rested
on a `CREATE POLICY` statement that a default deployment would have bypassed
completely — the application connecting as the role that ran the migration is
the obvious way to set a system up, and it is the one that removes isolation.

**Consequence.** A deployment step creates the application login role and
grants it membership in the DML-only role the migration defines. No password
appears in a migration file. `audit_event` is append-only by privilege —
`UPDATE` and `DELETE` are revoked from the application role — rather than by
convention.

**Note.** This is verifiable in about four queries and should be verified per
environment, not assumed: connect as the application role, set one tenant, and
confirm that a second tenant's rows are invisible and a cross-tenant insert is
rejected.
