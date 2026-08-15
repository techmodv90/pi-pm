# Async Pipeline Scheduler Plan

Spec sufficient: the owner selected clean Git plus isolated worktrees and sequential patch integration.

1. `go-pic/cmd/pic/main.go`, `workflow.go`, `pipeline.go`, `pic_cli_test.go` — add durable pipeline run claims, lease-safe completion, listing, and cancellation. Test: `cd go-pic && go test ./cmd/pic -run TestPipelineRunLifecycle`. Acceptance: duplicate/stale claims are rejected and matching attempts transition atomically.
   Blocked by: none.
   Parallel with: 2.

2. `pi-ext/pipeline-scheduler.ts`, `pipeline-scheduler.test.ts`, `index.ts`, `package.json` — add pure stage selection plus pi-subagents RPC spawn/status reconciliation. Test: `cd pi-ext && node --experimental-strip-types --test pipeline-scheduler.test.ts`. Acceptance: independent tasks launch concurrently and one task never skips a gate.
   Blocked by: none for pure stage tests; task 1 for persisted integration.

3. `pi-ext/pipeline-scheduler.ts` — reject dirty Git, request isolated worker worktrees, apply a complete worker wave under a single integration queue, checkpoint-commit it, and block on conflicts. Test: pipeline scheduler test plus temporary Git integration test. Acceptance: no worker launches dirty; the complete worker wave integrates one patch at a time and is committed before review fan-out.
   Blocked by: 1, 2.

4. `pi-ext/agents/task-worker.md`, `task-reviewer.md`, `task-prompts.ts`, tests — make async Scan, Worker, and Review stages persist their results, then hand final verification back to the main contractor. Test: `cd pi-ext && npm test`. Acceptance: reviewed child Tasks unblock dependencies and the scheduler never launches a QA or verifier subagent.
   Blocked by: 2.

5. Generated runtime artifacts — run `cd pi-ext && npm run check && npm test && npm run build`, then `cd go-pic && go test ./...`. Acceptance: all commands exit zero and installed dashboard/binary artifacts remain present.
   Blocked by: 1-4.

## Risks

- Concurrent patch conflicts. Rollback: stop the affected pipeline before review and leave source branches/worktrees intact for manual resolution.
- Stale async completions. Rollback: lease token and attempt comparison prevents state advancement; cancel the stale run.
- Pi shutdown during integration. Rollback: recover from persisted run state and lifecycle artifacts; never mark review-ready until the patch is present in the parent checkout.
