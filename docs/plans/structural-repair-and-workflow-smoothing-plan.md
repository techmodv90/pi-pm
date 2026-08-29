# Plan: Structural Repair And Workflow Smoothing

## Implementation Status (2026-08-29)

Landed on `feature/two-phase-rri-t`, every commit verified with the full Go suite, extension suite, typecheck, and scoped lint:

| Task | Status | Commits |
|---|---|---|
| Task 1 — hygiene + RRI-T landing + dead parsers | Done | `74316e5`, `5f9a543`, `db53ce0`, `f1b2c6c`, `292f261` |
| Task 2 — scheduler split into module seams | Done | `a2dab6e` |
| Task 3 — typed pic show boundary + scoped lint | Done | `9ade0fa` |
| Task 6 — next-action oracle + checkpoint-decide | Done | `fdd94b9` |
| Task 8 — stage primer + attempt ledger | Done | `9c13d84` |
| Task 9 — TIP contract interfaces + close-out + verify prerequisites | Done | `1dcffa0` |
| Task 7 — dry-run preview + bounded scan-fanout repair | Done | `1a6a20a`, `6ab56a7` |
| Task 5 — versioned schema migrations + explicit classification | Done | `d9a9f2b` |
| Task 4 — Go internal packages: `internal/tip` extracted | First slice done | `73c05ac` |

Task 5 notes: `isLegacyBootstrapStatement` is deleted; every bootstrap statement is classified exactly once into `canonicalSchemaStatements` / `legacySchemaStatements` (mechanically derived from the old matcher, with one deliberate fix: `trg_work_item_pack_immutable` now guards canonical packs on fresh databases too). Migrations are ordered versioned steps recorded in `schema_migrations`; `initDB` remains a reconciler by design — steps re-run per open (guarded, idempotent) unless they opt into `once: true`, because the CLI must keep repairing degraded databases. The connection-scoped `PRAGMA foreign_keys = ON` moved out of the replayed list into `initDB` itself.

Task 4 remaining slices (each one domain at a time, CLI JSON byte-identical, full suite green per move): `internal/workitem` (CRUD/labels/relations/claim/readiness), `internal/workflow` (artifact stage machine, checkpoints, materialize, aggregate verify), `internal/pipeline` (runs, leases, checkpoints), `internal/acceptance` (Gherkin validation), `internal/store` (openDB + the migration runner from `schema_bootstrap.go`). The `internal/tip` extraction shows the pattern: move pure + transactional logic verbatim with a rename map, leave thin CLI handlers in `cmd/pic`, inject cross-domain validators (e.g. `SaveInput.ValidateAcceptance`).

## Review Response (2026-08-29, second pass)

All P0/P1 findings and both actionable P2s from the post-implementation review are fixed, each with regression coverage:

| Finding | Resolution | Commit |
|---|---|---|
| P0 migrations not atomic | Every step is one-shot (`once` semantics); statement/DML steps apply AND record their version inside one transaction (`tx: true`), so a crash leaves the step fully applied and recorded or not at all. Table-rebuild steps that manage their own transactions/pragmas stay self-managed, individually atomic, and re-run in full on retry. | `ecaf99f` |
| P3 current DBs mutate on every open | Resolved together with P0: strict once-semantics — an already-migrated database runs zero migration steps. The self-healing tests now simulate older-binary databases explicitly by clearing version records, which is what those fixtures meant all along. | `ecaf99f` |
| P1 partial legacy states | `migrateEpicWorkflowSchema` handles epics-only databases (previously early-returned, orphaning rows) and `migrateLegacyWorkItems` imports from whichever table exists; a task whose epic was not imported gets a NULL parent. A pre-existing latent bug surfaced here — dangling `tasks.epic_id` broke the rebuilt FK check even with both tables — now normalized to NULL during migration. | `65cc81d` |
| P1 parsePicShow element shapes | Every collection element must be a non-null object or parsing throws `pic show <key>[<index>] must be an object`. | `9af59c9` |
| P1 prose oracle | `NextAction` records with stable `id`, `kind` (tool/cli), `action`, argument template, `label`, and role `actor`; `workflow-status.next_actions` returns structured records and gate rejections render action+args+actor from the same records. | `d4fdeb3` |
| P1 primer drops missing context | `planPrimerContext` fails closed: any preceding profile stage without an approved checkpoint bound to a present artifact blocks dispatch with the named stages. | `9af59c9` |
| P1 dry-run counts only | The preview returns the exact artifact revisions, checkpoint rows, TIP generations, and dependent Work Items a reset would retire, queried from the same lineage the DELETE statements use. | `b254a86` |
| P2 inferred attempt number | Attempt numbering reads the persisted `attempt` counter on the pack's pipeline runs (max+1), robust across resets and review-fix epochs; escalation resolutions flow through the ledger's context argument on the canonical dispatch path. | `b694421`, `476e0a2` |
| P2 lint "new code only" | `no-explicit-any` is now an error in pipeline/tasking; every pre-existing occurrence carries an explicit per-line `-- legacy baseline` disable marker, so any new unmarked `any` fails lint (verified with a negative probe). | `f4870be` |
| P2 package extraction incomplete | Acknowledged as ongoing: `internal/tip` is the established pattern; the remaining packages (workitem, workflow, pipeline, acceptance, store) follow it one domain per commit. | — |

## Review Response (2026-08-29, third pass)

| Finding | Resolution | Commit |
|---|---|---|
| P0 steps 1/3 still not atomic | Every step is now transactional without exception. `rebuildSchemaTable`, the legacy schema reconciliation, artifact-stage widening, and the pipeline-column reconciliation all run on the caller's transaction; the runner owns `foreign_keys=OFF` + `legacy_alter_table=ON` at connection level around BEGIN (both are connection-scoped and cannot change inside a transaction) and restores them after commit. Step operations and the version record commit or roll back together. Proven by `TestSchemaMigrationFailureInjectionRollsBack`: a step running real reconcile operations then failing leaves no version record, no `work_items` table (created DDL rolled back), and untouched legacy rows; a DDL step failing after CREATE leaves no table; the subsequent real migration retries cleanly. | `30d581e` |
| P1 primer accepts unapproved/hash-mismatched checkpoints | `planPrimerContext` and `predecessorCheckpointFor` now consider only `decision_type` approved/accepted checkpoints whose `content_hash` matches the bound artifact revision, selecting the newest such revision; rejected, hash-stale, artifact-orphaned, and untyped-decision checkpoints count as missing and block dispatch. Regression cases cover rejected checkpoints, hash mismatch, and an older approved revision surviving a rejected newer one. | `d26ba70`, `8a84124`, `7540d9e` |
| P1 dry-run misses cascade-retired descendants | The preview enumerates the descendant-owned records the reset's foreign-key cascades retire: child artifacts, checkpoints, instruction packs, completion reports, and verification reports, each keyed by owning Work Item. The regression seeds descendant-owned rows and asserts they appear in the preview while remaining unmutated. | `3f3fca4` |
| P2 Task 4 remaining packages | Ongoing, documented: `internal/tip` is the pattern; workitem/workflow/pipeline/acceptance/store follow one domain per commit. | — |

## Review Response (2026-08-29, fourth pass)

| Finding | Resolution | Commit |
|---|---|---|
| P0 migration pragmas not guaranteed on the transaction's connection | `applySchemaMigration` pins a single `*sql.Conn` (`db.Conn(ctx)`) and runs the pragma capture, `foreign_keys=OFF` + `legacy_alter_table=ON`, `BeginTx`, the step, the version record, and the pragma restore on that one connection — nothing is pool-routed between pragma and transaction. The materializations rebuild drops its inline pragma toggling (a no-op inside a transaction and redundant under the runner-owned pragma). `TestSchemaMigrationPragmasRunOnThePinnedConnection` probes the transaction's own connection: `openSQLite`'s DSN enables `foreign_keys` on every fresh connection and the test pools no idle connections, so pool-routed pragmas read `foreign_keys=1` inside the step (test verified red against the old implementation) while the pinned runner reads `foreign_keys=0` / `legacy_alter_table=1` (green). | `356dbed` |
| P1 predecessorCheckpointFor bypasses artifact/hash validation | `predecessorCheckpointFor` resolves the predecessor through the same predicate as `planPrimerContext` via a shared `latestValidatedCheckpoint` helper: approved/accepted `decision_type`, bound artifact revision present, `content_hash` match, newest revision wins — so the primer's lineage line can never name an orphaned, hash-stale, or rejected checkpoint even though `approved_context` already failed closed. Regression cases cover orphaned, hash-stale, and rejected predecessors; the happy-path fixtures now carry the artifacts they claim. | `0d3627d` |
| P1 dry-run omits descendant pipeline runs (and other cascade tables) | Decision recorded: "exact" means every cascade-retired record. The preview is now table-driven over every child-owned FK table — pipeline runs, labels, dependencies, gates, relations, authorizations, escalations, owner decisions, aggregate decisions, delivery states, events, profiles, and sub-root materializations — in addition to the five planning-lineage tables, each row naming its owning Work Item. Corrective bugs are enumerated as second-order targets (a row dies with its descendant verification report or with the bug item itself; the bug Work Item survives unless it is itself a descendant, and the preview says so). The regression seeds every table, asserts each appears in the preview without mutation, and verifies the real reset retires exactly the previewed rows. | `593f0b7` |
| P2 Task 4 remaining packages | Ongoing, documented: `internal/tip` is the pattern; workitem/workflow/pipeline/acceptance/store follow one domain per commit. | — |

## Original Plan (unchanged)


## Goal

Reduce structural debt and workflow friction without changing lifecycle authority,
approval semantics, or immutable planning history.

## Non-Negotiable Invariants

- Go CLI remains sole lifecycle mutation authority. The extension renders and
  dispatches structured actions; it does not reproduce transition rules.
- Planning artifacts are immutable, content-hashed revisions with explicit owner
  checkpoints. No batch command combines checkpoint meaning or implies approval.
- Existing databases are upgraded non-destructively. Historical IDs, artifacts,
  hashes, reports, and events remain recoverable.
- Every retry, repair, reset, and migration transition has durable lineage.
- Generated `go-pic/web/build` and `go-pic/dist/pic` are produced by the normal
  build and included in installed-runtime verification.

## Execution Rules

- Worktree must be clean before each independently integrated phase. Existing
  user changes are never committed, deleted, or folded into this plan.
- No task performs `git commit`, `git rm`, or deletion of untracked files unless
  the owner explicitly authorizes that exact operation.
- Each task is one reviewable change. Required evidence is recorded before the
  next task starts.
- Behavioral changes require red, green, and adversarial regression evidence.
  Refactors require public-contract and transaction-semantics evidence.
- Common verification gate:

  ```text
  cd go-pic && go test ./...
  cd ../pi-ext && pnpm test && pnpm run check && pnpm run build
  cd .. && git diff --check
  ```

- Runtime-sensitive tasks additionally run the rebuilt binary, verify resolver
  selection, and perform a disposable fresh-DB smoke test. A source test does
  not prove loaded-extension or installed-binary behavior.

## Phase 0: Hygiene And RRI-T Landing

**Files**: `.gitignore`, `projects.json` status, `projects.example.json`,
`go-pic/cmd/pic/main.go`, existing RRI-T files and tests.

1. Inspect tracked and untracked state. Request owner approval before removing
   tracked `projects.json`, deleting `.DS_Store`, or creating a template. Keep
   unrelated dirty changes untouched.
2. Remove only the unreachable `return` after `run`'s switch if it still exists.
3. Separate pending RRI-T changes from hygiene. Do not commit them in this task.
   Land only through the repository's approved Work Item integration path.
4. Remove legacy RRI parsers only after proving zero production callers and
   preserving any parser needed by an explicit compatibility test.
5. Run the common verification gate and record the exact changed-file scope.

## Phase 1: TypeScript Module Decomposition

**Target**: retain `pi-ext/pipeline/pipeline-scheduler.ts` as the stable public
module until all callers move; use `pipeline/scheduler.ts` only if the rename is
intentional and all production/test imports change in one reviewed step.

1. Define dependency direction before moving code:
   `api -> pipeline/scheduler -> pure pipeline modules`; pure modules may depend
   on shared types and standard libraries, never on API registration.
2. Move pure functions in separate changes: `rri-t.ts`, `report-parsing.ts`,
   `integration.ts`, `worker-validation.ts`, `corrections.ts`, and
   `stage-prompts.ts`. Do not add a temporary barrel unless an existing external
   consumer requires it; remove it in the same change that updates all imports.
3. Keep `PipelineScheduler` and registration in one documented public module,
   with an explicit export-surface test covering `api/tool.ts` and
   `api/commands.ts`.
4. Prove observable behavior, exported symbols, persisted calls, retry lineage,
   and integration decisions remain unchanged. Do not use byte-identical source
   output as the acceptance criterion.
5. Run the common verification gate after each move.

## Phase 2: Typed Data Boundaries

**Files**: new `pi-ext/pipeline/pic-show.ts`, existing XML/JSON parsers,
pipeline/tasking modules, `package.json`, lockfile, TypeScript config, lint config.

1. Define `PicShowDocument` from the actual Go JSON contract, including required
   fields and optional collections. `parsePicShow` rejects wrong types, missing
   required fields, malformed collection entries, and unknown authority shapes
   with field paths in errors.
2. Use the already-installed TypeBox version pinned by the lockfile. Add no new
   dependency until its version and package API are confirmed. Add typed parses
   for RRI-T persona, instruction-pack, and scan-report payloads without
   changing accepted canonical schemas.
3. Replace `any` at boundaries first, then narrow internal types. Existing
   exceptions must be explicit and scoped; “new code only” means a diff-scoped
   lint command or a documented allowlist, not an unenforceable convention.
4. If ESLint is introduced, add exact config, scripts, pinned dependencies, and
   a CI/local command. Do not edit global `AGENTS.md`; document repository-local
   policy in the relevant project file.
5. Test missing fields, wrong types, malformed XML/JSON, and valid legacy fields.
   Run the common verification gate.

## Phase 3: Go Package Extraction

**Dependency direction**:

```text
cmd/pic -> acceptance, pipeline, workflow, workitem, tip, store
acceptance/pipeline/workflow/workitem/tip -> store
store -> Go standard library and SQLite driver only
```

1. Record current package-private functions, transaction boundaries, SQL error
   behavior, and JSON output as extraction contracts.
2. Extract one package per reviewed change. `internal/tip` is not assumed to be
   self-contained: prove its dependency graph first and inject required store
   operations rather than importing `cmd/pic`.
3. Keep CLI tests as external-contract tests. Add package tests for transaction
   commit/rollback, authorization checks, migration behavior, and error mapping.
4. Run `go test ./...`, `go vet ./...` where currently supported, and diff checks
   after every extraction. Stop on any JSON, SQL, or transaction-semantic drift.

## Phase 4: Versioned Schema Migrations

### Database states

The migration runner must classify and test these states before applying DDL:

1. **Fresh**: no application tables and no `schema_migrations`; create current
   canonical baseline and record its version in one transaction.
2. **Legacy**: pre-cutover tables/constraints exist and no migration ledger;
   inspect schema, record the baseline as already represented without replaying
   baseline DDL, then apply explicit cutover migrations.
3. **Partially migrated**: ledger exists; apply only missing ordered versions.
4. **Current**: all versions exist; perform no DDL or data mutation.
5. **Inconsistent**: ledger claims a version whose required schema is absent, or
   duplicate/invalid rows exist; fail closed with repair instructions and no
   partial advancement.

**Files**: `go-pic/internal/store`, migration fixtures, CLI tests, and only then
`cmd/pic` bootstrap wiring.

1. Add `schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL,
   applied_at TEXT NOT NULL)` and a transactionally applied ordered migration
   registry. Define commit/restart behavior and prove a failed migration can be
   retried without duplicate imports.
2. Build fixtures for fresh, legacy, partially migrated, current, and inconsistent
   databases. Assert preserved IDs, artifacts, hashes, reports, requirements,
   and historical events. Run each migration twice.
3. Use `m001_baseline` only for fresh databases. Do not replay it against an
   existing schema. Use explicit `m002_legacy_epic_task_cutover` and
   `m003_artifact_stage_widening`, followed by additive migrations.
4. Remove prefix filtering only after all current fresh and legacy paths are
   covered by migration tests. Existing legacy tables are migration input only;
   production reads/writes remain canonical.

### Requirement invalidation

Do not remove `trg_requirement_content_stales_packs` until every supported
requirement mutation route is enumerated: RRI finalization, planning amendment,
corrective verification, satisfaction updates, deletion, and any generic update
helper. If SQLite writes outside Go are supported, retain a narrow DB invariant
guard. If not supported, document and enforce SQLite internal-only access.

The replacement must be one shared Go transaction helper used by every public
mutation path, covering stale packs, review reset, and superseded verification
reports. Add public CLI tests for each route, rollback tests, concurrent-update
tests where supported, and restart/recovery tests before removing the trigger.

## Phase 5: Structured Next Actions And Owner Decisions

1. Define a Go-owned structured oracle response:

   ```json
   {"id":"approve_rri","action":"task_manager.approve_work_item_artifact",
    "arguments":{"stage":"rri"},"label":"Approve RRI artifact",
    "blockers":[]}
   ```

   Stable action IDs and arguments are authoritative. Human error text is
   rendered from the same structure; the extension never parses prose.
2. Add `next_actions[]` to `workflow-status` for every valid stage/profile and
   test that invalid transitions cite the same action ID. Keep actions advisory;
   Go transition validation remains authoritative.
3. Add batched decision entry only with full preflight validation, deterministic
   order, idempotency keys, and an explicit partial-success response. Each
   checkpoint still commits its own immutable revision transaction. Test failure
   after decision N, retry, duplicate decisions, and mixed valid/invalid input.
4. Extension renders structured pending decisions in one panel. Dashboard remains
   informational. Run the common verification gate.

## Phase 6: Durable Artifact Repair And Invalidation Preview

1. Persist stage retry count, parser error, source artifact revision/hash, and
   attempt/run identity in existing pipeline lineage or a reviewed additive
   schema. A prompt-only “at most once” rule is insufficient.
2. Generalize validate -> fail closed -> one retry with named error -> owner
   escalation for scan, vision, blueprint, contracts, and task graph. Test
   restart between failure and retry, duplicate scheduler starts, and stale
   artifact output.
3. Add `--dry-run` to planning amend/reset. Return exact downstream artifact
   revisions, approvals, task-graph checkpoints, TIP generations, and dependents
   that would invalidate. Dry-run performs no writes and uses the same dependency
   calculation as the real mutation.
4. Surface active planning profile, version/hash, and stage list in
   `workflow-status`; profile selection does not bypass required gates.

## Phase 7: Stage Primers And Attempt Ledger

1. Define a total prompt budget. Each predecessor digest has a bounded size,
   deterministic truncation, artifact ID/revision/hash binding, and a
   `load_planning_artifact` fallback. Missing or superseded predecessors block
   dispatch rather than silently using stale context.
2. Build typed `buildStagePrimer` data with lineage, profile version/hash, scan
   conventions, stage definition of done, and explicit prohibitions.
3. Render a deterministic Work Progress Ledger from persisted attempts,
   completion/review/verification reports, trimmed failed-command output,
   claimed files, and escalation resolutions. Bind it to TIP ID/generation and
   say `This is attempt N of TIP-xxx — continue, do not re-plan from scratch`.
4. Test retry, review-fix, autofix, stale TIP, missing evidence, truncation, and
   scheduler restart. Run `pnpm test`, `pnpm run check`, and production build.

## Phase 8: TIP Contracts And Close-Out

1. Extend the approved task-graph schema with typed `provides`, `consumes`, and
   `obligation_keys`. Define validation, migration defaults, and fail-closed
   behavior for old graphs without these fields. Never infer contract authority
   from filenames or descriptions.
2. Render `## CONTRACT INTERFACES` from the approved graph revision and bind it
   to the TIP content hash. Test exact ownership and neighbor references.
3. On executable close, emit one durable transition message listing newly-ready
   dependents from the canonical readiness oracle and their current TIP state.
   Test no-ready-dependent, multiple dependents, duplicate close, and stale TIP.
4. Render verification `setup_commands` as prerequisites. Setup failure reports
   `environment_blocked`; worker does not modify infrastructure. Test successful
   setup and failure paths.
5. Append behavior to `docs/work-item-model-spec.md` only after acceptance tests
   pass.

## Sequencing And Exit Criteria

| Phase | Scope | Exit gate |
|---|---|---|
| 0 | Hygiene and RRI-T landing | Owner-approved worktree changes; full suites and diff check |
| 1 | TypeScript moves | Export, behavior, lineage, and full runtime checks |
| 2 | Typed boundaries | Fail-closed parser tests; pinned lint/check/test/build |
| 3 | Go packages | Dependency direction and transaction tests; `go test ./...` |
| 4 | Migrations | All five DB-state fixtures, idempotency, crash retry, legacy parity |
| 5 | Next actions/decisions | Structured oracle and partial-batch evidence |
| 6 | Repair/preview | Durable retry lineage and restart evidence |
| 7 | Primers/ledger | Budget, hash binding, stale-context, and restart evidence |
| 8 | TIP/close-out | Approved schema fields, readiness transition, verification prerequisites |

No phase is complete from aggregate suite results alone. Each invariant requires
its own red/green regression, adversarial public-boundary review, and fresh
runtime artifact evidence where applicable.