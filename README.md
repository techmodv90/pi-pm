# Agent Project Management (APM)

APM is the agent-driven project management and delivery system formerly named
**task-system**. The runtime identifiers (`pic` binary, `pi-task-system` package
paths, `task-*` agent filenames, SQLite `tasks.db`, `wi-*` Work Item IDs) are
kept as-is for compatibility; the product name is APM.

## What it is

A staged, gated delivery pipeline run by agents with owner checkpoints:

```
Work Item → Scan → RRI → Vision → Blueprint → Contracts → Task Graph
         → materialize → authorize → implement → review → verify → accept → merge
```

- **Work Items** (Epic / Feature / Task / Bug / Chore / Gate) are tracked in a
  canonical SQLite store (`~/.pi/task-system/.pi/tasks.db`).
- **Planning artifacts** are immutable, content-hashed records with owner
  approval gates.
- **Managed execution** runs workers in isolated git worktrees, passes a review
  gate, and records contractor verification evidence before closure.

## Layout

- `go-pic/` — Go CLI (`pic`): lifecycle authority, HTTP API, embedded dashboard
- `pi-ext/` — Pi extension: scheduler, runner, reviewers, prompt surfaces
- `docs/` — specs (`work-item-model-spec.md`, `aggregate-delivery-workflow-spec.md`, …)

## Commands

```bash
cd go-pic && go build -o dist/pic ./cmd/pic   # rebuild CLI
cd pi-ext && pnpm run build && pnpm test      # extension build + tests
cd go-pic && go test ./...                    # CLI tests
```
