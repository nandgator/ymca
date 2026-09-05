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
migrations/embed.go        go:embed for the above
cmd/migrate/               applies migrations; forward-only
fga/model.fga              A1.2, reformatted (see below)
fga/assertions.yaml        A1.6 fixture + A1.7 assertions, executable
fga/embed.go               go:embed and the assertion parser
fga/sync_test.go           A1.8 rules 7-9, machine-checked
fga/grantable_test.go      A1.8 rule 10 — the model vs migration 0002
cmd/fga/                   applies the model, runs the assertions
cmd/api/                   the HTTP server, and GET /me
internal/config/           environment configuration
internal/db/               the pool, and the tenant transaction (8.2)
internal/auth/             the ADR-106 port and its provider registry
internal/auth/dev/         the development provider — //go:build dev only
internal/authz/            6.1's four-step check, and the OpenFGA client
internal/authz/roles.go    step 2 — effective assignments, per check
internal/audit/            8.5's DENY record
internal/httpx/            the middleware chain, A3.4's errors
```

## What §6 of the handoff planned and does not exist yet

```txt
compose.yaml     not needed — postgres and openfga already run under podman
Dockerfile       not needed until there is something to deploy
internal/organization  identity  membership  consumption  finance
```

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
go run ./cmd/fga test       # throwaway store, fixture, 31 assertions
```

`fga test` is the CI command. It creates its own store, applies the model,
writes the fixture and deletes the store afterwards, so a stale tuple from a
previous run can never turn a DENY assertion into a pass. Any failure exits
non-zero, as A1.8 rule 4 requires.

### The API

```sh
go run ./cmd/api                 # serve on YMCA_HTTP_ADDR
```

One route so far, `GET /api/v1/t/{tenant}/me` (`A3.7`, `A3.9`): who the caller
is, and which of four tenant-scoped permissions they hold. Each is a full 6.1
check against the tenant named in the path — the endpoint reports no
object-scoped permission, deliberately, because a `/me` that enumerated them
would be the reverse index `ADR-104` and `8.11` exist to prevent.

**The development authenticator is behind two independent gates** (`ADR-106`).
It compiles only under `-tags dev`, and even then only starts when
`YMCA_AUTH_PROVIDER` is exactly `dev`. There is no default: a binary built
without the tag reports that this build has no dev provider, rather than
quietly choosing one.

```sh
go build -tags dev ./cmd/api
go run  -tags dev ./cmd/api mint-token <idp-subject> <tenant-uuid>
```

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
go test ./...                              # unit, no services needed
go test -tags dev ./...                    # adds the dev provider's own tests
go test -tags 'integration dev' ./...      # needs postgres and openfga running
```

The integration tests use the environment above and **fail rather than skip**
when a variable is missing: a test that passes because it never ran is worse
than no test. They prove the things unit tests cannot — that RLS actually
isolates, that an unset `app.tenant_id` raises, that a real OpenFGA tuple
produces ALLOW and its absence produces an audited DENY, and that `GET /me`
returns exactly the permissions the graph grants.

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

**Every one of these was drift-tested**, per the handoff's standing rule:
deleting the term-window clause fails exactly the expired and not-yet-begun
cases, deleting the clearance clause fails exactly the clearance case, and
expiring `/me`'s role assignment drops the permission from the response.

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
`ADR-107`, `ADR-108`, `A1.1`, `A1.6`, `A1.7`, `A1.8` rule 7, `A2.1`, `A2.2`,
`A3.2`, `A3.4`, `A3.9`, `11.2`, `12`).

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

### Known gaps, carried deliberately

Recorded in `A2.1` and `11.2`, not worked around here:

- **Twelve tables carry no `tenant_id`** and so get no RLS policy —
  `charge` and `charge_component` mean an invoice id reaches its contents.
- **`verification`, `clearance`, `audit_event` have nullable `tenant_id`.**
  The policy makes those global rows invisible to every tenant connection.
- **No cycle check on `authorization_edge`.** A2.2 specifies a pre-commit
  recursive CTE bounded at depth 12; it is application code and unwritten.
  Until it exists the DAG is a graph.
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
