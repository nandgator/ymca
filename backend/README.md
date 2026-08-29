# backend

The YMCA platform backend. Greenfield — `prototype/` is reference only.

Design record: `docs/system-design/`. This directory implements it. Where the
two disagree, the design document is right and the code is a defect, except
where a file here records a deviation explicitly.

---

## What exists

```txt
migrations/0001_init.sql   the full A2 schema, 54 tables, RLS on 34
migrations/embed.go        go:embed for the above
cmd/migrate/               applies migrations; forward-only
fga/model.fga              A1.2, reformatted (see below)
fga/assertions.yaml        A1.6 fixture + A1.7 assertions, executable
fga/embed.go               go:embed and the assertion parser
cmd/fga/                   applies the model, runs the assertions
internal/config/           environment configuration
```

## What §6 of the handoff planned and does not exist yet

```txt
compose.yaml     not needed — postgres and openfga already run under podman
Dockerfile       not needed until there is something to deploy
cmd/api/         the HTTP server                          <- next
internal/db  auth  authz  httpx
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

### Every connection must name its tenant

```sql
SET app.tenant_id = '<uuid>';
```

The policy is `A2.1` verbatim, which uses `current_setting('app.tenant_id')`
without the `missing_ok` argument. A connection that has not set it **raises**
rather than returning zero rows. That is fail-closed and loud, which is the
right side to err on, but it means every pooled connection must set the value
before it touches a tenant-scoped table — including on reset after a
connection is returned to the pool.

Verified against the running cluster: a `ymca_api` connection with
`app.tenant_id` set to one tenant sees only that tenant's rows, a cross-tenant
`INSERT` is rejected by the policy, an unset `app.tenant_id` errors, and
`UPDATE`/`DELETE` on `audit_event` are denied.

---

## Divergence from the design record

There is none outstanding. Everything this directory does that A1 and A2 did
not originally say has been written into them (`ADR-107`, `ADR-108`, `A1.1`,
`A1.6`, `A1.7`, `A1.8` rule 7, `A2.1`, `A2.2`, `11.2`, `12`).

What remains is formatting, and it is checked rather than promised:

| Difference                                         | Where                              |
| -------------------------------------------------- | ---------------------------------- |
| Tables in topological order, not A2's grouping     | `0001_init.sql`, per A2.1          |
| `DEFAULT gen_random_uuid()` spelled out on each id | `0001_init.sql`, per A2's preamble |
| A2.12's unnamed indexes given names                | `0001_init.sql`                    |

### The drift guard

`go test ./fga/` implements A1.8 rule 7:

```txt
TestModelMatchesA1_2       model.fga must equal the A1.2 fence, byte for byte
TestAssertionsCoverA1_7    every assertion A1.7 promises must actually run,
                           with the expectation A1.7 gives it
```

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
