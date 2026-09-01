# Skill Shelf Audit

Audited 2026-08-30 alongside the skill-family routing plan
(`docs/plans/skill-family-routing-plan.md`). This document inventories every
skill root the extension resolves from, flags dead weight, and explains how to
read the `skill_family_routing` telemetry. It reports only — nothing under
`~/.pi` or `~/.agents` was modified.

## Packaged catalog (`pi-ext/task-skills/`) — 4 families, 55 skills, 2.3M

| Family | Size | Skills | Mandatory | appliesTo tokens |
|---|---|---|---|---|
| `frameworks/sveltekit` | 196K | 7 | sveltekit-svelte-code-writer, sveltekit-svelte-core-bestpractices | svelte, sveltekit, svelte 5, svelte.config / .svelte, .svelte.ts |
| `frameworks/shadcn` | 88K | 1 | shadcn-svelte | shadcn, shadcn/ui, shadcn-svelte |
| `languages/golang` | 1.9M | 45 | golang-code-style, -error-handling, -testing, -safety | golang, go language, go.mod, go.work, go test, gofmt, golang.org / .go |
| `languages/typescript` | 12K | 1 | typescript-development | typescript, tsconfig / .ts .tsx .mts .cts |

Standalone baseline skill: `codebase-design/` (16K, adapted from
mattpocock/skills, MIT — no family.json).

`frameworks/shadcn` was vendored in this pass from `~/.agents/skills/shadcn-svelte`
(copy, no symlink — catalogs forbid them). **Provenance/divergence risk:** the
packaged copy no longer auto-tracks upstream; refresh it when
`~/.agents/skills/shadcn-svelte` changes. It deliberately has no `.svelte`
token so it routes only on shadcn-specific signals, not on every Svelte task.

## Baseline shelf (always loaded per persona, frontmatter `skills:`)

| Agent | Baseline skills |
|---|---|
| task-worker | test-first, verification-gate, testing-anti-patterns, ponytail, logging-best-practices |
| task-planner | write-plan, shape-spec, codebase-design |
| task-debugger | systematic-debugging, root-cause-tracing, test-first, defense-in-depth, verification-gate |
| task-scout | codanna-explore, codanna-review |
| task-reviewer | defense-in-depth |

Change in this pass: `shadcn-svelte` was removed from the task-worker baseline
(88K of UI-only guidance loaded by every worker); it is now routed via
`frameworks/shadcn` when a task mentions shadcn.

## User-level roots (read-only inventory)

- `~/.pi/agent/skills` — 1.4M, 38 SKILL.md, 21 top-level entries, 5 symlinks
  into `~/.agents/skills`. Contains `methodology/` (17 subskills supplying most
  agent baselines) and a `sveltekit/` corpus (7 skills, 192K, non-prefixed
  names) reachable only as baseline candidates and referenced by no persona.
- `~/.agents/skills` — 464K, 6 skills (codanna-explore, codanna-review,
  find-skills, logging-best-practices, shadcn-svelte,
  vercel-react-native-skills).
- `~/.pi/agent/git` — 5.0M; one real repo
  (`github.com/DietrichGebert/ponytail`, 6 skills) plus an empty
  `github.com/techmodv90/` directory.

## Dead weight flags (owner decisions pending; nothing deleted)

1. **Duplicate sveltekit corpus.** Packaged `frameworks/sveltekit` (196K,
   `sveltekit-`-prefixed) and global `~/.pi/agent/skills/sveltekit/` (192K,
   non-prefixed names) cover identical topics with **diverged content** — two
   maintained variants of the same material. Recommend deleting or archiving
   the global copy (out of repo scope).
2. **`playwright-best-practices`** (`~/.pi/agent/skills`, 880K — largest skill
   in any root) and **`design-taste-frontend`** (87K) are referenced by no
   persona and never auto-loaded.
3. **`vercel-react-native-skills`** (260K) has no `description` in its
   frontmatter and no persona references it.
4. **Empty git root entry** `~/.pi/agent/git/github.com/techmodv90/`.
5. **Ponytail duplication:** `ponytail/.openclaw/skills/` duplicates the 6
   skills already under `ponytail/skills/` (12 SKILL.md total).
6. **Golang vendor overlap (declined this pass).** ~12 of 45 golang skills are
   vendor-specific guides (samber-lo/-hot/-do/-mo/-ro/-oops/-slog, uber-dig,
   uber-fx, google-wire, spf13-cobra, spf13-viper) that overlap
   golang-popular-libraries and golang-dependency-injection topics. They are
   non-mandatory and on-demand; prune only if routing telemetry shows noise.

## Reading `skill_family_routing` telemetry

One `work_item_events` row per worker/autofix launch
(`event_type='skill_family_routing'`, `payload_json` snake_case: `stage`,
`pack_id`, `selected_families`, `matched_families` with `matched_by` tokens,
`missing_families`, `evidence_sources`). Per work item:
`pic workflow events <id>`. Cross-work-item, against `<repo>/.pi/tasks.db`:

```sql
-- Enforcement-decision signal: matched-but-unselected families per launch
SELECT json_extract(payload_json,'$.missing_families') AS missing, COUNT(*) AS n
FROM work_item_events
WHERE event_type='skill_family_routing'
GROUP BY 1 ORDER BY n DESC;

-- Token-quality feedback: which families fire and via which appliesTo tokens
SELECT je.value->>'$.id' AS family, je.value->>'$.matched_by' AS via, COUNT(*) AS n
FROM work_item_events,
     json_each(json_extract(payload_json,'$.matched_families')) je
WHERE event_type='skill_family_routing'
GROUP BY 1, 2 ORDER BY n DESC;
```

Interpretation: recurring non-empty `missing_families` after the planner began
receiving `<skill_family_catalog>` means the deterministic router needs either
better manifest tokens or (only then) the deferred embedding re-ranker. The
dashboard "Routing" tab shows the same aggregates.

## Related finding: dead workflow-analytics dashboard stub

`go-pic/web/src/lib/api.ts:124` calls `GET /api/projects/{id}/workflow-analytics`,
but no such case exists in the `handleAPI` dispatcher
(`go-pic/cmd/pic/misc.go`), so the dashboard Analytics tab always renders
"No workflow analytics recorded." Flagged for a future decision: implement the
endpoint or remove the tab. The new Routing tab is separate and fully wired.
