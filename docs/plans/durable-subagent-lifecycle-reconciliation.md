# Plan: Durable Subagent Lifecycle Reconciliation

## Goal
Make managed subagent state accurate after hangs, orphaned exits, parent-session loss, and stale tool activity, using durable process identity and explicit activity/heartbeat state.

## Architecture
The Go pipeline and schema-v3 Effective Contract Snapshot remain authoritative for task claims and artifact acceptance. The TypeScript runtime will persist child-process lifecycle metadata, reconcile liveness independently of the last tool event, and expose terminal/activity state to the Agents widget. Bash commands keep the existing 10-minute bound; this change adds process identity, heartbeat freshness, process-group termination, and graceful worker deadlines without changing retry policy.

## Task 1: Persist process identity and structured activity

**Files**:
- Modify: `pi-ext/subagent/tracker.ts`
- Modify: `pi-ext/subagent/runner.ts`
- Test: `pi-ext/subagent/tracker.test.ts`
- Test: `pi-ext/subagent/runner.test.ts`

**Steps**:
1. Add persisted `pid`, `hostname`, `processStartIdentity`, `activityState`, and `activityAt` fields to `AgentRun`.
2. Capture the child PID and process-start identity after spawn; update tracker state on tool start, tool result, message, stderr, and completion.
3. Add a portable liveness helper that distinguishes alive, dead, and unverifiable processes; use start identity when available to prevent PID reuse false positives.
4. Add failing tests for persisted identity, activity updates, and PID reuse/dead-process reconciliation.
5. Run `cd pi-ext && node --experimental-strip-types --test subagent/tracker.test.ts subagent/runner.test.ts`.

**Expected output**: all targeted tests pass and persisted `state.json` includes process identity and activity fields.

## Task 2: Reconcile stale and stalled runs

**Files**:
- Modify: `pi-ext/subagent/tracker.ts`
- Modify: `pi-ext/pipeline/pipeline-scheduler.ts`
- Test: `pi-ext/subagent/tracker.test.ts`
- Test: `pi-ext/pipeline/pipeline-scheduler.test.ts`

**Steps**:
1. Make tracker sync clear activity and transition running runs whose process is demonstrably gone to `failed` with an explicit orphan reason.
2. Add heartbeat freshness checks that classify a live but silent process as `stalled` in diagnostics without falsely marking it terminal.
3. Make scheduler startup and reconciliation persist terminal recovery before UI notification, and synchronize tracker state for recovered subagent runs.
4. Add tests for dead PID, reused PID, stale heartbeat, and terminal-state activity clearing.
5. Run `cd pi-ext && node --experimental-strip-types --test subagent/*.test.ts pipeline/pipeline-scheduler.test.ts`.

**Expected output**: orphaned runs disappear from the active widget, stalled live runs remain inspectable, and terminal recovery is durable and non-retrying.

## Task 3: Add worker deadline and process-group stop

**Files**:
- Modify: `pi-ext/subagent/runner.ts`
- Modify: `pi-ext/pipeline/pipeline-scheduler.ts`
- Test: `pi-ext/subagent/runner.test.ts`
- Test: `pi-ext/pipeline/pipeline-scheduler.test.ts`

**Steps**:
1. Add a 30-minute mutation-worker wall-clock deadline; keep the 10-minute per-Bash timeout unchanged.
2. Terminate the worker process group at the deadline, with a bounded 5-second SIGKILL fallback.
3. Persist `timed_out`/`aborted` terminal state and final activity before resolving the runner result.
4. Add deterministic coverage for the deadline constants and process-group termination path.
5. Run targeted runner and scheduler tests.

**Expected output**: a mutation worker cannot remain alive indefinitely and exits with durable timeout evidence; read-only agents remain governed by their stage lifecycle.

## Task 4: Project lifecycle state in the Agents UI

**Files**:
- Modify: `pi-ext/subagent/tracker.ts`
- Modify: `pi-ext/subagent/ui.ts`
- Test: `pi-ext/subagent/tracker.test.ts`
- Test: `pi-ext/subagent/ui.test.ts`

**Steps**:
1. Render explicit terminal states and current `activityState`; do not use the last historical tool event as current activity for non-running runs.
2. Show stalled/deadline/orphan reasons in the overlay details and retain the final event history for inspection.
3. Add UI regressions proving `using bash` disappears after orphan/timeout reconciliation while the historical Bash event remains visible in the activity tab.
4. Run the UI and tracker tests.

**Expected output**: the Agents panel always shows current lifecycle state, never stale tool activity.

## Task 5: Full verification and runtime rebuild

**Files**:
- No source changes expected beyond the files above.
- Generated: `go-pic/web/build`, `go-pic/dist/pic` via normal build only.

**Steps**:
1. Run `cd pi-ext && npm run check`.
2. Run `cd pi-ext && npm test`.
3. Run `cd go-pic && go test ./... -count=1`.
4. Run `cd pi-ext && npm run build`.
5. Run `git diff --check` on changed source and plan files.

**Expected output**: TypeScript checks, extension tests, Go tests, and build pass; existing Svelte warnings may remain unchanged.
