# Plan: Aggregate Delivery Workflow

## Goal
Make executable children contractor-closable and make one authorized Epic/Feature delivery aggregate own final verification, owner acceptance, and direct merge to `develop`.

## Architecture
Add canonical delivery metadata and aggregate owner decisions beside existing Work Item evidence. Go owns transactional authority and workflow status; the Pi scheduler owns Git operations outside SQLite transactions. The scheduler binds reviewed child integration to the delivery branch, and direct merge completion is persisted only after remote `develop` confirms the resulting commit.

## Task 1: Add delivery authority schema and evidence

**Files**:
- Modify: `go-pic/cmd/pic/main.go`
- Modify: `go-pic/cmd/pic/workflow_schema.go`
- Modify: `go-pic/cmd/pic/work_items.go`
- Test: `go-pic/cmd/pic/pic_cli_test.go`

**Steps**:
1. Add red schema/lifecycle tests for delivery metadata, aggregate owner decisions, branch-owner uniqueness, and merge-pending state.
2. Run `go test ./cmd/pic -run 'TestAggregateDelivery|TestExecutableClosure' -count=1` and observe failures.
3. Add idempotent delivery and aggregate-decision tables, indexes, and migration columns required for current evidence.
4. Implement delivery-mode resolution and authorization binding to current branch/base SHA.
5. Run the focused tests and expect all lifecycle assertions to pass.

## Task 2: Remove child owner gate and add aggregate workflow stages

**Files**:
- Modify: `go-pic/cmd/pic/work_items.go`
- Modify: `go-pic/cmd/pic/workflow_schema.go`
- Test: `go-pic/cmd/pic/pic_cli_test.go`

**Steps**:
1. Add red tests proving passed child verification closes the child and unblocks a dependent without an owner decision, while aggregate status reports `aggregate_verification` only after descendants finish.
2. Run the focused Go tests and record the current owner-acceptance failures.
3. Change executable closure to require current passed review/completion/verification but not owner acceptance; keep explicit owner rejection behavior scoped to aggregate acceptance.
4. Add aggregate acceptance validation, stale SHA checks, coordination closure, and branch-owner `merge_pending` transition.
5. Run focused and full Go tests.

## Task 3: Implement direct aggregate merge

**Files**:
- Modify: `pi-ext/pipeline/pipeline-scheduler.ts`
- Modify: `pi-ext/api/tool.ts`
- Modify: `pi-ext/tasking/work-item-prompts.ts`
- Test: `pi-ext/pipeline/pipeline-scheduler.test.ts`
- Test: `pi-ext/tasking/work-item-prompts.test.ts`

**Steps**:
1. Add red scheduler tests for clean-branch validation, `--no-ff` merge, push failure staying `merge_pending`, and remote SHA confirmation.
2. Run focused extension tests and record failures.
3. Add an idempotent `mergeAggregate` operation using a temporary detached worktree, the recorded delivery branch, current `develop`, and the existing Git write lock.
4. Add owner acceptance and merge retry tool actions with explicit role checks and user-facing blocker evidence.
5. Run the focused suite, full extension suite, typecheck, and build.

## Task 4: Update prompts and integration evidence

**Files**:
- Modify: `pi-ext/tasking/work-item-prompts.ts`
- Modify: `pi-ext/core/workflow-primer.ts`
- Modify: `docs/progress/gap.md`
- Test: `pi-ext/tasking/work-item-prompts.test.ts`

**Steps**:
1. Add red prompt assertions for child auto-close, aggregate-only owner acceptance, and `merge_pending` recovery.
2. Implement the concise workflow handoff and exact next-action mapping.
3. Record GAP-098 with red/green evidence and run `git diff --check`.
4. Run all Go and extension verification commands and commit the scoped implementation plus generated artifacts.
