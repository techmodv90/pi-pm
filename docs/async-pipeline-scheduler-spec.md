# Async Pipeline Scheduler

This spec proposes a persisted task-stage scheduler so independent DAG tasks can execute concurrently without parent-session polling or cross-session Intercom.

## Goals

- Launch scan, Worker, and Reviewer stages asynchronously through pi-subagents RPC; there is no child QA or verifier stage.
- Keep stages sequential within one task while running dependency-ready task pipelines concurrently.
- Persist stage attempt, lease, and subagent run identity in the project task database.
- Renew active leases and recover completed, failed, blocked, or expired pipelines after compaction or Pi session restart.
- Require a clean Git tree and isolated worktrees for concurrent workers; integrate the complete worker wave sequentially and checkpoint-commit it before review.

## Non-Goals

- Running parallel writers in one checkout.
- Supporting dirty-repository pipeline starts.
- Replacing pi-subagents lifecycle execution or artifact formats.
- Using Intercom for polling or routine stage completion.

## Constraints

### Compatibility
- Existing task_manager actions and task workflow artifacts remain valid.
- Existing non-pipeline task execution remains available.

### Performance
- Dependency-ready read-only stages may run concurrently up to pi-subagents limits.
- Worker concurrency is limited by isolated worktree availability.

### Security/Compliance
- RPC requests stay in-process and use pi-subagents discovery and validation.
- A stage result may advance only the matching persisted lease, attempt, active TIP ID/version/hash, and Task-level report outcome.
- Every Worker and Reviewer claim binds exactly one current active TIP. Worker receives the rendered TIP only; source workflow artifacts remain server-side.

### Operational
- Pipeline start requires a clean Git repository with `.pi/tasks.db*` and `.pi-subagents/` untracked and ignored.
- Pi session startup reconciles persisted running stages with pi-subagents status.
- Unknown lifecycle fields and events are ignored.

## Acceptance Criteria

1. Given two dependency-independent tasks and a clean repository, when a pipeline starts, then both eligible stages receive distinct persisted async run IDs.
2. Given a task pipeline, when no current active TIP or required report exists, then Worker or Reviewer is not launched.
3. Given a stale completion from an older attempt, when it is reconciled, then it cannot advance the current pipeline stage.
4. Given a dirty repository, when a pipeline starts, then it fails before launching any worker or creating a stage claim.
5. Given concurrent workers, when their runs start, then each uses an isolated worktree and the complete worker wave is integrated and checkpoint-committed before any review starts.
6. Given Pi restart, compaction, or lease expiry, when the extension starts, then active and recoverable terminal runs are reconciled without Intercom polling.
7. Given a runtime-successful Worker that reports `PARTIAL` or `BLOCKED`, when it is reconciled, then its Issue Report is persisted but its patch is not integrated and Reviewer is not launched.
8. Given an Epic root, when dependency-independent materialized Tasks are ready, then their Worker stages run concurrently and the scheduler requests one contractor Epic VERIFY REPORT only after every Task review passes.

## Open Questions

- Patch conflict policy: stop the affected pipeline and persist a blocked stage; the owner may later choose automated rebasing. Owner: repository owner. Impact: automatic recovery behavior.
- Concurrency defaults: initially defer to pi-subagents global limits rather than duplicate configuration. Owner: operator. Impact: throughput and model cost.
