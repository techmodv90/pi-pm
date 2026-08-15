# Task-System-Owned Subagent Runner

This spec proposes embedding the upstream subagent extension's agent discovery, isolated `pi` subprocess execution, JSONL streaming, cancellation, and orchestration primitives into `pi-ext` so task pipelines no longer depend on a third-party subagent extension.

## Goals

- Task pipelines launch `task-scout`, `task-worker`, and `task-reviewer` through code owned by `pi-ext`.
- Child assistant messages and tool events stream to task-system progress state while the child is running.
- Worker worktree isolation, cancellation, completion artifacts, and scheduler reconciliation remain durable across the existing pipeline lifecycle.
- Agent definitions continue to come from the task-system's packaged agent files and support the upstream JSONL subprocess contract.
- The task-system can run parallel pipeline children without relying on vendor RPC events or a global vendor registry.

## Non-Goals

- Replacing the existing Go task database, pipeline claims, lease protocol, completion reports, or review gates.
- Supporting the upstream extension's generic user-facing chain/parallel `subagent` tool as a separate product surface.
- Adding new model providers, tool implementations, or agent roles.
- Preserving the third-party extension's event names, global symbols, or transcript manager.

## Constraints

### Compatibility

- The existing `PipelineScheduler` public methods and `pipelineSpawnParams` test contract must remain valid unless the contract is replaced by an owned runner test.
- Worker subprocesses must receive the existing packaged agent prompt, model, tools, cwd, and worktree isolation requirements.
- Existing `.pi-subagents/pipeline/<claim>` output, status, and patch artifacts remain readable by reconciliation and resume paths.

### Performance

- Child output parsing must be incremental and must not wait for process exit before emitting progress.
- Parallel launches must not serialize child execution on output handling.
- Final output and stderr must retain bounded in-memory state; durable pipeline artifacts remain the source of truth.

### Security/Compliance

- Spawned processes must use `shell: false` and trusted argument arrays.
- Agent prompts written to temporary files must use private permissions and be removed after process exit.
- Project-local agent configuration must not be loaded from an arbitrary cwd without the existing task-system trust boundary.
- Child output and errors must not expose credentials beyond what the child process already receives.

### Operational

- The runner must work with the installed `pi` executable and the current Node runtime without adding a third-party extension dependency.
- Abort and `/task-pipeline stop` must terminate child processes and persist pipeline cancellation before runtime interruption.
- A child that exits without a terminal JSON event must become a failed pipeline run during reconciliation.

## Acceptance Criteria

1. Given a task pipeline launch, when the scheduler starts a scan, worker, or review stage, then the child is created by the task-system-owned runner and no `subagents:rpc:*` event or vendor global registry is read.
2. Given a running child that emits JSONL assistant or tool events, when each complete event is parsed, then the runner emits a progress update before child exit.
3. Given a Worker stage, when the child completes, then its output, status, worktree patch, completion report, and existing pipeline checkpoints are persisted exactly as before.
4. Given an active child, when the operator stops the pipeline or the parent abort signal fires, then the child receives termination and the persisted run is marked cancelled before runtime cleanup completes.
5. Given multiple dependency-ready child tasks, when a pipeline wave launches, then all eligible children run concurrently and each has an independent result and artifact directory.
6. Given the task-system package, when `npm test` and `npm run check` run, then the owned runner, scheduler integration, and agent discovery contracts pass without requiring a third-party subagent extension.

## Open Questions

- **Who knows:** task-system UI owner. **Question:** Should streamed child progress render in the existing built-in Agent widget or in a task-system-owned renderer/status panel? **Impact if wrong:** the runner can stream correctly while the user still sees an empty widget.
- **Who knows:** runtime/package owner. **Question:** Is `pi --mode json -p --no-session` the supported installed invocation for every deployment target? **Impact if wrong:** child startup can fail after removing the vendor bridge.
- **Who knows:** workflow owner. **Question:** Should generic upstream chain/parallel prompt files be retained as internal assets or removed after the pipeline runner is integrated? **Impact if wrong:** retaining them adds unused surface; removing them may discard expected orchestration recipes.