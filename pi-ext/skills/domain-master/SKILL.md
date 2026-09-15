---
name: domain-master
description: "Decompose a complex backend into clean, modular Go domains. Use when turning a raw feature list into a package structure: identifying bounded contexts from nouns/verbs, deciding which package owns a noun ('where does Profile live'), replacing cross-package struct imports with primitive ID references, ranking the module dependency hierarchy, escaping the 'internal/admin' garbage-folder trap, or decoupling cross-module side effects. Trigger on: 'domain decomposition', 'bounded context', 'which package/domain should this live in', 'module dependency hierarchy', 'packages are tangled', 'decouple modules', 'review internal/ layout', 'split into domains'. This is Go backend package architecture — do not load it for generic OOP refactoring, frontend structure, or bug fixes."
version: 1.0.0
adopted_from: owner-provided guide (2026-09-13)
tags: [go, ddd, domain-decomposition, architecture, packages, backend]
---

# Domain Master (Go Domain Decomposition)

Breaking down a complex business into clean, modular Go domains combines
business analysis with code architecture. Work the six steps in order for
every new requirement; the result is a backend that stays decoupled,
compile-safe, and scalable as the feature list grows.

> Where this skill conflicts with the project's canonical terms
> (`CONTEXT.md` Module/Interface/Seam) or the ADRs, the canonical terms win.

## Step 1: The "Ubiquitous Language" Audit (Bounded Contexts)

Never start by looking at data or drawing database diagrams. Start with
words. Go through the app requirements and highlight the main nouns and
verbs. Group them by who uses them and what they mean in that specific
context.

**The rule:** if a single noun (e.g. "Profile") has radically different
fields or rules depending on who is looking at it, it needs to be broken
into separate domains.

Application example:

- A user updating their "Profile" cares about their work history and CV
  file → **Profile Domain**.
- An admin handling a "Profile Claim" cares about matching tax IDs, legal
  validation, and audit logging → **Review/Audit Domain**.

Result: separate `internal/profile` from `internal/review`.

## Step 2: Establish Single Sources of Truth (Eliminate Structural Duplication)

When entities rely on each other, do not nest them as code objects. Enforce
references strictly by primitive IDs (strings/UUIDs).

**The rule:** Package A can import Package B, but Package B must never
import Package A. If you need to fetch nested data, do it in the HTTP
handler layer or an orchestration layer — never inside the core domain
file.

**Action item:** go through your `entity.go` files. If you see an imported
struct type from another package (e.g. `Company company.Company` inside
`profile.ExpertProfile`), change it to an ID string (`CompanyID string`).

## Step 3: Draw the Dependency Hierarchy (Top-to-Bottom Flow)

Rank modules from independent/upstream (no one else needed to exist) to
dependent/downstream (exists only because other things exist). Example
shape:

1. **Independent core:** `company` — companies can exist as seeded data
   without anything else.
2. **User centric:** `profile` — requires a user identity, references a
   `CompanyID`.
3. **Marketplace/action:** `job` — requires a `CompanyID`, searches across
   `ProfileID`s.
4. **Cross-cutting workflows:** `review` — requires a `ProfileID` or
   `CompanyID` to execute tickets.
5. **Read-only ingestion:** `analytics` — consumes IDs from everywhere to
   draw charts.

**The rule:** code can only look up the hierarchy ladder, never down.

## Step 4: Isolate Administrative Workflows (The "Admin" Trap)

A common mistake is creating an `internal/admin` package. It turns into a
garbage-collection folder for every feature in the app.

**The rule:** "Admin" is a role, not a domain.

**Action item:** if an admin is doing something to an existing feature
(like running a headhunt search), that logic belongs in the `job` or
`profile` domain under an administrative permission check. If the admin is
driving a completely separate multi-stage process (like approving profiles
or vetting claims), give that workflow its own home (e.g. `internal/review`).

## Step 5: Inversion of Control for Cross-Module Side Effects

When an action in Module A must kick off a reaction in Module B, do not let
Module A call Module B directly. Use interfaces or light message streams.

**The rule:** instead of hard-coding a database write to another package,
define a local interface or fire a structured internal payload event.

**Action item:** if a finalized profile review inside `internal/review`
needs to toggle the live status of an expert in `internal/profile`, define
a listener inside `profile` that handles the message. The review module
remains completely ignorant of what `profile` does with that information.

## Step 6: Map to the Directory Layout

Once the logic is isolated, lay the folders flat within Go's package
boundary rules. Every feature folder contains its domain logic, its local
application service, and its database queries file directly under it:

```text
internal/
└── <feature_name>/
    ├── entity.go       # Pure structures & specific rules for this context
    ├── repository.go   # Interface outlining database capabilities
    ├── service.go      # Business use-cases (the orchestration logic)
    ├── db_queries.go   # Real sqlc/postgres/gorm implementation blocks
    └── http_handler.go # The openapi-codegen strict endpoint adapters
```

## Pairing

- `domain-modeling` — canonical vocabulary and ADRs; settles *terms* before
  this skill settles *packages*.
- `codebase-design` — Module/Interface/Seam theory when the decomposition
  introduces or changes interfaces, not just folder boundaries.
