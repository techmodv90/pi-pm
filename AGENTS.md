# Project Instructions

## 10. Project context

**Mandatory first-run gate:** Before doing task work, inspect this repository and replace every TODO below with verified project-specific values. Derive commands and conventions from manifests, configuration, source layout, and existing tests; never guess. Delete fields that do not apply.

### Stack
- Language and version: Go 1.26; TypeScript 6; Svelte 5
- Framework(s): Go standard `net/http`; SvelteKit with static adapter; Pi extension APIs
- Package manager: Go modules; npm
- Runtime / deployment target: Native `pic` binary with embedded/static dashboard; Node.js Pi extension

### Commands
- Install: `cd go-pic && go mod download && cd web && npm ci && cd ../../pi-ext && npm ci`
- Build: `cd pi-ext && npm run build`
- Test (all): `cd go-pic && go test ./... && cd ../pi-ext && npm test`
- Test (single file): `cd go-pic && go test ./cmd/pic -run <TestName>` or `cd pi-ext && node --experimental-strip-types --test <file>.test.ts`
- Lint: None configured
- Typecheck: `cd pi-ext && npm run check`
- Run locally: `cd go-pic && go run ./cmd/pic <command>`; dashboard: `cd go-pic/web && npm run dev`

### Layout
- Source lives in: `go-pic/cmd/pic`, `go-pic/web/src`, and `pi-ext`
- Tests live in: `go-pic/cmd/pic/*_test.go` and `pi-ext/**/*.test.ts`
- Do not modify: `backup/typescript-cli`; generated `go-pic/web/build`; generated `go-pic/dist/pic`

### Conventions specific to this repo
- Naming: Go idioms in CLI; camelCase API fields; snake_case persisted SQLite fields
- Import style: gofmt-managed Go imports; ESM TypeScript imports
- Error handling pattern: return Go errors from CLI handlers; JSON HTTP errors with matching status codes
- Testing pattern and framework: Go `testing`; Node built-in test runner; real temporary SQLite databases for CLI tests

### Forbidden
- Do not restore a Node/TypeScript CLI fallback or bypass the canonical Work Item workflow.

## 11. Project Learnings

Add concrete, project-specific corrections here when an agent mistake reveals a missing rule. Tighten an existing rule instead of duplicating it.

- Present owner-facing Scan reports as rendered Markdown headings and lists; never wrap the report in a fenced `text` code block.

- Never work around a workflow blocker by filtering, relabeling, or bypassing state. Trace the persisted state transition first; valid handoffs must use an explicit workflow state, and every fix requires a regression test before retrying.
- Never infer an extension runtime reload from a repeated pipeline blocker notification; after scheduler source changes, require explicit reload confirmation before retrying.
