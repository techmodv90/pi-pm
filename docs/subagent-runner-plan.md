# Task-System-Owned Subagent Runner Plan

1. `pi-ext/subagent/agents.ts` — add upstream-style agent discovery and frontmatter parsing for task-system agent definitions. Test: `cd pi-ext && node --experimental-strip-types --test subagent/*.test.ts`.
   Acceptance: discovery returns the packaged task agents with names, tools, models, and prompt bodies without loading a third-party manager.
2. `pi-ext/subagent/runner.ts` — add the upstream JSONL child-process runner with private prompt files, incremental message/tool parsing, usage/result capture, abort handling, and bounded output. Test: `cd pi-ext && node --experimental-strip-types --test subagent/*.test.ts`.
   Acceptance: a fake child stream produces progress before completion, malformed lines are ignored, and abort rejects with termination evidence.
   Blocked by: 1.
3. `pi-ext/pipeline/pipeline-scheduler.ts` — replace vendor RPC spawn/stop, completion events, and global model lookup with the owned runner while preserving claims, artifacts, worktree patches, and reconciliation. Test: `cd pi-ext && node --experimental-strip-types --test pipeline/pipeline-scheduler.test.ts`.
   Acceptance: scheduler launch, completion, stop, and resume tests pass without `subagents:rpc` references.
   Blocked by: 2.
4. `pi-ext/pipeline/pipeline-scheduler.test.ts` — replace RPC-shaped assertions with owned-runner seams and add parallel progress/cancellation coverage. Test: `cd pi-ext && node --experimental-strip-types --test pipeline/pipeline-scheduler.test.ts`.
   Acceptance: tests verify independent child runs, progress callbacks, and persisted cancellation.
   Blocked by: 2.
5. `pi-ext/index.ts` and `pi-ext/subagent/*` — register the task-system-owned progress/UI bridge and remove the external subagent lifecycle dependency. Test: `cd pi-ext && npm run check && npm test`.
   Acceptance: extension loads with no vendor subagent extension and displays active child progress through the selected task-system UI surface.
   Blocked by: 3.

## Risks

- `pi --mode json` invocation contract changes. Rollback: restore the runner adapter while leaving persisted pipeline artifacts and scheduler data unchanged.
- Child cancellation can leave a process alive. Rollback: retain process IDs and use SIGTERM followed by bounded SIGKILL during stop/reconciliation.
- Existing dirty-tree gating and worktree patch integration regress. Rollback: revert only runner wiring; do not replay or delete existing pipeline artifacts.