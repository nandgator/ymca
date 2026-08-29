# 10 — Quality Requirements

## 10.1 Scenarios, not adjectives

Each is observable and testable.

### Sovereignty

| Q | Scenario | Expected |
|---|---|---|
| Q1 | A federation body holding all authority verbs except `may_read_member_data` requests a member record | Denied, audited |
| Q2 | A platform operator queries a tenant's members without a JIT grant | Denied, audited |
| Q3 | A tenant admin queries another tenant's resources | Denied; no existence disclosed |
| Q4 | A cross-tenant grant reaches its expiry during an active session | Next check denies |

### Correctness under failure

| Q | Scenario | Expected |
|---|---|---|
| Q5 | OpenFGA unavailable; admin attempts a role assignment | Fails closed |
| Q6 | OpenFGA unavailable; member walks into the gym | Cached decision within TTL, logged |
| Q7 | Revocation issued; OpenFGA write fails | Transaction rolls back; failure reported |
| Q8 | Two members race for the last lane | Exactly one succeeds |
| Q9 | Outbox dispatcher stalls for 10 minutes | Alert raised; grants delayed, not lost |

### Temporal correctness

| Q | Scenario | Expected |
|---|---|---|
| Q10 | Office term expires at midnight; holder acts at 00:01 | Denied, without any sweeper having run |
| Q11 | Clearance lapses mid-session for an instructor | Next check denies |
| Q12 | Tenant changes cancellation policy after a booking | Booking retains its snapshot |

### Auditability

| Q | Scenario | Expected |
|---|---|---|
| Q13 | Support acts under impersonation | Both actor and delegated identity recorded |
| Q14 | "Why is minimum age 21 here?" | Provenance chain returned |
| Q15 | "Who approved this membership, under what authority?" | Decision record with participants |
| Q16 | A restriction is read | Read itself is audited |

### Adaptability

| Q | Scenario | Expected |
|---|---|---|
| Q17 | A tenant creates a resource type the platform never anticipated | Succeeds by declaring an archetype |
| Q18 | A tenant bundles pool into base membership; another does not | Both expressible without code change |
| Q19 | A jurisdiction has no registry screening | Capability disabled; platform fully usable |

## 10.2 Performance targets

Indicative, to be validated.

```txt
check()                      p99 < 25 ms
check() with validity        p99 < 40 ms
Entitlement resolution       p99 < 60 ms
Outbox dispatch              p99 < 500 ms
Policy resolution (cached)   p99 < 10 ms
```

Policy resolution is cached with invalidation on
`PolicyDefinitionActivated`, since the DAG walk is expensive and policy
changes are rare.
