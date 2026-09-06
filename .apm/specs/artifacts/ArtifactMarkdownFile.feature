# .apm/specs/artifacts/ArtifactMarkdownFile.feature
# Status: @ready
# Type: COMMAND
# Created: 2026-09-05
#
# Inferred intent: every saved work-item planning artifact is projected to a
# markdown file on disk (SQLite work_item_artifacts remains canonical), so
# owner and agents can read, diff, and archive artifacts without the dashboard
# or SQL. Precedent: the Blueprint plan render already persists at
# .pi/artifacts/plans/<work-item-id>.md (pi-ext/core/blueprint-drafts.ts,
# OB-F3-1); this spec generalizes file persistence to artifact saves.

@ready
Feature: Artifact Markdown File Storage
  As the owner and contractor agents
  I want every saved Work Item artifact stored as a markdown file on disk
  So that artifacts are reviewable, diffable, and archivable outside the dashboard and SQLite

  ## Priority P1: Critical Path (Must Have)

  @P1 @US1
  Scenario: Saving an artifact stores its content as a markdown file
    Given Work Item "wi-abc123" exists
    And no artifact exists for work_item_id "wi-abc123", stage "rri", revision 1
    When an artifact is saved with work_item_id "wi-abc123", stage "rri", revision 1, and content "# RRI Report\n\nRequirement matrix follows."
    Then the artifact row is stored with content_hash equal to the sha256 of content
    And a markdown file exists at file_path "<project>/.apm/artifacts/wi-abc123/rri-r1.md"
    And the file bytes equal content exactly
    And an artifact_files row binds artifact_id to file_path with content_sha256 equal to content_hash
    # Clarified (NC-1): artifact scope — all artifacts? → all planning stages of
    #   work_item_artifacts (scan, rri, rri_t_scenarios, vision, blueprint,
    #   contracts, task_graph); execution reports out of scope
    # Clarified (NC-3): write timing — project on every save or approval-only? →
    #   every save
    # Clarified (NC-4): path convention → <project>/.apm/artifacts/<work_item>/
    #   <stage>-r<revision>.md; the existing .pi/artifacts/plans/ Blueprint render
    #   stays where it is, no migration
    # Clarified (NC-5): surface → projection inside artifact-save, not a
    #   pic markdown --out extension

  @P1 @US2
  Scenario: Best-effort projection keeps the canonical save on file-write failure
    Given Work Item "wi-abc123" exists
    And the directory "<project>/.apm/artifacts" is not writable
    When an artifact is saved with work_item_id "wi-abc123", stage "vision", revision 1, and content "# Vision\n"
    Then the operation succeeds and the work_item_artifacts row is stored
    And a warning event records the failed file projection for work_item_id "wi-abc123", stage "vision", revision 1
    And no markdown file exists at file_path "<project>/.apm/artifacts/wi-abc123/vision-r1.md"
    # Clarified (NC-2): failure semantics — fail-closed vs best-effort? →
    #   best-effort mirror: canonical save succeeds, failed projection recorded
    #   as a warning event, missing file recoverable via backfill

  ## Priority P2: Important (Should Have)

  @P2 @US3
  Scenario: New revision creates a new file and never rewrites prior files
    Given Work Item "wi-abc123" has an approved artifact at stage "blueprint", revision 1 with file "<project>/.apm/artifacts/wi-abc123/blueprint-r1.md"
    When an artifact is saved with work_item_id "wi-abc123", stage "blueprint", revision 2, and revised content
    Then a markdown file exists at file_path "<project>/.apm/artifacts/wi-abc123/blueprint-r2.md"
    And the bytes of "<project>/.apm/artifacts/wi-abc123/blueprint-r1.md" are unchanged

  @P2 @US4
  Scenario: Existing file with conflicting bytes blocks the save
    Given Work Item "wi-abc123" exists
    And a file already exists at file_path "<project>/.apm/artifacts/wi-abc123/contracts-r1.md" with bytes that differ from the artifact content
    And no artifact row exists for work_item_id "wi-abc123", stage "contracts", revision 1
    When an artifact is saved with work_item_id "wi-abc123", stage "contracts", revision 1, and content "# Contracts\n"
    Then the operation fails
    And the error message indicates "artifact file conflict"
    And the bytes at file_path "<project>/.apm/artifacts/wi-abc123/contracts-r1.md" are unchanged

  @P2 @US5
  Scenario: File drift from the canonical content is detectable
    Given Work Item "wi-abc123" has an artifact at stage "scan", revision 1 with a bound artifact_files row
    And the bytes at that row's file_path no longer hash to content_sha256
    When artifact file integrity is checked for work_item_id "wi-abc123"
    Then the check reports drift for artifact_id with its file_path
    And the check passes for every artifact whose file bytes hash to content_sha256
    # Clarified (NC-7): drift-check trigger → hash comparison: the check is an
    #   on-demand sha256 compare of file bytes against content_sha256; no
    #   scheduler and no verify-flow integration

  ## Priority P3+: Desirable (Nice to Have)

  @P3 @US6
  Scenario: Backfill files for artifacts saved before the projection existed
    Given Work Item "wi-abc123" has artifact rows with no bound artifact_files rows
    When a file backfill is run for work_item_id "wi-abc123"
    Then a markdown file exists for every artifact row with content equal to its stored content
    And every artifact row is bound to an artifact_files row with content_sha256 equal to content_hash

  @P3 @US7
  Scenario: Dashboard links each artifact to its file
    Given Work Item "wi-abc123" has artifacts with bound file_path values
    When the Work Item detail view is opened in the dashboard
    Then each artifact entry shows its file_path

  # === SUCCESS CRITERIA ===
  # UX: every saved artifact is readable as valid markdown at a deterministic
  #     file_path under its Work Item directory, with no dashboard or SQL access
  # Performance: file projection adds < 50ms at p95 to an artifact save
  # Reliability: every work_item_artifacts row is bound to an artifact_files
  #     row whose file bytes hash to content_sha256, except rows whose
  #     projection failed (each recorded as a warning event and recoverable
  #     via backfill); a failed projection leaves the canonical row intact and
  #     zero mutated existing files
  # Business: the owner can review and archive any artifact from disk alone

  # === NEEDS CLARIFICATION ===
  # 1. RESOLVED (NC-1): scope → all planning stages of work_item_artifacts;
  #    execution reports out of scope. Answer recorded under the P1 save
  #    scenario.
  # 2. RESOLVED (NC-2): failure semantics → best-effort mirror, warning event,
  #    canonical save kept. P1 sad path rewritten accordingly.
  # 3. RESOLVED (NC-3): write timing → project on every save.
  # 4. RESOLVED (NC-4): path → <project>/.apm/artifacts/<work_item>/
  #    <stage>-r<revision>.md; existing .pi/artifacts/plans/ render untouched.
  # 5. RESOLVED (NC-5): surface → projection inside artifact-save.
  # 6. RESOLVED (NC-6): Type → COMMAND; `# Type: COMMAND` added to the file header.
  # 7. RESOLVED (NC-7): drift-check trigger → hash comparison, on demand; no
  #    scheduler or verify-flow integration (interpretation recorded under the
  #    drift scenario — owner to review).
