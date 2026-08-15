# Work Item Model Implementation Plan

Spec sufficient: [work-item-model-spec.md](../work-item-model-spec.md) defines the model, lifecycle gates, migration constraints, and 23 acceptance criteria. This plan preserves historical rows and artifact hashes, uses the Go CLI as the only mutation authority, and cuts over the Go CLI, Pi extension, and Svelte dashboard in one release.

## Tasks

1. `go-pic/cmd/pic/main.go`, `go-pic/cmd/pic/workflow_schema.go`, `go-pic/cmd/pic/pic_cli_test.go` — Add the canonical `work_items` table, typed parent relationships, blocking dependency/gate tables, artifact revision/hash fields, approved checkpoints, and implementation-authorizations; add indexes for parent/status/claim/blocker queries and an idempotent migration that preserves existing IDs and artifact rows byte-for-byte. Test: `cd go-pic && go test ./cmd/pic -run 'TestWorkItemSchemaMigration|TestWorkflowMigrationPreservesLegacyRows'`.
   Blocked by: none.
   Parallel with: none; this establishes the persisted contract used by every later task.
   Acceptance: A legacy fixture migrates twice without changing row counts, IDs, historical hashes, timestamps, reports, or decisions, and `PRAGMA foreign_key_check` returns no rows.

2. `go-pic/cmd/pic/work_items.go` (new), `go-pic/cmd/pic/main.go`, `go-pic/cmd/pic/pic_cli_test.go` — Add `pic work-item create|list|show|update|status` for `epic`, `feature`, `task`, `bug`, `chore`, and `gate`; validate types, parent eligibility, arbitrary aggregate depth, and containment cycles transactionally. Test: `cd go-pic && go test ./cmd/pic -run 'TestWorkItemCRUD|TestWorkItemContainmentValidation'`.
   Blocked by: 1.
   Parallel with: 3.
   Acceptance: Aggregate children can nest, executable leaves reject children, cycle attempts roll back, and all reads return one consistent Work Item shape.

3. `go-pic/cmd/pic/work_items.go`, `go-pic/cmd/pic/pic_cli_test.go` — Implement indexed derived readiness and atomic claim rules from status, claim, deferral, blocking dependencies, and gate satisfaction; reject aggregate worker claims. Test: `cd go-pic && go test ./cmd/pic -run 'TestWorkItemReadiness|TestWorkItemClaim'`.
   Blocked by: 1.
   Parallel with: 2.
   Acceptance: Blocked or gated leaves never appear in ready results, become claimable when the last blocker closes, and readiness is not stored as mutable state.

4. `go-pic/cmd/pic/workflow.go`, `go-pic/cmd/pic/workflow_schema.go`, `go-pic/cmd/pic/epic_feature.go`, `go-pic/cmd/pic/pic_cli_test.go` — Replace the combined feature status calculation with revision/hash-bound gates for Scan acceptance, RRI approval, Vision approval, Blueprint approval, Contract approval, task-graph approval, and implementation authorization; invalidate only downstream artifacts when an upstream revision changes. Test: `cd go-pic && go test ./cmd/pic -run 'TestWorkItemArtifactGateSequence|TestWorkItemDownstreamInvalidation'`.
   Blocked by: 1, 2.
   Parallel with: 5.
   Acceptance: Each stage unlocks only its successor, stale or unbound approvals are rejected, and revising Blueprint invalidates Contracts onward without invalidating accepted Scan, RRI, or Vision revisions.

5. `go-pic/cmd/pic/workflow.go`, `go-pic/cmd/pic/workflow_schema.go`, `go-pic/cmd/pic/pic_cli_test.go` — Persist Blueprint, Contracts, and task graphs as separate immutable versioned artifacts with content hashes and owner decisions; retain existing `designs` rows as immutable migration history. Test: `cd go-pic && go test ./cmd/pic -run 'TestSeparateDesignArtifactRevisions|TestLegacyDesignHistoryPreserved'`.
   Blocked by: 1, 2.
   Parallel with: 4.
   Acceptance: Blueprint, Contract, and graph revisions can be proposed and approved independently, and no approval can bind to a different revision or hash.

6. `go-pic/cmd/pic/instruction_packs.go`, `go-pic/cmd/pic/epic_feature.go`, `go-pic/cmd/pic/pic_cli_test.go` — Parse and validate the approved task-graph artifact into aggregate and executable nodes, including type, parent, dependencies, requirement assignments, source provenance, bounded scope, and verification; reject cycles, unknown requirements, ambiguous leaves, and mechanical checklist promotion before writes. Test: `cd go-pic && go test ./cmd/pic -run 'TestApprovedTaskGraphValidation|TestTaskGraphRejectsInvalidLeaves'`.
   Blocked by: 3, 4, 5.
   Parallel with: none.
   Acceptance: A valid graph passes without mutating storage, while every invalid graph leaves Work Items, dependencies, checkpoints, and TIPs unchanged.

7. `go-pic/cmd/pic/epic_feature.go`, `go-pic/cmd/pic/instruction_packs.go`, `go-pic/cmd/pic/pic_cli_test.go` — Replace Epic task-plan materialization with one atomic, idempotent graph materialization that creates aggregate children, executable leaves, blocking edges, provenance, the approved checkpoint, and immutable inactive TIP revisions. Test: `cd go-pic && go test ./cmd/pic -run 'TestWorkItemGraphMaterialization|TestWorkItemGraphMaterializationRollback'`.
   Blocked by: 6.
   Parallel with: none.
   Acceptance: Repeating materialization creates no duplicates, injected late failures roll back every write, and no resulting TIP is active.

8. `go-pic/cmd/pic/instruction_packs.go`, `go-pic/cmd/pic/pipeline.go`, `go-pic/cmd/pic/acceptance.go`, `go-pic/cmd/pic/pic_cli_test.go` — Add explicit implementation authorization that activates current TIPs, bind new pipeline evidence directly to TIP revision/content hash, and enforce conditional leaf verification/acceptance plus aggregate closure rules. Test: `cd go-pic && go test ./cmd/pic -run 'TestImplementationAuthorization|TestWorkItemExecutionAndClosureGates'`.
   Blocked by: 3, 7.
   Parallel with: 9.
   Acceptance: Materialized leaves cannot launch before authorization; authorized ready leaves can; routine leaves close after review/integration/Completion Report; configured gates and open children block closure.

9. `go-pic/cmd/pic/epic_verification.go`, `go-pic/cmd/pic/work_items.go`, `go-pic/cmd/pic/pic_cli_test.go` — Generalize aggregate verification and owner acceptance to recursively evaluate descendant completion, approved requirements, Blueprint obligations, Contract obligations, integrated behavior, and deferred/failed requirement disposition. Test: `cd go-pic && go test ./cmd/pic -run 'TestAggregateWorkItemVerification|TestAggregateWorkItemClosure'`.
   Blocked by: 4, 5, 7.
   Parallel with: 8.
   Acceptance: A Feature or Epic cannot close with an open descendant or unmet required obligation and can close after one current passing aggregate report plus any required owner acceptance.

10. `go-pic/cmd/pic/workflow_schema.go`, `go-pic/cmd/pic/legacy.go`, `go-pic/cmd/pic/pic_cli_test.go`, `go-pic/cmd/pic/legacy_feature_workflow_test.go` — Migrate Epics, Tasks, phase Tasks, parent Tasks, dependencies, materializations, and archived Task Items into the Work Item model; preserve Task Items as read-only history and quarantine incompatible in-progress pipelines instead of restarting them. Test: `cd go-pic && go test ./cmd/pic -run 'TestWorkItemLegacyMigration|TestWorkItemPipelineMigration|TestExistingFeatureProxyRemainsReadableDuringMigration'`.
   Blocked by: 7, 8, 9.
   Parallel with: none.
   Acceptance: Every legacy ID and relationship is retained, former executable parents become Features, archived items cannot be claimed, and active lineage either resumes unchanged or is explicitly quarantined.

11. `go-pic/cmd/pic/main.go`, `go-pic/cmd/pic/core.go`, `go-pic/cmd/pic/feature.go`, `go-pic/cmd/pic/phase_repair.go`, `go-pic/cmd/pic/rebuild.go`, `go-pic/cmd/pic/pic_cli_test.go` — Complete CLI cutover: route generic `list`/`show` through Work Items, remove `epic`, `task`, `task-item`, and `feature` mutation commands plus phase-repair authority, and retain only explicit read-only historical output where required. Test: `cd go-pic && go test ./cmd/pic -run 'TestWorkItemCommandCutover|TestArchivedTaskItemReadOnly'`.
   Blocked by: 10.
   Parallel with: none.
   Acceptance: `pic work-item ...` is the only lifecycle mutation surface and every removed command fails without changing the database.

12. `pi-ext/api/tool.ts`, `pi-ext/api/commands.ts`, `pi-ext/tasking/task-prompts.ts`, `pi-ext/tasking/task-artifacts.ts`, `pi-ext/tasking/workflow-gates.ts`, `pi-ext/tasking/task-prompts.test.ts`, `pi-ext/tasking/task-artifacts.test.ts`, `pi-ext/tasking/workflow-gates.test.ts` — Replace Epic/Task/Task Item actions and combined design prompts with Work Item actions and separate Scan, RRI, Vision, Blueprint, Contract, task-graph, materialize, and authorize prompts; require explicit owner action at every gate. Test: `cd pi-ext && node --experimental-strip-types --test tasking/task-prompts.test.ts tasking/task-artifacts.test.ts tasking/workflow-gates.test.ts`.
   Blocked by: 4, 5, 11.
   Parallel with: 13.
   Acceptance: The extension exposes no legacy mutation action, never advances a gate automatically, and emits the exact next-stage prompt from persisted CLI state.

13. `pi-ext/pipeline/pipeline-scheduler.ts`, `pi-ext/pipeline/pipeline-types.ts`, `pi-ext/pipeline/phase-orchestration.ts`, `pi-ext/pipeline/task-phase-repair.ts`, `pi-ext/pipeline/pipeline-scheduler.test.ts` — Schedule only authorized, dependency-ready executable Work Items; remove phase-task repair/orchestration and aggregate worker launch paths while preserving worker/reviewer evidence binding and cancellation behavior. Test: `cd pi-ext && node --experimental-strip-types --test pipeline/pipeline-scheduler.test.ts`.
   Blocked by: 8, 11.
   Parallel with: 12.
   Acceptance: Aggregates never launch workers, unauthorized or blocked leaves never schedule, dependency-ready siblings can schedule, and candidate/review evidence remains bound to the active TIP hash.

14. `go-pic/cmd/pic/misc.go`, `go-pic/cmd/pic/legacy_web_parity_test.go`, `go-pic/cmd/pic/pic_cli_test.go` — Replace Epic/Task/Task Item HTTP mutation endpoints and dashboard queries with Work Item list/detail/tree, readiness, gate-state, artifact-revision, graph, authorization, and status endpoints backed only by Go CLI rules. Test: `cd go-pic && go test ./cmd/pic -run 'TestWorkItemWebAPI|TestLegacyWebAPIParity'`.
   Blocked by: 9, 11.
   Parallel with: 12, 13.
   Acceptance: Web writes use the same validation and transactions as CLI writes, removed endpoints return a non-success response without mutation, and recursive hierarchy/gate data is returned without application-memory readiness traversal.

15. `go-pic/web/src/lib/api.ts`, `go-pic/web/src/lib/stores.ts`, `go-pic/web/src/routes/dashboard/+page.svelte`, `go-pic/web/src/routes/search/+page.svelte`, `go-pic/web/src/routes/work-item/[id]/+page.svelte` (new), `go-pic/web/src/routes/epic/[id]/+page.svelte`, `go-pic/web/src/routes/task/[id]/+page.svelte`, `go-pic/web/src/app.css` — Replace separate Epic/Task screens and Task Item controls with a typed Work Item dashboard and recursive detail view showing hierarchy, blockers, readiness, staged approvals, artifact revisions, TIP authorization, evidence, and allowed actions. Test: `cd go-pic/web && npm run check && npm run build`.
   Blocked by: 14.
   Parallel with: none.
   Acceptance: The built dashboard can create and navigate all Work Item types, cannot add Task Items, exposes only currently legal gate/actions, and renders deep aggregates and executable leaves without overlap at desktop and mobile widths.

16. `pi-ext/README.md`, `docs/feature-workflow.md`, `docs/V1.0.0.md`, `go-pic/cmd/pic/*_test.go`, `pi-ext/*.test.ts`, `pi-ext/**/*.test.ts` — Remove stale Epic/Task/phase command documentation, document the Work Item workflow and migration behavior, run the full lockstep suite, and produce required runtime artifacts through the normal build. Test: `cd go-pic && go test ./... && cd ../pi-ext && npm test && npm run check && npm run build`.
   Blocked by: 12, 13, 15.
   Parallel with: none.
   Acceptance: All Go and TypeScript tests pass, type checking and dashboard/runtime builds succeed, `go-pic/web/build` and `go-pic/dist/pic` remain installed, and repository search finds no live legacy mutation path.

## Dependency Summary

```text
1 → 2,3 → 4,5 → 6 → 7 → 8,9 → 10 → 11
4,5,11 → 12
8,11 → 13
9,11 → 14 → 15
12,13,15 → 16
```

The migration and cutover are intentionally sequential around Tasks 6-11 because they change the same authoritative schema and lifecycle files. Parallel work is safe only at the explicitly listed artifact, execution, extension, and API boundaries.

## Risks And Rollback

- **Tasks 1 and 10: destructive schema migration risk.** Rollback: copy the pre-migration SQLite database back into place; migration must create and verify a backup before schema replacement and must not delete archived Task Item rows.
- **Tasks 4-8: approval or TIP lineage corruption.** Rollback: disable new Work Item mutations and restore the previous binary against the untouched pre-cutover database; never rewrite historical artifact or TIP hashes.
- **Task 7: partial graph materialization.** Rollback: transaction rollback only; no manual row deletion is acceptable.
- **Task 11: command compatibility break.** Rollback: restore the previous lockstep binary/extension/dashboard release; do not reintroduce a TypeScript CLI fallback.
- **Tasks 12-15: client/server version mismatch.** Rollback: deploy the previous complete artifact set; Go CLI, extension, and dashboard must not be rolled back independently.
- **Task 16: generated runtime regression.** Rollback: rebuild from the last passing source revision; do not patch `go-pic/web/build`, `go-pic/dist/pic`, or installed dependencies by hand.

## Coverage

- Spec acceptance criteria 1-3: Tasks 1, 2, 10.
- Criteria 4-7: Tasks 3, 8.
- Criteria 8-13: Tasks 5-7.
- Criteria 14-16: Tasks 8, 9.
- Criteria 17-18: Tasks 10, 11.
- Criteria 19-22: Tasks 4, 5, 7-9.
- Criterion 23: Task 16.