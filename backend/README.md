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

## Deviations from the design record

Each is also recorded at the point in the file where it applies.

### `migrations/0001_init.sql`

| #   | Deviation                                                                     |
| --- | ----------------------------------------------------------------------------- |
| D1  | `location` is created here. A2.2 and A2.5 reference it; A2 defines it nowhere |
| D2  | Tables are in dependency order, not A2's presentation order                   |
| D3  | `DEFAULT gen_random_uuid()` on every `id`, per A2's conventions preamble      |
| D4  | `FORCE ROW LEVEL SECURITY` alongside every `ENABLE`                           |
| D5  | Role `ymca_app` is created                                                    |
| D6  | A2.12's unnamed indexes are given names                                       |

Carried from A2 rather than fixed:

- **Twelve tables carry no `tenant_id` and therefore no RLS policy** —
  including `charge` and `charge_component`, which means invoice contents are
  reachable with an invoice id alone. A2.1 says every tenant-scoped table
  carries `tenant_id`; these do not.
- **`verification`, `clearance` and `audit_event` have a nullable
  `tenant_id`** for deliberately global rows. Under A2.1's policy those rows
  are invisible to every tenant connection, and nothing here provides another
  path to them.
- **No cycle check on `authorization_edge`.** A2.2 specifies a pre-commit
  recursive CTE bounded at depth 12. That is application code and is not
  written. Until it is, the DAG is a graph.
- **`audit_event` is not partitioned.** A2.10 marks it monthly on
  `occurred_at`. C3 records that there are no scale figures yet, and
  partitioning now would fix a key before anyone knows the retention policy.

### `fga/model.fga`

**Not byte-identical to A1.2, because A1.2 does not parse.** The OpenFGA DSL
requires each `define` on one line; A1.2 wraps four of them across six
continuation lines for readability:

```dsl
define admin: [principal]
              or admin from auth_parent      # <- syntax error
```

Those six lines are joined here. Nothing else changed: 315 lines became 310,
and the model is otherwise A1.2 character for character. **A1.2 should be
reformatted to match**, or the two will drift the first time someone edits one
of those four relations.

### `fga/assertions.yaml`

Two defects in A1, both demonstrated rather than argued — see the header
comment in that file.

- **DEFECT-1: A1.6's tuples cannot satisfy A1.7's first assertion.** A1.6
  lists `subscription:sub-77 grants_bundle entitlement_bundle:pool-access`,
  but nothing in the model reads `subscription.grants_bundle`. The relation
  that resolves is `entitlement_bundle.via_subscription`, and A1.6 never
  writes it — nor `membership:m-123 holder person:alice`. Removing those two
  tuples from the fixture fails `alice may_use` and `alice may_record`.
- **DEFECT-2: `entitlement_bundle.via_plan` is unreachable.**
  `define beneficiary: [person] or beneficiary from via_subscription` never
  mentions `via_plan`, so a bundle conferred by a membership plan entitles
  nobody. `membership_plan.entitlement_bundle_id` in A2.4 implies the other
  reading. Admitting a member will need a synthetic `subscription` object
  purely to carry the plan's own bundle, unless A1.2 changes.

A1.7 also writes subjects bare — `alice`, `natl-sec`. The model does not:
`resource.may_use` reaches a **person** through an entitlement bundle, while
`organizational_unit.member_read` takes a **principal** directly. The same
name is two different subjects depending on the relation. `assertions.yaml`
spells the type out on every line and marks each one `TYPED`.

Three assertions are marked `ADDED`. They are positive checks that exist to
prove a neighbouring DENY is not vacuous — a negative test against an empty
fixture passes for the wrong reason, which is the weakness `REVIEW.md` §2
found in 8.12 test 2.
