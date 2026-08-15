# Plan: Feature Workflow Pipeline

## Goal
Add an end-to-end feature workflow that turns a feature idea into an epic, scan, QA/RRI requirements, design, defined implementation tasks, design-derived task items, and continuous implementation with review/verification gates.

## Architecture
Build this as a thin orchestration layer over existing task-system primitives instead of creating a second workflow engine. The CLI should expose a `pic feature ...` command group for deterministic state changes, while the pi extension exposes `/task feature ...` and tool actions that guide the LLM through scan, QA, design, decomposition, and continuous work. Persist all source-of-truth data in the existing per-project SQLite database and keep project discovery in the global registry `~/.pi/task-system/projects.json` only.

## Current Codebase Context

- CLI entrypoint: `cli/src/cli.ts`
- DB/schema/types: `cli/src/db.ts`
- Project helpers: `cli/src/commands/projects.ts`
- Epic commands: `cli/src/commands/epics.ts`
- Task commands: `cli/src/commands/tasks.ts`
- Workflow facade: `cli/src/commands/workflow.ts`
- Scan commands: `cli/src/commands/workflow-scan.ts`
- Requirement commands: `cli/src/commands/workflow-requirements.ts`
- Design commands: `cli/src/commands/workflow-design.ts`
- Phase repair/materialization: `cli/src/commands/workflow-repair*.ts`, `cli/src/commands/workflow-rollup.ts`
- Existing tests: `cli/repair-phases.test.mjs`
- Pi command registration: `pi-ext/commands.ts`
- Pi tool bridge: `pi-ext/tool.ts`
- Prompt builders: `pi-ext/task-prompts.ts`
- Continuous phase work: `pi-ext/phase-orchestration.ts`, `pi-ext/task-phase-repair.ts`
- Browser/menu helpers: `pi-ext/task-browser*.ts`

## Key Design Decisions

1. **Epic is the product container**
   - Feature workflow starts with an epic.
   - If an epic already exists, the workflow can attach to it.

2. **Planning task is the source artifact**
   - Create one parent/planning task under the epic.
   - Attach scan reports, requirements, design, and phase metadata to this parent task.

3. **Generated tasks are implementation units**
   - After design approval, materialize child tasks from the design/phase plan.
   - Each child task gets concrete task items derived from the design/TIPs.

4. **Implementation stays gated**
   - Designed/full workflows cannot start work until scan + requirements + approved design + ready TIPs/task items exist.
   - Continuous implementation should process child phase tasks in order and stop at review/verification gates.

5. **No new project metadata store**
   - Do not recreate per-project `.pi/task-system/projects.json` metadata.
   - Use `~/.pi/task-system/projects.json` only for project registry pointers.

---

## Proposed User Flow

### CLI

```bash
pic feature start "Add dependency graph" --epic "Web dashboard" --mode designed
pic feature scan <planning-task-id>
pic feature qa <planning-task-id>
pic feature design <planning-task-id>
pic feature materialize <planning-task-id>
pic feature work <planning-task-id>
```

### Pi Extension

```text
/task feature Add dependency graph
/task feature continue <planning-task-id>
/task feature work <planning-task-id>
```

### Internal Artifact Flow

```text
Epic
  └─ Planning Task workflow_mode=designed/full
       ├─ Scan Report
       ├─ Requirements
       ├─ Design
       ├─ TIPs / phase plan
       └─ Child Implementation Tasks
            ├─ Task Items
            ├─ Completion Reports
            ├─ Review Status
            └─ Verification Reports
```

---

## Milestone 1: Feature Workflow Service Skeleton

### Task 1: Add feature workflow command module

**Files**:
- Create: `cli/src/commands/feature-workflow.ts`
- Modify: `cli/src/cli.ts`
- Test: `cli/feature-workflow.test.mjs`

**Steps**:
1. Create `cli/src/commands/feature-workflow.ts`.
2. Export small orchestration functions:
   - `cmdFeatureStart(db, input)`
   - `cmdFeatureStatus(db, planningTaskId)`
   - `cmdFeatureMaterialize(db, planningTaskId)` placeholder returning not-yet-implemented.
3. Add function comments for every exported function.
4. Wire a new CLI command group in `cli/src/cli.ts`:
   - `pic feature start <title>`
   - `pic feature status <planning-task-id>`
5. Add initial tests in `cli/feature-workflow.test.mjs`.
6. Run:
   ```bash
   cd cli && npm test
   ```
7. Expected output:
   - Existing tests pass.
   - New feature workflow tests pass.

**Commit message**:
```text
feat: add feature workflow command skeleton
```

---

### Task 2: Implement `pic feature start`

**Files**:
- Modify: `cli/src/commands/feature-workflow.ts`
- Modify: `cli/src/cli.ts`
- Test: `cli/feature-workflow.test.mjs`

**Steps**:
1. `cmdFeatureStart` should create or reuse an epic.
2. Add options:
   - `--epic <title-or-id>`
   - `--mode <quick|standard|designed|full>` default `designed`
   - `--description <text>`
   - `--priority <low|medium|high>` default `medium`
3. Create a parent planning task under the epic.
4. Set planning task fields:
   - `workflow_mode = designed/full`
   - `design_status = pending`
   - `refined = 0`
   - description includes intake metadata.
5. Return JSON:
   ```json
   {
     "epic": { ... },
     "planning_task": { ... },
     "next_step": "scan"
   }
   ```
6. Test that no task items are generated yet.
7. Run:
   ```bash
   cd cli && npm test
   ```

**Commit message**:
```text
feat: implement feature start workflow
```

---

## Milestone 2: Scan and QA/RRI Gates

### Task 3: Add feature status gate evaluator

**Files**:
- Modify: `cli/src/commands/feature-workflow.ts`
- Test: `cli/feature-workflow.test.mjs`

**Steps**:
1. Implement `cmdFeatureStatus` to inspect the planning task and related artifacts.
2. Return gate booleans:
   - `has_scan_report`
   - `has_requirements`
   - `has_design`
   - `design_approved`
   - `has_ready_tips`
   - `has_child_tasks`
   - `can_materialize`
   - `can_work`
3. Include `next_step` value:
   - `scan`
   - `qa`
   - `design`
   - `approve_design`
   - `materialize`
   - `work`
4. Add tests for empty, scanned, requirements-added, design-approved states.
5. Run:
   ```bash
   cd cli && npm test
   ```

**Commit message**:
```text
feat: add feature workflow status gates
```

---

### Task 4: Add pi prompt action for feature scan

**Files**:
- Modify: `pi-ext/tool.ts`
- Modify: `pi-ext/task-prompts.ts`
- Test: `pi-ext/task-prompts.test.ts`

**Steps**:
1. Add tool action `feature_scan` or route existing `scan_task` with feature-specific context.
2. Prompt should ask for a persisted scan report using existing `save_scan_report` action.
3. The prompt must include:
   - feature title
   - epic/project context
   - codebase areas to inspect
   - expected scan report fields
4. Test prompt includes instruction to persist via `task_manager.save_scan_report`.
5. Run:
   ```bash
   cd pi-ext && npm test
   ```

**Commit message**:
```text
feat: add feature scan prompt flow
```

---

### Task 5: Add pi QA/RRI action for feature planning

**Files**:
- Modify: `pi-ext/tool.ts`
- Modify: `pi-ext/task-prompts.ts`
- Test: `pi-ext/task-prompts.test.ts`

**Steps**:
1. Add feature-focused QA/RRI prompt builder.
2. Prompt should ask clarifying questions and produce requirements.
3. Require persisted requirements through existing `add_requirement` action.
4. Gate rule: do not proceed to design until requirements exist or user explicitly confirms no additional requirements.
5. Test prompt includes scan context and requirement persistence instructions.
6. Run:
   ```bash
   cd pi-ext && npm test
   ```

**Commit message**:
```text
feat: add feature qa requirements prompt
```

---

## Milestone 3: Design and Approval

### Task 6: Add feature design prompt

**Files**:
- Modify: `pi-ext/task-prompts.ts`
- Modify: `pi-ext/tool.ts`
- Test: `pi-ext/task-prompts.test.ts`

**Steps**:
1. Reuse or wrap `buildTaskDesignPrompt` for feature planning tasks.
2. Prompt must output:
   - Vision summary
   - Blueprint
   - Contracts/interfaces
   - Phase plan
   - Risks and rollback
   - Testing strategy
3. Require saving via `save_design`.
4. Require explicit approval via `approve_design` before materialization.
5. Run:
   ```bash
   cd pi-ext && npm test
   ```

**Commit message**:
```text
feat: add feature design prompt
```

---

### Task 7: Enforce design approval before materialization

**Files**:
- Modify: `cli/src/commands/feature-workflow.ts`
- Test: `cli/feature-workflow.test.mjs`

**Steps**:
1. `cmdFeatureMaterialize` should fail unless:
   - planning task exists
   - scan report exists for designed/full
   - at least one requirement exists
   - latest design is approved
2. Return structured error:
   ```json
   { "error": "Design must be approved before materialization", "next_step": "approve_design" }
   ```
3. Add tests for each missing gate.
4. Run:
   ```bash
   cd cli && npm test
   ```

**Commit message**:
```text
feat: enforce feature materialization gates
```

---

## Milestone 4: Generate Defined Tasks and Task Items

### Task 8: Parse design phase plan into child tasks

**Files**:
- Modify: `cli/src/commands/feature-workflow.ts`
- Reuse: `cli/src/commands/workflow-repair-parser.ts`
- Test: `cli/feature-workflow.test.mjs`

**Steps**:
1. Reuse existing phase-plan parser where possible.
2. Read latest approved design for the planning task.
3. Extract phase/task definitions from the design.
4. Create child tasks under the same epic.
5. Add phase metadata with `cmdTaskSetPhase` or equivalent existing helper.
6. Add dependencies between sequential phases.
7. Make operation idempotent: rerunning materialization updates existing child tasks instead of duplicating.
8. Run:
   ```bash
   cd cli && npm test
   ```

**Commit message**:
```text
feat: materialize feature design into child tasks
```

---

### Task 9: Generate task items from design/TIPs

**Files**:
- Modify: `cli/src/commands/feature-workflow.ts`
- Test: `cli/feature-workflow.test.mjs`

**Steps**:
1. For each child task, derive checklist items from:
   - phase plan details
   - linked TIPs
   - acceptance criteria
2. Use existing task item insertion helpers.
3. Do not add duplicate task items on rerun.
4. Mark child tasks refined once items are present and required artifacts are attached.
5. Test:
   - items created for each child task
   - rerun does not duplicate items
   - refined flag set only when child task is ready
6. Run:
   ```bash
   cd cli && npm test
   ```

**Commit message**:
```text
feat: generate task items from approved feature design
```

---

### Task 10: Link materialized Tasks to requirements and dependencies

**Files**:
- Modify: `cli/src/commands/feature-workflow.ts`
- Test: `cli/feature-workflow.test.mjs`

**Steps**:
1. Map each child Task to the requirement keys declared by its Task Plan node.
2. Persist ordered Task-to-Task dependency edges.
3. Return the materialized Task summary.
4. Run:
   ```bash
   cd cli && npm test
   ```

**Commit message**:
```text
feat: link feature tasks to requirements and dependencies
```

---

## Milestone 5: Continuous Implementation

### Task 11: Add `pic feature work` orchestration

**Files**:
- Modify: `cli/src/commands/feature-workflow.ts`
- Modify: `cli/src/cli.ts`
- Test: `cli/feature-workflow.test.mjs`

**Steps**:
1. Add `pic feature work <planning-task-id>`.
2. Return the next eligible child task based on:
   - phase order
   - dependencies complete
   - task status not done/cancelled
   - design and Task dependency gates satisfied
3. If no child task is eligible, return why:
   - blocked dependency
   - awaiting review
   - awaiting verification
   - all done
4. Do not implement code inside CLI; return orchestration state for pi extension.
5. Run:
   ```bash
   cd cli && npm test
   ```

**Commit message**:
```text
feat: add feature continuous work selector
```

---

### Task 12: Add `/task feature work` pi command

**Files**:
- Modify: `pi-ext/commands.ts`
- Modify: `pi-ext/phase-orchestration.ts`
- Test: `pi-ext/task-browser-actions.test.ts` or new `pi-ext/feature-workflow.test.ts`

**Steps**:
1. Add `/task feature work <planning-task-id>` command.
2. Call `pic feature work <planning-task-id>`.
3. If next child task exists, send hidden work prompt using existing `sendHiddenWorkPrompt` pattern.
4. If blocked, notify user with the blocker reason.
5. Test command routing and blocked-state formatting.
6. Run:
   ```bash
   cd pi-ext && npm test
   ```

**Commit message**:
```text
feat: add task feature continuous work command
```

---

### Task 13: Integrate review and verification gates

**Files**:
- Modify: `pi-ext/tool.ts`
- Modify: `pi-ext/workflow-gates.ts`
- Modify: `pi-ext/phase-orchestration.ts`
- Test: `pi-ext/workflow-gates.test.ts`

**Steps**:
1. Ensure continuous feature work stops after completing all task items.
2. Require task-reviewer to persist review status.
3. Require verification report before marking child task done.
4. Roll up completed child phases to parent planning task.
5. Continue only after review/verification gates pass.
6. Run:
   ```bash
   cd pi-ext && npm test
   ```

**Commit message**:
```text
feat: gate continuous feature work on review and verification
```

---

## Milestone 6: UX Polish and Docs

### Task 14: Add `/task feature` command help and shortcuts

**Files**:
- Modify: `pi-ext/commands.ts`
- Modify: `pi-ext/task-browser-menu.ts`
- Test: `pi-ext/task-browser-menu.test.ts`

**Steps**:
1. Add help text:
   - `/task feature <title>`
   - `/task feature continue <planning-task-id>`
   - `/task feature work <planning-task-id>`
2. Add menu hint for planning tasks with next workflow action.
3. Ensure normal `/task create` remains lightweight.
4. Run:
   ```bash
   cd pi-ext && npm test
   ```

**Commit message**:
```text
feat: expose feature workflow in task command help
```

---

### Task 15: Add web dashboard feature workflow visibility

**Files**:
- Modify: `cli/src/web/dashboard-service.ts`
- Modify: `cli/src/web/api.ts`
- Modify: `cli/web-assets/app.js`
- Modify: `cli/web-assets/index.html`
- Modify: `cli/web-assets/app.css`
- Test: `cli/web-dashboard-service.test.mjs`, `cli/web-api.test.mjs`

**Steps**:
1. Add API endpoint for planning task status:
   - `GET /api/projects/:project_id/features/:task_id/status`
2. Show planning task gate state in the task detail view.
3. Add labels for:
   - Scan needed
   - QA needed
   - Design pending/approved
   - Ready to materialize
   - Ready to work
4. Run:
   ```bash
   cd cli && npm test
   ```

**Commit message**:
```text
feat: show feature workflow gates in web dashboard
```

---

### Task 16: Document the feature workflow

**Files**:
- Create: `docs/feature-workflow.md`
- Modify: `docs/web-dashboard.md` if linking from dashboard docs

**Steps**:
1. Document the user-facing flow.
2. Include examples for CLI and pi extension.
3. Explain gates and artifacts.
4. Explain when to use quick vs standard vs designed/full workflows.
5. Run:
   ```bash
   cd cli && npm test
   cd ../pi-ext && npm test
   ```

**Commit message**:
```text
docs: add feature workflow guide
```

---

## Suggested Final UX

### Start a feature

```text
/task feature Add dependency graph to web dashboard
```

Expected behavior:

1. Creates/reuses epic.
2. Creates planning task.
3. Sends scan prompt.
4. User/agent saves scan report.
5. Continues to QA/RRI.
6. Saves requirements.
7. Continues to design.
8. Saves design.
9. Requires approval.
10. Materializes child tasks/items.
11. Starts continuous implementation.

### Continue an interrupted feature

```text
/task feature continue t-plan-123
```

Expected behavior:

- Reads current gate state.
- Tells the user the next step.
- Sends the appropriate prompt or action.

### Implement continuously

```text
/task feature work t-plan-123
```

Expected behavior:

- Picks the next eligible child task.
- Sends hidden work prompt.
- Stops at review/verification gates.
- Rolls up parent progress after each passed child phase.

---

## Open Questions

1. Should `pic feature start` always create a new epic, or should it default to creating only a planning task under an existing epic when `--epic` is provided?
2. Should materialization be fully deterministic from saved design markdown, or should the LLM create child tasks through tool calls after design approval?
3. Should quick/standard feature workflows skip design approval, or simply use a lighter design artifact?
4. Should continuous implementation auto-commit after each verified child task?
5. Should the web UI offer buttons for each next gate, or remain read-only/status-first for this workflow?

## Recommended Implementation Order

1. CLI skeleton and `feature start`.
2. Status gate evaluator.
3. Pi `/task feature` command that drives scan/QA/design prompts.
4. Materialization into child tasks/items.
5. Continuous work orchestration.
6. Web dashboard visibility.
7. Documentation.

This order keeps the feature usable early while avoiding a large all-at-once workflow engine.
