# 03 — Context and Scope

## 3.1 Business context

```txt
                        ┌──────────────────────┐
   Members,             │                      │      Payment
   Guardians ──────────▶│                      │─────▶Providers
                        │                      │      (regional)
   Association          │    YMCA Platform     │
   Admins,      ───────▶│                      │─────▶Screening
   Office holders       │                      │      Providers
                        │                      │
   Federation   ───────▶│                      │─────▶Safeguarding
   Bodies               │                      │      Case System
                        │                      │
   Platform     ───────▶│                      │◀────▶Identity
   Operators (JIT)      └──────────────────────┘      Provider
```

| External                 | Direction | Exchanged                             |
| ------------------------ | --------- | ------------------------------------- |
| Identity Provider        | in        | Authenticated principal (OIDC)        |
| Payment providers        | both      | Payment intents; settlement callbacks |
| Screening providers      | both      | Check requests; verdicts only         |
| Safeguarding case system | out       | Opaque case references only           |
| Accounting systems       | out       | Not designed; see 11                  |

## 3.2 What is in scope

```txt
Organizational structure, affiliation, authority
Identity, guardianship, consent
Membership: plans, admission, lifecycle, entitlement
Programmes, offerings, enrollment, occurrences
Resources, booking, hostel stay, walk-in access
Consumption: obligations, records, declared absence
Governance: offices, terms, committees, decision records
Policy definition, inheritance, evaluation
Finance: invoicing, charges, payments, reconciliation
Safeguarding: verification, clearance, restriction
Platform: provisioning, JIT access, break-glass, audit
```

## 3.3 What is deliberately out of scope

Each exclusion is a decision, not an omission.

| Excluded                              | Reason                                                                                    |
| ------------------------------------- | ----------------------------------------------------------------------------------------- |
| Elections, ballots, meetings, minutes | A different product; decisions are recorded, process is not (ADR-072)                     |
| Safeguarding case management          | Authorities are the system of record; privilege and retention differ (ADR-089)            |
| Screening evidence                    | No authorization use; storing it creates custodianship of criminal-history data (ADR-086) |
| General ledger / accounting           | Associations have existing systems; export is a future integration                        |
| Property management                   | `Location` is descriptive; buildings are not assets here                                  |
| CRM, fundraising, donor management    | Adjacent products                                                                         |
| Learning management                   | Training completion is a verification; delivery is elsewhere                              |
| Waitlist mechanics                    | Status reserved; feature deferred (ADR-062)                                               |
| Inter-organizational dues             | Deferred; the model accommodates it (ADR-085)                                             |

## 3.4 What the platform refuses to decide

Distinct from the above: these are in scope as **machinery**, with the
answers supplied by tenants.

```txt
Which plans exist and what they cost
Whether facilities are bundled into membership
Whether staff are also members
Which resource types exist
Who approves admission
Constitutional eligibility criteria
Whether screening is available
Tax treatment
```

A sovereign tenant that cannot express its own constitution is not
sovereign. See 04.7.
