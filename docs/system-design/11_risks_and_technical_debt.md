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

| Gap                           | Impact                                                                                               | Where   |
| ----------------------------- | ---------------------------------------------------------------------------------------------------- | ------- |
| Instructor double-booking     | Allocation protects resources, not people                                                            | 05.4.12 |
| Deposit forfeiture rules      | Settlement exists; criteria unspecified                                                              | 05.8.13 |
| Renewal workflow              | Referenced throughout; not designed                                                                  | 05.3.12 |
| Recurring schedule generation | Occurrences modelled individually                                                                    | 05.4.12 |
| Recurring bookings            | A weekly slot is N independent bookings                                                              | 05.5.11 |
| Temporal policy resolution    | "What was the policy on a past date"                                                                 | 05.7.11 |
| Policy simulation             | No blast-radius view for policy changes                                                              | 05.7.11 |
| Succession                    | Terms expire; no successor workflow                                                                  | 05.6.12 |
| Duplicate detection heuristic | Must not leak existence                                                                              | 05.2.12 |
| Guardianship disputes         | Conflicting guardian instructions                                                                    | 05.2.12 |
| Multi-currency tenants        | One currency per invoice; untested across                                                            | 05.8.13 |
| Accounting export             | No ledger integration                                                                                | 03.3    |
| Appeal against restriction    | Likely out-of-platform                                                                               | 05.9.11 |
| Upward member data            | Whether national bodies hold rosters is unestablished                                                | 05.1.11 |
| Cycle check unimplemented     | `authorization_edge` accepts a cycle; the DAG is a graph until the pre-commit recursive CTE exists   | A2.2    |
| Twelve tables without RLS     | No `tenant_id` column, so no policy. `charge` means an invoice id reaches its contents               | A2.1    |
| Global rows invisible         | Nullable `tenant_id` on `verification`, `clearance`, `audit_event` never matches a tenant connection | A2.1    |
| A2.7 role tables unmigrated   | Specified and wired into A1.2 and 6.1; migration `0002` does not exist, so no role can yet be held   | A2.7    |
| Two A2.11 triggers unwritten  | MANDATORY_TERM needs `valid_until`; `role_permission` must reject a non-grantable permission         | A2.11   |
| Office appointment workflow   | `office_conferred_role` exists; materializing assignments on appointment is unwritten (05.6.4)       | A2.7    |
| Committee-conferred roles     | 05.6.7 routes approval through committee membership; only offices have a conferral table             | A2.7    |
| Role template catalogue       | ADR-079 says templates are cloned; no template source exists to clone from                           | 05.6.12 |
| No reverse role query         | "who holds this permission here" is a PostgreSQL query nobody has written (05.6.7 step 2)            | ADR-109 |

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
