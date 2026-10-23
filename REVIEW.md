# Design review — open items and decisions

Working document. Untracked by intent; not part of the design record.
Source: full read of `docs/system-design/` (8,404 lines, 95 ADRs) on 2026-08-29,
plus decision rounds with Joe the same day.

Status: `todo` | `wip` | `done` | `deferred` | `wontfix`

---

## 0. Established context

These came out of questioning, not from the document. Several contradict it.

| #   | Established                                                                             | Consequence                                                                                |
| --- | --------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| K1  | Real deployment, not an exercise                                                        | Operational gaps (API, read model, auth, onboarding) outrank internal elegance             |
| K2  | The hostel mess problem is still live, and the hostel is the **v1 tenant**              | The platform's first slice must deliver mess management on platform foundations            |
| K3  | Several associations expected on day one                                                | Multi-tenancy is real; affiliation and authority verbs are justified                       |
| K4  | Solo build, hostel is waiting                                                           | **Standing top risk.** See R1                                                              |
| K5  | Auth is swappable at **deployment** level (Cognito/Supabase/Appwrite), not tenant level | ADR-039 and R3 survive intact. Auth becomes a port with one concrete pick per deployment   |
| K6  | Next milestone is a vertical slice                                                      | Design only what the slice needs; defer the rest                                           |
| K7  | Consumption is a **generalized** record, not mess-specific                              | Meals, check-ins, attendance, issues — one tenant-typed mechanism                          |
| K8  | Slice = **entry to bill, end to end**                                                   | Tenant, Person, Membership, Consumption, Finance. Anything less leaves the hostel on paper |
| K9  | **OpenFGA from day one**                                                                | Authorization does not retrofit; deferring it rebuilds the prototype with extra steps      |
| K10 | **Full Finance context**, not a computed number                                         | Promotes two declared gaps to blocking. See F1                                             |
| K11 | Order: design Consumption -> fix defects -> code                                        | Matches how the rest of the document was produced                                          |

### F1 — Full Finance promotes declared gaps to blocking

`05.8.13` listed gapless invoice numbering as _"required, but the
concurrency-safe allocation mechanism is not specified"_. **Resolved** by
ADR-103: a counter row per tenant per financial year, incremented inside the
issuing transaction. A sequence cannot do it — `nextval` is non-transactional,
so a rollback leaves a gap.

Instalments remain unmodelled — a bill paid in parts is still three invoices.
Deferred until the hostel needs it.

**Mapping already works.** The mess bill fits the existing charge model:

```txt
Charge(source_type = CONSUMPTION, source_id = billing period)
    SUBSCRIPTION_FEE  RECURRING   flat monthly fee
    DISCOUNT          signed -ve  long-leave deduction
    USAGE_FEE                     optional breakfast items
    USAGE_FEE                     non-veg dinner surcharges
    TAX
```

Finance needs one change: `CONSUMPTION` added to `Charge.source_type`
(today `MEMBERSHIP | SUBSCRIPTION | ENROLLMENT | BOOKING | STAY | DEPOSIT |
DUES | ADJUSTMENT`). A Published Language change per 05.0.4, not a redesign.

### R1 — Standing risk: capacity versus scope

Ten bounded contexts, an FGA model with CI assertions, and sixteen synchronous
revocation paths, built solo, for a customer already waiting. Raised twice;
scope was cut only by deferring tenant-level BYO IdP (which proved to be a
misunderstanding and therefore cost nothing). **Unresolved.** Revisit at the
first slipped milestone rather than pretending it is closed.

**2026-08-29, slice scoping.** Raised a third time. Joe chose the largest of
three slice options — backend, admin and member mobile — against a
recommendation of backend plus a warden UI. Partly offset by choosing one
permission-gated Flutter app over two clients, which removes a whole surface.
Net: still the top risk, and now with a first milestone that includes two user
roles, a greenfield backend, full Finance and OpenFGA.

---

## 1. The mess collision ← blocks the slice

The platform models no consumption record that is not a reservation.
`Booking` and `Stay` exist to create an `Allocation`, whose sole purpose is
exclusivity. `WALK_IN_ONLY` (05.5.2) enforces nothing and **records nothing**.

A meal needs no exclusivity and must be recorded. So does a gym check-in, a
class attendance mark, a library issue. The design has no shape for any of them.

The mess specifically needs, per `prototype/CONTEXT.md`:

```txt
Entry(person, date, meal_type, items[], veg_flag)
    submitted same-day, editable until midnight, then immutable
Absence defaults that differ per meal type
    breakfast absent -> not billed;  dinner absent -> billed
Leave(person, start, end) with LONG/SHORT classification by tenant threshold
Month-end aggregation into a bill
```

None of this is expressible today. **Decision: add a tenth bounded context,
Consumption, generalized.**

### Resolved semantics (round 5)

| #   | Choice                                                                    | Consequence                                                                                                                                                                                                                   |
| --- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M1  | **Reservation vs realization**                                            | `Booking`/`Stay` record reserved use; `Consumption` records actual use. A record may optionally reference the allocation it realizes. Walk-in finally has something to record                                                 |
| M2  | **Obligation minus exceptions**, and obligations are _optional machinery_ | A `ConsumptionType` declares whether it obligates. Dinner does, breakfast does not — so the per-meal absence default disappears rather than being configured                                                                  |
| M3  | **Corrections, not edits**                                                | Mirrors the invoice rule already in the design. A record is superseded, never mutated                                                                                                                                         |
| M4  | **Consumption owns `ExpectedAbsence`**                                    | Leave is the primary exception to an obligation; splitting them across contexts would force a cross-context query at billing time, which 05.0.8 prohibits. Not Membership: suspension removes entitlement, leave removes none |

M2 was qualified by Joe as _local to this hostel; may vary elsewhere_ — hence
obligations are opt-in per consumption type, never a platform assumption.

M4 detail — two independent relief dimensions replace the prototype's LONG/SHORT enum:

```txt
ExpectedAbsence relieves
    the obligation to record     always
    the obligation to pay        by tenant policy threshold
```

Relief flags are snapshotted at declaration (pattern 8.4), so a tenant changing
its threshold cannot retroactively rebill a past absence.

---

## 2. Defects — the design states a guarantee it does not deliver

| #   | Item                                                             | Status | Lands in                        |
| --- | ---------------------------------------------------------------- | ------ | ------------------------------- |
| A1  | Outbox can resurrect revoked authority                           | done   | ADR-101, 8.3, 6.4, A2           |
| A2  | Cached ALLOW contradicts synchronous revocation                  | todo   | new ADR + 6.9                   |
| A3  | `may_administer` / `may_read_member_data` absent from FGA schema | done   | ADR-102, A1, 05.1, 8.12, 10, 11 |

**A1.** Grants dispatch async (8.3); revocations write synchronously (6.4).
Grant commits an outbox row -> dispatcher stalls (7.4 alerts, does not block)
-> revocation deletes a tuple that does not exist yet, a no-op -> commits,
reported successful -> dispatcher recovers, applies the stale row -> tuple
exists again, and no synchronous path runs a second time.
Idempotency (8.9) is not ordering. The validity check (6.1 step 3) covers term
windows, clearance and restrictions — not revocation — so the tuple is the sole
source of truth. This is verbatim the failure 6.4:210 says cannot happen.
**Fixed** by ADR-101. Outbox rows name the _fact_ — not the tuple; the
dispatcher renders that, so no model version is frozen into stored payloads.
Revocation voids pending rows for its fence key, then deletes.

The fix direction first noted here was half wrong, and worth recording. Two
distinct races need two distinct locks:

```txt
revoke vs dispatcher       row lock, held across the OpenFGA write
revoke vs a concurrent     pg_advisory_xact_lock on the fence key —
  granting transaction     a grant that began before the revoke and
                           commits after it is invisible to the void
```

Only the first was in the original note. The second is the one that survives a
careless implementation of the first.

**A2.** 6.9 permits TTL-bounded cached ALLOW for low-risk permissions including
walk-in entry; 8.3 says revoking may not lag. A safeguarding restriction does not
reach a door terminal holding a cached allow. 6.9 reasons only about the outage
case; normal-operation cacheability is undefined.

**A3.** 05.1.5 and A2 enumerate five authority verbs; A1's `affiliation` type
defined three. **Fixed** by ADR-102: deliberate, and now said. The three that
resolve act _on_ the affiliation; the two that did not exist would have acted
_through_ it, which Principle 1 forbids. Both are now grantable and resolve to
nothing, so holding them is auditable and the refusal is visible.

Two findings the original note missed:

- **`may_administer` was defined twice.** `tenant.may_administer` was an
  unrelated internal relation feeding `organizational_unit.admin`. Wiring the
  cross-tenant verb into it — the obvious reading of the shared name — would
  have handed a national body admin over every unit in the target tenant.
  Renamed `administered_by`; logged as temptation 11 in 11.4.
- **8.12 test 2 was weaker than it claimed.** "The single test that proves the
  design's central claim" could grant only three verbs, and its
  `natl-sec may_administer tenant:bombay` DENY line tested the internal
  relation nobody had granted. `natl-sec` now holds all five and is asserted
  against nine paths.

---

## 3. Undesigned surfaces

| #   | Item                            | Status   | Note                                                                                                                                                     |
| --- | ------------------------------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B0  | **Consumption context**         | **done** | 05.10 + ADR-096..100. See section 1                                                                                                                      |
| B1  | Read/query model                | partial  | Most consequential. `grep ListObjects` and `grep pagination` -> zero hits                                                                                |
| B2  | API layer                       | partial  | Nine contexts, zero endpoints. 8.2:65 makes "tenant carried explicitly" load-bearing in the one layer never specified                                    |
| B3  | Authentication                  | partial  | Reduced by K5. Still needs: session/refresh lifetime, how a token names the tenant, MFA on the platform plane, ELEVATED issuance, service-to-service     |
| B4  | Notification                    | todo     | Required by 8.7, 05.6, 05.8, 05.9, 6.5 and C8. Owned by nobody. C8 is regulatory                                                                         |
| B5  | Tenant onboarding / bulk import | todo     | Rosters, membership numbers, historical invoices. C11 gapless numbering conflicts with importing history                                                 |
| B6  | Inter-context domain event bus  | todo     | Opened by A1. `05.0.8` calls the outbox the only way state propagates between contexts; the only outbox in `A2` feeds OpenFGA                            |
| B7  | Operator / admin CLI surface    | todo     | Still roadmap. Weighed and rejected for 8.3's configuration surface (R9); the argument for it stands, the cost is pulling the framework decision forward |
| B8  | Plan lifecycle over the API     | todo     | A3.7 creates and lists plans; 05.3.2's supersession, retirement and `CLOSED_TO_NEW` are unreachable. Opened by R9                                        |

**B1 detail.** The Zanzibar reverse-index problem: "list members of this
chapter, filtered, sorted, paginated, authorization-aware". **Mechanism
designed** (ADR-104, 8.11): a list names its scope, the scope is checked once,
and rows come back by keyset-paginated SQL under RLS. No `ListObjects`, no
permission projection. Under-reports rather than over-reports, so a list may
never be used to prove a negative.

Still open: `10` sets a p99 for `check()` only and now needs one for lists, and
the per-row exceptions are named per endpoint rather than enumerated.

**B2 detail.** Minimal contract written (A3): tenant as a path segment
(ADR-105), bearer token through the auth port, keyset pagination, idempotency
keys, 403/404 indistinguishability, and the entry-to-bill endpoints. Nine
contexts still have no surface — deliberately.

**B3 detail.** Port defined (ADR-106) with `PERSONAL`/`ELEVATED` principals and
a development implementation. Session lifetime, refresh, MFA and ELEVATED
issuance remain undesigned; none changes the port's shape.

**B7 detail.** Raised 2026-09-02 as the question "can this system support a
robust CLI", checked against the code. Transport lives only in
`internal/httpx` and `cmd/api`; `authz.Check`, `db.InTenantTx` and the ADR-106
auth port are all transport-neutral, and the nine remaining contexts are
planned as `internal/` packages reusable from any binary. `cmd/migrate` and
`cmd/fga` already establish a multi-binary convention, and `cmd/api
mint-token` establishes build-tag gating for a privileged subcommand. So the
plumbing is ready and no refactor is owed.

Undesigned, and the decisions a robust CLI still needs:

1. A CLI framework, chosen once and applied consistently — today's tools are
   hand-rolled `os.Args` switches, fine for three verbs and not for help,
   nested subcommands, flags or completion. Retrofit `migrate` and `fga`.
2. How a CLI caller acquires a principal. The design's grain says: through
   the auth port, running `InTenantTx` + `authz.Check` exactly like a
   handler — never a break-glass path that bypasses `authz` and tenant
   isolation (ADR-108, the deliberately narrow `db.Pool()` escape hatch).
3. A per-invocation correlation id to stand in for the one `httpx` generates,
   so a CLI-initiated DENY audits with a real id (HANDOFF §8 already requires
   audit assertions to scope to a unique per-run id).
4. Which subcommands are safe to compile into a deployment binary.

Bulk import via CLI additionally runs into B5's unresolved conflict with
gapless numbering; a single-action operator CLI does not.

**Standing decision.** Every design decision from here takes the CLI into
account as a possible caller: anything that could live in an `internal/`
package rather than a handler does live there, so both transports call the
same code.

---

## 4. Thin spots

| #   | Item                                                                              | Status |
| --- | --------------------------------------------------------------------------------- | ------ |
| C1  | No PostgreSQL<->OpenFGA reconciler; 7.4 names the incident, designs no repair     | todo   |
| C2  | `may_review_compliance` returns nothing defined — the central claim has no output | todo   |
| C3  | No scale figures to justify ADR-031 against                                       | todo   |
| C4  | No observability beyond audit; 10's targets have no means of validation           | todo   |
| C5  | No rate limiting or abuse model                                                   | todo   |

---

## 5. Order of work

Agreed sequence:

```txt
1  B0   design the Consumption context          DONE
2  A1   outbox fence                            DONE
3  A3   the two absent verbs                    DONE
4  F1   invoice numbering                       DONE  ADR-103
5  B1   read model mechanism                    DONE  ADR-104
6  B2   minimal API contract                    DONE  ADR-105, A3
7  B3   auth port                               DONE  ADR-106
8  ---  build the slice                                              <- in progress
     8.1  migrations + FGA model + assertions   DONE  56a671e
     8.2  the auth port, dev implementation      DONE
     8.2.5 role assignment, and step 2         DONE  8f4d4f8, 3570e85
     8.2.5b the platform plane                  DONE  4681323, 0c1e1eb
     8.3  entry to bill, the A3.7 endpoints                          <- in progress
          foundations: idempotency, outbox, paging  DONE
          configuration: bundles, plans, types      DONE  234d568
          platform: tenant + owner provisioning     DONE  0c1e1eb
          people, units, the cycle check            DONE  d3be0e8, e16aaf1
          admission (ADR-107, A3.6 widened)         DONE
          consumption                                         <- next
          finance
     8.4  the Flutter app, once GET /me is real
9  A2 -> B4 -> B5 -> B6 -> B7 -> C*
```

Deferred until the slice forces them: gapless numbering mechanism (F1),
instalments, multi-currency, accounting export.

---

## 6. Change log

### R1 · 2026-08-29 · Consumption context added

| File                                                   | Change                                                                              |
| ------------------------------------------------------ | ----------------------------------------------------------------------------------- |
| `05_building_block_view/05.10_consumption.md`          | New. The tenth context                                                              |
| `09_architecture_decisions.md`                         | ADR-096..100; new `R` reference class for review-sourced decisions                  |
| `05.0_overview.md`                                     | Nine -> ten; aggregate roots; two context relationships; store split                |
| `05.8_finance.md`                                      | `CONSUMPTION` added to `Charge.source_type`                                         |
| `A1_openfga_schema.md`                                 | `consumption_type` type; absences listed under deliberately-absent; four assertions |
| `A2_data_model.md`                                     | New A2.9 with five tables; two invariants; one index; sections renumbered           |
| `12_glossary.md`                                       | Four terms, two distinctions                                                        |
| `00_README.md`, `03_context_and_scope.md`, `README.md` | Counts, file layout, scope list                                                     |

Not yet done for R1: `06_runtime_view.md` has no consumption scenario, and
`10_quality_requirements.md` has no consumption quality scenario. Both are
worth adding before the slice is built.

### R2 · 2026-08-29 · A1 outbox fence

| File                           | Change                                                                                  |
| ------------------------------ | --------------------------------------------------------------------------------------- |
| `09_architecture_decisions.md` | ADR-101, stated generally: async projection is fenced against synchronous removal       |
| `08_crosscutting_concepts.md`  | Fence in 8.3; grant and revocation paths take the advisory lock; revocation voids       |
| `06_runtime_view.md`           | 6.4 gains "Ordering alone is not enough"; ADR-101 added to the 6.10 coverage table      |
| `A2_data_model.md`             | Fence columns, `voided_at`, two `CHECK`s, fence index; `outbox_pending` excludes voided |

Decided in the round, not carried by the ADR text alone:

- The producer names the fact; the dispatcher renders the tuple. Keeps FGA
  model versions out of stored payloads.
- `authorization_outbox` projects authorization facts only. The event bus of
  05.0.8 is a separate, unmodelled thing — logged as B6 rather than papered over.
- Voided rows are retained. A fenced row is evidence a race reached production.

### R3 · 2026-08-29 · A3 the reaching verbs

| File                             | Change                                                                                                                                                        |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `09_architecture_decisions.md`   | ADR-102: three verbs resolve, two are recorded and resolve to nothing                                                                                         |
| `A1_openfga_schema.md`           | `tenant.may_administer` -> `administered_by`; both verbs defined on `affiliation`; `natl-sec` holds all five in A1.6; invariant 2 asserted against nine paths |
| `05.1_organization.md`           | Which verbs resolve, and why; 5.1.9 fragment shows all five; open item for the assumption                                                                     |
| `08_crosscutting_concepts.md`    | 8.12 test 2 restated in its strong form                                                                                                                       |
| `10_quality_requirements.md`     | Q1 now grants `may_read_member_data` rather than withholding it                                                                                               |
| `11_risks_and_technical_debt.md` | Temptations 11 and 12; known gap for upward member data                                                                                                       |

Unvalidated assumption, carried deliberately: **no national or world body in
the movement holds member rosters.** Joe did not know either way. ADR-102 takes
the safest reading. If rosters do flow upward, `may_read_member_data` needs a
narrow real path and ADR-102 is revisited. Recorded in 05.1.11 and 11.2 so it
surfaces when a real association is asked, rather than living only here.

### R4 · 2026-08-29 · Slice design gate

Decisions taken before any code, in three rounds.

| Decision                                                   | Where         |
| ---------------------------------------------------------- | ------------- |
| Slice is backend + one permission-gated Flutter app        | R1 log above  |
| Greenfield `backend/`; `prototype/` stays reference only   | —             |
| Invoice numbers from a transactional counter, per year     | ADR-103, A2   |
| Lists authorize the scope, not the row                     | ADR-104, 8.11 |
| Tenant is a path segment, compared against the token       | ADR-105, A3.1 |
| Authentication is a port; dev implementation for the slice | ADR-106, A3.2 |
| Both principal kinds modelled now                          | ADR-106       |
| SQL migrations + FGA model applied as a build step, CI'd   | to build      |

`08_crosscutting_concepts.md` gained 8.11 Reading and listing; the old 8.11
Testing is now 8.12, and the three references to it were repointed.

Next: the code. Backend first — migrations, FGA model deployment, the auth
port, then entry-to-bill. The Flutter app follows once `/me` returns real
permissions.

### R5 · 2026-08-29 · The schema and the FGA model, applied

Step 8.1. First code in the repository. Commit `56a671e`.

| File                               | Change                                                        |
| ---------------------------------- | ------------------------------------------------------------- |
| `backend/migrations/0001_init.sql` | The whole of A2, 54 tables, topologically ordered; RLS on 34  |
| `backend/cmd/migrate/`             | goose embedded as a library; refuses `down`, `reset`, `redo`  |
| `backend/fga/model.fga`            | A1.2, six continuation lines joined so that it parses         |
| `backend/fga/assertions.yaml`      | A1.6 fixture + A1.7, executable; 31 assertions, all passing   |
| `backend/cmd/fga/`                 | applies the model; `test` runs the suite in a throwaway store |
| `backend/internal/config/`         | environment configuration                                     |
| `backend/README.md`                | every deviation, and the role model RLS requires              |

Decided in the round, not carried by the design text:

- **`FORCE ROW LEVEL SECURITY` and a non-superuser role.** A2.1 gives only
  `ENABLE`. The table owner bypasses a policy without `FORCE`, and a superuser
  bypasses it regardless — and `pgadmin` is this cluster's only role. Without
  both changes, tenant isolation would have been decoration in every
  environment. Migration 0001 creates `ymca_app`; `ymca_api` is created out of
  band so no password enters a migration file.
- **`location` is created.** A2.2 and A2.5 reference it; A2 defines it
  nowhere. The shape is the minimum those two foreign keys need and is not
  part of the design record.
- **Forward-only was taken literally.** HANDOFF §8 asked for reverse
  migrations; 7.4 says forward-only. 7.4 governs. `cmd/migrate` refuses the
  reversing verbs rather than letting goose half-reverse a schema.

### A4 — three defects in A1, found by making the assertions run

| #   | Defect                                              | Status |
| --- | --------------------------------------------------- | ------ |
| A4a | A1.2 does not parse as OpenFGA DSL                  | open   |
| A4b | A1.6's tuples cannot satisfy A1.7's first assertion | open   |
| A4c | `entitlement_bundle.via_plan` is unreachable        | open   |

**A4a.** The DSL requires each `define` on one line. A1.2 wraps four of them
across six continuation lines for readability — `organizational_unit.admin`,
`.member`, `.member_read` and `resource.manager`. `fga/model.fga` joins them
and is therefore not byte-identical to A1.2, which A1.8's "copy verbatim"
discipline assumed it would be. Either A1.2 is reformatted or the discipline
is restated. Leaving it is the worst of the three.

**A4b.** A1.6 lists `subscription:sub-77 grants_bundle
entitlement_bundle:pool-access`. Nothing in the model reads
`subscription.grants_bundle`. What resolves is
`entitlement_bundle.via_subscription`, which A1.6 never writes, and
`membership:m-123 holder person:alice`, which it also never writes. Removing
those two from the fixture fails `alice may_use` and `alice may_record` —
demonstrated, not argued. A1.6 should gain both tuples.

**A4c.** `define beneficiary: [person] or beneficiary from via_subscription`.
`via_plan` is declared and never read, so a bundle conferred by a membership
plan entitles nobody. `membership_plan.entitlement_bundle_id` in A2.4 implies
the other reading. **This one has teeth for 8.3:** admitting a member will
need a synthetic `subscription` object whose only purpose is to carry the
plan's own bundle, unless A1.2 changes. Decide before writing admission, not
during.

A fourth finding, not a defect but a trap: **A1.7 writes subjects bare**, and
the same bare name is two different subjects depending on the relation.
`resource.may_use` reaches a `person` through an entitlement bundle;
`organizational_unit.member_read` takes a `principal` directly. Every
assertion in `assertions.yaml` names the type, marked `TYPED`.

Three assertions are marked `ADDED` — positive checks whose only purpose is to
prove a neighbouring DENY is not vacuous. This is the same weakness §2 found
in 8.12 test 2, and it recurs whenever a negative test meets a thin fixture.

### R6 · 2026-08-29 · The record and the code, reconciled

Joe's instruction: nothing the code does may go unsaid in the record. Every
divergence R5 left open is closed, in the record where the record was wrong
and in the code where the code was.

| File                             | Change                                                                                          |
| -------------------------------- | ----------------------------------------------------------------------------------------------- |
| `A1_openfga_schema.md`           | A1.1 gains ONE LINE PER DEFINE; A1.2 parses and wires `via_plan`; A1.5, A1.6, A1.7 rewritten    |
| `A1_openfga_schema.md`           | A1.8 gains rule 7 — A1.2 and A1.7 are machine-checked against `backend/fga/`                    |
| `A2_data_model.md`               | A2.1 rewritten: `FORCE`, the role split, the twelve tables, the nullable three; A2.2 `location` |
| `09_architecture_decisions.md`   | ADR-107 entitlement routes; ADR-108 isolation rests on a role                                   |
| `11_risks_and_technical_debt.md` | Three known gaps: cycle check, twelve tables, global rows                                       |
| `12_glossary.md`                 | Two distinctions: _subscription_ ours vs theirs; plan- vs subscription-conferred                |
| `backend/fga/sync_test.go`       | The guard rule 7 names                                                                          |

**ADR-107 was decided by evidence, not by taste.** The question was whether a
membership plan confers entitlement directly or whether everything routes
through a subscription. Joe answered with three association membership pages
rather than picking an option:

```txt
Poona        one category; the fee "includes the use of the Swimming pool,
             Health Club and other indoor Sports activities"
Bombay       five categories; privileges "as laid down in the Bye-laws"
Ernakulam    Associate and Life confer by category; Hostel Membership confers
             "access to the indoor and outdoor games with monthly subscription"
```

Ernakulam settles it: one person, one tenant, both routes at once. So both are
first-class. Subscription-only would have made Poona invent a subscription per
member for a concept it does not have, and turned a Bombay bye-law amendment
into a tuple rewrite per member.

Three relations came out in the process — `membership.plan`,
`membership_plan.grants_bundle`, `subscription.grants_bundle`. Each expressed
a fact in the direction OpenFGA cannot travel, and nothing read any of them.
`via_plan` was the fourth and the only one anybody noticed, because it was the
only one an assertion needed.

**The lesson worth keeping.** A1.8 had six rules of change discipline, all
obeyed, and three defects still shipped through a full review — one of which
meant the model did not parse at all. The reviewer was reading DSL nobody had
ever fed to a parser. Rule 7 exists because prose cannot enforce prose, and
the guard was tested against real drift rather than observed to pass.

**Not done, and now visible rather than implied:** the `authorization_edge`
cycle check A2.2 specifies is unwritten, so ADR-016's guarantee does not hold
yet. Logged in 11.2.

---

### R7 · 2026-08-31 · The auth port, and the first authorized request

Step 8.2. `GET /api/v1/t/{tenant}/me` now runs the real thing end to end: a
token becomes a principal, the path tenant is compared against the token's,
6.1's three steps run against live PostgreSQL and live OpenFGA, and a DENY
lands in `audit_event` inside the same transaction that produced it.

| File                             | Change                                                                                 |
| -------------------------------- | -------------------------------------------------------------------------------------- |
| `internal/auth/`                 | The ADR-106 port, and a provider registry no application code imports a provider into  |
| `internal/auth/dev/`             | The development provider and `Mint` — every file `//go:build dev`                      |
| `internal/db/`                   | The pool, and `InTenantTx`: the only way a tenant-scoped query reaches a row           |
| `internal/authz/`                | 6.1's three steps, the pinned OpenFGA client, and step 3's four named limbs            |
| `internal/audit/`                | 8.5's DENY record                                                                      |
| `internal/httpx/`                | Recover, request id, logging, Authenticate, TenantMatch; A3.4's shape                  |
| `cmd/api/`                       | Wiring, `GET /me`, and `mint-token` behind the same build tag                          |
| `08_crosscutting_concepts.md`    | 8.2 why per-transaction and why it raises; 8.5 the authentication-failure carve-out    |
| `09_architecture_decisions.md`   | ADR-106: three principal kinds, the two gates, the dev secret, `mint-token`            |
| `A3_api_contract.md`             | A3.2 three kinds; A3.4 the code vocabulary and `X-Request-Id`; A3.9 what `/me` reports |
| `11_risks_and_technical_debt.md` | Two gaps: role assignment unmodelled, restriction kind unmapped                        |

**Three design defects were found by trying to build against the record.**

`ADR-106` and `A3.2` both said two principal kinds. `05.2.3` and the applied
`principal` table say three. The schema had already shipped, so the record was
the defect and was corrected — not the other way round.

`/me` had no defined permission set. It is now exactly four tenant-scoped
checks (`A3.9`), and the rule that made the choice is the load-bearing part:
an object-scoped permission is reported by the endpoint that returns the
object, never by `/me`. A `/me` that enumerated them would be precisely the
reverse index ADR-104 and 8.11 exist to prevent.

`6.1 step 3 is only partly buildable.` A2 defines no role assignment table at
all, though the FGA model declares both `role_definition` and
`role_assignment`. So the term window and the clearance check have nothing to
query, and `restriction.kind` maps to no permission. Rather than fake them or
quietly omit them, each is a named function in `validity.go` that returns
valid and says why in a comment, and both are logged in 11.2. **An expired
term still authorizes.** That is now written down in three places instead of
being true and unmentioned.

**One implementation defect, caught only by the integration tests.**
`jsonb_build_object` takes `"any"`, so PostgreSQL could not infer the type of
the two placeholders building `audit_event.context` and rejected the INSERT
outright (`42P18`). Every DENY path failed — which the unit tests could not
see, because a fake `pgx.Tx` accepts any SQL. The fix is two `::text` casts.
The lesson is the one R6 already drew: a check that never touches the real
thing proves nothing about it.

**A second defect, in the test rather than the code.** The DENY assertion
counted `audit_event` rows for the action and expected exactly one. But
`audit_event` is append-only by privilege, so it passed on a fresh database
and failed on every re-run — found by running the suite a second time, which
is the only thing that finds it. It now scopes the assertion to a request id
unique to the run, and checks that `context->>'reason'` says `graph`: a row
that merely exists proves less than one naming which check produced it.

Both defects are the same shape. A test that never touches the real thing
proves nothing about it, and a test run only once proves nothing about the
second run.

**Deliberate, and worth naming.** `TenantMatch` audits a mismatch into the
tenant the _path_ named, not the one the token named — the row belongs with
the tenant whose door was tried. `Authenticate` wraps the mux rather than each
route, so an unauthenticated request to an unknown path is 401 before it is
404; that is the non-disclosure rule of 8.8, not an accident of ordering.

---

### R8 · 2026-09-05 · Role assignment, resolved before the graph

Step 8.2.5, inserted ahead of 8.3 because 8.3 hands people roles. Joe chose
the full model over deferring or half-wiring it.

**The finding that shaped it.** 6.1 step 3 could never have been written as
specified. `Check` returns a boolean, not a path, so a validity step running
after the graph cannot tell which route produced an ALLOW. For a principal
holding both a lapsed role and a direct grant, "does this principal hold an
expired assignment?" denies wrongly and "is any assignment effective?" allows
wrongly — and there is no third question. That is why 8.2's `checkTermWindow`
and `checkClearance` shipped as named no-ops: **the defect was in the shape of
the check, not in the code.**

Three more, found the same way — by trying to build against the record:

- `role_assignment` and `office.confers` were declared in A1.2 and read by
  nothing. A1.6 even wrote three tuples for "Alice is Branch Secretary" that
  resolved to nothing. Same defect class as `via_plan` in R6, still present
  after that round closed.
- **`membership.approve` did not exist.** 05.3, 05.6.7 and 6.2 all check it;
  A1.2 declared no such relation. 8.3's first endpoint had no permission to
  check.
- `restriction_kind_permission` turned out smaller than the gap suggested.
  Two of 05.9.4's four kinds act on the role path, not on a permission, so
  they need no mapping rows — they suppress assignments in step 2.

| File                           | Change                                                                             |
| ------------------------------ | ---------------------------------------------------------------------------------- |
| `09_architecture_decisions.md` | ADR-109 resolve-per-check; ADR-110 the grantable set; ADR-101 narrowed             |
| `06_runtime_view.md`, `08.1`   | 6.1 becomes four steps: TENANT, ROLE, GRAPH, VALIDITY                              |
| `A1_openfga_schema.md`         | Eleven relations gain `role_assignment#holder`; A1.8 rules 8, 9, 10                |
| `A2_data_model.md`             | A2.7 six tables; A2.8 restriction mapping; invariants and indexes                  |
| `A3_api_contract.md`           | A3.9: `/me` reports five, not four                                                 |
| `05.6`, `11.2`                 | The office arrow made real; two gaps closed, six honest ones opened                |
| `migrations/0002_roles.sql`    | The A2.7 tables, two constraints A2.11 claims, RLS on the two carrying `tenant_id` |
| `internal/authz/roles.go`      | Step 2 — one query, everything temporal in its WHERE clause                        |
| `internal/authz/validity.go`   | Step 4 — smaller, and `checkRestriction` is real                                   |

**ADR-110's grantable set was decided by what a permission names.** A relation
is role-conferrable if it names a **job**. `admin` is the sharp exclusion: it
propagates through the whole DAG, so a role conferring it at the tenant
reaches every unit below — ADR-078's escalation route arrived at without
anyone deciding to allow it. Entitlement is excluded for a different reason: a
person reaches a facility because a membership covers them, and a role
granting `may_use` would be a second, invisible route that no membership
records and no invoice reflects.

**One decision changed while implementing, and A2 changed with it.** A2.11
called for a trigger keeping `role_permission` inside the grantable set. A
foreign key to a seeded `grantable_permission` table does it declaratively and
cannot be skipped by a bulk load. The cost is that the set is now stated
twice — A1.2 and migration 0002 — so `TestGrantableSetMatchesMigration` reads
both artifacts on a checkout and fails in either direction. The dangerous
direction is silent: a permission seeded but not modelled becomes grantable in
PostgreSQL and unresolvable in OpenFGA, so a role confers something that never
works and nothing raises.

**Everything was drift-tested**, which is now a standing rule rather than a
habit. Removing the term-window clause fails exactly the expired and
not-yet-begun cases; removing the clearance clause fails exactly that case;
expiring `/me`'s assignment drops the permission from the response; a
permission in one of the two grantable lists but not the other fails with a
message naming which side is missing it.

**The load-bearing test is the negative one.** `no role tuple was ever stored`
runs the same check _without_ contextual tuples and requires a DENY. If any
role tuple had reached the store, everything above would still pass while the
expiry guarantee rested on a sweeper nobody has written.

---

### R9 · 2026-09-05 · The 8.3 foundations, and the chain the slice could not run without

Six commits. Three were foundations nothing could be built on top of, and one
was a finding that changed what 8.3 is.

**The finding.** A3.7 had no endpoint for an entitlement bundle, a membership
plan, or the link between them — while `membership.plan_id` is `NOT NULL`,
ADR-107's `covered_member` tuple needs a plan to point at, and
`consumption_type.may_record` resolves through `entitled` from a bundle. A
member admitted through the API would have held no entitlements and could not
have recorded dinner. **The hostel case — the reason the slice exists — could
not have worked**, and nothing in the record said so.

That is the fourth consecutive round where building against the record found
the record incomplete: R7 (principal kinds), R8 (step 3 unbuildable,
`membership.approve` absent), the idempotency store, and now the entitlement
chain. Each was invisible to review and obvious within minutes of writing code.

| Ref | Commit    | What                                                                    |
| --- | --------- | ----------------------------------------------------------------------- |
| —   | `d1f86df` | `idempotency_key` — A3.6 required a store A2 never defined              |
| —   | `19c63c5` | The outbox dispatcher and ADR-101's two locks                           |
| —   | `a9cab71` | Keyset pagination, and `400 invalid_request` which A3.4 lacked entirely |
| —   | `234d568` | The entitlement chain: bundles, entitles, plans, consumption types      |

**Three record gaps closed as they were found.** A3.6 required idempotency
storage A2 never defined. A3.4 listed no status for a malformed request at
all. A3.7 could not create the entitlement chain. All three were written into
the record in the same commit as the code, per the divergence rule.

**Two decisions Joe made, both narrowing scope rather than widening it.**

Configuration got minimal create+list endpoints rather than a CLI or seed
fixtures. B7's argument was real — configuration is operator work — but taking
it would have pulled the CLI framework decision, the operator principal and
invocation audit forward into 8.3.

The membership number is caller-supplied, with `UNIQUE (tenant_id, number)` as
the entire mechanism. Deliberately **not** ADR-103's counter: gapless
allocation exists to satisfy a tax authority, and a membership register has no
such rule, so that machinery would buy contention for nothing. It also leaves
each association's numbering convention intact — the ADR-107 lesson, applied
again without needing a second round of evidence.

**What the tests now prove that nothing proved before.**

```txt
an expired term DENIES, no sweeper run          8.2.5
a grant in flight cannot outlive a revocation   ADR-101's actual subject
a rolled-back attempt leaves nothing to replay  A3.6
a member on the plan MAY record                 the hostel case
a person on no plan may NOT                     the negative twin
```

**Drift-testing became a standing rule this session**, at Joe's instruction,
and immediately earned it. Removing the advisory lock reproduced the exact
historical failure ADR-101 was written for. Reversing ADR-107's `via_plan`
direction — the defect that shipped through a full review once — is now
_rejected_ by OpenFGA as a type violation rather than resolving to nothing,
which is ADR-110's principle paying off somewhere it was not designed for.

**Three defects were mine, and all three were found by running rather than
reading.** A drift edit that silently failed to apply and reported a false
pass. A test cleanup that deleted outbox rows `WHERE true`, which would have
clobbered another package's rows since `go test` runs packages in parallel —
and then, once scoped by `payload->>'tenant_id'`, silently missed two of five
events whose payloads carry no tenant id, so a failed run poisoned the next
one. And a helper that evaluated `errors.Is(err, err)`, which is always true:
logic-shaped code doing nothing.

---

### R10 · 2026-09-05 · The platform plane, and a tenant that is not born inert

Step 8.3.5, inserted ahead of the rest of 8.3 in the shape 8.2.5 had. Three
commits. Joe settled five decisions; four became ADRs.

**The findings, all confirmed by running rather than reading.** `POST
/platform/tenants` was unbuildable in four separate places at once — the dev
authenticator refused a tenant-less token, `authz.Check` raises
`invalid input syntax for type uuid: ""` with no tenant, `audit_event` refuses
the null-tenant row A2 promised it accepted, and nothing named the platform
singleton (though the assertion fixture already used `platform:main`). A fifth:
ADR-104 spells the members list as `SELECT ... WHERE unit_id = $1` and no table
carried the column.

Beneath them the one that mattered: **nothing gave a created tenant an owner.**
Every write past provisioning authorizes through `admin`, and `tenant.admin` is
`[principal] or owner`. A tenant created without one returns 201, exists, and
is administrable by nobody. The same silent shape as ADR-107's member with no
entitlements — the fifth consecutive round where building against the record
found the record incomplete.

| Ref | Commit    | What                                                            |
| --- | --------- | --------------------------------------------------------------- |
| —   | `4681323` | The record: ADR-111 to ADR-114, A1/A2/A3, migration 0004        |
| —   | `0c1e1eb` | The platform plane in code, and the bootstrap                   |
| —   | `e8c99a4` | Reopening the cycle-check gap the record had closed prematurely |

**Two decisions worth keeping.** ADR-112 isolates platform audit by privilege
rather than by policy: a second policy on `audit_event` would have rested on
`current_setting('app.platform')`, and `app.*` is a placeholder GUC any role
may set, so a tenant-path bug setting it would read every platform row. ADR-108
decided this class once already. And ADR-111 makes the tenant plane the ZERO
value of the plane field — `TenantID == ""` meaning platform would have made
the zero-value `Principal` a platform principal, which is exactly the footgun
ADR-106's tag polarity exists to avoid.

`audit_event.tenant_id` became NOT NULL with **no foreign key**, which looks
like an omission and is not. The ADR-105 mismatch DENY records the tenant the
caller _named_, and someone probing for tenants they cannot reach names one
that does not exist; an FK would refuse precisely the row that evidences the
probe, and `TenantMatch` only logs a failed audit write. Verified by writing
such a row before deciding.

**Three things this round got wrong before it got them right**, and they are
the useful part.

_The drift test passed when it should have failed._ Removing `PlatformOnly`
from the route left the integration test green, because `CheckPlatform`'s step
1 independently refuses the plane and returns the same 403. Defence in depth
working — and it meant an end-to-end assertion could not tell whether the gate
was wired at all. The audit trail that would distinguish them is unreadable to
the application by ADR-112's own design. Direct middleware tests now fail when
either gate stops gating.

_The integration suite needs `-p 1`._ The dispatcher drains the whole table by
design, so two packages driving dispatchers against one cluster steal each
other's rows. Latent before this round — three runs passed — and the new
test's extra dispatches widened the window. Intermittent, which is worse than
consistent.

_The record closed a gap the code had not._ 11.2 removed the cycle-check row
on the condition that `POST /units` landed; it had not, and `e8c99a4` puts it
back. Closing a gap when the decision is taken rather than when the code lands
is the failure 11.2 exists to catch.

**A guard's failure message can send you the wrong way.**
`TestGrantableSetMatchesMigration` hardcoded migration 0002 because 0002 was
the only migration that seeded anything. The moment 0004 seeded
`may_register_person` it failed claiming the permission was "not seeded" —
false — and named 0002 as the file to fix, which under forward-only migrations
has already been applied and must never be edited. It now reads every
migration.

**Delegation cost more than it saved, for a second reason this time.** The
record round went to a Sonnet subagent and was good work; its own arithmetic
about the result was wrong in two places, which foreground verification caught.
The implementation agent then died on a rate limit, and resuming it via message
silently promoted it to Opus — a resumed agent does not carry its spawn's model
override. Re-spawning explicitly did not stick either. The implementation was
done in the foreground instead, which on this round's evidence was the right
place for it: three of the four findings above came from verifying rather than
writing.

---

### R11 · 2026-09-08 · People, units, and a DAG that is now actually a DAG

The rest of 8.3's identity and organization surface. Two commits. Joe settled
two decisions; one became an ADR.

| Ref | Commit    | What                                                    |
| --- | --------- | ------------------------------------------------------- |
| —   | `d3be0e8` | The record: ADR-115, A2.2, A3.7, 05.1.3, migration 0005 |
| —   | `e16aaf1` | Four endpoints, and the first code that inserts an edge |

**The finding that shaped the round.** `POST /t/{t}/units` is the first
endpoint that can insert an `authorization_edge`, and R10 specified that it
run A2.2's depth-12 recursive CTE before committing. Building it showed that
**a cycle is unreachable from that endpoint by construction** — nothing
points at a unit that did not exist a moment ago — so of ADR-016's three
invariants, only the depth bound could ever be violated there. A check living
in that handler could never have been observed to fail.

That is why ADR-115 moves all three into a trigger. It is also the second
argument, independent of the first: 05.1.3 words the invariant as "enforced
on every write", and this table's writers will include a second-auth-parent
endpoint, bulk import, a repair script and every integration test.

**Two decisions.** Enforcement in the database rather than the domain package,
above. And the slice stays inside A3.7: one authorization parent per unit
through the API, with ADR-016's multiple-parents case — a pool under both a
chapter and a department — left unreachable and logged in 11.2, rather than
adding an endpoint the contract does not have.

**Three things this round got wrong before it got them right.**

_The test cleanup leaked, in the exact way it had been warned about._ The
`AuthorizationEdgeCreated` payload carries no `tenant_id`, so scoping the
outbox delete by `payload->>'tenant_id'` left 61 pending rows behind. The
configuration test's own cleanup comment records making this mistake in
session 5 and fixing it by scoping to `aggregate_id`. Reading that comment
did not prevent repeating it; running the suite twice did.

_Those rows then failed a test that was not wrong, with a message that was._
`TestConfigurationChain_EndToEnd` counted pending rows over the WHOLE outbox,
so two strays from another package failed it at 7-want-5 while naming "the
bundle, type and plan tenant edges" as the suspects. A table-wide count over a
queue the dispatcher drains globally is an assertion about other packages'
leftovers. Now scoped to its own aggregates. This is R10's
`TestGrantableSetMatchesMigration` lesson — a guard's failure message sending
the reader the wrong way — in a second place, and it will keep happening
wherever a test asserts over shared global state.

_Two authorization gates had no negative case at all._ Every case in the new
end-to-end test used the tenant admin, who is allowed, so deleting the
`member` and `member_read` checks from the two GET handlers left the file
green. That is session 5's `PlatformOnly` finding one step from happening
again, and it was found by removing each gate on purpose rather than by
review. Each of the four now fails a test when removed.

**The drift-testing paid twice.** Disabling the trigger fails four tests.
Renaming one constraint fails exactly one — and leaves the other three green,
because the cycle is still refused, just under a name Go does not know. That
second result is the valuable one: the constraint name is a contract between
SQL and Go that a rename would break silently, since it compiles and vets
clean and turns every refusal into a 500.

**Deleted before commit:** `organization.ListUnits`. It was written for
symmetry, nothing routes it, and code read by nothing is the `via_plan`
defect class this project has now paid for three times.

### R12 · 2026-09-12 · Admission, and the guard that passed when it should have failed

ADR-107's `covered_member` tuple, published as a fact through the outbox, and
the endpoint that writes it. No new ADR: every decision applied an existing
one, as the configuration round did.

| Ref | What                                                                     |
| --- | ------------------------------------------------------------------------ |
| —   | The record: `A3.4` two 409 codes, `A3.6` scope, `A3.7` admission, `11.2` |
| —   | `internal/membership/admission.go`, `cmd/api/membership_handlers.go`     |

**Three decisions Joe settled**, each against an alternative that read as
tidier:

```txt
two 409 codes, not one `conflict`   A3.4 writes the message for a developer,
                                    so a client must not parse it; a taken
                                    number and an existing membership need
                                    different client behaviour
A3.6 widened to admission           it creates neither money nor a
                                    consumption record, so the scope
                                    sentence changed rather than the
                                    endpoint quietly requiring more
the term is passed through          valid_from/valid_until unvalidated
                                    against plan.duration, for the same
                                    reason the number is supplied
```

**The finding that shaped the round.** Eight guards were drift-tested and
seven failed exactly the case they exist for. The eighth — removing the
`Idempotency-Key` requirement — left **the whole suite green**. The zero-valued
key that resulted reached PostgreSQL as `''::uuid`, raised `22P02`, and came
back as the same 400 the test was asserting. The guard had never been
observed to fail and was indistinguishable from one that could not, in the
smallest possible way: one error path shadowed by another with the same status
code.

Two things came out of it. `runIdempotent` refuses a zero key with a logged
500, because a handler that ignored `httpx.IdempotencyKey`'s error was
blaming the client for the server's mistake. And the test asserts the message
names the header — a client must not parse a message, but a drift test may,
and here the code alone cannot pin the refusal to its own reason.

**The same shape, twice more.** Renaming one of the two collision constraints
fails exactly one of the two 409s and leaves the other green: the constraint
name is a contract between the schema and Go, which is R11's finding in a
second place. And the first version of this test's cleanup read `membership`
through `db.Pool()` — a policied table, so A2.1's fail-closed error raised, the
error was ignored, and three pending outbox rows were left behind reporting
nothing. The cleanup now reads inside `InTenantTx`, collects ids by query
rather than from variables, renders the tuples to delete from the outbox's own
fence columns, and **checks itself**, failing the test if anything survives.

**One guard the record asked for and nothing would have enforced.** 05.3.2
calls `CLOSED_TO_NEW` "the common real state — existing life members keep a
plan that no longer accepts applicants". Admission reads the plan's status in
the same statement that inserts the membership, so a plan closing between the
two cannot be raced. Unlike R11's cycle check, this one is observable: no
endpoint can close a plan (B8), so the test seeds a closed plan straight at
the table, exactly as the DAG tests write edges.

**Opened in `11.2`**, five rows, each a thing admission deliberately does not
do rather than something nobody noticed: nothing revokes a membership's three
tuples, because no endpoint changes a membership's state while `08` lists that
operation among the sixteen requiring synchronous revocation; no term is
derived, so a `LIFETIME` plan accepts an end date; approval authority is
tenant-scoped only, because A1.2 declares no admission relation on
`organizational_unit`; 05.3.4's application workflow is bypassed entirely, so
`application_id` is always NULL; and dependents are unreachable, so `covered`
resolves to the holder alone.

**One cross-cutting fix.** A malformed uuid in a body used to be a 500 on
every endpoint — `22P02` fell through to the default. It is now
`invalid_request` in `writeDomainError`, in one place, for all of them.

### R13 · 2026-10-22/23 · Consumption to cash, the app, and a record that contradicted itself

The slice is finished. What the round found, by what found it — and, as in every
round since R5, nearly all of it came from running something.

**Found by building.** 05.10 and 05.8 left the endpoints the slice needs open in
ways only an attempt exposes: A3.7 had no way to define an option or an
obligation, so dinner could never obligate and an egg could never be priced;
nothing stored a price, so Finance could not bill a unit; a correction that
withdraws a record had no representation (`voided`); exactly-once close was
specified as a problem with no mechanism (an exclusion constraint, ADR-116); a
membership could be admitted and never changed, which left the largest open item
in `11.2`. Twelve error codes, two tables, one column and three ADRs followed. A3.10, A3.11,
A3.12 and A3.13 say what each endpoint does and what it deliberately does not.

**Found by drift-testing**, which caught the same mistake five times in
different clothes. The assertion for `admin from tenant` named a type the admin
already reached through the DAG, so the guard could not fail. The late-correction
window, `Void` in `Suspend`, and the term check each had a sibling that answered
with the same status — removing either changed nothing, which is how two of a
thing looks from outside. A test that held a transaction hung the suite for ten
minutes when it failed. A cleanup swallowed a batch-delete error and leaked a
tenant's administrator into the next run. Each rule in HANDOFF §3 under "what
session 8 added" is one of these.

**Found by the other language.** `TestFlutterClientAgreesWithTheServer` boots
the real routes, seeds a draft, a part-paid and a paid invoice, and runs the Dart
client against them. On its first run it found that the front desk could not read
the plans it admits onto, and that a front desk holding `may_approve_membership`
could not register the person it was admitting — so `/me` reports
`may_register_person` and `GET /membership-plans` is gated on the approver. A
fake that agrees with the client by construction cannot find either.

**Found by a cold start and a browser.** Provisioning a real server from nothing
and driving the Flutter web build in headless Chromium found that nothing could
make the first platform operator (`bootstrap-operator`, deliberately not behind
the dev tag) and that a browser may not call the API at all (CORS, closed by
default, preflight answered before authentication, `Idempotency-Key` allowed —
without which the browser strips the header A3.6 depends on, silently).

**Found by review, not by tests.** Three Haiku workers built six screens against
a fixed contract. All three made the same idempotency-key mistake — a key made
once when a screen opened and reused after the request had changed — and mounted
their tests inside `MaterialApp.home`, which hides every provider from pushed
routes. The app would have thrown on the first tap on a bill; their tests passed.
The screens were rewritten where wrong and every key is scoped to its request and
drift-tested.

**The record contradicted itself.** 05.3.10 said suspension is "domain state,
consulted after the graph answers"; 5.3.9 and 8.3 said it is synchronous and
that every removal is. The first cannot be built — the graph returns a boolean,
not a path — so ADR-118 resolves it: suspension stays in PostgreSQL, its effect
is the synchronous deletion of the coverage tuple, and a term that ended is a
decision-time check. This is the first code to run 8.3's revocation path; ADR-101
had been tested and never used.

**Opened in `11.2`:** no way to give a registered person a login (the largest
remaining gap), grants that lag, a `LAPSED` nobody sets, coarse suspension
authority, no credit notes, no tenant time zone, and the consumption and finance
omissions listed in HANDOFF §7.
