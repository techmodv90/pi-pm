# Distilled: APM Task System

> Distilled by `/apm distill .` on 2026-09-05. Reference artifact — records
> what the code currently does. Not a `.plan.md`, `.feature`, or requirement
> source; enter the pipeline via `/apm spec` citing this as `Context:`.

## Surface

```
Analyzed: 33,623 LOC total
├── go-pic/cmd/pic      16,395 Go (incl. 7,449 *_test.go) — pic CLI + SQLite state
├── go-pic/web/src         863 Svelte/TS — static dashboard
├── pi-ext              10,029 TS (excl. tests) — Pi extension (core/pipeline/tasking/ui/subagent)
└── tests (pi-ext)      6,337 TS (*.test.ts)
```

External dependencies: Go stdlib only (net/http, crypto/sha256, modernc SQLite
via driver); TypeScript: `@mariozechner/pi-coding-agent`, `@mariozechner/pi-tui`,
`@mariozechner/pi-ai`, `typebox`, `fast-xml-parser`, Node built-ins. No
frontend framework beyond SvelteKit static adapter.

## Business Rules

1. SQLite is the only canonical state store; files under `.apm/` are
   discovery artifacts. [confirmed: code — artifacts.dbml, AGENTS.md]
2. Work Item lifecycle transitions (open → in_progress → done/cancelled) and
   stage progression are mutated **only** through `pic`; agents cannot mutate
   the workflow lifecycle through pic when identified as agent. [confirmed:
   code — workflow.go:28 returns "cannot mutate workflow lifecycle through
   pic"]
3. Work Item `status` ∈ {open, in_progress, done, cancelled}; `type` ∈
   {epic, feature, task, bug, chore, gate}; `priority` ∈ {low, medium, high};
   artifact `stage` ∈ {scan, rri, rri_t_scenarios, vision, blueprint,
   contracts, task_graph}. [confirmed: code — workflow_schema.go CHECK
   constraints]
4. Artifact revisions are immutable: `work_item_artifacts` is
   UNIQUE(work_item_id, stage, revision) with content hash; new content is a
   new revision, never an UPDATE. [confirmed: code — workflow_schema.go]
5. Workflow checkpoints bind one stage to exactly one artifact revision:
   UNIQUE(work_item_id, stage, artifact_revision), storing content_hash and
   the owner decision. [confirmed: code — workflow_schema.go]
6. Scan/RRI rows are owned by XOR task_id or epic_id — never both, never
   neither. [confirmed: code — CHECK((task_id IS NOT NULL) != (epic_id IS
   NOT NULL))]
7. Requirements carry tier1/tier2/tier3 priority and a status of
   pending/satisfied/failed/deferred; `inherit_to_descendants` propagates
   contract keys down the Work Item tree. [confirmed: code — requirements
   table]
8. Worker runs execute in isolated git worktrees; a post-exit invariant
   re-checks that the run's worktree is still the git top-level, else the run
   is blocked. [confirmed: code — pipeline-scheduler.ts:272 "worker worktree
   invariant failed after exit"]
9. Auto-batching never touches items with a live claim; explicit retries are
   the only path to re-claim. [confirmed: code — pipeline-scheduler.ts:402]
10. A stage never dispatches before the Plan profile is persisted — the
    handoff depends on it. [confirmed: code — pipeline-scheduler.ts:514]
11. Cancelled/stopped runs block the run, release the claim, and stop —
    never retried automatically. [confirmed: code — pipeline-scheduler.ts:791]
12. Ephemeral handoffs expire five minutes after first load and are never
    persisted. [confirmed: code — pipeline-scheduler.ts:840,1078]
13. Scan evidence flows scout → ephemeral handoff → contractor synthesis;
    the contractor validates every section against source before saving one
    canonical Scan Report. [confirmed: code — pipeline-scheduler.ts:840]
14. Escalated runs never integrate; the escalation guard narrows before
    integration is attempted. [confirmed: code — pipeline-scheduler.ts:974]
15. Verification blocks a pipeline run when the latest verification does not
    follow the newest task completion; personas never execute procedures or
    self-grade. [confirmed: code — pipeline-scheduler.ts:180, report-parsing]
16. Additive SQLite migrations are idempotent: every ALTER TABLE ADD COLUMN
    is guarded by a column-exists check and must survive re-runs against
    already-widened tables. [confirmed: code — schema_bootstrap.go +
    schema_helpers.go]
17. SQLite `RAISE()` is never used outside trigger programs; callers assert
    affected-row counts instead. [assumed: code — convention; no test asserts
    the negative]
18. Task-system subagents never receive a `turnBudget`; control uses runtime
    timeout only. [assumed: code — convention recorded in AGENTS.md]
19. Child pipelines end at worker completion + passed review; there is no
    child QA agent — contractor verification is executed directly by the main
    contractor and persisted as one Verification Report. [assumed: code —
    AGENTS.md policy; scheduler enforces sequencing]
20. APM subcommands are thin handlers: hidden full prompt to the LLM
    (display:false, triggerTurn:true), prompt loaded from
    `pi-ext/core/prompts/` at call time so edits need no reload. [confirmed:
    test apm-command.test.ts]
21. Prompt templates carry the input prefix (`- plan: {INPUT}` etc.);
    handlers inject the bare value — never both. [confirmed: test
    apm-command.test.ts — "prompt templates carry the input prefix" test]
22. `/apm spec` emits `.feature` + DBML; `/apm clarify` upgrades @draft →
    @ready only on a passing Auto-QA; `/apm tech-plan` gates on the APM
    constitution and ratifies DBML; `/apm breakdown` requires
    **Status:** Approved plan + @ready spec. [confirmed: test apm-command.test.ts]
23. Go raw strings cannot interpolate — SQL needing computed values breaks
    out of the raw string or parameterizes with `?`. [assumed: code —
    convention from a silent-wrong-semantics incident]
24. `pic` CLI returns Go errors; HTTP handlers return JSON errors with
    matching status codes. [confirmed: code — misc.go, workflow.go]
25. Parallel execution of Work Items goes only through the persisted
    scheduler with per-child worktree isolation; writers never share one
    checkout. [confirmed: code — pipeline-scheduler.ts; AGENTS.md]

## Contracts

```go
// pic CLI surface (main.go switch): version, init, project, work-item,
// workflow, activity, search, markdown, web, list, show — JSON output,
// errors as Go error values from handlers
type Subcommand = "init" | "project" | "work-item" | "workflow" | "activity"
                | "search" | "markdown" | "web" | "list" | "show"
```

```ts
// APM subcommand contract (apm-command.ts): each sub = dedicated handler,
// hasApmWorkspace(ctx) gate, {INPUT} placeholder replacement, sendHiddenPrompt
type ApmSub = "init" | "prd" | "spec" | "clarify" | "tech-plan" | "breakdown" | "distill"
type Handler = (pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext) => void
```

```ts
// Scheduler stage lifecycle (pipeline-scheduler.ts): claim → dispatch →
// TIP lineage → integrate; integration requires verificationBlock === null
type PipelineStage = "scan" | "rri" | "rri_t_scenarios" | "vision"
                   | "blueprint" | "contracts" | "task_graph"
```

## Invariants (cross-module)

- Artifact content hash ↔ checkpoint content_hash must match per (stage,
  revision); `pic work-item show` and drift checks depend on it
- requirement_keys coverage ↔ Task Graph acceptance: every executable node's
  Given/When/Then maps to persisted requirements
- `go-pic/web/build` and `go-pic/dist/pic` are build outputs only — always
  present for the installed `/task-app` runtime, never hand-edited

## Scenarios

```gherkin
Feature: APM Task System
  A Pi extension plus a Go CLI that manage Work Items through a staged
  pipeline (scan → rri → vision → blueprint → contracts → task_graph) with
  SQLite as the single source of truth and scheduled workers as the only
  execution path.

  Background:
    Given a project workspace with a tasks.db SQLite database
    And a registered /apm command set in the Pi extension

  Scenario: Agent attempts a lifecycle mutation
    Given an agent-identified caller
    When it invokes a workflow-lifecycle mutation through pic
    Then pic refuses with "cannot mutate workflow lifecycle through pic"
    And no state changes

  Scenario: Artifact revision is written
    Given a Work Item with an existing artifact at stage "blueprint" revision 3
    When new content is saved for the same stage
    Then a new row with revision 4 and a fresh content_hash is inserted
    And the revision-3 row remains byte-identical

  Scenario: Owner approves an artifact revision
    Given artifact (stage, revision) saved and content_hash recorded
    When the owner approves it
    Then one checkpoint row binds (work_item, stage, artifact_revision, decision)
    And a second approval of the same tuple is rejected by the UNIQUE constraint

  Scenario: Worker exits from a tampered worktree
    Given a worker run assigned to worktree W
    When the run exits and W is no longer the git top-level
    Then the run is blocked with "worker worktree invariant failed after exit"
    And nothing integrates

  Scenario: Auto-batch skips a claimed item
    Given an item with a live claim
    When the auto-batch pass runs
    Then the item is skipped
    And only an explicit retry can re-claim it

  Scenario: Ephemeral handoff expires
    Given a scout handoff loaded once
    When five minutes elapse after first load
    Then the handoff no longer resolves
    And no handoff content was ever persisted

  Scenario: Verification out of order
    Given a task completion report at time T1
    And a verification report older than T1
    When the pipeline checks integration readiness
    Then the run is blocked until a fresh verification follows the newest completion

  Scenario: Idempotent migration re-run
    Given a database already widened by a prior migration
    When schema bootstrap runs again
    Then no ALTER TABLE fails
    And schema_migrations records reflect the applied set

  Scenario: /apm spec enters a new feature
    Given a typed requirement and the newest PRD per prd-history.json
    When /apm spec runs for one feature
    Then .feature + DBML land at .apm/specs/<domain>/<Name>.feature
    And no other feature's user stories are extracted

  Scenario: /apm breakdown on a Draft plan
    Given a .plan.md with **Status:** Draft
    When /apm breakdown is invoked
    Then the gate stops with DO-NOT-ADVANCE
    And no .tasks.md is written

  Scenario: /apm distill records evidence
    Given an existing code module
    When /apm distill runs
    Then .apm/specs/<domain>/<Name>.distilled.md is written at ≤20% of analyzed LOC
    And every extracted rule is tagged confirmed-by-test or assumed-from-code
    And no secrets, Work Items, or pipeline artifacts are created
```

## Risks Identified

- `USAGE` in `apm-command.ts` is a single-line JS string with literal `\n`
  escapes — twice caused silent edit failures (multi-line edits miss);
  a constant block or template literal would remove the trap
- `pic_cli_test.go` is a 5,408-line monolith (102 tests) — the project's own
  modular-rules discipline (files <500 lines) is not applied to its largest
  test file
- `workflow.go` is 30 lines with the mutation guard; the lifecycle logic
  lives in `work_items.go` (4,487 lines) — the biggest churn target and the
  most consequential single file to refactor carefully
- Distill itself: whole-project run has no per-module split; per-module
  distills would keep each file within the 20% compression rule more honestly
