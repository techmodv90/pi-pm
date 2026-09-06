# Feature: APM Implement Handoff

## Status
@ready

## Summary
Redesign `/apm implement` (pi-ext/core/prompts/apm-implement.md) from a
standalone TDD executor into an entry orchestrator: gate → import → owner
authorization relay → read-only observation. Execution itself always belongs
to the canonical Work Item scheduler (task-worker → task-reviewer →
contractor QA), never to the implement prompt.

## Motivation
The current BOUNDARY step makes implement stop dead on Work Item-tracked
features, which feels broken: the command has two personalities (standalone
executor vs. tracked handoff) with a wall between them. With the APM-to-Work-
Item importer (`pic workflow import-apm`) shipped, implement can bridge the
two: any approved feature gets imported, then the durable scheduler owns
execution. This completes the deprecation of the legacy planning pipeline:
APM files are the planning artifacts, the importer is the only bridge, Work
Items are the only execution record.

## Scenarios

### US1 — Gate unchanged (P1)
Given an approved .tasks.md with companion .plan.md and @ready .feature,
when /apm implement runs, the gate checks pass exactly as before.

### US2 — Untracked feature imports then hands off (P1)
Given an approved feature not yet imported,
when /apm implement runs, it dry-runs `pic workflow import-apm`, shows the
parsed graph, then performs the real import and reports the epic ID, feature
and task counts.

### US3 — Already-imported feature resumes (P1)
Given a feature whose .tasks.md hash label already exists in the store,
when /apm implement runs, it reports status (epic ID, per-feature child
states, next ready work) instead of erroring, and offers the next action
(authorize, or wait for running children).

### US4 — Authorization is relayed, never self-granted (P1)
Given a freshly imported or ready epic,
when implement reaches the authorization step, it asks the owner in
conversation and only on an explicit yes calls
`authorize_work_item_implementation` with actor_role=owner.

### US5 — Implement never executes tasks inline (P1)
Given any state of the flow,
when implement has handed off (or while children run), it writes no
implementation code, runs no TDD cycle, and mutates no Work Item state
beyond what the owner explicitly approved.

### US6 — Exit after authorization, report on demand (P2)
Given an authorized epic,
when implement finishes the handoff, it exits with a completion summary and
instructions for on-demand status checks; it does not attach a polling loop.

### US7 — Breakdown handoff mentions the new flow (P3)
Given the apm-breakdown prompt,
when a user reads its handoff section, it points to `/apm implement` as
import + scheduler handoff, not as inline execution.

## Non-goals
- No Go/importer changes (importer already implements gates and graph).
- No scheduler changes (execution semantics unchanged).
- No legacy planning-stage resurrection.
