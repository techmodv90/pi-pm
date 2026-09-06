# Technical Blueprint: ApmWorkItemImport

**Spec:** .apm/specs/workflow/ApmWorkItemImport.feature
**DBML:** N/A — importer adds no tables or fields; it writes through existing Work Item tables
**Date:** 2026-09-06
**Status:** Approved

---

## Summary

A new deterministic Go CLI subcommand `pic workflow import-apm` parses an owner-approved
`.tasks.md`, validates it (Rule D), and transactionally creates the canonical Work Item
hierarchy — one branch-owning epic, three P-tier features (F1 carrying Phase 1+2 foundation),
task subitems with verbatim descriptions and `Acceptance:` lines as acceptance criteria —
plus milestone labels and intra-feature `depends_on` edges. `--dry-run` emits the identical
parse as JSON to stdout without writing. This retires the legacy scan→RRI→vision→blueprint→
contracts→task-graph→TIP planning pipeline for APM-driven features (deprecation itself is a
separate policy step, not this import path).

---

## Invariants Check

| Art | Invariant | Status | Notes |
|-----|-----------|--------|-------|
| I | Gherkin is King | ✅ | All 11 scenarios map 1:1 to importer behavior; no behavior invented |
| I-B | Schema as Living Truth | ✅ | No data model of its own; reuses `work_items`, `work_item_labels`, `work_item_dependencies` exactly as ratified |
| II | Surgical Precision | ✅ | One new module + one subcommand wiring; no existing handler modified |
| III | Plug-and-Play Architecture | ✅ | New `apm_import.go`; `cmdWorkflow` gains one `case` |
| IV | Las Vegas Rule | ✅ | No external services; filesystem and SQLite are the only boundaries |
| IV-B | Entry Point Rule | ✅ | Rule D validation runs at the importer entry before any creation call |
| V | Modern Code Standards | ✅ | Typed structs for parsed task/phase/graph; explicit error values |
| VI | Context Adaptability | ✅ | Stack from manifests: Go 1.26, stdlib only, modernc.org/sqlite |
| VII | Defensive Coding | ✅ | Validation mismatches abort with full discrepancy list; zero partial writes via one transaction |
| VIII | Clarification Protocol | ✅ | Spec is `@ready`; 4 markers resolved and recorded in the spec |
| — | Canonical Workflow | ✅ (reconciled) | Importer creates Work Items through the same creation path as `pic work-item create`; plan is discovery only. Constitution header's legacy flow (Scan→RRI→…) is superseded by owner policy decision 2026-09-06: APM artifacts become the planning source of truth; deprecation lands with regression tests in the deprecation step, not silently here |

**Result: ✅ PASS**

---

## Technical Context

| Aspect | Value |
|--------|-------|
| **Language** | Go 1.26 (`go-pic/go.mod`) |
| **Framework** | Go standard library only — `os`, `fmt`, `regexp`, `strings`, `encoding/json`, `crypto/sha256` |
| **Dependencies** | `modernc.org/sqlite` (existing); no new dependencies |
| **Storage** | SQLite via existing canonical schema (`work_items`, `work_item_labels`, `work_item_dependencies`) |
| **Testing** | Go `testing`, real temporary SQLite databases (`pic_cli_test.go` pattern) |
| **Platform** | Native `pic` binary |
| **Performance** | N/A — single-file parse + ~30 row inserts; no criteria in spec |
| **Constraints** | Task edges never cross feature containment; features execute strictly sequentially; no git operations; no TIP generation; scheduler owns branch cut and execution |

---

## API Contract

### `pic workflow import-apm <tasks.md> --milestone <version> [--dry-run]`

Request (flags):

| Flag | Required | Meaning |
|------|----------|---------|
| `<tasks.md>` | yes | Path to an approved APM task list (`**Status:** Approved` required) |
| `--milestone` | yes | PRD version string; becomes label `milestone:<version>` on the epic |
| `--dry-run` | no | Parse, validate, print JSON graph, write nothing |

Gate order (each failure → non-zero exit, JSON error object, zero writes):

1. Tasks file exists and contains `**Status:** Approved`
2. Companion `.plan.md` referenced by the `**Plan:**` header exists
3. Companion `.feature` (resolved from the plan's `**Spec:**` header) is `@ready`
4. Re-import check: no existing Work Item carries label `import:<sha256-12 of tasks.md content>`
5. Rule D: Execution Order block consistent with the task list (every TID exists, appears once, phases match headers, `║` pairs carry `[P]`, US tags match the Scenario Map)
6. Tier structure: Phase 3 contains `P1`/`P2`/`P3` tier headers (P3 header may be `P3` or `P3+`)

Success output (`--dry-run`, JSON on stdout):

```json
{
  "epic":     { "name": "…", "milestone_label": "milestone:v1.0",
                "verification_commands": ["T021 …", "T022 …", "T023 …"] },
  "features": [ { "key": "F1", "name": "P1: Critical Path", "task_ids": ["T001","…"] } ],
  "tasks":    [ { "tid": "T001", "feature": "F1", "description": "verbatim block",
                  "files": "…", "trace": "…", "acceptance": "…", "depends_on": [] } ],
  "edges":    [ { "from": "F2", "to": "F1" } ]
}
```

Non-dry-run success: same JSON plus `"imported": true` and the created Work Item IDs; exit 0.

Errors (exit 1): `{"error": "<message>", "discrepancies": ["…"], "imported": false}`

---

## Data Model

N/A — no new tables or columns. Mapping onto existing tables:

| Import concept | Existing table | Notes |
|----------------|----------------|-------|
| Epic / features / tasks | `work_items` | Types `epic`/`feature`/`task`; `parent_id` wires containment; epic status/branch semantics unchanged |
| Milestone label | `work_item_labels` | `milestone:<version>` on epic |
| Import provenance / re-import guard | `work_item_labels` | `import:<sha256-12 of tasks.md content>` on epic |
| Task and feature edges | `work_item_dependencies` | `(depends_on)` pairs; never cross feature containment at task level |
| Worker input | `work_items.description` | Verbatim task block + `Files:` + `Trace:` as context + `Acceptance:` line marked as the acceptance criteria |

Phase 5 tasks (final verification) are **not** Work Items: their commands are recorded in the
epic description and executed by aggregate verification.

---

## Architecture

New module `go-pic/cmd/pic/apm_import.go`, four pure-ish stages plus one transactional write:

```
parseTasksFile   → []Task{TID, tags, phase, tier, description, files, trace, acceptance}, phases, tier headers, Execution Order block, headers (Status/Feature/Plan)
validate (Rule D)→ []discrepancy; any non-empty → abort
buildGraph       → Epic, 3 Features (tier grouping: Phase1+2+P1→F1, P2→F2, P3+polish→F3), Rules A–C edges, Phase 5 → epic verification commands
renderJSON       → dry-run output (same structs as the writer consumes)
createWorkItems  → single SQLite transaction: epic, features, tasks, labels, dependencies
```

`cmdWorkflow` gains `case "import-apm"`. File parsing is regex/string-based over the
established `.tasks.md` format (identical shape to the spec-kit template; the only parallel
marker is `║` inside the fenced Execution Order block). Task→tier assignment: explicit
`[P1]/[P2]/[P3]` tags on Phase 3 tasks; setup tasks (Phase 1) and foundation tasks (Phase 2)
join F1; polish tasks (Phase 4) join F3; Phase 5 tasks join no feature.

Feature naming derives from the Phase 3 tier headers verbatim (`P1: Critical Path` →
`"P1: Critical Path"`); epic name from the `# Tasks: <Name>` header.

Creation reuses the same insertion logic as `pic work-item create` (single insert per node
with type/parent/description), plus label and dependency inserts — all inside one
transaction so any failure rolls back to an empty store (R02/R06/R07 semantics).

---

## Dependencies

None new. Everything is Go standard library plus the existing SQLite driver.

---

## Complexity Tracking

| Decision | Simpler Alternative | Why Rejected |
|----------|---------------------|--------------|
| Importer as deterministic Go subcommand | LLM prompt doing the import | Parsing and edge generation must be reproducible bit-for-bit (R03 determinism); a prompt cannot guarantee that |
| Parse `.tasks.md` directly | Persisted `.tasks.json` intermediate | Second source of truth drifts after owner edits the approved `.md`; owner decision 2026-09-06 — dry-run JSON is an output, not an input |
| Import provenance via label `import:<sha256-12>` | New provenance table or column | Schema change needs a migration and violates Surgical Precision; label already has a unique index and is queryable |
| Tier structure derived from file (headers + tags) | Owner-supplied tier-map argument | Deterministic from the approved artifact; an argument invites typos and defeats "same file → same graph" |
| Re-import as hard stop | Idempotent update of the existing graph | Owner decision 2026-09-06: silent drift risk when the `.md` changed between imports |
| Single transaction for all creations | Per-node creation with cleanup | Partial graphs are forbidden by R02/R06/R07; transaction is the only honest guarantee |
