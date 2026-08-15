# Work Item Model

This spec proposes replacing the separate Epic, Task, phase-Task, and Task Item hierarchy with one recursive Work Item graph so that planning, decomposition, readiness, and execution use one consistent unit of work without weakening execution-contract guarantees.

## Goals

- Represent epics, features, tasks, bugs, chores, and gates as typed Work Items with optional parent relationships.
- Use one dependency graph and one derived readiness rule for every independently actionable Work Item.
- Attach an Execution Contract only to Work Items that enter implementation, preserving TIP, pipeline, review, Completion Report, and explicitly required gate authority.
- Replace Epic materialization and Task phase repair with one operation that creates dependency-linked child Work Items.
- Require sequential Scan acceptance, RRI approval, Vision approval, Blueprint approval, Contract approval, task-graph approval, and explicit implementation authorization before materialization or execution.
- Infer proposed Work Item types, decomposition, blocking dependencies, and checklist promotion from approved Blueprint and Contract artifacts before materialization.
- Preserve existing IDs, historical artifacts, lineage hashes, reports, decisions, and audit events through migration.
- Remove Task Items as an independently persisted workflow concept: independently actionable steps become child Work Items, while non-authoritative checklist text belongs in descriptions or TIP content.

## Non-Goals

- Adopt Beads, Dolt, or a second workflow database.
- Weaken or replace TIP activation, candidate review binding, Completion Report evidence, or explicitly required contractor verification and owner acceptance gates.
- Permit every Work Item type to launch a worker or own implementation evidence.
- Preserve the current Epic and Task workflows indefinitely as parallel authorities.
- Introduce project releases or cross-project Work Item graphs in this change.

## Work Item Model

Every Work Item has a stable ID, type, title, description, status, priority, optional parent, and revision. Supported initial types are:

- `epic`: an aggregate intent and acceptance boundary.
- `feature`: a deliverable capability that may contain executable children.
- `task`: an independently executable and reviewable implementation change and leaf in the Work Item tree.
- `bug`: an independently executable defect correction and leaf in the Work Item tree.
- `chore`: independently executable maintenance work and leaf in the Work Item tree.
- `gate`: a non-executable approval or external-condition blocker.

Parentage expresses containment through aggregate `epic` and `feature` Work Items and may have arbitrary aggregate depth. A `task`, `bug`, or `chore` cannot own child Work Items. Dependencies express blocking execution order independently of containment. A Work Item is ready when it is open, unclaimed, not deferred, all blocking dependencies are closed, and every required gate is satisfied. Readiness is derived and is never stored as an independently mutable status.

Closing a child does not automatically close its parent. A routine executable leaf may close after candidate review, integration, and promotion of a valid Completion Report. Conditional contractor-verification or owner-acceptance gates must pass when required by the Work Item, its Execution Contract, risk policy, or aggregate policy. An aggregate Work Item may close only after all non-cancelled children are closed and its applicable aggregate verification and owner-acceptance gates pass. Closing a parent does not implicitly close or cancel children.

## Execution Contract

An Execution Contract is optional and may be created and activated only for `task`, `bug`, or `chore` Work Items. It consists of the existing implementation authority:

- One active immutable Task Instruction Pack revision containing the complete resolved implementation authority.
- Pipeline runs bound to the TIP revision and content hash.
- Candidate patch evidence bound to a pipeline run.
- A review verdict bound to the integrated candidate.
- One promoted canonical Completion Report.
- Conditional contractor verification when required by the Work Item, its Execution Contract, risk policy, or aggregate policy.
- Conditional owner acceptance at an explicit acceptance boundary or for configured high-risk work.

The name Task Instruction Pack remains unchanged in this release because it identifies implementation instructions rather than the generic Work Item record. Materializing a TIP makes that revision immutable; explicit implementation authorization activates it for execution. Changing any execution input requires a new TIP revision and authorization. Every execution artifact binds directly to the applicable TIP revision and content hash. A Work Item with no active Execution Contract cannot launch a worker, publish a Completion Report, or be marked implementation-complete. Routine leaves do not require contractor verification or owner acceptance unless an explicit gate requires them.

Aggregate `epic` and `feature` Work Items own intent, requirements, design, decomposition, aggregate verification, and owner acceptance. They do not launch implementation workers directly. A `gate` owns a deterministic satisfaction condition and never owns an Execution Contract.

## Aggregate Workflow

An aggregate `epic` or `feature` moves through these sequential, revision-bound gates:

1. **Create:** Record the initial outcome, scope, and workflow mode.
2. **Scan:** Inspect the repository and present a Scan Report covering the stack, architecture, commands, patterns, and risks. The owner accepts the report or requests factual corrections. An accepted Scan revision unlocks RRI.
3. **RRI:** Conduct the owner interview using accepted Scan evidence, checkpoint answers, and present the complete RRI Report and requirements matrix. Owner approval of the RRI revision unlocks Vision.
4. **Vision:** Present the product and architecture direction, user or operator flows, system boundaries, and stack decisions. Owner approval of the Vision revision unlocks Blueprint design.
5. **Blueprint:** Present the detailed solution design, including applicable file/module structure, API endpoints, database schema, authorization, validation, state transitions, error behavior, verification strategy, and requirement traceability matrix. Owner approval of the Blueprint revision unlocks Contracts.
6. **Contracts:** Present stable behavioral, API, data, compatibility, STEP/GATE, and requirement-mapping obligations. Owner approval of the Contract revision unlocks task-graph planning.
7. **Task Graph:** Present proposed aggregate children, executable `task`, `bug`, and `chore` leaves, containment, dependencies, requirement assignments, bounded ownership, and verification commands. Owner approval of the task-graph revision permits checkpoint and TIP materialization.
8. **Materialize:** Atomically save the approved design checkpoint, create the Work Item graph, and create an inactive immutable TIP revision for every executable leaf. Materialization does not authorize worker launch.
9. **Authorize Implementation:** Wait for an explicit owner action that authorizes implementation and activates eligible TIPs. Only dependency-ready leaves with active TIPs may launch workers.
10. **Implement and Review:** Execute dependency-ready leaves through worker, candidate review, integration, and canonical Completion Report promotion until all required leaves close.
11. **Aggregate Verification and Close:** Verify the integrated aggregate against approved requirements, Blueprint, Contracts, and operational checks. Resolve failed or deferred requirements explicitly, obtain owner acceptance when required, close Features, and close the Epic only after every required aggregate gate passes.

Every acceptance or approval binds to one immutable artifact revision and content hash. A material change creates a new revision and invalidates approvals, task graphs, checkpoints, and TIPs derived from the changed stage onward; earlier unaffected approvals remain valid. No stage may be skipped, combined with its successor, or treated as implicitly approved. Scan acceptance confirms repository evidence rather than product intent; every later stage requires explicit owner approval.

## Decomposition

Approved designs produce child aggregate or executable Work Items and dependency edges through one canonical decomposition operation. Standalone executable work that proves too large must be converted to an aggregate `feature` before that operation creates executable leaf children; phase repair no longer creates a separate phase-specific hierarchy or Work Item type.

After Blueprint and Contract approval, the system infers a task-graph proposal from those approved artifacts: Work Item types, parentage, blocking dependencies, requirement assignments, and which checklist entries qualify as executable leaves. Inference is non-authoritative and must include the source artifact revision, source text, and rationale for every proposed Work Item and dependency. The owner may edit, approve, or reject the proposal. Only an approved task-graph revision may be materialized, and materialization remains the sole authority that creates graph records or inactive Execution Contracts.

Inference must not silently promote ambiguous or mechanical checklist text. When the prose does not establish a bounded outcome, one owner, an observable acceptance condition, or a clear blocking relationship, the proposal must leave the item as TIP content or mark it unresolved for owner review rather than inventing missing scope or dependencies.

An executable leaf becomes a `task`, `bug`, or `chore` only when it has bounded scope, is independently executable and reviewable, has one worker owner at a time, and has an observable acceptance condition. It should fit one focused execution session. Work requiring multiple owners, independently verifiable contributions, unrelated acceptance outcomes, or ownership handoffs is too large and must be split into dependency-linked leaves under an aggregate parent.

A proposed leaf is too small when it is only a mechanical edit or cannot produce meaningful verification evidence independently. File edits, imports, schema statements, and similar implementation steps remain non-authoritative description or TIP content unless they independently satisfy a defined product, accessibility, security, or operational outcome. Decomposition follows behavioral or contract boundaries rather than file, team, or architectural-layer boundaries. A foundational leaf may establish an independently verifiable contract required by another leaf even when it has no direct end-user value.

Parentage expresses containment under an aggregate Work Item. Blocking dependencies express execution order between executable leaves and affect readiness. No separate subtask type or non-blocking task-link relationship is introduced.

Execution policy (`strict_sequential`, `partially_parallel`, `parallel_allowed`, or `deferred_optional`) belongs to the parent-child decomposition edge or its materialization record. Phase number and phase title become optional presentation metadata, not a distinct lifecycle authority.

Creation of child Work Items, dependencies, provenance, the approved checkpoint, and initial inactive Execution Contracts is atomic. Repeating the same approved task graph is idempotent. TIP activation and worker launch require a later explicit implementation-authorization decision.

## Migration

- Existing Epic IDs become `epic` Work Item IDs.
- Existing Task IDs remain unchanged and become `task` Work Item IDs.
- Existing materialized Tasks retain their parent Epic, design provenance, dependencies, and TIP lineage.
- Existing phase child Tasks remain `task` Work Items, retain their parent and dependency relationships, and keep phase presentation metadata.
- Existing standalone parent Tasks that aggregate repaired phases become `feature` Work Items. A migrated `task`, `bug`, or `chore` cannot retain children; any existing direct execution evidence remains attached as immutable history and must be reconciled explicitly during migration.
- Existing Task Items remain readable as archived checklist history during migration. They are not automatically converted to child Work Items because their text does not establish an executable contract.
- Existing foreign-key-owned artifacts are migrated transactionally to the corresponding Work Item ID without changing artifact IDs, hashes, timestamps, or report content.
- Historical records remain readable after migration, but legacy `pic task ...` and `pic feature ...` commands are removed at cutover. All Work Item reads and mutations use `pic work-item ...`.

## Constraints

### Compatibility

- Existing Epic and Task IDs, URLs, historical records, and active pipeline lineage must remain valid after migration.
- Existing schema-v3 TIP hashes must not change solely because their owner becomes a Work Item. Existing Effective Contract Snapshot records and hashes remain immutable historical evidence but are not created or consulted by the new workflow.
- A migrated in-progress pipeline must either resume against the same owner ID and legacy lineage hashes or be explicitly quarantined; migration must not silently restart it. New pipeline runs bind directly to a TIP revision and content hash.
- The Go CLI remains the only lifecycle mutation authority; TypeScript and Svelte clients consume its persisted state and APIs.

### Performance

- Ready-work queries must use indexed parent, status, claim, and blocking-dependency fields and must not traverse the graph in application memory.
- Decomposition and lifecycle mutations must retain the current transactional behavior; no cross-database coordination is introduced.

### Security/Compliance

- Existing owner-decision, audit-event, contract-approval, review, and verification provenance must remain immutable and attributable.
- No migration or compatibility path may close a Work Item without the gates required by its type, active Execution Contract, risk policy, or aggregate policy; unrequired verification and acceptance records must not be fabricated.
- Untrusted Work Item type, dependency, and parent inputs must be validated by the Go CLI, including cycle prevention.

### Operational

- SQLite remains the authoritative per-project workflow store.
- Migration must be automatic, idempotent, and covered by legacy-database fixtures.
- The installed Go CLI, Svelte dashboard, and `pi-ext` runtime move to the Work Item model in one lockstep project release.
- Generated dashboard and runtime artifacts are produced only by the existing build commands.

## Acceptance Criteria

1. Given migrated Epic, Task, phase, and dependency records, when the database opens, then their IDs, hierarchy, dependencies, artifact lineage, hashes, and historical reports remain unchanged and the migration can run again without mutation.
2. Given a parent Work Item at any supported depth, when a child is created, then the child appears in the same graph and a containment cycle is rejected transactionally.
3. Given a `task`, `bug`, or `chore`, when child creation is requested, then the operation is rejected transactionally because executable Work Items are leaves.
4. Given an open Work Item with an unresolved blocking dependency or gate, when ready work is queried or claimed, then that Work Item is excluded; after every blocker closes, it becomes claimable without a stored readiness update.
5. Given an aggregate `epic` or `feature`, when worker launch is requested directly, then launch fails before creating a pipeline run.
6. Given an executable Work Item without an active Execution Contract, when worker launch is requested, then launch fails with the next required contract step.
7. Given an executable Work Item with an active immutable TIP revision, when its pipeline runs, then the run and all resulting candidate, review, and Completion Report evidence bind to that TIP revision and content hash; required contractor-verification or owner-acceptance gates run only when explicitly required.
8. Given an approved task graph, when it is materialized twice, then only one set of child Work Items, dependencies, provenance records, checkpoint, and initial inactive Execution Contracts exists.
9. Given a decomposition proposal containing independently executable units and mechanical checklist steps, when it is materialized, then only bounded, single-owner units with observable acceptance conditions become executable leaves and mechanical steps remain description or TIP content.
10. Given proposed work that requires multiple independent owners or unrelated verification outcomes, when its task graph is approved, then it is represented as an aggregate with dependency-linked executable leaves rather than one shared executable Work Item.
11. Given approved Blueprint and Contract revisions, when inference runs, then it proposes Work Item types, parentage, blocking dependencies, requirement assignments, and checklist promotion with source provenance and rationale without mutating the Work Item graph.
12. Given ambiguous prose or a mechanical checklist step, when inference runs, then it remains TIP content or is marked unresolved for owner review rather than being silently promoted or assigned an invented dependency.
13. Given an inferred task graph that has not been approved, when materialization is requested, then no Work Item, dependency, provenance record, checkpoint, or Execution Contract is created.
14. Given a routine executable leaf with no conditional gates, when review passes, the candidate is integrated, and a valid Completion Report is promoted, then the leaf can close without contractor verification or owner acceptance.
15. Given an executable leaf with a required contractor-verification or owner-acceptance gate, when the Completion Report is promoted, then closure remains blocked until that required gate passes.
16. Given a parent with an open non-cancelled child, when parent closure is requested, then closure fails; after all children close, aggregate verification and owner acceptance remain required where applicable.
17. Given a migrated Task Item, when its former owner is read, then the checklist text and completion state remain available as history but the row cannot be claimed or executed as a Work Item.
18. Given the lockstep release after cutover, when CLI commands and extension tool actions are enumerated, then `pic work-item ...` is the only Work Item authority and no `pic task ...` or `pic feature ...` command remains.
19. Given an Epic or Feature workflow, when each stage completes, then only an accepted Scan unlocks RRI, only an approved RRI unlocks Vision, only an approved Vision unlocks Blueprint, only an approved Blueprint unlocks Contracts, and only approved Contracts unlock task-graph planning.
20. Given an approved task graph, when materialization completes, then its checkpoint, children, dependencies, provenance, and immutable TIP revisions are created atomically, but no worker can launch before explicit implementation authorization activates eligible TIPs.
21. Given an approved upstream artifact changes materially, when its new revision is saved, then every downstream approval and derived task graph, checkpoint, or TIP is invalidated while unaffected earlier approvals remain valid.
22. Given all required executable leaves are closed, when aggregate verification runs, then it evaluates every approved requirement, Blueprint obligation, Contract obligation, integrated behavior, and required operational check before the Feature or Epic can close.
23. Given the production build and legacy database fixtures, all Go tests, TypeScript tests, type checks, and dashboard builds pass with no stale Epic/Task mutation path.

## Open Questions

- **Checklist retention:** Should archived Task Items remain indefinitely readable or be removed after a documented migration window? The project owner knows whether historical checklist detail is relied upon. Getting this wrong either preserves avoidable schema indefinitely or deletes useful history.