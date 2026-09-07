# Feature: Lean Task Claim
# Status: @ready
# Type: COMMAND

@ready
Feature: LeanTaskClaim

# === USER STORIES / SCENARIOS ===

  @P1 @US1
  Scenario: Claiming a task with no legacy pipeline state takes the lean path
    Given a task Work Item with no instruction packs and no materializations
    And no other agent holds a claim on it
    When an agent claims it for work
    Then the status becomes "in_progress" with claimant and claim time recorded
    And a claim event is appended to the Work Item activity log
    And no instruction pack, checkpoint, or materialization is created

  @P1 @US2
  Scenario: A second agent cannot claim while a claim is active
    Given a task Work Item claimed and in progress by agent A
    When agent B attempts to claim the same task
    Then the claim is rejected with a clear in-use error
    And no state change or event is recorded for the rejected attempt

  @P1 @US3
  Scenario: An item carrying legacy pipeline state routes to the legacy path
    Given a Work Item with an active instruction pack or an authorized materialization
    When an agent claims it
    Then the existing legacy claim gates run unchanged
    And no lean-path behavior alters the legacy provenance record

  @P2 @US4
  Scenario: Lean worker input is the task description verbatim
    Given a task Work Item claimed on the lean path
    When the worker input is assembled
    Then it is exactly the stored description plus its Acceptance criteria
    And no TIP rendering, pack expansion, or instruction pack reference appears

  @P2 @US5
  Scenario: Lean completion closes the task with an event only
    Given a task Work Item in progress on the lean path with its Acceptance command passing
    When the agent completes the task
    Then the status becomes "done" and a completion event is appended
    And no completion-report row keyed on pack columns is required

  @P2 @US6
  Scenario: Review and verification on the lean path are not pack-bound
    Given a lean-path task with a completed worker run
    When a reviewer reviews it and verification is recorded
    Then review and verification records reference the task and run without requiring pack columns
    And a failed review can route a fix attempt without a pack-hash cycle cap

  @P3 @US7
  Scenario: Legacy read-back integrity for historical items
    Given historical Work Items that completed under the legacy pipeline with packs
    When their detail, completion, and verification records are read
    Then all legacy joins still resolve and nothing from the lean change mutates their rows

# === SUCCESS CRITERIA ===
# UX: any task can be claimed, worked, and closed by one agent with status
#     flips and an activity log — no pack, authorization, or materialization
#     ceremony
# Reliability: single-writer exclusion still holds for every task; legacy
#     items keep their full provenance and read paths
# Business: the deprecation policy holds — legacy is frozen and read-only,
#     lean is the only path new work enters

# === NEEDS CLARIFICATION ===
# 1. RESOLVED: in-flight legacy items — verified none (0 in_progress with
#    legacy state; the 3 materialized/no-pack items are terminal deprecated
#    features). Lean claim may take over their open children safely.
# 2. RESOLVED: routing is state-driven (presence of pack/materialization),
#    not label-driven; no new labels are introduced.
