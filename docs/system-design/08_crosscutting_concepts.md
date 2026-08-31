# 08 — Crosscutting Concepts

Patterns that appear in several contexts and must be implemented identically
in each. Where a context deviates, its subsection says so explicitly.

---

## 8.1 Authorization

### The check interface

```txt
check(principal, permission, resource, context) → ALLOW | DENY
```

Domain services never implement authorization rules. They ask. A service
containing `if user.role == "admin"` is a defect regardless of whether the
condition is correct.

### The three-part decision

```txt
1  TENANT       is there a tenant ancestor on the path?     mandatory
2  GRAPH        does a relationship or role reach this?     OpenFGA
3  VALIDITY     is that relationship currently effective?   PostgreSQL
```

Part 3 covers term windows, clearance validity, and restrictions. It exists
because temporal state in the graph would require rewriting tuples on a
clock, making expiry dependent on a sweeper.

### Permission naming

```txt
<object>.<verb>

membership.approve      resource.close      finance.refund
member.read             booking.cancel      safeguarding.read
```

Permissions are system-defined and immutable (ADR-076). Roles bundle them and
are tenant-configurable.

### What is never in the graph

```txt
NOT IN OPENFGA
    membership suspension          business state
    booking, stay, allocation      business state with periods
    clearance validity             temporal, sweeper-dependent
    invoice contents               business state
    policy values                  domain evaluation
    PII of any kind                ADR-030
```

The rule: **if it changes for business reasons, it is not authority.**

---

## 8.2 Tenancy

### Enforced at four layers

```txt
API          tenant carried explicitly in the request, never inferred
             from a resource lookup
AUTHZ        no relation resolves without a tenant ancestor
SERVICE      tenant boundary checked independently of authorization
DATABASE     row-level security on tenant_id
```

Independence is the point. An authorization defect alone must not be
sufficient to expose another tenant's rows.

### Row-level security

```sql
ALTER TABLE <table> ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON <table>
  USING (tenant_id = current_setting('app.tenant_id')::uuid);
```

`app.tenant_id` is set per transaction from the authenticated request context
and never from user input.

Per transaction is not a stylistic preference. The pool hands a connection to
the next, unrelated request as soon as this one returns it, so a value set on
the connection would outlive the request that set it and be read by whoever
got the connection next. The setting is therefore made local to the
transaction, and there is no code path that sets it any other way.

`current_setting` is called without `missing_ok`, so a query that reaches a
tenant-scoped table outside such a transaction **raises** rather than quietly
matching no rows. That is the fail-closed half of the design: a forgotten
tenant context is a loud error, not an empty list that looks like a legitimate
answer. It must not be softened by catching that error anywhere.

**Tables exempt from RLS**, and why:

```txt
person              global by design (05.2.2); visibility is
                    controlled by relationship, not by row
guardianship        global; concerns two humans, not a tenant
principal           global; belongs to a person
restriction         may be platform-wide (05.9.9)
```

Each exemption is deliberate, listed here so the set is auditable, and
protected by application-level relationship checks instead.

### The two cross-tenant flows

Only two exist. Both are explicit, audited, and time-bounded where possible:

```txt
CrossTenantGrant           05.1.6   scoped, expiring, reasoned
Platform-wide restriction  05.9.9   safeguarding, platform authority
```

Any third would be a change to Principle 1 and belongs in this document
before it belongs in code.

---

## 8.3 Consistency

### The asymmetry

```txt
GRANTS may lag        outbox dispatch, sub-second target
REVOCATIONS may not   synchronous write before commit
```

### Grant path

```txt
BEGIN
  pg_advisory_xact_lock(hash(fence key))
  business mutation
  INSERT INTO authorization_outbox (..., fence key)
COMMIT
        ↓  dispatcher
    OpenFGA
```

### Revocation path

```txt
BEGIN
  pg_advisory_xact_lock(hash(fence key))
  mark revoked in PostgreSQL
  UPDATE authorization_outbox SET voided_at = now()
    WHERE fence key matches AND dispatched_at IS NULL
  DELETE tuple from OpenFGA        ← synchronous, before commit
  if delete fails → ROLLBACK, report failure
COMMIT
```

The failure mode is "revocation did not happen and you were told," never
"revocation appeared to succeed but authority persisted."

### Operations requiring synchronous revocation

```txt
role assignment revoked        membership suspended / lapsed / terminated
office vacated                 dependent removed
clearance lapsed               subscription lapsed
restriction imposed            authority verb revoked
affiliation sanctioned         cross-tenant grant expired
guardianship ended             consent withdrawn
principal suspended            committee membership removed
```

### Outbox

```txt
authorization_outbox
    id, aggregate_type, aggregate_id, event_type, payload,
    fence_subject, fence_relation, fence_object,
    created_at, dispatched_at, voided_at, attempts, last_error
```

At-least-once delivery. Tuple writes are idempotent, so redelivery is safe.
Undispatched rows older than a threshold are an operational alert — a
silently stalled dispatcher means authority is not being granted.

This table projects authorization facts and nothing else. It is not the
inter-context event bus of 05.0.8.

### The fence

Idempotency is not ordering. A row pending while a revocation runs would
otherwise be applied afterwards and restore the tuple the revocation removed,
with no synchronous path left to run again. Three obligations, per ADR-101:

```txt
1  every row projecting a revocable relation names it
       fence_subject, fence_relation, fence_object — domain level;
       the dispatcher renders the tuple

2  grant and revoke of one fact serialize
       pg_advisory_xact_lock(hash(fence key)) in either transaction

3  the dispatcher holds its row lock across the OpenFGA write
       SELECT ... FOR UPDATE SKIP LOCKED
           WHERE dispatched_at IS NULL AND voided_at IS NULL
       write tuple, set dispatched_at, COMMIT
```

The lock and the void are shown in the revocation path above. Voided rows are
retained, not deleted — a row that had to be fenced is evidence of a race that
reached production, and belongs in the record.

Obligation 3 holds a row lock across a network call. `SKIP LOCKED` confines
that to the one row; a statement timeout bounds it, rolling back and leaving
the row pending for retry.

---

## 8.4 Snapshotting

A pattern that emerged independently in three contexts and should be applied
uniformly.

> **When a person agrees to terms, capture the terms. Do not reference them.**

```txt
Enrollment.price_snapshot            05.4.5
Booking.cancellation_policy_id       05.5.5   snapshot, not a live reference
Invoice.tax_snapshot                 05.8.4
Membership.plan                      versioned, never edited in place
```

The general rule: a tenant changing its terms must never retroactively alter
what someone already accepted. Any future field that participates in an
agreement between the association and a person is a snapshot candidate.

**Distinguish from history.** Snapshotting captures what was agreed;
versioning (plans, policies, offerings) captures what was published. Both are
needed and they are not the same mechanism.

---

## 8.5 Audit

### What is always audited

```txt
Every authorization decision that DENIES
Every privileged access grant and every action within it
Every role, office, or authority change
Every restriction read, imposition, or lifting
Every financial mutation
Every policy definition or binding change
Every cross-tenant operation
```

**An authentication failure is not one of these.** It is refused before any
principal or tenant exists, so there is no actor to name and no tenant whose
isolation policy the row could satisfy — `audit_event` is tenant-scoped like
every other table, and a row with no tenant is not insertable from a tenant
connection at all. Those failures go to the structured log, correlated by the
same request id (A3.4). A tenant mismatch is the opposite case and _is_
audited: it has an authenticated principal and a tenant the caller named.

### Record shape

```txt
AuditEvent
    id
    tenant_id             nullable for platform-plane events
    actor_principal_id
    delegated_identity_id nullable — impersonation (ADR-068)
    action
    object_type, object_id
    outcome               ALLOWED | DENIED | ERROR
    severity              INFO | NOTABLE | HIGH
    context               request id, source, JIT grant reference
    occurred_at
```

**Both principals, always.** Under impersonation the record shows
`actor = Bob, delegated_identity = Alice`. A record showing only one is
forensically useless.

### High severity

```txt
break-glass invocation and every action within it
restriction imposed or lifted
affiliation sanction
role definition edited (with blast radius)
cross-tenant grant created
platform-wide restriction
payment reversed
```

High-severity events route to a separate stream with independent retention
and alerting.

### Audit is append-only

No update, no delete. Retention is longer than every other data class and
survives `SCRUBBED` persons — the trail must outlive the personal data it
references, which is why audit records hold identifiers rather than names.

---

## 8.6 Data protection

### Storage rules

```txt
NEVER STORED
    criminal record content, offence details
    registry match content
    allegation narratives, investigation material
    special-category attributes as Person fields
    PII in authorization tuples

STORED AS VERDICT ONLY
    background clearance      status, provider, dates
    registry screening        match / no-match
    compliance attestation    status, standard, period
```

### Erasure

Implemented as `SCRUBBED`, not `DELETE`. Personal fields nulled; structural
references and audit preserved.

**Known tension, recorded rather than resolved:** erasure rights conflict
with safeguarding and financial retention obligations. The design preserves
audit and structure by default and expects tenants to configure retention per
jurisdiction. See 11.

### Consent

Purpose is enumerated, never free text — free-text purposes cannot be
reasoned about at query time and make "what has this guardian agreed to?"
unanswerable. Withdrawal preserves the record, since it is the evidence that
prior processing was lawful.

### Minors

```txt
consent required from a verified guardian
communications route to the guardian by default
records visible to guardians and operational role-holders only
optional fields collected only against a stated purpose
age of majority read from Tenant.jurisdiction, never hard-coded
```

---

## 8.7 Time and scheduling

### Storage

All timestamps `timestamptz`, stored UTC. All ranges `tstzrange`.

### Display

Rendered in the **resource's** timezone, not the viewer's. A member in
Bengaluru booking a Mumbai pool sees Mumbai time. Booking systems that render
in viewer-local time produce a class of error that is easy to create and hard
to detect.

### Expiry

```txt
Evaluated at decision time, never by a sweeper.
```

Applies to terms, clearances, verifications, consent, JIT grants and
cross-tenant grants. Sweepers exist only for notification and cleanup — never
for enforcement.

### Notification before silent expiry

```txt
OfficeTermExpiring         90 / 30 / 7 days
VerificationExpiring       60 / 30 / 7 days
MembershipExpiring         60 / 30 / 7 days
```

Decision-time enforcement is correct but abrupt. Discovering at 00:01 that
authority has ended is the right behaviour and a bad experience.

---

## 8.8 Errors

### Denial must be specific

```txt
WRONG    "Access denied"

RIGHT    entitlement    "A pool subscription is required"
         availability   "That session is full"
         window         "Booking closes 2 hours before the session"
         suspension     "Your membership is currently suspended"
         clearance      "An active clearance is required for this role"
```

The two-question split (Principle 3) exists partly so these produce different
messages. Collapsing them wastes the distinction the architecture paid for.

### Denial must not leak

```txt
Never reveal     that a person exists in another tenant
                 the contents or existence of a restriction to
                 anyone but authorized readers
                 why an application was rejected (05.3.4)
                 which specific registry matched
```

Where specificity and non-disclosure conflict, non-disclosure wins and the
event is audited.

---

## 8.9 Idempotency

```txt
Tuple writes            naturally idempotent
Outbox dispatch         at-least-once; consumers must tolerate redelivery
Payment webhooks        untrusted, verified, deduplicated by provider
                        reference, reconciled against a provider query
Allocation inserts      the exclusion constraint makes retry safe
Invoice numbering       NOT idempotent — a counter row incremented
                        inside the issuing transaction. ADR-103
```

---

## 8.10 Configuration versus code

```txt
CODE                       CONFIGURATION
─────────────────────────────────────────────────────
permissions                roles
policy types               policy definitions and bindings
resource archetypes        resource types
FGA model                  tuples
plan machinery             plans, prices, entitlement bundles
state machines             approval routing
tax interface              tax treatments
```

Adding a policy type or a permission is a platform release. That friction is
deliberate: it forces someone to define the semantics before tenants can
depend on them.

---

## 8.11 Reading and listing

Every scenario in 6 is a write or a single-object check. This section covers
the other half of the traffic, which the admin surfaces are almost entirely
made of.

### One check, then SQL

A list names the scope it lists under. Authority is held over that scope, so
it is checked once (ADR-104):

```txt
1  resolve the scope object from the path
2  check(caller, permission, scope)      one graph query
3  SELECT ... WHERE scope = $1           keyset pagination, under RLS
```

There is no `ListObjects`, no permission projection, and no reconciliation
between two stores at page boundaries. List latency is a database property
with an index behind it.

### Rows with independent authorization

Where a row can be authorized by something other than its scope, that must be
stated where the list is defined, and those rows are checked individually over
the returned page:

```txt
page size N  →  at most N checks, never |result set| checks
```

If a screen needs this on every row, it is usually the wrong scope: the fix is
to list under something the caller holds authority over, not to check harder.

### Under-reporting is the safe direction

A caller who could reach a row by some path other than the named scope sees
fewer rows than they are entitled to. That is deliberate.

```txt
absent from a list   ≠   does not exist
absent from a list   ≠   caller may not see it
```

A list must therefore never be used to prove a negative. Uniqueness,
existence and eligibility are decided by a constraint or a check, never by an
empty page — which is the same rule 11.3 applies to duplicate detection, for
the same reason.

### Pagination

Keyset, not offset. Offset pagination over a table that is being written to
skips and repeats rows, and consumption records are written continuously
during service hours.

```txt
GET  ...?limit=50&cursor=<opaque>
     → { items: [...], next_cursor: <opaque|null> }
```

The cursor encodes the sort key of the last row and is opaque to the client,
so the sort can change without breaking pagers.

---

## 8.12 Testing

The invariants from 04.9 are testable and should be tested as explicit
negative cases:

```txt
1   no permission resolves without a tenant ancestor
2   a body with may_sanction cannot read the sanctioned tenant's data
3   no authority verb grants another
4   negative scoping cannot be expressed
5   a role assignment without scope is rejected
6   an expired term denies at decision time, with no sweeper run
7   an expired clearance denies at decision time
8   a lapsed suspension is a row, not a computation over invoices
9   platform administration alone reaches no tenant object
10  no permission fails open
```

Test 2 is the sharpest: a national body holding **all five** authority verbs
must be denied every path to a member record, resource, invoice or unit. It is
the single test that proves the design's central claim.

The strong form is only expressible because `may_administer` and
`may_read_member_data` are grantable relations that resolve to nothing
(ADR-102). While they were absent from the schema, this test could grant only
three verbs and proved correspondingly less.

### Model testing

The FGA model is a versioned artifact with its own test suite —
assertion-based tuple tests run in CI, and no model change ships without
them.
