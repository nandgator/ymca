# backend

The YMCA platform backend. Greenfield — `prototype/` is reference only.

Design record: `docs/system-design/`. This directory implements it. Where the
two disagree, the design document is right and the code is a defect, except
where a file here records a deviation explicitly.

---

## What exists

```txt
migrations/0001_init.sql   the full A2 schema, 54 tables, RLS on 34
migrations/0002_roles.sql  A2.7 roles, A2.8 restriction mapping, RLS on 2
migrations/0003_idempotency.sql  A3.6's store, which A2 had never defined
migrations/0004_platform.sql     platform audit, may_register_person, org_unit_id
migrations/0005_authorization_edge_invariants.sql
                           ADR-016's three invariants, as a trigger (ADR-115)
migrations/0006_consumption.sql  voided, once-only supersede, period close, rates
migrations/0007_finance.sql      invoice order, one draft per payer, party keys
migrations/embed.go        go:embed for the above
cmd/migrate/               applies migrations; forward-only
fga/model.fga              A1.2, reformatted (see below)
fga/assertions.yaml        A1.6 fixture + A1.7 assertions, executable
fga/embed.go               go:embed and the assertion parser
fga/sync_test.go           A1.8 rules 7-9, machine-checked
fga/grantable_test.go      A1.8 rule 10 — the model vs every migration
cmd/fga/                   applies the model, runs the assertions
cmd/api/                   the HTTP server, /me, configuration, platform,
                           people, units and admission
cmd/api/handlers.go        the 6.1 check, A3.4's error mapping, body decoding,
                           and A3.6's flow around one operation
cmd/api/consumption_handlers.go  A3.10 — and the one place Consumption meets
                           Finance (ADR-117)
cmd/api/finance_handlers.go      A3.11
cmd/api/harness_integration_test.go  the shared setup: production routes
                           (registerRoutes), a tenant, and a cleanup that proves it
internal/config/           environment configuration
internal/db/               the pool, and the tenant transaction (8.2)
internal/auth/             the ADR-106 port and its provider registry
internal/auth/dev/         the development provider — //go:build dev only
internal/authz/            6.1's four-step check, and the OpenFGA client
internal/authz/roles.go    step 2 — effective assignments, per check
internal/authz/platform.go the platform plane's three-step check (ADR-111)
internal/audit/            8.5's DENY record
internal/idempotency/      A3.6 — the key is stored with the work it describes
internal/outbox/           ADR-101 — the fence, and the dispatcher
internal/page/             A3.5 keyset pagination; the cursor is opaque
internal/membership/       bundles, plans, and their outbox renderers
internal/membership/admission.go ADR-107 — a member who may actually do something
internal/membership/members.go   ADR-104's unit member list
internal/identity/         people — global rows, tenant-gated (ADR-114)
internal/consumption/      types, options, obligations, records, corrections,
                           absences and the period close (05.10, A3.10)
internal/finance/          rates, invoices, issue (ADR-103), payments, and the
                           charge a closed period composes (A3.11)
internal/organization/     tenants and their first owner (ADR-113)
internal/organization/units.go   units and the DAG edges (ADR-016, ADR-115)
internal/httpx/            the middleware chain, A3.4's errors
```

## What the handoff planned and does not exist yet

```txt
compose.yaml     not needed — postgres and openfga already run under podman
Dockerfile       not needed until there is something to deploy
```

Step 8.3 is complete: configuration, organization, admission, consumption and
finance are all reachable through the API, from a cold tenant to a paid
invoice. What the design deliberately leaves out is in `11.2`.

---

## Running it

Both services are already running as podman containers. Nothing here starts
them.

```sh
podman ps                     # postgres :5432, openfga :8080 (HTTP), :8081 (gRPC)
curl -s localhost:8080/healthz
```

### Environment

```sh
export YMCA_DATABASE_URL='postgres://ymca_api:<password>@localhost:5432/ymca?sslmode=disable'
export YMCA_FGA_API_URL='http://localhost:8080'
export YMCA_FGA_STORE_ID='...'    # printed by `fga apply`
export YMCA_FGA_MODEL_ID='...'    # printed by `fga apply`
export YMCA_AUTH_PROVIDER='dev'   # no default; see The API, below
export YMCA_DEV_AUTH_SECRET='...' # >= 32 bytes; only read by the dev provider
```

`YMCA_DATABASE_URL` names two different roles depending on the command, and
the difference matters — see **Roles** below.

### Migrations

```sh
go run ./cmd/migrate up        # apply everything pending
go run ./cmd/migrate status
go run ./cmd/migrate version
```

**Forward-only** (`07_deployment_view.md` §7.4). No migration declares a Down
section, and `cmd/migrate` refuses `down`, `reset` and `redo` outright rather
than letting goose half-reverse a schema.

To reset a development database:

```sh
podman exec postgres psql -U pgadmin -d postgres \
  -c 'DROP DATABASE ymca' -c 'CREATE DATABASE ymca'
go run ./cmd/migrate up
```

The drop fails if anything holds a session on `ymca` — pgAdmin keeps one open.
Disconnect it there, or terminate the backend:

```sh
podman exec postgres psql -U pgadmin -d postgres \
  -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='ymca'"
```

### The FGA model

```sh
go run ./cmd/fga apply      # write the model to a durable store
go run ./cmd/fga test       # throwaway store, fixture, 47 assertions
```

`fga test` is the CI command. It creates its own store, applies the model,
writes the fixture and deletes the store afterwards, so a stale tuple from a
previous run can never turn a DENY assertion into a pass. Any failure exits
non-zero, as A1.8 rule 4 requires.

### The API

```sh
go run ./cmd/api                 # serve on YMCA_HTTP_ADDR
```

`GET /api/v1/t/{tenant}/me` (`A3.7`, `A3.9`) reports who the caller is and
which of seven tenant-scoped permissions they hold. Each is a full 6.1 check
against the tenant named in the path — the endpoint reports no object-scoped
permission, deliberately, because a `/me` that enumerated them would be the
reverse index `ADR-104` and `8.11` exist to prevent. Beside it are A3.7's
configuration endpoints (bundles, plans, consumption types), the
people, units and admission endpoints, and one platform-plane route.

```txt
POST /api/v1/t/{t}/people               may_register_person   ADR-114
POST /api/v1/t/{t}/units                admin on the NAMED PARENT
GET  /api/v1/t/{t}/units/{unit}         member on the unit
GET  /api/v1/t/{t}/units/{unit}/members member_read on the unit  ADR-104
POST /api/v1/t/{t}/memberships          may_approve_membership   ADR-107
                                        + Idempotency-Key, required
```

Consumption and money (`A3.10`, `A3.11`, `A3.12`):

```txt
POST /t/{t}/consumption-types/{ct}/options       manager on the type
POST /t/{t}/consumption-types/{ct}/obligations   manager on the type
POST /t/{t}/consumption-types/{ct}/records       may_record (person OR principal)
                                                 / may_record_for_other, + the
                                                 SUBJECT's own may_record
                                                 + Idempotency-Key
POST /t/{t}/consumption-records/{r}/correction   may_correct [+ may_correct_late]
POST /t/{t}/absences                             own: an ACTIVE membership
                                                 other: tenant admin
POST /t/{t}/consumption-types/{ct}/close         may_close_period + Idempotency-Key
                                                 -> composes the invoice, in the
                                                 same transaction (ADR-117)
POST /t/{t}/consumption-types/{ct}/rates         finance_manager
GET  /t/{t}/invoices                             finance_reader   (scope)
GET  /t/{t}/invoices/{i}                         viewer, as person AND principal
POST /t/{t}/invoices/{i}/issue                   finance_manager + Idempotency-Key
POST /t/{t}/invoices/{i}/payments                finance_manager + Idempotency-Key
GET  /t/{t}/me/consumption                       own records, by identity
GET  /t/{t}/me/invoices                          own invoices, drafts excluded
GET  /t/{t}/me/consumption-types                 what I may record (visibility)
GET  /t/{t}/memberships?q=                       the member directory
```

**Two things about these are easy to get wrong.** Invoices are not created by
an endpoint: a closed period composes onto the payer's open draft, so closing
a new period before issuing the last invoice appends to it. And an invoice's
authorization facts are grants, so a draft is unreadable — even to the
administrator — until the dispatcher runs.

`POST /units` writes the unit row and its `authorization_edge` in one
transaction. The edge is **not** derived from `org_parent_id` and never may
be (05.1.3 invariant 4) — the request names its authorization parent
explicitly, and that named parent is what `admin` is checked against.

Creating two levels at once needs a dispatch between them. The new unit's
`auth_parent` tuple is a grant, so it reaches OpenFGA when the dispatcher
runs; until then a request to create a child beneath it is refused. That is
`ADR-101`'s asymmetry, not a race.

### Admission

`POST /t/{t}/memberships` is the endpoint that makes a member able to do
anything. It writes the membership row and three authorization facts:

```txt
tenant:<t>                tenant          membership:<m>
person:<p>                holder          membership:<m>
membership:<m>#covered    covered_member  membership_plan:<plan>
```

The third is `ADR-107`'s, and **getting it wrong is silent**: without it the
member simply holds no entitlements, every `may_record` check denies —
correctly — and nothing reports why. The `#covered` suffix is load-bearing,
because what a plan covers is every person the membership covers, not the
membership. All three are grants, so a member cannot record until the
dispatcher has run.

It is gated on `may_approve_membership`, **not** `admin`: `ADR-110` made that
relation role-grantable so that a front desk can admit members without
holding administration of the association.

It is also the first endpoint requiring `Idempotency-Key` (`A3.6`, whose
scope was widened for it). Admission creates neither money nor a consumption
record and `UNIQUE (tenant_id, number)` already prevents a duplicate — what
the key buys is an answer. Without it, a retry after a lost 201 returns
`membership_number_taken` and the caller cannot tell its own success from
somebody else's collision.

Two collisions, two codes, both 409:

```txt
membership_number_taken  UNIQUE (tenant_id, number) — choose another
already_active_member    one_active_membership_per_tenant (ADR-043) —
                         one person, at most one live membership per tenant
```

The plan must be `OPEN`, read in the same statement that inserts the row.
`valid_from` and `valid_until` are the caller's and are not derived from the
plan's duration; `org_unit_id` may be NULL, which is an association-level
membership and appears under no unit's member list (`ADR-104`).

### The two planes

`A3.1` gives the API two planes, and `ADR-111` makes which one a credential
belongs to a property of the credential rather than of the route:

```txt
/api/v1/t/{tenant}/...   tenant plane     behind httpx.TenantMatch
/api/v1/platform/...     platform plane   behind httpx.PlatformOnly
```

A request satisfies exactly one gate, never both and never neither. A
tenant-bound credential is refused at a platform route and a platform
credential is refused at a tenant route, both before any handler runs.

**The polarity is the load-bearing part.** `auth.PlaneTenant` is the zero
value, so a `Principal` whose plane was never assigned comes out tenant-bound
rather than holding platform authority nobody granted it — the same argument
`ADR-106` makes for the `dev` build tag, in a second place where a default is
reachable by accident.

`POST /api/v1/platform/tenants` provisions a tenant **and its first owner**
together (`ADR-113`). It cannot be two calls: every write past that point
authorizes through `admin` on the tenant, and `tenant.admin` is
`[principal] or owner`, so a tenant created without an owner principal is one
nobody can ever administer — and it would return 201 all the same. That
silence is the failure the integration test targets, which is why the test
asserts a successful tenant-plane write by the new owner rather than the shape
of the provisioning response.

**The development authenticator is behind two independent gates** (`ADR-106`).
It compiles only under `-tags dev`, and even then only starts when
`YMCA_AUTH_PROVIDER` is exactly `dev`. There is no default: a binary built
without the tag reports that this build has no dev provider, rather than
quietly choosing one.

```sh
go build -tags dev ./cmd/api
go run  -tags dev ./cmd/api mint-token          <idp-subject> <tenant-uuid>
go run  -tags dev ./cmd/api mint-platform-token <idp-subject>
```

`mint-platform-token` is a separate command rather than `mint-token` with the
tenant left off, and `dev.MintPlatform` is a separate function from
`dev.Mint` for the same reason: `mint-token` still refuses an empty tenant, so
platform authority is never what you get by omitting an argument.

`mint-token` carries the same build tag as the provider, so a deployment build
can no more issue a credential than it can accept one. The token it prints is
valid for 24 hours and is signed with `YMCA_DEV_AUTH_SECRET`; `<idp-subject>`
must match a `principal.idp_subject` already in the database.

```sh
curl -sS localhost:8000/api/v1/t/$TENANT/me \
  -H "Authorization: Bearer $TOKEN" -H 'X-Request-Id: local-1'
```

### Tests

```sh
go test ./...                                   # unit, no services needed
go test -tags dev ./...                         # adds the dev provider's tests
go test -tags 'integration dev' -p 1 ./...      # needs postgres and openfga
```

**`-p 1` is required, not a preference.** The outbox dispatcher drains the
whole `authorization_outbox` table by design — a dispatcher that had to name a
tenant could not do its job — so two packages running dispatchers against one
shared cluster steal each other's rows. Without `-p 1`, `internal/outbox`
intermittently fails with `no renderer for event type
"EntitlementBundleCreated"`: it picked up a row `cmd/api`'s tests had
enqueued, and its own renderer map does not know that event.

The failure is intermittent, which is worse than a consistent one — it passed
three runs in a row before appearing. It is a property of testing a global
queue against a single shared database, not a defect in the dispatcher, and
serializing the packages is the accurate fix rather than a workaround.

The integration tests use the environment above and **fail rather than skip**
when a variable is missing: a test that passes because it never ran is worse
than no test. They prove the things unit tests cannot — that RLS actually
isolates, that an unset `app.tenant_id` raises, that a real OpenFGA tuple
produces ALLOW and its absence produces an audited DENY, and that `GET /me`
returns exactly the permissions the graph grants.

They prove the DAG is a DAG, too, and only because they write edges the way
nothing in the application does — straight at the table, with no `CreateUnit`
in the path. Had they gone through the endpoint, the cycle and depth cases
would be testing its arithmetic rather than the database's invariant, and
removing the trigger would leave them green.

They also now prove the claim ADR-109 exists to make. `TestRoleAssignment_
AgainstRealStore` holds a role assignment fixed in OpenFGA — there is nothing
there to hold — and changes only a column in PostgreSQL:

```txt
a current assignment ALLOWs
an EXPIRED assignment DENIES        no sweeper ran; a column changed
an assignment not yet begun DENIES
a revoked assignment DENIES
a missing required clearance DENIES ADR-087
NO_ROLE_ASSIGNMENT suppresses it    05.9.4, applied in step 2
no role tuple was ever stored       the same check WITHOUT contextual
                                    tuples must DENY
```

The last one is the load-bearing one. If any role tuple had reached the
store, the check without contextual tuples would pass, and the expiry
guarantee above would be resting on a sweeper nobody has written.

`TestOutbox_AgainstRealCluster` proves ADR-101, whose whole reason for
existing is a race that a careful implementation still gets wrong:

```txt
a queued fact is projected
redelivery is not an error          at-least-once (8.9) must not wedge
a voided row is never dispatched
a grant in flight cannot outlive the revocation      <- the one that matters
an unrenderable row records its attempt and stays pending
```

The fourth is ADR-101's actual subject. A grant transaction that began before
a revocation and commits after it must not slip a row past the void. Removing
the advisory lock makes that test report exactly the historical failure:
`Void completed while the grant held the fence lock (voided 0 rows)` — the
revocation deletes a tuple that does not exist yet, reports success, and the
dispatcher then puts it back. The record notes the first draft of this fix had
only the row lock and was wrong; this is what stops the second draft being
wrong the same way.

`TestAdmission_EndToEnd` is the chain's payoff, and what it asserts is an
ALLOW the same person did not have a moment earlier — not that three rows
were enqueued:

```txt
a person with no membership may NOT record   the state before, asserted
the three facts are pending until dispatch   ADR-101's asymmetry, not a race
the admitted member MAY record               holder -> covered ->
                                             covered_member -> via_plan ->
                                             entitled
a retry with the same key replays the 201    and there is ONE membership row
the same key with a different body is 409
a rolled-back attempt leaves nothing to replay
a taken number and an existing membership answer with DIFFERENT codes
a plan that is not OPEN is refused           seeded at the table; no
                                             endpoint can close a plan (B8)
an identifier that is not a uuid is 400, not 500
a caller without may_approve_membership is refused, and writes no row
```

**Every one of these was drift-tested**, per the handoff's standing rule:
deleting the term-window clause fails exactly the expired and not-yet-begun
cases, deleting the clearance clause fails exactly the clearance case, and
expiring `/me`'s role assignment drops the permission from the response.
Admission's eight guards behave the same way — removing ADR-107's
`covered_member` fact fails exactly the pending-count and `may_record` cases,
and renaming one of the two collision constraints fails exactly one of the
two 409s while the other stays green, which is session 6's constraint-name
lesson in a second place.

**One of them passed when it should have failed.** Removing the
`Idempotency-Key` requirement left the whole suite green: the zero-valued key
that resulted reached PostgreSQL as `''::uuid`, raised `22P02`, and came back
as the same 400 the test was expecting. `runIdempotent` now refuses a zero key
with a logged 500 — the honest answer for a server-side mistake — and the test
asserts the message names the header. A check that has only ever been observed
to pass is indistinguishable from one that cannot fail, in the small as well
as the large.

### Consumption to cash

`TestConsumption_RecordingAndCorrection`, `TestBilling_FromObligationToPaidInvoice`
and their neighbours (`consumption_`, `billing_`, `close_`, `directory_`
`_integration_test.go`) drive a whole hostel through the production routes.
What they pin, and not merely observe:

```txt
a retry is the same meal; a different body on the same key is its own 409
one key against two TYPES is two requests      the type is in the key's scope
a second live record for a day is already_recorded, not a second record
staff cannot bill a person who is not entitled  person is global (A2.1)
a correction supersedes; a void is revivable; a record is superseded once
a correction past its window needs may_correct_late, and is audited NOTABLE
obligated units follow the recurrence          20 = 30 days - 10 relieved
a withdrawn absence relieves nothing; a short one relieves recording only
a period closes exactly once                   an exclusion constraint
a closed period takes no record and no correction
no rate rolls the whole close back             and the period stays open
issue numbers an invoice and returns the number on rollback   ADR-103
a payment never exceeds the balance; a retried payment is not a second
reading money is not writing it                finance_reader is not finance_manager
a payer is not shown a draft
```

**Every guard was broken on purpose and failed for its own reason.** Two were
shadowed and needed a second test to be seen at all: the late-correction guard
in the domain is answered identically by the handler's pre-check, so the API
test cannot tell which is missing; `Correct` is called directly to reach it.
And the first draft of the assertion for `admin from tenant` passed with the
path removed, because the type it named hangs under a unit the admin already
administers through the DAG — it now names a type with no `auth_parent`, as
every type created through the API is.

`TestClose_WaitsForTheSubjectLock` holds the (type, subject) lock in one
transaction and asserts the close does not finish. Removing the lock from the
close made it fail correctly — and then hang for ten minutes, because the
failing test left the holder's transaction open and `pool.Close` waits for
every connection. The holder is now released in a cleanup.

`TestFlutterClientAgreesWithTheServer` is the cross-language guard. It boots
the real routes, seeds a draft, a part-paid and a paid invoice, and runs
`app/test/live_api_test.dart` against them. The widget tests use a fake that
agrees with the client by construction, so only this catches a model reading a
key the server does not write. It found two real defects the first time it
ran: the front desk could not read the plans it admits onto, and a front desk
holding `may_approve_membership` could not register the person it was
admitting. Both fixed; `/me` now reports `may_register_person` so the app can
show a button that works. It skips, loudly, without `flutter` on `PATH`, and
CI sets `YMCA_REQUIRE_FLUTTER` so it cannot skip there.

---

## Roles

RLS is how tenant isolation is enforced (`A2.1`, `08.2`). It does not apply to
superusers and it does not apply to roles holding `BYPASSRLS`. If the API
connects as `pgadmin` — this cluster's superuser — every policy in `0001` is
decoration, and the design's central claim is untested everywhere including
production.

So there are two roles, and they are not interchangeable:

| Role       | Used by                | Attributes                           |
| ---------- | ---------------------- | ------------------------------------ |
| `pgadmin`  | `cmd/migrate` only     | superuser; needs DDL, bypasses RLS   |
| `ymca_api` | `cmd/api`, every query | member of `ymca_app`; subject to RLS |

`ymca_app` is created by migration `0001`. It is `NOLOGIN` and holds only DML,
so no password appears in a migration file. The login role is created out of
band, once:

```sql
CREATE ROLE ymca_api LOGIN PASSWORD '<choose one>' IN ROLE ymca_app;
```

`audit_event` is append-only (`A2.10`): `UPDATE` and `DELETE` are revoked from
`ymca_app`, so the append-only property is a privilege rather than a
convention.

### Every query must name its tenant

The policy is `A2.1` verbatim, which uses `current_setting('app.tenant_id')`
without the `missing_ok` argument. A connection that has not set it **raises**
rather than returning zero rows. That is fail-closed and loud, which is the
right side to err on.

`db.InTenantTx` is the only way the application satisfies it, and the value is
set **inside the transaction**, never on the connection:

```sql
BEGIN;
SELECT set_config('app.tenant_id', $1, true);   -- true = local to this tx
...
COMMIT;
```

`SET LOCAL` cannot take a parameter, hence `set_config`. Setting it on the
connection instead would leak the tenant forward: the pool hands that
connection to an unrelated request the moment this one returns it. Anything
reaching a tenant-scoped table outside `InTenantTx` is a defect, and PostgreSQL
says so rather than returning a plausible empty result.

The four tables `8.2` exempts from RLS — `person`, `principal`, `guardianship`,
`restriction` — carry no `tenant_id` and are read through `db.Pool()` directly.
That is the only legitimate use of `Pool()`.

Verified against the running cluster: a `ymca_api` connection with
`app.tenant_id` set to one tenant sees only that tenant's rows, a cross-tenant
`INSERT` is rejected by the policy, an unset `app.tenant_id` errors, and
`UPDATE`/`DELETE` on `audit_event` are denied.

---

## Divergence from the design record

There is none outstanding. Everything this directory does that the record did
not originally say has been written into it (`8.2`, `8.5`, `ADR-106`,
`ADR-107`, `ADR-108`, `ADR-115`, `A1.1`, `A1.6`, `A1.7`, `A1.8` rule 7,
`A2.1`, `A2.2`, `A3.2`, `A3.4`, `A3.6`, `A3.7`, `A3.9`, `05.1.3`, `11.2`,
`12`).

`A3.6` is this round's: its scope sentence said "money or a consumption
record", admission is neither, and the endpoint requires the header anyway —
so the sentence changed rather than the code doing more than the record
describes.

What remains is formatting, and it is checked rather than promised:

| Difference                                         | Where                              |
| -------------------------------------------------- | ---------------------------------- |
| Tables in topological order, not A2's grouping     | `0001_init.sql`, per A2.1          |
| `DEFAULT gen_random_uuid()` spelled out on each id | `0001_init.sql`, per A2's preamble |
| A2.12's unnamed indexes given names                | `0001_init.sql`                    |

### The drift guard

`go test ./fga/` implements A1.8 rules 7 to 10:

```txt
TestModelMatchesA1_2       model.fga must equal the A1.2 fence, byte for byte
TestAssertionsCoverA1_7    every assertion A1.7 promises must actually run,
                           with the expectation A1.7 gives it
TestForbiddenCoversA1_7Refuse
                           every relation A1.7 says must be REFUSED is
                           actually probed by the suite
TestGrantableSetMatchesMigration
                           A1.2's role_assignment#holder set and migration
                           0002's grantable_permission seed must agree, in
                           both directions
```

`go run ./cmd/fga test` adds two runtime guards the parser cannot give:
`LoadAssertions` refuses to write a role tuple to the store at all, and every
tuple in the `forbidden` block must be **rejected** by OpenFGA rather than
merely denied — a DENY means the tuple was accepted and one edit away from
resolving.

Both were checked against real drift, not just observed to pass: editing
`model.fga`, deleting an assertion, and flipping an expectation each produce a
failure naming the line.

This exists because A1.8 rules 1 to 6 were all obeyed and three defects
shipped anyway — A1.2 did not parse, A1.6 was missing two tuples without which
A1.7's first assertion could not resolve, and `entitlement_bundle.via_plan`
was declared and read by nothing. All three are fixed; the guard is what stops
the fourth.

### The DAG invariants

`ADR-016`'s three invariants on `authorization_edge` — no cycles, both
endpoints in this tenant, at most 12 edges on any path — are enforced by a
`BEFORE INSERT OR UPDATE` trigger (`0005`, `ADR-115`), not by the endpoint.

The reason is not tidiness. **A cycle is unreachable through
`POST /t/{t}/units` by construction**: nothing points at a unit that did not
exist a moment ago, so that endpoint can only ever violate the depth bound.
A checker living there could never be observed to fail, which is
indistinguishable from one that cannot — and `05.1.3` words the invariant as
"enforced on every write", which a checker the caller invokes is not.

Two details that look like oversights:

- The recursion carries its own depth bound. An unbounded recursive CTE over
  a graph that already holds a cycle does not terminate, and a guard that
  hangs is worse than one that refuses.
- The function is **not** `SECURITY DEFINER`. It reads two tables under
  `FORCE ROW LEVEL SECURITY`, and a superuser definer would traverse other
  tenants' edges to decide this tenant's invariant — `ADR-108`'s point, in a
  place it would be easy to lose.

`internal/organization` maps the refusal to a domain error **by constraint
name**, which makes that name a contract with Go that nothing else in the
build would notice breaking: a rename compiles, vets clean, and silently
turns every refusal into a 500. `TestCreateUnit_MapsTheTriggerToDomainErrors`
asserts the name the database actually sends.

Drift-tested rather than observed to pass. Disabling the trigger fails four
tests; renaming one constraint fails exactly one, and leaves the other three
green because the cycle is still refused — just under a name Go does not
know. That discrimination is the point.

### Known gaps, carried deliberately

Recorded in `A2.1` and `11.2`, not worked around here:

- **Twelve tables carry no `tenant_id`** and so get no RLS policy —
  `charge` and `charge_component` mean an invoice id reaches its contents.
- **`verification`, `clearance`, `audit_event` have nullable `tenant_id`.**
  The policy makes those global rows invisible to every tenant connection.
- **A unit gets one authorization parent through the API.** `ADR-016`
  permits many — its own example puts a pool under both a chapter and a
  department — and `POST /units` creates the first. Nothing adds a second.
- **The depth limit is not configurable.** `A2.2` says "configured maximum,
  defaulting to 12"; `0005` makes it a constant in the trigger.
- **Three edge types are refused rather than validated.** `A1.2` declares
  `auth_parent` on `resource`, `programme` and `consumption_type`; the
  trigger refuses them rather than admitting an edge whose endpoints nothing
  checked. Fail-closed, and the migration that makes one writable must
  extend the check.
- **`audit_event` is not partitioned.** A2.10 marks it monthly on
  `occurred_at`; C3 records that there are no scale figures to size it against.
- **No office or committee appointment workflow.** `office_conferred_role`
  exists and `role_assignment.via_office_holding_id` is there for it, but
  nothing yet materializes assignments when someone is appointed, or ends
  them when an office is vacated (05.6.4). Committees have no conferral
  table at all, though 05.6.7 routes approval through them.
- **No reverse role query.** "Who holds this permission at this scope" is a
  PostgreSQL query nobody has written. 05.6.7's approval routing needs it;
  ADR-104 and 8.11 mean it must never become a graph query.
- **Plans can be created and listed, not superseded.** 05.3.2 gives plans a
  real lifecycle — editing a live plan is prohibited, supersession is
  supported, `CLOSED_TO_NEW` is the common real state. None of it is
  reachable through the API yet.
- **Membership numbers are caller-supplied and unvalidated.** `UNIQUE
(tenant_id, number)` is the whole mechanism; nothing enforces a format, so
  two conventions can coexist in one tenant.
- **Nothing sweeps `idempotency_key`.** The index for it exists; the sweep
  does not. Keys accumulate until something removes them.
- **Nothing revokes a membership's tuples.** `08` lists "membership
  suspended / lapsed / terminated" among the operations requiring synchronous
  revocation, and no endpoint changes a membership's state — so coverage
  granted at admission outlives the membership that justified it.
- **Admission derives no term from the plan.** `valid_from` and `valid_until`
  are passed through, so a `LIFETIME` plan accepts a membership carrying an
  end date. Deriving one is plan-lifecycle logic no ADR specifies.
- **The application workflow is bypassed.** 05.3.4's DRAFT→ADMITTED state
  machine has no endpoint; admission inserts a membership directly and
  `membership.application_id` is always NULL.
- **Dependents are unreachable.** `membership_dependent`,
  `plan.max_dependents` and A1.2's `membership.dependent` all exist; nothing
  adds a dependent, so `covered` resolves to the holder alone.
- **Admission authority is tenant-scoped only.** 05.3.4 puts approval at "the
  tenant or an org unit above the application"; A1.2 declares no admission
  relation on `organizational_unit`, so only the tenant half is checkable.
- **`invalid_request` had no code until now.** A3.4 listed no status for a
  malformed request at all; a bad cursor, an out-of-range limit and an
  unreadable body had nowhere to go. Added as 400.
- **No dispatcher metrics.** 7.4 makes undispatched rows older than a
  threshold an operational alert. `attempts` and `last_error` are recorded
  per row; nothing watches them (C4).
