# Five-Step Algorithm Plan — Lean Questioning, Reversible Deletion, and a Learning Loop

## Goal

Apply Musk's five-step algorithm — question every requirement, delete, simplify, accelerate,
automate, in strict order — to the task-system itself: its requirement intake (RRI), its own
pipeline ceremony, its task decomposition, and its meta-process for adding rules. The deliverable
that makes every other step self-correcting is a **learning loop**: deletions (and rules) treated
as falsifiable hypotheses with pre-registered escape signals, observation windows, and an
add-back ledger. No new approval gates, no new workflow states; every change rides existing
checkpoints, prompts, and persisted signals.

## Background

Two comparison threads (2026-09-02) motivated this plan:

- The decomposition policy v2 plan (`decomposition-policy-v2-plan.md`) already imported the
  strong ideas from `mattpocock/skills` `to-spec`/`to-tickets`: seams approved at Blueprint,
  vertical-by-default Task Graphs, minimal rationale-carrying edges.
- A review of Musk's five-step algorithm (with the Parkinson's Law framing: work expands to fill
  the container it is given) identified that our ladder already encodes steps 3–5 well, and is
  weakest at steps 1–2 applied to **requirements after RRI** and to **the pipeline's own
  ceremony** — a task-system's product is process, so it accretes stages, fields, validations,
  and AGENTS.md rules, and almost nothing is ever deleted.

Target shape:

```text
RRI:            three-question challenge set (provenance / justification / consequence)
                + prefer-defer over withdraw
Pipeline:       ceremony budgets per workflow mode; "feeds a decision or gate" entry test;
                evidence-driven removal experiments (never state bypass)
Task Graph:     estimate challenge at node-goal time; 2x overrun -> re-plan, not worker pressure
Learning loop:  deletions and rules as hypotheses -> pre-registered escape signal
                -> observation window -> verdict (keep-deleted | add-back | scope)
                -> add-back rate ~10% as calibration; silence (never-fired) triggers the ledger
Analytics:      per-project tasks.db stays SQLite (OLTP record); a separate Go binary
                (official duckdb-go) reads them in place over a manifest; central
                view is derived, versioned, and rebuildable — never a system of record
```

## What Already Holds (do not re-litigate)

- Step 3 (simplify): decomposition policy v2 — vertical tracer-bullet default, minimal blocking
  edges, no node-count targets, "stop before mechanical fragments."
- Step 4 (accelerate): `partially_parallel`/`parallel_allowed` execution policies, contract-first
  sibling lanes, the six node questions already include "can it fit one focused execution
  session?"
- Step 5 (automate): hash-verified lineage, materialization, seam-bound verification commands.
- Step 1 (partially): RRI interviews, `source_questions` into `rri_requirements_matrix`, the
  planner's spec sufficiency gate, Scan-before-RRI grounding requirements in code reality.

## Non-Negotiable Invariants

- No new approval gates or workflow states (same stance as the v2 plan). The question set rides
  the RRI interview and the Contractor's existing five checks; granularity and removal reviews
  ride the existing Task Graph checkpoint.
- Never remove or disable a workflow check by filtering, relabeling, or bypassing state
  (AGENTS.md Project Learnings). Every removal experiment rides an explicit workflow-mode or
  configuration scope, with the escape signal written down before the switch is flipped.
- `pic` stays cgo-free: DuckDB lives only in a separate analytics binary target. Nothing imports
  `duckdb-go` from the main `pic` build.
- Per-project `tasks.db` stays SQLite and remains the sole system of record. The central
  analytics view is derived and rebuildable from the project databases at any time.
- Analytics centralizes metrics and rule/check IDs — never artifact prose, titles, or project
  content.
- Guidance over schema: phase 1 adds no required artifact fields. A challenge question that
  needs a new mandatory field to work has failed its own deletion test at birth.

## The Proposal (phased, strict order)

### Phase 1 — Question (requirement intake)

Three-question challenge set applied to every requirement at RRI, and to owner-sourced
requirements equally ("even smart or powerful people's requirements get challenged"; the
requirement survives on its recorded justification, not its author):

1. **Provenance** — who asked? A person-level source: an owner answer, an ADR, or an external
   constraint. "Industry standard practice" / "we always do this" has no person and fails.
2. **Justification** — one line of *why*, captured at RRI (rides `source_questions`; no new
   field). Circular justifications are flagged, not argued.
3. **Observable consequence** — "if we shipped everything except this, who notices and what
   breaks?" Nobody notices ⇒ defer candidate. (This is the fiberglass-strip test: it kills
   requirements that *have* an author but no observable value.)

Touchpoints: RRI stage prompts; Contractor fifth-check guidance (flag circular / inherited /
Scan-contradicted justifications). The narrowing power of Scan is explicit: a requirement whose
value Scan shows already exists gets narrowed, not duplicated.

### Phase 2 — Delete (requirements and process, evidence-first)

- **Evidence report v0** (starts as manual SQL against per-project `tasks.db`; no tooling
  prerequisite):
  1. validation/check fire counts **with denominators** (fired / opportunities, by rule ×
     workflow mode × task-system version),
  2. "Not applicable: …" field rates per Task Graph field (persisted already),
  3. defer / withdraw / add-back history per requirement,
  4. Reviewer rejection reasons and verification outcomes, grouped by mode.
  Phase 2 also produces the **gap list**: signals the report needs that are not persisted today
  (those become small additive, re-run-guarded schema additions — the only schema work in this
  plan).
- **Prefer-defer rule**: contract operations guidance — `defer` unless the requirement was never
  real (invented, duplicated, untraceable ⇒ `withdraw`). Defer is a deletion with a cheap
  add-back path; the deferred backlog is the running deletion ledger.
- **Bounded removal experiments**: disable a never-firing check for one workflow mode only, for
  a fixed batch of work items, with the escape signal pre-registered ("a Reviewer rejects an
  artifact for a reason this check would have caught"). Verdict per the learning loop below —
  including *scope* verdicts (restore for `quick`, stay off for `standard`) and conversions of
  LLM-judgment checks into deterministic schema validation where possible.

### Phase 3 — Simplify (ceremony, only after evidence)

- **"Feeds a decision or gate" entry test**: any newly proposed stage, field, check, or
  AGENTS.md rule must name the decision it feeds and the rule it tightens or replaces. Extends
  the existing "tighten an existing rule instead of duplicating it" convention with a
  deletion-side counterpart.
- **Ceremony budgets per workflow mode**: bounded interview rounds, artifact field sets, and
  checkpoint checks for `quick`/`standard`/`designed`/`full`, set from the phase-2 evidence —
  the budget is what "smaller" gets measured against.
- **Handoff re-load audit**: examine where stages re-read full predecessor artifacts whose
  lineage the scheduler already hash-verified; combine where the second read feeds no decision.
  Audit first, change later; hash binding itself is never weakened.
- **Seam authoring preference (guidance only)**: prefer extending an existing seam with
  `prior_art` cited; a new seam needs a one-line justification. This does **not** amend v2
  Design Decision 3 (seam scope is per-contract, highest seam that isolates the requirement) —
  it is authoring guidance about reuse, not a minimization rule.

### Phase 4 — Accelerate (containers, not pressure)

- **Estimate-at-goal-time**: the "one focused session" challenge is asked when the node goal is
  written, before its fields are populated — the container is set before the work expands into
  it (Parkinson's Law).
- **2× overrun ⇒ re-plan trigger**: a node exceeding ~2× its estimate stops and re-plans (it
  was probably two outcomes wearing one key) instead of the worker grinding. Implemented as a
  stop condition + re-plan path, never as deadline pressure on workers — pressure induces
  exactly the completed-state fictions our Project Learnings forbid.
- **Time-to-first-verified-node** as the pipeline's measured dial (RRI approval → first passing
  node verification), derived entirely from persisted events.
- **Deferred policy option**: first vertical node dispatch after *its* obligations' contracts
  approve, not after the whole contracts artifact. Decided only after the metric above exists;
  lineage-hash binding is the mitigation for early-node invalidation.

### Phase 5 — Automate (last, and split by kind)

- **Judgment automation frozen** until phases 2–3 land: no new LLM-judgment checks (Contractor
  checks, review layers) while the process they police is still being deleted.
- **Deterministic automation allowed**: evidence queries, fire counters, migration guards —
  stable mechanisms that police things that don't change shape as ceremony shrinks.
- Where a judgment check can become a deterministic validation, prefer the conversion (the
  Tesla lesson: replace a fragile robot with a simple fixture).

## The Learning Loop (deletions as hypotheses)

A deletion without a stated "here is what we would see if this were wrong" is unfalsifiable.
Today the system's memory is add-only prose (AGENTS.md Project Learnings, written after
something hurt); deletions have no memory at all.

**Deletion ledger** — event-sourced, versioned file in this repo (policy lives with the rules it
justifies), joined by the analytics layer at query time:

```json
[
  { "event": "proposed",   "id": "DEL-0007", "kind": "requirement|check|field|stage",
    "target": "REQ-002 tamper-evident hash chain", "reason": "no person; no observable consequence",
    "expected_escape": "a compliance requirement names tamper evidence",
    "window": "12 work items", "scope": "project X | mode quick | all", "ts": "..." },
  { "event": "escape_observed", "id": "DEL-0007", "escape": "…", "location": "reviewer rejection", "ts": "..." },
  { "event": "verdict",        "id": "DEL-0007", "verdict": "keep-deleted | add-back | scope",
    "scope_change": "restore for quick mode only", "ts": "..." }
]
```

- **Right deletion** = pre-registered signal + observation window with real traffic + zero
  escapes. The *location* of an escape is diagnostic: it may justify a scope verdict rather
  than a full add-back.
- **Add-back band (~10%) as calibration**: near-0% across many deletions means step 1 is too
  timid; far above ~10% means the questioning step is broken. The rate is the system's honesty
  meter about its own deletes.
- **Died of silence**: a validation whose fire-counter stays at zero over N opportunities is a
  rule that failed by silence and enters the ledger the same way a failure does.
- **Failure attribution routing**: every Reviewer rejection, failed verification, TIP stop
  condition, and pipeline blocker attributes to a cause; each cause routes somewhere — artifact
  defect ⇒ stage prompt fix; validation gap ⇒ new rule *naming the rule it replaces*;
  instruction gap ⇒ TIP template. AGENTS.md learnings gain structure (trigger / rule / replaced)
  so tightening can also mean deleting.
- **Honest limitation**: some deletions fail silently (documentation nobody needed until an
  onboarding six months later) and learning needs traffic. That irreducible uncertainty is
  exactly why prefer-defer exists, and every deferral carries a review date.

## Analytics Topology

Decision: **SQLite and DuckDB, one job each.** `tasks.db` is OLTP (point lookups, small
transactional writes, WAL readers, durable system of record, additive re-run-guarded
migrations) — SQLite's design center. The cross-project evidence layer is OLAP over many files
(fire rates by rule × mode × version, add-back cohorts) — DuckDB's design center, reading the
SQLite sources in place. Migrating `tasks.db` to DuckDB is rejected: it reverses the cgo
boundary, worsens the write path, and serves an aesthetic "same engine everywhere" requirement
that fails its own phase-1 test (no person, no observable consequence).

```text
per-project tasks.db (SQLite, WAL, sole record)
        │  read-only
        ▼
manifest of project roots
        ▼
pic-analytics (separate Go binary; official github.com/duckdb/duckdb-go/v2,
               sqlite_scanner; joins repo deletion-ledger files at query time)
        ├── one-page evidence report (phase 2 artifact)
        └── disposable .duckdb cache (rebuildable; engine version pinned
            by the duckdb-go tag, which encodes the DuckDB version)
```

- Separate binary target in `go-pic` (e.g. `cmd/pic-analytics`); shares the Go schema
  definitions, eliminating schema drift between analytics and the database it reads. The main
  `pic` build never links it.
- Build the tool **only after** the manual evidence report has changed at least one real
  keep/delete/scope decision (step 5: automate last). Report first, tool second, dashboard last
  and only if earned.
- Every aggregated row carries task-system + policy version, so "never fired" can never mean
  "never had the chance."
- Live databases are scanned read-only with retry (WAL allows concurrent readers); when a
  project is actively writing, prefer its content-hashed Parquet snapshot instead.

## Simulation (worked example, condensed)

Feature "audit trail for work item state changes" arrives as six requirements at `designed`
floor. Step 1 kills two by consequence (tamper-evident hash chain: no person *and* nobody would
notice; email-on-every-transition: nobody, it spams the requester) and defers backfill (circular
justification); Scan narrows REQ-001 from "build audit logging" to "add actor column"
(column-exists guard). Step 2 defers all three reversibly with add-back triggers written down.
Step 3 merges five drafted nodes into three verticals, zero new seams (materialization-test seam
reused with prior art). Step 4: UI node estimate challenged at goal time; runtime overrun
reveals a missing fixture harness ⇒ split into `shared_contract` + consumer with a rationale
edge, not a death march. Step 5 automates only the deterministic transition-actor test; the
weekly evidence query rides the report. Escalation example for the loop: suppose a `quick`-mode
Blueprint with no `verification_seams` later reaches the Task Graph and dies there — the
escape's location says the check is load-bearing *at the planning boundary for unattended
modes* ⇒ scope verdict (restore for `quick`), or convert it to a deterministic schema check.

## Sequencing

Phase order is the algorithm's order and is itself the discipline: no phase-4/5 item starts
before its phase-1–3 prerequisites land. Phase 1 is guidance-only (one session). Phase 2 starts
with manual SQL + the gap list; the `pic-analytics` binary is conditional on the report proving
decision-changing. Phase 3 consumes phase-2 evidence. Phase 4's policy option (first-slice
early start) waits for its metric. Phase 5's freeze lifts only when 2–3 have landed.

## Out of Scope

- Migrating `tasks.db` (or any operational store) to DuckDB.
- New approval gates, workflow states, or required artifact fields for the question set.
- A dashboard or continuous sync over the analytics layer (revisit only after the ledger has
  driven decisions).
- Weakening hash-verified lineage or the handoff verification the scheduler performs.
- Retroactive re-questioning of approved v1/v2 artifacts in flight.

## Implementation Status

| Item | Status | Evidence |
|---|---|---|
| All phases | Proposed (2026-09-02) | This document; no code or prompt changes yet |
