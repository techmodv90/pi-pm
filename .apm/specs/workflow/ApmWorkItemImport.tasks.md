# Tasks: ApmWorkItemImport

**Feature:** workflow/ApmWorkItemImport
**Plan:** .apm/specs/workflow/ApmWorkItemImport.plan.md
**Date:** 2026-09-06
**Status:** Approved

## Format: [TID] [P?] [Priority] [US?] Description

## Scenario Map

| US | Scenario (.feature) | Tier |
|----|--------------------|------|
| US1 | Import creates the epic-feature-task hierarchy with deterministic edges | @P1 |
| US2 | Rule D validation mismatch aborts the import with zero writes | @P1 |
| US3 | Dry-run prints the full JSON graph and writes nothing | @P2 |
| US4 | Cross-feature ordering is carried by feature edges only | @P2 |
| US5 | Phase 5 tasks become epic-level verification, not Work Items | @P2 |
| US6 | Re-importing an already-imported task list is rejected | @P2 |
| US7 | Import gate refuses a task list whose planning artifacts are not intact | @P2 |
| US8 | Import gate refuses a task list whose companion .feature is not @ready | @P2 |
| US9 | Milestone label carries the PRD version with no merge authority | @P3 |
| US10 | Import creates no branch | @P3 |

---

## Phase 1: Setup (parallelizable)

- [T001] [P] Setup: Add test fixtures — a valid approved 23-task `.tasks.md` fixture (5 phases, 3 tier headers, Execution Order block, companion `.plan.md`/`.feature` stubs) plus corrupt variants, as Go string constants.
  - Files: `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Plan Architecture parseTasksFile inputs; real temporary SQLite pattern.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportFixture` compiles and runs against the fixture constants.

- [T002] [P] Setup: Wire an empty `case "import-apm"` into `cmdWorkflow` returning a usage error, proving command dispatch without behavior.
  - Files: `go-pic/cmd/pic/workflow.go`, `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Plan Architecture (one `case` in `cmdWorkflow`).
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestWorkflowCommandDispatch` passes with the new subcommand listed.

---

## Phase 2: Foundation (sequential after Setup)

- [T003] RED: Write failing parser tests covering task-block extraction (TID, tags, description, `Files:`, `Trace:`, `Acceptance:`), phase and tier-header detection, Execution Order block capture, and `Status:`/`Feature:`/`Plan:` headers.
  - Files: `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Plan Architecture parseTasksFile; API Contract gate order.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportParse` fails because the parser does not exist.

- [T004] GREEN: Implement `parseTasksFile` with typed structs and no inference beyond the documented format.
  - Files: `go-pic/cmd/pic/apm_import.go`, `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Plan Architecture; Complexity Tracking (deterministic parse).
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportParse` passes for valid and malformed fixtures.

- [T005] RED: Write failing Rule D validator tests: missing TID in block, duplicate TID, phase mismatch, `║` pair without `[P]`, US tag vs Scenario Map mismatch, missing tier header.
  - Files: `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Spec US2; Plan API Contract gate order step 5–6.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportRuleD` fails because validation is not implemented.

- [T006] GREEN: Implement the Rule D validator returning the complete discrepancy list.
  - Files: `go-pic/cmd/pic/apm_import.go`, `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Spec US2; Plan Defensive Coding invariant.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportRuleD` passes with every discrepancy case represented.

---

## Phase 3: Priorities (per tier, grouped by scenario)

### P1: Critical Path

- [T007] [P1] [US1] [US3] RED: Write a failing dry-run test asserting the full JSON graph (epic, 3 features, tasks with verbatim blocks, `edges`) on stdout, zero Work Item rows written, and identical output across two runs.
  - Files: `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Spec US1/US3; Plan API Contract success output.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportDryRun` fails because the graph builder and renderer do not exist.

- [T008] [P1] [US2] RED: Write a failing abort test: corrupt Execution Order block → exit 1, JSON error with the full discrepancy list, store unchanged.
  - Files: `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Spec US2; Plan API Contract error shape.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportAbort` fails before abort wiring exists.

- [T009] [P1] [US1] [US3] GREEN: Implement `buildGraph` (tier grouping: Phase 1+2+P1→F1, P2→F2, P3+polish→F3; Rules A–C edges; Phase 5 → epic verification commands) and `renderJSON`, plus `--milestone`/`--dry-run` flag handling.
  - Files: `go-pic/cmd/pic/apm_import.go`, `go-pic/cmd/pic/workflow.go`, `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Plan Architecture; Complexity Tracking (tier derivation from file).
  - Acceptance: `cd go-pic && go test ./cmd/pic -run 'TestApmImportDryRun|TestApmImportAbort'` passes, including the Status-not-Approved gate.

- [T010] [P1] [US1] RED: Write a failing creation test: non-dry-run creates epic (branch-owning semantics), 3 features, task subitems with verbatim description + `Acceptance:` as criteria, `work_item_dependencies` edges within features only, all in one transaction.
  - Files: `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Spec US1; Plan Data Model mapping table.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportCreatesWorkItems` fails because creation is not implemented.

- [T011] [P1] [US1] GREEN: Implement `createWorkItems` — single SQLite transaction inserting epic, features, tasks, edges; rollback leaves the store empty on any failure.
  - Files: `go-pic/cmd/pic/apm_import.go`, `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Plan Complexity Tracking (single transaction); Data Model.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportCreatesWorkItems` passes, including a forced-failure rollback case.

### P2: Important

- [T012] [P2] [US7] [US8] RED: Write failing gate tests — missing companion `.plan.md` exits 1; companion `.feature` tagged `@draft` exits 1; both write nothing.
  - Files: `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Spec US7/US8; Plan API Contract gate order steps 2–3.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportGates` fails before gate checks exist.

- [T013] [P2] [US7] [US8] GREEN: Implement gates 2–3 (plan exists, feature `@ready`) at the importer entry, before any creation.
  - Files: `go-pic/cmd/pic/apm_import.go`, `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Plan IV-B Entry Point Rule; Complexity Tracking.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportGates` passes.

- [T014] [P2] [US6] RED: Write a failing re-import test: second import of the same file content exits 1 with an "already imported" error naming the epic, and creates nothing.
  - Files: `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Spec US6; Plan Complexity Tracking (hard stop).
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportReimport` fails before the provenance check exists.

- [T015] [P2] [US6] GREEN: Implement the `import:<sha256-12>` provenance label and hard-stop check.
  - Files: `go-pic/cmd/pic/apm_import.go`, `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Plan Data Model (provenance via label).
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportReimport` passes.

- [T016] [P2] [US4] [US5] RED: Write failing tests proving no task edge crosses feature containment (T010→T011 pair spans F1/F2 with no edge) and no task Work Items exist for Phase 5 TIDs while the epic description records their commands.
  - Files: `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Spec US4/US5; Plan Architecture task→tier assignment.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run 'TestApmImportNoCrossFeatureEdges|TestApmImportPhase5Epic'` fails.

- [T017] [P2] [US4] [US5] GREEN: Enforce both constraints in `buildGraph`/`createWorkItems`.
  - Files: `go-pic/cmd/pic/apm_import.go`, `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Plan Architecture constraint "task edges never cross feature containment".
  - Acceptance: `cd go-pic && go test ./cmd/pic -run 'TestApmImportNoCrossFeatureEdges|TestApmImportPhase5Epic'` passes.

### P3+: Nice to Have

- [T018] [P3] [US9] RED/GREEN: Add a failing test that the epic carries `milestone:<version>` and the `import:<hash>` labels and nothing else, then implement the label inserts.
  - Files: `go-pic/cmd/pic/apm_import.go`, `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Spec US9; Plan Data Model (milestone label metadata only).
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportLabels` passes.

- [T019] [P3] [US10] RED/GREEN: Add a failing test that import succeeds in a temporary directory containing no git repository and the epic carries no branch metadata, then confirm the implementation performs no git operations (no `os/exec`).
  - Files: `go-pic/cmd/pic/apm_import.go`, `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Spec US10; Plan Constraints (no git operations).
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImportNoGit` passes.

---

## Phase 4: Polish (parallelizable)

- [T020] [P] REFACTOR: Review importer error paths — every failure returns the JSON error object with `discrepancies` populated where applicable; no swallowed errors; behavior unchanged.
  - Files: `go-pic/cmd/pic/apm_import.go`, `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Plan Defensive Coding invariant; API Contract error shape.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImport` passes.

- [T021] [P] Docs/Inventory: Verify spec/plan/comments stay consistent with the implemented flag names and label formats; no DBML change exists to make.
  - Files: `.apm/specs/workflow/ApmWorkItemImport.plan.md`, `go-pic/cmd/pic/apm_import_test.go`
  - Trace: Plan API Contract; Data Model N/A note.
  - Acceptance: `rg -n "import-apm|import:|milestone:" .apm/specs/workflow/ApmWorkItemImport.plan.md go-pic/cmd/pic/apm_import.go` shows matching names.

---

## Phase 5: Final Verification (sequential)

- [T022] Run the focused importer tests and the full Go suite.
  - Files: `go-pic/cmd/pic/apm_import_test.go`
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestApmImport && go test ./...` passes.

- [T023] Verify vet and build.
  - Files: `go-pic/cmd/pic/`
  - Acceptance: `cd go-pic && go vet ./... && go build ./...` passes.

---

## Execution Order

```text
Phase 1: T001 ║ T002
           ↓
Phase 2: T003 → T004 → T005 → T006
           ↓
Phase 3 P1: T007 → T008 → T009 → T010 → T011
           ↓
Phase 3 P2: T012 → T013 → T014 → T015 → T016 → T017
           ↓
Phase 3 P3: T018 → T019
           ↓
Phase 4: T020 ║ T021
           ↓
Phase 5: T022 → T023
```

**Legend:** `→` sequential | `║` parallel

---

## Nyquist Mapping

| Requirement | Source | Verifying task | Verification command | Status |
|-------------|--------|----------------|----------------------|--------|
| R01 Import creates the epic-feature-task hierarchy with deterministic depends_on edges | Import happy path (P1) | T007/T009/T010/T011/T022 | `cd go-pic && go test ./cmd/pic -run 'TestApmImportDryRun|TestApmImportCreatesWorkItems'` | Covered |
| R02 Rule D mismatch aborts with full discrepancy list and zero writes | Rule D abort (P1) | T005/T006/T008/T009/T022 | `cd go-pic && go test ./cmd/pic -run 'TestApmImportRuleD|TestApmImportAbort'` | Covered |
| R03 Dry-run prints the complete JSON graph, writes nothing, deterministic | Dry-run purity (P2) | T007/T009/T022 | `cd go-pic && go test ./cmd/pic -run TestApmImportDryRun` | Covered |
| R04 Task edges never cross feature containment | Cross-feature prohibition (P2) | T016/T017/T022 | `cd go-pic && go test ./cmd/pic -run TestApmImportNoCrossFeatureEdges` | Covered |
| R05 Phase 5 tasks become epic-level verification, not Work Items | Phase 5 mapping (P2) | T007/T016/T017/T022 | `cd go-pic && go test ./cmd/pic -run TestApmImportPhase5Epic` | Covered |
| R06 Re-import is a hard stop with explicit error and no writes | Re-import rejection (P2) | T014/T015/T022 | `cd go-pic && go test ./cmd/pic -run TestApmImportReimport` | Covered |
| R07 Import gate verifies companion .plan.md and @ready .feature | Import gate (P2) | T012/T013/T022 | `cd go-pic && go test ./cmd/pic -run TestApmImportGates` | Covered |
| R08 Milestone label is metadata only — no merge authority | Milestone label (P3) | T018/T022 | `cd go-pic && go test ./cmd/pic -run TestApmImportLabels` | Covered |
| R09 Import performs no git operations; branch cut at authorization | No-branch guarantee (P3) | T019/T022 | `cd go-pic && go test ./cmd/pic -run TestApmImportNoGit` | Covered |

## Nyquist Result

Total requirements: 9
Covered: 9
P1 coverage: 100%
P2 coverage: 100%
P3+ coverage: 100%
Result: PASS — no verification debt registered.
