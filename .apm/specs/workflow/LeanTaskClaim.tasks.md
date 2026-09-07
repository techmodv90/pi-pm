# Tasks: Lean Task Claim

**Feature:** workflow/LeanTaskClaim
**Plan:** .apm/specs/workflow/LeanTaskClaim.plan.md
**Date:** 2026-09-07
**Status:** Approved

Spec: .apm/specs/workflow/LeanTaskClaim.feature

## Format: [TID] [P?] [Priority] [US?] Description

## Scenario Map

| US  | Scenario                                   | Tier |
|-----|--------------------------------------------|------|
| US1 | Lean claim on bare task                    | @P1  |
| US2 | Single-writer exclusion                    | @P1  |
| US3 | Legacy state routes legacy                 | @P1  |
| US4 | Verbatim worker input                      | @P2  |
| US5 | Lean completion                            | @P2  |
| US6 | Review/verification not pack-bound         | @P2  |
| US7 | Legacy read-back integrity                 | @P3  |

## Phase 1: Setup (parallelizable)

- [T001] [P] Setup: Extend the Go CLI pipeline test fixtures with helpers for temporary SQLite stores that can seed bare tasks (no pack/materialization state) and legacy tasks (active pack or authorized materialization), plus event-log assertions.
  - Files: `go-pic/cmd/pic/pipeline_claim_test.go` (new)
  - Trace: Plan Invariants (real temporary SQLite databases); repository testing convention.
  - Acceptance: Fixture helpers compile and the focused command runs to the existing baseline: `cd go-pic && go test ./cmd/pic -run TestPipelineClaimFixture`.

- [T002] [P] Setup: Add a pi-ext scheduler test seam capturing the assembled worker input for a claimed task without spawning a real worker.
  - Files: `pi-ext/pipeline/pipeline-scheduler.test.ts`
  - Trace: Plan Lean worker input; Plan API/surface (scheduler input assembly).
  - Acceptance: `cd pi-ext && node --experimental-strip-types --test pipeline/pipeline-scheduler.test.ts` runs to the existing baseline with the new input-capture case pending.

---

## Phase 2: Foundation (sequential after Setup)

- [T003] GREEN: Implement state-driven routing in the pipeline claim entry — items with an active pack or authorized materialization take the unchanged legacy gates; items without take a lean claim branch that flips status, records claimant/time, and appends one claim event.
  - Files: `go-pic/cmd/pic/pipeline.go`
  - Trace: Plan Architecture routing decision point; requirements R01, R03.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run 'TestLeanClaimRouting|TestLeanClaimClaim'` passes, including "no pack/checkpoint/materialization rows created" and "legacy gates byte-for-byte unchanged" assertions.

- [T004] RED/GREEN: Add the single-writer test — a second lean claim while the task is in progress is rejected with a clear error and writes no state or event.
  - Files: `go-pic/cmd/pic/pipeline_claim_test.go`, `go-pic/cmd/pic/pipeline.go`
  - Trace: requirement R02; Feature US2.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestLeanClaimSingleWriter` passes.

---

## Phase 3: Priorities (per tier, grouped by scenario)

### P1: Claim path

- [T005] [P1] [US1] [US2] End-to-end lean claim integration: claim a bare task, verify the activity log ordering (claim event), and read the item back through `pic show` with status/claimant fields populated; verify a legacy-state task still reaches its legacy gates in the same store.
  - Files: `go-pic/cmd/pic/pipeline_claim_test.go`
  - Trace: requirements R01, R02, R03; Feature US1, US2, US3.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestLeanClaimEndToEnd` passes.

### P2: Worker input, completion, and review

- [T006] [P2] [US4] RED/GREEN: Lean worker input assembly — for state-routed-lean tasks the scheduler uses the stored description verbatim (Acceptance and Behavior context included) with no pack render and no pack columns in the run record.
  - Files: `pi-ext/pipeline/pipeline-scheduler.ts`, `pi-ext/pipeline/pipeline-scheduler.test.ts`
  - Trace: requirement R04; Plan Lean worker input.
  - Acceptance: `cd pi-ext && node --experimental-strip-types --test pipeline/pipeline-scheduler.test.ts` passes the verbatim-input assertions.

- [T007] [P2] [US5] RED/GREEN: Lean completion — status becomes done with a completion event appended; no completion-report row is created (pack columns are NOT NULL by design); affected-row counts asserted, no ad hoc SQL triggers.
  - Files: `go-pic/cmd/pic/pipeline.go`, `go-pic/cmd/pic/pipeline_claim_test.go`
  - Trace: requirement R05; Feature US5.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestLeanClaimCompletion` passes.

- [T008] [P2] [US6] RED/GREEN: Review and verification records for lean tasks reference task and run identifiers without requiring pack columns; a failed review routes a fix attempt without pack-hash cycle caps.
  - Files: `go-pic/cmd/pic/work_items.go`, `go-pic/cmd/pic/pipeline_claim_test.go`
  - Trace: requirement R05; Feature US6.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestLeanClaimReviewVerification` passes.

### P3+: Legacy integrity and close-out

- [T009] [P3] [US7] RED/GREEN: Legacy read-back integrity regression — historical done items with packs keep resolvable detail/completion/verification joins, and lean operations perform zero writes to legacy rows.
  - Files: `go-pic/cmd/pic/pipeline_claim_test.go`
  - Trace: requirement R06; Feature US7.
  - Acceptance: `cd go-pic && go test ./cmd/pic -run TestLegacyReadBackIntegrity` passes.

- [T010] [P3] Polish: exercise the lean close-out sequence end-to-end in the scheduler tests (worker → reviewer → contractor verification report → close) against a real temporary SQLite database.
  - Files: `pi-ext/pipeline/pipeline-scheduler.test.ts`
  - Trace: requirement R05; Plan Lean completion.
  - Acceptance: `cd pi-ext && node --experimental-strip-types --test pipeline/pipeline-scheduler.test.ts` passes the lean close-out case.

---

## Phase 4: Polish (parallelizable)

- [T011] [P] Polish: sync the `/apm implement` prompt and scheduler docs with the lean path (OBSERVE/REPORT wording reflects state-driven routing; no pack ceremony language for lean tasks).
  - Files: `pi-ext/core/prompts/apm-implement.md`
  - Trace: Plan API/surface; implement v2 handoff wording.
  - Acceptance: `cd pi-ext && pnpm test 2>&1 | grep -E "pass|fail" | tail -2` shows the apm-command tests passing with the updated wording.

---

## Phase 5: Final Verification (sequential)

- [T012] Full Go suite, vet, and build clean: `cd go-pic && go test ./... && go vet ./... && go build ./...`.

- [T013] Full pi-ext suite, lint, and typecheck clean (known runner flake excluded): `cd pi-ext && pnpm test 2>&1 | tail -5 && pnpm lint && pnpm run check`.

## Execution Order

```text
Phase 1: T001 ║ T002
           ↓
Phase 2: T003 → T004
           ↓
Phase 3 P1: T005
           ↓
Phase 3 P2: T006 → T007 → T008
           ↓
Phase 3 P3: T009 → T010
           ↓
Phase 4: T011
           ↓
Phase 5: T012 → T013
```

**Legend:** `→` sequential | `║` parallel

## Nyquist Mapping

| Req | Verifying task(s) | Command |
|-----|-------------------|---------|
| R01 | T002, T003, T005 | `cd go-pic && go test ./cmd/pic -run 'TestLeanClaimClaim\|TestLeanClaimEndToEnd'` |
| R02 | T004, T005 | `cd go-pic && go test ./cmd/pic -run 'TestLeanClaimSingleWriter\|TestLeanClaimEndToEnd'` |
| R03 | T001, T003, T005 | `cd go-pic && go test ./cmd/pic -run 'TestLeanClaimRouting\|TestLeanClaimEndToEnd'` |
| R04 | T006 | `cd pi-ext && node --experimental-strip-types --test pipeline/pipeline-scheduler.test.ts` |
| R05 | T007, T008, T010 | `cd go-pic && go test ./cmd/pic -run 'TestLeanClaimCompletion\|TestLeanClaimReviewVerification'` |
| R06 | T009 | `cd go-pic && go test ./cmd/pic -run TestLegacyReadBackIntegrity` |

## Nyquist Result

6/6 requirements covered, 100% at all tiers, PASS, no verification debt.
