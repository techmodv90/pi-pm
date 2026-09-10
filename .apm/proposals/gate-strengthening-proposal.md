# Proposal: Approval Gates as Hash-Bound State

## Intent

The three APM gates (propose→spec, clarify @ready→plan, breakdown→implement) plus design review are currently **prose**: prompts ask the agent to check for owner approval, but nothing enforces it. Cheap to bypass, weak in practice. Goal: gates that are **cheap to satisfy** (one owner action) but **strong to bypass** (machine-checked, invalidation on drift).

Also adopted as part of this proposal's conventions (owner directive 2026-09-08):
- **Feature location:** Gherkin features live at `.apm/specs/features/<domain>/<Name>.feature` — the `features/` tier separates forward specs from distilled/reference artifacts.
- **Language:** all tags, labels, states, and CLI flags in English. No Spanish (`APROBADO`→`APPROVED`, `PENDIENTE`→`PENDING`, `critica`→`critical`, `umbral`→`threshold`). The Spanish remnants in existing prompts (propose, design, pseudocode, drift, spec-score, review) get swept to English when this spec lands.

## Root Cause (why the Spanish/paths happened)

Not an output typo — an adoption-policy gap:
1. dc commands declare `i18n: true` — built for translation.
2. Our adoptions kept source-language labels as "fidelity" instead of honoring the flag; no convention ruled either way, so each adoption decided ad hoc and Spanish leaked into propose/design/drift/spec-score.
3. Spec layout came from dc's `@specs/` ported as-is; without a documented schema, `features/` never existed and content mixed at one level.

Fixes at cause: adoption-language rule and layout schema now recorded as standing conventions in `.apm/architecture.md` (verified 2026-09-08); the English sweep + path templates in Scope below clear the existing debt. Future adoptions check conventions first.

## Scope

**In:** a shared gate-check helper for apm handlers; approval stamps bound to artifact content hash; one `/apm approve` command; auto-revert to draft on post-approval edit; English label sweep; `features/` path convention in spec/tech-plan/breakdown/drift prompts.
**Out:** new persistence (no DB, no state files — stamps live in the artifact itself); changes to the pi-ext Work Item pipeline gates (already machine-enforced: review verdicts, assertCleanGit, spec-validate); pseudocode/prd gates (advisory by design); renaming existing committed artifacts (their paths are referenced by Work Items and history).

## Approach

**Approval-as-state.** Same pattern `clarify` already uses for specs (`@draft` → `@ready`), generalized:

| Artifact | Unapproved | Approved |
|----------|-----------|----------|
| `.apm/proposals/<slug>-proposal.md` | `State: PENDING` | `State: APPROVED hash=<sha1>` |
| `.apm/specs/features/<domain>/<Name>.feature` | `@draft` | `@ready` (existing) |
| `.apm/design/<f>-design.md` | `State: DRAFT` | `State: APPROVED hash=<sha1>` |
| `.apm/specs/features/<domain>/<Name>.tasks.md` | `State: PENDING` | `State: APPROVED hash=<sha1>` |

Two mechanics make it strong:

1. **Enforcement at load** — `spec`/`tech-plan`/`breakdown`/`implement` handlers call one shared helper `assertGate(file, "APPROVED|@ready")` (~20 LoC in `apm-shared.ts`): parse state line, recompute content hash over the body, fail with a pointed error if stamp missing or hash mismatched.
2. **Invalidation on drift** — the stamp binds the hash of the *body* at approval time. Any later edit resets the gate: downstream command sees hash mismatch → "artifact changed since approval; re-approve". No stale approval can launder new content.

**Owner cost stays cheap:** `/apm approve <artifact>` prints a 5-line summary (what it gates, last-modified diff stat, open quality-gate verdicts) and flips the line on confirmation. One command, one keystroke-y reply. No forms, no DB, no ceremony.

**Why hash in the file, not a sidecar:** the artifact stays the single source of truth (Gherkin-is-King extends to approval-is-in-the-artifact); `git diff` shows approvals; no lookup machinery.

## Risks

- **Rubber-stamp risk rises** — one command to approve may tempt skipping the read. Mitigation: `approve` always prints the artifact's own quality-gate checklist results (spec-score, validate verdicts) before asking; skip-reading becomes the owner's explicit choice, not an accident.
- **Merge noise** — approval edits are one-line changes; rebase conflicts only when both sides edit the same line (rare, visible).
- **First-run friction** — existing artifacts without stamps are treated as unapproved (fail-closed), grandfather nothing.
- **Path migration** — features moving under `specs/features/` affects the location templates in spec/tech-plan/breakdown/drift prompts and the workflow map; drift's scope matching must accept both old and new paths during transition.

## Don Cheli complexity

| Component | Complexity | Notes |
|-----------|-----------|-------|
| `assertGate` helper in apm-shared.ts | low | parse + sha1, one function |
| `/apm approve` command + handler | medium | summary rendering, confirm flow |
| Handler wiring (5 commands) | low | one call each at top |
| English label sweep + path templates | low | mechanical, several prompts |
| Hash-mismatch error UX | low | message names the fix |
| **Hard part** | medium | hash canonicalization — must hash the exact body the downstream reads, or mismatch loops |

## Estimate

~150 LoC + tests. Complexity 2 (standard) — pseudocode optional; skip.

## State: APPROVED hash=fc62fa8836f6b310eef95615d8b707fe1ccb7865
