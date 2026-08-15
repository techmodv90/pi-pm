# gopls MCP server reference

## Starting the server

A standalone `gopls` instance can speak MCP over stdin/stdout:

```bash
gopls mcp
```

Configure that command in the Pi MCP server used by the current environment. The server only sees files as they exist on disk.

## MCP tools

| Tool | Purpose |
| --- | --- |
| `go_workspace` | Detect module, workspace, or GOPATH layout. |
| `go_vulncheck` | Check which known vulnerabilities the current build reaches. |
| `go_search` | Fuzzy-search workspace symbols. |
| `go_file_context` | Summarize a file's same-package dependencies. |
| `go_package_api` | Show a local or resolved dependency's public API. |
| `go_symbol_references` | Find references before changing a symbol. |
| `go_diagnostics` | Return build and analyzer diagnostics for files. |
| `go_rename_symbol` | Rename a symbol and its references with language-server checks. |

Use `functions.mcp` to discover the configured tool names before calling them; servers may namespace these names.

## Boundaries

The MCP server can read source files and invoke Go tooling to resolve package metadata. This may access the configured Go proxy and write to Go caches. It reasons only about the current workspace and dependencies resolved by `go.mod`, `go.sum`, `go.work`, and `replace` directives.

For packages outside the local build, use the `golang-pkg-go-dev` skill.
