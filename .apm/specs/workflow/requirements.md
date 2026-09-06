# Requirements Checklist: ApmWorkItemImport

**Spec:** .apm/specs/workflow/ApmWorkItemImport.feature
**Date:** 2026-09-06
**Status:** @ready

| # | Requirement | Source | Status |
|---|-------------|--------|--------|
| R01 | Import creates the epic-feature-task hierarchy with deterministic depends_on edges from the Execution Order block | Import happy path (P1) | Open |
| R02 | Rule D validation mismatch aborts with the full discrepancy list and zero writes | Rule D abort (P1) | Open |
| R03 | Dry-run prints the complete JSON graph, writes nothing, and is deterministic across runs | Dry-run purity (P2) | Open |
| R04 | Task-level depends_on edges never cross feature containment; cross-feature order via feature edges only | Cross-feature prohibition (P2) | Open |
| R05 | Phase 5 tasks become epic-level aggregate verification commands, not task Work Items | Phase 5 mapping (P2) | Open |
| R06 | Re-import of an already-imported task list is a hard stop with an explicit error and no writes | Re-import rejection (P2) | Open |
| R07 | Import gate verifies companion .plan.md exists and companion .feature is @ready | Import gate (P2) | Open |
| R08 | Epic carries milestone label as metadata only — no merge authority | Milestone label (P3) | Open |
| R09 | Import performs no git operations; branch is cut at authorization time | No-branch guarantee (P3) | Open |
