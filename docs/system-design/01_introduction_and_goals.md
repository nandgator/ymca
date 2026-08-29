# 01 — Introduction and Goals

## 1.1 What this system is

A multi-tenant platform serving a federated movement of legally independent
membership associations. Each association administers its own people,
memberships, facilities and finances. A central operator runs the platform
and holds narrowly defined global authority that does not include access to
tenant data.

Developed against the YMCA movement as the reference domain: a global
federation of national movements and independent local associations, with
divergent constitutions, plan catalogues, facilities and jurisdictions.

## 1.2 Requirements overview

| # | Requirement |
|---|---|
| R1 | Each association administers its own domain without platform or federation interference |
| R2 | Federation bodies may publish standards and enforce compliance without data access |
| R3 | A person may hold relationships with several associations under one identity |
| R4 | Members, staff, volunteers, hostellers and guests coexist as relationships on one person |
| R5 | Associations configure their own plans, entitlements, resources and policies |
| R6 | Facilities are consumed by booking, continuous stay, or walk-in |
| R7 | Constitutional governance — offices, terms, approval authority — is representable |
| R8 | Safeguarding obligations are enforced without the platform holding case material |
| R9 | Money is handled across jurisdictions, providers, and offline methods |
| R10 | Every consequential act is auditable |

## 1.3 Quality goals

Ranked. Where they conflict, higher wins.

| Rank | Goal | Why it ranks here |
|---|---|---|
| 1 | **Tenant sovereignty** | The premise. A breach of it is not a bug but a failure of the product's reason to exist |
| 2 | **Auditability** | Charitable governance and safeguarding both require answering "who did what, under what authority" years later |
| 3 | **Correctness under failure** | Authorization that degrades open is worse than authorization that stops |
| 4 | **Adaptability** | Practice diverges so widely that an unconfigurable platform is unusable outside its first tenant |
| 5 | **Comprehensibility** | A model nobody can hold in their head will be violated by well-meaning changes |
| 6 | Performance | Genuinely matters; ranks below the above |

Note what is **not** in this list: feature breadth. The design repeatedly
chooses to model less (no elections, no case management, no ledger) in order
to model the remainder correctly.

## 1.4 Stakeholders

| Stakeholder | Concern |
|---|---|
| Member | Join, pay, book, participate; know their data is not shared |
| Guardian | Consent for a minor, receive communications, act on their behalf |
| Association admin | Run their association without asking permission |
| Office holder | Exercise constitutional authority for a bounded term |
| Federation body | Set standards, verify compliance, sanction — without intruding |
| Platform operator | Run the platform; provision, support, secure |
| Safeguarding lead | Ensure screening obligations are met and enforced |
| Auditor / regulator | Reconstruct any decision and its authority |
| Implementer | Understand the model well enough not to break it |
