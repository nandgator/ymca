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
500  internal           anything the caller cannot act on
```

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

Required on every POST that creates money or a consumption record. The key is
stored with the result; a repeat returns the original response rather than
creating a second row.

A warden on hostel wifi will retry. Without this, a retried meal is a second
meal and a retried payment is a second payment.

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

IDENTITY AND MEMBERSHIP
  POST   /t/{t}/people                             create person
  GET    /t/{t}/units/{unit}/members               list  — scope: unit
  POST   /t/{t}/memberships                        admit a member

CONSUMPTION
  POST   /t/{t}/consumption-types                  define a type
  GET    /t/{t}/consumption-types
  POST   /t/{t}/consumption-types/{ct}/records     record use
  POST   /t/{t}/consumption-records/{r}/correction supersede  (ADR-098)
  GET    /t/{t}/consumption-types/{ct}/records     list  — scope: type
  POST   /t/{t}/absences                           declare an absence
  POST   /t/{t}/consumption-types/{ct}/close       close a period

FINANCE
  GET    /t/{t}/invoices                           list  — scope: tenant
  GET    /t/{t}/invoices/{inv}
  POST   /t/{t}/invoices/{inv}/issue                allocate number (ADR-103)
  POST   /t/{t}/invoices/{inv}/payments             record a payment

ME
  GET    /t/{t}/me                                 principal, kind, permissions
  GET    /t/{t}/me/consumption                     own records
  GET    /t/{t}/me/invoices                        own invoices
```

`/me` is what makes one permission-gated app possible: the client renders
from the permissions it is told it has, rather than from a role it guessed.

### Platform provisioning creates a tenant AND its first owner

```txt
POST /platform/tenants
{ "legal_name": ..., "display_name": ..., "jurisdiction": ...,
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
POST /t/{t}/units       check: admin on the named parent (tenant or unit)
GET  /t/{t}/units/{unit}  check: member on the unit
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

**Create and read only.** 05.3.2 gives plans a real lifecycle — editing a live
plan is prohibited, supersession is supported, `CLOSED_TO_NEW` is the common
real state. None of that is here. A plan can be created and listed; it cannot
yet be superseded or retired through the API. That is a deferral, logged in
11.2, not an omission.

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

**Recording for another person is not a different endpoint.** It is the same
`POST .../records` with a different subject, gated by `may_record_for_other`
rather than `may_record`. Two endpoints would mean two places to get the
authorization wrong.

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

`permissions` is exactly five candidates, each a full 6.1 check against the
tenant object named in the path: `tenant:admin`, `tenant:member`,
`tenant:finance_reader`, `tenant:safeguarding_reader` and
`tenant:may_approve_membership`. Five checks, fixed, whatever the tenant
contains; only the ones that pass are listed.

`may_approve_membership` joined the set with ADR-110. It is tenant-scoped and
role-grantable, and 8.3's admission endpoint is the first thing the client
must show or hide on it — which is exactly what this endpoint is for. Three of
the five now resolve through a role as readily as through a direct grant, and
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
