# 12 — Glossary

The ubiquitous language. Terms are used in exactly these senses throughout
the document and should be used the same way in code, schema and
conversation.

Pairs that are routinely confused are grouped under **Distinctions** at the
end; those are worth reading even if you skip the alphabetical list.

---

## Terms

**Access mode** — Whether a resource may be used walk-in or requires booking
during a given time window. A property of the schedule window, never of the
resource itself.

**ADR** — Architecture Decision Record. See section 09.

**Affiliation** — A stateful relationship between two organizations, one of
which recognizes the other as part of the movement. Carries a state
(`APPLIED`, `AFFILIATED_IN_GOOD_STANDING`, `AFFILIATED_IN_ARREARS`,
`SUSPENDED`, `LAPSED`, `WITHDRAWN`). Confers no authority by itself.

**Allocation** — The reservation of a resource's capacity for a time range.
The shared substrate beneath Booking and Stay, and where exclusivity is
enforced.

**Allocation unit** — The atomic reservable thing for a resource: `BED` or
`ROOM` for accommodation, a lane or a slot elsewhere. Varies by room type.

**Archetype** — A system-defined behavioural category a tenant-created
resource type must declare: bookable-slot, allocatable-unit, walk-in-only,
non-consumable. Fixes behaviour while leaving vocabulary to the tenant.

**Attestation** — A recorded verdict about a person or organization —
cleared, certified, verified — without the underlying evidence. See
Principle 5.

**Authority verb** — One of the five independent powers one organization may
hold over another: `may_set_policy`, `may_review_compliance`, `may_sanction`,
`may_administer`, `may_read_member_data`.

**Authorization containment** — The deliberately maintained graph that
governs how scope propagates. A DAG. Permitted to disagree with both
organizational ownership and physical location.

**Beneficiary** — The person who consumes an operation, who may differ from
the actor performing it. A guest occupying a room booked by a member.

**Booking** — A reservation of a resource for a discrete slot. An independent
aggregate with its own state machine, sharing nothing with Stay but the
underlying Allocation.

**Break-glass** — Emergency privileged access invoked by platform security
for a genuine incident. Requires dual control, notifies the tenant regardless
of consent, expires hard, and triggers mandatory review.

**Chapter** — One type of Organizational Unit. Analogous to "branch." Not a
distinct domain concept.

**Clearance** — A Verification of type background check. A precondition on
holding roles that declare it. Expires; renders an assignment inert on
expiry.

**Consent grant** — A tenant-scoped, purpose-bound, expiring, revocable
permission — typically given by a guardian for a minor's data. Distinct from
the guardianship relationship that makes it possible.

**Dependent** — A person covered by another's membership without being its
primary holder. Commercial, not legal.

**Enrollment** — A person's participation in a specific Offering. Not a
membership.

**Entitlement** — What a person is related to or permitted in principle.
Answered by authorization. Distinct from whether an operation may happen now.

**Guardianship** — A Person→Person relationship establishing who may consent
for, receive communications about, and act on behalf of a minor. Global,
evidenced, and independent of any membership.

**Hosteller** — A person holding an accommodation relationship. A
relationship, not a person type.

**JIT privileged access** — Just-in-time elevation of a human's authority for
a stated reason, target, scope and duration, with full audit and automatic
expiry. Replaces standing administrative identities.

**Occurrence** — A single scheduled session of an Offering. Tuesday at 18:00.

**Offering** — A specific commercial or operational instance of a Programme.
The September 2026 beginner batch. Need not be commercial.

**Office** — A constitutional position with a term and governance meaning:
President, Treasurer, General Secretary. May confer a Role. Not itself a
permission bundle.

**Organizational Unit** — Any structural subdivision of a tenant, typed:
`CHAPTER`, `DEPARTMENT`, `CENTRE`, `REGION`, `INSTITUTE`, `PROJECT`. One
concept, many types, arbitrary depth.

**Outbox** — The table written inside the business transaction and dispatched
reliably to OpenFGA, avoiding distributed transactions.

**Party** — Either a Person or an Organization, in a financial context. Both
sides of every invoice are Parties.

**Person** — A human. Global, one record per human across the platform,
independent of any tenant.

**Plane** — One of the two independent authority domains: platform or tenant.
Neither implies the other.

**Policy** — A typed domain object with known semantics, at one of three
levels: `MANDATORY`, `DEFAULT`, `LOCAL`. Not executable code.

**Principal** — An authenticated identity. One person may deliberately hold
several — personal, staff — that are genuinely distinct, not aliases.

**Programme** — Something the organization offers. Swimming, counselling,
outreach. Distinct from the resources it may consume.

**Propagation** — Whether a permission granted at a scope node reaches that
node's descendants. Declared per permission, not per role.

**Resource** — Something whose capacity or availability can be consumed: a
pool, a gym, a room, a bed, a hall.

**Restriction** — A recorded bar on a person holding certain roles, typically
youth-facing. An authorization fact. The case behind it is out of scope.

**Role definition** — A named, reusable bundle of system-defined permissions.
May be global.

**Role assignment** — The grant of a role definition to a subject at a scope.
Always scoped; may carry a term.

**Sanction** — An explicit act by a body holding `may_sanction`, imposing
consequences on an affiliate. Never automatic, never derived from a state
change.

**Scope** — The target of a role assignment. Any authorization object.
Governed by authorization containment, not by domain containment.

**Sovereignty** — A tenant's exclusive authority over its own people, data,
money and facilities, enforced structurally rather than by convention.

**Stay** — An accommodation relationship consuming a bed or room over a
continuous period. Requires approval. An independent aggregate, distinct from
Booking.

**Tenant** — A sovereign association. The root of every authorization path
and an immutable security boundary.

**Term** — The validity period of a role assignment. Declared as
`PERMANENT`, `OPTIONAL_TERM` or `MANDATORY_TERM` on the role definition.

**Verification** — A first-class record that something was checked: student
status, identity, guardianship, clearance, compliance. Carries verifier,
timestamp, expiry and status — never the evidence.

**Waitlist** — Reserved as a status; the feature is deferred. Capacity checks
return "would waitlist" rather than hard rejection so it can be built later.

---

## Distinctions

These pairs are the ones that cause the most confusion. Each is a real
architectural boundary, not a naming preference.

**Membership vs Enrollment**
Membership is the relationship with the association. Enrollment is
participation in an offering. Five subscriptions do not make five
memberships.

**Office vs Role**
An Office is a constitutional position with a term. A Role is a permission
bundle. An office may confer a role; neither is the other.

**Role definition vs Role assignment**
The definition is a template and may be global. The assignment binds it to a
subject at a scope and is never global.

**Scope vs Organizational containment**
Scope governs authority propagation. Organizational containment describes
ownership. They are maintained separately and are permitted to disagree.

**Organizational vs Physical vs Authorization containment**
A department may own a pool, the pool may sit at a branch, and both may hold
authority over it. Three independent relationships.

**Affiliation vs Authority**
Being affiliated confers nothing. Each of the five authority verbs is granted
separately and independently.

**Authorization vs Eligibility**
Authorization says Alice is entitled. Domain rules say whether it may happen
right now. Never conflate them.

**Guardianship vs Dependency**
Guardianship is legal and concerns consent. Dependency is commercial and
concerns coverage and payment. They frequently coincide and must not be
merged.

**Actor vs Beneficiary**
The member who books is the actor. The guest who stays is the beneficiary.
Both are recorded.

**Actor vs Delegated identity**
Under impersonation, both are always recorded. `actor = Bob,
delegated_identity = Alice`.

**Suspension vs Sanction**
Suspension applies to a person's membership. Sanction applies to an
organization's affiliation. Both are explicit acts with named actors; neither
is ever a derived computation.

**Attestation vs Evidence**
The platform stores the first and never the second.

**Booking vs Stay**
Independent aggregates with independent state machines. They share only the
Allocation beneath them.

**Walk-in vs Booked**
A property of a time window on a resource, not of the resource.

---

## Terms deliberately not used

**Branch** — use *Chapter*, or *Organizational Unit* when the type is not
significant. "Branch" is ambiguous with version control in engineering
conversation.

**Parent YMCA** — implies authority that affiliation does not confer. Name
the specific authority verb instead.

**Superuser / God mode** — no such identity exists. Use *JIT privileged
access* or *break-glass*.

**Service account** (for humans) — a service account represents a non-human
workload. Temporary human elevation is *JIT privileged access*.

**Tenant admin** (as a synonym for platform access) — platform administration
and tenant administration are separate planes and must not share vocabulary.

**Permission group** — use *Role definition*.

**Facility** — ambiguous between Organizational Unit and Resource. Use the
precise term.
