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
principal_kind    PERSONAL | ELEVATED
tenant_id         compared against the path
expires_at        checked at decision time, never by a sweeper
```

A person acting as themselves and the same person acting with elevated
authority are different principals, and therefore different subjects. Nothing
in the API lets one request carry both.

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

---

## A3.7 The slice

```txt
PLATFORM
  POST   /platform/tenants                         create tenant

ORGANIZATION
  POST   /t/{t}/units                              create org unit
  GET    /t/{t}/units/{unit}

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
