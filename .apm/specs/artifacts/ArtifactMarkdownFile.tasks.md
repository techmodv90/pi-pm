# Tasks: ArtifactMarkdownFile

**Feature:** artifacts/ArtifactMarkdownFile
**Plan:** .apm/specs/artifacts/ArtifactMarkdownFile.plan.md
**Date:** 2026-09-06
**Status:** Approved

## Format: [TID] [P?] [Priority] [US?] Description

## Scenario Map

| US | Scenario (.feature) | Tier |
|----|--------------------|------|
| US1 | Saving an artifact stores its content as a markdown file | @P1 |
| US2 | Best-effort projection keeps the canonical save on file-write failure | @P1 |
| US3 | New revision creates a new file and never rewrites prior files | @P2 |
| US4 | Existing file with conflicting bytes blocks the save | @P2 |
| US5 | File drift from the canonical content is detectable | @P2 |
| US6 | Backfill files for artifacts saved before the projection existed | @P3 |
| US7 | Dashboard links each artifact to its file | @P3 |

---

## Phase 1: Setup (parallelizable)

- [T001] [P] Setup: Extend the Go CLI artifact test fixtures with helpers for deterministic project roots, artifact rows, projected paths, and event assertions.
  - Files: `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Plan Technical Context (real temporary SQLite databases and temporary project roots); Architecture (filesystem operations localized for tests).
  - Acceptance: Fixture helpers compile and the focused package test command runs to the existing fixture baseline: `cd go-pic && go test ./cmd/pic -run TestArtifactFileProjectFixture`.

- [T002] [P] Setup: Add dashboard test coverage wiring for the existing Work Item detail response and artifact revision rendering seam without changing runtime behavior.
  - Files: `go-pic/cmd/pic/pic_cli_test.go`, `go-pic/web/src/routes/work-item/[id]/+page.svelte`
  - Trace: Plan Read side (CLI detail payload and dashboard Artifact Revisions table).
  - Acceptance: Existing Go and web build entry points remain runnable: `cd go-pic && go test ./cmd/pic -run 'TestNativeWorkItemGenericShowUsesCanonicalShape|TestWorkItemCRUDAndContainment'` and `cd go-pic/web && npm run build`.

---

## Phase 2: Foundation (sequential after Setup)

- [T003] RED: Write a failing migration test proving `artifact_files` is created with the ratified columns, unique bindings, foreign keys, and idempotent re-open behavior.
  - Files: `go-pic/cmd/pic/pic_cli_test.go`
  - Trace: Plan Data Model and Dependencies; repository migration policy.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestArtifactFilesSchemaMigration` fails because the table is not yet present.

- [T004] GREEN: Add the `artifact_files` schema definition and ordered migration, preserving canonical artifact immutability and making the migration safe when migration records are replayed.
  - Files: `go-pic/cmd/pic/workflow_schema.go`, `go-pic/cmd/pic/schema_bootstrap.go`, `go-pic/cmd/pic/pic_cli_test.go`
  - Trace: Plan Data Model; Architecture migration requirement; Complexity Tracking (separate binding table).
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestArtifactFilesSchemaMigration` passes twice against the same database with no duplicate-table or duplicate-column failure.

- [T005] RED: Write failing unit/integration tests for deterministic Work Item/stage/revision path construction, Work Item ID validation, SHA-256 comparison, and atomic file creation semantics.
  - Files: `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Plan Architecture (new `artifact_files.go` module), Technical Context (ID regex, stdlib hashing, temp-file plus rename).
  - Acceptance: `cd go-pic && go test ./cmd/pic -run 'TestArtifactFilePath|TestArtifactFileHash|TestArtifactFileAtomicWrite'` fails for the missing projection module.

- [T006] GREEN: Implement the projection module’s path, hash, conflict, atomic-write, binding, and drift primitives with the plan’s permissions and deterministic naming.
  - Files: `go-pic/cmd/pic/artifact_files.go`, `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Plan Architecture; API Contract conflict semantics; Complexity Tracking (atomic rename and separate binding table).
  - Acceptance: `cd go-pic && go test ./cmd/pic -run 'TestArtifactFilePath|TestArtifactFileHash|TestArtifactFileAtomicWrite'` passes, including invalid IDs and unchanged existing bytes on conflict.

---

## Phase 3: Priorities (per tier, grouped by scenario)

### P1: Critical Path

- [T007] [P1] [US1] RED: Add a failing end-to-end test that saves artifacts for every planning stage and asserts canonical hash, exact markdown bytes, deterministic `file_path`, binding row, and response field for each stage.
  - Files: `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Feature US1 and NC-1/NC-3; Plan API Contract and Architecture save flow.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestArtifactSaveProjectsAllPlanningStages` fails before save integration is implemented.

- [T008] [P1] [US2] RED: Add a failing end-to-end test that makes the projection directory unwritable and asserts successful canonical persistence, empty response path, warning event payload, and no projected file.
  - Files: `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Feature US2; Plan API Contract projection failure semantics and Defensive Coding invariant.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestArtifactProjectionFailureIsBestEffort` fails before save integration is implemented.

- [T009] [P1] [US1] [US2] GREEN: Extend `workItemArtifactSave` with revision-aware pre-flight conflict checking, post-commit best-effort projection/binding, warning events, and `file_path` response behavior.
  - Files: `go-pic/cmd/pic/work_items.go`, `go-pic/cmd/pic/artifact_files.go`, `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Plan Architecture numbered save flow; Entry Point Rule; API Contract.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run 'TestArtifactSaveProjectsAllPlanningStages|TestArtifactProjectionFailureIsBestEffort'` passes and confirms failed projection leaves the artifact row intact.

- [T010] [P1] [US1] RED/GREEN: Add a benchmark or bounded timing test for projection overhead and record the p95 `< 50ms` success criterion without weakening functional assertions.
  - Files: `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Feature success criteria; Plan Technical Context performance criterion.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestArtifactProjectionP95Under50ms -count=1` reports the measured p95 and fails if it exceeds 50ms in the test environment.

### P2: Important

- [T011] [P2] [US3] RED: Add a failing revision test that saves two revisions and proves revision 2 uses a new path while revision 1 bytes remain unchanged.
  - Files: `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Feature US3; Plan Flow invariant and immutable artifact model.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestArtifactRevisionCreatesNewFile` fails before revision projection behavior is complete.

- [T012] [P2] [US4] RED: Add a failing conflict test that pre-creates divergent bytes and asserts non-zero save, conflict text containing `artifact file conflict` and the path, unchanged bytes, and no artifact row.
  - Files: `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Feature US4; Plan API Contract conflict pre-flight and Complexity Tracking.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestArtifactFileConflictBlocksSave` fails before conflict validation is integrated.

- [T013] [P2] [US5] RED: Add a failing integrity-check test covering `ok`, `drift`, and `missing` statuses for bound artifact files.
  - Files: `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Feature US5; Plan API Contract and NC-7 on-demand hash comparison.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestArtifactFileIntegrityCheck` fails before the command is routed.

- [T014] [P2] [US3] [US4] [US5] GREEN: Route artifact backfill/check commands and complete save conflict and revision behavior using the projection module’s primitives.
  - Files: `go-pic/cmd/pic/work_items.go`, `go-pic/cmd/pic/artifact_files.go`, `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Plan Architecture command routing; API Contract for `artifact-backfill` and `artifact-check`.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run 'TestArtifactRevisionCreatesNewFile|TestArtifactFileConflictBlocksSave|TestArtifactFileIntegrityCheck'` passes.

### P3+: Nice to Have

- [T015] [P3] [US6] RED: Add a failing backfill test with unbound historical artifacts and divergent pre-existing bytes, asserting every row is bound and canonical content overwrites the file.
  - Files: `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Feature US6; Plan API Contract backfill recovery intent and Complexity Tracking.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestArtifactFileBackfill` fails before the backfill handler is complete.

- [T016] [P3] [US6] GREEN: Implement artifact backfill selection, canonical overwrite, binding insertion, and `{written,bound,skipped}` JSON output.
  - Files: `go-pic/cmd/pic/artifact_files.go`, `go-pic/cmd/pic/work_items.go`, `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Plan API Contract and Flow invariant.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestArtifactFileBackfill` passes and verifies stored content is restored exactly.

- [T017] [P3] [US7] RED: Add a failing detail/read test that binds an artifact file and asserts the Work Item detail payload contains `file_path` for each artifact.
  - Files: `go-pic/cmd/pic/pic_cli_test.go`
  - Trace: Feature US7; Plan Read side LEFT JOIN requirement.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestWorkItemDetailIncludesArtifactFilePath` fails before the read query is updated.

- [T018] [P3] [US7] GREEN: Add the artifact-file LEFT JOIN to the detail query and render each artifact’s `file_path` in the Artifact Revisions table.
  - Files: `go-pic/cmd/pic/misc.go`, `go-pic/web/src/routes/work-item/[id]/+page.svelte`, `go-pic/cmd/pic/pic_cli_test.go`
  - Trace: Plan Read side and dashboard surface; no new endpoint.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestWorkItemDetailIncludesArtifactFilePath` and `cd go-pic/web && npm run build` both pass.

---

## Phase 4: Polish (parallelizable)

- [T019] REFACTOR: Review projection error paths and keep all caught filesystem/database errors observable through returned errors or `artifact_projection_failed` warning events, with behavior unchanged.
  - Files: `go-pic/cmd/pic/artifact_files.go`, `go-pic/cmd/pic/work_items.go`, `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Plan Defensive Coding invariant and API Contract warning payload.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run 'TestArtifact(File|Projection|Revision|Backfill|FileIntegrity)'` passes.

- [T020] Docs/Inventory: Update the DBML projection inventory and artifact test comments only if implementation names or schema details differ from the approved plan; do not alter scope or behavior.
  - Files: `.apm/specs/db_schema/artifacts.dbml`, `go-pic/cmd/pic/artifact_files_test.go`
  - Trace: Plan Data Model ratification and repository inventory conventions.
  - Acceptance: `rg -n "artifact_files|content_sha256|file_path" .apm/specs/db_schema/artifacts.dbml go-pic/cmd/pic/artifact_files_test.go` matches the implemented names.

---

## Phase 5: Final Verification (sequential)

- [T021] Run the focused Go artifact tests and full Go suite.
  - Files: `go-pic/cmd/pic/artifact_files_test.go`, `go-pic/cmd/pic/pic_cli_test.go`
  - Acceptance: `cd go-pic && go test ./cmd/pic -run 'TestArtifact|TestWorkItemDetailIncludesArtifactFilePath' && go test ./...` passes.

- [T022] Verify web build, Pi extension tests, lint, and type-check.
  - Files: `go-pic/web/src/routes/work-item/[id]/+page.svelte`, `pi-ext/`
  - Acceptance: `cd go-pic/web && npm run build && cd ../../pi-ext && npm test && npm run lint && npm run check` passes.

- [T023] Verify the production build generates the runtime artifacts through the supported build command.
  - Files: `pi-ext/package.json`, generated `go-pic/web/build`, generated `go-pic/dist/pic`
  - Acceptance: `cd pi-ext && npm run build` exits successfully and leaves `go-pic/web/build` and `go-pic/dist/pic` present.

---

## Execution Order

```text
Phase 1: T001 ║ T002
           ↓
Phase 2: T003 → T004 → T005 → T006
           ↓
Phase 3 P1: T007 → T008 → T009 → T010
           ↓
Phase 3 P2: T011 → T012 → T013 → T014
           ↓
Phase 3 P3: T015 → T016 → T017 → T018
           ↓
Phase 4: T019 → T020
           ↓
Phase 5: T021 → T022 → T023
```

**Legend:** `→` sequential | `║` parallel

## Nyquist Mapping

| Requirement | Source | Verifying task | Verification command | Status |
|-------------|--------|----------------|----------------------|--------|
| R01 Save every planning-stage artifact to deterministic markdown with exact bytes/hash and binding | `.apm/specs/artifacts/ArtifactMarkdownFile.feature` US1; Plan API Contract | T007/T009/T021 | `cd go-pic && go test ./cmd/pic -run 'TestArtifactSaveProjectsFile|TestArtifact'` | Covered |
| R02 Projection failure preserves canonical row, records warning, and leaves no file | Feature US2; Plan API Contract | T008/T009/T021 | `cd go-pic && go test ./cmd/pic -run TestArtifactProjectionFailureIsBestEffort` | Covered |
| R03 All seven planning stages use the save projection; execution reports remain out of scope | Feature US1 clarification NC-1/NC-3; Plan Summary | T007/T009/T021 | `cd go-pic && go test ./cmd/pic -run TestArtifactSaveProjectsAllPlanningStages` | Covered |
| R04 Revision 2 gets a new file and prior bytes remain unchanged | Feature US3 | T011/T014/T021 | `cd go-pic && go test ./cmd/pic -run TestArtifactRevisionCreatesNewFile` | Covered |
| R05 Conflicting existing bytes block before DB mutation and preserve bytes | Feature US4; Plan Complexity Tracking | T012/T014/T021 | `cd go-pic && go test ./cmd/pic -run TestArtifactFileConflictBlocksSave` | Covered |
| R06 Drift check reports ok, drift, and missing via on-demand SHA-256 comparison | Feature US5/NC-7; Plan API Contract | T013/T014/T021 | `cd go-pic && go test ./cmd/pic -run TestArtifactFileIntegrityCheck` | Covered |
| R07 Backfill writes canonical bytes and binds every unbound artifact | Feature US6; Plan API Contract | T015/T016/T021 | `cd go-pic && go test ./cmd/pic -run TestArtifactFileBackfill` | Covered |
| R08 Detail payload and dashboard show `file_path` | Feature US7; Plan Read side | T017/T018/T022 | `cd go-pic && go test ./cmd/pic -run TestWorkItemDetailIncludesArtifactFilePath` and `cd go-pic/web && npm run build` | Covered |
| R09 `artifact_files` schema is ratified, constrained, and migration-idempotent | Plan Data Model; DBML | T003/T004/T021 | `cd go-pic && go test ./cmd/pic -run TestArtifactFilesSchemaMigration` | Covered |
| R10 Projection overhead is below 50ms p95 | Feature success criteria; Plan Technical Context | T010/T021 | `cd go-pic && go test ./cmd/pic -run TestArtifactProjectionP95Under50ms -count=1` | Covered |
| R11 Projection errors remain observable and no existing file is mutated on failure | Plan Defensive Coding and Reliability criteria | T008/T012/T019/T021 | `cd go-pic && go test ./cmd/pic -run 'TestArtifactProjectionFailureIsBestEffort|TestArtifactFileConflictBlocksSave'` | Covered |

## Nyquist Result

Total requirements: 11  
Covered: 11  
P1 coverage: 100%  
P2 coverage: 100%  
P3+ coverage: 100%  
Result: PASS — no verification debt registered.

## Risks

- T004 shared schema migration: Rollback is to stop before recording the migration; the additive table remains harmless and can be removed only through an explicitly reviewed database migration.
- T009 artifact-save public CLI behavior: Rollback is to revert the projection call and command changes while retaining the additive table; canonical artifact rows remain authoritative.
- T017 dashboard read surface: Rollback is to remove the LEFT JOIN/rendered cell; artifact persistence remains available through the CLI.
- T023 generated runtime artifacts: Rollback is to rerun the supported build after source correction; do not hand-edit generated output.
