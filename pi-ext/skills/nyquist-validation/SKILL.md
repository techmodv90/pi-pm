---
name: nyquist-validation
description: "Pre-coding coverage gate: verify every extractable requirement from the spec and plan has at least one mapped test or registered verification debt before implementation starts. Activate when the user says: 'nyquist', 'spec coverage', 'requirement test mapping', 'verification debt', or when an approved task list (.tasks.md) is about to be implemented."
version: 1.0.0
adopted_from: don-cheli-sdd habilidades/validacion-nyquist (v1.0.0, itself adapted from gsd-build/get-shit-done Nyquist Validation Layer)
tags: [quality, nyquist, tests, coverage, requirements]
gate_position: "after /apm breakdown, before implementation begins"
---

# Nyquist Validation (Testability Pre-Coding)

> Pre-coding coverage gate: no implementation starts until every verifiable
> requirement is mapped to a verification, or its debt is explicitly
> registered. Where this skill conflicts with the canonical Work Item
> workflow (AGENTS.md, `.apm/prd/prd-v*.md`), the canonical workflow wins.

## Problem It Prevents

TDD proves tests exist for what was remembered. It does not prove every
requirement was remembered. Nyquist catches "we built it but cannot measure
whether it works" **before** code is written.

> Signal theory: to capture a signal faithfully you must sample at twice its
> frequency. To capture requirements faithfully you need at least one
> verification per requirement.

```
Without Nyquist:
  plan → implement → "does it work?" → "unknown — no test covers that"

With Nyquist:
  plan → requirement↔verification mapping → 100%/80%/50% covered?
       → implement → verify against the same mapping
```

## Gate Position

```
/apm breakdown → .tasks.md (Draft → Approved)
  → [NYQUIST VALIDATION] ← this skill
  → Work Item implementation (materialize → authorize → worker run)
```

The gate fires between an approved task list and the start of implementation
work. The contractor runs it; the owner (or the gate mechanism loading this
skill) enforces its verdict before authorizing implementation.

## Process

### Step 1 — Extract Requirements

From the `.feature` (scenarios, success criteria) and the `.plan.md`
(invariants, API contract, data model, architecture, complexity tracking),
extract every verifiable requirement:

```markdown
## Extracted Requirements

| ID | Requirement | Source | Type |
|----|-------------|--------|------|
| R01 | Artifact save projects content to file_path, bytes equal content | ArtifactMarkdownFile.feature:Save scenario | Functional |
| R02 | Unwritable dir keeps canonical save, records warning event | ArtifactMarkdownFile.feature:Best-effort scenario | Functional |
| R03 | Projection adds < 50ms p95 to artifact save | ArtifactMarkdownFile.feature:Success criteria | Performance |
```

Every requirement carries a source reference (file:scenario/section) — no
requirement without provenance, no invented requirements. The `.feature`
remains the behavioral authority; the plan contributes technical
requirements (performance, security, data-model constraints).

### Step 2 — Map Verifications

For each requirement, name the task (from the approved `.tasks.md`) and the
exact verification command that proves it:

```markdown
## Nyquist Mapping

| Requirement | Verifying task | Verification command | Status |
|-------------|----------------|----------------------|--------|
| R01 | T008/T010 | `go test ./cmd/pic -run TestArtifactFileProjection` | ✅ Covered |
| R02 | T009/T010 | `go test ./cmd/pic -run TestArtifactProjectionWarning` | ✅ Covered |
| R03 | — | — | ❌ Uncovered |
```

### Step 3 — Score Coverage

```markdown
## Nyquist Result

Total requirements: 3
Covered: 2
Uncovered: 1
Nyquist coverage: 67% (minimum: 100% for P1, 80% for P2, 50% for P3+)

❌ DO-NOT-ADVANCE — missing verifications:
  - R03 (Performance): add a p95 timing test for artifact save
```

### Step 4 — Close Gaps

Before implementation, append the missing RED tasks to the `.tasks.md` and
re-run Step 2 until thresholds pass:

```markdown
## Tasks Added by Nyquist

- [T025] [P1] RED: p95 timing test for artifact save (< 50 ms projection cost)
  - Files: `go-pic/cmd/pic/artifact_files_test.go`
```

## Coverage Thresholds

| Tier | Minimum coverage | Action if unmet |
|------|------------------|-----------------|
| **P1** (critical path) | 100% | ❌ DO-NOT-ADVANCE |
| **P2** (important) | 80% | ⚠️ WARNING — may advance with recorded justification |
| **P3+** (desirable) | 50% | ℹ️ INFO — register debt |

Tier comes from the spec's scenario priority tags (`@P1`/`@P2`/`@P3`); plan
derived requirements inherit the tier of the scenario they serve, defaulting
to P1 for cross-cutting technical requirements.

## Verification Types

Not every requirement is verified by an automated test:

| Type | Verification | Example |
|------|--------------|---------|
| **Automated** | Unit/integration test command | "duplicate save conflict returns error" |
| **Performance** | Automated benchmark/timing test | "projection adds < 50 ms p95" |
| **Visual** | Human checkpoint | "dashboard renders file_path correctly" |
| **Manual** | UAT checklist | "owner can archive artifacts from disk alone" |

Requirements with **Visual** or **Manual** verification are tagged
`[checkpoint-human]` and do **not** block this gate — but they must be
registered as verification debt. Never accept "will be tested manually" for
an automatable requirement; classify it Automated and map a command.

## Verification Debt

Requirements that cannot be verified automatically are recorded as debt, not
silently dropped:

```markdown
## Verification Debt

| Requirement | Type | Reason | Resolved when |
|-------------|------|--------|---------------|
| R-visual-01 | Visual | requires human review of rendered dashboard | owner checkpoint |
| R-manual-01 | Manual | requires live archive workflow | before release |
```

Debt entries flow forward: at verification time each open debt must be
resolved or explicitly re-registered — the debt ledger may not silently
shrink.

## Guardrails

- **Never** advance to implementation with P1 below 100% Nyquist coverage
- **Never** accept "will be tested manually" for automatable requirements
- **Never** invent a requirement that has no source in the `.feature` or
  `.plan.md`
- **Always** register verification debt for non-automatable requirements
- **Always** append missing tasks to the `.tasks.md` before implementation —
  gap closure changes the task list, never the spec or plan
