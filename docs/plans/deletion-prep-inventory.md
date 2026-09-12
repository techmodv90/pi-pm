# Deletion Prep — DEL-0001/0002 Blast Radius (2026-09-12)

Read-only inventory for the mechanical deletion pass. No changes made yet.
Windows still open; this table is what deletion executes from once owner gives go.

## Key finding: Go side is already 80% lean

`internal/work-item/artifacts.go` — `Stages = ["rri_t_scenarios"]` only. The
contractor's in-session RRI-T scenarios during `/apm review` are the one retained
supplementary artifact; the save path already rejects all planning stages. Lean
flow touched Go more than expected: the planning ladder survives mostly in
`internal/profile` and schema/migration history.

## Go

| Symbol / surface | File | Callers | Verdict |
|---|---|---|---|
| `PlanStagesForProfile`, `ComputePlanStages`, `LifecycleStagesByName`, `DepthInfo` | `internal/profile/profile.go` | `profile.Ensure` → `pipeline/claim.go:82` (claim-time profile resolution) | **Redirect**: `Ensure` is live execution authority (profile version/hash binding at claim). Strip planning-stage selection; lifecycle stays plan/implement/qa or collapse to implement/qa. Delete tiering. |
| `profile.LifecycleForStage` | `internal/profile/profile.go` | `pipeline/claim.go:78` | **Keep** (used for live claim binding); delete `plan` branch if planning claims become impossible. |
| `profile.Ensure`, `ProfileList` | `internal/profile/profile.go` | `claim.go`, `profile-list` CLI | **Keep.** |
| `stage.Stages` + `stage.Contains` | `internal/stage/stages.go` | `profile.go` only | **Delete** after profile strip (comment already says legacy, render-only). |
| Planning branch in `pipeline/claim.go` (`lifecycle == "plan"`) | `internal/pipeline/claim.go:90-105` | scheduler plan-stage claims | **Delete** — post-cutover no plan-stage claims exist (last plan-stage run 2026-09-07). |
| `work_item_artifacts` planning-stage rows | DB | — | **Keep** (audit history), same as relic tables policy. |
| `rri_t_scenarios` stage | `artifacts.go`, `lifecycle.go`, `pipeline/rri-t.ts` | `/apm review` RRI-T scenarios | **Keep** — lean-flow feature. |
| `pipeline_runs` planning rows | DB | — | **Keep** rows (audit); `pipeline_runs` table itself still live for worker/review/autofix. **Not** a relic table. DEL-0003 correction below. |
| `workflow_checkpoints`, `work_item_materializations` | schema | `tip.go`, `execution.go` | **Keep** — TIP freeze lineage reads task_graph checkpoints (dead post-DEL-0001 but read-path kept until TIP binding source is re-verified at deletion time). |
| `ParseTaskPlanJSON`, TIP generation from planning lineage | `internal/tip/tip.go` | first-claim TIP freeze | **Redirect** — verify what feeds TIP content post-ladder (`import-apm` creates items without checkpoints; TIP source must be `.tasks.md` provenance). Open question, not a silent delete. |
| `ValidatePlanningDepth` / depth defaults | `schema/`, `work_items` table | `import.go` writes `'full'` | **Simplify**: lean flow has no depth concept; column becomes inert metadata. Keep column (additive-migration rule), stop writing/reading meaningfully. |

## TypeScript

| Symbol / surface | File | Verdict |
|---|---|---|
| `PLANNING_STAGES`, planning stage resolution | `tasking/workflow-modes.ts`, `pipeline/stage-resolution.ts` | **Delete** planning ladder branches; keep worker/review/autofix resolution. |
| RRI interview handlers | `api/rri-interview.ts`, `tool.ts` cases `checkpoint_rri_interview`/`load_rri_interview`/`save_rri_interview` | **Delete** (DEL-0002). |
| Blueprint draft loop | `core/blueprint-drafts.ts` (16.4K+36.8K tests), `api/blueprint-approval.ts`, `tool.ts` cases `save/load_blueprint_draft`, `review_blueprint_checkpoint`, `approve_blueprint_draft` | **Delete** (DEL-0002). |
| `save_work_item_artifact` / `approve_work_item_artifact` / `load_planning_artifact` / `reject_work_item_scan` / `amend_work_item_planning` / `reset_work_item_planning` / `validate_work_item_graph` / `materialize_work_item` | `api/tool.ts` | **Delete** cases + Go handlers (DEL-0002); keep `reset_work_item_execution`, all execution/verify/accept/merge actions. |
| Planning prompt machinery | `tasking/workflow-stage-prompts.ts`, `tasking/stage-primer.ts`, `pipeline/stage-prompts.ts` planning sections | **Delete** planning stages; keep worker/review prompts. |
| `agent-capabilities.ts` planning references | `tasking/agent-capabilities.ts` | **Triage** at deletion. |
| `finish.ts` blueprint references | `pipeline/finish.ts` | **Triage** — likely reviewer five-check text referencing blueprint artifacts. |

## DB (DEL-0003 correction)

| Table | Production readers | Verdict |
|---|---|---|
| `task_items`, `tasks`, `task_materializations`, `task_phase_metadata`, `epic_events`, `task_events` | Go: schema bootstrap only (`canonical.go` CREATE statements, kept for re-run safety). TS: zero. | Confirmed zero production readers. 90-day drop window stands (started 2026-09-12). |
| `pipeline_runs` | **Live** — worker/review/autofix runs (268 completed). | **Remove from DEL-0003**; it is not a relic. Only its planning-stage rows are history. |

## Open question before deletion executes

1. **TIP content source post-ladder**: `tip.go` binds TIPs to task_graph checkpoint
   lineage. Imported items have no checkpoints. Verify how TIPs materialize for
   imported work items today (first live lean-flow import will answer; if the path
   is broken, deletion must include the TIP-source redirect to `.tasks.md`
   provenance).
2. **Depth column fate**: inert metadata vs dropped from writes (keep column per
   additive-migration rule either way).
