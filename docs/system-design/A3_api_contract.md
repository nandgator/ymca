# A3 — API contract

The minimum the slice needs. Not a full surface for ten contexts — only the
rules every endpoint obeys, and the endpoints entry-to-bill requires.

Written before the handlers because 8.2 makes explicit tenant carriage
load-bearing in this layer, and because a rule implemented three different
ways is no longer a rule.

---

## A3.1 Shape

```txt
/api/v1/t/{tenant}/...     tenant plane — always names its tenant
/api/v1/platform/...       platform plane — never names one
```

The token also names a tenant. The two are compared before routing and a
mismatch is rejected (ADR-105). A tenant-plane route with no `{tenant}` does
not exist; a platform-plane route reaching a tenant object is a defect
(invariant 9, 8.12).

**The converse holds too, and nothing stated it until now.** A tenant-bound
credential must not reach a platform-plane route, and a platform credential
must not satisfy the tenant-plane match. Both are refused at the edge, before
a handler runs:

```txt
tenant credential   -> platform route     refused: PlatformOnly gate fails
platform credential -> tenant route       refused: TenantMatch never matches
                                           a plane the token did not name
```

`PlatformOnly` and `TenantMatch` are mutually exclusive gates over the same
credential (ADR-111): a request satisfies exactly one, never both and never
neither.

---

## A3.2 Authentication

```txt
Authorization: Bearer <token>
```

Resolved through the port of ADR-106 to:

```txt
principal_id      the subject of every tuple
principal_kind    PERSONAL | STAFF | ELEVATED
plane             PLATFORM, or absent for the tenant plane
tenant            required when plane is absent; must be ABSENT
                  when plane is PLATFORM
expires_at        checked at decision time, never by a sweeper
```

A person acting as themselves, as staff, and with elevated authority are
three different principals, and therefore three different subjects (05.2.3).
Nothing in the API lets one request carry more than one.

**The polarity of `plane` is the load-bearing part, not its presence**
(ADR-111). The zero value of a `Principal` — every field left at its Go
default — must be the tenant plane, never the platform plane. A `Principal`
constructed without thinking about it, in a test fixture, a partially-filled
struct literal, or a provider that forgot to set the field, must come out
ordinary and tenant-bound rather than holding platform authority nobody
granted it. This is the same argument as ADR-106's `dev` build tag: the
accidental case must be the safe one, never the dangerous one.

---

## A3.3 Authorization

Every handler performs the check of 6.1 before doing anything else. Lists
check their scope once (ADR-104); writes check the object they write.

```txt
403   the check denied
404   the object does not exist, OR the caller may not know that it does
```

The two are deliberately indistinguishable from outside. 11.3 already requires
this for duplicate detection; it applies to every object whose existence is
itself information.

---

## A3.4 Errors

```json
{ "error": { "code": "consumption_window_closed", "message": "..." } }
```

`code` is stable and machine-readable; `message` is for a developer, never
rendered to a member. No stack traces, no SQL, no object identifiers the
caller could not already name.

The codes every endpoint may emit, and their status:

```txt
400  invalid_request    the request could not be read as one
401  unauthenticated    no credential, or one that will not be accepted
401  token_expired      the credential was valid and no longer is
403  tenant_mismatch    the path names a tenant the token does not
403  forbidden          the check of 6.1 denied
404  not_found          absent, or the caller may not know it exists (A3.3)
409  idempotency_key_reused  same key, different request body (A3.6)
409  membership_number_taken the number is another membership's (A3.7)
409  already_active_member   that person is already a member here (A3.7)
409  already_recorded        a live record exists for that person, type, day (A3.10)
409  record_already_corrected the record already has a correction (A3.10)
409  consumption_window_closed the correction window has closed (A3.10)
409  period_closed           the day lies in a period already closed (A3.10)
409  period_already_closed   that period overlaps one already closed (A3.10)
409  obligation_overlaps     an obligation of that type already covers those days
409  option_code_taken       that option code is already used on the type
409  rate_overlaps           a rate already covers those days (A3.11)
409  no_rate                 obligated units but no rate in force (A3.11)
409  currency_mismatch       two currencies where one is required (A3.11)
409  invoice_wrong_state     the invoice is not in the state this needs
409  invoice_empty           nothing to issue
409  payment_exceeds_balance more than is owed
409  credit_exceeds_invoice  more than the invoice still asks for (ADR-120)
409  membership_wrong_state  the membership is not in the state this needs (A3.13)
409  plan_wrong_state        the plan is not in the state this needs (ADR-121)
409  plan_has_holders        members still hold the plan (ADR-121)
422  record_mode_not_allowed the type does not take this kind of recording
500  internal           anything the caller cannot act on
```

**Admission's two collisions are two codes, not one `conflict`.** The message
is for a developer, so a client must not parse it — and the two call for
different behaviour: a taken number means choose another, while an existing
active membership means the membership already exists and is the thing to
show. Collapsing them would put that distinction somewhere no client may read
it.

`invalid_request` covers a body that is not the JSON the endpoint documents, a
`limit` outside its range, and a `cursor` that does not decode. It is the one
code whose `message` may name the offending field, because the caller supplied
it and telling them discloses nothing they did not already send.

**A malformed cursor is refused, not ignored.** The tempting alternative —
treat it as absent and start from the beginning — silently returns the wrong
page, and a pager that silently restarts is worse than one that stops. This is
the opposite of the rule for `X-Request-Id` above, and the difference is that a
correlation id does not change the answer.

`unauthenticated` collapses every reason a credential was refused — missing,
malformed, bad signature, unknown subject, suspended principal — into one
answer, per 8.8. `token_expired` is the single exception, and exists only
because a client reacts to it differently, by refreshing.

`tenant_mismatch` is 403 rather than 401: the caller authenticated
successfully, and refusing a tenant the caller named themselves discloses
nothing (ADR-105). Falling back to 401 would instead tell them their
credential was the problem, which it was not.

### Request correlation

```txt
X-Request-Id: <token>
```

Accepted on any request and always returned. A well-formed incoming value is
honoured; anything else — absent, over-long, or carrying whitespace or
control characters — is replaced by a server-generated one rather than
rejected, since a bad correlation id is not worth failing a request over. The
value reaching the server is what appears in the structured log and in
`audit_event.context` (8.5), so a caller can find their own request in the
trail.

---

## A3.5 Pagination

Keyset, per 8.11.

```txt
GET  ...?limit=50&cursor=<opaque>
     → { "items": [...], "next_cursor": "<opaque>" | null }
```

`limit` has a server maximum. `cursor` is opaque and encodes the sort key —
clients must not construct or parse one.

---

## A3.6 Idempotency

```txt
Idempotency-Key: <client-generated>
```

Required on every POST that creates money or a consumption record, and on
admission. The key is stored with the result; a repeat returns the original
response rather than creating a second row.

A warden on hostel wifi will retry. Without this, a retried meal is a second
meal and a retried payment is a second payment.

**Admission is here for the same reason, not by category.** It creates
neither money nor a consumption record, and `UNIQUE (tenant_id, number)`
already prevents a second membership — so the mechanism buys no protection
against duplicate state. What it buys is an answer: without a key, a retry
after a lost 201 gets `membership_number_taken`, and the caller cannot tell
its own earlier success from somebody else's collision. With one it gets the
original 201 and the membership id. The scope was widened when 8.3 built the
endpoint and found that out, rather than the endpoint quietly requiring a
header this section did not ask for.

The key is stored in `idempotency_key` (A2.10) **inside the transaction that
does the work**, so the effect and the record of it commit together.

A replay is **semantically** the original response, not byte-identical. The
body is stored as `jsonb`, which normalizes insignificant whitespace and may
reorder keys — invisible to any JSON client, and the price of the database
validating what it stores rather than holding opaque text. Nothing in this API
signs or hashes a response body; if anything ever does, this is the decision
it must revisit. A
rolled-back attempt therefore leaves nothing behind, and a retry proceeds
fresh rather than replaying a response for work that never happened.

A key replayed against a **different body** is a client defect and gets its
own code rather than the first response:

```txt
409  idempotency_key_reused   same key, different request
```

Returning the original would silently discard the second request, which is
the failure this header exists to prevent, inverted.

**The mechanism does not cover the platform plane, and this is accepted
rather than fixed.** `idempotency_key.tenant_id` is `NOT NULL REFERENCES
tenant` and the table is under RLS — there is no tenant to key a platform
request by. A retried `POST /platform/tenants` therefore creates a second
tenant, with a second owner principal, and nothing here stops it. This
scope's own rule is "every POST that creates money or a consumption record",
and a tenant is neither, so the gap is a narrowing of scope rather than an
oversight — but it belongs in 11.2 rather than being found the way every
other gap in this design has been found. Recorded there.

---

## A3.7 The slice

```txt
PLATFORM
  POST   /platform/tenants                         create tenant + owner

ORGANIZATION
  POST   /t/{t}/units                              create org unit
  GET    /t/{t}/units/{unit}

CONFIGURATION
  POST   /t/{t}/entitlement-bundles                define a bundle
  GET    /t/{t}/entitlement-bundles
  POST   /t/{t}/entitlement-bundles/{b}/entitles   bundle entitles an object
  POST   /t/{t}/membership-plans                   define a plan
  GET    /t/{t}/membership-plans
  POST   /t/{t}/membership-plans/{p}/close         stop admitting (ADR-121)
  POST   /t/{t}/membership-plans/{p}/reopen
  POST   /t/{t}/membership-plans/{p}/supersede     { successor_plan_id }
  POST   /t/{t}/membership-plans/{p}/retire        only once nobody holds it

IDENTITY AND MEMBERSHIP
  POST   /t/{t}/people                             create person
  GET    /t/{t}/units/{unit}/members               list  — scope: unit
  POST   /t/{t}/memberships                        admit a member
  POST   /t/{t}/memberships/{m}/suspend            A3.13, ADR-118
  POST   /t/{t}/memberships/{m}/reinstate
  POST   /t/{t}/memberships/{m}/terminate
  POST   /t/{t}/memberships/{m}/plan               move to another plan (ADR-121)

CONSUMPTION                                        (detail: A3.10)
  POST   /t/{t}/consumption-types                  define a type
  GET    /t/{t}/consumption-types
  POST   /t/{t}/consumption-types/{ct}/options     define an option
  GET    /t/{t}/consumption-types/{ct}/options
  POST   /t/{t}/consumption-types/{ct}/obligations obligate a member
  POST   /t/{t}/consumption-types/{ct}/records     record use
  POST   /t/{t}/consumption-records/{r}/correction supersede  (ADR-098)
  GET    /t/{t}/consumption-types/{ct}/records     list  — scope: type
  POST   /t/{t}/absences                           declare an absence
  POST   /t/{t}/absences/{a}/withdraw
  POST   /t/{t}/consumption-types/{ct}/close       close a period

FINANCE                                            (detail: A3.11)
  POST   /t/{t}/consumption-types/{ct}/rates       price a unit
  GET    /t/{t}/invoices                           list  — scope: tenant
  GET    /t/{t}/invoices/{inv}
  POST   /t/{t}/invoices/{inv}/issue                allocate number (ADR-103)
  POST   /t/{t}/invoices/{inv}/payments             record a payment
  POST   /t/{t}/invoices/{inv}/credit-notes         correct an issued invoice (ADR-120)

ME                                                 (detail: A3.11, A3.12)
  GET    /t/{t}/memberships?q=                     the member directory
  GET    /t/{t}/me                                 principal, kind, permissions, calendar
  GET    /t/{t}/me/consumption-types               what I may record
  GET    /t/{t}/me/consumption                     own records
  GET    /t/{t}/me/invoices                        own invoices
```

`/me` is what makes one permission-gated app possible: the client renders
from the permissions it is told it has, rather than from a role it guessed.

It also carries the tenant's calendar (ADR-119): `time_zone` (an IANA name),
`today` (the calendar day there, `YYYY-MM-DD`) and `utc_offset_seconds` (the
offset in force now, east positive). A client has no zone database; it turns an
instant into the tenant's day by adding the offset.

### Platform provisioning creates a tenant AND its first owner

```txt
POST /platform/tenants
{ "legal_name": ..., "display_name": ..., "jurisdiction": ...,
  "time_zone": "Asia/Kolkata",           // optional, an IANA region; UTC if absent
  "owner": { "display_name": ..., "idp_subject": ... } }

creates   tenant row
          person row          the owner, globally
          principal row       kind STAFF — the owner acts for the
                              association, not as themselves (05.2.3)
publishes principal:<p> owner tenant:<t>   through the outbox, fenced
```

One transaction, or the tenant is inert (ADR-113). Without an owner
principal nothing can create a unit, a plan or a person in the new tenant —
a `tenant` row with no path to `owner` is a tenant the API can never again
reach into, since every write past this point checks `admin` or something
that resolves through it.

The owner's principal is `STAFF`, not `PERSONAL`: they act for the
association from the moment the tenant exists, before any person of theirs
has logged in as themselves. This is the same distinction ADR-106 draws
between a support engineer's routine booking and their operational
authority — here, drawn at the very first principal a tenant ever has.

**Why one transaction, not a saga.** `tenant`, `person`, `principal` and
`authorization_outbox` all carry no `tenant_id` and none is under RLS — there
is no tenant context to run inside, because the tenant is what is being
born. So the whole bootstrap runs in one plain pool connection's transaction,
with no cross-service step to compensate if it fails partway. The `owner`
tuple is written to the outbox in the same transaction as the rows it
describes, fenced exactly as every other authorization fact is (ADR-101).

### Organization endpoints, and the permissions nothing had named

```txt
POST /t/{t}/units         check: admin on the named parent (tenant or unit)
GET  /t/{t}/units/{unit}  check: member on the unit
```

```txt
POST /t/{t}/units
{ "type": "CHAPTER", "name": ...,
  "parent": { "type": "tenant" | "organizational_unit", "id": ... },
  "org_parent_id": ...            optional, and NOT the authorization edge }

creates   organizational_unit row
          authorization_edge row   child = the new unit, parent = `parent`
publishes tenant:<t>       tenant       organizational_unit:<u>
          <parent>         auth_parent  organizational_unit:<u>
```

`parent` is required and there is exactly one of it. ADR-016 permits many
authorization parents and this endpoint creates the first; a second is added
by an endpoint that does not exist in this slice (11.2). `org_parent_id` is
the ORGANIZATIONAL parent and has no authorization meaning whatever — 05.1.3
keeps the three containments apart, and passing one does not create an edge.

`location_id` is not accepted. `location` has no endpoint in A3.7, so
nothing could supply an id that exists; a field that can only ever be null
would read as a capability the API does not have.

The three refusals a caller can provoke, all from migration 0005's trigger
(ADR-115), all `invalid_request` except where noted:

```txt
parent is another tenant       tenant_mismatch, 403, and an audited DENY —
                               ADR-105 step 1, not the trigger
parent unit does not exist     forbidden, 403. The `admin` check on it
                               denies before any row is written, and A3.3
                               makes that indistinguishable from absence
path would exceed 12 edges     invalid_request, 400
edge would close a cycle       conflict is not in D7's vocabulary; this is
                               invalid_request too, and unreachable from
                               THIS endpoint (ADR-115)
```

`POST /t/{t}/units` checks `admin` on whichever parent the request names —
`tenant:{t}` for a top-level unit, an existing unit for a nested one. No
model change was needed for either endpoint: `organizational_unit.admin`
already resolves `admin from auth_parent`, and a unit's `member` already
resolves `member from auth_parent`, so a tenant admin reaches
`GET /t/{t}/units/{unit}` through `member from auth_parent` without anything
new being declared.

Creating a unit does **not** create its `auth_parent` edge automatically
(05.1.3 invariant 4, ADR-016). The endpoint writes both the `organizational_unit`
row and the edge in one transaction, running the cycle check (A2.2) before
committing either — the invariant that "creating a unit with an org parent
does not implicitly create an authorization edge" describes the model, not
this endpoint's convenience; the endpoint may offer the matching edge, and
here it does, explicitly, as its own write.

### `POST /t/{t}/people`

```txt
POST /t/{t}/people        check: may_register_person on the tenant (ADR-114)
{ "display_name": ..., "given_name": ..., "family_name": ...,
  "date_of_birth": ..., "preferred_language": ... }

creates   person row, global, status ACTIVE
publishes nothing
```

`display_name` is the only required field. A2.3 makes every name column
nullable because a real register holds people known by one name, and the
front desk taking an application (05.3.4) has whatever the applicant gave
them.

**It publishes no tuple, and that is not an omission.** A1.2's `person` type
carries `principal` and `guardian` and no tenant relation: a person row is
reachable in the graph through the memberships and principals that name it,
never on its own. Registering a person creates somebody the tenant can now
admit; it grants nothing, which is why ADR-114 could put the permission on
the tenant rather than on the row.

No principal is created either. `principal.idp_subject` comes from the
identity provider, and a person registered at a front desk has not
authenticated anywhere yet.

### `GET /t/{t}/units/{unit}/members`

Lists memberships whose `org_unit_id` names the unit **in the request path,
and no other** — not its descendants. This is ADR-104's permitted
under-reporting: the mechanism must never over-report, and folding
descendants into the list would require either a recursive query per list
(unbounded by page size) or a denormalized closure table this design does
not carry.

**The asymmetry is deliberate and worth stating plainly, because it looks
like a bug.** `organizational_unit.member_read` DOES reach descendants in
the graph (A1.4) — a caller authorized at a parent may list a **child's**
members by naming the child explicitly. What that caller never sees is a
child's members folded into the **parent's** list; a parent's list shows
only memberships whose `org_unit_id` is the parent itself. Authority
propagates down the DAG; the membership rows do not fold up it.

An association-level membership — `org_unit_id IS NULL` — appears under no
unit's list at all, by the same rule (A2.4). Open, 11.2.

### Why configuration is here at all

The slice could not run without it. `membership.plan_id` is `NOT NULL`, so
admission needs a plan; ADR-107's `covered_member` tuple needs a plan to point
at; and `consumption_type.may_record` resolves through `entitled` from an
entitlement bundle. With no endpoint creating a bundle, a plan, or the link
between them, a member admitted through this API would hold no entitlements
and could not record a meal — which is the whole of the hostel case (K2, K8).
The gap was found by trying to build 8.3, not by reading A3.

**A plan is never edited once people hold it** (05.3.2). It is created `OPEN`,
and its lifecycle is the four endpoints below (ADR-121); a plan's entitlements
are changed by creating a successor and moving holders onto it.

```txt
OPEN ──close──▶ CLOSED_TO_NEW ──retire──▶ RETIRED
  ▲                  │
  └───── reopen ─────┘            while it has no successor
```

All four require `tenant:admin`, take an `Idempotency-Key` (a retry after a
lost 200 gets the answer and not `plan_wrong_state`), and answer the plan. Any
transition from a state it does not belong to is `409 plan_wrong_state`.

- **close** — `OPEN` to `CLOSED_TO_NEW`. Admission refuses the plan from then on
  (`invalid_request`, the plan is not OPEN); holders keep it. Admission
  share-locks the plan row until it commits, so a close waits for an admission
  in flight and the next retire counts the new holder.
- **reopen** — back to `OPEN`, unless the plan has a successor.
- **supersede** — `{ "successor_plan_id": ... }`. The successor must be another
  plan of the tenant that is `OPEN` (`invalid_request` otherwise). The plan
  becomes `CLOSED_TO_NEW` with `superseded_by_id` set, shown in the plan list.
  Holders are not moved. A replaced plan is always closed (migration 0011's
  CHECK), which is also why the chain cannot loop.
- **retire** — `CLOSED_TO_NEW` to `RETIRED`, only when no membership on it is
  `ACTIVE`, `SUSPENDED` or `LAPSED` (`409 plan_has_holders`; a `TERMINATED`
  member does not hold a plan).

Moving a holder is `POST /t/{t}/memberships/{m}/plan { "plan_id": ... }` —
`tenant:admin`, idempotent, "explicit, per-membership, auditable" (05.3.2). The
target must be another `OPEN` plan of the tenant; the membership must be
`ACTIVE` or `SUSPENDED` (`409 membership_wrong_state` otherwise). An `ACTIVE`
member follows the revocation path for the plan they leave — the same fence,
void and synchronous delete as a suspension (ADR-118), rolled back and reported
if the delete fails — and the grant for the plan they join is queued, so for a
dispatch they are under-covered and never over-covered. A `SUSPENDED` member's
plan changes and reinstating grants the new one. Each move writes a
`membership_plan_change` row: membership, from, to, who, when.

### The membership number is supplied, not allocated

```txt
POST /t/{t}/memberships   { person_id, plan_id, number, ... }
```

`membership.number` is a **tenant-visible identifier** (05.3.5) and the caller
supplies it. `UNIQUE (tenant_id, number)` is the entire mechanism; a collision
is a 409.

Deliberately **not** ADR-103's counter. Gapless allocation exists because a tax
authority requires an invoice series with no gaps; a membership register has no
such rule, so that machinery would buy contention for nothing. It also keeps
each association's own numbering convention intact — the same reasoning that
settled ADR-107 — and leaves historical numbers importable without a special
case when B5 arrives.

### What admission does

```txt
POST /t/{t}/memberships
Idempotency-Key: <client-generated>          required (A3.6)
{ person_id, plan_id, number, org_unit_id?, valid_from?, valid_until? }
    -> 201 { id, person_id, plan_id, org_unit_id, number, state,
             admitted_at, valid_from, valid_until }
```

Gated on `tenant:may_approve_membership`, not on `tenant:admin`. ADR-110 made
that relation role-grantable precisely so a front desk can admit members
without holding administration of the association (05.3.4). The unit-scoped
half of 05.3.4's authority — "the tenant or an org unit above the
application" — is not checked, because A1.2 declares no admission relation on
`organizational_unit` to check against. Open, 11.2.

**The person already exists.** `POST /t/{t}/people` creates the global row
under `may_register_person` (ADR-114); admission takes a `person_id`. A
`person_id` or `org_unit_id` that does not resolve is 404, which A3.3 makes
indistinguishable from one the caller may not know about.

**The plan must be OPEN.** 05.3.2 calls `CLOSED_TO_NEW` the common real state
— existing life members keep a plan that no longer accepts applicants — and
`DRAFT` is a plan not yet live. Admitting onto either is `invalid_request`.
The plan's status is read in the same statement that inserts the membership,
so a plan closing between the two cannot be raced.

**The term is the caller's, and nothing derives it.** `valid_from` and
`valid_until` are passed through unvalidated against `plan.duration`, for the
same reason the number is supplied: an association's own convention survives,
and B5 imports historical memberships without a special case. The
consequence, that a `LIFETIME` plan will accept a membership carrying an end
date, is open in 11.2 rather than closed by a derivation this document does
not specify.

**Three authorization facts, through the outbox.** All three are grants, so
they may lag a dispatch interval (8.3) and a member cannot record until the
dispatcher has run:

```txt
tenant:<t>                    tenant          membership:<m>
person:<p>                    holder          membership:<m>
membership:<m>#covered        covered_member  membership_plan:<plan>
```

The third is ADR-107's, and the `#covered` suffix is the load-bearing part:
the user is the membership's covered USERSET, because what the plan covers is
every person the membership covers. They are three separate facts rather than
one because each is separately revocable — a suspension lifts coverage while
the membership and its holder remain (08's synchronous revocation list).

**Recording for another person is not a different endpoint.** It is the same
`POST .../records` with a different subject, gated by `may_record_for_other`
rather than `may_record`. Two endpoints would mean two places to get the
authorization wrong.

---

## A3.10 Consumption

Built in 8.3 from 05.10. Everything below was found by building it, not by
reading 05.10; each item says what 05.10 left open.

```txt
POST /t/{t}/consumption-types/{ct}/options          check: manager on the type
GET  /t/{t}/consumption-types/{ct}/options
POST /t/{t}/consumption-types/{ct}/obligations      check: manager on the type
POST /t/{t}/consumption-types/{ct}/records          Idempotency-Key
POST /t/{t}/consumption-records/{r}/correction      Idempotency-Key
GET  /t/{t}/consumption-types/{ct}/records          check: may_read on the type
POST /t/{t}/absences
POST /t/{t}/absences/{a}/withdraw
POST /t/{t}/consumption-types/{ct}/close            Idempotency-Key
```

### The tenant's admin manages every type in it

`consumption_type.manager` gained `admin from tenant` (A1.2). Before this the
owner of a new tenant could define a type and never again do anything to it:
`manager` resolved only through an `auth_parent` the API never writes, and no
endpoint creates a role assignment, so nothing could grant `may_close_period`
or `may_record_for_other` either. `may_correct_late` joined the type with the
same shape, because 05.10.7 requires it and A1.2 declared no such relation.

### Defining a type

`POST /t/{t}/consumption-types` takes `correction_window_days` (optional;
null is no limit, 05.10.7) beside `name`, `obligates`, `recurrence` and
`record_mode`, and the type reports it back. A window of zero days makes
every correction after the day itself late.

### Recording

```txt
POST /t/{t}/consumption-types/{ct}/records
Idempotency-Key: <client-generated>                       required (A3.6)
{ "subject_person_id": ...,        optional; default = the caller's person
  "occurred_on": "2026-10-04",     required; not more than a day ahead of UTC
  "variant_code": "NONVEG",        optional
  "option_codes": ["EGG"] }        optional
    -> 201 { id, consumption_type_id, subject_person_id, actor_person_id,
             occurred_on, obligation_id, variant_code, option_codes,
             supersedes_id, voided, recorded_at }
```

Recording for oneself is checked as `may_record`, **for the caller's person
and for the caller's principal, either sufficing**. Entitlement is a
property of a `person` (a membership covers a human, ADR-107) while
`manager` is a property of a `principal`, so a single-subject check would
deny either the member or the owner. The two are asked of the graph together
and a DENY is audited only if both refuse. Recording for another person is
`may_record_for_other`, principal only.

**The subject must be entitled, too.** `person` is global (A2.1), so
`may_record_for_other` alone would let any tenant's staff bill any person in
the instance. The subject is checked as `person:<subject> may_record` on the
same type; an unentitled subject is `forbidden`. This is the DOMAIN half of
05.10.9's split applied to the person recorded about, not only the person
recording.

`record_mode` (05.10.3) is enforced here and is `record_mode_not_allowed`
(422) when violated: `SELF` refuses a subject other than the actor, `STAFF`
refuses the actor as subject, `EITHER` allows both. Authorization decides
whether the caller may; `record_mode` decides whether this type takes it.

The remaining refusals:

```txt
type does not exist / not ACTIVE        404 / invalid_request
subject_person_id does not exist        404
occurred_on in the future               invalid_request
an option_code unknown or WITHDRAWN     invalid_request
a live record exists for that day       409 already_recorded
the day is inside a closed period       409 period_closed
```

`already_recorded` is the partial unique index of A2.9; the constraint name
is a contract with Go, as migration 0005's are. The client's recourse is a
correction, not a second record.

`obligation_id` is set when the subject has an obligation of this type
covering `occurred_on`, so a record discharges the obligation without the
caller naming it. **A late first record is not a correction**: 05.10.7's
window bounds corrections, and a record for a day nobody recorded is a first
fact. It is still refused inside a closed period, because that period has
already been handed to Finance.

**Variants carry no price.** 05.10.5 distinguishes a variant (modifies the
obligated unit, may carry a surcharge) from an option (an addition), but A2.9
holds a catalogue of options only. `variant_code` is stored as given and
reported at close; a variant surcharge is expressed as an option with the
same code. A variant catalogue is 11.2's.

### Options and obligations

```txt
POST .../options      { code, name, charge_kind, amount, currency }   -> 201
GET  .../options
POST .../obligations  { subject_person_id, membership_id,
                        effective_from, effective_until? }            -> 201
```

A2.9 defined both tables and A3.7 no way to fill them, so the hostel's
dinner could never be an obligation and the egg never an option. An
obligation is created from a **membership** (`source_type = MEMBERSHIP`,
`source_id = membership_id`), which must belong to the tenant and be held by
the named person; `STAY` and `SUBSCRIPTION` sources have no endpoint. It
requires `obligates = true` on the type. Two obligations of one type for one
person may not overlap (`obligation_overlaps`, 409 — A2.9's exclusion).

### Dates are UTC midnights

Every `date` in this section is a calendar day, and the `tstzrange` columns
store `[from 00:00 UTC, until+1 00:00 UTC)`. Which day it is _now_ belongs to
the tenant (ADR-119): `occurred_on` is still a date the caller supplies and
never a timestamp the server derives, "the future" is any day after today
where the tenant is, a record is late when the tenant's day began longer ago
than the type's correction window, and an ended membership's obligations stop
after the tenant's today. API ranges are **inclusive** at both ends
(`"from": "2026-10-01", "to": "2026-10-31"`).

### Corrections

```txt
POST /t/{t}/consumption-records/{r}/correction
Idempotency-Key: <client-generated>                       required
{ "void": true }                                   or
{ "variant_code": ..., "option_codes": [...] }
    -> 201, the new record (supersedes_id = {r})
```

Check: `may_correct` on the record's type. A correction keeps the superseded
record's subject, type and day; moving a record to another day is a void
plus a new record. **`void` is a correction that says no consumption
occurred** (A2.9 gained `consumption_record.voided`, migration 0006): without
it a mistaken record could only be changed, never taken back, and ADR-098
forbids deleting it. A voided head counts as nothing at close and is hidden
from the default list.

A voided head can itself be corrected, and a non-void correction of it
revives the day: a void is a statement, not a deletion, and the day must
not be lost to a mis-tap. Voiding a record that is already void is
`invalid_request`. An absent record answers 404 before there is a type to
check permission against; record ids are unguessable, which is what makes
that cheap.

```txt
r is not the live head of its chain      409 record_already_corrected
r's day is inside a closed period        409 period_closed
r is older than correction_window        needs may_correct_late, else
                                         409 consumption_window_closed
```

A record may be superseded **once** (A2.9's index covers originals only;
migration 0006 adds the other half), so two simultaneous corrections cannot
fork the chain. The window is `occurred_on + correction_window < now()`,
null meaning unbounded (05.10.7); a successful late correction writes an
`audit_event` at severity `NOTABLE`.

Self-correction is not offered: `may_correct` is staff authority, so a
resident who mis-tapped needs the warden. Open, 11.2.

### Listing

`GET .../consumption-types/{ct}/records?subject=&from=&to=&history=` pages
by `(occurred_on desc, id)` (A3.5), checks `may_read` on the type once
(ADR-104), and returns **live heads only** — neither superseded nor voided —
unless `history=true`.

### Expected absence

```txt
POST /t/{t}/absences
{ "subject_person_id": ..., "from": "2026-10-10", "to": "2026-10-20" }
    -> 201 { id, subject_person_id, period_from, period_to,
             relieves_recording, relieves_payment, declared_by_person_id }
POST /t/{t}/absences/{a}/withdraw        -> 200
```

Declaring for oneself needs **an ACTIVE membership held by the caller's
person in this tenant**, read from PostgreSQL. This is the one handler in the
slice that does not run a 6.1 graph check first: an absence is not per type,
no relation on the tenant means "may declare my own absence", and adding one
to A1.2 for a fact PostgreSQL already holds would write a tuple per member
for no gain. Declaring for another person needs `tenant:admin`; a finer
relation is 11.2's.

`relieves_recording` is always true. `relieves_payment` is true when the
period is **seven days or longer** — the reference tenant's threshold, the
platform default until 05.7's policy engine exists to carry
`ABSENCE_PAYMENT_RELIEF` per tenant. Both flags are snapshotted onto the row
(ADR-099), so changing the default later rebills nothing. `decision_record_id`
is always null (self-declared). Withdrawal sets `withdrawn_at`; only the
declarer or a tenant admin may withdraw. Overlapping absences are allowed —
relief is a union.

### Closing a period

```txt
POST /t/{t}/consumption-types/{ct}/close
Idempotency-Key: <client-generated>                       required
{ "from": "2026-10-01", "to": "2026-10-31" }
    -> 201 { closed: [ { id, subject_person_id, obligated_units,
                         summary, invoice_id | null } ] }
```

Check: `may_close_period` on the type. For every person with an obligation
overlapping the period or a live record inside it, one row of
`consumption_period_close` is written (migration 0006) and Finance composes
its charge (A3.11) **in the same transaction**. All or nothing: one person
with no rate rolls back the whole close.

**Exactly-once per person-period is an exclusion constraint** (05.10.12's
open item): a second close overlapping an existing one raises 23P01 and is
`409 period_already_closed`. No ordering of concurrent requests can defeat
it, and an idempotency key alone could not — two different keys are two
different requests. Recording and closing both take
`pg_advisory_xact_lock` on `(type, subject)`, so a record committing while
its person is being closed is either counted or refused, never lost.

`obligated_units` for a person, from the obligation's `effective` ∩ the
period, with days relieved by a payment-relieving, un-withdrawn absence
removed (05.10.4):

```txt
DAILY            unrelieved days
WEEKLY           ISO weeks containing at least one unrelieved day
PER_OCCURRENCE   live records in the period (nothing is standing)
```

A non-obligating type has no obligated units; its charge is options only.
`summary` is the published `ConsumptionPeriodClosed` (05.10.8): units,
relieved units, variant counts and option counts with the catalogue price at
close. **There is no event bus (B6):** the handoff is a direct call from the
handler into Finance within the transaction, and the handler is the only
place the two contexts meet. Consumption never imports Finance.

Option prices are the live catalogue price at close; 05.10.12's price
versioning remains open.

---

## A3.11 Finance

```txt
POST /t/{t}/consumption-types/{ct}/rates         check: finance_manager
GET  /t/{t}/invoices                             check: finance_reader  (scope)
GET  /t/{t}/invoices/{inv}                       check: viewer
POST /t/{t}/invoices/{inv}/issue                 check: finance_manager, Idempotency-Key
POST /t/{t}/invoices/{inv}/payments              check: finance_manager, Idempotency-Key
POST /t/{t}/invoices/{inv}/credit-notes          check: finance_manager, Idempotency-Key
```

Invoices are **not created by an endpoint**. A3.7 listed list/get/issue/pay
and nothing that makes one, and 05.8 says Finance composes from what other
contexts publish; the only publisher in the slice is period close. That is
now the sole way an invoice begins, which is the K8 story (entry to bill)
rather than an omission.

### `tenant.finance_manager`

A1.2 had `finance_reader` and nothing that writes. `finance_manager` is its
write side — rates, issue, payments — role-grantable (ADR-110: A1.2, the
`grantable_permission` seed in 0006, and an assertion), and reachable by
`admin`. It joins `/me`'s candidates, which are now **six** (A3.9).

### Rates

```txt
POST .../rates   { amount, currency, from, to? }   -> 201
```

Finance prices a unit of consumption (05.10.8: "pricing and tax are
Finance's"), and nothing stored a price until `consumption_rate` (0006). One
rate per type per day (`rate_overlaps`, 409). Close reads the rate in force
on the period's first day; a rate that changes mid-period does not split the
period (11.2). A type with obligated units and no rate in force is
`409 no_rate`, not a silent zero: an unpriced close would mark the period
closed and bill nothing, and the exclusion constraint would then forbid
closing it again.

### What close composes

Per closed person, Finance finds that payer's open `DRAFT` invoice in the
charge's currency or starts one, then adds one `charge`
(`source_type = CONSUMPTION`, `source_id = the close row`, ADR-100) with:

```txt
USAGE_FEE   ONE_TIME   obligated_units × rate        (when > 0)
<kind>      ONE_TIME   count × option amount, per option used
```

No flat board fee and no `DISCOUNT` line, unlike 05.10.8's example: that
example assumed a subscription fee, and the hostel's bill is per unit, so
relief shows as fewer units rather than a negative. A person with nothing to
bill gets no charge and `invoice_id: null`. Tax is unbuilt, so `TAX`
components and `tax_snapshot` are absent (11.2). Mixed currencies within one
close are `409 currency_mismatch`.

The invoice's payer is the person's **party** (a `PERSON` party is created on
first need) and the payee the tenant's `ORGANIZATION` party. Creating a draft
publishes `tenant:<t> tenant invoice:<i>` and `person:<p> payer invoice:<i>`
through the outbox, fenced like every grant (ADR-101).

### Issue — ADR-103

```txt
POST .../invoices/{inv}/issue   { "due_at": "2026-11-15T00:00:00Z" }   optional
    -> 200 the invoice, with number, issued_at, due_at, total_amount
```

`DRAFT` only (`invoice_wrong_state`, 409) and at least one component
(`invoice_empty`, 409). The number is `INV/<financial year>/<value, six
digits>`, drawn from `invoice_number_series` by `UPDATE ... SET next_value =
next_value + 1 RETURNING next_value - 1` **as the last write before the
invoice row is updated**, in the transaction that issues the invoice, so a
rollback returns the number. The invoice row is locked first, so two issues
of one invoice serialise on it and the loser sees `ISSUED`. The financial
year starts in April for every tenant; a tenant-configurable year is 11.2's.
`total_amount` is the sum of the signed components. After issue, an invoice's
charges are never touched (A2.8's immutability).

### Payments

```txt
POST .../invoices/{inv}/payments
{ "method": "CASH", "amount": "500.00", "currency": "INR",
  "provider_reference": ..., "received_at": ... }          -> 201
```

`ISSUED` only (`invoice_wrong_state`); currency must equal the invoice's
(`currency_mismatch`); amount positive and no more than the outstanding
balance — the total less credit notes less earlier payments
(`payment_exceeds_balance`, 409). The invoice row is locked, so two
payments cannot both fit the same balance. A payment that brings the balance
to zero moves the invoice to `PAID`. Every payment through this endpoint is
staff-recorded: `recorded_by` is the caller's person and the method is one of
the offline set (`CASH`, `CHEQUE`, `BANK_TRANSFER`, `UPI`); `CARD` and
`PROVIDER_HOSTED` need an adapter that does not exist (05.8.9). `state` is
`COMPLETED`.

### Credit notes

```txt
POST .../invoices/{inv}/credit-notes
{ "amount": "15.00", "reason": "one egg was never served" }    -> 201
```

The correction an issued invoice gets (ADR-120); the invoice itself is never
edited. `ISSUED` or `PAID` only (`invoice_wrong_state`, which a `DRAFT` and a
`VOIDED` invoice also get). The amount is positive with at most two places and
no more than the invoice still asks for after earlier credit notes
(`credit_exceeds_invoice`, 409); the reason is required, at most 500
characters. The invoice row is locked, so a credit and a payment cannot both
fit a balance only one can have. The note is numbered `CN/<financial
year>/<6 digits>` from its own gapless series, drawn last in the transaction
as an invoice number is (ADR-103).

The invoice then moves by what the new figures imply: **`VOIDED`** when it asks
for nothing and nothing was received; **`PAID`** when what was received covers
what it asks; otherwise unchanged. `balance` — on the detail and on every row
of both invoice lists — is the total
less payments **and** credit notes, and is **negative when more was received
than is now asked for — a refund due**. Refunds are payments in reverse and
have no endpoint (11.2). `credit_notes` appears on the detail, in order issued.

### Reading

`GET /invoices` checks `finance_reader` once (ADR-104). `GET /invoices/{inv}`
checks `viewer` as the caller's person and principal together — the payer
tuple is a `person`, `finance_reader from tenant` is a principal's — and a
`DRAFT` additionally needs `finance_reader`: a payer is not shown a bill
nobody has issued. An unissued draft is a 404 to anyone else (A3.3).

### `/me`

```txt
GET /t/{t}/me/consumption?limit&cursor     own live records, newest first
GET /t/{t}/me/invoices?limit&cursor        own invoices, ISSUED and PAID only
```

Both are scoped by the caller's own person id and run no graph check: the
rows are the caller's by identity, and RLS already bounds the tenant. A
principal with no person gets an empty page.

---

## A3.12 What the app reads

8.4 is one permission-gated Flutter app for members and staff, and building
the screens found two reads the slice had no endpoint for.

```txt
GET /t/{t}/me/consumption-types       types the caller may record against
GET /t/{t}/memberships?q=             the member directory, with names
```

### `GET /me/consumption-types`

`GET /consumption-types` is a tenant-admin list, so a member could not be
told what they may record — and "which types may I record against" is exactly
the object-scoped question A3.9 says `/me` cannot enumerate, and says to
answer "where the object is returned". This is that place: the active types
of the tenant, filtered to those where the caller holds `may_record` as their
person or as their principal, **one graph question per row** and never
`ListObjects` (ADR-104 refuses it). A page can come back shorter than its
`limit` without being the last one; `next_cursor` is taken before the filter,
so it resumes where the page stopped.

The questions go to OpenFGA directly and not through 6.1. This is visibility,
not a decision: nothing is granted by a type appearing here, recording runs
the full check, and a person with no membership listing the catalogue would
otherwise write a DENY to the audit trail for every type in the tenant. The
tenant admin sees every type, because `manager` is inside `may_record`.

### `GET /memberships`

Every membership in the tenant with the holder's `display_name`, paged by
number (A3.5), narrowed by `q` — a prefix of the number or a fragment of the
name. Checked as `may_approve_membership` on the tenant, once for the page.

Each entry carries `in_term`: whether the membership's term covers today where
the tenant is (ADR-119). A term that ends sets no state, because 8.7 forbids a
sweeper, so `state` alone shows such a member as `ACTIVE`; the decision-time
check refuses them, and `in_term: false` is what tells the front desk why.

`GET /units/{unit}/members` deliberately carries no names: `member_read` on a
unit must not carry a person's record. This list may, because the authority it
needs is the one that took the application and already knows the name. `person`
is global (A2.1) and is read only through a membership of this tenant, so the
tenant sees the names of its own members and no others.

A warden who may record for others (`may_record_for_other`, per type) but not
approve memberships cannot read this list, and the app lets them name a person
by id instead. A directory scoped to the people a type's staff may record for
would need a reverse query ADR-104 refuses. Open, 11.2.

---

## A3.13 Membership lifecycle

```txt
POST /t/{t}/memberships/{m}/suspend     { "reason": "ARREARS" }   check: tenant:admin
POST /t/{t}/memberships/{m}/reinstate                              check: tenant:admin
POST /t/{t}/memberships/{m}/terminate                              check: tenant:admin
    each: Idempotency-Key required (A3.6)  ->  200 the membership
```

Built when the largest open item in 11.2 was written down: admission wrote
three grants and nothing could ever take one away, so coverage outlived the
membership that justified it. 05.3's state machine is:

```txt
ACTIVE --suspend--> SUSPENDED --reinstate--> ACTIVE
ACTIVE|SUSPENDED --terminate--> TERMINATED        terminal
```

`LAPSED` is in the machine and nothing enters it (11.2). Any other transition is
`409 membership_wrong_state`. A `reason` is one of 05.3's four categories —
`ARREARS`, `CONDUCT`, `SAFEGUARDING`, `ADMINISTRATIVE` — and is recorded with the
caller's person and a timestamp in `membership_suspension` (ADR-047). It never
reaches the authorization store.

### What each one revokes

Suspend and terminate delete **one** tuple, `membership:<m>#covered
covered_member membership_plan:<p>`, and leave the membership's `tenant` and
`holder` edges — the membership and its holder remain, which is exactly why
admission wrote three facts and not one. The delete is synchronous and a failed
delete rolls the state change back (ADR-118). A grant for the same fact that was
still pending is voided in the same transaction, so a member suspended before
the dispatcher ran does not acquire coverage a moment later.

Reinstate re-enqueues the coverage grant. Grants lag (8.3): the member can
record again after the next dispatch, not at the instant of the response.
Reinstating can fail with `already_active_member` if the person was admitted
again while suspended — ADR-043 allows one ACTIVE membership per tenant, and a
suspension frees the slot.

Terminate also **ends the member's obligations** (A3.10): each obligation
sourced from the membership stops after today. A suspension does not; arrears
owe board.

### Terms are checked at decision time

`valid_from` and `valid_until` have been stored and read by nothing. For the
entitlement-derived permissions — `consumption_type.may_record`,
`resource.may_use`, `resource.may_book` — a decision allowed to a **person**
now also requires an ACTIVE membership in the tenant whose term covers today.
The check is in PostgreSQL, at decision time, with no sweeper (8.7), and is
audited as a DENY with reason `membership_not_valid`. A principal's own
authority (`manager`, a role) never depends on membership, and a person's
standing as an invoice's payer is not an entitlement, so a terminated member
still reads their bills.

---

## A3.8 Not in the slice

```txt
renewal        instalments      multi-currency     accounting export
bookings       programmes       governance         safeguarding
notifications  bulk import      cross-tenant grants
```

Present in the design, absent from the API until something needs them.

---

## A3.9 What `/me` reports

```json
{
  "principal_id": "...",
  "kind": "PERSONAL",
  "person_id": "...",
  "tenant_id": "...",
  "permissions": ["tenant:admin", "tenant:member"]
}
```

`permissions` is exactly seven candidates, each a full 6.1 check against the
tenant object named in the path: `tenant:admin`, `tenant:member`,
`tenant:finance_reader`, `tenant:finance_manager`,
`tenant:safeguarding_reader`, `tenant:may_approve_membership` and
`tenant:may_register_person`. Seven checks, fixed, whatever the tenant
contains; only the ones that pass are listed. `finance_manager` joined with
A3.11: the client must show or hide issue-and-pay on it, exactly as it does
admission on `may_approve_membership`. `may_register_person` joined with the
app (A3.12): admitting a stranger is two acts — registering the person
(ADR-114) and admitting them — held by two relations, and a front desk with
one and not the other should be shown a button that works.

`may_approve_membership` joined the set with ADR-110. It is tenant-scoped and
role-grantable, and 8.3's admission endpoint is the first thing the client
must show or hide on it — which is exactly what this endpoint is for. Five of
the seven now resolve through a role as readily as through a direct grant, and
the response says nothing about which: the client is told what it may do, not
how it came to be allowed.

A permission on any other object — `consumption_type:may_record`, an invoice,
a unit — is reported by the endpoint that returns that object, at most one
check per row of a page (8.11), never here. `/me` cannot enumerate
object-scoped permissions without a reverse index, and ADR-104 and 8.11
refuse `ListObjects` precisely so that no such index exists — a `/me` that
answered "which consumption types may I record against" would be that index
under another name. It reports authority held over the tenant; everything
else is answered where the object is returned.

This is what makes the single permission-gated Flutter app possible (A3.7):
the client gates its shell on `/me` and gates each row on the flags the list
that row came from already gave it.
