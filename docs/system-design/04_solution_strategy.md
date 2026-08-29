# 04 — Solution Strategy

The core ideas. Everything in section 05 is an elaboration of what follows;
everything in section 09 is a decision made in service of it.

If you read one section of this document, read this one.

---

## 4.1 The problem in one paragraph

A federated movement of legally independent associations wants one platform.
Each association owns its people, money and facilities outright, and no
national or global body may reach into another's records by virtue of sitting
above it on an org chart — yet standards genuinely do flow downward,
compliance genuinely is enforced, and a person may legitimately belong to
several associations at once. The platform must serve all of this without
becoming either a surveillance system or a collection of disconnected
single-tenant deployments.

The word for what is required is **sovereignty**, and the architecture takes
it literally.

---

## 4.2 Why the obvious approaches fail

### A single-tenant deployment per association

Fragments the movement. Makes cross-association anything — a shared regional
programme, a member moving cities, a national compliance report — a data
integration project. Multiplies operational cost by the number of tenants,
and guarantees version drift, which is itself a security liability.

### One database with a `tenant_id` column and application checks

This is multi-tenancy in the weakest sense. A single missing `WHERE` clause
is a cross-tenant data breach. It provides no answer at all to the harder
question, which is not "can Alice see this row" but "may YMCA India set
policy for YMCA Bombay without reading its member records."

### Plain RBAC

```txt
alice → ADMIN
```

In a multi-tenant federation this is actively dangerous. It has no concept of
*where* the role applies, and it produces role explosion the moment
inheritance appears: `bombay_procter_pool_manager`,
`bombay_central_pool_manager`, and so on until nobody can audit anything.

### Encoding the org chart as the permission hierarchy

The most tempting failure, and the one this design spent the longest
unlearning. It assumes that organizational containment implies authority. The
movement does not work that way — and once you have built the assumption in,
every genuine case (a department operating a facility located at a branch, a
local association running a regional programme) forces the org chart to
contort around an authorization problem.

---

## 4.3 The five principles

### Principle 1 — Tenancy is an immutable security boundary

Every protected object belongs to a tenant. Every authorization path
terminates at a tenant ancestor. Cross-tenant authority exists only as an
explicit, time-bounded, audited grant — never as an inherited property.

```txt
(subject, tenant, resource, action, context)
```

Enforced in depth, so that any single layer failing does not expose data:

```txt
API
 ↓  request carries tenant explicitly, never inferred
Authorization
 ↓  no relation resolves without a tenant in the path
Service
 ↓  tenant boundary checked independently
Database
    row-level security on tenant_id
```

The authorization graph and the data layer represent isolation
**independently**. That redundancy is the point: an authorization defect
alone must not be sufficient to leak another tenant's rows.

### Principle 2 — Organizational relationship is not authority

```txt
organizational relationship
        ≠  legal / control authority
        ≠  policy authority
        ≠  compliance / sanction authority
        ≠  data-access authority
```

Five independent graphs over the same set of organizations. The affiliation
between a national body and a local association says nothing by itself about
what either may do to the other. Authority is expressed with explicit verbs:

```txt
may_set_policy          publish standards binding on the target
may_review_compliance   audit conformance, request evidence
may_sanction            suspend affiliation, revoke brand use
may_administer          manage the target's internal affairs
may_read_member_data    access the target's personal data
```

None implies another. The sharpest expression of this principle, and the one
to test every future change against:

> A national body may suspend a local association's affiliation entirely
> without ever gaining the ability to read a single member record.

Authority may also run **sideways or upward** — a local association
administering a regional programme is a normal case, not a hack. This is why
the authorization scope graph is maintained separately from, and is permitted
to disagree with, the organizational structure.

### Principle 3 — Authorization answers entitlement; domain rules answer permission-now

```txt
AUTHORIZATION   is this person entitled or related at all?
DOMAIN RULES    may this operation happen right now?
```

```txt
Authorization says:   Alice is entitled to use Procter Pool
Domain says:          her membership is suspended,
                      the session is full,
                      the booking window closed an hour ago
```

This distinction recurs at every layer — membership, resource use, booking,
enrollment — and is the single load-bearing idea preventing the authorization
store from becoming a second, permanently stale copy of the business
database.

The practical rule: **if a fact changes for business reasons, it belongs in
PostgreSQL.** If it changes because someone's authority changed, it belongs
in the authorization graph.

### Principle 4 — Roles grant capabilities; relationships establish scope; attributes supply context

```txt
RBAC    coarse roles, always within a scope
ReBAC   membership, hierarchy, ownership, delegation
ABAC    state, context, attribute constraints
```

Role *definitions* may be global. Role *assignments* are always scoped.

```txt
Definition:   Branch Secretary → member.read, member.update
Assignment:   Alice → Branch Secretary @ Procter
```

Relationships may confer authority directly, without a role in between, so
that not every meaningful relationship has to be reified as a role:

```txt
Alice ──organizer──> Event 123     implies event.edit
```

And propagation is declared **per permission**, not per role, because an
association-level secretary may legitimately read every chapter's member
records while having no business closing a chapter's pool.

### Principle 5 — Store the verdict, not the evidence

For background checks, registry screening, safeguarding restrictions and
compliance attestation, the platform records the outcome and its validity
period — never the underlying record.

```txt
STORE        Verification(type, status, provider, checked_on, expires_on)
NEVER STORE  criminal records, offence details, report contents,
             registry matches, allegation narratives
```

The attestation is what authorization needs. The evidence has no
authorization use, is special-category data under GDPR and India's DPDP Act,
and holding it converts every tenant into a custodian of criminal-history
material. Screening providers already retain it; the platform holds a
reference and a verdict.

---

## 4.4 Technology decisions and why

### OpenFGA for the relationship graph

Zanzibar-style fine-grained authorization, chosen rather than built. The
relationship graph, inheritance, negation-free composition and consistency
problems are solved; reimplementing them is a multi-year detour.

**One shared store**, with every object namespaced by tenant and every
relation terminating at a tenant ancestor. Sovereignty comes from the model,
not from physical separation. Store-per-tenant was rejected because it makes
cross-tenant grants impossible to express without syncing tuples between
stores, leaves platform-plane tuples homeless, and multiplies schema
migrations by tenant count.

Hard data-residency requirements are met by separate **regional platform
instances**, not by splitting stores within one instance.

### PostgreSQL as the system of record

Business state, transactional integrity, row-level security, and the
allocation exclusivity constraint that keeps two independent booking
aggregates from double-allocating the same resource.

### Transactional outbox, not distributed transactions

```txt
PostgreSQL transaction
 ├── business mutation
 └── authorization outbox row
          ↓  reliable dispatcher
       OpenFGA
```

Two-phase commit across PostgreSQL and OpenFGA is substantial operational
cost for little benefit. A brief lag on *granting* authority is acceptable.
A lag on *revoking* it is not — so security-sensitive transitions
(revocation, suspension, role removal) write synchronously before the
operation becomes externally effective.

### A policy engine, not a policy language in the database

Policies are typed domain objects with known semantics, evaluated by domain
services. They are not arbitrary executable code stored as rows, and OpenFGA
is not asked to be a general-purpose policy engine.

---

## 4.5 The shape of an authorization decision

```txt
Can(subject, action, resource, context)?

  subject authenticated?                      ← IdP, global identity
      ↓
  tenant in the path?                         ← mandatory, always
      ↓
  relationship or role assignment reaches
  this resource at this scope?                ← OpenFGA
      ↓
  assignment currently valid?                 ← term, clearance, expiry
      ↓
  domain state permits the operation now?     ← PostgreSQL, policy engine
      ↓
  ALLOW / DENY
```

Steps one to four answer *entitlement*. Step five answers *permission-now*.
Conflating them is the failure mode this architecture exists to prevent.

**On unavailability**, failure is graded rather than uniform:

```txt
SECURITY-CRITICAL   fail closed    admin mutations, privileged access,
                                   role assignment
MONEY / HIGH-VALUE  fail closed    payment confirmation, expensive booking
                    or authoritative fallback
LOW-RISK            narrow, explicitly named fallback from cached decisions
```

There is no generic "authorization unavailable, therefore allow" rule
anywhere in the system.

---

## 4.6 Two administrative planes

```txt
PLATFORM PLANE              TENANT PLANE
  platform_operator           tenant_owner
  platform_security_admin     tenant_admin
  platform_support            managers / committee
  platform_auditor            members
```

Neither implies the other. Platform administration confers **no** tenant-data
access. Support and incident response are served by just-in-time privileged
access instead:

```txt
Routine support
    → request with reason, target, scope, duration
    → explicit tenant approval, SLA-bounded
    → short-lived grant, fully audited, auto-expiring

Security / legal / safety incident
    → break-glass, invoked by platform security
    → dual control: a second officer co-signs
    → tenant notified regardless of consent
    → hard expiry, mandatory post-hoc review
```

Impersonation is supported where operationally necessary, and the audit
record always preserves both principals:

```txt
actor = Bob
delegated_identity = Alice
```

No standing superuser identity exists anywhere in the platform.

---

## 4.7 What the platform deliberately does not decide

The movement's practices diverge so sharply between associations that
encoding any of the following would make the platform unimplementable
somewhere:

| Question | Resolved by |
|---|---|
| Which plans exist, and what they cost | tenant configuration |
| Whether facilities are bundled into membership | tenant configuration |
| Whether staff are also members | tenant policy |
| Which resource types exist | tenant, within system archetypes |
| Who approves membership admission | tenant governance |
| Constitutional eligibility criteria | tenant configuration |
| Whether registry screening is available | jurisdiction, tenant-enabled |
| Tax treatment | pluggable per-jurisdiction calculator |

The platform ships **machinery**, not answers. This is not indecision; it is
the direct consequence of Principle 1. A sovereign tenant that cannot express
its own constitution is not sovereign.

---

## 4.8 Solution strategy summary

| Quality goal | Approach |
|---|---|
| Tenant sovereignty | Mandatory tenant ancestor; explicit cross-tenant grants; dual-layer isolation |
| Auditability | No negative scoping; explicit sanctions; both principals recorded; blast-radius confirmation on role edits |
| Correctness under failure | Graded fail-closed; synchronous revocation; DB-enforced allocation exclusivity |
| Adaptability across jurisdictions | Tenant-configured plans, policies, types; pluggable tax and screening |
| Data protection | No PII in tuples; verdict-not-evidence; scoped, expiring consent grants |
| Comprehensibility | Five principles; no exceptions carved into them |

---

## 4.9 The invariants

Stated once, to be tested against every future change.

1. No permission resolves without a tenant in the path.
2. Cross-tenant authority is always explicit, time-bounded and audited.
3. No authority verb implies another.
4. No negative scoping exists.
5. Role assignments carry scope; role definitions may not.
6. Authorization never depends on live financial state.
7. Suspension and sanction are explicit acts with named actors, never
   derived computations.
8. The platform stores attestations, never the evidence behind them.
9. Platform administration never confers tenant-data access.
10. There is no generic fail-open.

If a proposed change requires breaking one of these, the change is wrong.
