# Handoff — YMCA Platform

Current as of 2026-10-23, end of the ninth working session — the one that
closed the slice's loose ends (a login for a member, the tenant's own day,
credit notes, plan lifecycle, a reconciler). Session 8 finished the slice.
Written so that state can be picked up by another session or another person.

**Untracked by intent**, like `REVIEW.md` and `ORCHESTRATION.md`. Not part of
the design record.

Read in this order: this file for state and process, `ORCHESTRATION.md` for how
the unattended finish was run, `REVIEW.md` for the open items and their agreed
order, `docs/system-design/` for the design itself.

---

## 1. Where the work stands

**The slice is done.** Step 8 — entry to bill, end to end, plus one
permission-gated Flutter app — runs from a cold start: a bootstrapped operator
provisions a tenant, its owner configures plans and consumption types, the front
desk admits members, members record meals, an administrator closes a period, and
a bursar issues and settles the invoice that falls out. Eight migrations,
118 ADRs, a CI workflow, and a Flutter client for web, Android and Linux. It was driven end to end in a real browser
against a real server before this was written.

**Session 9 — eight commits on top of session 8's** (`d4684f7..HEAD`), each
with its tests, its ADR and its doc changes in the same commit:

```txt
feat(ops): reconcile-coverage compares PostgreSQL with the graph (ADR-122)
feat: an invoice list shows its balance, and the directory says when a term has ended
feat(membership): a plan is closed, replaced and retired, and a holder moves explicitly (ADR-121)
feat(app): staff issue credit notes, and a payer sees them
feat(finance): a credit note corrects an issued invoice (ADR-120)
feat(app): the day is the tenant's, not the device's or UTC's
feat(backend): a tenant has a time zone, and today is the tenant's (ADR-119)
feat(backend): bootstrap-principal gives a registered person a login
```

Migrations 0009 (tenant time zone), 0010 (credit notes, `VOIDED`) and 0011
(plan lifecycle) are applied; the total is eleven. Four ADRs, 119–122.

**Pushed to a branch, not to `main`.** The work is on `session-9` of
`nandgator/ymca` (all of it, unsigned, authored as `nandgator`); `main` is still
at `d4684f7`. No pull request exists, and CI runs only on pull requests and on
pushes to `main`, so **the workflow has still never run on GitHub**. It was
reviewed against the commands run locally and now also checks that the suite
leaves no tenant and that the graph and database agree.

**Authorship was rewritten** to `nandgator <38415384+nandgator@users.noreply.github.com>`
(the identity of every earlier commit), signatures were removed, and git is
configured to the same identity with signing off. No commit message, comment or
document names a tool as author.

**Thirteen commits in session 8** (`a29b61c..d4684f7`): 131 files, 15,255 insertions
across `backend/`, `app/`, `docs/` and `.github/`:

```txt
8bc1e9f  test: two cleanups that could not fail, and so leaked a tenant per run  <- HEAD
c3c36f3  feat(app): suspend, reinstate and terminate a member
1c57c37  feat(backend): a membership can be suspended ... coverage ends
5b438f0  docs(design): ADR-118
6cd409b  feat(backend): a first operator, and a browser that is allowed to ask
d6d92c7  feat(app): the screens
59f53f2  feat(ci): the suite runs somewhere that is not a laptop
7a01600  feat(app): the spine
42d9718  feat(backend): what the app reads
7bec2d7  feat(backend): consumption to cash
f3c8880  docs(design): ADR-116 and ADR-117
b1e34a5  feat(backend): the tenant admin manages its consumption
a670152  docs(design): consumption and finance, written down first
```

**Nothing was pushed.** `origin/main` is where session 7 left it; pushing is
Joe's call and was not given. Check `git log origin/main..HEAD` rather than
trusting this paragraph. **The CI workflow has never run** — it was written
against what was run locally, step by step, and it is the first thing to push
and watch (§8).

**`.gitignore` carries an unrelated `.codegraph` line** added by tooling, not
by this work. Left unstaged deliberately, now across eight sessions.

**Verified working right now**, not merely written — in a cloud container with
no podman, rebuilt as below. Every Go test run was on PostgreSQL 18.6 and
OpenFGA 1.22.0, both the latest stable at the time:

```txt
go build ./... && go vet ./...                      clean, also -tags dev and 'integration dev'
gofmt -l backend                                    clean
go test ./...                                       unit + the A1.8 sync guards
go test -tags 'integration dev' -p 1 ./...          every package, run twice, zero tenants left
go run ./cmd/fga test                               47/47 (assertions unchanged this session)
go run ./cmd/api reconcile-coverage                 0 disagreements
TestFlutterClientAgreesWithTheServer                PASSES — first time the Dart half ran here
flutter analyze / dart format / flutter test        clean / clean / 101 pass, 1 skip (live test, by design)
bunx prettier --check / markdownlint-cli2           0 issues on every changed file
```

**Rebuilding the environment in a fresh container** (nothing here persists):

```txt
postgres 18   curl the zonky jar from Maven Central
              (io/zonky/test/postgres/embedded-postgres-binaries-linux-amd64/18.6.0), unzip, tar -xJf the
              .txz into /opt/pg18; initdb as the `postgres` user in /opt/pg18data with -U pgadmin
              --auth=trust; start on :5432. NOT under /tmp/claude-0 — that directory is 0700 and
              the postgres user cannot reach it. Then: create database ymca; run cmd/migrate up as
              pgadmin; CREATE ROLE ymca_api LOGIN PASSWORD 'ci-api' IN ROLE ymca_app.
openfga       GOBIN=<dir> go install github.com/openfga/openfga/cmd/openfga@latest ; `openfga run`
              (in-memory) ; `go run ./cmd/fga apply` prints the store and model ids to export.
flutter       curl the stable archive named in
              storage.googleapis.com/flutter_infra_release/releases/releases_linux.json
              into /opt/flutter; add /opt/flutter/bin to PATH; set YMCA_REQUIRE_FLUTTER=1.
github.com, ftp.postgresql.org and apt.postgresql.org are refused by the proxy;
proxy.golang.org, repo1.maven.org, storage.googleapis.com and registry.npmjs.org work.
```

The table below is session 8's, from Joe's machine, kept for the commands:

```txt
go build ./... && go vet ./...                      clean, also -tags dev and integration
gofmt -l backend                                    clean
go test ./...                                       unit + the A1.8 sync guards
go test -tags 'integration dev' -p 1 ./...          every package, run twice
go run ./cmd/migrate status                         0001-0008 applied
go run ./cmd/fga test                               47/47 + 4 forbidden refused
flutter analyze                                     no issues
flutter test                                        78 pass, 1 skip (the live-API test, by design)
dart format --set-exit-if-changed lib test          clean
bunx prettier --check / markdownlint-cli2           0 issues in 29 files
```

`~/.claude/projects/-home-joe-dev-ymca-mess/.../scratchpad/verify.sh` ran the
Go half in one command; it is session-scoped and gone, so rebuild it from the
table above. The suite leaves the database and OpenFGA clean, and **that is now
asserted rather than hoped**: each harness cleanup reads back the outbox and
the tuples it deleted and fails the test if anything survives (§3).

**The environment was cold at the start of this session.** The postgres and
openfga containers had been stopped for weeks: `podman start postgres openfga`.
A fresh `go run ./cmd/fga apply` with `YMCA_FGA_STORE_ID` empty creates a store
and prints both ids; export them for the integration tests. There are now many
stores named `ymca` in OpenFGA for exactly that reason.

**Environment traps**, unchanged and still costly:

```txt
.env quotes its value    YMCA_API_PASSWORD='...' — strip the quotes before
                         building a connection string, or SASL auth fails
migrations need pgadmin  cmd/migrate wants the SUPERUSER (README's Roles
                         table). ymca_api is DML-only and cannot run DDL.
                         Get it from `podman exec postgres printenv
                         POSTGRES_PASSWORD`
podman source address    a host connection arrives from the podman gateway,
                         so it falls through to scram-sha-256
pkill -f in a Bash call  matches the Bash tool's own command line and kills
                         the shell (exit 144). Kill by PID
```

**`-p 1` is still required.** The outbox dispatcher drains the whole table by
design, so two packages running dispatchers against one cluster steal each
other's rows. Run the suite **twice**, always.

**The platform test leaked one tenant per run until this session.** Its cleanup
deleted `entitlement_bundle` through a bare pool connection, a policied table, so
A2.1's fail-closed error raised, the cleanup ignored it, and the tenant row could
not then be deleted under the surviving bundle. Thirty-eight tenants had
accumulated since session 5. It is the same mistake HANDOFF §3 names for session
7's cleanup — a cleanup that cannot fail the test cannot tell you it failed — and
it happened in a test written before the rule. The cleanup now runs inside
`InTenantTx` and reports a failed delete, as does the bootstrap test's (whose own
first version closed its pool before its cleanup ran). The database holds zero
tenants, principals and people after a full run, and that is worth re-checking:

```txt
podman exec postgres psql -U pgadmin -d ymca -tAc \
  "select (select count(*) from tenant),(select count(*) from principal),(select count(*) from person)"
```

**Sixteen DISPATCHED outbox rows from sessions 5 and 6 are still in the table**,
orphaned and harmless. Left rather than deleted unprompted; ask Joe.

---

## 2. Sessions

| Session                                | Relationship           | Notes                                                    |
| -------------------------------------- | ---------------------- | -------------------------------------------------------- |
| (this clone, no id recorded)           | this one, session 9    | login command, time zone, credit notes, plans, reconcile |
| `82f4fa32`                             | predecessor, session 8 | consumption to cash, the app, CI, lifecycle; R13         |
| `2d515af4`                             | predecessor, session 7 | admission, A3.6's scope, the 22P02 fix; R12              |
| `86435294`                             | predecessor, session 6 | people, units, the DAG invariants; R11                   |
| `dfa63480`                             | predecessor, session 5 | step 8.3.5, the platform plane; R10                      |
| `bd9ff2b3`                             | predecessor, session 4 | B7; step 8.2.5; 8.3 foundations + config; R8 and R9      |
| `0e7d0387`                             | predecessor, session 3 | backend step 8.2; record reconciliation; R7              |
| `68adc319`                             | predecessor, session 2 | backend step 8.1; A1/A2 reconciliation; R5 and R6        |
| `Conventional commit message [0b3288]` | predecessor, session 1 | design review R1–R4, all doc commits through `7738a77`   |

**Session 9 used small workers for mechanical work only and kept every
correctness-bearing piece in the foreground.** Four jobs went out, each with a
command as its acceptance: an inventory of every place the code decides what day
it is (112 call sites, classified), a review of `ci.yml` against what ran
locally, installing a Flutter SDK and running the app's baseline, and the
prettier/markdownlint gate over the changed documents. All four were read-only or
gated by an exit code, and none was asked to write a test. Two were checked
against the repository afterwards (the inventory's counts and the CI review's
`sed` claim); the inventory also noticed a hard-coded month in a test that would
have failed on 1 November, which was then fixed. Everything that changes a
decision — the migrations, the SQL, the handlers, the screens and every test —
was written and drift-tested in the foreground.

**Session 8 used three Haiku subagents, once each, and never resumed them.**
They built the six Flutter screens against a fixed contract (the API interface,
the fake, the conventions, a Key per widget) in parallel, in ~12 minutes each.
Every one of them shipped the same defect — an idempotency key made once when a
screen opened and reused after the request had changed, which the server answers
`idempotency_key_reused` — and their tests mounted screens inside
`MaterialApp.home`, which hides every provider from pushed routes, so the app
would have thrown on the first tap on a bill. One agent reported 11 failing tests
as "test infrastructure". All three were **reviewed line by line and the screens
rewritten where wrong**; their tests were replaced by ones that were each broken
on purpose. Delegation paid for the volume (~6,000 lines of widget code and
tests); it did not pay for the correctness, which stayed in the foreground. See
§3 and `ORCHESTRATION.md`.

**Session 7 used no subagents.** The round was one endpoint and three
decisions, and its findings again came from running things rather than
writing them: the drift test that passed, the cleanup that leaked, and a
misleading failure message all surfaced by executing the suite.

**Session 6 used no subagents.** The work was two decisions and their
consequences, and three of its four findings came from running the suite
rather than from writing code — the same split session 5 reported.

**Session 5 used one subagent and then stopped.** The record round went to a
Sonnet subagent and produced good work; its own arithmetic about the result was
wrong in two places, which foreground verification caught. The implementation
agent died on a rate limit, and two things followed that are worth knowing:
**resuming an agent by message does not carry its spawn's `model` override**, so
it silently came back on Opus, and re-spawning with the override set explicitly
did not stick either. The implementation was done in the foreground instead.

On this round's evidence that was the right place for it anyway. Three of the
four real findings came from verifying rather than writing — see R10.

**Session 4 used no subagents.** Everything was done in the foreground, which
suited work whose whole value was in noticing that a specified mechanism could
not be built.

**Session 3 used two Sonnet subagents**, at Joe's instruction, one for the
record changes and one for the implementation. Both are finished; nothing is
running. The implementation agent **died mid-verification** on a session
limit, having written every file but never having run the integration tests.
Its work was reviewed and finished in the foreground — which is how the
`42P18` defect was found. Treat an agent's unreported work as unverified.

Transcripts:

```txt
session 7  ~/.claude/projects/-home-joe-dev-ymca-mess/2d515af4-670d-44fd-8d7f-992cf2f1d2e6.jsonl
session 6  ~/.claude/projects/-home-joe-dev-ymca-mess/86435294-005c-4bc3-8c17-bc3e9cd729d0.jsonl
session 5  ~/.claude/projects/-home-joe-dev-ymca-mess/dfa63480-ff46-4587-bb59-a094141ace2f.jsonl
session 4  ~/.claude/projects/-home-joe-dev-ymca-mess/bd9ff2b3-29ff-4e12-b936-da1485b40afe.jsonl
session 3  ~/.claude/projects/-home-joe-dev-ymca-mess/0e7d0387-613a-45d9-8bf3-50d8fc2f4fdc.jsonl
session 2  ~/.claude/projects/-home-joe-dev-ymca-mess/68adc319-a67d-452a-ab34-bc366630ee7b.jsonl
session 1  ~/.claude/projects/-home-joe-dev-ymca-mess/a4b4c26c-aeaa-44a5-8b50-244a500965bc.jsonl
```

---

## 3. Standing process rules

These came from Joe and hold for whoever continues.

| Rule                                                                                           | Where it lives                                          |
| ---------------------------------------------------------------------------------------------- | ------------------------------------------------------- |
| **Grill relentlessly until there is shared understanding before making changes.**              | stated in-session; every work item began with questions |
| **Nothing the code does may go unsaid in the design record.**                                  | stated session 2; see below                             |
| **Update this file at the end of every session.**                                              | stated session 2                                        |
| **Keep `backend/README.md` current at the end of every session too.**                          | stated session 3                                        |
| **Every decision takes the CLI into account as a possible caller.**                            | stated 2026-09-02; `REVIEW.md` §3 B7                    |
| **Every guard a session adds is drift-tested before it is believed.**                          | stated 2026-09-05; see below                            |
| **The integration suite runs with `-p 1`, twice.**                                             | session 5; see §1                                       |
| **A test must not assert over state it does not own.**                                         | session 6; see below                                    |
| **A test's cleanup is scoped by `aggregate_id`, never by a payload field.**                    | session 6; see below                                    |
| **A drift test must fail for its OWN reason, not merely fail.**                                | session 7; see below                                    |
| **A key belongs to a request, not to a screen or a form.**                                     | session 8; `app/README.md`                              |
| **A cleanup must read back what it deleted and fail if anything survives.**                    | session 8; `harness_integration_test.go`                |
| **A test goroutine holding a transaction releases it in a cleanup.**                           | session 8; `TestClose_WaitsForTheSubjectLock`           |
| **Two guards with one status code shadow each other; test each where the other cannot reach.** | session 8; see below                                    |
| **Run the real thing: cold start, a browser, the other language's client.**                    | session 8; see below                                    |
| **Code that decides a day is tested in two zones, +14 and -11.**                               | session 9; `time_zone_integration_test.go`              |
| **A harness cleanup reports a tenant it cannot delete.**                                       | session 9; `harness_integration_test.go`                |
| **A guard that nothing can fail is not written** (session 9 deleted one)                       | session 9; `SupersedePlan`                              |
| No `Co-Authored-By` trailer in commit messages                                                 | memory `no-co-authored-by-trailer.md`                   |
| A code fence that cannot be classified uses `txt`                                              | memory `txt-for-unclassified-code-fences.md`            |
| `REVIEW.md` and `HANDOFF.md` stay untracked                                                    | stated in-session                                       |
| Conventional Commits                                                                           | commit history                                          |
| `bun run format` then `bun run lint` before committing markdown                                | `package.json`                                          |

Lint config: MD013 off, MD024 siblings-only, `prototype/**` ignored.

**On drift-testing.** A guard is not believed because it passed. It is
believed because it was made to fail, on purpose, for the reason it exists —
and then restored. Every session that adds a check runs it against real drift
before trusting it: break the thing the guard watches, confirm the guard
fails, confirm it fails for the right reason and that nothing else fails with
it, then put it back.

This is not ceremony. A1.8 had six rules of change discipline, all obeyed, and
three defects shipped through a full review — one of which meant the model did
not parse at all. Session 3's DENY assertion passed on a fresh database and
failed on every re-run. Both were found by running the thing rather than
reading it. A check that has only ever been observed to pass is indistinguishable
from a check that cannot fail.

**And it must fail for its own reason.** Session 7 removed the
`Idempotency-Key` requirement from admission and the whole suite stayed
green. The zero-valued key that resulted reached PostgreSQL as `''::uuid`,
raised `22P02`, and came back as the same 400 the test was asserting — one
error path shadowed by another with the same status code. A guard whose
absence is masked by a second guard has never been observed to fail, which is
the same position as not having it.

Two consequences worth carrying. When two failure modes share a status code,
assert something that only one of them can produce — the message, if that is
all there is; a client must not parse a message, but a drift test may. And
when a drift edit makes a test fail, read WHY: the same session's
`#covered` drift failed a second, unrelated assertion whose message blamed
the replay for a row left pending by a failed dispatch, which is the
"guard's failure sending the reader the wrong way" problem for the third
round running.

Two practical notes, learned the expensive way. Take the backup **before** the
drift edit and restore from that copy — `git checkout <path>` on a file with
uncommitted work reverts it to HEAD and discards the session's changes, not
just the drift edit. And put the backup in the session scratchpad, never in the
working tree, per the snapshot rule below.

**On asserting over shared state.** Session 6 had a test fail at 7-want-5
while naming the wrong suspects, because it counted pending rows over the
WHOLE `authorization_outbox` and two strays from another package were in it.
The dispatcher drains that table globally by design, so a table-wide count is
an assertion about every other test's leftovers. Scope every such assertion to
the aggregates the test itself created. `-p 1` reduces this class; it does not
close it, because leftovers survive between runs.

The cleanup half is the same lesson from the other side. Scoping an outbox
delete by `payload->>'tenant_id'` misses every fact whose payload has no
tenant_id — `AuthorizationEdgeCreated` is one — and session 6 left 61 rows
behind that way. **The configuration test's own cleanup comment already warned
about this, in those words, and reading it did not prevent repeating it.**
Scope by `aggregate_id`, which every fact carries, and collect the ids by
QUERY rather than from variables: a subtest that was supposed to be refused
and was not creates a row whose id nothing holds.

Session 7 found the third way to get this wrong, and it is the quietest.
Its cleanup collected those ids correctly and read them through `db.Pool()`
— `membership` is policied, so A2.1's fail-closed error raised, the error was
ignored because a cleanup "cannot fail a test", and three pending rows were
left behind reporting nothing. **A cleanup that cannot fail the test cannot
tell you it did not work.** The fix is two lines: read inside `InTenantTx`,
and have the cleanup count what it left behind and `t.Errorf` if it is not
zero.

**What session 8 added to the process, each from a thing that went wrong.**

_A fixture that cannot fail._ The first assertions for `admin from tenant`
named `bombay/dinner`, which hangs under a unit the admin already administers
through the DAG, so removing the new path left all forty-seven green. It was
found because the drift test passed. They now name `bombay/lunch`, a type with
no `auth_parent`, as every type created through the API is. A negative test
against a fixture that grants nothing, and a positive one against a fixture
that grants it by another route, are the same mistake.

_Shadowed guards._ Three times a guard and its sibling answered with the same
status, so removing either changed nothing through the API: the late-correction
window (handler pre-check and `Correct`'s recheck, both 409), `Void` called
twice in `Suspend`, and the term check against the revoked tuple. The fix is the
same each time — call the inner layer directly, or assert at the graph, where
the outer one cannot answer first. If removing a guard leaves everything green,
there are two of it, or none.

_A test that holds a transaction._ `TestClose_WaitsForTheSubjectLock` failed
correctly when its guard was removed — and then hung for ten minutes, because
the failing test left the lock holder's transaction open and `pool.Close` waits
for every connection. The holder is released in a cleanup now.

_A cleanup that cannot tell you it failed_ — HANDOFF §3 already said so, and it
happened again: a fact granted twice (admitted, then reinstated) has two
dispatched outbox rows for one tuple, OpenFGA refuses a delete batch naming one
tuple twice, and the cleanup passed the batch whole, failed, ignored the failure
and leaked a tenant's administrator into the next run. De-duplicate, delete, and
read back.

_Run the real thing._ The Dart half of `TestFlutterClientAgreesWithTheServer`
found two defects no widget test could (the front desk could not read the plans
it admits onto; a front desk with one of two relations could not complete an
admission). Cold-starting a server and driving the Flutter web build in headless
Chromium found two more (no CORS, and no way to make the first operator). A
fake that agrees with the client by construction proves the client agrees with
itself.

_Provider placement._ Providers inside `MaterialApp.home` are invisible to every
pushed route and bottom sheet. Tests that mount a screen inside `home:` cannot
see it; `test/shell_test.dart` drives the real app through a navigation.

_Delegation._ See §2. A worker gets a task when its inputs fit in the prompt,
its acceptance is a command that exits 0, and a wrong answer is caught by a gate
and not by judgement. Even then its tests are the part to distrust: three
workers produced tests that passed with the guard removed. Never resume a worker
by message (it comes back on the parent's model).

_Session 9, each from a thing that went wrong or nearly did._

_A test that discriminates only part of the day._ Kiritimati (UTC+14) and Pago
Pago (UTC-11) are never both on the UTC date, so a test that runs both fails for
a site that went back to UTC at every hour the suite could run. One zone alone
does so for about half of them. The same pair is used in Go and in Dart, and one
Dart test takes an injected clock so a month boundary can be crossed on purpose.

_A guard that was dead code._ `SupersedePlan` first refused a successor that was
`OPEN` and replaced; removing the second clause left everything green, because a
replaced plan is always closed (a CHECK in migration 0011) and the status clause
answered first. It is gone, with a comment saying why. This is the session 8 rule
("if removing a guard leaves everything green there are two of it, or none")
applied to a guard I had written an hour earlier.

_A harness that hid a leak._ A new tenant-scoped table made the tenant
undeletable and the test passed anyway, because the harness ignored the result of
its own `DELETE FROM tenant`. Found when the next run failed on a duplicate key.
It reports now, and the CI workflow fails on a tenant left behind.

_A refusal found by the other language._ The first live credit note in the Dart
contract test asked for 100.00 against a smaller invoice and the server said
`credit_exceeds_invoice` — through the real client, and not as a unit test.

**On divergence.** Session 2 shipped `56a671e` with a "deviations from the
design record" section listing things the code did that the record did not
say. Joe's response was to close them before proceeding, not to carry them.
The right move when the code must depart from the document is to change the
document in the same breath — or to change the code. A deviation list is a
debt, not a disclosure.

**On answering design questions.** Asked to choose between two entitlement
models, Joe answered with three association membership pages rather than
picking an option. The evidence settled it in a way argument would not have,
and produced a better answer than either option offered (ADR-107). When a
domain question comes up, look at what real associations actually do.

**One hard-won operational rule:** never place a historical snapshot of the
docs in the live working tree to compare against. It was done once in session
1 to reconstruct a commit split, and it collided with a lint run Joe kicked
off at the same moment. Use `git worktree add` or the session scratchpad.

**On delegation.** Session 3 delegated both the record changes and the
implementation to Sonnet subagents. It worked, with two costs worth knowing.
An agent running `bun run format` reformats the whole repo, this file
included — scope it with `bunx prettier --write <paths>` instead. And an
agent that dies before reporting has still written files: review them and run
the verification yourself rather than trusting the last thing it said.

Session 5 adds three more, and they changed how the session ran:

- **A resumed agent does not carry its spawn's `model` override.** Sending a
  message to a rate-limited Sonnet agent brought it back on Opus — the
  expensive path, silently. Re-spawning with the override set explicitly did
  not stick either. Check which model an agent is actually on before assuming
  a delegation is saving anything.
- **Verify an agent's numbers, not just its work.** The record agent's output
  was good; its report miscounted the assertions and the linted file count.
  Both were wrong in harmless directions, which is exactly how a wrong number
  survives into a handoff and gets trusted next session.
- **A cold subagent re-reads everything.** The record round cost ~262k tokens
  across 109 tool calls, most of it reading files the parent session already
  had. Delegation pays when the agent's context is genuinely separable; it
  does not when the work depends on facts the parent just established.

**Shell note.** The environment reports fish, but the Bash tool runs zsh:
heredocs work, unquoted variables do not word-split, and `cd` persists between
calls. Prefer absolute paths and `env VAR=x cmd`.

---

## 4. Environment — resolved

Checked 2026-08-29. The container question is **closed**.

```txt
go        1.26.5 linux/amd64
podman    6.0.2 rootless, podman-compose present
docker    NOT INSTALLED         no longer relevant
psql      NOT INSTALLED on the host; use `podman exec postgres psql`
```

**Postgres and OpenFGA already run as podman containers.** No `compose.yaml`
was written and none is needed.

```txt
postgres   18.4-alpine     :5432          superuser pgadmin
openfga    latest          :8080 HTTP  :8081 gRPC  :3000 playground
valkey     :6379           unrelated to this project
pgadmin    :5431           holds an idle session on the ymca database
```

Four things about this cluster that shape the code:

- **OpenFGA stores its own state in the `postgres` database**, including its
  own `goose_db_version` table. The application uses the `ymca` database, so
  the two goose ledgers never collide.
- **`pgadmin` is a superuser with `BYPASSRLS`.** RLS is how tenant isolation
  is enforced and does not apply to superusers. Migration 0001 creates
  `ymca_app` (NOLOGIN, DML only); the login role `ymca_api` is created out of
  band. Connecting the API as `pgadmin` makes every policy decoration. ADR-108.
- pgAdmin's idle session blocks `DROP DATABASE ymca`, which is the documented
  reset path under forward-only migrations. Terminate it first —
  `backend/README.md` has the statement.
- A dev login role `ymca_api` exists with a throwaway password. Session 3
  rotated it — the previous one had never been recorded anywhere, so nothing
  could use it. The current one is in `.env` at the repo root as
  `YMCA_API_PASSWORD`, **quoted**, and `.env` is gitignored (`.gitignore:170`);
  it is also in the session-3 transcript. Rotate or drop it before this
  reaches anything real. Strip the quotes before building a connection
  string — §1's first trap — or use the scratchpad script that does it.

---

## 5. Decisions on the record

Twenty-two ADRs added across sessions 1, 2, 4, 5, 6, 8 and 9, taking the total to 122. **Session 7 added none.** Session 9 added four (119–122, below the table). Session 8 added three (116–118), each a decision
the record had left open or contradicted: how a period closes exactly once, how
Consumption hands off to Finance without an event bus, and how a membership's
coverage ends — where 05.3.10 said suspension is "consulted after the graph"
and 5.3.9 and 8.3 said it is synchronous, and only one can be built. Full text
in `docs/system-design/09_architecture_decisions.md`; per-file change logs in
`REVIEW.md` §6 as rounds R2 through R12.

**Session 7's three decisions live in the contract rather than in an ADR**,
because each chose between readings of an existing decision rather than
making a new one:

```txt
two 409 codes, not one `conflict`    A3.4. A3.4 writes the message for a
                                     developer, so a client must not parse
                                     it; only the code can distinguish
                                     "choose another number" from "this
                                     person is already a member"
A3.6 widened to admission            A3.6. It creates neither money nor a
                                     consumption record, so the scope
                                     sentence changed rather than the
                                     endpoint quietly requiring more than
                                     the record asks for
the term is passed through, never    A3.7 and 11.2. Deriving it from
derived                              plan.duration is plan-lifecycle logic
                                     nothing specifies; the consequence
                                     (a LIFETIME plan accepting an end
                                     date) is open rather than closed by
                                     invention
```

| ADR | Decision                                                                               | Session |
| --- | -------------------------------------------------------------------------------------- | ------- |
| 101 | Async projection is fenced against synchronous removal                                 | 1       |
| 102 | The two reaching verbs are recorded, never graph-bearing                               | 1       |
| 103 | Invoice numbers come from a transactional counter                                      | 1       |
| 104 | Lists authorize the scope, not the row                                                 | 1       |
| 105 | The tenant is a path segment                                                           | 1       |
| 106 | Authentication is a port                                                               | 1       |
| 107 | Entitlement reaches a person by plan or by subscription                                | 2       |
| 108 | Tenant isolation rests on a role, not only on the schema                               | 2       |
| 109 | Role assignments resolved per check, never stored                                      | 4       |
| 110 | The grantable set is declared by type restriction                                      | 4       |
| 111 | The plane is a property of the credential; zero = tenant                               | 5       |
| 112 | Platform audit is isolated by privilege, not by policy                                 | 5       |
| 113 | A tenant is provisioned with its first owner, or inert                                 | 5       |
| 114 | Registering a person is a tenant permission                                            | 5       |
| 115 | The DAG's invariants are enforced by the database                                      | 6       |
| 116 | A period is closed once by a constraint, and serialises with the records it counts     | 8       |
| 117 | Consumption hands off to Finance by a call inside the closing transaction              | 8       |
| 118 | A membership's coverage is revoked synchronously; its term is checked at decision time | 8       |
| 119 | "Today" is the tenant's: a zone on the tenant and one place that derives a day         | 9       |
| 120 | An issued invoice is corrected by a credit note with its own number                    | 9       |
| 121 | A plan is closed, replaced and retired, never edited; a holder moves explicitly        | 9       |
| 122 | PostgreSQL and the graph are compared by a command that reports before it repairs      | 9       |

Ten carry implementation obligations that are easy to get wrong.

**ADR-101 — two races, two locks.** A revocation must take
`pg_advisory_xact_lock` on the fence key _and_ the dispatcher must hold its row
lock across the OpenFGA write. The row lock alone serializes revoke against the
dispatcher but not against a grant that began before the revoke and commits
after it. The first draft of this fix had only the row lock and was wrong.

**ADR-102 — `tenant.may_administer` was renamed `administered_by`.** There were
two unrelated relations with the same name: an internal one feeding
`organizational_unit.admin`, and the cross-tenant authority verb. Wiring the
verb into the internal relation — the obvious reading — would have handed a
national body admin over every unit in the target tenant. Do not merge them back.

**ADR-103 — allocate the number as late in the issuing transaction as
possible.** `UPDATE invoice_number_series SET next_value = next_value + 1 ...
RETURNING next_value - 1`, inside the transaction that issues the invoice.

**ADR-107 — the bundle names its sources, never the reverse.** OpenFGA
traverses forward from the object, so `entitlement_bundle.via_plan` and
`.via_subscription` are the only expressible direction. A plan naming the
bundles it grants reads naturally and resolves to nothing — which is exactly
what the original model did, and it shipped through a full review.

Admission writes that tuple now — one per membership,
`internal/membership/admission.go` — and **getting it wrong is silent**: a
membership without it holds no entitlements, every `may_record` check denies
correctly, and nothing reports why. The `#covered` suffix is the part an edit
will lose. The user is the membership's covered USERSET, because what a plan
covers is every person the membership covers, not the membership; A1.2 states
that as a type restriction, so removing the suffix fails the dispatcher's row
rather than granting the wrong thing.

**ADR-106 — two gates, and the tag is `dev` not `!prod`.** The development
authenticator compiles only under `-tags dev` and starts only when
`YMCA_AUTH_PROVIDER` is exactly `dev`. The tag polarity is the load-bearing
part: a forgotten build flag must yield a binary WITHOUT the dev
authenticator, never one with it. `internal/auth/registry.go` keeps the
parent package from importing the provider at all, so a plain build simply
lacks the name rather than failing to compile.

**ADR-111 — the zero value is the tenant plane, and that is the whole point.**
`auth.PlaneTenant` is `""`. Do not give it an explicit string value to make the
type "tidy": that would make the zero value neither plane, and every
construction site would then have to remember. `TestPlanePolarity` asserts the
accident, not the deliberate case, and it is the only test that would catch a
flip. `Mint` still refuses an empty tenant; platform tokens come only from
`MintPlatform`.

**ADR-112 — the application cannot read what it writes.** `ymca_app` holds
INSERT and nothing else on `platform_audit_event`. Never record anything there
the code later needs to query. Note the consequence for testing: an end-to-end
test cannot distinguish which layer refused a request by reading the audit
trail, which is how the `PlatformOnly` drift test came to pass when it should
have failed.

**ADR-113 — provisioning is not a single-row insert.** Tenant, owner person,
owner principal and the fenced `owner` outbox fact commit together or not at
all. A tenant without the owner tuple returns 201 and is administrable by
nobody, silently. The integration test asserts a successful tenant-plane write
by the new owner for exactly that reason; anything weaker passes while the
tenant is inert.

**ADR-115 — the trigger is not `SECURITY DEFINER`, and that is deliberate.**
It reads `authorization_edge` and `organizational_unit`, both under `FORCE ROW
LEVEL SECURITY`. Running as a superuser definer would exempt those reads from
the tenant policy, so the walk would traverse other tenants' edges to decide
this tenant's invariant — ADR-108's principle, in a place it is easy to lose
while "fixing" a permissions error. The recursion's own `depth <= max_depth`
bound is load-bearing too: an unbounded recursive CTE over a graph that
already holds a cycle does not terminate, and a guard that hangs is worse than
one that refuses. And the constraint NAMES are a contract with Go — renaming
one compiles, vets clean, and silently turns every refusal into a 500.

**ADR-116 — the exclusion constraint is the once-only guarantee.** Not the
idempotency key (two keys are two requests) and not a check-then-insert (both
pass). `consumption_period_close` excludes overlapping periods per type and
person; recording, correcting and closing all take `pg_advisory_xact_lock` on
`(type, subject)`, the close in sorted order. A closed period stays closed.

**ADR-117 — one call site, deliberately.** `handleClosePeriod` calls Finance
inside the closing transaction and is the only place the two contexts meet, so
B6's event bus replaces one line. All or nothing: a person with units and no
rate rolls the whole close back, because an unpriced close would mark the period
closed and forbid closing it again.

**ADR-118 — two layers, and they shadow each other.** Suspend and terminate
delete `membership#covered covered_member plan` synchronously (8.3's path:
lock, mark, void, delete, roll back on failure); a term that ended is refused at
decision time, for the person route on entitlement permissions only. A
principal's authority and a payer's right to read their bills do not depend on
membership. Assert revocation at the graph, not through the API.

**ADR-119 — three functions are the only place a day is derived.**
`tenant_today`, `tenant_day_start` and `tenant_utc_offset_seconds` (migration
0009). A new query that needs "today" calls them; one that writes
`now() AT TIME ZONE 'UTC'` or `current_date` reintroduces the bug the ADR fixed
and no existing test will say so unless it is run in two zones. Stored dates keep
A3.10's representation (a UTC midnight where a timestamp is needed); only the
question of which day it is _now_ moved. A tenant that does not exist has no day
(NULL), so a term check fails closed. The zone has no endpoint to change it.

**ADR-120 — the balance is derived, and `VOIDED` is reached only by a credit
note.** `balance = total - payments - credit notes`, computed on every read, and
negative means a refund is due; nothing moves that money. A payment is measured
against what the invoice asks for _after_ its credits, so `RecordPayment` and
`IssueCreditNote` both lock the invoice row. The credit number is drawn last, as
ADR-103 requires, from its own series (`kind = CREDIT_NOTE`).

**ADR-121 — admission share-locks the plan row.** `FOR SHARE OF p` in `Admit`'s
`INSERT ... SELECT` is what stops a retire overtaking an admission in flight; it
looks removable and no test would notice (it is not tested under concurrency).
Moving an `ACTIVE` member runs ADR-118's revocation path for the plan they leave:
`Void` first (it takes the fence lock), then the state change, then the
synchronous delete, then the grant for the new plan.

**ADR-122 — a repair is an authorization change.** The default is to report.
`UNEXPECTED` repairs revoke on the synchronous path and roll the tenant's
transaction back if the delete fails; `MISSING` repairs enqueue, so they lag a
dispatch like any grant. A grant already pending is not drift.

**ADR-108 — three silent ways to have no isolation.** No `FORCE` (the table
owner is exempt), superuser, or `BYPASSRLS`. None produces an error. Verify
per environment with four queries rather than assuming; `backend/README.md`
records the check that was actually run.

---

## 6. The slice

`REVIEW.md` §5 puts this at step 8.

**Scope, as Joe chose it:** entry to bill, end to end — tenant, person,
membership, consumption, finance — plus one permission-gated Flutter app
serving both admin and member. Full Finance context, not a computed number.
OpenFGA from day one. Greenfield `backend/`; `prototype/` is reference only.

```txt
8.1   migrations + FGA model + assertions             DONE  56a671e, 2e080a1
8.2   the auth port, with a development implementation DONE  008dfef, 6386998
8.2.5 role assignment, and 6.1 step 2                DONE  8f4d4f8, 3570e85
8.3.5 the platform plane, and the bootstrap          DONE  4681323, 0c1e1eb
8.3   entry to bill — the A3.7 endpoints             DONE
      foundations   idempotency, outbox, paging      DONE
      configuration bundles, entitles, plans, types  DONE  234d568
      people + units + the A2.2 invariants          DONE  d3be0e8, e16aaf1
      admission (ADR-107 covered_member)            DONE  fd28c63, e1fbbd6
      consumption   records, corrections, absences, close   DONE  7bec2d7
      finance       rates, invoices, issue, payments        DONE  7bec2d7
      me            /me/consumption, /me/invoices           DONE  7bec2d7
      the app's reads  directory, /me/consumption-types     DONE  42d9718
8.3+  membership lifecycle, synchronous revocation   DONE  1c57c37 (ADR-118)
8.3+  the first operator, and CORS                   DONE  6cd409b
8.4   the Flutter app                                DONE  7a01600, d6d92c7, c3c36f3
7.5   CI                                             WRITTEN, NEVER RUN  59f53f2, and session 9
9.1   bootstrap-principal                            DONE
9.2   tenant time zone (ADR-119)                     DONE  migration 0009, /me, app calendar
9.3   credit notes (ADR-120)                         DONE  migration 0010, app dialog
9.4   plan lifecycle + moving a holder (ADR-121)     DONE  migration 0011, API only
9.5   reconcile-coverage (ADR-122)                   DONE  membership coverage only
```

### The backend, as built

`compose.yaml` and `Dockerfile` were not created: the containers already run
(§4), and there is nothing to deploy yet.

```txt
backend/
  cmd/migrate/                goose, embedded; refuses down/reset/redo
  cmd/fga/                    apply | test
  cmd/api/                    the HTTP server and its subcommands
    main.go                   wiring; registerRoutes (the tests mount the SAME routes)
    handlers.go               the 6.1 check, checkObject, A3.4's errors, runIdempotent
    me.go                     GET /me — seven permission candidates
    membership_handlers.go    admission, the directory, suspend/reinstate/terminate
    consumption_handlers.go   A3.10 — and the one place Consumption meets Finance
    finance_handlers.go       A3.11
    bootstrap.go              bootstrap-operator (NOT behind the dev tag)
    bootstrap_principal.go    bootstrap-principal <person-id> <idp-subject>
    reconcile.go              reconcile-coverage [--repair]
    devtoken.go               mint-token — //go:build dev
  migrations/                 0001 schema, 0002 roles, 0003 idempotency,
                              0004 platform, 0005 DAG invariants, 0006 consumption,
                              0007 finance, 0008 membership lifecycle,
                              0009 tenant time zone, 0010 credit notes,
                              0011 plan lifecycle
  fga/                        model.fga (extracted from A1.2, byte-checked),
                              assertions.yaml (47), sync_test.go, grantable_test.go
  internal/
    config/ db/ auth/ auth/dev/ audit/ idempotency/ outbox/ page/
    authz/        6.1's steps, AlsoAsPerson, and the decision-time term check
    httpx/        the middleware chain, A3.4's errors, CORS (closed by default)
    organization/ tenants + first owner; units + the DAG (ADR-113, 115)
    identity/     people — global rows, tenant-gated (ADR-114)
    membership/   bundles, plans, admission, the directory, lifecycle.go (ADR-118)
    consumption/  types, options, obligations, records, absences, close
    finance/      rates, compose, invoices, issue (ADR-103), payments
app/
  lib/api/        models, the YmcaApi interface, the HTTP implementation
  lib/state/      the session and what /me says it may do
  lib/ui/         shell, sign-in, meals, bills, members (+ actions), admission,
                  invoices, close, invoice detail; support.dart is the one way to
                  fail and the one way to page
  test/           widget tests against a fake; live_api_test.dart is the Dart
                  half of the cross-language contract test
.github/workflows/ci.yml   docs, build/vet/format, model, integration x2, app
```

**Three things about 8.2 that are decisions, not implementation detail.**

`internal/authz/validity.go` keeps 6.1 step 3 as four separately named
functions, three of which return valid and say in a comment why they cannot
do more. That is deliberate: an unbuilt limb should be visible in the code as
unbuilt. Collapsing them into one query would have hidden two real gaps.

`TenantMatch` is applied per route, not around the mux, because `ServeMux`
only populates `r.PathValue` while resolving a specific route — a middleware
wrapping the mux sees an empty tenant. `Authenticate`, by contrast, wraps the
whole mux, so an unauthenticated request to an unknown path is 401 before it
is 404. Both are 8.8's non-disclosure rule, not accidents of ordering.

`db.Pool()` exists only for the four tables 8.2 exempts from RLS. Every other
query goes through `InTenantTx`. A new caller reaching for `Pool()` is almost
certainly a defect.

**What 8.2.5 changed, and why it was not optional.**

6.1 step 3 could never have been written as specified, and this is worth
understanding before touching `internal/authz`. `Check` returns a **boolean,
not a path**. A validity step running after the graph therefore cannot tell
which route produced an ALLOW. For a principal holding both a lapsed role and
a direct grant, "does this principal hold an expired assignment?" denies
wrongly and "is any assignment effective?" allows wrongly — and there is no
third question available. That is why 8.2's `checkTermWindow` and
`checkClearance` shipped as named no-ops. The defect was in the shape of the
check, not the code.

ADR-109 reorders it. PostgreSQL resolves the assignments granting the one
permission being checked — term window, clearance, ACTING cover and the
role-suppressing restrictions all in the same `WHERE` clause — and supplies
the survivors to OpenFGA as **contextual tuples**. An ineffective assignment
is not in the input, so no path through it exists to be found. Nothing is
projected, nothing is stored, no outbox row and no sweeper.

```txt
1  TENANT     ancestry
2  ROLE       PostgreSQL, before the graph      <- new, and the ordering
3  GRAPH      OpenFGA + step 2's tuples            is the whole mechanism
4  VALIDITY   principal/person status, restriction
```

**Do not move step 2 after step 3.** It looks like a tidy-up. It reintroduces
the exact defect above, and the tests that would catch it are the integration
ones, not the unit ones.

**`role_assignment` lost `definition` and `scope`** from the FGA model. They
were declared and read by nothing — the same defect class as `via_plan` in R6,
still present after that round closed. A1.6 even wrote three tuples for "Alice
is Branch Secretary" that resolved to nothing.

**`tenant.may_approve_membership` is new.** 05.3, 05.6.7 and 6.2 all check
`membership.approve` and A1.2 declared no such relation. 8.3's first endpoint
had no permission to check.

**The grantable set is enforced twice, deliberately.** A1.2's type
restrictions decide it; migration 0002 seeds the same set into
`grantable_permission` so a foreign key can enforce it in PostgreSQL.
`TestGrantableSetMatchesMigration` fails in both directions. The dangerous
direction is silent: a permission seeded but not modelled becomes grantable in
the database and unresolvable in the graph, so a role confers something that
never works and nothing raises.

**What the units work established, and the finding that reshaped it.**

`POST /t/{t}/units` is the first endpoint that can insert an
`authorization_edge`. R10 specified that it run A2.2's depth-12 recursive CTE
before committing — and building it showed **a cycle is unreachable from that
endpoint by construction**, because nothing points at a unit that did not
exist a moment ago. Of ADR-016's three invariants, only the depth bound can be
violated there. A check living in that handler could never have been observed
to fail, which is indistinguishable from one that cannot.

ADR-115 moves all three into a trigger (migration 0005). Two consequences to
know before touching it:

```txt
edges are written straight at the table by the tests, deliberately —
  going through CreateUnit would test its arithmetic, not the invariant
the endpoint does NOT re-derive the walk; it maps the trigger's
  check_violation to a domain error BY CONSTRAINT NAME
```

**Creating two levels at once needs a dispatch between them.** The new unit's
`auth_parent` tuple is a grant, so it reaches OpenFGA only when the dispatcher
runs; until then a request to create a child beneath it is refused. That is
ADR-101's asymmetry, not a race, and every test that builds a hierarchy has to
dispatch between levels.

**The three containments stay apart.** `org_parent_id` is accepted and carries
no authorization meaning; creating the edge must never back-fill it, and the
integration test asserts that it does not. 05.1.3 calls this the invariant
implementers will most want to relax.

**What 8.3's configuration established, and the finding that reshaped it.**

A3.7 had no endpoint for an entitlement bundle, a membership plan, or the link
between them — while `membership.plan_id` is `NOT NULL`, ADR-107's
`covered_member` tuple needs a plan to point at, and
`consumption_type.may_record` resolves through `entitled` from a bundle. **The
hostel case could not have worked**, and nothing in the record said so. Joe
chose minimal create+list endpoints in A3.7 over a CLI (B7) or seed fixtures.

The membership number is **caller-supplied**, with `UNIQUE (tenant_id, number)`
as the whole mechanism. Deliberately not ADR-103's counter — gapless
allocation exists to satisfy a tax authority, and a membership register has no
such rule. Do not "fix" this by adding a counter.

**Three orderings are load-bearing and look like tidy-ups.** Each one, moved,
reintroduces a defect the tests would catch only at integration level:

```txt
step 2 BEFORE the graph      ADR-109; Check returns a boolean, not a path
the advisory lock BEFORE     ADR-101; the row lock alone does not close
  the outbox insert            the grant-in-flight race
the idempotency row INSIDE   A3.6; outside it, a retry replays a response
  the work's transaction       for work that was rolled back
```

**What admission established, and the finding that reshaped it.**

`POST /t/{t}/memberships` is the endpoint the whole configuration chain
exists for, and the first one to use A3.6's mechanism — `runIdempotent` in
`cmd/api/handlers.go` holds that flow now, for consumption and finance to
reuse. It is also the first code to depend on the third load-bearing
ordering above: the key row is recorded INSIDE the work's transaction, so a
failed admission leaves nothing to replay and the same key is free to carry
different work. There is a test for exactly that.

```txt
three facts, not one     each is separately revocable — a suspension lifts
                         coverage while the membership and holder remain
may_approve_membership   not `admin`. ADR-110 made it role-grantable so a
                         front desk can admit without administering the
                         association
the plan must be OPEN    read in the statement that inserts the row, so a
                         plan closing between check and insert cannot race
two 409s, not one        a taken number and an existing active membership
                         need different client behaviour
```

The finding was a drift test that PASSED. Removing the `Idempotency-Key`
requirement left the whole suite green, because the zero-valued key that
resulted raised `22P02` and came back as the same 400 the test expected.
`runIdempotent` now refuses a zero key with a logged 500 — blaming the client
for a server mistake was the actual defect — and the test asserts the message
names the header. §3 carries the rule that came out of it.

**A malformed uuid used to be a 500 on every endpoint.** `22P02` fell through
`writeDomainError`'s default. It is now `invalid_request`, in one place, for
all of them — the one cross-cutting change this round made, and it was made
centrally rather than in admission alone so that two endpoints cannot answer
differently for the same mistake.

**A1.2 and `fga/model.fga` are no longer kept in sync by discipline.** A1.8
gained rule 7 and `go test ./fga/` enforces it: the model must equal the A1.2
fence byte for byte, and every assertion A1.7 promises must actually run with
the expectation A1.7 gives it. The guard was checked against real drift — an
edited model, a deleted assertion, a flipped expectation — not merely observed
to pass.

It exists because A1.8's other six rules were all obeyed and three defects
shipped anyway: A1.2 did not parse, A1.6 could not satisfy its own first
assertion, and `entitlement_bundle.via_plan` was declared and read by nothing.
All three are closed. The model had been reviewed line by line by a human and
never once fed to a parser.

---

## 7. What is still open

Ordered as `REVIEW.md` §5 orders it: after the slice comes
`A2 -> B4 -> B5 -> B6 -> B7 -> B8 -> C*`. The slice is done, so this is the
order of the next work.

| Ref | Open item                                                                                                                                                                                |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A2  | Cached ALLOW (6.9) contradicts synchronous revocation (8.3). **Sharper now:** ADR-118's revocation and ADR-109's per-check roles both die the moment a decision is cached                |
| B4  | Notification — required by five sections, owned by nobody. C8 is regulatory                                                                                                              |
| B5  | Tenant onboarding and bulk import; conflicts with gapless invoice numbering                                                                                                              |
| B6  | Inter-context event bus — 05.0.8 assumes one. **ADR-117 and the terminate handler are the two call sites it would replace**                                                              |
| B7  | Operator/admin CLI. Three commands now (`bootstrap-operator`, `bootstrap-principal`, `reconcile-coverage`); configuration still goes through the API                                     |
| C1  | **Narrowed, not closed.** `reconcile-coverage` compares a membership's three tuples. Units, plans, bundles, invoices and tuples whose row is gone are not compared; nothing schedules it |
| C2  | `may_review_compliance` produces nothing defined                                                                                                                                         |
| C3  | No scale figures                                                                                                                                                                         |
| C4  | No observability beyond audit — including no dispatcher metrics                                                                                                                          |
| C5  | No rate limiting or abuse model                                                                                                                                                          |

**Closed in session 9:** a login for a registered person (a command, not an
endpoint), the tenant time zone, credit notes and the `VOIDED` state, plan
lifecycle (B8) and moving a holder, the invoice list's missing balance, a
directory that says when a term has ended, and a reconciler for membership
coverage.

**Closed in session 8:** consumption, finance, `/me`, the Flutter app, CI as a
file, the first operator, CORS, and the largest gap session 7 opened — nothing
revoked a membership's tuples. Admission, consumption and finance each now have
a writer for every relation they declare.

**Opened in session 9**, each deliberate and written in `11.2`:

- **A tenant's time zone cannot be changed**, only set at provisioning.
- **No refunds.** A credit note after payment leaves a negative balance and
  nothing moves the money back.
- **Plan lifecycle and moving a holder are API only**; the app has no plan
  screen, and there is no "move everyone on plan A".
- **The reconciler is membership-only and unscheduled.**
- **The `FOR SHARE` on admission and the invoice row lock are not tested under
  concurrency.** They are the mechanism; no test races them.
- **The Dart date pickers' fix (`pickedDay`) is unit-tested, not driven through
  a picker**, and a test machine in UTC would not discriminate it anyway.

**Opened this session**, all in `11.2`, each a thing the code deliberately does
not do rather than a thing nobody noticed:

- **A login is made by a command, and the identity-provider adapter does not
  exist.** `bootstrap-principal` binds a subject to a person; `POST /people`
  still creates no principal. **A real identity provider (Cognito, Supabase or
  Appwrite behind ADR-106's port) is what stands between the platform and a
  first real tenant**, and it is a choice for Joe — nothing in the record
  chooses one.
- **A new object is unreadable until the dispatcher runs** (ADR-101's lag): an
  administrator who defines a type and immediately adds an option, or closes a
  period and immediately opens the invoice, gets a 403 for a few seconds.
- **Nothing sets a membership `LAPSED` and nothing renews one.** A term that
  ends is enforced at decision time and the state stays `ACTIVE`; the directory
  now reports `in_term` and the app labels such a member TERM ENDED, but no
  renewal workflow exists.
- **Suspension and termination need `tenant:admin`**; a bursar who issues
  arrears suspensions needs a finer relation (ADR-110's three places).
- **Credit notes exist (ADR-120); refunds do not.** A correction inside a closed
  period is now done with a credit note.
- **Consumption:** no variant catalogue (a variant is a label, a surcharge is an
  option with the same code); option prices are live, not snapshotted; no
  self-correction; the subject's `NO_RESOURCE_ACCESS` restriction is not checked
  when staff record for them; obligations only from a membership (no `STAY`, no
  `SUBSCRIPTION`); an absence's payment-relief threshold is a platform default of
  seven days until 05.7's policy engine exists.
- **Finance:** one rate per period (a rate change mid-period does not split the
  close), no tax, an April financial year for every tenant, no instalments.
- **A warden who may record for others cannot search the member directory**
  (`may_approve_membership` gates it).
- **The app stores its token in preferences.** A real provider's token belongs
  in secure storage; there is no iOS or macOS target; the web build needs its
  origin in `YMCA_CORS_ORIGINS`.

Carried from earlier sessions and still true, abridged — the full list is
`11.2`: twelve tables carry no `tenant_id`; a unit gets one authorization parent
through the API; the DAG depth limit is a constant; three edge types are refused
rather than validated; `location` has no endpoint; no office or committee
appointment workflow; no reverse role query; nothing sweeps `idempotency_key`;
platform-plane POSTs are outside the idempotency mechanism;
`platform_audit_event` is written and read by nothing; membership number formats
are unvalidated; dependents are unreachable; the application workflow
(05.3.4) is bypassed; approval authority is tenant-scoped only; `06_runtime_view`
has no consumption scenario and `10_quality_requirements` no consumption quality
scenario.

**One unvalidated assumption is carried on purpose:** that no national or world
body in the movement holds member rosters. Joe did not know either way, and
ADR-102 takes the safest reading. If rosters do flow upward,
`may_read_member_data` needs a narrow real path and ADR-102 must be revisited.

**The standing top risk (R1) came out the way the largest option said it
might**: a solo build of ten contexts, an authorization model with CI
assertions and sixteen synchronous revocation paths, for a customer already
waiting — and the slice is finished. What it cost is the shape to keep
watching. Eight rounds in, every one found the record incomplete by trying to
build it; this one found it _contradictory_ (05.3.10 against 5.3.9 and 8.3),
which is a worse and rarer kind. Of 8.3's synchronous
revocation list, four are covered — role assignment, clearance and restriction
resolve in PostgreSQL at decision time (ADR-109), and membership suspended or
terminated now runs the delete path (ADR-118) — and ten have no endpoint to need
one: office vacated, dependent removed, subscription lapsed, authority verb
revoked, affiliation sanctioned, cross-tenant grant expired, guardianship ended,
consent withdrawn, principal suspended, committee membership removed. Each will
have to run `lifecycle.go`'s shape: void, mark, delete, roll back on failure.

---

## 8. Immediate next actions

```txt
1  Open a pull request from `session-9` and watch CI — that is the first real
     run. (`session-9` is pushed; nothing is on `main`.) Three things in the workflow are still unverified on GitHub: the
     openfga container (in-memory datastore), the `ymca_api` role creation after
     migration 0001, and the Flutter install the contract test depends on
     (YMCA_REQUIRE_FLUTTER makes it fail, not skip). Session 9 ran every one of
     those steps' commands locally on the same versions, and added three steps:
     no tenant left behind, `reconcile-coverage`, and a 20-minute test timeout.
2  Choose an identity provider (ADR-106's port) and write its adapter. Until
     then `bootstrap-principal` is how a member gets a login. This is the gate
     to a first real tenant, and it is Joe's choice.
3  Rotate or drop the throwaway ymca_api dev password (§4) — on Joe's machine;
     the container used for session 9 had its own, discarded with it.
4  Refunds. A credit note after payment leaves a negative balance and nothing
     moves the money; a refund is a payment in reverse (05.8.6) and wants its
     own state, endpoint and reconciliation.
5  A plan screen in the app, and a way to change a tenant's time zone that an
     operator can audit (ADR-119's consequences say why it is not a setting).
6  Extend the reconciler beyond membership coverage, and schedule it.
7  A finer relation than tenant:admin for suspending (ADR-110's three places):
     a bursar who issues arrears suspensions should not need to administer.
8  Optional, 30 seconds: 16 orphaned DISPATCHED outbox rows from sessions 5 and
     6 on Joe's database. Harmless; ask Joe first.
```

**Before touching `internal/authz`**, read the three load-bearing orderings in
§6 and ADR-118's two layers — each looks like a tidy-up and each, moved,
reintroduces a real defect. **Before touching the platform plane**, read
ADR-111's polarity note. **Before touching `authorization_edge` or migration
0005**, read ADR-115's note. **Before touching admission, `runIdempotent`, or
any screen that sends an `Idempotency-Key`**, read §6's admission block and the
keys paragraph in `app/README.md`: a key belongs to the request, and the same key
with a different body is a refusal. **Before touching membership lifecycle**,
read `lifecycle.go`'s header: Void comes before the state change because it takes
the fence lock, and revocation is asserted at the graph.

**Before adding a query that needs "today"** (ADR-119), call `tenant_today`;
never `now() AT TIME ZONE 'UTC'` or `current_date`. **Before touching plan
admission or `MovePlan`** (ADR-121), read the share lock in `Admit` and the
order in `MovePlan`: Void, state change, synchronous delete, then the new grant.
**Before adding a tenant-scoped table**, add its delete to the harness `purge`,
or the next run fails on a tenant it cannot remove.

**Before adding any permission a role should confer** (ADR-110): three places —
A1.2's type restriction, a migration's `grantable_permission` seed, and an
assertion naming an object the admin does NOT reach by another route.
`TestGrantableSetMatchesMigration` fails until the first two agree.
