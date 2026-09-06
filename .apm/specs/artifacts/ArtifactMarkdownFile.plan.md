# Technical Blueprint: ArtifactMarkdownFile

**Spec:** .apm/specs/artifacts/ArtifactMarkdownFile.feature
**DBML:** .apm/specs/db_schema/artifacts.dbml
**Date:** 2026-09-05
**Status:** Approved

---

## Summary

Every save of a `work_item_artifacts` row (all seven planning stages) gains a
best-effort file projection: the artifact content is written to
`<project>/.apm/artifacts/<work_item>/<stage>-r<revision>.md`, bound through a
new `artifact_files` table, with hash-based conflict detection, on-demand drift
checking, and backfill. The projection lives in the `pic` Go CLI inside
`workItemArtifactSave` (the sole lifecycle mutation authority per PRD §5.1);
pi-ext and the dashboard only surface the resulting `file_path`.

---

## Invariants Check

Verification against the APM constitution (`pi-ext/core/rules/constitution.md`,
adopted from don-cheli-sdd; canonical Work Item workflow takes precedence per
the constitution header):

| Art | Invariant | Status | Notes |
|-----|-----------|--------|-------|
| I | Gherkin is King | ✅ | Plan implements only the 7 scenarios in the `.feature`; projection scope (NC-1), timing (NC-3), path (NC-4), surface (NC-5), failure semantics (NC-2), and drift trigger (NC-7) come from recorded clarifications |
| I-B | Schema as Living Truth | ✅ | Every field used (`artifact_id`, `file_path`, `content_sha256`, `content_hash`, `stage`, `revision`) exists in the DBML with exact names; `artifact_files` is ratified below |
| II | Surgical Precision | ✅ | One domain (artifact persistence); existing `.pi/artifacts/plans/` Blueprint render untouched (NC-4); execution reports out of scope (NC-1) |
| III | Plug-and-Play Architecture | ✅ | New `artifact_files` table + one projection helper; `workItemArtifactSave` gains a post-commit projection call, not an inflated monolith; backfill and drift check are new commands |
| IV | Las Vegas Rule | ✅ | Filesystem I/O is isolated in a small projection module behind a seam (injectable write/hash functions) so Go tests can exercise failure with real temp dirs, no external service involved |
| IV-B | Entry Point Rule | ✅ | Conflict validation sits in `workItemArtifactSave` — the exact entry point the `When "an artifact is saved"` step invokes |
| V | Modern Code Standards | ✅ | Typed Go signatures; JSON payloads via existing `writeJSON`/`map[string]any` CLI convention; no raw untyped DTO shapes introduced |
| VI | Context Adaptability | ✅ | Stack read from `go-pic/go.mod` (Go 1.26, modernc.org/sqlite) — std-lib `crypto/sha256`, `os`, `path/filepath`; no conflicting dependency patterns |
| VII | Defensive Coding | ✅ | Projection failure is logged as a warning event with full error detail (never swallowed); conflict and usage errors return Go errors with clear messages; no retry loops |
| VIII | Clarification Protocol | ✅ | Spec passed `/apm clarify` — all 7 NC items RESOLVED with answers recorded in-scenario; COMMAND precondition/postcondition pattern present |
| — | Canonical Workflow | ✅ | This file is a discovery artifact only; RRI/Blueprint/Contract planning artifacts will be authored by the Work Item's own planning flow, never from this file |

**Result: ✅ PASS** — the plan is compatible with the invariants.

---

## Technical Context

| Aspect | Value |
|--------|-------|
| **Language** | Go 1.26 (`go-pic/go.mod`: `go 1.26`) |
| **Framework** | Go standard `net/http`; no framework in the CLI itself |
| **Dependencies** | `modernc.org/sqlite v1.53.0` (only direct dep); projection uses std-lib `crypto/sha256`, `os`, `path/filepath` — no new deps |
| **Storage** | SQLite at `<project>/.pi/tasks.db` (canonical); new on-disk markdown files under `<project>/.apm/artifacts/<work_item>/` (projection only, SQLite stays source of truth) |
| **Testing** | Go `testing` with real temporary SQLite databases (`go-pic/cmd/pic/*_test.go` pattern: `go test ./cmd/pic -run <TestName>`) |
| **Platform** | Native `pic` binary; artifact saves flow pi-ext `api/tool.ts` → `pic work-item artifact-save` → SQLite + file projection |
| **Performance** | Spec: projection adds < 50 ms p95 to an artifact save — one temp-file write + rename per save, trivially within budget; verified by timing assertion optional |
| **Constraints** | `work_item_artifacts` is immutable (triggers `trg_work_item_artifact_immutable` / `trg_work_item_artifact_delete_immutable`) — the plan never updates artifact rows, only inserts the separate `artifact_files` binding; best-effort semantics per NC-2; Work Item IDs validated as `^wi-[a-z0-9]+$` before path construction (precedent: `blueprintPlanPath`, pi-ext/core/blueprint-drafts.ts) |

---

## API Contract

No HTTP endpoints are added. The surface is the existing `pic` CLI (the
`save_work_item_artifact` tool in pi-ext shells through it unchanged):

**`pic work-item artifact-save <id> <stage> <content>`** (existing command, extended)

- Request: unchanged (work item id, one of the 7 planning stages, content string)
- Success response: existing JSON fields (`id`, `work_item_id`, `stage`,
  `revision`, `content_hash`) plus `file_path` — the projected path, or `""`
  when the projection failed (warning event recorded; see error semantics)
- Conflict error (pre-flight, before any DB write): non-zero exit, message
  contains `artifact file conflict` and the `file_path`; no bytes mutated, no
  rows written
- Projection failure (post-commit): exit 0, row stored, `file_path: ""`,
  warning event `artifact_projection_failed` with payload
  `{stage, revision, file_path, error}` (NC-2)

**`pic work-item artifact-backfill <id>`** (new, COMMAND)

- Request: work item id
- Success: JSON `{written, bound, skipped}` — one file per unbound artifact
  row, each bound to an `artifact_files` row; backfill overwrites any existing
  divergent file bytes with canonical stored content (recovery intent per
  NC-2/success criteria)
- Errors: usage / unknown work item → non-zero exit

**`pic work-item artifact-check <id>`** (new, on-demand drift check per NC-7)

- Request: work item id
- Response: JSON array `[{artifact_id, file_path, status}]` with status
  `ok | drift | missing` per bound artifact row; no scheduler, no verify-flow
  integration (NC-7)

**`pic work-item show <id>`** (existing, additive)

- Artifact rows in the response gain `file_path` (LEFT JOIN on
  `artifact_files`; empty string when unbound). The dashboard Work Item detail
  "Artifact Revisions" table renders it (P3 scenario).

---

## Data Model

Ratified from `.apm/specs/db_schema/artifacts.dbml`; field names exactly as
declared.

**`work_item_artifacts`** (existing canonical table — unchanged, reference only)

- PK `id` (`wia-…`); FK `work_item_id` → `work_items(id)` ON DELETE CASCADE
- `stage` CHECK in (`scan`,`rri`,`rri_t_scenarios`,`vision`,`blueprint`,`contracts`,`task_graph`); `revision` > 0
- `content`, `content_hash` (existing `hashJSON` sha256 digest), `created_at`
- `UNIQUE(work_item_id, stage, revision)`; immutability triggers in force

**`artifact_files`** (new)

| Field | Type | Constraints |
|-------|------|-------------|
| `id` | text | PK (`wiaf-…` short ID) |
| `artifact_id` | text | NOT NULL, UNIQUE — 1:1 with an immutable artifact row; FK → `work_item_artifacts(id)` ON DELETE CASCADE |
| `work_item_id` | text | NOT NULL; FK → `work_items(id)` ON DELETE CASCADE |
| `stage` | text | NOT NULL (mirrors bound artifact) |
| `revision` | integer | NOT NULL (mirrors bound artifact) |
| `file_path` | text | NOT NULL, UNIQUE — deterministic `<project>/.apm/artifacts/<work_item>/<stage>-r<revision>.md` (NC-4), stored absolute from the project root |
| `content_sha256` | text | NOT NULL — equals the bound artifact's `content_hash` |
| `created_at` | timestamp | NOT NULL, default now |

- Indexes: `UNIQUE(work_item_id, stage, revision)` (mirrors the artifact
  uniqueness); `artifact_id` and `file_path` uniques above
- Additive migration: `CREATE TABLE IF NOT EXISTS` guarded exactly like
  `workflow_schema.go` table registration; must survive a re-run against an
  already-migrated database (repo migration policy)
- No field the design needs is missing from the DBML → ratification is a
  marker swap only

---

## Architecture

**Entry point — `workItemArtifactSave` (`go-pic/cmd/pic/work_items.go`), extended in place:**

```
1. existing validation (stage, agent capability, vision/blueprint/contracts shape)
2. NEW pre-flight conflict check (before any DB write):
   path := artifactFilePath(root, work_item_id, stage, revision)
   if file exists && sha256(bytes) != sha256(content) → error "artifact file conflict"
   (revision = current MAX+1, same query as the existing insert path)
3. existing transaction: INSERT work_item_artifacts, clear downstream
   checkpoints, COMMIT  — unchanged; artifact row remains canonical
4. NEW projection (best-effort, NC-2/NC-5):
   temp-file write (0600, dir 0700) + os.Rename commit  [precedent: writeBlueprintPlan]
   on success  → INSERT artifact_files binding (second tx; on failure → warning event)
   on failure  → addEvent("artifact_projection_failed", …warning payload…), file_path ""
   every caught error is recorded with its detail — never swallowed
```

**New module — `go-pic/cmd/pic/artifact_files.go`:** path construction with
`^wi-[a-z0-9]+$` ID validation, atomic write, sha256 compare, `artifact_files`
insert, backfill and drift-check command handlers. Filesystem operations are
localized here so tests use real `t.TempDir()` project roots.

**Read side:** `work-item show` (`misc.go`) artifacts query becomes a LEFT
JOIN on `artifact_files` adding `file_path`; the dashboard Work Item detail
page (`go-pic/web/src/routes/work-item/[id]/+page.svelte`, "Artifact
Revisions" table) renders `file_path` per revision — a one-line addition to
the existing table cell.

**Backfill / drift check:** new `pic work-item artifact-backfill` and
`artifact-check` subcommands routed from `main.go` like existing artifact
subcommands.

Flow invariant: SQLite stays the single canonical store; the file tree is a
regenerable projection. Any divergence (missing file, unbound row, drifted
bytes) is recoverable through `artifact-backfill` or detectable through
`artifact-check`, never through mutation of the canonical row.

---

## Dependencies

None new. `crypto/sha256`, `os`, `path/filepath` are Go standard library;
`modernc.org/sqlite` is already the direct dependency. Nothing speculative.

---

## Complexity Tracking

| Decision | Simpler Alternative | Why Rejected |
|----------|---------------------|--------------|
| Conflict check as pre-flight before the DB transaction | Write the file after commit and fail the save on conflict | A post-commit conflict would strand the committed artifact row with no file and no backfillable path (backfill would hit the same conflict forever), violating the reliability success criterion; the pre-flight check keeps "operation fails" truthful — zero rows, zero mutated bytes |
| Separate `artifact_files` table | Nullable `file_path` column on `work_item_artifacts` | `work_item_artifacts` is canonical and immutability-triggered; a binding table keeps projection state (path, sha256) out of the ratified canonical schema, gives the backfill a clean "unbound rows" target, and matches the DBML as declared (I-B) |
| Atomic temp-file + rename write | Single `os.WriteFile` | A crash mid-write would leave truncated bytes at the deterministic path, which then permanently trips the conflict check on retry; rename-commit is the established repo precedent (`writeBlueprintPlan`) |
| Backfill overwrites divergent file bytes | Backfill refuses on byte mismatch | The spec's backfill scenario is unconditional ("content equal to its stored content") and its stated purpose is recovery of failed projections; refusing would preserve the very corruption backfill exists to repair |
| Warning event stored in existing `work_item_events` | Separate log file or stderr only | The spec requires a recorded, queryable warning tied to the work item; `addEvent` is the existing durable event mechanism — no new transport |
