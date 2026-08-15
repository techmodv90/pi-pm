---
name: golang-gopls
description: "Golang semantic code intelligence via `gopls`, the official Go language server. Use its MCP server when available or its CLI for navigation, references, diagnostics, safe renames, refactors, formatting, and local package API discovery. Use when navigating or refactoring resolved Go workspace code. For the published ecosystem use `golang-pkg-go-dev`; for vulnerability audits use `golang-security`."
license: MIT
---

**Persona:** You are a Go engineer who reaches for semantic code intelligence instead of grep whenever a question is about the resolved build — grep finds text, `gopls` finds meaning (types, call graphs, shadowing, implementation relationships).

**Dependencies:** `gopls` — `go install golang.org/x/tools/gopls@latest` (v0.20+). The MCP tools require a configured `gopls mcp` server; otherwise use the CLI (see [references/mcp.md](references/mcp.md)).

`gopls` is the official Go language server. It only answers questions about **your specific, locally resolved build** — your workspace plus every dependency exactly as pinned in `go.sum`, including `replace` directives. For a package that isn't part of that build (versions, docs, licenses, CVEs of something you haven't added yet), → See `golang-pkg-go-dev` skill (`godig`) instead.

## Two ways to reach gopls

- **gopls MCP server (preferred when configured)** — tools accept names, file paths, and fuzzy queries. It runs headless over stdio and only sees files saved to disk. See [references/mcp.md](references/mcp.md).
- **gopls CLI** — invoke `gopls <command> <file:line:col>` for one-shot checks when MCP is unavailable. Positions are `file:line:col` (1-indexed UTF-8 bytes) or `file:#offset` (0-indexed). See [references/cli.md](references/cli.md).

Full capability mapping: [references/matrix.md](references/matrix.md).

## Use cases

- **Navigation** — jump to a definition, an implementation, or trace a call graph before touching code you didn't write. Details: [references/features.md](references/features.md#navigation).
- **Code discovery** — learn a workspace's shape (`go_workspace`), fuzzy-search a symbol you can't place exactly (`go_search`), or read a dependency's public surface (`go_package_api`) before using it.
- **Documentation** — hover for type/doc/size info, signature help while calling a function, or browse rendered package docs (`source.doc`, including internal packages pkg.go.dev never sees).
- **Diagnostics & safety** — compiler and analyzer errors after every edit (`go_diagnostics` / automatic with `LSP`), plus a lightweight `go_vulncheck` reachability check: once as a baseline right after detecting the workspace, and again after any `go.mod` change.
- **Formatting** — canonical `gofmt`-equivalent formatting and import organization, both scriptable and code-action-driven.
- **Refactoring** — safe rename (blocks a change that would break interface satisfaction), extract/inline, and the full `refactor.rewrite.*` family (fill struct/switch, invert if, split/join lines, remove unused parameter, add struct tags, implement interface). Full catalog with gotchas: [references/features.md](references/features.md#transformation).

## Efficient workflows

These Read/Edit workflows encode the order that avoids redundant queries and half-applied edits — treat every step as required, not optional, even to save a round trip.

- **Session start** — call `go_workspace` once to detect whether this is a Go workspace at all; if it is, immediately follow with a baseline `go_vulncheck` to surface vulnerabilities the workspace already carries. This is unconditional, separate from the edit workflow's later check after a dependency change.

**Read workflow** (understand before touching anything):

1. `go_workspace` — layout (module/workspace/GOPATH); same call as the session-start check above if it hasn't run yet.
2. `go_search` — fuzzy-locate a type/function/variable by name.
3. `go_file_context` — right after reading any Go file for the first time, see what it pulls in from the rest of its package; re-run if that file's dependencies change.
4. `go_package_api` — a third-party dependency's or sibling package's public surface, without reading every file.

**Edit workflow** (iterate until diagnostics are clean):

1. Read first (workflow above).
2. `go_symbol_references` before modifying any definition — judge the blast radius, then read every referencing file that needs a matching edit.
3. Make all planned edits, including the reference-site edits, before moving on.
4. `go_diagnostics` on every changed file — mandatory after each modification, not an optional cleanup pass.
5. Fix reported errors: review any suggested quick-fix diff before applying, then re-run diagnostics to confirm the fix landed. Ignore hint/info diagnostics unrelated to the task. A diagnostic message can paraphrase the surrounding source rather than quote it verbatim.
6. Only if `go.mod` dependencies changed, run `go_vulncheck` on the whole workspace — after diagnostics are clean, not before.
7. Run `go test <changed-package-paths>` — not `./...` unless explicitly asked, since a full-repo run slows the iteration loop.

**Gotchas worth knowing before you rely on a result:**

- `references` results only reflect the **build configuration of the queried file** — a query on `foo_windows.go` will not surface matches in `bar_linux.go`; re-run under the relevant `GOOS`/build tags if a cross-platform result is missing.
- `call_hierarchy` only shows **static** calls — calls through function values or interface methods are invisible to it; corroborate with `references` when the call site matters.
- Extract/inline refactors are less rigorous than rename: comments are sometimes dropped, and generated files marked `DO NOT EDIT` receive no code actions at all.
- `refactor.rewrite.fillStruct` searches only the current file above the cursor and needs the struct's package already imported — run `source.organizeImports` first if the type was just typed in.

## gopls vs godig vs Context7 vs govulncheck

`gopls` only reasons about code present and resolvable in the local build:

- For anything not tied to that build (version history, license, ecosystem-wide importers, CVEs of a package not yet added) → See `golang-pkg-go-dev` skill (`godig`) — it queries pkg.go.dev directly, no local checkout needed.
- For a comprehensive, whole-tree vulnerability audit (CI gates, periodic sweeps) rather than gopls's lightweight on-demand `go_vulncheck` → See `golang-security` skill (`govulncheck`).
- Context7 remains a fallback for non-Go docs or a Go module not indexed on pkg.go.dev.

Use `golang-pkg-go-dev` for published package documentation and `golang-security` for whole-tree vulnerability checks.
