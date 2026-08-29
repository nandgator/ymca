# YMCA Mess Management — Domain Glossary

This is the ubiquitous language for this codebase. Code, API field names, DB
columns, and conversations about this project should all use these exact terms.
If you're about to invent a synonym, don't — check here first.

## Actors

- **CentralAdmin** — manages the list of `Hostel`s and their `Secretary`
  accounts. Cannot touch a hostel's roster, menu, pricing, or billing.
- **Secretary** — staff, scoped to exactly one `Hostel`. Full control of that
  hostel's roster, `MenuCycle`, `OptionalItem` prices, leave approvals (leave
  itself is self-registered, not approved — see below), and month-end billing.
- **Member** — belongs to exactly one `Hostel`. Logs `Entry` records and
  `Leave` periods for themselves only.

None of these self-register. Accounts are provisioned offline (outside this
system) with a `MemberID`/`StaffID` plus a verified email and/or mobile
number. Login is OTP-only: submit the ID, receive an OTP by email or SMS,
exchange it for a session. There are no passwords anywhere in this system.

## Hostel & HostelPolicy

A **Hostel** is a physical mess site. Every domain rule that could vary is
attached to that hostel's **HostelPolicy**, set by its Secretary — nothing
below is a global constant:

- `FlatMonthlyFee` — baseline charge, assumes veg dinner every night.
- `MenuCycle` — a 7-day repeating breakfast menu (fixed items, no charge).
- `OptionalItemPriceList` — a la carte breakfast add-ons (boiled egg, milk,
  tea, coffee, omelette, sandwich, ...), each independently priced, chosen
  per `Entry`.
- `NonVegSurcharge` — extra amount added for a day a member logs non-veg
  dinner.
- `LongLeaveThresholdDays` — a `Leave` whose duration is ≥ this becomes
  `LONG`; shorter is `SHORT`. (E.g. threshold = 7.)
- `DailyDeductionRate` — the per-day amount subtracted from the flat fee for
  each `LONG` leave day.

## Entry

An **Entry** is an after-the-fact record: `(member_id, date, meal_type)`
where `meal_type ∈ {BREAKFAST, DINNER}`. Submitted same-day, editable until
midnight, then **locked** — no edits, including by the Secretary, after lock
(Secretary can add an administrative override note, but the original entry
is immutable history).

- A `BREAKFAST` entry carries zero or more chosen `OptionalItem`s (from the
  hostel's price list). The fixed weekly menu item itself is not billed.
- A `DINNER` entry carries a `veg` or `non_veg` flag, decided fresh each time
  — this is **not** a standing member preference. `non_veg` triggers
  `NonVegSurcharge` for that day only.

### Default when no Entry and no Leave exists for a day (the "forgot" case)

This default is **per meal type**, not a single global default:

- `BREAKFAST` with no entry → **not billed**, treated as absent.
- `DINNER` with no entry → **billed**, treated as present (baseline veg rate).

## Leave

A **Leave** is `(member_id, start_date, end_date)`, self-registered by the
member with no approval step. While a Leave is active, the member does not
need to submit entries for either meal on those dates.

- `duration_days >= HostelPolicy.LongLeaveThresholdDays` → **LONG**: exempt
  from billing (deducted via `DailyDeductionRate` per day) and from entries.
- `duration_days < threshold` → **SHORT**: exempt from the entry requirement
  only — still billed as if present (baseline dinner rate), since the mess
  still may have provisioned for them.

## Month-end Bill (computed by Secretary, per member, per calendar month)

```
bill = FlatMonthlyFee
     − (count of LONG leave days that month × DailyDeductionRate)
     + Σ(price of each chosen OptionalItem across all BREAKFAST entries)
     + (count of non_veg DINNER entries × NonVegSurcharge)
```

This is a computed number shown to the Secretary/member. The system does not
collect payment — settlement happens offline (cash/bank transfer).

## Explicitly out of scope (confirmed, not oversights)

- Lunch is not tracked — only breakfast and dinner.
- No guest/non-member diners.
- No payment gateway integration.
- No cross-hostel roster sharing — a Secretary only ever sees their own
  hostel; CentralAdmin sees hostel/secretary metadata only, never entries,
  menus, or bills.
