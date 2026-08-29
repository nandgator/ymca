# 02 — Constraints

## 2.1 Organizational

| C | Constraint | Consequence |
|---|---|---|
| C1 | Associations are separately incorporated and legally independent | Tenancy is a security boundary, not a partition |
| C2 | Federation authority is narrow and varies by relationship | Five independent authority verbs |
| C3 | Constitutions differ per association | Governance is configuration, not code |
| C4 | Roughly half of local associations may be unaffiliated | Affiliation is optional, never presumed |
| C5 | Board approval governs admission and conferral | Approval workflow is unavoidable |

## 2.2 Regulatory

| C | Constraint | Consequence |
|---|---|---|
| C6 | GDPR and India's DPDP Act, plus others per jurisdiction | Purpose limitation, minimisation, erasure |
| C7 | Special-category data (health, religion, criminal history) attracts stricter handling | Never stored; verdicts only |
| C8 | Minors are present by design (junior membership categories) | Guardian consent, minimisation, guardian-mediated contact |
| C9 | Safeguarding screening obligations, recurring and event-triggered | Verification lifecycle, clearance gating |
| C10 | Mandatory reporting to authorities | Case material stays outside the platform |
| C11 | Statutory receipt numbering in some jurisdictions | Gapless invoice numbers per tenant per year |
| C12 | Data residency may be legally mandated | Regional platform instances |

## 2.3 Technical

| C | Constraint | Consequence |
|---|---|---|
| C13 | OpenFGA as the authorization engine | Zanzibar semantics; no negative relations |
| C14 | PostgreSQL as system of record | RLS and exclusion constraints available and used |
| C15 | No distributed transactions between stores | Transactional outbox; synchronous revocation |
| C16 | Registry screening infrastructure exists in few jurisdictions | Capability, disabled by default |
| C17 | Payment providers are regional | Provider facade, no provider vocabulary in the domain |
| C18 | Occupancy sensing absent at most sites | Walk-in capacity unenforced |

## 2.4 Conventions

```txt
Documentation      arc42, C4 diagrams, ADRs, DDD vocabulary
Format             Markdown, docs-as-code, reviewed by diff
Identifiers        opaque UUIDs; no PII in identifiers or tuples
Time               timestamptz UTC; display in the resource's timezone
Naming             the glossary (12) is binding, including its
                   list of terms deliberately not used
```
