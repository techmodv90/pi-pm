# APM Workflow — Full Command Map

## The lifecycle at a glance

```
                        ┌───────────── recurring / standing ─────────────┐
                        │                                                │
  /apm init ──► /apm prd ──► /apm explore ──► /apm propose ──► GATE ──► /apm spec
   (once)      (project)    (evidence)      (RFC, WHY)    owner ok   (Gherkin,
                                                                    WHAT, per cmd)
                                                                        │
                                                              /apm clarify   ← @draft → @ready gate
                                                                        │
                                                            /apm spec-validate (gate)
                                                            /apm spec-score   (measurement only)
                                                                        │
                                        ┌───────────────────────────────┤
                                        │ (complexity ≥ 2)              │
                                 /apm pseudocode                        │
                                 (logic sketch)                /apm tech-plan
                                        │                      (blueprint .plan.md, DBML,
                                 /apm design                    constitution gate)
                                 (ADRs, HOW/WHY)                     │
                                        └──────────┬────────────────┘
                                                   ▼
                                          /apm breakdown
                                          (.tasks.md, TDD markers)
                                                   │
                                            GATE: owner approval
                                                   ▼
                                          /apm implement
                                          (RED→GREEN→REFACTOR, per phase)
                                                   ▼
                                          /apm review (aggregate verify, RRI-T)
                                                   ▼
                                          owner acceptance
```

Standing/auxiliary tools, usable at any point:
- **`/apm explore`** — evidence-cited codebase scan, updates the single `.apm/architecture.md` in place; run before `propose` so proposals rest on verified facts.
- **`/apm distill`** — reverse-engineer specs from existing code (legacy comprehension, not forward planning).
- **`/apm drift`** — spec-vs-tests conformance check after implementation; gaps become Bug Work Items.

## Phase-by-phase

| # | Command | Input → Output | Gate before next step |
|---|---------|----------------|----------------------|
| 0 | `init` | — → `.apm/prd`, `.apm/specs` (tracked), `.apm/state` (ignored) | once per repo |
| 1 | `prd` | description/brief/Figma → `.apm/prd/prd-v<maj.min>.md` | owner review of PRD |
| 2 | `explore` | codebase area → section of `.apm/architecture.md` with `verified_at` coverage | none; refresh stale sections before proposing |
| 3 | `propose` | change idea + architecture facts → `.apm/proposals/<slug>-proposal.md` (Intent/Scope/Approach/Risks/complexity) | **PENDING APPROVAL** — owner must approve before any spec |
| 4 | `spec` | approved proposal → `.apm/specs/<domain>/<Name>.feature` (Gherkin scenarios) | — |
| 5 | `clarify` | `.feature` → ambiguity review + Auto-QA | **@draft → @ready flip only on passing verdict** |
| — | `spec-validate` | scope → PASS/WARN/ERROR | ERRORs block `tech-plan`/`breakdown` |
| — | `spec-score` | scope → 0–100 score (IEEE 830/ISO 29148) | none — measurement, not gate |
| 6 | `pseudocode` | domain's `.feature` files → `.apm/pseudocode/<slug>-pseudocode.md` (flows, invariants, edge cases; zero language constructs) | owner approval; optional below complexity 2 |
| 7 | `design` | `.plan.md`/proposal → `.apm/design/<feature>-design.md` (ADRs with rejected alternatives, diagrams, complexity table) | owner review → APPROVED FOR IMPLEMENTATION |
| — | `tech-plan` | `@ready` spec → `.plan.md` blueprint beside it (contracts, models, DBML ratification) | constitution gate |
| 8 | `breakdown` | approved blueprint → `.tasks.md` (TDD RED/GREEN/REFACTOR markers, parallelism) | **owner approval of task list** |
| 9 | `implement` | `.tasks.md` → executed phase by phase with native test/lint/build | human checkpoint per phase (`--dry` for preview) |
| 10 | `review` | implemented aggregate → evidence-cited verdict (Don Cheli dims 1–5, in-session RRI-T scenarios, acceptance brief) | FAIL blocks; PAINFUL/medium → Bug Work Items + owner deferral |
| 11 | acceptance | acceptance brief → owner accepts; branch merges per tier policy | owner only |

## The three hard gates

1. **`propose` → `spec`** — no Gherkin is written before the owner approves the RFC.
2. **`clarify` @draft → @ready** (and `spec-validate` errors) — no blueprint on an ambiguous spec.
3. **`breakdown` → `implement`** — no code before the owner approves the task list.

Plus the post-implementation gate: `review` verdicts — clean-with-evidence passes, blocking findings spawn corrective bugs, medium findings become deferrable Bug Work Items.

## Where pseudocode fits

Complexity ≥ 2 (Estándar) and flow-heavy domains: insert `pseudocode` between `clarify` and `tech-plan`/`design`. It settles decision order and invariants while they are still cheap; the design's ADRs may not contradict pseudocode invariants without a superseding note. Skip for thin CRUD and complexity-1 commands.

## Artifact map (all under `.apm/`, all tracked)

```
.apm/prd/prd-v1.0.md              project scope
.apm/architecture.md              single evolving architecture doc
.apm/proposals/<slug>-proposal.md RFCs
.apm/specs/features/<domain>/<Name>.feature Gherkin (source of truth)
.apm/pseudocode/<slug>-pseudocode.md logic sketches
.apm/specs/<domain>/<Name>.plan.md  blueprints
.apm/design/<feature>-design.md     ADRs
.apm/specs/features/<domain>/<Name>.tasks.md task lists
.apm/artifacts/<wi>/                RRI-T scenarios + review projections
```
