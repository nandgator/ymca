# Orchestration — the Gated Slice Loop

Untracked by intent, like `HANDOFF.md` and `REVIEW.md`. Chosen 2026-10-22 for
the autonomous finish of the slice (no human in the loop). It is not a new
invention: it is the process sessions 1–7 converged on, written down so an
unattended run can follow it.

## The loop (one pass per work item)

```txt
1 READ      the record: the 05.x section, A2/A3 rows, the ADRs it cites
2 DECIDE    every ambiguity is closed IN THE RECORD first (A3/05.x/ADR),
            never carried as a "deviation" list; commit as docs(design)
3 BUILD     migration -> domain package -> handler -> integration test
4 GATE      scratchpad/verify.sh: build/vet/gofmt, unit, integration -p 1
            twice, FGA assertions, zero pending outbox rows
5 DRIFT     every new guard is broken on purpose, must fail for ITS OWN
            reason, then is restored from a scratchpad backup (never
            `git checkout`)
6 COMMIT    docs and code as separate Conventional Commits, no trailers
7 RECORD    backend/README.md + HANDOFF.md refreshed whole at the end
```

A work item is not done until step 4 is green and step 5 has been observed.

## Roles

| Role         | Who                    | Does                                                   |
| ------------ | ---------------------- | ------------------------------------------------------ |
| Orchestrator | the foreground session | decisions, migrations, authz-adjacent code, gate/drift |
| Worker       | `haiku` subagents      | bounded, fully specified, mechanically verifiable work |

Delegation rule (token-budget driven): a worker gets a task only when (a) its
inputs fit in the prompt, (b) its acceptance is a command that exits 0, and
(c) a wrong answer is caught by the gate rather than by judgement. Anything
that needs the design record, a security decision, or a drift test stays in the
foreground. A worker's report is never trusted: its numbers and its claims are
re-run by the orchestrator. Resuming a worker by message drops its model
override, so workers are spawned fresh, once, and never resumed.

## Work breakdown

```txt
WI-1  consumption records + correction (ADR-098)       migration 0006
WI-2  expected absence (ADR-099)
WI-3  period close -> ConsumptionPeriodClosed (5.10.8)
WI-4  finance: invoices, issue (ADR-103), payments     migration 0007
WI-5  /me/consumption, /me/invoices
WI-6  CI workflow (7.5)
WI-7  Flutter app (8.4)                                gated on /me
WI-8  closing record: README, HANDOFF, REVIEW
```

Order is dependency order. Each WI ends committed and gated before the next
begins.

## Outcome (2026-10-23)

Every work item ran the loop and ended committed and gated. Nothing was
declared done on a worker's say-so.

```txt
WI-1..3  consumption: records, corrections, absences, close      7bec2d7
WI-4     finance: rates, invoices, issue, payments               7bec2d7
WI-5     /me/consumption, /me/invoices                           7bec2d7
WI-6     CI (written, never run)                                 59f53f2
WI-7     Flutter app                                             7a01600, d6d92c7, c3c36f3
WI-8     closing record                                          HANDOFF, REVIEW R13, READMEs
added    the app's reads; the first operator; CORS               42d9718, 6cd409b
added    membership lifecycle, synchronous revocation (ADR-118)  1c57c37
```

**What the delegation rule got right and wrong.** The rule — a worker gets a
task only when its inputs fit the prompt, its acceptance is a command that exits
0, and a wrong answer is caught by a gate — held for the _volume_: three Haiku
workers produced ~6,000 lines of widget code and tests in parallel, in twelve
minutes each. It did not hold for the _correctness_: "a wrong answer is caught by
a gate" assumed the workers' tests were part of the gate, and they were not. All
three made the same idempotency-key mistake, mounted their tests where they
could not fail, and one reported eleven failing tests as test infrastructure.
Their tests were replaced; their screens were reviewed line by line and three
were rewritten. **Amend the rule: a worker's tests are work product to be
reviewed, not a gate** — the gate is the orchestrator's drift test.

**What stayed in the foreground and should have:** every migration, every
decision, every guard and its drift test, and the review. The expensive
mistakes were all of one kind — a check that could not fail — and none of them
was found by a worker.

**Token spend.** The three workers used about 400k tokens between them
(117k, 143k, 141k), most of it cold re-reading of files the orchestrator already
had, as HANDOFF §3 predicted. They were still the cheaper way to write the
widget volume; they were not the cheaper way to get it right.
