# 11 — Risks and Technical Debt

## 11.1 Accepted risks

### The four RLS exemptions

`person`, `principal`, `guardianship`, `restriction` cannot carry row-level
security because all four are deliberately global (08.2). They are protected
by application-level relationship checks — a weaker guarantee than the
database-enforced isolation everything else gets.

**Mitigation:** these tables have a narrow access surface, and every query
path against them is reviewed as security-relevant. The exemption list is
short and auditable by design.

### Platform-wide restrictions cross a tenant boundary

The single deliberate exception to Principle 1 (05.9.9). Justified because a
person barred from youth-facing roles in one association may reasonably be
barred everywhere, and independent discovery by each tenant is worse.

**Open:** the approval workflow and the appeal path are undesigned. This must
be closed before the feature is built.

### Sixteen synchronous revocation paths

Each must write to OpenFGA before commit and roll back on failure (08.3).
Substantial engineering commitment, and each is a place where a mistake
leaves authority in force.

**Mitigation:** one shared implementation, not sixteen; a test per path.

### Person is global within an instance

Necessary for one-identity semantics, but it means the strongest isolation
boundary in the system has a deliberate hole in it. Visibility is controlled
by relationship rather than by row.

**Mitigation:** person lookup is never a global search; create-or-link is
mediated by verified contact points so existence is not disclosed.

## 11.2 Known gaps

| Gap                                                      | Impact                                                                                                                                                                                                                                                                                                                                    | Where          |
| -------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------- |
| Instructor double-booking                                | Allocation protects resources, not people                                                                                                                                                                                                                                                                                                 | 05.4.12        |
| Deposit forfeiture rules                                 | Settlement exists; criteria unspecified                                                                                                                                                                                                                                                                                                   | 05.8.13        |
| Renewal workflow                                         | Referenced throughout; not designed                                                                                                                                                                                                                                                                                                       | 05.3.12        |
| Recurring schedule generation                            | Occurrences modelled individually                                                                                                                                                                                                                                                                                                         | 05.4.12        |
| Recurring bookings                                       | A weekly slot is N independent bookings                                                                                                                                                                                                                                                                                                   | 05.5.11        |
| Temporal policy resolution                               | "What was the policy on a past date"                                                                                                                                                                                                                                                                                                      | 05.7.11        |
| Policy simulation                                        | No blast-radius view for policy changes                                                                                                                                                                                                                                                                                                   | 05.7.11        |
| Succession                                               | Terms expire; no successor workflow                                                                                                                                                                                                                                                                                                       | 05.6.12        |
| Duplicate detection heuristic                            | Must not leak existence                                                                                                                                                                                                                                                                                                                   | 05.2.12        |
| Guardianship disputes                                    | Conflicting guardian instructions                                                                                                                                                                                                                                                                                                         | 05.2.12        |
| Multi-currency tenants                                   | One currency per invoice; untested across                                                                                                                                                                                                                                                                                                 | 05.8.13        |
| Accounting export                                        | No ledger integration                                                                                                                                                                                                                                                                                                                     | 03.3           |
| Appeal against restriction                               | Likely out-of-platform                                                                                                                                                                                                                                                                                                                    | 05.9.11        |
| Upward member data                                       | Whether national bodies hold rosters is unestablished                                                                                                                                                                                                                                                                                     | 05.1.11        |
| A unit gets one authorization parent over the API        | ADR-016 permits many and `POST /t/{t}/units` creates the first; nothing adds a second. ADR-016's own example — a pool under both a chapter and a department — is unreachable                                                                                                                                                              | A3.7, ADR-016  |
| DAG depth is not configurable                            | A2.2 says "configured maximum, defaulting to 12"; migration 0005 makes it a constant in the trigger, per-instance and not per-tenant                                                                                                                                                                                                      | A2.2, ADR-115  |
| Three edge types are refused, not validated              | `auth_parent` is declared on `resource`, `programme` and `consumption_type`; the trigger refuses them rather than inserting an edge whose endpoints nothing checked. Fail-closed                                                                                                                                                          | A1.2, ADR-115  |
| `location` has no endpoint                               | `organizational_unit.location_id` and `resource.location_id` reference a table nothing can create a row in, so the physical containment of 05.1.3 is unreachable through the API                                                                                                                                                          | A3.7, 05.1.3   |
| Twelve tables without RLS                                | No `tenant_id` column, so no policy. `charge` means an invoice id reaches its contents                                                                                                                                                                                                                                                    | A2.1           |
| Global rows invisible                                    | Nullable `tenant_id` on `verification`, `clearance` never matches a tenant connection                                                                                                                                                                                                                                                     | A2.1           |
| Plan changes have no app screens, and no bulk move       | Close, reopen, supersede, retire and moving a holder are API only (ADR-121); the app has no plan-administration screen. Holders move one membership at a time, with no "move everyone on plan A" and no migration of an in-flight obligation to a new rate                                                                                | A3.7           |
| Membership numbers unvalidated                           | Caller-supplied and unique per tenant; no format rule, so two conventions can coexist in one tenant                                                                                                                                                                                                                                       | A3.7           |
| Nothing sweeps idempotency keys                          | `idempotency_key` grows without bound; the index for a sweep exists, the sweep does not                                                                                                                                                                                                                                                   | A2.10          |
| Office appointment workflow                              | `office_conferred_role` exists; materializing assignments on appointment is unwritten (05.6.4)                                                                                                                                                                                                                                            | A2.7           |
| Committee-conferred roles                                | 05.6.7 routes approval through committee membership; only offices have a conferral table                                                                                                                                                                                                                                                  | A2.7           |
| Role template catalogue                                  | ADR-079 says templates are cloned; no template source exists to clone from                                                                                                                                                                                                                                                                | 05.6.12        |
| No reverse role query                                    | "who holds this permission here" is a PostgreSQL query nobody has written (05.6.7 step 2)                                                                                                                                                                                                                                                 | ADR-109        |
| Platform-plane POSTs outside idempotency                 | `idempotency_key.tenant_id` is `NOT NULL`; a retried `POST /platform/tenants` creates a second tenant. Accepted, not fixed — A3.6's own scope is money and consumption records                                                                                                                                                            | A3.6, ADR-113  |
| `platform_audit_event` unread                            | Written by nothing yet either — `may_read_platform_audit` now has a referent (ADR-112) but still no endpoint reads it. C2, still open                                                                                                                                                                                                     | A2.10, ADR-112 |
| Association-level membership invisible to unit lists     | `membership.org_unit_id IS NULL` appears under no unit's member list — the deliberate under-report ADR-104 requires, not an oversight                                                                                                                                                                                                     | A2.4, A3.7     |
| Nothing sets a membership LAPSED, and nothing renews one | A term that ends is enforced at decision time (A3.13), which is correct and silent: the state stays ACTIVE. The directory reports `in_term: false` for such a member and the app labels it TERM ENDED, but nothing stores LAPSED. There is no renewal workflow either, so "renew" in 05.3.4's diagram has no endpoint                     | 05.3, A3.13    |
| Suspension and termination need `tenant:admin`           | A bursar who issues arrears suspensions would need a finer relation; adding one is ADR-110's three places                                                                                                                                                                                                                                 | A3.13          |
| Suspending leaves the member's obligations running       | Arrears owe board; termination ends them. Whether CONDUCT or SAFEGUARDING suspensions should stop billing is the tenant's policy and nothing carries it                                                                                                                                                                                   | A3.13          |
| Admission derives no term from the plan                  | `valid_from` and `valid_until` are passed through unvalidated, so a `LIFETIME` plan accepts a membership carrying an end date. Deliberate: deriving one is plan-lifecycle logic no ADR specifies                                                                                                                                          | A3.7, 05.3.2   |
| Admission authority is tenant-scoped only                | 05.3.4 puts approval at "the tenant or an org unit above the application"; A1.2 declares no admission relation on `organizational_unit`, so only the tenant-scoped half is checkable                                                                                                                                                      | A1.2, 05.3.4   |
| The application workflow is bypassed                     | 05.3.4's DRAFT→ADMITTED state machine has no endpoint; admission inserts a membership directly and `membership.application_id` is always NULL, so no decision record ties a member to an approval                                                                                                                                         | A3.7, 05.3.4   |
| Dependents are unreachable                               | `membership_dependent`, `plan.max_dependents` and A1.2's `membership.dependent` all exist; nothing adds a dependent, so `covered` resolves to the holder alone                                                                                                                                                                            | A2.4, A1.2     |
| Tenant time zone cannot be changed                       | `tenant.time_zone` is set at provisioning (ADR-119) and has no endpoint to change it: moving a tenant to another zone shifts what "today" means for periods already closed, so it is an operator's act on a specific tenant                                                                                                               | A3.10          |
| Consumption: no variant catalogue                        | `variant_code` is stored and reported, never priced or validated; a surcharge is an option with the same code (A3.10)                                                                                                                                                                                                                     | A3.10          |
| Consumption: option price is live                        | A correction or close after a catalogue price change uses the new price. 05.10.12's price versioning is still open                                                                                                                                                                                                                        | A3.10          |
| Consumption: no self-correction                          | `may_correct` is staff authority, so a resident who mis-recorded needs the warden (A3.10)                                                                                                                                                                                                                                                 | A3.10          |
| Consumption: subject restrictions unchecked              | Recording for another person checks the subject's entitlement but not a `NO_RESOURCE_ACCESS` restriction on them (A3.10)                                                                                                                                                                                                                  | A3.10          |
| Absence: finer authority and a policy-carried threshold  | Declaring for another person needs `tenant:admin`; relief threshold is a platform default of seven days until 05.7's `ABSENCE_PAYMENT_RELIEF` exists (A3.10)                                                                                                                                                                              | A3.10          |
| Consumption: obligations only from a membership          | `STAY` and `SUBSCRIPTION` sources have no endpoint, so a boarder's dinner cannot be obligated by a stay; no endpoint ends or lists obligations (A3.10)                                                                                                                                                                                    | A3.10          |
| Close: one rate per period, no tax                       | A rate changing mid-period does not split the close; `TAX` components and `tax_snapshot` are absent (A3.11)                                                                                                                                                                                                                               | A3.11          |
| Finance: financial year is April                         | `invoice_number_series.series` assumes an April start for every tenant (A3.11)                                                                                                                                                                                                                                                            | A3.11          |
| Reconciler covers membership coverage only               | `api reconcile-coverage` (ADR-122) compares a membership's tenant edge, holder and plan coverage. Tuples for units, plans, bundles and invoices, and tuples whose membership row is gone, are not compared; nothing runs it on a schedule                                                                                                 | ADR-122        |
| Finance: no refunds                                      | A credit note after payment leaves a negative balance, a refund due (ADR-120), and nothing moves the money back: a refund is a payment in reverse and has no endpoint, state or reconciliation                                                                                                                                            | A3.11          |
| Close after payment                                      | A correction is refused (`period_closed`) once its period is closed; the remedy is a credit note (ADR-120), which corrects the invoice and leaves the closed period and its records as they were                                                                                                                                          | A3.11          |
| A warden cannot search the member directory              | `GET /memberships` needs `may_approve_membership`; a person who may only record for others names a member by id (A3.12)                                                                                                                                                                                                                   | A3.12          |
| Logins are made by a command, not an endpoint            | `api bootstrap-principal <person-id> <idp-subject>` binds an identity-provider subject to an ACTIVE person as a PERSONAL principal (idempotent; a subject already bound to another person is refused). `POST /people` still creates no principal, because `idp_subject` comes from the identity provider (A3.7), which has no adapter yet | A3.7           |
| A new object is unreadable until the dispatcher runs     | Every grant lags the outbox (ADR-101), so an administrator who defines a type and immediately adds an option, or closes a period and immediately opens the invoice, gets a 403 for a few seconds. Correct by the design, and a real surprise in a UI that does not retry                                                                  | A3.11          |

**Closed this round, this time by code.** The cycle-check row is gone
because migration 0005 exists, not because a decision was taken: ADR-016's
three invariants are enforced by a trigger on `authorization_edge`
(ADR-115), and the drift test writes a real cycle and a 13-edge path and
watches both be refused.

R10 removed this row on the strength of having _specified_ where the CTE
runs, and `e8c99a4` put it back — the near-miss is kept here because it is
the useful part. The rule it produced: a gap closes when the code lands and
has been made to fail, never when the decision is taken.

Two of the rows that replace it come straight out of building the thing.
Only the depth bound is reachable from `POST /t/{t}/units` — a brand-new
unit cannot close a cycle, since nothing points at it — so the endpoint
alone could never have exercised the invariant it was specified to enforce.
That is what moved the check into the database.

**Still open, and now reachable.** `may_register_person` and
`membership.org_unit_id` were the `via_plan` defect class — declared in
migration 0004 and read by nothing. `POST /t/{t}/people` and
`GET /t/{t}/units/{unit}/members` now read both, so that pair is closed.

## 11.3 Unresolved tensions

### Erasure versus retention

GDPR and DPDP erasure rights conflict with safeguarding and financial
retention obligations. The design preserves audit and structure by default
and scrubs personal fields.

**Not resolved.** Which obligation prevails is jurisdictional and, in some
cases, genuinely contested. Tenants configure retention; the platform does
not pretend to adjudicate.

### Specific errors versus non-disclosure

Denials should say why (08.8), but must not leak existence, restriction
contents, or rejection reasons. Where they conflict, non-disclosure wins and
the event is audited — which means some denials are less helpful than they
could be.

### Conditional policies

"Minimum age 16 with guardian consent, 18 without" is currently two policy
types. This will not scale if conditional criteria become common, and the
alternative — expressions in policy values — is the code-in-the-database
outcome ADR-033 exists to prevent.

**Watch item.** If a third or fourth conditional case appears, the policy
model needs revisiting rather than working around.

## 11.4 Things most likely to be broken by well-meaning changes

Ranked by how plausible the mistake is.

| #   | The temptation                                                   | Why it is wrong                                                                                                                               |
| --- | ---------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Auto-create an authorization edge when an org unit gets a parent | Collapses authorization containment into organizational containment; undoes the central correction of the design                              |
| 2   | Put `may_sanction` on `tenant` instead of `affiliation`          | Removes the structural guarantee that sanction cannot reach data                                                                              |
| 3   | Add a `reason` field to application rejection                    | Creates a record the association never intended and may not lawfully hold                                                                     |
| 4   | Store the certificate number "just for reference"                | Converts every tenant into a custodian of screening evidence                                                                                  |
| 5   | Use `but not` for a scope exclusion                              | Makes "why can this person do this?" undecidable                                                                                              |
| 6   | Move clearance validity into OpenFGA                             | Reintroduces sweeper-dependent expiry                                                                                                         |
| 7   | Make `Allocation` an aggregate root to restore DDD purity        | Puts the exclusivity invariant back into application code                                                                                     |
| 8   | Have Finance suspend memberships directly                        | Couples authorization to live financial state                                                                                                 |
| 9   | Give platform admin a "read everything" role for support         | Defeats the two-plane separation entirely                                                                                                     |
| 10  | Render times in the viewer's timezone                            | Produces booking errors that are easy to create and hard to detect                                                                            |
| 11  | Wire the `may_administer` verb into `tenant.administered_by`     | The two are unrelated and were once identically named; the verb reaches nothing, `administered_by` feeds `organizational_unit.admin`. ADR-102 |
| 12  | Give `may_read_member_data` a path "because it is granted"       | The grant records an authority the platform declines to implement. Real access is a `CrossTenantGrant`. ADR-102                               |

Each of these has an ADR and a test. If a change requires one of them, the
change is wrong — or the ADR needs superseding first, deliberately, in this
document.
