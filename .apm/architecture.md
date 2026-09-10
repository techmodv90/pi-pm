# Architecture — task-system

<!-- Single evolving doc. /apm explore updates sections in place; see pi-ext/core/prompts/apm-explore.md for the update semantics. -->

## Coverage

| Section | verified_at | Confidence |
|---|---|---|
| Repo-wide | 1454cf3 | HIGH |

## Repo-wide

Explored: 2026-09-08 (medium depth, Assumptions Mode, empty input → whole repo). Absorbs `.apm/specs/distilled/*.md` as the descriptive layer (owner decision, option A).
Scope: repo tree, Go CLI dispatch, internal Go packages, pi-ext module layout, scheduler/dispatch seams, `.apm` workspace, working-tree state. ≈30 files surveyed.

### Stack Detected

- **Go 1.26** CLI (`pic`) — only external dep `modernc.org/sqlite v1.53.0` (pure-Go SQLite); Go standard `net/http` for the dashboard (`go-pic/go.mod`)
- **TypeScript ^6.0.3** pi extension (`pi-ext`), pnpm workspace, Node built-in test runner for `*.test.ts`
- **SvelteKit** dashboard (`go-pic/web`, static adapter) — present, not deep-dived here
- **pi extension API** (`@mariozechner/pi-coding-agent`); subagent execution via pi-subagents-lite Agent tool
- Data: single SQLite DB `.pi/tasks.db` with ordered migrations

### Structure

```
task-system/
├── go-pic/                      # Go CLI
│   ├── cmd/pic/                 # thin dispatch layer (main 70, workflow 119, work_items 106 LoC)
│   ├── internal/                # 13 packages, ~10,750 LoC, all ≤400 LoC per file
│   │   ├── work-item/           # 21 files, 4,077 LoC (items/execution/lifecycle/artifacts/
│   │   │                        #   status/aggregate/authorize/actions/support) — largest domain
│   │   ├── pipeline/            # 7 files, 1,296 LoC (claim/complete/escalation/diagnostics)
│   │   ├── apm/                 # 4 files, 861 LoC (parse/graph/import)
│   │   ├── tip/                 # 2 files, 751 LoC (pre-existing, pre-400-rule)
│   │   ├── schema/              # 8 files, 1,006 LoC (migrations, backfills, canonical DDL)
│   │   ├── project/ store/ dashboard/ acceptance/ profile/ stage/ activity/ instruction/
│   └── web/                     # SvelteKit dashboard
├── pi-ext/                      # pi extension (one composition root, pi-ext/index.ts)
│   ├── api/        10 files, 1,131 LoC   # task-manager MCP tool, dispatch/review/aggregate actions
│   ├── core/       29 files, 1,847 LoC   # /apm command router + handlers + prompts
│   ├── pipeline/   20 files, 3,117 LoC   # scheduler, dispatch seam, stage resolution, RRI-T
│   ├── tasking/    11 files, 1,266 LoC   # work-item prompts, settings, workflow modes
│   ├── subagent/   11 files, 1,416 LoC   # runner, worktrees, spawn utils
│   └── agents/ skills/ methodologies/ reporting/ ui/
├── .apm/                        # spec workspace (prd/, specs/{workflow,artifacts,db_schema,distilled}/, state/)
├── docs/                        # ADRs, plans, progress/gap.md ledger
├── evals/                       # trigger evals + fixtures (some uncommitted)
└── plans/                       # legacy hand-written plans
```

### Conventions Found

- **Go:** thin `cmd/pic` dispatch → `internal/<domain>` logic; ≤400 LoC per file (300 optimal); gofmt; errors returned from handlers; package-level unit tests per domain (table-driven, named subtests); SQLite persisted fields snake_case
- **TypeScript:** ESM imports; camelCase API fields; `*.test.ts` siblings under `node --experimental-strip-types --test`; typescript-eslint with `no-explicit-any` as error in `pipeline/pic-show.ts`
- **/apm commands:** house handler pattern — `hasApmWorkspace` gate → call-time `loadPrompt()` → `{INPUT}` substitution → `sendHiddenPrompt`; prompts are markdown in `core/prompts/apm-<name>.md`; router is one `if (sub === ...)` chain in `core/apm-command.ts`
- **Workflow:** lean — Work Items imported from `.apm` specs via `pic workflow import-apm`; stages worker → review → contractor verification; legacy planning (scan/rri/vision/blueprint/contracts/task_graph/TIP) deleted; aggregates verified via `/apm review` + in-session RRI-T scenarios
- **Scheduler:** never spawns processes; writes dispatch records; contractor binds Agent-tool agent ids and completes dispatches via `task_manager`
- **Adoption language (owner directive 2026-09-08):** /apm commands adopted from don-cheli-sdd carry `i18n: true` in source — adoption MUST translate all labels, tags, states, and CLI flags to English (`APPROVED`, `PENDING`, `CRITICAL`, `--threshold`); Spanish remnants in existing prompts (propose, design, pseudocode, drift, spec-score) are a known debt pending the gate-strengthening spec
- **Spec layout schema:** `.apm/specs/features/<domain>/<Name>.feature` is the canonical feature location (features/ tier separates forward specs from artifacts/, distilled/, workflow/); new specs write here; old paths matched for read compatibility during transition

### Relevant Existing Functionality

- **Internal port complete:** every Go domain has an `internal/<domain>` package with tests (`cmd/pic/workflow.go:20-40` dispatch switch; `cmd/pic/work_items.go` 106 LoC)
- **Agent-tool dispatch seam:** `pi-ext/pipeline/pipeline-dispatch.ts` (hard-gated `run_in_background`, `bindPipelineDispatch` at :65); scheduler bind/complete at `pipeline-scheduler.ts:96-99`; lean routing at `stage-resolution.ts:232`
- **Lean spec sources:** `.apm/specs/workflow/ApmWorkItemImport.{feature,plan.md,tasks.md}`, `LeanTaskClaim.*` — the canonical description of the import → authorization → handoff flow
- **Aggregate machinery:** `internal/work-item/aggregate.go` (verify/accept/merge/close), `pipeline/rri-t.ts` (scenario grading)
- **Gap ledger:** `docs/progress/gap.md` records every justified direct sqlite call / CLI bypass (RLB-GAP-001..008)

### Confirmed Assumptions (Assumptions Mode)

- S1 ✅ Two-component monorepo (Go CLI + pi extension) plus `.apm`/docs/evals/plans data dirs; single git repo — **HIGH**
- S2 ✅ `cmd/pic` is thin dispatch; all Go logic lives in `internal/<domain>` packages, ≤400 LoC per file, tested per package — **HIGH**
- S3 ✅ pi-ext is one extension with a single composition root (`index.ts`, 12 registrations); `/apm` commands follow the prompt+handler+router pattern — **HIGH**
- S4 ✅ Scheduler dispatches all stages through the Agent-tool dispatch seam; contractor drives bind/complete via `task_manager`; failed review verdicts report as completed stages — **HIGH**
- S5 ✅ Lean workflow only: spec import → worker → review → verification; legacy planning machinery deleted; aggregates use `/apm review` + RRI-T — **HIGH**
- S6 ✅ Working tree carries a large uncommitted pi-ext refactor split (+271/−1,979 across 8 modified files, 23 untracked split files); new work must build on the split files — **HIGH**
- S7 ✅ Persistence: single SQLite `.pi/tasks.db`, ordered idempotent migrations in `internal/schema`; snake_case persisted fields with noted exceptions — **MEDIUM**

### Open Questions

- **Base commit for next work:** the S6 refactor split is uncommitted — must it be committed/reviewed before any new proposal builds on it?
- **Role of `plans/`:** three legacy plan files at repo root (`plans/sveltekit-rewrite.md`, `localhost-project-management-web-app.md`, `feature-workflow-pipeline.md`) — active inputs or historical only?
- **evals fixtures status:** several untracked `evals/fixtures/*.md` (ADR-FORMAT, AGENTS, CONTEXT-FORMAT, domain-modeling-skill) — pending commit or discarded?

### ⚠️ Notes

- `internal/tip/tip.go` is 753 LoC — pre-existing over the 400 ceiling, left per the surgical rule; splitting is an offered follow-up
- Child-agent mutation is gated by `PI_TASK_AGENT_NAME` read-only allowlist (`cmd/pic/workflow.go:22`) — new workflow subcommands must be added to the allowlist explicitly if agents should read them
- Every direct sqlite call or CLI bypass must be recorded in `docs/progress/gap.md` with justification (standing contractor rule)
- Never commit to an integration branch while a pipeline run is in flight (review base changes block integration)
