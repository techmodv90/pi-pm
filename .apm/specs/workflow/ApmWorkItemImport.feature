# .apm/specs/workflow/ApmWorkItemImport.feature
# Status: @ready
# Created: 2026-09-06
# PRD: .apm/prd/prd-v1.0.md (v1.1, draft) — APM delivery pipeline context
# Policy source: owner session decisions on APM-artifact-driven Work Item creation

@ready
Feature: ApmWorkItemImport
  As the owner of an APM-tracked repository
  I want to import an approved APM task list (.tasks.md) into canonical Work Items
  So that APM artifacts are the planning source of truth and the legacy scan→RRI→vision→blueprint→contracts→task-graph→TIP planning pipeline is retired

  Background:
    Given the file `.apm/specs/artifacts/ArtifactMarkdownFile.tasks.md` with `**Status:** Approved`
    And it contains 23 tasks in 5 phases with an Execution Order block
    And Phase 3 has tier headers "P1: Critical Path", "P2: Important", "P3+: Nice to Have"
    And the only parallel pair in the Execution Order block is `T001 ║ T002`

  ## Priority P1: Critical Path (Must Have)

  @P1
  Scenario: Import creates the epic-feature-task hierarchy with deterministic edges
    When I run `pic workflow import-apm .apm/specs/artifacts/ArtifactMarkdownFile.tasks.md --milestone v1.0`
    Then the canonical Work Item store contains one epic "ArtifactMarkdownFile" that is the branch-owning aggregate and carries the milestone label "milestone:v1.0"
    And three feature Work Items exist:
      | feature | contents                                        |
      | F1      | Phase 1 setup, Phase 2 foundation, P1 block     |
      | F2      | P2 block                                        |
      | F3      | P3 block and Phase 4 polish                     |
    And F2 depends_on F1 and F3 depends_on F2
    And each task Work Item exists as a subitem of its feature with:
      | field        | value                                                    |
      | description  | the .tasks.md task block verbatim (TID, tags, body)      |
      | acceptance   | the task's `Acceptance:` line as its acceptance criteria |
      | context      | the task's `Files:` and `Trace:` lines                   |
    And task-level depends_on edges follow the Execution Order block within each feature only
    And T001 and T002 share the same predecessor set with no edge between them
    And no task edge crosses feature containment
    And every imported Work Item is immediately ready with no pending scan, RRI, vision, blueprint, contracts, task-graph, or TIP stage

  @P1
  Scenario: Rule D validation mismatch aborts the import with zero writes
    Given the Execution Order block references task "T024" which does not exist in the task list
    When I run `pic workflow import-apm .apm/specs/artifacts/ArtifactMarkdownFile.tasks.md --milestone v1.0`
    Then the command exits non-zero
    And the error lists every discrepancy between the Execution Order block and the task list
    And the canonical Work Item store is unchanged

  ## Priority P2: Important (Should Have)

  @P2
  Scenario: Dry-run prints the full JSON graph and writes nothing
    When I run `pic workflow import-apm .apm/specs/artifacts/ArtifactMarkdownFile.tasks.md --milestone v1.0 --dry-run`
    Then the JSON on stdout contains the epic, all three features, every task, every depends_on edge, and the milestone label
    And the canonical Work Item store is unchanged
    And running the dry-run twice on the same file produces identical JSON

  @P2
  Scenario: Cross-feature ordering is carried by feature edges only
    Given the Execution Order block implies T010 (last task of F1) precedes T011 (first task of F2)
    When the import creates the graph
    Then no task-level depends_on edge exists between T010 and T11 or any pair of tasks in different features
    And F2 depends_on F1 is the only ordering constraint between the features

  @P2
  Scenario: Phase 5 tasks become epic-level verification, not Work Items
    When the import creates the graph
    Then no task Work Items exist for T021, T022, or T023
    And the epic description records the Phase 5 verification commands to be run by aggregate verification

  @P2
  Scenario: Import gate refuses a task list whose planning artifacts are not intact
    Given the companion `.plan.md` referenced by the task list does not exist
    When I run `pic workflow import-apm .apm/specs/artifacts/ArtifactMarkdownFile.tasks.md --milestone v1.0`
    Then the command exits non-zero reporting the missing planning artifact
    And no Work Items are created

  @P2
  Scenario: Import gate refuses a task list whose companion .feature is not @ready
    Given the companion `.feature` is tagged `@draft`
    When I run `pic workflow import-apm .apm/specs/artifacts/ArtifactMarkdownFile.tasks.md --milestone v1.0`
    Then the command exits non-zero reporting the unready spec
    And no Work Items are created

  @P2
  Scenario: Re-importing an already-imported task list is rejected
    Given a previous import of `.apm/specs/artifacts/ArtifactMarkdownFile.tasks.md` succeeded
    When I run `pic workflow import-apm .apm/specs/artifacts/ArtifactMarkdownFile.tasks.md --milestone v1.0` again
    Then the command exits non-zero with an explicit "already imported" error naming the epic
    And no Work Items are created, updated, or duplicated

  ## Priority P3+: Desirable (Nice to Have)

  @P3
  Scenario: Milestone label carries the PRD version with no merge authority
    When the import creates the epic with `--milestone v1.0`
    Then the epic carries the label "milestone:v1.0"
    And the label is metadata only — it grants no branch ownership, merge authority, or workflow-state changes

  @P3
  Scenario: Import creates no branch
    When the import creates the graph
    Then no delivery branch is cut and no git operation is performed
    And the epic's delivery branch is cut later, at authorization time, by the existing scheduler

  # === SUCCESS CRITERIA ===
  # UX: `--dry-run` prints the complete JSON graph to stdout and writes no rows
  # Reliability: any validation mismatch aborts with the full discrepancy list and zero rows written (transactional)
  # Reliability: same input file always produces the identical graph (deterministic parse, no inference)
  # Reliability: features execute strictly sequentially — the scheduler never runs two features of one epic in parallel
  # Business: imported Work Items enter execution directly; legacy planning stages are deprecated for new Work Items via explicit policy states with regression tests

  # === RESOLVED DECISIONS (owner, 2026-09-06) ===
  # 1. Re-import policy: hard stop — idempotent update rejected (silent-drift risk when the .md changed between imports)
  # 2. Naming: epic from the .tasks.md Feature: field, features from Phase 3 tier headers — no override flags
  # 3. Branch: cut at authorization time by the scheduler, never at import
  # 4. Gate: importer verifies companion .plan.md exists and .feature is @ready before importing
