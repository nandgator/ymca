# YMCA Platform

A centralized-yet-sovereign, multi-tenant platform for federated membership
organizations, developed against the YMCA movement as the reference domain.

Each association owns its people, money and facilities outright. No national
or global body may reach into another records by virtue of sitting above it
on an org chart — yet standards genuinely flow downward, compliance is
enforced, and one person may belong to several associations at once. The
platform serves all of this without becoming either a surveillance system or
a pile of disconnected single-tenant deployments.

## Start here

**The design is written; the implementation is not.**
[`docs/system-design/`](./docs/system-design/) is the full architecture
description (arc42 + C4 + ADRs + DDD). Read it before writing code:

| Read                                                                                                                                                 | If you are                                        |
| ---------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------- |
| [`docs/system-design/04_solution_strategy.md`](./docs/system-design/04_solution_strategy.md)                                                         | **everyone — start here**                         |
| [`docs/system-design/12_glossary.md`](./docs/system-design/12_glossary.md)                                                                           | reading anything else — the vocabulary is binding |
| [`docs/system-design/05_building_block_view/`](./docs/system-design/05_building_block_view/)                                                         | implementing a context                            |
| [`docs/system-design/08_crosscutting_concepts.md`](./docs/system-design/08_crosscutting_concepts.md)                                                 | touching auth, tenancy, data, or audit            |
| [`docs/system-design/A1_openfga_schema.md`](./docs/system-design/A1_openfga_schema.md) · [`A2_data_model.md`](./docs/system-design/A2_data_model.md) | implementing authorization or persistence         |

## The five principles

Every decision in the design descends from these. If a change violates one,
the change is wrong, not the principle.

1. **Tenancy is an immutable security boundary.** Every protected object
   belongs to a tenant; no authorization path resolves without a tenant in
   it. Cross-tenant authority exists only as an explicit, time-bounded,
   audited grant.
2. **Organizational relationship is not authority.** Five independent graphs —
   legal control, policy, compliance/sanction, administration, data access.
   Being "above" another body implies nothing about what you may do to it.
3. **Authorization answers entitlement; domain rules answer permission-now.**
   _Is this person entitled at all?_ is OpenFGA. _May this happen right now?_
   (suspended, full, window closed) is PostgreSQL and the policy engine.
4. **Roles grant capabilities; relationships establish scope; attributes
   supply context.** Role definitions may be global; role assignments are
   always scoped.
5. **Store the verdict, not the evidence.** Background checks, screening and
   safeguarding restrictions record the outcome and its validity — never the
   underlying record.

## Architecture

```txt
                        Identity Provider (OIDC)
                                 │ authenticated principal
                                 ▼
┌──────────────────────────────────────────────────────────────┐
│                        API Gateway                           │
│            tenant carried explicitly, never inferred         │
└───────────────┬───────────────┬──────────────┬───────────────┘
                ▼               ▼              ▼
        ┌──────────────┐ ┌──────────────┐ ┌───────────┐
        │   Domain     │ │Authorization │ │  Policy   │
        │   Services   │─│   Service    │ │  Engine   │
        │ (10 contexts)│ │              │ │           │
        └──────┬───────┘ └──────┬───────┘ └───────────┘
               ▼                ▼
        ┌──────────────┐ ┌──────────────┐
        │  PostgreSQL  │ │   OpenFGA    │
        │ business     │─│ relationship │
        │ state, RLS   │ | outbox graph │
        └──────────────┘ └──────────────┘
```

- **PostgreSQL** — system of record. Row-level security on `tenant_id`,
  exclusion constraints for allocation exclusivity, gapless invoice
  numbering. Business state only.
- **OpenFGA** — Zanzibar-style relationship graph. One store per instance,
  every object namespaced by tenant, every relation terminating at a tenant
  ancestor. No PII in tuples.
- **Transactional outbox** — the business mutation and its authorization
  tuple write commit in one PostgreSQL transaction; a dispatcher reliably
  applies to OpenFGA. Grants may lag (sub-second target); revocations are
  written synchronously before they take effect.
- **Policy engine** — policies are typed domain objects with known
  semantics, evaluated by domain services. Not executable code in the
  database.

Domain services **never implement their own authorization rules** — they call
`check(principal, permission, resource, context) → ALLOW | DENY`.

### Bounded contexts

Ten, each owning its data outright; none reaches into another's tables.
Detailed in [`docs/system-design/05_building_block_view/`](./docs/system-design/05_building_block_view/).

| Context      | Owns                                                  |
| ------------ | ----------------------------------------------------- |
| Organization | tenants, org units, affiliation, authority grants     |
| Identity     | Person, principals, guardianship, consent             |
| Membership   | plans, memberships, admission, entitlements           |
| Programme    | programmes, offerings, enrollment, occurrences        |
| Resource     | resources, windows, allocation, booking, stay         |
| Governance   | offices, office holding, committees, decision records |
| Policy       | policy definitions, inheritance, evaluation           |
| Finance      | invoices, charges, payments, reconciliation           |
| Safeguarding | verifications, clearances, restrictions               |
| Consumption  | consumption types, obligations, records, absence      |

Plus two crosscutting services: **Authorization** (owns the FGA model,
answers `check`, holds no business state) and **Platform** (tenant
provisioning, JIT privileged access, break-glass, platform audit).

## Intended stack

The design mandates only PostgreSQL and OpenFGA. The rest:

| Component                                       | Stack                   |
| ----------------------------------------------- | ----------------------- |
| Domain / authorization / policy services (APIs) | Go                      |
| Admin portal                                    | Flutter (web/desktop)   |
| Member & staff mobile app                       | Flutter (Android + iOS) |
| Authorization store                             | OpenFGA                 |
| System of record                                | PostgreSQL              |
| Identity                                        | external OIDC provider  |

## Deployment

Separate **regional platform instances** (IN, EU, US, …) answer data
residency — instances share code and schema, not data. Within an instance:
many tenants, one OpenFGA store, `Person` global to the instance. There is no
cross-instance federation. See
[`docs/system-design/07_deployment_view.md`](./docs/system-design/07_deployment_view.md).

## Repo layout

```txt
docs/system-design/   the architecture description — read this first
prototype/            archived YMCA Mess Management prototype (Go + KMP)
README.md             this file
```
