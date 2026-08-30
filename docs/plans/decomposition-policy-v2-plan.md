# Decomposition Policy v2 — Vertical-by-Default Task Graph with Seam-Bound Verification

## Goal

Change decomposition policy, not workflow authority: Blueprint becomes the solution spec with
owner-approved verification seams, Contract becomes the typed translation layer from behavior to
obligations, and the Task Graph defaults to tracer-bullet (vertical) Work Items with enforced,
justified exceptions and minimal blocking edges. All persisted approval, lineage, TIP, review, and
merge controls are unchanged. No new approval gates or workflow states.

## Background

Comparison with `mattpocock/skills` `to-spec` and `to-tickets` (2026-08-30) concluded:

- Our ladder (Scan → RRI → Vision → Blueprint → Contract → Task Graph → TIP) already out-structures
  both templates on artifact separation, requirement authority, and execution governance.
- Two ideas are worth importing as **decomposition discipline inside the Task Graph stage**, not as
  a replacement workflow: the tracer-bullet vertical-slice default (to-tickets) and test-seam design
  before specification approval (to-spec).
- The testing surface is currently an unowned decision in our ladder: each TIP carries a
  `verification` array, but nothing upstream decides or approves *where* behavior gets proven, and
  nothing binds Blueprint architecture to per-task verification.

Target shape:

```text
Scan
-> RRI: owner-impacting decisions and requirements
-> Vision: product/system direction
-> Blueprint: solution spec and verification seams
-> Contract: behavioral and technical obligations (typed, seam-bound)
-> Task Graph:
     vertical Work Items by default
     explicit shared-contract / wide-refactor / integration-gate exceptions
     minimal blocking edges with rationale
     requirement and obligation coverage
     owner granularity review
-> Materialize -> Authorize -> TIP at first claim
-> Worker / Review / Completion Report -> Contractor verification
-> Aggregate verification -> Owner acceptance -> Merge
```

## Non-Negotiable Invariants

- Go CLI remains sole lifecycle mutation authority; the extension renders and dispatches.
- Planning artifacts stay immutable, content-hashed revisions with explicit owner checkpoints.
- Existing databases and in-flight artifacts upgrade non-destructively: v1 artifacts validate under
  v1 rules for their whole lifecycle; v2 rules apply only to artifacts carrying the v2 policy marker.
- No retro-editing of approved, hash-bound artifacts. In-flight structural plans finish under the
  current policy; the new policy applies to graphs authored after Task 3 lands.
- No new approval gates: seam approval rides the existing Blueprint approval checkpoint; granularity
  review rides the existing Task Graph approval checkpoint, before materialization. Implementation
  authorization remains execution permission, not a second granularity decision.

## Design Decisions

1. **Blueprint drops `task_decomposition_preview` and gains `verification_seams`.** Nothing
   downstream parses the preview except its own validator; the contractor's five-check review
   redefines the fifth check from `task_decomposition` to `verification_seams` (five checks remain).
   The owner's early cost signal survives via Contract's `task_graph_summary` (`tip_count`,
   `estimated_minutes`, both already validated non-empty).
2. **Versioned artifact schemas.** Each Blueprint, Contract, and Task Graph carries a
   `decomposition_policy_version` marker. Version 1 artifacts are never re-validated against v2
   rules at any stage. Existing format/version fields remain unchanged; the policy marker is not
   substituted for an artifact's format version.
3. **Seam scope is per-contract, not global.** The rule is "highest seam that isolates the
   requirement under test" — not to-spec's "ideal number of seams is one." Global seam minimization
   collapses every task verification into end-to-end runs: slow verification, expensive review-fix
   cycles, coarse failure evidence. Aggregate verification already is the highest seam.
4. **Reference, don't restate (policy point 10).** Each layer cites upstream artifacts instead of
   re-authoring them. Every executable node must have an effective Given/When/Then acceptance
   contract: it may be authored on the node or resolved from exactly one covered requirement and
   frozen into the TIP. Nodes composing multiple requirements must state their own acceptance.
5. **Artifact owns provenance.** The approved Task Graph is the immutable source of node, edge,
   requirement, obligation, mode, rationale, seam, acceptance, and policy-version provenance.
   Materialized Work Items are execution projections and must retain the source graph artifact ID,
   revision, and content hash; mutable projection columns are not a replacement for that record.

## The Policy (authoring convention)

1. Blueprint defines solution behavior, architecture, and seams — not ticket boundaries.
2. Verification seams are named before Blueprint approval and approved by the owner.
3. Contract converts behavior into typed obligations bound to declared seams.
4. Task Graph nodes are vertical by default: one requirement-keyed, independently verifiable outcome.
5. Horizontal work is allowed only under an explicit mode with a reason. `wide_refactor` requires a
   paired cleanup/contract node; `shared_contract` requires explicit provider/consumer edges and a
   downstream consumer; `integration_gate` is a verification-only node and has no cleanup-pair
   requirement.
6. Blocking edges are minimal, direct, and rationale-carrying; containment and informational
   relations never affect readiness.
7. Provenance (requirement keys, obligation keys, mode, rationale, owner-approved changes) is
   mandatory and visible in the artifact schema.
8. The owner reviews granularity, verification, blockers, and exceptions at the existing Task Graph
   approval checkpoint, before materialization, via five structured questions.
9. External weaknesses are not imported: RRI interviews stay; user stories are not duplicated;
   SQLite/CLI remain canonical; migration/staleness/aggregate gates stay.
10. Each layer references, never restates: requirements own Gherkin acceptance; Blueprint scenarios
    cite requirement keys; nodes cite requirement and obligation keys and an effective acceptance
    contract; TIPs freeze the execution detail and its resolved acceptance.

## Artifact Schema (v2)

### Blueprint additions (`validateBlueprintArtifact`)

```json
{
  "decomposition_policy_version": 2,
  "verification_seams": [
    {
      "id": "cli-materialize",
      "surface": "pic work-item materialize against a temporary SQLite database",
      "isolates": "materialization atomicity and idempotency",
      "prior_art": "TestPlanningResetDryRunDoesNotMutate"
    }
  ]
}
```

- `task_decomposition_preview` is removed from the required sections and from the blueprint prompt.
- v2 validation requires at least one seam; every seam needs unique `id`, non-empty `surface` and
  `isolates`. `prior_art` is optional but recommended (existing tests probing that seam).
- A v2 Blueprint must declare the seams needed by its requirements and must retain the approved
  Blueprint artifact ID, revision, and hash for downstream binding.

### Contract additions (`validateContractReport`)

Each obligation gains:

```json
{
  "obligation_key": "…",
  "class": "data_invariant",
  "seam": "cli-materialize"
}
```

- `class` ∈ `user_behavior | data_invariant | interface_contract | security | migration_rule |
  operational_rule | integration_gate` (one primary class; hybrids pick the dominant one).
- `seam` must reference a seam declared in the approved Blueprint on the same planning lineage
  (validated at the Contract save/approve path, which already runs inside the transaction).
- The Contract must retain the approved Blueprint artifact ID, revision, and content hash. Every
  obligation has one primary seam and one obligation key.

### Task Graph node additions (`TaskPlanNode`)

```json
{
  "key": "F01",
  "decomposition_mode": "vertical",
  "acceptance": "Given an approved graph, when materialization runs, then Work Items, dependencies, provenance, checkpoint, and inactive TIP records commit together and repeat runs create no duplicates",
  "depends_on_rationale": { "S01": "consumes the persisted schema contract S01 establishes" },
  "verification": [{ "seam": "cli-materialize", "obligation_keys": ["OB-1"], "command": "go test ./cmd/pic -run TestMaterialize", "expected": "atomic commit and idempotent repeat" }]
}
```

Exception shape:

```json
{
  "key": "S01",
  "decomposition_mode": "shared_contract",
  "exception_reason": "widens the work_items schema consumed by CLI, scheduler, and dashboard; a vertical slice would touch all three before the contract stabilizes",
  "paired_contract_node": "S09"
}
```

- `decomposition_mode` ∈ `vertical | shared_contract | wide_refactor | integration_gate`
  (default `vertical`; absent field on v2 = `vertical`).
- `exception_reason` is required and non-empty when mode ≠ `vertical`.
- `paired_contract_node` is required only for `wide_refactor`: it identifies the node that contracts
  or cleans up the expansion, and the declaring node must be in that node's `depends_on` closure.
- `shared_contract` requires at least one declared provider/consumer relationship and a consumer
  node that depends on the shared-contract node. `integration_gate` requires no pair and must list
  the obligations or requirements it verifies.
- Every executable leaf requires an effective `acceptance` with separate Given, When, and Then
  steps. A node with one requirement may resolve that requirement's acceptance; a node with two or
  more requirements must provide node-level acceptance. The resolved acceptance is frozen into the
  TIP before claim.
- Every `depends_on` edge needs a rationale entry in `depends_on_rationale`.
- Every verification entry requires a seam ID, at least one requirement or obligation key, an
  executable command/assertion, and an expected evidence statement. Each executable leaf needs at
  least one such entry, and every Contract obligation needs at least one evidence-producing node.
- Node implementation hints (`files`, `business_rules`, `state_transitions`, …) remain TIP inputs;
  the TIP is the sole authoritative copy after freeze.

## Implementation Tasks

| # | Task | Touchpoints | Evidence required |
|---|---|---|---|
| 1 | **Artifact schemas** | `internal/tip/tip.go` (`TaskPlanNode`, `ParseTaskPlanJSON` v2 fields, seams type); `go-pic/cmd/pic/work_items.go` (`validateBlueprintArtifact`, `validateContractReport` structs); `pi-ext/pipeline/pic-show.ts` optional fields | Schema-shape tests; **v1 fixtures validate unchanged** (regression proving the version gate); `pnpm run check` clean |
| 2 | **Fail-closed validation** | Blueprint validator: ≥1 seam, uniqueness, non-empty surface/isolates; preview checks removed for v2. Contract validator: class enum + seam cross-artifact binding and Blueprint lineage. Graph validation (materialize + `graph-validate`): executable leaf ⇒ effective Gherkin acceptance plus seam-bound verification; obligation coverage extended to `obligation_keys`; `wide_refactor` ⇒ reason + paired node + DependsOn closure; `shared_contract` ⇒ provider/consumer dependency; `integration_gate` ⇒ explicit verification coverage; every edge rationale non-empty | Red/green per rule: one test asserting each rejection names the defect, one happy path per rule; test inherited single-requirement acceptance and frozen TIP resolution; all existing tests green (v1 path untouched) |
| 3 | **Prompt surfaces** | `pi-ext/tasking/work-item-prompts.ts`: Blueprint prompt (solution-spec shape, seams, no decomposition; fifth checkpoint check becomes `verification_seams`), Task Graph prompt (tracer-bullet default, six node questions, effective acceptance, seam-bound verification, exception rules, reference-don't-restate); `pi-ext/pipeline/stage-prompts.ts` + `next_actions.go`: **Task Graph approval checkpoint** renders the five granularity questions (too coarse/fine? independently verifiable? every blocker genuine? exceptions justified? merge/split?) | Prompt-contract tests in the existing source-regex style; Task Graph approval output shows the questions |
| 4 | **Persistence + CLI** | Migration 8 (transactional, once): add dependency rationale and projection fields `decomposition_mode`, `decomposition_reason`, and `paired_contract_node`, plus source Task Graph `artifact_id`, `revision`, and `content_hash`; materialization records those values alongside each Work Item projection; `pic show` and its existing dashboard/API consumer expose projection metadata and source lineage; `graph-validate` reports new checks | Migration transactionality/once-semantics tests in the established pattern; source lineage survives reload; CLI JSON additive-only (no removed keys); `TestPlanningResetDryRunDoesNotMutate` still green |
| 5 | **Docs** | This plan's Implementation Status updated; convention section added; Task 4 package extraction (`internal/workitem`, `workflow`, `pipeline`, `acceptance`, `store`) explicitly demoted — stays documented-ongoing unless it materially unblocks behavior | Doc review |

**Six node questions (Task Graph prompt):** What behavior becomes possible? Which requirement keys
does it cover? What is the smallest independently verifiable outcome? What is its direct blocker?
What command or test proves it? Can it fit one focused execution session?

**Five owner questions (Task Graph approval checkpoint):** Are slices too coarse or too fine? Does each
slice have independently meaningful verification? Does each blocker genuinely gate execution? Are
any horizontal exceptions justified? Should any node merge or split?

## Sequencing

1 → 2 are the correctness core and land fail-closed with regression cover **before** any prompt
tells agents the new rules (3) — the policy cannot be advertised before it is enforced. Task 4 is
additive projection and lineage surface only. Task 5 closes the loop. Roughly one focused session
per task; the cross-artifact seam binding in Task 2 (contract obligation → Blueprint seam) and
effective acceptance resolution are the pieces needing the most care.

## Verification Gate (per task)

- `cd go-pic && go test ./...`
- `cd pi-ext && pnpm test && pnpm run check && pnpm lint`
- Working tree clean after each integrated task.

## Out of Scope

- Tracker export (GitHub/Linear with native blocking links) — deferred until wanted.
- Changes to TIP freeze semantics, review/verify/merge gates, or pipeline scheduling.
- Retro-editing in-flight structural plans (they finish under v1 rules).
- Promoting the package-extraction work to a refactor Work Item (stays documented-ongoing).

## Version And Lineage Rules

- `decomposition_policy_version: 1` means existing behavior. Such artifacts may be resumed,
  materialized, and verified under v1 rules without seams, edge rationales, or decomposition modes.
- `decomposition_policy_version: 2` is required on newly authored Blueprint, Contract, and Task
  Graph artifacts after this policy lands. Each downstream artifact must bind to the exact approved
  predecessor artifact ID, revision, and content hash.
- New v2 planning chains require v2 Blueprint, Contract, and Task Graph predecessors. A v1 planning
  chain remains v1 for its whole lifecycle; upgrading it requires a new planning reset and newly
  authored v2 artifacts, never in-place reinterpretation of an approved v1 artifact.
- Materialized Work Items retain source graph identity, but their mutable projection fields do not
  revalidate or rewrite historical v1 artifacts. Resuming a v1 graph continues under v1 rules.

## Implementation Status

| Task | Status | Evidence |
|---|---|---|
| 1. Artifact schemas | Done (2026-08-29) | `TaskPlanDocument.DecompositionPolicyVersion`, `VerificationSeam`, node `decomposition_mode`/`exception_reason`/`paired_contract_node`/`acceptance`/`depends_on_rationale`, obligation `class`/`seam` in `internal/tip`; validators parse v2 fields in `work_items.go`; `pic-show.ts` optional projection fields; `TestBlueprintArtifactPolicySchemas`/`TestContractArtifactPolicySchemas` prove v1 fixtures validate unchanged and the v2/v3 version gate |
| 2. Fail-closed validation | Done (2026-08-29) | Blueprint validator wired into `artifact-save` (was dead code): v2 requires ≥1 unique seam with surface/isolates, drops the preview requirement. Contract validator: class enum + seam; `validateContractPolicyBinding` runs inside the transaction at both the Contract save and approve paths, checking the exact approved Blueprint id/revision/hash and per-obligation seam membership. Graph rules run in `validateTaskGraphArtifact` (graph-validate, approval, and now materialization): edge rationales, effective acceptance (node-authored Gherkin or resolved single requirement), seam-bound verification gates with known requirement/obligation keys, and all four decomposition modes with their exception rules. `TestDecompositionPolicyApprovalChain` asserts one rejection per rule plus the happy path; `TestDecompositionPolicyV1Unchanged` proves the v1 path; `internal/tip/tip_test.go` covers the TIP acceptance freeze |
| 3. Prompt surfaces | Done (2026-08-29) | Blueprint prompt = solution spec with `verification_seams` and `decomposition_policy_version: 2`, fifth checkpoint check redefined to `verification_seams` (policy-dependent in `review_blueprint_checkpoint`); Contract prompt binds `source_blueprint` and classifies obligations; Task Graph prompt carries the tracer-bullet default, six node questions, effective acceptance, seam-bound verification, and exception rules; `workflow-status` renders the five granularity questions as `checkpoint_questions` at the Task Graph approval checkpoint (`TestTaskGraphApprovalCheckpointQuestions`); prompt-contract tests in `work-item-prompts.test.ts`; Blueprint renderer (`blueprint-report.ts`) renders a VERIFICATION SEAMS section for v2 |
| 4. Persistence + CLI | Done (2026-08-29) | Migration 8 `decomposition_policy_projection` (transactional, once, re-run tolerant): `rationale` on **work_item_relations** (see notes) and `decomposition_mode`/`decomposition_reason`/`paired_contract_node`/`source_graph_artifact_id`/`source_graph_revision`/`source_graph_content_hash` on work_items; materialization records projection + lineage per node and edge rationales on `blocks` relations; `pic show` exposes projection metadata, edge rationale, and source lineage; `graph-validate` reports `decomposition_policy_version`; `TestDecompositionProjectionMigration` (pre-v8 simulation + once-semantics) and `TestDecompositionProjectionMaterialization` (aggregate + standalone projections, `pic show` surface) |
| 5. Docs | Done (2026-08-29) | This status section, implementation notes below, and the decomposition convention in AGENTS.md. Task 4 package extraction (internal/workitem, workflow, pipeline, acceptance, store) stays documented-ongoing; this plan does not promote it |

## Implementation Notes (2026-08-29)

- **Rationale column location.** The plan named `work_item_dependencies`; implemented on
  `work_item_relations`. `work_item_dependencies` is the legacy table retired by the canonical
  backfills — materialization writes blocking edges and `pic show` reads dependencies from
  `work_item_relations`, so that is the table a rationale can live on.
- **Re-run tolerance.** The established older-binary simulation clears `schema_migrations`
  records, so every migration must survive a re-run against already-widened tables. Migration 8
  guards each `ALTER TABLE ... ADD COLUMN` with a column-exists check instead of failing on
  duplicate columns.
- **Seam binding scope.** Verification-entry seams bind to the approved Blueprint only when the
  planning profile carries a Blueprint stage (full/designed depths). Quick/standard and standalone
  profiles have no Blueprint, so v2 graphs there get shape-checked seams without cross-artifact
  binding; obligation-key references still fail closed when no approved Contract exists.
  *(Superseded by the review response below: v2 graphs now require Blueprint and Contract
  predecessors outright.)*
- **Materialization re-validates.** `materialize` now runs the full `validateTaskGraphArtifact`
  (parse + coverage + obligations + decomposition policy) rather than parse + coverage, so a
  graph can never materialize under rules it was not approved against.
- **TIP freeze.** A node-authored acceptance (required when the node composes two requirements)
  is frozen into the pack content and rendered under `## EFFECTIVE ACCEPTANCE`; single-requirement
  nodes resolve their acceptance from the frozen requirement snapshot.
- **Approval output.** The five granularity questions ride the existing Task Graph approval
  checkpoint as `checkpoint_questions` on `workflow-status` output — no new workflow state.

## Review Response (2026-08-29, first pass)

All six findings are fixed, each with a regression that fails before the fix and passes after:

| Finding | Resolution |
|---|---|
| P1 migration default contradicts v2 semantics | Migration 8 creates `decomposition_mode` with `DEFAULT 'vertical'` (existing rows pick up the default on `ALTER`), and materialization normalizes an absent mode to `vertical` via `decompositionModeOf` before both the aggregate insert and the standalone update. Regressions: the migration test asserts the pre-v8 row reads `vertical`; the projection test asserts the aggregate root carries the column default and that a v2 node omitting the mode persists as `vertical`. |
| P1 provider uniqueness unenforced | `validateTaskGraphObligations` rejects `len(providers[key]) != 1` ("must have exactly one provider node, found N") instead of only the zero case. Regression: a graph where two nodes provide the same obligation is rejected with `found 2`; the prompt surfaces now state "exactly one provider node". |
| P1 v2 graphs bypass seam authority | A policy-v2 graph now requires the planning profile to carry BOTH Blueprint and Contract stages — quick/standard and standalone profiles reject v2 graphs with "requires approved Blueprint and Contract predecessors … keep the graph on policy v1" instead of silently skipping binding. `approvedBlueprintSeams` also fails closed if the profile lacks the stage. The standalone Task Graph prompt stays v1 (names the field only to forbid it), and `v2StandaloneGraphV2Policy` regression: saving a v2 standalone graph fails with "requires an approved Contract on the same planning lineage". |
| P1 Task Graph predecessor lineage missing | `TaskPlanDocument` gains `source_contract` (`ArtifactLineage`: artifact_id/revision/content_hash). `validateTaskGraphSourceContractBinding` (v2-only) requires the exact approved Contract lineage and runs at artifact-save (binding-only, draft flow preserved), graph-validate, approval, and materialization. The lineage is persisted as part of the immutable approved graph artifact (Design Decision 5: the artifact owns provenance; the projection's `source_graph_*` columns then chain root → graph → contract). Regressions: stale hash, wrong predecessor id, and missing `source_contract` each fail at save with named defects; the aggregate prompt demands the binding while the standalone prompt stays on v1. |
| P2 integration_gate verification bypass | Non-executable nodes are now validated when their mode is `integration_gate`: the coverage check is followed by the same ≥1-entry seam-bound verification loop as executable leaves, regardless of node type. Regression: a gate node with `verification: []` is rejected ("G01 requires at least one seam-bound verification entry"); the happy-path graph carries `G01` (gate, integration_gate, one seam-bound entry) through validation, approval, and materialization. |
| P2 inherited acceptance not frozen | `MaterializedInstructionPack` resolves a single-requirement node's acceptance criteria into `content.acceptance` before hashing/persistence (an authored node acceptance still wins; a multi-requirement v1 node without one stays empty since v2 rejects that shape). Regressions: `internal/tip/tip_test.go` asserts resolution and the composed-node empty case; `TestDecompositionProjectionMaterialization` claims the T01 child after authorization and parses the persisted active pack, asserting `content.acceptance` equals REQ-001's criteria. |

Verified after the fixes: `cd go-pic && go test ./... -count=1` green, `cd pi-ext && pnpm test && pnpm run check && pnpm lint` green (278 tests).

## Review Response (2026-08-29, second pass)

**P1 — rejected checkpoints could become v2 planning authority.** Every
authority-selection query over `workflow_checkpoints` now filters
`decision_type` through `approvedCheckpointDecision(stage)` — `accepted` for
Scan, `approved` for every other planning stage. The newest APPROVED checkpoint
of a stage is the authority; a newer rejected checkpoint is transparent.

Predicate sites: Contract→Blueprint binding (validateContractPolicyBinding),
Contract authority in `validateTaskGraphObligations` and the policy-v2
obligation map, Blueprint seam authority (`approvedBlueprintSeams`), Task
Graph→Contract source binding, materialization's approved-graph selection and
the graph revision flow, the RRI finalization scan/rri gates, the generic
predecessor count in `approveWorkItemArtifactTx`, both workflow-status
checkpoint lookups (aggregate hash-bound join and standalone status), the
pipeline claim gate for planning stages, and the instruction-pack freeze's
direct-checkpoint branch in `internal/tip`. Display and enumeration queries
(`pic show`, planning-reset dry run, revision re-binding) intentionally list
rejected checkpoints and stay unfiltered.

Regression (`TestRejectedCheckpointsDoNotSupplyPlanningAuthority`) covers the
intended policy — **continue using the last approved checkpoint**: with an
approved Contract revision 1 coexisting beside a newer rejected revision 2, the
v2 graph still validates, approves, and materializes against revision 1; a
rejected newer Task Graph checkpoint re-materializes nothing and gains zero
materialization mappings; with ONLY a rejected Blueprint checkpoint, a v2
Contract save fails closed ("requires an approved Blueprint on the same
planning lineage") and the contracts stage refuses to clear its predecessor
gate ("Previous stage blueprint is not approved"); the predecessor gate
continues to clear on the last approved checkpoint when a newer revision was
rejected. Each assertion flips under the unfiltered queries (the binding picks
the newest row, the mapping count lands on the rejected checkpoint, the
fail-closed saves succeed).

Two pre-existing test fixtures seeded Scan checkpoints with
`decision_type='approved'` — Scan's owner decision is `accepted`, and the old
unfiltered queries masked the mistake; the fixtures are corrected and the
predicate is what surfaced them.

Verification after the fix: `env -u PI_TASK_AGENT_NAME go test ./cmd/pic -run
'TestTaskGraph|TestDecomposition|Test.*Policy|Test.*Lineage|TestRejected|...'
-count=1` green, full `go test ./... -count=1` green, `pnpm test && pnpm run
check && pnpm lint` green (278 tests).
