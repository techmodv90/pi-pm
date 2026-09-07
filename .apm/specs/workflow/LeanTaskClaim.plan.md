# Plan: Lean Task Claim

## Status
Approved

## Summary
Claim routing becomes state-driven. A Work Item with no instruction pack and
no authorized materialization takes a lean claim path: status flip + claim
metadata + activity-log event, description verbatim as worker input, pack-free
completion/review/verification. Items carrying legacy pipeline state route to
the unchanged legacy claim. No labels, no new tables, no schema change; legacy
machinery stays frozen for read-back of historical items.

## Invariants check
- Canonical workflow: reconciled with the owner decision (2026-09-07) — the
  pack/provenance chain is not required for imported/new work; lean replaces
  it by default. Legacy path preserved read-only for historical items.
- Single-writer exclusion is retained as the only hard claim invariant.
- No `RAISE()` in ad hoc SQL; affected-row counts asserted in Go.
- Additive-only: no ALTER TABLE needed at all (lean uses existing columns
  status/claimed_at/claimed_by and work_item_events).

## Architecture

### Routing decision point
`workflowPipelineClaim` (go-pic/cmd/pic/pipeline.go) — at claim entry, before
the worker/review/autofix pack gates:

```
hasLegacyState = EXISTS active pack
              OR EXISTS authorized materialization
hasLegacyState → existing legacy gates (unchanged, byte-for-byte)
otherwise      → leanClaim()
```

### Lean claim (US1, US2)
- Guard: reject if an active claim exists (status='in_progress' with unexpired
  claim, or an active pipeline run row) — clear error, no writes.
- Write: status 'in_progress', claimed_at/claimed_by set, one
  work_item_events row (event_type 'claimed', actor recorded).
- Read paths (`pic show`, web detail) already surface these columns — no new
  surface.

### Lean worker input (US4)
- Worker input assembly (pi-ext scheduler, `pipeline-scheduler.ts`): for
  lean-path tasks the input is the stored description verbatim (which already
  embeds Acceptance criteria and Behavior context). No
  `pic workflow instruction-pack` render, no pack columns in the run record.

### Lean completion / review / verification (US5, US6)
- Completion: status 'done' + completion event; completion report rows are
  NOT created for lean tasks (their pack columns are NOT NULL by design).
- Review verdict and verification records reference task + run identifiers
  only. No pack-hash cycle caps; the event log is the retry evidence.
- The scheduler's close-out sequence for lean tasks: worker → reviewer →
  contractor verification report → close, with pack-keyed joins skipped.

### Legacy read-back (US7)
- Zero writes to legacy rows. Regression tests prove existing joins
  (completion ↔ pack ↔ verification) still resolve for historical done items
  after the change.

## API / surface
- `pic workflow pipeline-claim <task> <stage>` — gains the routing branch;
  new lean branch returns the claimed work item JSON (same envelope shape as
  legacy claim output where fields overlap).
- `task_manager work_on_work_item` — unchanged call surface; scheduler uses
  lean input assembly for state-routed-lean tasks.
- No new CLI commands.

## Flow
1. Claim attempt → routing check (R03)
2. Lean: single-writer guard (R02) → claim write (R01)
3. Worker executes against verbatim description (R04)
4. Completion/review/verification pack-free (R05)
5. Historical items unchanged (R06)

## Complexity / risks
- Legacy claim output envelope parity: lean claim returns the fields the
  scheduler actually reads; anything else omitted deliberately.
- The 3 materialized/no-pack terminal items route legacy-safe (they have
  authorizations but are deprecated features; their open children have no
  legacy state and route lean).
- Review-fix circuit breakers are legacy-scoped; lean retries are owner-
  observed via the event log (accepted trade-off, 2026-09-07).

## Milestone
- Labels: none new. Verification commands live in the tasks file Phase 5.
