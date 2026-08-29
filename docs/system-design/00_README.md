# YMCA Platform — Software Design Document

A centralized-yet-sovereign, multi-tenant platform for federated membership
organizations, developed against the YMCA movement as the reference domain.

---

## What this document is

This is an architecture description following the **arc42** template, with:

- **C4** for structural diagrams (context → container → component)
- **ADRs** (Architecture Decision Records) for the decision log
- **DDD** vocabulary for the domain model (bounded contexts, aggregates,
  ubiquitous language)

Everything here is docs-as-code: plain Markdown, kept in version control,
reviewed by diff.

## What this document is not

It is not a requirements specification, a project plan, or an implementation
guide. It records **decisions and their rationale** so that a reader arriving
in two years can understand why the system is shaped this way — and, more
importantly, which constraints they must not casually break.

---

## Reading guide

Depth is deliberately uneven. Sections carrying architectural risk are written
to implementation-ready detail; sections that merely orient the reader are
kept short. Uniform depth is how design documents become unreadable.

| Section | Depth | Read if you are |
|---|---|---|
| 01 Introduction and Goals | terse | new to the project |
| 02 Constraints | terse | assessing regulatory exposure |
| 03 Context and Scope | terse | drawing the system boundary |
| 04 Solution Strategy | full | **everyone — start here** |
| 05 Building Block View | implementation-ready | implementing anything |
| 06 Runtime View | full | implementing a workflow |
| 07 Deployment View | terse | operating the system |
| 08 Crosscutting Concepts | implementation-ready | touching auth, data, or audit |
| 09 Architecture Decisions | decisions + rationale | asking "why is it like this?" |
| 10 Quality Requirements | terse | testing or benchmarking |
| 11 Risks and Technical Debt | terse | planning |
| 12 Glossary | terse | reading anything else |
| A1 OpenFGA Schema | implementation-ready | implementing authorization |
| A2 Data Model | implementation-ready | implementing persistence |

**Shortest useful path:** 04 → 12 → 05 → 08.

---

## File layout

```txt
00_README.md                     this file
01_introduction_and_goals.md
02_constraints.md
03_context_and_scope.md
04_solution_strategy.md
05_building_block_view/
    05.0_overview.md
    05.1_organization.md
    05.2_identity_and_person.md
    05.3_membership.md
    05.4_programme_and_offering.md
    05.5_resource_and_consumption.md
    05.6_governance_and_office.md
    05.7_policy.md
    05.8_finance.md
    05.9_safeguarding_and_verification.md
06_runtime_view.md
07_deployment_view.md
08_crosscutting_concepts.md
09_architecture_decisions.md
10_quality_requirements.md
11_risks_and_technical_debt.md
12_glossary.md
A1_openfga_schema.md
A2_data_model.md
```

One file per arc42 section, so that section numbers and filenames agree and
a change to one section produces a diff touching only that section.

---

## The five principles

Everything in this document descends from these. If a proposed change
violates one, the change is wrong, not the principle.

**1. Tenancy is an immutable security boundary.**
Every protected object belongs to a tenant. No authorization path resolves
without a tenant in it. Cross-tenant authority exists only as an explicit,
time-bounded, audited grant.

**2. Organizational relationship is not authority.**

```txt
organizational relationship
        ≠  legal / control authority
        ≠  policy authority
        ≠  compliance / sanction authority
        ≠  data-access authority
```

Five independent graphs. Being "above" another body on an org chart implies
nothing about what you may do to it.

**3. Authorization answers entitlement; domain rules answer permission-now.**

```txt
AUTHORIZATION   is this person entitled or related at all?
DOMAIN RULES    may this operation happen right now?
```

This recurs at every layer — membership, resource use, booking. It is what
stops the authorization store from becoming a second copy of the business
database.

**4. Roles grant capabilities; relationships establish scope; attributes
supply context.**
Role definitions may be global. Role assignments are always scoped.

**5. Store the verdict, not the evidence.**
For background checks, registry screening and safeguarding restrictions, the
platform records the outcome and its validity — never the underlying record.
The attestation is what authorization needs; the evidence is a liability.

---

## Provenance

The decisions recorded here were established through a structured design
interrogation of roughly 100 questions across 33 rounds. Section 09 preserves
the original question numbers so any decision can be traced to the exchange
that produced it.

Domain facts were verified against primary sources — association membership
pages, national council material, and published safeguarding standards —
rather than assumed. Where sources conflict across associations (they
frequently do), the divergence is itself recorded as a requirement for
tenant configurability.

## Status

| | |
|---|---|
| Foundation | closed |
| Domain model | complete — all nine contexts |
| Concrete schemas | complete — A1, A2 |
| Decision log | 95 ADRs, all traceable |
| Open questions | 14 known gaps, 3 unresolved tensions — section 11 |

All twelve arc42 sections and both appendices are written. What remains is
validation against real associations, not further design.
