---
description: Aggregate peer review with RRI-T grading after all feature children close. Use when the user says "apm review", "review the feature", "aggregate review", "review the epic delivery". Runs the five deep review dimensions in-session, authors RRI-T scenarios without persona subagents, grades with executed evidence, and routes findings to individual bug Work Items instead of fix loops.
i18n: true
---

# /apm review

## Objective

Run the aggregate-level peer review for a delivery aggregate (feature or epic)
whose executable children are all done, reviewed, and contractor-verified.
Assume the role of **Software Architect / Lead Developer**. The review asks
what no child review can: *does the integrated branch satisfy the feature as a
whole?* — functional completeness against the spec, performance smells,
architecture/contract compliance, and security.

You never patch the reviewed aggregate. Findings become individual Bug Work
Items scheduled at the right containment level; blocking findings gate the
aggregate instead.

> Child patches were already reviewed per-task (task-reviewer, hash-bound).
> Do not re-review children. This review operates on the integrated branch.

## Branch tier policy (owner decision 2026-09-08)

- Feature-level review: evidence runs against the feature's delivery branch
  (`feat/*` or its parent feature branch); reviewed work merges there. Never
  touch `develop` at feature level.
- Epic-level verification: the epic branch (containing verified `feat/*`
  branches) merges into `develop`. Only aggregate verification at epic tier
  integrates to `develop`.

## Input

- target: {INPUT} — a Work Item ID (`wi-…`) at `next_stage:
  aggregate_verification`, or empty to discover it via
  `ready_work_items` / `show_work_item` on the branch-owning aggregate.

If the aggregate still has open or in_progress children, stop and report the
outstanding descendants — run `/apm review` only after they close.

## The five review dimensions

Skip style/lint and clean-diff checks: gofmt/vet gates and patch-hash scope
validation already enforce them harder than a checklist could.

### 1. Functional correctness (spec-anchored)

- Every P1 acceptance criterion (Given/When/Then) from the task graph and
  contracts demonstrated against the integrated branch, not the child worktree.
- Every requirement graded by at least one executed RRI-T scenario.
- No stubs: grep the integrated diff for TODO/FIXME/stub markers touching the
  feature surface.

### 2. Tests and behavior

- All required commands pass on the integrated branch (not just in child
  worktrees).
- Tests verify behavior, not implementation; no order/time/global-state
  dependence introduced by the feature.
- Nyquist validation: P1 scenarios fully covered, P2 scenarios ≥ 80%.

### 3. Performance smells

Hunt explicitly, and report file:line for each hit:

- N+1 queries (loop issuing one query per item)
- Avoidable O(n²) over datasets the feature owns
- Missing pagination on queries that can grow
- Large objects held in memory without release
- Redundant serialization/deserialization
- Missing indexes on frequent query paths

### 4. Architecture and contracts

- Every contract obligation's `class` and Blueprint-declared `seam` honored by
  the integrated code.
- No cross-module coupling introduced beyond declared seams.
- Dependencies injected, not hardcoded; concerns separated per the Blueprint.

### 5. Security

- Input validated at every new trust boundary.
- Queries parameterized (no string-built SQL — including Go raw strings that
  interpolate values).
- No credentials or secrets in code or logs; no sensitive data in event
  payloads or log lines added by the feature.

## Verdict discipline (no rubber-stamping)

Every dimension verdict must cite evidence: a command run and its observed
output, or a file:line inspection result. A bare "looks fine" is not a
verdict. There is no mandatory-finding quota — a dimension with clean cited
evidence is clean.

## RRI-T: author in-session, persist, then grade

RRI-T is a methodology, not a cast. No persona subagents are spawned.

1. **Author** — in this session, derive risk-relevant scenarios from the
   approved requirements: persona lens (coverage label, not a character:
   End User / Business Analyst / QA Tester / Developer / Operator) × dimension
   (D1–D7) × stress axis (TIME / DATA / ERROR / COLLABORATION / EMERGENCY /
   SCALE / COMPLIANCE / EVOLUTION). Select risk-relevant combinations rather
   than enumerating every cell; mark omitted areas N/A with a reason. Each
   scenario needs `id`, `persona`, `dimension`, `stress_axis`,
   `requirement_id`, `procedure` (a concrete command plus expected observable),
   and `remediation_hint`.
2. **Persist (save-before-grade)** — save the authored list with
   `save_work_item_artifact` stage `rri_t_scenarios` as JSON:
   `{personas, scenarios: [{id, persona, dimension, stress_axis, requirement_id, procedure, remediation_hint}], not_applicable, open_blockers}`.
   Grading binds to the persisted artifact only; an unpersisted list fails
   closed.
3. **Grade** — execute each retained scenario's procedure against the
   integrated branch in this session and record the command plus observed
   output as evidence. Each scenario gets exactly one outcome: PASS,
   ACCEPTABLE, PAINFUL, or FAIL — or `not_applicable` with a concrete reason
   when the procedure cannot run. Never grade a scenario you did not execute.

The review dimensions produce the evidence; the RRI-T outcomes are how it is
recorded and gated. Severity maps to outcome:

| Review finding severity | RRI-T outcome | Consequence |
|---|---|---|
| Blocking (data loss, security, contract violation) | FAIL | blocks aggregate verification; corrective Bug with owner approval gate |
| Medium (perf smell, missing edge case, rough seam) | PAINFUL | fix now or owner defers via Bug ticket |
| Minor, accepted tradeoff | ACCEPTABLE | requires explicit owner tradeoff |
| Clean with cited evidence | PASS | — |

## Findings become Bug Work Items, not fix loops

Findings do not loop back into the reviewed aggregate.

- **Blocking finding** → call `verify_aggregate_work_item` with
  `verification_status: failed`; the corrective Bug is created automatically
  with its requirement and acceptance, and waits for an explicit owner
  decision.
- **Non-blocking finding** → create a Bug Work Item
  (`create_work_item`, type `bug`) carrying: the verification report context,
  the finding with file:line, repro/evidence, severity justification, and a
  shrunken Given/When/Then acceptance for just the finding. Then verify the
  aggregate as `partial` and ask the owner to defer the open bugs explicitly.
  Scoping:
  - Bug touches one feature's files/seams → child of the feature.
  - Bug crosses features or lives in shared code → child of the epic. State
    the blast-radius reason; it determines the dependency graph.
  - Set a `blocks` relation when the bug gates other ready work.
- Severity gate: never defer a FAIL-grade finding via ticket. A ticket is not
  a place to hide a broken invariant while the branch merges.
- Deduplicate against existing backlog before creating; no compensating tickets
  for clean dimensions.

After the aggregate is accepted and merged, deferred bugs are plain ready Work
Items on `develop` — own branch, own worker, own reviewer, own verification.

## Process

```
1. GATE          — all descendants done/passed/verified; else stop and report
2. REVIEW        — run dimensions 1–5 against the integrated branch with
                   cited evidence
3. RRI-T         — author scenarios in-session → save artifact → execute and
                   grade with evidence
4. VERDICT       — passed (all PASS) / partial (deferred bugs ticketed) /
                   failed (blocking finding, corrective bug) via
                   verify_aggregate_work_item with the graded JSON
5. HANDOFF       — owner acceptance is the owner's call; never call
                   accept_aggregate_work_item yourself
```

## Guardrails

- Never patch, commit, or re-integrate the reviewed aggregate — the review is
  read-only over the integrated branch plus artifact writes.
- Never grade a scenario without executed evidence from this session.
- Never defer a blocking finding to a bug ticket.
- Never approve with uncited verdicts.
- Owner acceptance stays with the owner; report and stop.
