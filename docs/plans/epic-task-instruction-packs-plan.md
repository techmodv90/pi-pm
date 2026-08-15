# Epic Workflow And Task Instruction Packs Plan

Spec sufficient after the decisions below: Epics become optional first-class feature workflow roots. Standalone Tasks retain their own mode-appropriate workflow. Every Task that reaches Worker has exactly one current active versioned `task_instruction_pack`. An approved Epic Task Plan DAG materializes execution-ready Tasks, Task dependency edges, and packs; standalone workflows activate packs from approved Task-owned artifacts. The pack is the immutable Task-specific execution contract. Source artifacts and Task dependencies remain authoritative, while the pack preserves the exact approved execution snapshot used by Worker and Reviewer.

## Goals

- Run Full Scan → Vision → RRI → Requirements → Design → approval directly on an Epic.
- Keep manual standalone Tasks valid without an Epic.
- Materialize an approved Epic Task Plan into dependency-linked implementation Tasks.
- Generate one immutable/versioned instruction pack for every Task before Worker, whether materialized or standalone.
- Route Worker and Reviewer through the same active pack without raw workflow-artifact handoffs.
- Preserve exact requirement wording and source hashes in each activated pack so one version cannot render differently later.
- Verify and accept assembled features at Epic scope and standalone work at Task scope after every required review passes.

## Non-Goals

- Do not restore the removed legacy TIP schema, commands, lifecycle, or dependency graph.
- Do not require an Epic or materialization row for a standalone Task.
- Do not duplicate Task dependencies inside instruction-pack tables.
- Do not clone Epic requirements into materialized Tasks.
- Do not add child QA, verifier agents, or parent Gate B review.

## Canonical Task Instruction Pack Template

Every active `task_instruction_pack` renders exactly this contract. Header/context values marked as derived are generated from persisted Epic, Task, design, requirement, and dependency records; they are not independently editable pack fields.

```markdown
# TIP-[XXX]: [Task Name]

## HEADER
- TIP-ID: TIP-[XXX]
- Task: [TASK-ID]
- Origin: manual / materialized
- Epic: [EPIC-ID or "None"]
- Task Plan node: [stable node key or "None"]
- Design: [design ID/version or approved "Not required"]
- Requirements: [REQ-IDs]
- Module: [derived bounded module or "Not specified"]
- Depends on: [derived Task/TIP display IDs or "None"]
- Priority: P0 / P1 / P2
- Estimated effort: [minutes or "Not estimated"]

## CONTEXT
- Working directory: current process CWD (authoritative; isolated worktree when provided)
- Source Scan: [artifact ID]
- Source Vision: [event/artifact ID or approved "Not required"]
- Source RRI: [session/report ID or approved "Not required"]
- Approved Design: [design ID/version or approved "Not required"]
- Key files to reference: [task-scoped paths]
- Patterns to follow: [verified file/symbol references]
- Contracts affected: [contract IDs/sections]

## TASK
[One bounded, independently reviewable implementation outcome]

## SPECIFICATIONS
### Business Rules
[Detailed Task-scoped rules approved before pack activation]

### Validation
[Input/field, rule, and required failure behavior]

### Error Handling
[Failure condition, required behavior, and recovery/propagation rule]

### State Transitions
[Only transitions owned by this Task, or approved Not applicable reason]

### Contract Obligations
[Detailed scoped obligations with source contract IDs/sections; no raw contract paste]

## ACCEPTANCE CRITERIA
### REQ-[XXX] — Scenario: [name]
Given [condition]
When [action]
Then [observable result]

## CONSTRAINTS
- Allowed files/modules: [scope]
- Must not change: [boundaries]
- Reuse: [existing helpers/patterns]
- Compatibility/migration constraints: [if applicable]
- Approved deviations: [owner decision IDs or "None"]

## VERIFICATION
- `[focused test command]`
- `[typecheck/lint/build command when applicable]`
- Observable behavior: [expected result]

## REPORT FORMAT
Produce the canonical COMPLETION REPORT and identify this TIP ID/version.
```

Template rules:

- `TIP-[XXX]` is the human display key for the new instruction pack, not a legacy identifier or lifecycle.
- Persist structured section fields and render the Markdown deterministically; do not maintain a second independently edited Markdown source.
- Dependencies are rendered from authoritative `task_dependencies`; no pack dependency graph is persisted.
- Materialized packs link Epic-owned requirements. Standalone packs link requirements owned by that Task. Cross-root links are invalid.
- At activation, persist each linked requirement's exact approved Given/When/Then snapshot and canonical source hash. Source requirements remain authoritative, but the immutable snapshot proves what Worker and Reviewer received.
- Include only Task-scoped rules and citations approved in the Task Plan node or standalone Task workflow; never paste raw Scan, Vision, RRI, Blueprint, or contract documents.
- An active pack's content, snapshots, and links are immutable. Any specification, scope, requirement, design, constraint, or verification change creates a new version. Lifecycle state may transition separately.
- Worker Completion Report and Reviewer evidence must both identify the exact pack ID, version, and content hash used.
- Runtime acceptance remains owned by `pi-subagents`; the TIP and custom Worker do not define or validate `criterion-1` or an `acceptance-report` protocol.

## Approved Task Plan Node Contract

TIP content is authored and approved during Design, not inferred after approval. Every materializable Task Plan node contains:

```task-plan-json
{
  "version": 1,
  "nodes": [
    {
      "key": "T01",
      "name": "Create invoice persistence",
      "goal": "Persist a valid invoice and its line items atomically.",
      "requirement_keys": ["REQ-001", "REQ-004"],
      "depends_on": [],
      "priority": "P0",
      "module": "invoice",
      "estimated_effort_minutes": 90,
      "files": [
        "internal/invoice/service.go",
        "internal/invoice/repository.go",
        "internal/invoice/service_test.go"
      ],
      "patterns": [
        {
          "file": "internal/order/service.go",
          "symbol": "CreateOrder",
          "reason": "Existing transaction-bound aggregate creation pattern"
        }
      ],
      "business_rules": ["Invoice number is unique within an account."],
      "validation_rules": [
        {
          "input": "account_id",
          "rule": "Required and must identify an existing account",
          "failure": "Return the existing account not-found error"
        }
      ],
      "error_handling": [
        {
          "condition": "Line-item insertion fails",
          "behavior": "Roll back the complete transaction",
          "recovery": "Propagate the repository error"
        }
      ],
      "state_transitions": [
        {
          "from": "absent",
          "to": "draft",
          "trigger": "CreateInvoice succeeds"
        }
      ],
      "contract_obligations": [
        {
          "contract_id": "CONTRACT-INVOICE-03",
          "section": "Persistence transaction",
          "obligation": "Commit invoice and line items in one transaction"
        }
      ],
      "constraints": {
        "scope_roots": ["internal/invoice"],
        "must_not_change": ["Invoice issuance behavior", "Payment behavior", "Public API response shape"],
        "required_reuse": ["Existing transaction helper", "Existing invoice-number allocator"],
        "compatibility": ["No database migration in this Task"],
        "approved_deviation_ids": []
      },
      "verification": [
        {
          "command": "go test ./internal/invoice -run TestCreateInvoice",
          "required": true
        },
        {
          "observable_behavior": "Invoice and line items are committed atomically"
        }
      ]
    }
  ]
}
```

The approved Blueprint contains exactly one fenced `task-plan-json` block parsed with Go's standard `encoding/json`; no YAML dependency or ad hoc Markdown parser is introduced. Every section must contain actionable content or `Not applicable: <specific approved reason>`. Owner design approval covers these node-level execution details. Materialization only parses, validates, links, hashes, and persists approved content; it must never ask a model to derive missing business rules, validation, error handling, constraints, or verification after approval.

Standalone Tasks produce the same structured content during their Scan/RRI/Vision/Design workflow. Quick/standard Tasks may use approved `Not applicable` reasons for genuinely irrelevant sections, but no Worker launch bypasses pack activation.

## Persisted Data Shape

### Task

A materialized Task remains a lean lifecycle/display record. It does not copy Epic Scan, Vision, RRI, requirements, Blueprint, contracts, or the instruction text.

```json
{
  "id": "task-invoice-persistence",
  "epic_id": "epic-invoicing",
  "title": "Create invoice persistence",
  "origin": "materialized",
  "priority": "high",
  "status": "open",
  "review_status": "pending"
}
```

Manual standalone Tasks use `origin = "manual"` and may have `epic_id = null`. Existing Tasks migrate to `origin = "manual"` unless an unambiguous existing feature/phase materialization record proves otherwise. A Task attached manually to an Epic is still manual, owns its own workflow artifacts and requirements, and does not inherit Epic design authority; Epic membership alone must never imply materialized behavior.

### Materialization Provenance

`task_materializations` identifies the approved DAG node that authorized the Task:

```json
{
  "task_id": "task-invoice-persistence",
  "epic_id": "epic-invoicing",
  "plan_node_key": "T01",
  "design_id": "design-invoicing-v3",
  "design_version": 3,
  "execution_policy": "strict_sequential",
  "ordinal": 1
}
```

Standalone Tasks have no `task_materializations` row. Their pack provenance records `source_type = standalone_task`, a source Task revision, and an optional approved Task-owned design ID/version.

### Dependencies

`task_dependencies` is the only dependency authority:

```json
{
  "task_id": "task-invoice-api",
  "depends_on_task_id": "task-invoice-persistence",
  "dependency_type": "blocks"
}
```

Instruction packs render current dependency IDs/status for execution but never persist a second dependency graph.

### Task Instruction Pack

`task_instruction_packs` stores structured values that render deterministically into the canonical Markdown template:

```json
{
  "id": "pack-invoice-persistence-v1",
  "display_key": "TIP-001",
  "task_id": "task-invoice-persistence",
  "version": 1,
  "status": "active",
  "source_type": "epic_task_plan",
  "source_task_revision": 1,
  "source_design_id": "design-invoicing-v3",
  "source_design_version": 3,
  "content_hash": "sha256:<canonical-pack-content>",
  "goal": "Persist a valid invoice and its line items atomically.",
  "module": "invoice",
  "estimated_effort_minutes": 90,
  "files_json": [
    "internal/invoice/service.go",
    "internal/invoice/repository.go",
    "internal/invoice/service_test.go"
  ],
  "patterns_json": [
    {
      "file": "internal/order/service.go",
      "symbol": "CreateOrder",
      "reason": "Existing transaction-bound aggregate creation pattern"
    }
  ],
  "business_rules_json": [
    "Invoice number is unique within an account.",
    "An invoice requires at least one line item.",
    "Total equals the sum of normalized line-item amounts.",
    "Draft invoices do not affect account balance."
  ],
  "validation_rules_json": [
    {
      "input": "account_id",
      "rule": "Required and must identify an existing account",
      "failure": "Return the existing account not-found error"
    }
  ],
  "error_handling_json": [
    {
      "condition": "Line-item insertion fails",
      "behavior": "Roll back the complete transaction",
      "recovery": "Propagate the repository error"
    }
  ],
  "state_transitions_json": [
    {
      "from": "absent",
      "to": "draft",
      "trigger": "CreateInvoice succeeds"
    }
  ],
  "contract_obligations_json": [
    {
      "contract_id": "CONTRACT-INVOICE-03",
      "section": "Persistence transaction",
      "obligation": "Commit invoice and line items in one transaction"
    }
  ],
  "constraints_json": {
    "scope_roots": ["internal/invoice"],
    "must_not_change": ["Invoice issuance behavior", "Payment behavior", "Public API response shape"],
    "required_reuse": ["Existing transaction helper", "Existing invoice-number allocator"],
    "compatibility": ["No database migration in this Task"],
    "approved_deviation_ids": []
  },
  "verification_json": [
    {
      "command": "go test ./internal/invoice -run TestCreateInvoice",
      "required": true
    },
    {
      "observable_behavior": "Invoice and line items are committed atomically"
    }
  ],
  "requirement_snapshots_json": [
    {
      "requirement_id": "req-create-invoice",
      "requirement_key": "REQ-001",
      "source_hash": "sha256:<canonical-requirement-content>",
      "scenario": "Create a draft invoice",
      "given": "an existing account and one valid line item",
      "when": "CreateInvoice is called",
      "then": "a draft invoice and its line item are persisted"
    }
  ]
}
```

`instruction_pack_requirement_links` links the source requirements assigned to the pack. Materialized packs accept only requirements owned by their Epic; standalone packs accept only requirements owned by that Task. Activation verifies that every snapshot hash matches its source row. Rendering uses the immutable snapshot, not mutable live requirement text; any later source-hash mismatch marks the active pack stale.

```json
[
  {
    "instruction_pack_id": "pack-invoice-persistence-v1",
    "requirement_id": "req-create-invoice"
  },
  {
    "instruction_pack_id": "pack-invoice-persistence-v1",
    "requirement_id": "req-atomic-persistence"
  }
]
```

### Pack Lifecycle And Integrity

Pack states are `draft`, `active`, `stale`, and `superseded`:

- `draft -> active` only after complete structural, provenance, requirement-hash, dependency-reference, and verification validation.
- `active -> stale` when any source design, requirement hash, approved scope, or Task revision changes.
- `active|stale -> superseded` when a replacement version activates.
- Content columns, requirement snapshots, and requirement links cannot update or delete after activation; database triggers enforce this. Lifecycle timestamps/status are the only mutable pack fields.
- A partial unique index enforces one `active` pack per Task. `(task_id, version)` and `display_key` are unique. Display keys are allocated transactionally.
- Canonical JSON key ordering and normalized Markdown/list rendering produce `content_hash`; Worker and Reviewer identify that hash.

## Worker Claim Validation

Before claiming or launching Worker, the orchestrator atomically verifies:

1. The Task has exactly one active instruction pack, regardless of `manual` or `materialized` origin.
2. Materialized provenance matches the currently approved Epic design and Task Plan node; standalone provenance matches the current Task revision and approved Task-owned design when required by mode.
3. Every linked requirement exists, belongs to the correct workflow root, has a source hash matching its immutable pack snapshot, and is not superseded/deferred outside this Task.
4. Every blocking Task dependency is `done` with `review_status = passed` against its current pack.
5. Every required canonical section is populated or explicitly says `Not applicable: <approved reason>`.
6. Focused verification contains at least one executable command unless the approved pack records why automation is impossible.
7. Repository cleanliness/worktree policy is satisfied.

The transaction that claims Worker also binds `instruction_pack_id`, pack version, `content_hash`, and the resolved Worker model into `pipeline_runs`. Repeat the same freshness check before Completion/Issue Report persistence, before patch integration, and before Reviewer launch. A mismatch blocks the run and never integrates a stale patch.

Failure blocks before pipeline claim. The Worker never receives an incomplete or stale pack and never fills missing design details itself.

## Exact Task-Worker Handoff

The scheduler passes only the rendered active instruction pack as the child `task` payload. The packaged `task-worker` system prompt and inherited project instructions remain separate.

```markdown
# TASK INSTRUCTION PACK: TIP-001

## HANDOFF VALIDATION
- Pack: pack-invoice-persistence-v1
- Pack version: 1
- Pack status: active
- Pack content hash: sha256:<canonical-pack-content>
- Design: design-invoicing-v3 / version 3
- Design freshness: current
- Requirement links: valid
- Dependency state: satisfied
- Working directory: available
- Result: READY

## HEADER
- TIP-ID: TIP-001
- Pack ID: pack-invoice-persistence-v1
- Pack version: 1
- Task: task-invoice-persistence
- Task name: Create invoice persistence
- Origin: materialized
- Epic: epic-invoicing
- Task Plan node: T01
- Design: design-invoicing-v3 / version 3
- Requirements: REQ-001, REQ-004
- Module: invoice
- Depends on: None
- Priority: P0
- Estimated effort: 90 minutes

## CONTEXT
- Working directory: current process CWD is authoritative; never edit a registered parent checkout path from an isolated worktree
- Source Scan: scan-invoicing-01
- Source Vision: vision-invoicing-01
- Source RRI: rri-invoicing-02
- Approved Design: design-invoicing-v3 / version 3
- Key files:
  - `internal/invoice/service.go`
  - `internal/invoice/repository.go`
  - `internal/invoice/service_test.go`
- Patterns:
  - `internal/order/service.go:CreateOrder` — existing transaction-bound aggregate creation pattern
- Contract references:
  - CONTRACT-INVOICE-01 § CreateInvoice input
  - CONTRACT-INVOICE-03 § Persistence transaction

## TASK
Persist a valid invoice and its line items atomically.

## SPECIFICATIONS

### Business Rules
- Invoice number is unique within an account.
- An invoice requires at least one line item.
- Total equals the sum of normalized line-item amounts.
- Draft invoices do not affect account balance.

### Validation
| Input | Rule | Failure behavior |
|---|---|---|
| `account_id` | Required and must identify an existing account | Return the existing account not-found error |
| `line_items` | At least one item | Return the existing validation error |
| `quantity` | Integer greater than zero | Return the existing validation error |
| `unit_price` | Non-negative decimal with at most two fractional digits | Return the existing validation error |

### Error Handling
| Condition | Required behavior | Recovery |
|---|---|---|
| Duplicate invoice number | Return the existing domain conflict error | Do not retry locally |
| Unknown account | Return not-found without writes | None |
| Line-item insertion failure | Roll back the complete transaction | Propagate repository error |
| Database unavailable | Propagate repository error | Do not retry locally |

### State Transitions
- `absent -> draft` when `CreateInvoice` succeeds.
- `draft -> issued` is outside this Task.

### Contract Obligations
- CONTRACT-INVOICE-01 § CreateInvoice input: accept the approved input without changing its shape.
- CONTRACT-INVOICE-03 § Persistence transaction: commit invoice and line items in one transaction.
- Do not change the public API response shape.

## ACCEPTANCE CRITERIA

### REQ-001 — Scenario: Create a draft invoice
Given an existing account and one valid line item
When CreateInvoice is called
Then a draft invoice and its line item are persisted

### REQ-004 — Scenario: Preserve atomicity
Given valid invoice data
When inserting any line item fails
Then neither the invoice nor any line item remains persisted

## CONSTRAINTS

### Allowed Files
- `internal/invoice/service.go`
- `internal/invoice/repository.go`
- `internal/invoice/service_test.go`

### Must Not Change
- Invoice issuance behavior
- Payment behavior
- Public API response shape

### Required Reuse
- Existing transaction helper
- Existing invoice-number allocator

### Compatibility
- No database migration in this Task.

### Approved Deviations
- None

## VERIFICATION
- `go test ./internal/invoice -run TestCreateInvoice`
- Confirm invoice and line items commit atomically.
- Confirm failed line-item insertion leaves no invoice records.

## REPORT FORMAT
Return the canonical report inline:

### Identity
- Task ID: task-invoice-persistence
- TIP ID: pack-invoice-persistence-v1
- TIP version: 1
- TIP content hash: sha256:<canonical-pack-content>

**STATUS:** DONE / PARTIAL / BLOCKED

**FILES CHANGED:**
- Created: <path — purpose, or None>
- Modified: <path — purpose, or None>

**TEST RESULTS:**
- <REQ-ID>: PASS / FAIL — <evidence>
- Verification: `<exact command>` — PASS / FAIL / NOT RUN — <result>

**ISSUES DISCOVERED:**
- <severity> — <description> — <suggestion, or None>

**DEVIATIONS FROM SPEC:**
- <what> — <why> — <impact, or None>

**SUGGESTIONS FOR CHỦ THẦU:**
- <observation> — <recommendation, or None>
```

The Worker does not receive raw Blueprint Markdown, raw contract Markdown, full Scan/Vision/RRI reports, sibling requirements/specifications, a duplicate dependency graph, parent orchestration instructions, or final-verification/owner-acceptance instructions. A standalone handoff renders `Epic`, `Task Plan node`, Vision/RRI/Design fields as `None` or the approved mode-appropriate source IDs, but otherwise uses the same template. Reviewer receives this same rendered pack ID/version/content hash plus the Completion Report and integrated diff.

## Worker Report And Integration Policy

`pi-subagents` owns checked-runtime acceptance. The task system does not instruct Worker about `criterion-1` or independently decide runtime acceptance. After the extension reports a checked child completion, the scheduler parses and validates the canonical task-system report:

- `DONE`: all TIP acceptance criteria and required verification passed. Persist the Completion Report, revalidate pack freshness, sequentially integrate the patch, then launch Reviewer.
- `PARTIAL`: persist an Issue Report and pipeline blocked state; do not integrate the patch and do not launch Reviewer.
- `BLOCKED`: persist an Issue Report and pipeline blocked state; do not integrate a patch and do not launch Reviewer.
- Missing/malformed report, mismatched Task/pack/version/hash, failed acceptance criterion, missing required verification, or unapproved deviation cannot be treated as `DONE`.

Partial/blocked worktree patches remain diagnostic artifacts only. A later Worker attempt starts from the integrated repository state unless an owner-approved recovery explicitly applies a prior patch.

Completion Reports persist `instruction_pack_id`, pack version/hash, `pipeline_run_id`, Worker model, status, files, tests, REQ evidence, issues, deviations, and suggestions. History is append-only and each retry creates a separate report.

## Reviewer Contract

Reviewer loads the exact pack bound to the accepted Completion Report, not current Task description or raw workflow artifacts. The current review projection stores `reviewed_instruction_pack_id`; review events retain pack version/hash, Completion Report ID, pipeline run, reviewer model, verdict, findings, and evidence. Activating or staling a pack resets `review_status` and clears the current reviewed-pack projection while preserving history.

Review fixes reuse the same pack only when the execution contract is unchanged. A specification/design/requirement correction creates and activates a new pack version before another Worker run.

## Worker Model Resolution

Reuse `pi-subagents` model settings instead of adding a second task-system preference store. The custom `task-worker` model resolves through project/user `subagents.agentOverrides`, then agent frontmatter/default behavior. Persist the resolved model returned by the run in `pipeline_runs` and Completion Reports. Changing model configuration never changes TIP identity or version.

## Existing Feature Migration

Migrate an existing `[Feature]` planning Task to Epic ownership only when its Epic, artifacts, approved design, children, and dependency graph map unambiguously. The migration is transactional and idempotent, records old/new IDs in an event, and leaves the proxy row readable until the Epic copy is verified. Ambiguous or partially migrated workflows are reported and blocked from materialization; they are never guessed or deleted. Fresh workflows use Epic ownership immediately.

## Implementation Tasks

1. `go-pic/cmd/pic/pic_cli_test.go`, `legacy_web_parity_test.go` — add RED migration tests for optional `tasks.epic_id`, Epic workflow metadata/artifact ownership, and preservation of existing Task-owned artifacts. Test: `cd go-pic && go test ./cmd/pic -run 'TestEpicWorkflowSchemaMigration|TestStandaloneTaskWithoutEpic' -count=1`.
   Acceptance: tests fail before implementation because Tasks require an Epic and workflow artifacts cannot belong to an Epic.
   Parallel with: none.

2. `go-pic/cmd/pic/main.go`, `rebuild.go` — extend `epics` with workflow/design/owner state; make `tasks.epic_id` nullable; add `epic_events`; rebuild Scan, RRI, requirement, design, owner-decision, escalation, and verification tables with exactly one owner (`task_id` or `epic_id`); add per-owner uniqueness indexes. Test: `cd go-pic && go test ./cmd/pic -run 'TestEpicWorkflowSchemaMigration|TestPipelineSchemaMigratesLegacyColumns' -count=1`.
   Acceptance: existing Task data survives, a standalone Task can have Task-owned artifacts, and an Epic can have independently owned workflow artifacts with enforced owner constraints.
   Blocked by: 1.

3. `go-pic/cmd/pic/core.go`, `workflow.go`, `misc.go`, `pic_cli_test.go` — make Epic create/show/update expose workflow metadata and artifacts; make Task create accept an optional Epic while preserving explicit Epic attachment; route Scan, Vision events, RRI, requirements, design, decisions, escalation, and verification commands by subject type and ID. Test: `cd go-pic && go test ./cmd/pic -run 'TestEpicWorkflowLifecycle|TestStandaloneTaskWithoutEpic|TestTaskWorkflowReadModel' -count=1`.
   Acceptance: `pic show <epic-id>` returns Epic artifacts, `pic show <task-id>` returns Task artifacts, and a manual Task can complete its existing workflow without an Epic.
   Blocked by: 2.

4. `go-pic/cmd/pic/main.go`, `rebuild.go`, `pic_cli_test.go` — add `tasks.origin` and monotonic `tasks.revision`, `task_materializations`, `task_instruction_packs`, and `instruction_pack_requirement_links`; store structured canonical sections, requirement snapshots/hashes, source provenance, lifecycle timestamps, and content hash; enforce immutable activated content/links, legal state transitions, one active pack per Task, and owner-matched requirement links. Add pack ID/version/hash, Worker model, and raw canonical report Markdown to completion/pipeline evidence plus `reviewed_instruction_pack_id` to the Task review projection without adding a pack dependency graph. Test: `cd go-pic && go test ./cmd/pic -run 'TestTaskInstructionPackLifecycle|TestInstructionPackImmutability|TestInstructionPackRequirementOwnership|TestInstructionPackRendersCanonicalTemplate|TestPipelineRunBindsInstructionPack' -count=1`.
   Acceptance: manual and materialized Tasks can have historical pack versions but only one active version; activated content, snapshots, and links cannot mutate; canonical rendering and hashing are stable; Completion/Review evidence identifies the executed version; and no instruction-pack dependency table exists.
   Blocked by: 2.
   Parallel with: 3 after 2.

5. `go-pic/cmd/pic/feature.go`, `pic_cli_test.go`, `legacy_feature_workflow_test.go` — define and parse the detailed approved Task Plan node contract; replace the hidden planning-Task feature root with Epic-scoped `feature start/status/materialize/work`; create materialized Tasks, provenance, dependency edges, immutable requirement snapshots, and active packs atomically without post-approval model derivation. Add conservative, idempotent migration for unambiguous existing `[Feature]` proxies and block ambiguous/partial mappings. Test: `cd go-pic && go test ./cmd/pic -run 'TestEpicFeatureMaterialization|TestDetailedTaskPlanRequired|TestTaskPlanDependencyGraph|TestLegacyFeatureProxyMigration' -count=1`.
   Acceptance: one approved Epic design materializes one Task and complete active canonical TIP per DAG node; missing business-rule/validation/error/contract/constraint/verification content blocks materialization; reruns are idempotent before implementation; fresh workflows create no proxy Task or cloned child requirement; and ambiguous legacy data is retained and reported.
   Blocked by: 3, 4.

6. `go-pic/cmd/pic/feature.go`, `workflow.go`, `misc.go`, `pic_cli_test.go` — add standalone pack preparation/activation from Task-owned artifacts and add universal freshness/invalidation: source Task revision, design supersession, changed requirement hash, or changed approved scope stales affected packs; historical packs remain immutable; work blocks until a new version activates. Test: `cd go-pic && go test ./cmd/pic -run 'TestStandaloneTaskInstructionPack|TestInstructionPackInvalidation|TestInstructionPackHistoryIsImmutable' -count=1`.
   Acceptance: every standalone or materialized Worker-ready Task has one active pack, links only requirements owned by its workflow root, stale provenance cannot reach Worker, regeneration creates a new version, and prior Completion Reports retain their original pack reference.
   Blocked by: 5.

7. `pi-ext/tool.ts`, `commands.ts`, `task-prompts.ts`, `task-prompts.test.ts` — add explicit `subject_type=task|epic` and `subject_id` workflow contracts; add Epic Scan/Vision/RRI/Design/materialize prompts; make standalone prompts prepare/activate a Task-owned pack before work; remove assumptions that feature workflow has a planning Task ID. Test: `cd pi-ext && node --experimental-strip-types --test task-prompts.test.ts tool.test.ts && npm run check`.
   Acceptance: Epic workflow prompts use Epic IDs/artifacts, standalone workflow prompts use Task-owned artifacts and activate a pack, neither path invents a proxy planning Task, and ID prefixes are never used to infer subject ownership.
   Blocked by: 3, 5.

8. `pi-ext/task-artifacts.ts`, `task-prompts.ts`, `workflow-gates.ts`, `pipeline-scheduler.ts`, corresponding tests — load and render exactly one active pack for every Worker path; source artifacts are read server-side only for provenance/freshness validation and never appended raw; bind pack/version/hash and the `pi-subagents`-resolved Worker model to the pipeline run; revalidate before report persistence, integration, and Reviewer; parse canonical `DONE|PARTIAL|BLOCKED` task-system reports; integrate only `DONE`; remove child Scan/RRI/Design gates and stale child-verification dependency checks for materialized Tasks. Test: `cd pi-ext && node --experimental-strip-types --test task-artifacts.test.ts task-prompts.test.ts workflow-gates.test.ts pipeline-scheduler.test.ts`.
   Acceptance: every Worker handoff contains only one complete canonical TIP plus inherited project/runtime instructions; standalone and materialized Tasks share the same Worker gate; stale/missing packs block; partial/blocked patches never integrate or reach Reviewer; and model/TIP identity persists per run.
   Blocked by: 5, 6, 7.

9. `pi-ext/agents/task-worker.md`, `task-reviewer.md`, `task-prompts.ts`, tests — keep the simplified custom Builder contract (`RECEIVE TIP -> READ CONTEXT -> IMPLEMENT -> SELF-TEST -> REPORT`); remove task-system-authored runtime-acceptance instructions; require the five-section Completion/Issue Report with pack ID/version/hash; make Reviewer load the pack bound to the accepted Completion Report and persist that reviewed-pack identity. Test: `cd pi-ext && node --experimental-strip-types --test task-prompts.test.ts pipeline-scheduler.test.ts`.
   Acceptance: Worker and Reviewer use the same immutable pack/hash, neither loads raw workflow artifacts or reinterprets HEADER/CONTEXT, `pi-subagents` alone owns runtime acceptance, and activating/staling a pack invalidates prior review state.
   Blocked by: 8.

10. `go-pic/cmd/pic/misc.go`, `feature.go`, `workflow.go`, `pi-ext/phase-orchestration.ts`, `task-prompts.ts`, tests — implement pack-bound Completion/Issue Report persistence, review projection/history, Epic rollup, contractor VERIFY REPORT persistence, freshness invalidation after child activity, and explicit owner acceptance at Epic or standalone Task scope. Test: `cd go-pic && go test ./cmd/pic -run 'TestCompletionReportBindsInstructionPack|TestPartialWorkerReportDoesNotCompleteTask|TestReviewBindsInstructionPack|TestEpicContractorVerification|TestStandaloneContractorVerification|TestEpicOwnerAcceptance' -count=1 && cd ../pi-ext && node --experimental-strip-types --test task-prompts.test.ts task-browser-actions.test.ts`.
   Acceptance: only a current `DONE` Completion Report can reach review; review is bound to that exact pack; all required Epic Tasks must be done/review-passed before Epic contractor verification; standalone verification belongs to the Task; later pack/child activity invalidates verification and acceptance; and owner acceptance requires a current passing contractor report.
   Blocked by: 5, 8, 9.

11. `go-pic/cmd/pic/misc.go`, `go-pic/web/src/routes/epic/[id]/+page.svelte`, `go-pic/web/src/routes/task/[id]/+page.svelte`, `go-pic/web/src/lib/api.ts`, `pi-ext/task-browser.ts`, browser/dashboard tests — expose Epic workflow gates, approved design, Task DAG, active/stale/historical pack identity/hash, bound Completion/Review evidence, contractor verification, and owner acceptance; show materialized Tasks as execution units and standalone Tasks with their Task-owned packs. Test: `cd go-pic && go test ./cmd/pic -run 'TestLegacyDashboardServiceParity' -count=1 && cd web && npm run check && npm run build && cd ../../pi-ext && node --experimental-strip-types --test task-browser-actions.test.ts task-navigation.test.ts`.
   Acceptance: Epic detail displays the feature lifecycle and DAG, Task detail displays active/historical pack provenance and report/review binding, and standalone Task views expose the same execution lifecycle without requiring Epic navigation.
   Blocked by: 3, 6, 10.

12. `docs/feature-workflow.md`, `docs/async-pipeline-scheduler-spec.md`, repository-owned workflow docs — document Epic-first feature workflow, universal pre-Worker TIP gate, standalone Task-owned packs, pack snapshots/source-of-truth rules, direct materialized-Task execution, report/integration outcomes, model resolution, and Epic/Task contractor verification. Do not edit global `~/.pi` governance files from this repository; any required global instruction update is a separately scoped owner action. Test: `rg -n 'planning task|\[Feature\]|parent task|worker.*without.*TIP' docs/feature-workflow.md docs/async-pipeline-scheduler-spec.md`.
   Acceptance: repository documentation describes one current flow, requires one active TIP for every Worker, and does not instruct agents to rerun discovery on materialized Tasks, use a planning-Task proxy, or duplicate runtime acceptance.
   Blocked by: 7–11.

13. Generated runtime artifacts and full verification — rebuild installed dashboard/binary artifacts and execute all suites. Test: `cd pi-ext && npm test && npm run check && npm run build && cd ../go-pic && go test ./... -count=1 && cd .. && git diff --check`.
   Acceptance: all TypeScript and Go tests pass, dashboard and binary builds exist, no active legacy TIP surface returns, and whitespace validation passes.
   Blocked by: 1–12.

## Source Of Truth

| Concern | Authority | Instruction-pack treatment |
|---|---|---|
| Intent and boundaries | Epic Vision for materialized work; approved Task scope for standalone work | Reference source identity; render only approved Task-scoped outcome |
| Architecture and repository evidence | Epic Scan or standalone Task Scan | Reference source ID and render scoped citations |
| Requirements | Epic requirements for materialized Tasks; Task requirements for standalone Tasks | Link owner-matched source ID and preserve approved wording/hash snapshot |
| Architecture/contracts | Approved Epic design or mode-appropriate Task-owned design | Reference design ID/version and render only approved scoped obligations |
| Dependency readiness | `task_dependencies` | Render live; never duplicate |
| Task priority/title/status | Task | Render live |
| Pack provenance | `task_materializations` for materialized Tasks; Task revision/design for standalone Tasks | Persist source type, stable node when applicable, and source versions |
| Task execution scope/specification/constraints | Active instruction pack | Authoritative and immutable per version |
| Completion evidence | Append-only Completion/Issue Report | Bind executed pack ID/version/hash, pipeline run, and Worker model |
| Review outcome | Task review projection plus review event history | Bind reviewed pack ID/version/hash and Completion Report |
| Final acceptance | Epic contractor VERIFY REPORT or standalone Task contractor VERIFY REPORT | Never duplicate in child packs |

## Risks

- Tasks currently require `epic_id`; making it nullable affects CLI, web queries, joins, filters, and navigation. Rollback: restore the prior schema/binary together; do not deploy a mixed client/server version.
- Moving feature artifact ownership from proxy Task to Epic is a destructive semantic migration. Rollback: retain old rows until the Epic-owned copy is verified, then remove proxy behavior in a later migration checkpoint rather than deleting first.
- Pack generation can duplicate or drift from source artifacts. Rollback: require approved detailed Task Plan/Task-owned content, immutable requirement snapshots, provenance hashes, and activation validation; regenerate rather than editing active packs.
- Re-materialization after implementation can overwrite execution scope. Rollback: once implementation starts, prohibit in-place mutation and require a new pack version plus explicit affected-Task selection.
- Scheduler concurrency can execute or integrate stale packs if freshness is checked only during materialization/claim. Rollback: bind pack/hash to the run and revalidate at claim, report persistence, integration, and Reviewer launch.
- A runtime-successful subagent may return a Task-level `PARTIAL` or `BLOCKED` report. Rollback: treat runtime acceptance and Task outcome separately; persist the Issue Report but never integrate or review its patch.
- Live requirement rendering can change an immutable pack. Rollback: render activated requirement snapshots and use live rows only for hash-based staleness detection.
- Registered project roots can point isolated Workers at the parent checkout. Rollback: make process CWD authoritative and never render a parent path as an editable Worker location.
- Existing phased-parent workflows and Epic DAG workflows overlap. Rollback: keep legacy phase repair readable during migration, but route new Epic materialization only through `task_materializations`; remove phase compatibility in a separately reviewed cleanup.
