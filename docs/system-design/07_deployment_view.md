# 07 — Deployment View

## 7.1 Topology

```txt
┌─────────────────── REGIONAL INSTANCE ───────────────────┐
│                                                          │
│   API Gateway ──▶ Domain Services ──▶ PostgreSQL         │
│                        │                   │             │
│                        ▼                   │ outbox      │
│                 Authorization Svc ──▶ OpenFGA ◀──────────┤
│                        │                                 │
│                 Policy Engine                            │
│                        │                                 │
│                 Outbox Dispatcher                        │
│                                                          │
│   Adapters: payment · screening · notification           │
└──────────────────────────────────────────────────────────┘

    Many tenants per instance. One OpenFGA store per instance.
```

## 7.2 One store, many tenants

Sovereignty comes from the model, not from physical separation (ADR-031).

```txt
Mandatory tenant ancestor on every path      04.9 invariant 1
Namespaced object identifiers                A1.1
Cross-tenant authority only as explicit grants  ADR-019
```

Store-per-tenant was rejected: it makes cross-tenant grants unexpressible
without syncing tuples between stores, leaves platform-plane tuples homeless,
and multiplies schema migrations by tenant count — producing version drift
that is itself a security liability.

## 7.3 Regional instances

The answer to data residency (C12).

```txt
Instance IN     tenants with Indian residency requirements
Instance EU     GDPR residency
Instance US     ...
```

A tenant belongs to exactly one instance, determined by
`Tenant.jurisdiction`. **`Person` is global within an instance, not across
instances** — a human active in two regions has two Person records, which is
accepted as the cost of residency compliance rather than solved.

There is no cross-instance federation. Instances share code and schema, not
data.

## 7.4 Operational requirements

| Concern | Requirement |
|---|---|
| OpenFGA availability | Higher than the application; its loss fails security-critical operations closed |
| Outbox lag | Alert if undispatched rows exceed threshold — a stalled dispatcher means authority is not being granted |
| Model deployment | FGA model is versioned; assertions run in CI; deployed before dependent code |
| Migration | Forward-only; tuple rewrites batched and reversible |
| Backup | PostgreSQL and OpenFGA backed up consistently; a restore that desynchronizes them is an authorization incident |
| Audit retention | Longer than all other data classes; separate stream for high-severity |

## 7.5 Environments

```txt
Production     per region
Staging        one, with synthetic tenants exercising cross-tenant
               and federation scenarios
CI             ephemeral; FGA assertions and DB constraint tests
```

Staging must contain at least two tenants with an affiliation between them,
because the invariants that matter most are cross-tenant and cannot be tested
in a single-tenant fixture.
