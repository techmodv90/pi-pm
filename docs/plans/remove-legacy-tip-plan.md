# Remove Legacy TIP Plan

Spec sufficient: permanently remove the old TIP data model and active compatibility surface. Do not create `task_instruction_packs` in this change; that follows in a separate plan.

1. `go-pic/cmd/pic/pic_cli_test.go`, `legacy_repair_parity_test.go` — add migration and CLI regression coverage proving existing TIP tables/foreign-key columns are removed, non-TIP report data survives, and legacy TIP commands are rejected. Test: `cd go-pic && go test ./cmd/pic -run 'TestLegacyTIPRemoval|TestLegacyRepairAndCoreParity' -count=1`.
   Acceptance: tests fail before implementation because TIP tables, columns, commands, and payload fields still exist.
   Parallel with: none.

2. `go-pic/cmd/pic/main.go`, `rebuild.go` — remove TIP tables from the canonical schema and rebuild existing databases without `completion_reports.tip_id`, `verification_items.tip_id`, or `escalations.tip_id`; explicitly drop legacy TIP tables after preserving unrelated rows. Test: `cd go-pic && go test ./cmd/pic -run 'TestLegacyTIPRemoval|TestPipelineSchemaMigratesLegacyColumns' -count=1`.
   Acceptance: a disposable legacy database reopens with all non-TIP reports intact and no TIP table or TIP foreign-key column.
   Blocked by: 1.

3. `go-pic/cmd/pic/workflow.go`, `core.go`, `misc.go` — delete TIP CLI commands, TIP lifecycle updates/events, TIP report linkage, TIP verification-item parsing, and TIP output from `show`; keep completion, escalation, and verification behavior task/requirement-based. Test: `cd go-pic && go test ./cmd/pic -run 'TestLegacyTIPRemoval|TestWorkflowLifecycle|TestPassedVerification' -count=1`.
   Acceptance: legacy TIP commands return unknown-command errors and normal completion/verification/escalation flows persist without TIP fields.
   Blocked by: 2.

4. `go-pic/cmd/pic/phase_repair.go`, `legacy_repair_parity_test.go`, `pic_cli_test.go` — remove automatic parent/child TIP creation, TIP requirement links, TIP dependency repair, and TIP assertions while preserving requirement, design, Task, dependency, and phase repair. Test: `cd go-pic && go test ./cmd/pic -run 'TestLegacyRepairAndCoreParity|TestRepair' -count=1`.
   Acceptance: repair creates and updates phase Tasks and Task dependencies without creating TIP records or returning TIP payloads.
   Blocked by: 2, 3.

5. `pi-ext/tool.ts`, `task-prompts.ts`, and affected `pi-ext/*.test.ts` — remove legacy TIP parameters and forwarding from the task-manager schema and completion/escalation/report contracts; retain Task and requirement traceability only. Test: `cd pi-ext && npm test && npm run check`.
   Acceptance: the extension exposes no `tip_id`, `tip_key`, or legacy TIP markdown parameter and all active prompt/tool tests pass.
   Parallel with: 4 after 2.

6. `docs/walkthrough-gap-analysis.md`, other active `docs/*.md`, `/Users/justin/.pi/AGENTS.md`, `/Users/justin/.pi/agent/AGENTS.md` — remove claims that the legacy TIP graph or commands remain active; state that the new `task_instruction_packs` design is deferred to its own plan. Test: `rg -n -i '\bTIP\b|tip_id|tip_key|tip-add|tip-status|tip-dependency|tip-link-requirement' go-pic/cmd/pic pi-ext docs --glob '!go-pic/dist/**' --glob '!go-pic/web/build/**'`.
   Acceptance: remaining matches occur only in the removal plan or explicit negative regression tests; governing instructions no longer describe TIP as compatibility data.
   Blocked by: 3, 4, 5.

7. Generated runtime artifacts and full repository verification — rebuild the dashboard and Go binary, then run all tests and static checks. Test: `cd pi-ext && npm test && npm run check && npm run build && cd ../go-pic && go test ./... -count=1 && cd .. && git diff --check`.
   Acceptance: TypeScript tests/typecheck/build and all Go tests pass, `go-pic/web/build` and `go-pic/dist/pic` are present, and `git diff --check` reports no whitespace errors.
   Blocked by: 2–6.

## Risks

- Task 2 permanently discards legacy TIP rows and TIP foreign-key values. Rollback: NOT POSSIBLE for discarded TIP data; restore the database from an external backup if historical TIP records are needed.
- Rebuilding tables can lose unrelated report data if column mapping is incomplete. Rollback: stop on the disposable-database regression failure and do not run the new binary against another database.
- Removing TIP fields changes the Go CLI and extension tool contract. Rollback: restore the prior binary/extension build together; mixed old/new clients are unsupported.
- The new `task_instruction_packs` artifact is intentionally absent after this removal. Rollback: none required; continue using current Task handoffs until the separate Epic/TIP plan is implemented.
