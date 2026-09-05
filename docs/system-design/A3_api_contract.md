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

---

## A3.2 Authentication

```txt
Authorization: Bearer <token>
```

Resolved through the port of ADR-106 to:

```txt
principal_id      the subject of every tuple
principal_kind    PERSONAL | STAFF | ELEVATED
tenant_id         compared against the path
expires_at        checked at decision time, never by a sweeper
```

A person acting as themselves, as staff, and with elevated authority are
three different principals, and therefore three different subjects (05.2.3).
Nothing in the API lets one request carry more than one.

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

---

## A3.7 The slice

```txt
PLATFORM
  POST   /platform/tenants                         create tenant

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
