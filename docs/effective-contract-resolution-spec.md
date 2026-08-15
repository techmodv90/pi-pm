# Effective Contract Resolution

This spec proposes deterministic, versioned contract resolution so that task workers execute the currently approved requirements without treating historical or superseded statements as simultaneously mandatory.

## Goals

- Preserve requirements, owner decisions, TIPs, reports, and evidence as immutable historical records.
- Represent approved contract evolution through scoped `replace`, `withdraw`, and `defer` operations.
- Compile one immutable Effective Contract Snapshot when a schema-v3 TIP is activated.
- Bind worker, review, and verification stages to the same snapshot and quarantine stale output.
- Detect conflicts structurally through stable contract keys instead of semantic comparison of prose.
- Keep completed work historically valid while surfacing explicitly retroactive contract changes for owner triage.

## Non-Goals

- Automatically infer contradictions, replacements, or contract keys from requirement prose.
- Rewrite or delete legacy requirements, historical TIPs, completion reports, or verification evidence.
- Support arbitrary include/exclude subject lists or free-form defer conditions.
- Reuse evidence from a replaced requirement as evidence for its replacement.
- Automatically reopen completed tasks or integrate output produced against a stale contract.

## Contract Model

### Requirements

New enforceable requirements are immutable and require a stable `contract_key`. At most one requirement for a contract key may be effective for a subject. A changed requirement is a new record; existing requirement content is never revised in place.

Legacy requirements without a contract key remain independently effective by ID. They are never compared semantically. A replacement for a legacy requirement introduces a key and explicitly targets the legacy requirement ID.

Requirements enter a task contract when they are explicitly assigned to the task or when their owning project, epic, or phase marks them for descendant inheritance. Other parent requirements remain visible as history but do not constrain every descendant.

### Contract Operations

An operation has one subject scope (`project`, `epic`, `phase`, or `task`) and an `inherit_to_descendants` flag.

- `replace`: deactivates one or more targeted requirements and activates exactly one new requirement.
- `withdraw`: deactivates targeted requirements without replacement.
- `defer`: temporarily removes targeted requirements until `subject_completed` or `owner_reactivation`.

Amendment, narrowing, expansion, clarification, and scoped exceptions use `replace` with a new immutable requirement at the intended scope.

Operations begin as `draft`. They become effective only through an explicit owner approval that atomically records the authorizing owner-decision ID. Rejected operations never affect contract compilation.

Every operation declares `completed_task_impact` as `none` or `review`. Both affect future snapshots and stale affected in-progress TIPs. `review` additionally marks affected completed tasks `contract_outdated`; it does not reopen them.

### Effective Contract Snapshot

TIP activation compiles the applicable requirements and approved operations into an immutable snapshot containing:

- Effective requirement IDs and frozen requirement content hashes.
- Excluded historical requirement IDs and their resolution operation IDs.
- Scope and inheritance provenance.
- Contract keys and deterministic conflict results.
- A canonical snapshot content hash.

Schema-v3 TIPs bind to the snapshot ID and hash. Pipeline runs, completion reports, reviews, verification reports, and verification items retain that binding. Review and verification use the bound snapshot; they do not recompute requirements mid-pipeline.

## Resolution Rules

1. Task scope precedes phase, epic, and project scope.
2. At equal scope, explicit operation chains determine the effective requirement; creation time does not.
3. More than one effective requirement for the same non-empty contract key is an unresolved conflict.
4. Missing targets, invalid replacement keys, cycles, ambiguous scope, and unapproved operations fail compilation.
5. Deferred requirements resume only through their declared deterministic condition.
6. A newly approved operation marks affected active TIPs stale.
7. Running output against a newly stale snapshot is preserved but cannot be integrated, reviewed, verified, or accepted.

## Constraints

### Compatibility

- Existing requirement IDs and historical artifacts remain readable and unchanged.
- Completed tasks remain valid under their historical TIP and contract snapshot.
- Legacy schema-v1/v2 TIPs remain readable; open or in-progress tasks require schema v3 before their next worker launch.
- Existing task and epic requirement ownership remains supported during incremental migration.

### Performance

- Contract compilation runs transactionally during TIP activation, not during every read.
- Snapshot and operation lookups use indexed IDs, subject scope, requirement targets, and contract keys.
- Pipeline claim validation compares stored IDs and hashes without semantic analysis.

### Security/Compliance

- Only explicit owner approval activates a contract operation.
- Operation approval, snapshot creation, TIP activation, and stale-pack updates are auditable.
- No worker, reviewer, or verifier may silently choose between unresolved contracts.
- Quarantined stale output cannot mutate the parent repository through integration.

### Operational

- SQLite remains the authoritative workflow store.
- The Go CLI owns compilation and validation; the TypeScript scheduler consumes persisted results.
- Migrations are additive and preserve existing databases.
- Contract compilation failures are non-retryable until the contract or TIP changes.

## Acceptance Criteria

1. Given two effective requirements with the same contract key, when a schema-v3 TIP is activated without an approved resolution, activation fails and no active TIP or snapshot is created.
2. Given an approved `replace` operation, when a TIP is activated in its applicable scope, the snapshot includes the replacement and records the targeted requirement as excluded.
3. Given an unapproved operation, when a snapshot is compiled, the operation has no effect.
4. Given task-assigned and descendant-inherited requirements, when a child snapshot is compiled, it contains those requirements and excludes unrelated parent requirements.
5. Given a legacy requirement without a contract key, when it is explicitly replaced, the replacement becomes effective without mutating the legacy record.
6. Given a deferred requirement, when its deterministic resume condition is unmet, it is excluded; after the condition is met, a new snapshot includes it.
7. Given an approved operation that changes an active snapshot, the affected TIP becomes stale and a running result is preserved but rejected before integration.
8. Given a stale snapshot, review and verification claims fail before launching child agents.
9. Given a replacement requirement, prior evidence remains attached to the old requirement and does not satisfy coverage for the replacement.
10. Given `completed_task_impact=review`, affected completed tasks appear as `contract_outdated`; with `none`, they remain historically accepted.
11. Given an existing database, migration preserves all historical rows and allows legacy artifacts to render.
12. Given an open task with a schema-v1/v2 TIP, worker launch requires activation of a schema-v3 TIP bound to an effective snapshot.

## Open Questions

- **Project scope ownership:** The current schema directly owns requirements by task or epic, not project. The implementation owner must decide whether project-scoped requirements belong in this first release or are rejected until project entities are persisted in the workflow DB. Impact: schema breadth and applicability traversal.
- **Phase identity:** Phases currently use task metadata rather than a first-class phase table. The implementation owner must confirm whether a phase-scoped operation targets the phase parent task plus phase number or only the materialized phase task. Impact: scope uniqueness and descendant traversal.
- **Contract-outdated presentation:** The dashboard owner must choose whether outdated completed tasks use a task column, a separate impact table, or a derived queue only. Impact: UI/API compatibility and migration cost.
