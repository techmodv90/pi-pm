# Tasks: APM Implement Handoff

## Status
Approved

## Feature
ApmImplementHandoff — /apm implement becomes import + scheduler handoff

## Scenario Map

| US | Priority | Scenario |
|----|----------|----------|
| US1 | P1 | Gate unchanged |
| US2 | P1 | Untracked feature imports then hands off |
| US3 | P1 | Already-imported feature resumes |
| US4 | P1 | Authorization relayed, never self-granted |
| US5 | P1 | Implement never executes inline |
| US6 | P2 | Exit after authorization, report on demand |
| US7 | P3 | Breakdown handoff mentions new flow |

## Phase 1: Setup

- [ ] T001 [P1] [US1] Verify baseline: pi-ext suite green and apm-command.test.ts pins survive.
  Files: `pi-ext/core/apm-command.test.ts`
  Acceptance: `cd pi-ext && pnpm test` passes before any edit.

## Phase 2: Implement prompt rewrite

- [ ] T002 [P1] [US1,US2,US3,US4] RED: write a failing content check asserting apm-implement.md contains IMPORT/AUTHORIZATION/HANDOFF step names and no longer contains the BOUNDARY stop rule.
  Files: `pi-ext/core/apm-command.test.ts`
  Acceptance: `cd pi-ext && node --experimental-strip-types --test core/apm-command.test.ts` fails with missing-step assertions.
- [ ] T003 [P1] [US1,US2,US3,US4] GREEN: rewrite apm-implement.md Process to GATE → IMPORT (dry-run then real import, re-import hard stop becomes RESUME status report) → AUTHORIZATION (owner relay) → HANDOFF (exit with summary); remove BOUNDARY, phase-execution, TDD-cycle, stub-detection, and implementation stop-loss sections; keep Input contract `- tasks: {INPUT}` and checkpoint taxonomy scoped to the handoff.
  Files: `pi-ext/core/prompts/apm-implement.md`
  Acceptance: `cd pi-ext && node --experimental-strip-types --test core/apm-command.test.ts` passes, including new assertions.
- [ ] T004 [P1] [US5,US6] Add content assertions: no inline-execution instructions remain (no "RED:" TDD cycle, no "Stub Detection") and HANDOFF explicitly says exit without polling.
  Files: `pi-ext/core/apm-command.test.ts`
  Acceptance: `cd pi-ext && node --experimental-strip-types --test core/apm-command.test.ts` passes with the negative assertions.

## Phase 3: Breakdown handoff

- [ ] T005 [P3] [US7] Update apm-breakdown.md handoff section: implement = import + owner authorization + scheduler execution; discovery boundary text stays.
  Files: `pi-ext/core/prompts/apm-breakdown.md`
  Acceptance: `rg -n "import-apm|scheduler" pi-ext/core/prompts/apm-breakdown.md` matches the updated handoff text.

## Phase 4: Polish

- [ ] T006 [P2] [US6] Full verification: pi-ext suite, lint, typecheck.
  Files: `pi-ext/core/prompts/apm-implement.md`, `pi-ext/core/prompts/apm-breakdown.md`
  Acceptance: `cd pi-ext && pnpm test && pnpm lint && pnpm run check` passes.

## Execution Order

```
T001
T002 → T003 → T004
T004 → T005
T005 → T006
```

## Nyquist Mapping

| Requirement | Source | Verifying task | Verification command | Status |
|-------------|--------|----------------|----------------------|--------|
| R01 Gate unchanged | .feature US1 | T001/T002/T003/T006 | `cd pi-ext && pnpm test` | Covered |
| R02 Import then handoff | .feature US2 | T002/T003/T006 | `rg -n "IMPORT" pi-ext/core/prompts/apm-implement.md` | Covered |
| R03 Resume on already-imported | .feature US3 | T002/T003/T006 | `rg -n "RESUME" pi-ext/core/prompts/apm-implement.md` | Covered |
| R04 Authorization relay only | .feature US4 | T002/T003/T006 | `rg -n "authorize_work_item_implementation" pi-ext/core/prompts/apm-implement.md` | Covered |
| R05 No inline execution | .feature US5 | T004/T006 | `rg -n "Stub Detection" pi-ext/core/prompts/apm-implement.md` returns nothing | Covered |
| R06 Exit after authorization | .feature US6 | T004/T006 | `rg -n "HANDOFF" pi-ext/core/prompts/apm-implement.md` | Covered |
| R07 Breakdown handoff updated | .feature US7 | T005/T006 | `rg -n "import-apm" pi-ext/core/prompts/apm-breakdown.md` | Covered |

## Nyquist Result

Total requirements: 7
Covered: 7
P1 coverage: 100%
P2 coverage: 100%
P3+ coverage: 100%
Result: PASS — no verification debt registered.
