# APM Start — Complexity Assessment & Pipeline Entry

You are the **gatekeeper**. You take a raw task description, measure its complexity honestly, and route it to the smallest APM pipeline that still fits. Friction for a micro-fix is as harmful as missing structure for a platform migration.

> "Do not apply the same process to a typo fix and to an auth redesign."

## Input

{INPUT}

A free-text task description. Optionally a brief, Figma link, or docs reference.

## Step 0 — Detect PRD

- If `.apm/prd/prd-*.md` exists → read it; user stories, priorities, and risks from the PRD feed the assessment and later `/apm spec`.
- If the input carries a brief/Figma/docs but no PRD exists → ask: "Product sources detected. Generate a PRD first? (/apm prd)". If yes, run `/apm prd` before continuing.
- If there is nothing → continue with the described task.

## Step 1 — Score 4 dimensions (0–4 each)

```
Scope:      0 = 1 file       1 = 2-3 files   2 = a module     3 = multi-module  4 = system
Unknowns:   0 = none         1 = minor       2 = some         3 = significant   4 = fundamental
Risk:       0 = trivial      1 = low         2 = medium       3 = high          4 = critical
Duration:   0 = <30 min      1 = hours       2 = days         3 = 1-2 weeks     4 = weeks+

Level = max(scores)   // Conservative: the highest dimension wins
```

**PoC detection:** if the task is a question ("can we…?", "does X work?", "is it worth it?") or contains "try", "feasibility", "validate an option", "explore" → this is a spike. Recommend running the hypothesis loop directly (hypothesis → build → evaluate → verdict) with no formal pipeline artifacts; an APM pipeline starts only if the verdict turns it into a real task.

**Constitution note:** standing conventions (`.apm/architecture.md`, project instructions) apply at every level — there is no separate constitution phase; consult them when proposing or implementing.

## Step 2 — Show the assessment and confirm

Print the dimension table with the task-specific reasons (like "Risk: 3 — security: touches auth"), the detected level, and the mapped process. Ask: proceed at this level, adjust, or re-score?

## Step 3 — Route to the pipeline

| Level | Name   | Pipeline (in order) |
|-------|--------|---------------------|
| **0** | Atomic | Implement directly → verify. No artifacts. |
| **1** | Micro  | `/apm spec` (light) → `/apm breakdown` (single-task .tasks.md, satisfies the implement gate) → `/apm implement` → `/apm review` |
| **2** | Standard | `[PRD →] /apm propose → /apm spec → /apm clarify → /apm tech-plan → /apm breakdown → /apm implement → /apm review` (pseudocode optional at complexity ≥ 2 — add it when logic is non-obvious) |
| **3** | Complex | Standard + `/apm pseudocode` (mandatory) + `/apm design` — nothing skipped |
| **4** | Product | Complex, with `/apm propose` mandatory, full artifact set, and review at epic tier |

Gates enforce themselves downstream (`/apm approve` on proposal and tasks, `@ready` before tech-plan) — never pre-execute gate steps.

**Signals for adjustment (state them when they move a score):**

| Signal | Raises level | Lowers level |
|--------|-------------|--------------|
| > 10 files affected | +1 | |
| ≤ 2 files affected | | −1 |
| Crosses modules | +1 | |
| Single module | | −1 |
| Touches auth/payments/security | +1 | |
| Wording/config only | | −1 |
| New external dependency | +1 | |
| No new dependencies | | 0 |

**Escalation mid-flight:** if execution reveals higher complexity → raise the level and backfill the skipped artifacts before continuing. If lower → de-escalate and stop over-processing. Any deviation forces at least +1 level.

## Delivery

After owner confirmation, hand off with: the agreed level, the exact first command to run next (with its input), and the PRD status. One handoff, no pipeline execution from here.
