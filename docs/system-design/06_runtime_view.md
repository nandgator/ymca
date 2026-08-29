# 06 — Runtime View

Nine scenarios chosen because each exercises a decision that is easy to get
wrong. They are not a survey of system behaviour; they are the cases where
the architecture either holds or does not.

---

## 6.1 Authorization check — the base path

Every scenario below sits on top of this one.

```txt
Caller                Auth Service         OpenFGA        PostgreSQL
  │                        │                  │                │
  │ check(principal,       │                  │                │
  │   permission,          │                  │                │
  │   resource, context)   │                  │                │
  ├───────────────────────▶│                  │                │
  │                        │                  │                │
  │                        │ 1. tenant in     │                │
  │                        │    the path?     │                │
  │                        │    (reject if    │                │
  │                        │     absent)      │                │
  │                        │                  │                │
  │                        │ 2. graph query   │                │
  │                        ├─────────────────▶│                │
  │                        │◀─────────────────┤                │
  │                        │   ALLOW / DENY   │                │
  │                        │                  │                │
  │                        │ 3. validity checks│               │
  │                        ├──────────────────┼───────────────▶│
  │                        │   term window?   │                │
  │                        │   clearance?     │                │
  │                        │   restriction?   │                │
  │                        │◀─────────────────┼────────────────┤
  │                        │                  │                │
  │◀───────────────────────┤                  │                │
  │   ALLOW / DENY         │                  │                │
```

**Step 1 is not optional and not deferrable.** A check whose resource has no
resolvable tenant ancestor is rejected before any graph query. This is the
runtime enforcement of ADR-018.

**Step 3 is why the graph alone is insufficient.** OpenFGA answers *is Alice
related in a way that grants this*. PostgreSQL answers *is that relationship
currently effective* — term not expired, clearance not lapsed, no restriction
in force. Both must pass.

### Why validity is not in the graph

Putting term windows and clearance expiry into OpenFGA would require
rewriting tuples on a clock. Then a delayed sweeper means a lapsed clearance
still authorizes, which is the exact failure the design refuses. Consulting
PostgreSQL at decision time makes expiry instantaneous by construction.

---

## 6.2 Membership admission

Exercises: approval workflow, decision routing, payment gating, outbox.

```txt
Applicant   Membership    Governance    Finance    Auth    OpenFGA
    │            │             │           │        │         │
    │ apply      │             │           │        │         │
    ├───────────▶│             │           │        │         │
    │            │ create MembershipApplication      │         │
    │            │ state: SUBMITTED                  │         │
    │            │                                   │         │
    │            │ eligibility? [Policy]             │         │
    │            │ ─ age band, plan criteria         │         │
    │            │                                   │         │
    │            │ route for approval                │         │
    │            ├────────────▶│           │        │         │
    │            │             │ who holds membership.approve  │
    │            │             │ at this scope?      │         │
    │            │             ├─────────────────────▶│        │
    │            │             │◀─────────────────────┤        │
    │            │             │  set of principals   │        │
    │            │             │                      │        │
    │            │  [human decides]                   │        │
    │            │             │ DecisionRecord(APPROVED)       │
    │            │◀────────────┤                      │        │
    │            │ state: APPROVED                    │        │
    │            │                                    │        │
    │            │ raise charges                      │        │
    │            ├──────────────────────▶│            │        │
    │            │   ENTRANCE_FEE one-time            │        │
    │            │   SUBSCRIPTION_FEE recurring       │        │
    │            │   TAX                              │        │
    │            │                       │            │        │
    │◀───────────┼───────────────────────┤ invoice    │        │
    │ pay        │                       │            │        │
    ├────────────┼──────────────────────▶│            │        │
    │            │◀──────────────────────┤            │        │
    │            │   InvoicePaid         │            │        │
    │            │                                    │        │
    │            │ ── TRANSACTION ──                  │        │
    │            │    Membership(ACTIVE)              │        │
    │            │    outbox row                      │        │
    │            │ ── COMMIT ──                       │        │
    │            │                                    │        │
    │            │         dispatcher ────────────────┼───────▶│
    │            │                                    │  tuples│
```

**Approval precedes payment.** The association decides whether to admit; the
fee follows. Reversing this would mean taking money before deciding, which
misrepresents what admission is.

**Tuple creation is asynchronous.** A grant may lag by the outbox dispatch
interval. Acceptable per ADR-026 — the new member simply cannot use
facilities for a few hundred milliseconds.

**`CONFERRED` plans skip the Finance leg entirely** and require a
`DecisionRecord(LIFE_MEMBERSHIP_CONFERRAL)` instead of an invoice.

---

## 6.3 Booking a pool slot

Exercises: the two-question split, allocation exclusivity, snapshotting.

```txt
Member      Resource      Membership    Auth      PostgreSQL
   │            │             │          │            │
   │ book       │             │          │            │
   ├───────────▶│             │          │            │
   │            │                                     │
   │            │ QUESTION 1 — entitlement            │
   │            ├────────────▶│          │            │
   │            │  resolve_entitlements(person, tenant)│
   │            │◀────────────┤          │            │
   │            │  bundles: {library, tt, pool}        │
   │            │                                     │
   │            │  does any grant cover this resource?│
   │            │  → yes                              │
   │            │                                     │
   │            │ QUESTION 2 — may it happen now      │
   │            │  AccessWindow covering 18:00?       │
   │            │  → BOOKED, capacity 8               │
   │            │  suspension active? → no            │
   │            │  booking window open? → yes         │
   │            │                                     │
   │            │ ── TRANSACTION ──                   │
   │            │   INSERT Allocation                 │
   │            ├────────────────────────────────────▶│
   │            │                    EXCLUDE constraint
   │            │◀────────────────────────────────────┤
   │            │   ok │ conflict                     │
   │            │                                     │
   │            │   INSERT Booking(CONFIRMED)         │
   │            │   snapshot cancellation_policy      │
   │            │   outbox row                        │
   │            │ ── COMMIT ──                        │
   │◀───────────┤                                     │
```

**The two questions produce different failures.** Failing question 1 means
"you need a pool subscription." Failing question 2 means "try a different
time." A system that answers both with "access denied" tells nobody anything.

**Concurrency is resolved by the database, not by application logic.** Two
members racing for the last lane both attempt the insert; the exclusion
constraint rejects one. No lock, no check-then-act window, no possibility of
a code path forgetting to check.

**Cancellation policy is snapshotted at booking time.** A tenant tightening
its terms tomorrow does not change what this member accepted today.

---

## 6.4 Role revocation

Exercises: the synchronous-revocation rule. The most safety-critical
scenario in the document.

```txt
Admin       Auth Service     OpenFGA      PostgreSQL     Caller
  │              │              │              │            │
  │ revoke       │              │              │            │
  ├─────────────▶│              │              │            │
  │              │              │              │            │
  │              │ ── TRANSACTION ──           │            │
  │              │   mark assignment revoked   │            │
  │              ├─────────────────────────────▶│           │
  │              │                              │           │
  │              │ 1. DELETE tuple — SYNCHRONOUS│           │
  │              ├─────────────▶│               │           │
  │              │◀─────────────┤               │           │
  │              │   confirmed  │               │           │
  │              │                              │           │
  │              │ 2. ── COMMIT ──              │           │
  │              ├─────────────────────────────▶│           │
  │              │                              │           │
  │◀─────────────┤                              │           │
  │   revoked    │                              │           │
  │                                             │           │
  │              │              │  any check after this point:
  │              │              │◀──────────────┼───────────┤
  │              │              │   DENY        │           │
```

**Ordering is deliberate and inverted relative to grants.** The tuple is
deleted *before* the transaction commits. If the OpenFGA delete fails, the
transaction rolls back and the revocation is reported as failed — the admin
retries. The failure mode is "revocation didn't happen and you were told so,"
never "revocation appeared to succeed but authority persisted."

Grants use the outbox and may lag. Revocations may not. **Granting may lag;
revoking may not.**

Applies identically to: membership suspension, office vacation, clearance
lapse, restriction imposition, affiliation sanction, cross-tenant grant
expiry.

---

## 6.5 Break-glass access

Exercises: two-plane separation, dual control, mandatory expiry.

```txt
Support   Security A   Security B   Platform    Tenant    OpenFGA
   │          │            │           │          │          │
   │ incident: cannot reach tenant admin, active breach       │
   ├─────────▶│            │           │          │          │
   │          │ initiate break-glass   │          │          │
   │          ├───────────────────────▶│          │          │
   │          │   reason, target,      │          │          │
   │          │   scope, duration      │          │          │
   │          │                        │          │          │
   │          │  DUAL CONTROL          │          │          │
   │          │◀───────────┤ co-sign   │          │          │
   │          │            │           │          │          │
   │          │  JIT_MAX_DURATION? [Policy, MANDATORY]       │
   │          │                        │          │          │
   │          │            │           │ notify   │          │
   │          │            │           ├─────────▶│          │
   │          │            │           │  (regardless of consent)
   │          │            │           │          │          │
   │          │            │           │ create grant on     │
   │          │            │           │ ELEVATED principal  │
   │          │            │           ├────────────────────▶│
   │          │            │           │  expires_at required│
   │◀─────────┴────────────┴───────────┤          │          │
   │  access, 30 minutes                          │          │
   │                                              │          │
   │  every action: actor + delegated_identity + case ref     │
   │                                              │          │
   │  ── expiry ──                                │          │
   │           SYNCHRONOUS tuple delete ─────────────────────▶│
   │                                              │          │
   │  mandatory post-hoc review scheduled         │          │
```

**Dual control means platform security is not a unilateral superuser.** A
single compromised security account cannot reach tenant data. This is what
keeps ADR-005 from being merely aspirational.

**The tenant is notified, not asked.** For genuine incidents, consent may be
unobtainable — but sovereignty is preserved by making the intrusion visible
and reviewable rather than by pretending it never happens.

**The grant attaches to the `ELEVATED` principal**, never the personal one.
The support engineer's routine gym booking and their break-glass access are
different subjects in every audit record.

**Routine support takes a different path**: explicit tenant approval,
SLA-bounded, no dual control needed, escalating to break-glass only if the
tenant is unreachable during an active incident.

---

## 6.6 Policy resolution with DAG conflict

Exercises: multi-path inheritance, the conflict rule from 05.7.6.

```txt
Caller                  Policy Service              Organization
  │                          │                           │
  │ resolve(OPENING_HOURS,   │                           │
  │         procter_pool)    │                           │
  ├─────────────────────────▶│                           │
  │                          │ walk auth DAG upward      │
  │                          ├──────────────────────────▶│
  │                          │◀──────────────────────────┤
  │                          │  two paths:               │
  │                          │   via Procter Chapter     │
  │                          │   via PE Department       │
  │                          │                           │
  │                          │ collect bound definitions │
  │                          │   Chapter → 21:00 (LOCAL) │
  │                          │   PE Dept → 22:00 (LOCAL) │
  │                          │                           │
  │                          │ type semantics: OVERRIDE  │
  │                          │ → nearest wins            │
  │                          │ → BOTH ARE EQUALLY NEAR   │
  │                          │                           │
  │                          │ is there a LOCAL          │
  │                          │ definition on the pool?   │
  │                          │   ├─ yes → use it         │
  │                          │   └─ no  → FAIL LOUDLY    │
  │◀─────────────────────────┤                           │
```

**This conflict is detected at configuration time, not here.** Creating the
second authorization edge, or binding the second policy, triggers the same
check and refuses. Runtime failure is the backstop, not the mechanism.

**`CONSTRAINT`, `SET` and `APPEND` types never hit this.** Strictest-across-
all-paths and union-across-all-paths are well-defined regardless of path
count. Only `OVERRIDE` and `DEFAULT` are ambiguous under a DAG.

Every resolution returns **provenance** — the chain of definitions that
produced the value — because "why is the pool shut at 21:00?" must be
answerable without reading code.

---

## 6.7 Arrears to suspension

Exercises: the detect/decide/effect separation from ADR-047.

```txt
Finance          Policy        Membership       Auth        OpenFGA
   │                │              │             │             │
   │ invoice OVERDUE│              │             │             │
   │                │              │             │             │
   │ threshold?     │              │             │             │
   ├───────────────▶│              │             │             │
   │◀───────────────┤              │             │             │
   │  30 days       │              │             │             │
   │                │              │             │             │
   │ ArrearsThresholdReached       │             │             │
   ├──────────────────────────────▶│             │             │
   │                │              │             │             │
   │                │   [automation or human ISSUES suspension]│
   │                │              │             │             │
   │                │              │ MembershipSuspension      │
   │                │              │   issued_by: <actor>      │
   │                │              │   reason: ARREARS         │
   │                │              │             │             │
   │                │              │ SYNCHRONOUS ──────────────▶│
   │                │              │             │  entitlement│
   │                │              │             │  tuples out │
```

**Finance never suspends anything.** It raises an event. Membership decides
and records a suspension with a named actor.

**Authorization never asks about invoices.** An entitlement check cannot ask
"is this invoice 31 days overdue" — it asks whether a suspension exists. That
keeps authorization independent of live financial state, and keeps the reason
someone lost access to a single auditable row.

---

## 6.8 Affiliation sanction

Exercises: the sanction/data-access separation — the sharpest expression of
Principle 2.

```txt
National      Safeguarding    Organization      Auth       Tenant
   │               │               │             │           │
   │               │ AttestationLapsed           │           │
   │◀──────────────┤               │             │           │
   │                                             │           │
   │ may_review_compliance? ───────▶│            │           │
   │◀───────────────────────────────┤            │           │
   │   yes                          │            │           │
   │                                             │           │
   │ [reviews; local body does not remedy]       │           │
   │                                             │           │
   │ sanction(affiliation, grounds)              │           │
   ├───────────────────────────────▶│            │           │
   │                                │ may_sanction on THIS   │
   │                                │ affiliation? ─────────▶│
   │                                │◀───────────────────────┤
   │                                │   yes                  │
   │                                │                        │
   │                                │ Affiliation.state      │
   │                                │   → SUSPENDED          │
   │                                │ DecisionRecord         │
   │                                │ notify ───────────────▶│
   │                                                         │
   │ ─────────── attempt to read member data ───────────────▶│
   │◀───────────────────────────────────────────── DENY ─────┤
   │   no path from national body to tenant member records   │
```

**The sanction acts on the affiliation, never through it.** `may_sanction` is
granted on the `affiliation` object, which is why it is a first-class FGA
type (05.1.9). A national body can suspend a local association entirely and
still have no path to a single member record.

This is testable, and should be in the test suite as an explicit negative
case.

---

## 6.9 Authorization store unavailable

Exercises: graded failure from ADR-027.

```txt
                    OpenFGA unavailable
                            │
        ┌───────────────────┼───────────────────┐
        ▼                   ▼                   ▼
  SECURITY-CRITICAL    MONEY / HIGH-VALUE   LOW-RISK
        │                   │                   │
  role assignment      payment confirm     view own booking
  privileged access    expensive booking   read a programme
  policy change        stay approval       walk-in entry
        │                   │                   │
        ▼                   ▼                   ▼
    FAIL CLOSED         FAIL CLOSED        cached decision,
                        or authoritative   bounded TTL,
                        fallback           logged
```

**There is no generic fail-open.** Each low-risk fallback is named explicitly
in its context's specification. A permission not on the named list fails
closed by default — the absence of a decision is a denial.

Cached decisions carry a bounded TTL and every use during an outage is
logged, so the blast radius of a stale allow is knowable afterwards.

**Walk-in entry is on the low-risk list deliberately.** It is the one
high-volume, low-consequence check where denying every member during an
outage causes real harm at the door and permitting a lapsed member for a few
minutes causes almost none.

---

## 6.10 Scenario coverage

| Scenario | Decisions exercised |
|---|---|
| 6.1 Authorization check | ADR-018, ADR-025, ADR-070, ADR-087 |
| 6.2 Membership admission | ADR-046, ADR-026, ADR-050, ADR-083 |
| 6.3 Booking | ADR-023, ADR-057, ADR-059, ADR-060 |
| 6.4 Role revocation | ADR-026 |
| 6.5 Break-glass | ADR-005, ADR-066, ADR-067, ADR-068 |
| 6.6 Policy resolution | ADR-016, ADR-036, ADR-037 |
| 6.7 Arrears | ADR-047 |
| 6.8 Sanction | ADR-006, ADR-009, ADR-074 |
| 6.9 Unavailability | ADR-027 |

Scenarios not documented here — renewal, enrollment, stay check-in,
reconciliation — follow patterns established above and are specified in their
context subsections.
