# Skill-Family Routing — Wire the Guard in Observe Mode, Surface the Catalog, Harden Matching, Cut the Shelf

## Goal

Make skill-family selection in the task pipeline actually work end to end: the planner must see
the family catalog it is required to declare from, the applicability matcher must run in
production (telemetry-only first, enforcement later), routing decisions must be observable as
queryable wide events, and the always-loaded skill shelf must shrink where content is provably
off-task. Routing decisions must also be visible in the web dashboard, not only in SQL.
No workflow-state changes, no new approval gates, no schema migrations.

## Background (What Exploration Established, 2026-08-30)

- **The applicability guard is dead code.** `matchingSkillFamilies`,
  `requireApplicableFamilies`, `validateTaskPlanSkillFamilies`, and
  `validateInstructionPackSkillFamilies` (`pi-ext/subagent/skills.ts:123-194`) have zero
  production callers. The only production check is family-ID validity in
  `pipelineWorkerBlockReason` (`pi-ext/pipeline/stage-resolution.ts:161-166`). GAP-043's intent
  ("future TIPs and Task Plan nodes cannot omit matching installed families") was never wired.
- **The planner routes blind.** `pi-ext/agents/task-planner.md:114` requires `skillFamilies` on
  every Task Graph node, but no code surfaces the family catalog to the planner — it can only
  get IDs right by luck or memory.
- **Go only persists.** `go-pic/internal/tip/tip.go` stores `skill_families_json` and requires
  the field on graph nodes; no applicability matching exists on the Go side.
- **No logging infra in pi-ext.** The durable, queryable sink is
  `pic workflow event-add` → `work_item_events` (`go-pic/cmd/pic/misc.go:562-599`, columns in
  `go-pic/cmd/pic/schema_bootstrap.go:38`), payload via `--payload-json`, queryable with
  `json_extract` (precedent: circuit-breaker counters in `go-pic/cmd/pic/pipeline.go:308-353`).
- **The dashboard has a dead analytics tab.** `go-pic/web/src/lib/api.ts:124` calls
  `GET /api/projects/{id}/workflow-analytics`, but no such case exists in the `handleAPI`
  dispatcher (`go-pic/cmd/pic/misc.go:326-484`), so the dashboard's Analytics tab always
  renders "No workflow analytics recorded." (`go-pic/web/src/routes/dashboard/+page.svelte:216-218`).
  Discovered while scoping the routing panel (2026-08-30): the Routing tab is added alongside
  it as a fourth tab, and the stub is flagged in the audit doc, not silently repurposed.
- **The shelf.** Worker baseline loads `shadcn-svelte` (88K, UI-only) on every task
  (`pi-ext/agents/task-worker.md:10`). Packaged catalog is 3 families / 54 skills / 2.2M
  (`pi-ext/task-skills/`), golang alone being 45 skills / 1.9M with ~12 vendor-specific skills.
  User-level roots carry a diverged duplicate sveltekit corpus and other unreferenced bulk
  (detail in the audit doc, Phase 3).
- **Context of this plan:** assessment of a "dynamic skill retrieval" proposal (cut the shelf /
  tool search / embedding re-ranking) concluded the repo should stay deterministic: measure
  first, keep a guard so a routing miss is never silent, and only consider a re-ranker if
  telemetry proves the deterministic matcher is the bottleneck.

## Non-Negotiable Invariants

- Observe mode only in this pass: no launch blocking, no workflow-state changes, no new
  approval gates. The enforcement flip is a follow-up plan gated on telemetry.
- Go CLI remains sole lifecycle mutation authority. Go changes are read-only additions
  (the skill-routing JSON endpoint, its aggregation queries, and handler tests); the telemetry
  writer stays the existing `workflow event-add` command. The generated dashboard build
  (`go-pic/web/build`) is rebuilt via `npm run build`, never hand-edited.
- No schema migrations: telemetry lives in the existing `work_item_events` table.
- Dashboard changes follow the existing patterns: one typed method per endpoint in
  `go-pic/web/src/lib/api.ts`, client-side loading via `$state.raw` and the lazy `switchTab()`
  pattern, hand-rolled CSS classes (`.stats-grid`, `.table-scroll`, `.empty-state`); no new
  frontend dependencies (web has no shadcn/tailwind).
- `requireApplicableFamilies`'s error message format stays byte-identical
  (`pi-ext/subagent/skills.test.ts:102` asserts it); existing routing semantics (ID validity at
  the worker gate) are unchanged.
- No new `any` in `pipeline/` and `tasking/` (eslint error scope); persisted payload fields are
  snake_case, TS fields camelCase.
- Skill catalogs may not contain symbolic links; skills inside a family must use the family
  leaf prefix.

## Owner Decisions (2026-08-30)

1. **Guard mode: observe first.** Record routing events without blocking. Enforcement at the
   worker gate ("missing applicable skill families: ...") is deferred until observe-mode data
   shows planner recall is adequate; each enforcement miss costs a TIP revision cycle because
   active TIPs are immutable.
2. **Shelf scope: audit doc + move shadcn out of baseline.** Golang vendor-skill pruning was
   considered and declined for this pass (recorded as a flagged candidate in the audit doc).
3. **Dashboard visibility (added 2026-08-30).** Routing statistics must also be visible in the
   web dashboard: a read-only aggregation endpoint plus a Routing tab, not SQL-only access.

## Design Decisions

1. **One wide event per worker launch, not per matcher call.** `launchGroup`
   (`pi-ext/pipeline/pipeline-scheduler.ts`, around lines 524-600) evaluates
   `evaluateSkillFamilyMatches([packContent, scanEvidence])` — `packContent` from the active
   pack's `content_json` (available post-`normalizePipelineData`, see
   `pi-ext/pipeline/stage-resolution.ts:71`), `scanEvidence` from the normalized scan-report row
   (defensive: omit when absent, flag evidence sources in the payload) — and emits
   `pic workflow event-add <workItemId> skill_family_routing --summary "..." --payload-json
   '{...}' --actor-role scheduler` fire-and-forget (nonblocking, activity-tracker pattern,
   `pi-ext/core/activity-tracker.ts:36-49`). Telemetry failure must never block launch.
   Payload (snake_case): `stage`, `pack_id`, `selected_families`, `matched_families`
   (each `{id, matched_by}`), `missing_families`, `evidence_sources`.
2. **Pack-level evaluation instead of node-level.** `validateTaskPlanSkillFamilies` (markdown
   `task-plan-json` blocks) stays test-only: the pipeline materializes each node into its own
   TIP, so pack-level evaluation at worker launch covers node-level intent. The markdown-block
   variant belongs to Go's standalone materialization path, which is out of scope.
3. **Match details, not just IDs.** The internal matcher is reworked to return
   `{id, matchedBy[]}` (which `appliesTo` tokens fired) with a new exported
   `evaluateSkillFamilyMatches(evidence, options)`; `requireApplicableFamilies` keeps its exact
   error text and behavior.
4. **Boundary-aware extension tokens.** Tokens starting with `.` require a trailing boundary
   (regex-style: `.go` must not fire on `foo.gone` or `golang.org`, but must fire on
   `logo.go`); word tokens keep substring semantics. Deterministic and testable — no embeddings.
5. **Catalog reaches the planner via the handoff.** A `<skill_family_catalog>` section (ids +
   descriptions from `listSkillFamilies()`) is added to the task-graph handoff composition in
   `pi-ext/pipeline/stage-prompts.ts`. Additive prompt text only — the handoff is consumed by
   the planner, never parsed back, so no schema version bump.
6. **Shadcn becomes a routed family, not a baseline.** Vendor (copy — catalogs forbid
   symlinks) `~/.agents/skills/shadcn-svelte/` into
   `pi-ext/task-skills/frameworks/shadcn/shadcn-svelte/` with
   `mandatorySkills: ["shadcn-svelte"]` and
   `appliesTo.technologies: ["shadcn", "shadcn/ui", "shadcn-svelte"]` — deliberately no
   `.svelte`, so it does not ride along on every Svelte task. The skill name satisfies the
   `shadcn-` leaf-prefix rule. Upstream divergence risk is recorded in the audit doc.
7. **Read-only aggregate endpoint + Routing tab.** `GET /api/projects/{id}/skill-routing` is
   added to the `handleAPI` dispatcher (`go-pic/cmd/pic/misc.go:365-483`), modeled on
   `projectSummary`/`queryKeyCounts` (`misc.go:516-538`) and the
   `json_extract(...) ... AND json_valid(payload_json)` query style of the circuit-breaker
   counters (`go-pic/cmd/pic/pipeline.go:308-353`). Response shape (camelCase, matching web API
   conventions): `{projectId, totalEvents, familyCounts: [{family, count, matchedBy}],
   missingCounts: [{missing, count}], recentEvents: [{workItemId, createdAt, stage, packId,
   selectedFamilies, matchedFamilies, missingFamilies, evidenceSources}]}` (recentEvents capped,
   e.g. last 50). The frontend gains a fourth "Routing" tab in
   `go-pic/web/src/routes/dashboard/+page.svelte` (lazy-loaded in `switchTab()`, mirroring the
   analytics tab's lazy-load pattern) fed by a new `api.skillRouting()` method; the work-item
   detail page (`go-pic/web/src/routes/work-item/[id]/+page.svelte`) gains a small
   "Routing Events" section via `workItemDetailForWeb` (`misc.go:302-324`).

## Work Breakdown

### Phase 1 — Routing evaluation + telemetry (no workflow behavior change)

1. `pi-ext/subagent/skills.ts`: rework matcher to `{id, matchedBy[]}`; export
   `evaluateSkillFamilyMatches`; keep `requireApplicableFamilies` text identical.
2. `pi-ext/pipeline/stage-prompts.ts`: add `<skill_family_catalog>` to the task-graph handoff.
3. `pi-ext/tasking/phase-plan.ts:132` and `pi-ext/agents/task-planner.md`: point the
   `skillFamilies` guidance at the catalog section.
4. `pi-ext/pipeline/pipeline-scheduler.ts`: compute routing evaluation in `launchGroup`; emit
   `skill_family_routing` event fire-and-forget via `execPic`
   (`pi-ext/core/cli-helpers.ts`).
5. Tests: `pi-ext/subagent/skills.test.ts` (match details, token attribution, scanEvidence
   combination) and `pi-ext/pipeline/pipeline-scheduler.test.ts` (event emitted with expected
   type and payload keys; launches still succeed with missing families — observe mode).

### Phase 2 — Matcher quality (deterministic)

6. Boundary-aware extension matching in the matcher, with regression tests
   (`.go` vs `logo.go` / `foo.gone` / `golang.org`).
7. Manifest token enrichment, authoring only: golang += `go.mod`, `golang.org`; typescript +=
   `tsconfig`; sveltekit += `svelte.config`, `.svelte.ts` (`pi-ext/task-skills/*/family.json`).

### Phase 3 — Cut the shelf

8. Vendor shadcn-svelte into `pi-ext/task-skills/frameworks/shadcn/` (family.json per Design
   Decision 6) and remove `shadcn-svelte` from `skills:` in `pi-ext/agents/task-worker.md:10`;
   add a resolution test against the real packaged root (pattern at
   `pi-ext/subagent/skills.test.ts:106`).
9. `docs/skill-shelf-audit.md`: full inventory (packaged + `~/.pi/agent/skills`,
   `~/.agents/skills`, `~/.pi/agent/git`), dead-weight flags (diverged duplicate sveltekit
   corpus 192K vs packaged 196K; unreferenced `playwright-best-practices` 880K;
   `design-taste-frontend` 87K; `vercel-react-native-skills` missing frontmatter description;
   empty `~/.pi/agent/git/github.com/techmodv90/`; duplicate ponytail `.openclaw` copy; golang
   vendor-overlap candidate list), shadcn provenance note, and how to read
   `skill_family_routing` events. Also record the dead `workflow-analytics` dashboard stub
   (`api.ts:124` calls an endpoint `handleAPI` never implemented).

### Phase 4 — Dashboard visibility (read-only)

10. `go-pic/cmd/pic/misc.go`: add `GET /api/projects/{id}/skill-routing` to the `handleAPI`
    switch (Design Decision 7), and extend `workItemDetailForWeb` with the work item's recent
    `skill_family_routing` events; `queryMaps` with camelCase SQL aliases and
    `json_extract`/`json_each` guarded by `json_valid`.
11. `go-pic/web/src/lib/api.ts`: add the response interfaces and an `api.skillRouting(projectId)`
    method (and extend the `WorkItemDetail` interface).
12. `go-pic/web/src/routes/dashboard/+page.svelte`: fourth "Routing" tab — aggregate cards
    (total events, per-family match counts with matched-by tokens, missing occurrences) and a
    recent-events table reusing `.stats-grid`/`.table-scroll`/`.empty-state`; "Routing Events"
    section in `go-pic/web/src/routes/work-item/[id]/+page.svelte`.
13. Tests: `go-pic/cmd/pic/pic_cli_test.go` following the `webRequest` helper
    (`pic_cli_test.go:78-92`) and `TestWebAPISupportsDashboardContract`
    (`pic_cli_test.go:1992-2035`) patterns — seed rows via the real binary
    (`pic workflow event-add <id> skill_family_routing --payload-json '{...}'`), then assert
    the endpoint's aggregation math and the detail section's shape.
14. Rebuild the static dashboard: `cd go-pic/web && npm run build`.

## Non-Goals

- **No embeddings / re-ranker.** Revisit trigger: observe-mode events show recurring
  `missing_families` on real tasks after the catalog is surfaced to the planner.
- No lifecycle-mutating Go changes and no schema migrations (the skill-routing endpoint is
  read-only; telemetry reuses `workflow event-add`). No workflow-state or blocking changes.
  The dead `workflow-analytics` stub is flagged for a future plan, not implemented here.
- No golang vendor-skill pruning this pass (flagged in the audit doc).
- No user-level (`~/.pi`, `~/.agents`) content changes — the audit doc reports, it does not
  touch.

## Verification

- `cd pi-ext && pnpm test && pnpm run check && pnpm lint` — all green.
- `cd go-pic && go test ./...` — all green, including the new skill-routing handler tests.
- `cd go-pic/web && npm run build` — dashboard builds clean.
- Manual: trigger a worker launch on a real Work Item, then
  `pic workflow event-add`-emitted row visible via the `work_item_events` query path with the
  expected payload keys; confirm the task-graph handoff renders `<skill_family_catalog>`; confirm
  a Go-only task resolves the golang family and a non-UI task no longer receives shadcn-svelte;
  open `pic web` and confirm the Routing tab shows family counts, missing occurrences, and
  recent events, and the work-item detail page shows its Routing Events section.
