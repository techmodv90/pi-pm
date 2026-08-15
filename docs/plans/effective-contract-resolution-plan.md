# Effective Contract Resolution Implementation Plan

Spec sufficient: goals, non-goals, constraints, acceptance criteria, and migration behavior are defined in `docs/effective-contract-resolution-spec.md`. The three open storage questions must be resolved in Task 1 before schema code is merged.

1. `docs/effective-contract-resolution-spec.md`, `go-pic/cmd/pic/workflow_schema.go` — Resolve project/phase/outdated storage questions and document the final table contracts beside the schema definitions. Test: `cd go-pic && go test ./cmd/pic -run TestEffectiveContractSchema`. Acceptance: the approved table contract names every key, foreign key, status enum, scope invariant, and migration default without placeholders.

2. `go-pic/cmd/pic/workflow_schema.go`, `go-pic/cmd/pic/main.go`, `go-pic/cmd/pic/rebuild.go`, `go-pic/cmd/pic/pic_cli_test.go` — Add immutable requirement metadata, requirement assignments, contract operations/targets, effective snapshots/entries, snapshot bindings, and completed-task impact persistence through additive migration. Test: `cd go-pic && go test ./cmd/pic -run 'TestEffectiveContractSchema|TestExistingDatabaseMigration'`. Acceptance: a legacy database migrates without row loss and rejects invalid scope, target, status, and uniqueness combinations.
Blocked by: 1.

3. `go-pic/cmd/pic/effective_contracts.go` (new), `go-pic/cmd/pic/pic_cli_test.go` — Implement deterministic scope traversal, assignment/inheritance selection, approved-operation application, replacement-chain validation, defer/resume handling, contract-key uniqueness, and canonical hashing. Test: `cd go-pic && go test ./cmd/pic -run TestCompileEffectiveContract`. Acceptance: table-driven tests cover replacement, withdrawal, defer/resume, legacy targets, scope precedence, cycles, missing targets, unrelated parent exclusion, and duplicate-key conflicts.
Blocked by: 2.

4. `go-pic/cmd/pic/workflow.go`, `go-pic/cmd/pic/main.go`, `go-pic/cmd/pic/pic_cli_test.go` — Add CLI operations to propose, approve, reject, and reactivate contract operations; make approval atomic with owner-decision authorization, stale affected TIPs, and persist completed-task impacts. Test: `cd go-pic && go test ./cmd/pic -run TestContractOperationLifecycle`. Acceptance: drafts have no effect, approval records one authorizing decision, rejection cannot activate, and affected subjects receive the specified stale/outdated state.
Blocked by: 3.

5. `go-pic/cmd/pic/instruction_packs.go`, `go-pic/cmd/pic/epic_feature.go`, `go-pic/cmd/pic/pic_cli_test.go` — Require schema-v3 content for new open work, compile the snapshot inside each standalone/materialized TIP activation transaction, bind snapshot ID/hash into TIP canonical content, and reject unresolved contracts before superseding the prior active TIP. Test: `cd go-pic && go test ./cmd/pic -run 'TestInstructionPackEffectiveContract|TestMaterializedPackEffectiveContract'`. Acceptance: failed compilation leaves the prior active TIP untouched; successful activation creates exactly one immutable snapshot and matching TIP hash.
Blocked by: 3, 4.

6. `go-pic/cmd/pic/pipeline.go`, `go-pic/cmd/pic/core.go`, `go-pic/cmd/pic/pic_cli_test.go` — Bind worker/review claims to snapshot ID/hash, reject stale snapshots, expose effective and historical contract views through `show`, and classify snapshot failures as deterministic TIP-revision failures. Test: `cd go-pic && go test ./cmd/pic -run 'TestPipelineEffectiveContractBinding|TestShowEffectiveContract'`. Acceptance: worker and review claims cannot launch with a missing, changed, or stale snapshot and status output identifies the exact blocking operation or conflict.
Blocked by: 5.
Parallel with: 7.

7. `go-pic/cmd/pic/workflow.go`, `go-pic/cmd/pic/epic_verification.go`, `go-pic/cmd/pic/pic_cli_test.go` — Bind completion and verification reports to snapshots, validate evidence only against effective snapshot entries, retain historical evidence, and prevent replacement requirements from inheriting old coverage. Test: `cd go-pic && go test ./cmd/pic -run 'TestCompletionEffectiveContractBinding|TestVerificationEffectiveCoverage|TestEpicEffectiveCoverage'`. Acceptance: current coverage ignores excluded requirements, requires new evidence for replacements, and historical reports remain queryable.
Blocked by: 5.
Parallel with: 6.

8. `pi-ext/pipeline/pipeline-scheduler.ts`, `pi-ext/pipeline/pipeline-scheduler.test.ts` — Consume persisted snapshot bindings, check freshness before spawn and integration, quarantine stale worker artifacts, and send an actionable follow-up without terminating unrelated scheduling. Test: `cd pi-ext && node --experimental-strip-types --test pipeline/pipeline-scheduler.test.ts`. Acceptance: a contract change during execution preserves diagnostics and patch files but performs no `git apply`, review launch, or automatic retry.
Blocked by: 6, 7.

9. `pi-ext/tasking/phase-plan.ts`, `pi-ext/tasking/task-prompts.ts`, `pi-ext/agents/task-planner.md`, `pi-ext/tasking/task-prompts.test.ts` — Update planner contracts to assign requirements explicitly, mark descendant inheritance intentionally, reuse contract keys for replacements, and emit schema-v3 TIP content bound through activation rather than prose deviations. Test: `cd pi-ext && node --experimental-strip-types --test tasking/task-prompts.test.ts`. Acceptance: generated instructions contain no free-form precedence mechanism and require structured resolution before contradictory task plans can materialize.
Blocked by: 5.
Parallel with: 6, 7.

10. `go-pic/web/src/lib/api.ts`, `go-pic/web/src/routes/task/[id]/+page.svelte`, `go-pic/web/src/routes/epic/[id]/+page.svelte` — Display effective requirements separately from historical exclusions, resolution provenance, stale snapshot blockers, and `contract_outdated` owner-review entries using existing task/epic detail surfaces. Test: `cd go-pic/web && npm run check && npm run build`. Acceptance: an owner can identify the active requirement, replaced requirement, authorizing decision, affected scope, and required next action without reading raw JSON.
Blocked by: 6, 7.
Parallel with: 8, 9.

11. `go-pic/cmd/pic/pic_cli_test.go`, `pi-ext/pipeline/pipeline-scheduler.test.ts`, `docs/feature-workflow.md` — Add an end-to-end fixture covering legacy conflict, approved replacement, snapshot activation, mid-run staleness, quarantine, revised TIP, review, and effective verification; document migration and operator recovery. Test: `cd go-pic && go test ./... && cd ../pi-ext && npm run check && npm test && npm run build`. Acceptance: the full scenario passes, all existing suites pass, and generated `go-pic/web/build` plus `go-pic/dist/pic` remain installed.
Blocked by: 8, 9, 10.

## Risks

- Tasks 2-5 change authoritative SQLite workflow state. Rollback: deploy the prior binary; additive tables/columns remain unused and historical rows are retained.
- Task 4 can stale many active TIPs after an operation approval. Rollback: reject a still-draft operation; approved operations are immutable and require an explicit inverse operation rather than deletion.
- Task 5 changes TIP hashing and activation semantics. Rollback: completed historical TIPs remain readable, but open schema-v3 TIPs require the new binary; do not downgrade after activating v3 packs.
- Task 8 touches patch integration. Rollback: disable new launches and retain quarantined artifacts; never bypass snapshot checks to recover throughput.
- Task 10 changes user-visible workflow interpretation. Rollback: hide the new panels while preserving API fields and authoritative Go enforcement.