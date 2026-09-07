# Lean Task Claim — Requirements

Version: 1.0 (2026-09-07)

## R01 — Lean claim path (P1)

A Work Item with no instruction packs and no materializations is claimed by a
single state transition: status → in_progress, claimant + claim time recorded,
one claim event appended to the activity log. No pack, checkpoint, or
materialization is created. Verified by US1.

## R02 — Single-writer exclusion (P1)

While a claim is active (in_progress, claim held), any second claim attempt is
rejected with a clear error and records nothing. This is the only enforced
invariant of the lean path. Verified by US2.

## R03 — State-driven routing (P1)

Claim routing is decided by the item's own state: presence of an active pack
or authorized materialization routes to the unchanged legacy claim path;
absence routes lean. No labels, no flags. Verified by US3.

## R04 — Verbatim worker input (P2)

Lean worker input is the stored task description verbatim, including its
Acceptance criteria. No TIP rendering, pack expansion, or pack references.
Verified by US4.

## R05 — Lean completion and verification (P2)

Completion is a status flip plus event; review and verification records bind
to the task and run without pack columns; a failed review routes a fix attempt
without pack-hash cycle caps. Verified by US5, US6.

## R06 — Legacy read-back integrity (P3)

Historical items that completed under the legacy pipeline keep fully
resolvable detail/completion/verification joins. The lean change performs zero
writes to legacy rows. Verified by US7.
