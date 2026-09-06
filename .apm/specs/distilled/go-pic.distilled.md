# Distilled: go-pic module (pic Go CLI)

> Distilled by `/apm distill @go-pic` on 2026-09-05. Reference artifact —
> records what the code currently does. Not a `.plan.md`, `.feature`, or
> requirement source; enter the pipeline via `/apm spec` citing this as
> `Context:`.
>
> Evidence tags: `[confirmed: test <name>]` = a Go test proves the rule;
> `[assumed: code]` = only code implies it. Test evidence is based on test
> names in `cmd/pic/*_test.go` and `internal/tip/tip_test.go` (134 test
> functions); individual assertions were not all read.

## Surface

```
Analyzed: 9,497 LOC (non-test Go source, 14 files)
├── cmd/pic/work_items.go      4,487 — Work Item lifecycle, planning artifacts, aggregates
├── cmd/pic/pipeline.go        1,005 — pipeline runs, leases, circuit breakers, escalations
├── cmd/pic/misc.go              637 — activity, search, markdown, web dashboard commands
├── cmd/pic/main.go              617 — command dispatch, projects, DB discovery, registry
├── cmd/pic/schema_bootstrap.go  582 — canonical + legacy schema statements, migrations
├── internal/tip/tip.go          649 — Task Instruction Pack domain (TIP parse/render/activate)
├── cmd/pic/acceptance.go        433 — Gherkin validation + reusable-profile promotion gate
├── cmd/pic/workflow_schema.go   290 — legacy epic/task schema SQL
├── cmd/pic/next_actions.go      240 — transition-oracle hints + batched checkpoint decisions
├── cmd/pic/workflow.go          188 — workflow subcommand dispatch, actor guard
├── cmd/pic/workflow_*, profile, core, misc helpers — remaining surface
├── Boilerplate dropped (imports, JSON plumbing, option parsing): ~1,600
├── Internal implementation dropped (SQL row plumbing, renderer text): ~5,900
├── Duplication dropped (patch-artifact save repeated for integrated/artifact_saved,
│   dry-run cascade previews): ~1,100
└── Distilled result: ~460 lines (ratio ≈ 21:1, within 20% cap)
```

Tests consulted as behavior evidence: 7,547 LOC across 10 test files
(134 test functions). External dependency: Go stdlib + `modernc.org/sqlite`
(pure-Go SQLite driver) only.

## Business Rules

### Identity, storage, discovery

1. All durable state lives in a project-local SQLite DB at
   `<root>/.pi/tasks.db`; files under `.apm/` are discovery artifacts only.
   [assumed: code — main.go cmdInit, findDB]
2. DB discovery walks up from CWD to the nearest `.pi/tasks.db`; failing
   that, it resolves the git common dir and checks one level above it, so
   git worktrees share the parent repo's DB. [confirmed: test
   TestFindDBFromGitWorktree]
3. Every `openDB()` re-runs `initDB` (idempotent migrations) before use.
   [assumed: code — core.go openDB]
4. SQLite is opened with `foreign_keys(1)`, `busy_timeout(5000)`; WAL and
   `foreign_keys=ON` pragmas run at init and are deliberately kept outside
   versioned migrations. Max 1 open connection. [assumed: code — main.go,
   core.go]
5. Projects are registered in a global registry at
   `~/.pi/task-system/projects.json` supporting both camelCase and
   snake_case JSON keys (legacy parity). Registry match is by ID or
   realpath-resolved root. [confirmed: test TestLegacyProjectRegistryParity,
   TestWebAPIUsesGlobalProjectRegistry]
6. `project scan` walks the tree to a default depth of 5, skipping dotfiles,
   `_`-prefixed dirs, and {node_modules, dist, build, coverage, venv,
   __pycache__}, and stops descending at each found project. [assumed: code]
7. IDs are 8-hex random chars with type prefixes: `wi-` (work item),
   `wia-` (artifact), `wic-` (checkpoint), `wip-` (pack), `pr-` (pipeline
   run), `req-`, `od-`, `wiod-`, `wivr-`, `wicr-`, `wir-`, `wiauth-`,
   `wies-` (escalation). `shortID()` falls back to a time-hash when the CSPRNG
   fails. [assumed: code]
8. All CLI output is single-line JSON on stdout; errors are JSON on stderr
   with exit code 1. [assumed: code — main.go]

### Actor authority (workflow guard)

9. When `PI_TASK_AGENT_NAME` is set, agents may only call read-only
   work-item commands (`list`, `show`, `artifact-save`, `workflow-status`,
   `graph-validate`) and whitelisted read-only workflow subcommands
   (`instruction-pack-render`, `instruction-packs`, `verifications`,
   `events`, `pipeline-runs`, `pipeline-group`,
   `profile-list`, `profile-promotion-evaluate`); any other lifecycle
   mutation is rejected. [assumed: code — work_items.go, workflow.go]
10. Owner-role mutations (`accept`, `aggregate-accept`, `authorize`,
    `planning-reset`, `planning-amend`, `execution-reset`,
    `pipeline-circuit-reset`, `review-decision`) and contractor-role
    mutations (`verification-save`, `aggregate-verify`,
    `escalation-resolve`, `rri-finalize`) validate `--actor-role` exactly,
    and additionally fail if `PI_TASK_AGENT_NAME` is set — child agents can
    never assume owner/contractor workflow authority. [confirmed: test
    TestWorkItemRriFinalizeActorRole; assumed: code elsewhere]

### Work Items, relations, readiness

11. Work Item `type` ∈ {epic, feature, task, bug, chore, gate}; `status` ∈
    {open, in_progress, done, cancelled}; `priority` ∈ {low, medium, high};
    `planning_depth` ∈ {quick, standard, designed, full}. [confirmed: code
    CHECK constraints]
12. Only epic/feature may have children; setting a parent is cycle-checked
    over the ancestor chain. [confirmed: test
    TestWorkItemCRUDAndContainment]
13. Relations are `blocks`, `gates`, or `related`; a `gates` relation
    requires the related item to be type `gate`. `blocks`/`gates` insertions
    are cycle-checked transitively; `related` is not. [confirmed: test
    TestWorkItemRelateControlsReadinessByRelationType,
    TestReadinessRelationsRejectTransitiveCycle]
14. Readiness (claimable) requires: executable type (task/bug/chore),
    status `open`, not deferred, no claim, exactly one active instruction
    pack OR an unrevoked implementation authorization rooted at the
    materialization checkpoint, and no unfinished `blocks` or `gates`
    blocker. [confirmed: test TestWorkItemReadinessAndClaim,
    TestStandaloneWorkItemGeneratesTIPBeforeFirstClaim,
    TestMaterializedChildClaimRequiresCurrentParentAuthorization]
15. Claiming writes `claimed_at`/`claimed_by` atomically inside the ready
    predicate (compare-and-set); not-ready items are rejected, not queued.
    [confirmed: test TestWorkItemReadinessAndClaim]
16. `status done` on a managed executable item requires a passed review,
    current integrated completion report, and passed contractor
    verification. Terminal status (`done`/`cancelled`) cannot transition to
    anything else — reopening requires a new TIP generation via
    planning/execution reset. [confirmed: test
    TestExecutableWorkItemLifecycleUsesTIPAndGuardedClosure]
17. `status in_progress` on a managed item requires exactly one active
    mutation claim (a `claimed`/`running` worker/autofix pipeline run).
    [confirmed: test TestPipelineClaimBindsCanonicalWorkItemTIP]
18. Cancelling a Work Item cancels all its active pipeline runs (direct and
    recursive descendants) and cancels all non-terminal descendants.
    [confirmed: test TestCancellationRevokesPipelineLease,
    TestEpicCancellationCascadesToActiveDescendants]
19. A failed worker/autofix/escalation/expired lease releases the item's
    claim (back to `open`) only when no other active run remains.
    [confirmed: test TestExpiredWorkerLeaseReopensWorkItem]
20. Labels match `^[a-z0-9][a-z0-9._:/-]{0,254}$`, are deduplicated, and a
    child inherits its parent's labels at creation. [confirmed: test
    TestWorkItemLabels]

### Planning stages, artifacts, checkpoints

21. Planning stages in order: `scan`, `rri`, `rri_t_scenarios`, `vision`,
    `blueprint`, `contracts`, `task_graph`. The Plan profile per depth:
    `full` = all but rri_t_scenarios; `designed` = scan, rri, blueprint,
    task_graph; `quick`/`standard` = scan, rri, task_graph; standalone
    executables are always fixed to the lean profile regardless of depth.
    [confirmed: test TestPlanningDepthSelectsAggregateStages,
    TestPlanningDepthStandaloneIsFixedLeanProfile]
22. Artifacts are append-only per stage (revision = max+1, content hashed
    with `sha256:` over canonical JSON). DB triggers abort UPDATE on
    artifacts, pack identity/content updates, and mutation of contract-keyed
    requirement content. Approved artifacts also refuse DELETE.
    [confirmed: code — schema triggers; test TestWorkItemSchemaMigration]
23. Saving an artifact at stage N deletes that stage's and all later
    stages' checkpoints (the lineage downstream is invalidated), but never
    checkpoints that anchor instruction packs, materializations, or
    authorizations. [confirmed: test TestTIPRevisionInvalidatesActiveExecution;
    assumed: code — artifact-save cleanup predicate]
24. A stage may only be approved after its predecessor checkpoint carries
    the owner decision; Scan uses decision `accepted`, every other stage
    `approved`. A newer *rejected* checkpoint never supplies planning
    authority — the newest APPROVED checkpoint wins. [confirmed: test
    TestWorkItemArtifactGateSequence, TestRriStageOrder,
    TestRejectedCheckpointsDoNotSupplyPlanningAuthority]
25. `checkpoint-decide` records multiple stage decisions in one transaction,
    processed in profile order, reusing the same per-stage approval core;
    all-or-nothing. [confirmed: test
    TestWorkflowStatusNextActionsAndCheckpointDecide]
26. Vision artifacts require full structured JSON: project name, all six
    nature/dimension fields, architecture lists, complete user flows, tech
    stack rows with rationale/reuse, and a UI or non-UI direction.
    [confirmed: test TestVisionArtifactRequiresStructuredJSON]
27. Blueprint artifacts: policy marker ∈ {0,1,2}; v2 requires ≥1 verification
    seam (unique ids, non-empty surface/isolates) and retires
    `task_decomposition_preview`; v1/v0 require a complete
    task_decomposition_preview. The `schema_version: 2.1` marker (additive on
    v2) additionally requires `implementation_decisions` (non-empty array of
    {decision, rationale, alternatives_considered[]}), `excluded_keys`,
    `deferrals`, `not_yet_specified`, `out_of_scope`, `adr_candidates`
    arrays with non-empty-string row fields, and forbids retired
    `user_stories`/`testing` sections. [confirmed: test
    TestBlueprintArtifactPolicySchemas, TestBlueprintV21ShapeParity]
28. A v2.1 Blueprint's `excluded_keys` must each match an exclusion key in
    the newest approved RRI's `out_of_scope` on the same lineage; re-checked
    at approval time so a newer RRI cannot leave stale references.
    [confirmed: test TestBlueprintExcludedKeysBinding]
29. Contract artifacts: policy ∈ {0,1,2}; v2 obligations require unique ids,
    a primary `class` from {user_behavior, data_invariant,
    interface_contract, security, migration_rule, operational_rule,
    integration_gate}, a Blueprint-declared `seam`, and Gherkin
    Given/When/Then acceptance; v2 must bind `source_blueprint`
    (id+revision+content-hash) to the exact approved Blueprint on the same
    lineage. [confirmed: test TestContractArtifactPolicySchemas,
    TestDecompositionPolicyApprovalChain]
30. Blueprint review annotation dispositions (`{annotation,
    resolution: addressed|waived, evidence}`) are validated before any
    transaction opens and committed atomically with the Blueprint approval
    checkpoint. [confirmed: test TestBlueprintAnnotationEvidence]

### RRI (requirements and decisions)

31. RRI finalization requires: exactly one approved Scan checkpoint, valid
    JSON payload with non-empty requirements/decisions, requirement
    priority ∈ {tier1,tier2,tier3}, Gherkin acceptance criteria, unique
    keys, and contractor actor role. [confirmed: test
    TestWorkItemRriFinalizePersistsCanonicalInterview]
32. A prior unapproved finalization may be revised (artifact revision
    bumped, previous requirements/decisions deleted and reinserted) — but
    once approved, revision is blocked until a planning reset.
    [confirmed: test TestWorkItemRriFinalizePersistsCanonicalInterview,
    TestRriStageOrder]
33. The `rri_policy_version` marker gates schema strictness: version ≤2
    only; ≥2 reports must carry every core array (requirements_matrix,
    auto_answered, decisions_log, open_questions) and both scope sections
    (`not_yet_specified`, `out_of_scope`) even when empty; open_questions
    rows require status ∈ {open, resolved, deferred}, priority ∈
    {P0..P3}, mode ∈ {afk, hitl}, `blocks`, and well-formed resolutions
    (resolved/deferred require non-empty answer+source). Pre-marker reports
    keep legacy tolerance. [confirmed: test TestRriReportValidationMarkerGate,
    TestRriOpenQuestionFieldTypesRejectedByUnmarshal, TestRriScopeSections,
    TestWorkItemRriFinalizeMarkedEmptyScopeSectionsPersist,
    TestWorkItemRriFinalizeRejectsMalformedMarkedRows]
34. Publish gate: for marked (v2) reports, an open P0/P1 question blocks
    publication; a deferred P0/P1 question requires a non-empty owner
    deferral reason. Each deferred P0/P1 is persisted as an owner decision
    row (`decision='deferred'`). [confirmed: test
    TestRriPublishGateBlocksOpenP0P1Questions, TestRriDeferralReasonPersistence]
35. Terminology guard: marked RRI payloads fail closed when requirements or
    decisions use terms that contradict repository truth — `_Avoid_:`
    phrases in `CONTEXT.md` glossary entries, or explicit rejected practices
    parsed from `docs/adr/*.md` ADRs with `**Status**: accepted` (≥2-word
    phrases only). No truth root found → check skipped; truth unreadable →
    fail closed. Report text persists verbatim (case/whitespace-normalized
    matching only). [confirmed: test TestRriGlossaryConflictFailsClosed,
    TestRriGlossaryDecisionConflictFailsClosed, TestRriAdrConflictFailsClosed,
    TestRriGlossaryReportTextPersistsUnchanged, TestRriGlossaryParsesRepositoryTruth]
36. Approved RRI glossary updates are the sole CONTEXT.md writer: applied
    only at owner RRI approval (never interview checkpointing), via
    temp-file rename, resolved through the truth root (nested CWD safe),
    with a restore closure compensating a commit failure; a compensation
    failure surfaces alongside the rollback error. [confirmed: test
    TestRriGlossaryApproval, TestRriGlossaryApprovalFromNestedDirectory,
    TestRriGlossaryApprovalCompensationFailure]

### Task Graph and materialization

37. Task plans parse from exactly one ```task-plan-json fence; version
    1–3; unique node keys; known node types (default `task`); non-feature
    parents must be features; dependency cycles rejected; executable nodes
    require complete pack content (goal, files, all six rule arrays,
    constraints) and ≥1 requirement key; schema ≥2 requires skillFamilies
    (may be `[]`). [confirmed: test TestTaskPlanRejectsDependencyCycle,
    TestTaskPlanV2RequiresExplicitSkillFamilies, TestApprovedTaskGraphValidation]
38. Every requirement (non-deferred, including inherited-from-root for
    materialized children) must be covered by task-graph nodes; a node may
    carry at most 2 requirement keys; requirement acceptance criteria must
    be Gherkin. [confirmed: test
    TestTaskGraphValidationRejectsMissingRequirements,
    TestMaterializedChildInheritsParentRequirements]
39. Policy-v2 graphs require the planning profile to include blueprint and
    contracts stages, bind `source_contract` to the exact approved Contract
    lineage, bind node verification gates to Blueprint-declared seams,
    default `decomposition_mode` to `vertical` (any other mode needs
    `exception_reason`), require `depends_on_rationale` per edge, and
    require seam-bound verification entries (seam + requirement/obligation
    keys + command + expected) on executable leaves. Unsupported policy
    versions fail closed. [confirmed: test
    TestDecompositionPolicyApprovalChain, TestDecompositionPolicyV1Unchanged,
    TestVerificationGateParsing]
40. Mode-specific rules: `wide_refactor` needs a `paired_contract_node`
    that depends on it; `shared_contract` must have a downstream consumer;
    `integration_gate` must list obligations/requirements it verifies.
    [confirmed: test TestDecompositionPolicyApprovalChain]
41. Contract obligation binding: each obligation referenced by nodes must
    exist in the approved Contract; exactly one node may `provide` each
    obligation; every obligation needs an evidence node; consumers must
    depend on their providers. Verification-only graphs (gate-only,
    retrospective aggregates) must `evidence_for` every obligation at a
    gate node instead. [confirmed: test TestVerificationOnlyGraphEvidence]
42. Materialization re-runs full approval-time validation (including
    decomposition policy), is idempotent per checkpoint (existing node →
    reuse, else create), reuses a prior checkpoint's mapping for the same
    node key, maps node priorities P0/P1/P2 → high/medium/low, untyped
    nodes → task, and projects depends_on edges as `blocks` relations with
    rationale. Standalone executables materialize as themselves (a single
    self-mapping row). [confirmed: test TestWorkItemGraphMaterialization,
    TestStandaloneMaterialization, TestDecompositionProjectionMaterialization]
43. Implementation authorization (owner-only) requires an approved,
    materialized task graph with full requirement coverage; authorizes one
    checkpoint at a time (previous authorizations revoked), stales active
    packs from other checkpoints, and binds delivery mode: `branch` vs
    `coordination` (labels `integration:branch`/`integration:coordination`,
    kind features or aggregate executables default branch; an ancestor
    already owning a branch forces coordination and forbids a nested
    branch-owning label; branch requires non-base branch name ≠ HEAD/base,
    plus base commit). [confirmed: test TestImplementationAuthorization,
    TestAggregateDeliveryLifecycle]

### Instruction packs (TIP)

44. Exactly one active pack per Work Item (partial unique index).
    Activation cancels all active pipeline runs for the item, stales the
    previous active pack, and resets the item to `open`/unclaimed.
    [confirmed: test TestInstructionPackLineage, TestTIPRevisionInvalidatesActiveExecution]
45. Pack content: goal, files, business_rules, validation_rules,
    error_handling, state_transitions, contract_obligations, verification,
    constraints all required; schema ≥2 verification gates require an
    exact `command`, boolean-able `required`, string-list
    `expected_writes`, and `requires` prerequisites with `setup_commands`.
    [confirmed: test TestValidateInstructionPackVerificationContract,
    TestInstructionPackRendersContractInterfaces]
46. Requirement snapshots are frozen into the pack at save/first-claim time
    (id, key, title, description, acceptance criteria + source hash);
    acceptance is frozen: node-authored acceptance travels verbatim, a
    single-requirement node resolves its acceptance from the snapshot.
    [confirmed: test TestMaterializedPackAcceptanceFreeze,
    TestInstructionPackLineage]
47. The worker-facing render is deterministic: header with pack
    id/version/hash, contract interfaces (provides/consumes/evidence_for/
    obligation_keys) when present, effective acceptance, snapshots sorted
    by key. [confirmed: test TestInstructionPackRendersContractInterfaces]
48. Legacy `task_instruction_packs` (draft/active/stale/superseded) are
    migrated once into `work_item_instruction_packs` via a synthetic
    checkpoint (`wic-migrated-tip-…`). [confirmed: test
    TestWorkflowMigrationPreservesLegacyRows]

### Pipeline runs, leases, circuit breakers

49. Pipeline stages: scan, rri, vision, blueprint, contracts, task_graph,
    worker, review, autofix; run statuses: claimed, running, completed,
    failed, blocked, cancelled, expired; unique (task, stage, attempt) and
    at most one active run per (task, stage) via partial unique index.
    [confirmed: test TestPipelineSchemaMigrationPreservesDependentObjects]
50. Plan/Implement/QA lifecycle profiles are resolved and persisted exactly
    once at the first claim (plan = planning stages; implement = worker;
    qa = review+autofix); claims validate optional `--profile-version` /
    `--profile-hash` against the persisted profile. [confirmed: test
    TestProfileResolutionPersistsExactlyOnce, TestPipelineStageProfileBinding,
    TestPipelineStageProfileMismatchRejected]
51. Planning claims gate on owner-decided checkpoints: earlier profile
    stages must be approved, the current stage must not already be
    approved. [confirmed: test TestPipelineStageInvalidPredecessorRejected]
52. Execution claims (worker/review/autofix) require an active pack bound
    to the authorized parent materialization, and reject when a newer
    task-graph artifact exists without approval. A materialized child
    claims only under a live parent authorization. [confirmed: test
    TestMaterializedChildClaimRequiresCurrentParentAuthorization,
    TestPipelineClaimBindsCanonicalWorkItemTIP]
53. Leases: default 3600s at claim (positive integer required), renewal
    extends +4h, bind promotes claimed→running with a subagent run id;
    renew/bind/model/complete all compare-and-set on (id, lease_token,
    unexpired lease). [confirmed: test TestExpiredWorkerLeaseReopensWorkItem;
    assumed: code — bind/renew predicates]
54. Review claims require a completed worker/autofix candidate for the
    active pack with saved + integrated patch hashes; retry allowed on a
    reopened candidate. [confirmed: test
    TestPipelineReviewClaimAcceptsInProgressCandidate,
    TestPipelineReviewRetryAcceptsReopenedCandidate]
55. Review verdicts bind the exact candidate: `work-item review` only
    updates a Work Item when the supplied `--pipeline-run-id` is a
    completed review run matching the newest completed candidate's pack
    lineage and patch hash. Stale verdicts are rejected. [confirmed: test
    TestCurrentExecutionRejectsStaleReviewVerdict,
    TestCanonicalWorkItemReviewAndCompletionEvidence]
56. Automatic worker retries: max 3 attempts per unchanged pack (attempts
    with no output evidence — transient provider deaths — do not count);
    `environment_blocked`/`runner_protocol_invalid` failures block
    automatic retry unless the environment fingerprint changed or
    `--explicit-retry 1`; deterministic failures (worker_output_invalid,
    worker_artifact_invalid, scheduler_owner_lost) break the circuit after
    1 failure per unchanged contract. [confirmed: test
    TestTransientWorkerDeathsDoNotExhaustUnchangedPackRetryLimit,
    TestPipelineCircuitResetRestoresCanonicalRunnerRetry]
57. Autofix: max 3 completed/blocked attempts per unchanged pack; then
    owner action is required. [confirmed: test TestProfilePipeline*
    (autofix limits); assumed: code — maxAutomaticAutofixAttempts]
58. Review-fix loop: requires a completed failed review with a bound
    rejected candidate; max 3 review-fix cycles per unchanged pack; an
    unchanged rejected candidate (no progress) trips a circuit breaker;
    owner events (pipeline_circuit_reset, owner_rejected_completion,
    owner_review_decision) reset the attempt baselines via
    `after_attempt`. [confirmed: test TestReviewFixCapPersistsBlockedOwnerAction,
    TestPipelineCircuitResetClearsAutomaticWorkerRetryLimit]
59. Circuit reset (owner-only) requires: reason, change-type ∈
    {contract, environment, runner, artifact}, non-empty evidence JSON with
    `changed_fingerprint`, a terminal worker attempt, and a
    changed-fingerprint different from the last reset; works with zero
    active packs (falls back to latest pack) because a failed claim rolls
    back its generated TIP. [confirmed: test
    TestPipelineCircuitResetRestoresCanonicalRunnerRetry,
    TestPipelineCircuitResetWorksWithoutActivePack]
60. Escalations: worker saves one structured report (level L2/L3,
    non-empty `checked_sources`) bound to its active-TIP lineage; the run
    is blocked and the claim released atomically; any open escalation
    blocks all further claims until contractor resolution (exact wies-* id).
    [confirmed: test TestWorkItemEscalationLifecycle]
61. `pipeline-checkpoint`: `artifact_saved`/`integrated` set timestamps
    only for completed worker/autofix runs; `integrated` additionally
    requires a passed review verdict matching the candidate patch hash.
    Patches are stored hashed (sha256, content-addressed filename) in a
    0700 `review-patches` dir beside the DB via atomic temp-file rename.
    `advanced` is reconciliation metadata only and never carries
    authority. [assumed: code — pipeline.go; partially covered by
    TestCanonicalWorkItemReviewAndCompletionEvidence]
62. `pipeline-complete` may transition completed→blocked only (integration
    can fail after child completion); no other terminal state mutates a
    terminal run. [confirmed: test TestCancellationRevokesPipelineLease;
    assumed: code — currentStatuses predicate]
63. `pipeline-pending` sweeps stale generations: marks terminal runs
    `advanced` when their item is done/cancelled, their pack lineage no
    longer matches the active pack, or a newer terminal run exists.
    [confirmed: test TestPipelinePendingRetiresStaleGenerations]
64. `review-fix-block` elevates a failed review to
    `owner_approval_required=true` + `review_fix_round_cap` durably;
    `review-decision fix` (owner) clears the flag and appends an
    owner_review_decision event that resets cycle counters.
    [confirmed: test TestReviewFixCapPersistsBlockedOwnerAction,
    TestReviewDecisionFixClearsOwnerApprovalBlock]

### Completion, verification, aggregates

65. Completion reports (done/partial/blocked) require a pipeline run bound
    to the active pack lineage; `done` additionally requires integrated
    patch evidence. [confirmed: test
    TestCanonicalWorkItemReviewAndCompletionEvidence]
66. Contractor verification requires: the current integrated completion
    report, a passed review for it, and `--actor-role contractor`; records
    a `pipeline_high_water_rowid` so later pipeline activity invalidates
    the verification. Passed → item done; anything else → item reopened.
    [confirmed: test TestVerificationAfterAllPipelineActivityAnchorsCompletionReport,
    TestCurrentExecutionRejectsStaleReviewVerdict]
67. Owner acceptance applies only to aggregate Work Items (epic/feature);
    executable children close on passed contractor verification alone. One
    owner decision per completion report; a corrected completion report is
    required after a rejection. Rejection reopens the item and records an
    owner_rejected_completion event with `after_attempt`.
    [confirmed: test TestExecutableOwnerAcceptanceIsRemoved,
    TestExecutableWorkItemLifecycleUsesTIPAndGuardedClosure]
68. Aggregate verification (contractor): verifies all descendants are
    done/cancelled and no unmet requirements (a passed verdict marks
    requirements satisfied); non-passed verdicts auto-schedule exactly one
    corrective Bug (type bug, high priority) with a tier1 CORRECTIVE-*
    requirement, linked and deduplicated on the aggregate's unresolved
    corrective Bug; `failed` verdicts require owner approval before
    scheduling, `partial`/`blocked` do not. Optional RRI-T evidence:
    scenarios keyed by dimension|stress_axis|requirement|id (dimensions
    D1–D7, 8 stress axes, results PASS/ACCEPTABLE/PAINFUL/FAIL), deduped
    by that identity; aggregate `passed` requires every scenario PASS.
    [confirmed: test TestAggregateWorkItemVerificationAndClosure,
    TestFailedAggregateVerificationCreatesCorrectiveBug,
    TestCorrectiveExactlyOnce, TestCorrectiveRetryDedup, TestRriTScenarioIdentityContract]
69. Branch-mode aggregates additionally verify with the bound branch name +
    head commit + current base commit; the delivery state anchors
    verified_head. [confirmed: test TestAggregateDeliveryLifecycle]
70. Aggregate acceptance (owner) requires: the current passed verification,
    bound to the current task-graph checkpoint and current delivery state,
    unchanged verified head/base (branch mode), and no prior decision for
    the report. Accepted coordination → done; accepted branch →
    merge_pending; rejected → open + blocked merge. [confirmed: test
    TestAggregateWorkItemVerificationAndClosure,
    TestAggregateDeliveryLifecycle]
71. Merge result (merged/blocked) must match the verified delivery head and
    current owner acceptance; `merged` closes the aggregate (`done`);
    `blocked` records the error. Aggregate close additionally requires
    merge_status `merged` for branch mode. [confirmed: test
    TestAggregateDeliveryLifecycle]

### Owner re-scope, amendment, resets

72. Planning reset (owner): requires non-terminal item, no active pipeline
    runs in the subtree; deletes the entire planning lineage (artifacts,
    checkpoints, requirements, decisions, runs) and — when the graph was
    materialized — also the materialized child Work Items, authorizations,
    and materialization rows; records a planning_reset event. `--dry-run`
    enumerates the exact blast radius (per-table cascade rows, second-order
    corrective bugs) without mutating anything. [confirmed: test
    TestPlanningResetInvalidatesApprovedLineageForRescan,
    TestPlanningResetDryRunDoesNotMutate]
73. Planning amendment (owner, bounded): exact old→new string substitutions
    across the approved lineage only; requires a non-empty reason,
    old≠new, no active runs; contract-keyed requirements containing a
    target are immutable → replan instead; verification evidence whose
    surface text intersects a substitution is retired as `blocked` history
    (non-intersecting evidence survives); touched artifact stages get new
    revisions bound to the same checkpoints; packs whose content contains
    a target are staled; a no-op amendment is an error. [confirmed: test
    TestWorkItemPlanningAmendment,
    TestWorkItemPlanningAmendmentRetiresIntersectingEvidenceOnly]
74. Execution reset (owner): materialized children only; revokes the
    authorization, stales active packs, reopens the item; Task Graph and
    siblings preserved. [confirmed: test TestInstructionPackLineage; assumed:
    code — workItemExecutionReset]
75. Deviation approval (owner): approved deferral of pack-listed
    requirements (existing approved defers are idempotently skipped);
    revokes execution authority and stales the active pack so re-execution
    requires renewal. [assumed: code — workItemApproveDeviations]
76. Scan rejection (contractor): records a scan_report_rejected event;
    `scan-rejection` reports the latest rejection/reset event.
    [assumed: code — workItemScanReject]

### Transition oracle

77. `workflow-status` is the single transition authority: it reports the
    next stage plus structured `next_actions` (stable IDs, argument
    templates, actor binding) and gate rejections cite the same hints; the
    `done` stage yields no hints. Task-graph approval additionally surfaces
    the five fixed granularity checkpoint questions. Aggregate delivery
    states drive their own next_stage (implement → aggregate_verification →
    owner_acceptance → merge_pending/done). [confirmed: test
    TestWorkflowStatusNextActionsAndCheckpointDecide,
    TestAggregateDeliveryLifecycle]

### Reusable profile promotion

78. Promotion eligibility (read-only evaluation) hard-rejects unless: the
    evidence references the exact candidate profile id+hash; every included
    stage has current (≤48h, non-superseded) success-path (outcome
    `passed`) AND failure-path (partial/blocked/failed) run evidence bound
    to immutable run ids; corrective evidence covers passed/partial/
    blocked/failed with exactly one linked Bug; every registered REQ-*
    invariant has red, green, and review ids; and a full intake-to-merge
    lifecycle — which is always reconciled against real persisted
    verification/acceptance/merge rows, never taken from the payload —
    exists, is passed, and is not merge-blocked. [confirmed: test
    TestPromotionGate, TestPromotionGateProductionEntrypoint]

### Behavioral Gherkin contract

79. Acceptance criteria and node-level acceptance require Given, When, and
    Then steps; task descriptions combine with their requirements into
    Feature/Scenario behavioral Gherkin (each scenario complete).
    [confirmed: test TestInstructionPackRendersContractInterfaces (gherkin
    helpers exercised); assumed: code — validateGherkinSteps,
    validateBehavioralGherkin]

### Web dashboard & misc

80. `web` serves a static dashboard from `go-pic/web/build` (asset dir
    configurable), exposing a read-mostly JSON API backed by the global
    registry. [confirmed: test TestWebAPISupportsDashboardContract,
    TestDashboardBuildDirUsesConfiguredAssets]
81. Legacy epic/task command surfaces remain read-compatible through
    migration shims; new Work Item schema is canonical. [confirmed: test
    TestLegacySupportParity*, TestSchemaMigrationsVersioned]

## Scenarios

### Feature: Work Item execution lifecycle

```gherkin
Scenario: Managed executable reaches done only through guarded closure
  Given a task/bug/chore Work Item with exactly one active instruction pack
  When a worker run claims, completes, and integrates a patch
  And a review run passes that exact candidate patch hash
  And a contractor verification report is recorded for the integrated completion
  Then the Work Item is closed as done with review_status passed
  And any later pipeline activity anchors the verification stale via the
    recorded pipeline_high_water_rowid

Scenario: Terminal state is sticky
  Given a Work Item whose status is done or cancelled
  When any other status is requested
  Then the transition is rejected with "completed or cancelled managed work
    requires a new TIP generation before reopening"

Scenario: Child claims only under a live parent authorization
  Given a materialized child Work Item under an authorized task graph
  When the parent's implementation authorization is revoked or a newer
    task-graph artifact exists unapproved
  Then the pipeline claim is rejected
```

### Feature: Planning lineage

```gherkin
Scenario: Stage ordering enforced at both claim and approve
  Given a Plan profile [scan, rri, task_graph]
  When stage task_graph is approved while rri has no owner approval
  Then the approval is rejected and the gate names the next valid action
  And a rejected newer checkpoint never satisfies (or blocks) the gate

Scenario: Artifact save invalidates downstream approvals
  Given approved checkpoints at scan and rri
  When a new scan artifact revision is saved
  Then the scan and rri checkpoints are deleted unless they anchor packs,
    materializations, or authorizations
  And planning must restart from the revised stage

Scenario: Bounded amendment without a full re-scope
  Given approved planning artifacts containing value "8080"
  And the owner submits substitutions old="8080" new="9090" with a reason
  When no pipeline runs are active
  Then each affected artifact gains a new revision with "8080"→"9090"
  And requirements/decisions text is substituted in place
  And packs containing the old value are staled
  And verification evidence whose text intersected the value is retired
    as blocked with a supersession marker
  And contract-keyed requirements containing the old value abort the amendment
```

### Feature: Pipeline claim safety

```gherkin
Scenario: Open escalation blocks all claims
  Given a Work Item with one open escalation (wies-*)
  When any pipeline stage claims
  Then the claim is rejected listing the blocking escalation IDs

Scenario: Circuit breaker sequence for deterministic failures
  Given one unchanged active instruction pack
  When a worker run fails with failure_code worker_output_invalid
  Then no further automatic worker claim is permitted for that pack
  And the owner must pipeline-circuit-reset with change-type and a
    changed_fingerprint different from the previous reset

Scenario: Transient deaths never exhaust retries
  Given worker failures with no failure_code (provider died mid-run)
  When counting attempts against the unchanged-pack limit of 3
  Then only attempts with output evidence (completion/artifact or a
    classified failure_code) are counted

Scenario: Review-fix loop is capped and owner-gated
  Given three completed review-fix rounds without a passed review
  When the next fix would relaunch
  Then review-fix-block marks the failed review owner_approval_required
  And only the owner's review-decision fix re-opens the loop
```

### Feature: Aggregate delivery

```gherkin
Scenario: Branch aggregate closes only on confirmed merge evidence
  Given a branch-mode aggregate with passed aggregate verification
  And the owner accepted that verification for the verified head+base
  When merge result merged is recorded for the verified head
  Then the aggregate is closed as done
  # But merge result blocked → merge_status blocked, item stays open

Scenario: Failed aggregate verification schedules exactly one corrective bug
  Given an aggregate whose descendant verification fails
  When the aggregate verification report is recorded as failed
  Then one corrective bug Work Item is created with a tier1 CORRECTIVE-*
    requirement and owner approval is required before scheduling
  # And a retry of the ambiguous call reuses the existing bug (no duplicate)
```

### Feature: RRI frontier

```gherkin
Scenario: Publish gate on open P0/P1
  Given a marked (rri_policy_version 2) RRI with an open P1 question
  When the contractor finalizes the RRI
  Then publication is blocked naming the question ID
  # Deferred variant: persists with the owner-recorded reason as an owner decision

Scenario: Glossary conflict fails closed before any write
  Given CONTEXT.md defines a term with an _Avoid_: phrase
  And a finalized requirement title uses that phrase
  When rri-finalize runs with a marked report
  Then the save is blocked, no artifact/requirement/decision/event is written,
    and the report text persists unchanged on a later successful save
```

## Risks Identified

- **Duplicated patch-persist logic** (`pipeline-checkpoint` repeats the
  identical artifact-saved and integrated block verbatim) — drift hazard;
  a fix in one path can silently miss the other. [assumed: code]
- **Dry-run cascade preview duplicates the real DELETE blast radius as a
  hand-maintained 17-entry table** — a new child-owned table would be
  retired by FK cascade but invisible in the preview unless the table is
  updated. [assumed: code — descendantPreviews slice]
- **SQL identifier interpolation** in `applyToColumns` /
  `dryRunCascadeRows` builds table/column names into queries (from
  internal constants, parameterized values only) — safe today because no
  external input reaches those strings, but fragile under refactoring.
  [assumed: code]
- **`contains`/`toInt`/`queryOneMap` duplicated** between package main and
  internal/tip rather than shared. [assumed: code]
- **promotion evidence ages**: the 48h evidence window is a hard-coded
  parameter, not configuration. [assumed: code — acceptance.go]
- **Risky-but-commented design decisions** (documented invariants, not
  hacks): journal_mode persists in the file while foreign_keys must be
  re-enabled per open; ambiguous aggregate-verify retries dedupe on the
  unresolved corrective Bug because fresh random IDs can never match;
  unchanged-pack limiter excludes no-evidence attempts to avoid a
  claim/reset deadlock. [confirmed: code comments + tests]
- **No test was found** for the agent mutation guard (#9), pipeline
  checkpoint patch-atomicity details (#61), or scan rejection (#76) by
  name — these rules are labeled `assumed: code` above.

## Files Analyzed

cmd/pic/: work_items.go, pipeline.go, misc.go, main.go,
schema_bootstrap.go, acceptance.go, workflow_schema.go, next_actions.go,
workflow.go, profile.go, core.go, instruction_packs.go, schema_helpers.go;
internal/tip/: tip.go. Tests consulted: pic_cli_test.go,
profile_lifecycle_integration_test.go, promotion_gate_test.go,
profile_pipeline_test.go, planning_paths_test.go,
legacy_support_parity_test.go, corrective_qa_test.go,
verification_only_graph_test.go, dashboard_assets_test.go,
internal/tip/tip_test.go. Out of scope (separate modules):
go-pic/web (Svelte dashboard), pi-ext (TypeScript extension).
