# Pi Task System Extension

## Installation

`pnpm-workspace.yaml` and `pnpm-lock.yaml` are the sole canonical installation authority for this extension. Install deterministically from the committed lockfile with a frozen pnpm install:

```
cd pi-ext
pnpm install --frozen-lockfile
```

No `package-lock.json` or npm-specific lock authority is used. If a frozen install reports lock drift, update the package metadata and lockfile together through pnpm (re-run `pnpm install` and commit the regenerated `pnpm-lock.yaml`) rather than bypassing frozen validation.

Scripts are invoked through pnpm and match the documented commands:

```
pnpm run build   # build the pi extension binary and dashboard
pnpm test        # run the Node test suite
pnpm run check   # TypeScript typecheck
```

`index.ts` is the package entry point. Implementation is grouped by feature and dependencies flow inward as follows:

- `core/`: process adapters, extension lifecycle, dashboard launcher, and activity publishing. Does not depend on other feature folders.
- `tasking/`: task contracts, workflow modes, prompts, artifacts, review context, and gates. Depends only on `core/`.
- `pipeline/`: persisted Worker/Reviewer scheduling and phased-task orchestration. Depends on `core/` and `tasking/`.
- `reporting/`: completion notes, verification evidence, and report rendering. Independent feature helpers.
- `subagent/`: task-system-owned agent discovery, isolated `pi` subprocess execution, JSONL progress streaming, and the barebone orchestration tool. It does not depend on another subagent extension.
- `ui/`: interactive task browser, search, settings, and work views. Consumes tasking, pipeline, and reporting features.
- `api/`: Pi command and `task_manager` tool adapters. This is the outer wiring layer and may consume every feature.
- `agents/` and `methodologies/`: packaged agent definitions and workflow reference material.

Tests live beside the feature they cover. Keep cross-feature imports pointed at the owning feature's public module rather than duplicating behavior.

## Canonical Work Items

The extension exposes Work Item actions only for lifecycle mutation. Use `create_work_item`, `update_work_item`, `update_work_item_status`, staged artifact actions, graph validation/materialization, explicit implementation authorization, and aggregate verification/closure. Archived Epic, Task, Task Item, Feature, and phase-repair paths are migration history, not mutation APIs.

The scheduler launches only authorized, dependency-ready executable Work Items. Aggregate Work Items never launch workers. Worker, Reviewer, Completion Report, and verification evidence remain bound to the active immutable TIP revision and content hash.
