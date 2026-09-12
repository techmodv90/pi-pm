# Lean Flow — Canonical Workflow

Adopted 2026-09-09 (commit series: `/apm start`, hash-bound approvals, lean-flow handoff).
This is the only authoritative process map. `apm-workflow-map.md` details the command
artifacts; this file defines the end-to-end pipeline and what is retired.

## End-to-end pipeline

```text
/apm init                 once per repo (.apm/ workspace)
   |
/apm start <task>         complexity assessment (scope/unknowns/risk/duration, 0-4 each,
   |                      level = max; PoC spikes route to the hypothesis loop)
   |
   +-- 0 Atomic   -> implement directly
   +-- 1 Micro    -> spec + single-task breakdown + implement + review
   +-- 2 Standard -> propose -> spec -> clarify -> tech-plan -> breakdown
   |                -> implement -> review
   +-- 3 Complex  -> adds pseudocode + design
   +-- 4 Product  -> full artifacts, mandatory propose, epic-tier review
   |
planning artifacts are flat files under .apm/
  (.apm/specs/**/*.feature, .plan.md, .tasks.md, .apm/design/, .apm/pseudocode/)
   |
/apm implement  ->  pic workflow import-apm --milestone <version>
   |                creates Work Items (epic / feature / task) from .tasks.md
   |
execution: implementation authorization -> worker (TIP frozen at first claim)
   -> child review -> completion report -> aggregate verification
   -> owner acceptance -> branch merge
```

Standing tools, usable at any point: `/apm explore`, `/apm distill`, `/apm drift`
(gaps become Bug Work Items), `/apm spec-validate`, `/apm spec-score`, `/apm approve`,
`/apm archive`.

## Retired: the durable planning ladder

The Work Item planning stages — `scan -> rri -> vision -> blueprint -> contracts ->
task_graph` with immutable artifact revisions and owner checkpoints per stage — are
**legacy flow**, superseded by the lean flow above. Evidence:

- Last planning artifact written 2026-09-08 07:06 UTC; zero planning-stage artifacts
  since. Zero Work Item events of any kind since 2026-09-08 10:30 UTC.
- All commits from 2026-09-09 onward are lean-flow command development.
- The `import-apm` path creates Work Items directly from `.apm/` artifacts and never
  runs the planning ladder.

Consequences for agents:

- Do **not** start new work through `task_manager` planning actions
  (`checkpoint_rri_interview`, `save_rri_interview`, `save_blueprint_draft`,
  `review_blueprint_checkpoint`, `approve_blueprint_draft`,
  `save_work_item_artifact` for planning stages, `materialize_work_item`).
  They remain wired but belong to the retired flow; removal is tracked in
  `docs/plans/deletion-ledger.json`.
- New tracked work enters via `/apm` commands and `import-apm`.

## What survives from the Work Item model

- Work Items as durable **execution** containers (epic / feature / task / bug / chore /
  gate), imported from `.apm/` artifacts.
- TIP freeze at first claim, isolated worktree workers, child review, completion
  reports, aggregate verification, owner acceptance, branch merge.
- Immutable, content-hashed artifact binding and the no-bypass debugging rule
  (never filter, relabel, or bypass persisted state to unblock a gate).
- `.apm drift` conformance findings becoming Bug Work Items.

## Superseded documents

- `docs/work-item-model-spec.md` — the staged aggregate workflow (Scan/RRI/Vision/
  Blueprint/Contracts/Task Graph) is retired; artifact immutability, TIP, verification,
  and acceptance semantics still apply.
- `docs/feature-workflow.md` — lifecycle diagram retired; execution semantics still apply.
- Decomposition policy v2 (`docs/plans/decomposition-policy-v2-plan.md`) — governs only
  legacy planning artifacts authored before 2026-09-09; the lean flow decomposes via
  `/apm breakdown` (.tasks.md with TDD markers).
