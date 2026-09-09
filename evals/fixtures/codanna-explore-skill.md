---
name: codanna-explore
description: Codebase exploration and code-intelligence skill — read the FULL SKILL.md body with the read tool before answering any "how does X work" / "how is X implemented" question, any request to explore, map, or trace a feature, subsystem, or flow ("explore the dispatch pipeline", "trace the auth flow"), any callers/call-graph/impact question — "who calls X", "what uses X", "what breaks if I change X" — which is this skill's core use case, never a grep job, and any semantic find-the-code request ("find where we handle X", "where is the code that does Y"). Also read it BEFORE running any rg/grep/find whose purpose is understanding code — callers, usage, blast radius, or where behavior lives — because grep cannot answer those; only a literal-string locate ("find all occurrences of X") is rg territory. Do not load this skill for writing code, doc edits, or literal text-pattern searches.
---

# Codanna Explore

Topic-driven codebase exploration via codanna's local code intelligence
(semantic search + tree-sitter symbol graph + RAG over docs).

Docs: https://docs.codanna.sh/  ·  Repo: https://github.com/bartolli/codanna
Workflow diagram & rationale: [README.md](README.md)

## When to use

- User gives a topic: "explore the auth flow", "map the parser", "how does X work".
- Need call graph, callers, or impact analysis for a function / type.
- Need semantic ("find code that does Y") search, not just grep.

**Don't use** for: regex / text-pattern searches (use `rg`, fall back to
`grep`), nix lookups (`search-nix-manix`), or trivial single-file questions.

## Tool surface (prefer MCP, fall back to CLI)

| Purpose | MCP tool | CLI |
|---|---|---|
| Status check | `get_index_info` | `codanna mcp get_index_info --json \| toon` |
| Anchor in code | `semantic_search_with_context` | `codanna mcp semantic_search_with_context query:"…"` |
| Semantic over docstrings | `semantic_search_docs` | `codanna mcp semantic_search_docs query:"…" limit:5` |
| Fuzzy symbol search | `search_symbols` | `codanna retrieve search "…" --kind function --limit 10` |
| Exact symbol lookup | `find_symbol` | `codanna retrieve symbol <name>` |
| Symbol details | — | `codanna retrieve describe <name>` |
| Outgoing calls | `get_calls` | `codanna retrieve calls <name\|symbol_id:N>` |
| Incoming callers | `find_callers` | `codanna retrieve callers <name\|symbol_id:N>` |
| Trait implementors | — | `codanna retrieve implementations <Trait>` |
| Full dependency graph | `analyze_impact` | `codanna mcp analyze_impact <name>` |
| Docs RAG search | `search_documents` | `codanna documents search "…" --collection <name>` |
| List doc collections | — | `codanna documents list` |

Output flags: `--json`, `--fields=<top-level-keys>`, `-k/--kind`,
`-l/--limit`, `-m/--module`. Add `--watch` to `codanna mcp …` to reindex stale
files before the call.

**`--fields=` only matches top-level JSON keys**, not nested paths. For
`retrieve search`-style results the top level is `symbol`, `file_path`,
`relationships` — use `--fields=symbol,file_path` (not
`--fields=name,symbol_id,file_path`, which silently drops everything but
`file_path`). The symbol's ID lives at `symbol.id`, but the CLI/MCP **input
parameter** is `symbol_id:N` (name mismatch is intentional in codanna).

**Default text output is often better than `--json`**: codanna's default
formatter is compact, shows similarity scores, inlines relationships, and most
tools end with a 💡 hint pointing to the next useful call (e.g. "Explore
'get_calls' to see what it calls"). Read those hints — they save round-trips.
`search_documents` is the one exception that doesn't emit a 💡 line. Reach for
`--json | toon` only when you need to extract `symbol.id`s programmatically or
paginate large result sets.

**Always suppress stderr** when piping JSON through `toon`:
`codanna ... --json 2>/dev/null | toon`. Tantivy emits lock-busy warnings on
stderr that break `toon`'s JSON parser.

## Workflow

1. **Status check** — `get_index_info` and `codanna documents list`. Branch
   into bootstrap if `symbol_count: 0` or no docs collections exist.
   **Note**: `semantic_search.enabled: false` in `get_index_info` is misleading
   — the embedding model loads on demand. Try `semantic_search_*` anyway; only
   fall back to fuzzy `search_symbols` if it actually errors.

2. **Bootstrap if missing** (skip questions the user has already answered):
   - **2a. Code** — if not indexed, autodetect code dirs. Standard list:
     `src lib app cmd internal pkg`. For JS/TS / web projects also consider
     `pages components routes app server client`. If nothing standard matches,
     `ls` the repo root and ask the user which dirs to index. Then `codanna
     init` + `codanna index <dirs>`. User refusal = stop; exploration needs code.
     `codanna init` also drops a `.codannaignore` (gitignore-style file for
     codanna's indexer, with sensible defaults) — leave it committed.
   - **2b. Docs** — if no collection, autodetect docs targets: directories
     `docs openspec ADRs adrs`; or, if only a root `README.md` (no docs/
     directory), use `codanna documents add-collection readme . --pattern
     "README.md"`. Ask y/n, then `codanna documents index --all`. User refusal
     or nothing detected = skip step 3.
   - **2c. `.gitignore` tip** — if any bootstrap actually ran, **append**
     (don't overwrite) these to the repo's `.gitignore`:
     ```
     .codanna/
     .fastembed_cache
     ```
     `.codanna/settings.toml` contains absolute paths (`workspace_root`,
     `indexed_paths`) and machine-specific config, so the whole `.codanna/`
     directory must stay local. `.fastembed_cache` is a symlink into
     `~/.codanna/models/` — broken on every other machine. Leave
     `.codannaignore` committed (project-shared).

3. **Harvest concepts from docs** (only when a docs collection exists) —
   `search_documents query:"<topic>"`. Read the previews and **freeform-extract
   concepts and likely symbol names** related to the query — no fixed schema,
   whatever looks useful. Feed into step 4. Note: enriching the anchor query
   often *shifts* the result set toward the harvested terms rather than
   simply boosting recall; compare with the bare query if you suspect drift.

4. **Anchor in code** — `semantic_search_with_context query:"<topic + harvested
   terms>"`. Returns the highest-quality entry points (file + symbol +
   relationships). **For top hits, call graph / callers / impact are inlined**
   in the response — step 6 may be unnecessary for those symbols. No hits →
   rephrase once, then fall back to a regex search with `rg`/`grep` (step 8).

5. **Lock + disambiguate symbols** — `find_symbol` (exact) or `search_symbols
   kind:function|struct|trait|class|method|interface`. For same-name collisions,
   grab the symbol ID from `--json` output (the field path is `data[].symbol.id`
   for `retrieve search`) and pass it as `symbol_id:N` downstream. You can
   shrink the payload with `--fields=symbol,file_path` (top-level keys only).

6. **Map relationships** — treat as **hints**, not truth (codanna is static):
   - `get_calls <sym|symbol_id:N>` — outgoing dependencies.
   - `find_callers <sym|symbol_id:N>` — incoming uses.
   - `analyze_impact <sym|symbol_id:N>` — full blast radius. Usually small
     (10-30 lines in practice); call it freely and narrow with `find_callers`
     only if `data` is actually large. Often redundant when step 4 already
     inlined relationships.

7. **Read source** — `zat <file>` for signatures-first skim, then
   `Read(offset, limit)` into specific bodies. Use `ctx_read` or `Read` when
   you need full text.

8. **Always-on regex sanity pass** — run `rg` (preferred) or `grep` on the
   topic keyword + harvested terms from step 3. Catches what static analysis
   misses: macros, dynamic dispatch, FFI, generated code, string-based
   references. **Run every time, not just when you suspect gaps.** If new
   symbols surface, loop back to step 5.

9. **Report (text)** — entry points as `path:line`, call graph summary,
   surprising coupling, gaps where codanna's static view looked incomplete,
   plus any regex findings from step 8.

10. **Offer a markdown+mermaid report** (only if the `mermaid-diagrams` skill
    is listed as available) — after delivering the text report, ask the user
    whether they want a richer artifact with diagrams. Good candidates:
    - **Call/dependency graph** (flowchart) from `get_calls` / `find_callers` /
      `analyze_impact` data.
    - **Sequence diagram** for traced execution flow through the entry points.
    - **Component / module diagram** when several modules are involved.
    - **Class diagram** for struct / trait / impl relationships.
    On yes → invoke the `mermaid-diagrams` skill and emit a markdown file with
    embedded fenced ` ```mermaid ` blocks. On no → done.

## Companion tools (prefer if installed)

Run `command -v <tool>` once per session, then route accordingly.

| Tool | Use for | Example |
|---|---|---|
| `toon` | Codanna `--json` output you actually need to parse (extracting `symbol.id`, large result sets). **Default text is usually better** — see note above. | `codanna retrieve search "parse" --json --fields=symbol,file_path 2>/dev/null \| toon` |
| `zat` | Signature-first file reading (vs full `cat` / `Read`). Supports C, C++, C#, Go, Haskell, Java, JS, Kotlin, MD, Python, Ruby, Rust, Swift, TS/TSX. Exit 1 on unsupported. | `zat src/parser.rs` then `Read(offset, limit)` on interesting line numbers |
| `rg` (ripgrep) | **Default for any regex / text-pattern search** — sanity pass for what codanna missed (macros, dynamic dispatch, FFI, generated code) | `rg -nP --type rust 'macro_rules!\s*\w+' src/` |
| `grep` | Regex fallback when `rg` not on PATH | `grep -RnE 'pattern' src/` |

Routing rule: codanna first (structural understanding) → `rg` (or `grep` if
`rg` is unavailable) for any regex or text-pattern search.

## Output shape

All MCP / `retrieve` JSON wraps results in
`{type, status, code, exit_code, message, hint, data, meta}`. The shape of
`data` **varies by command** — don't assume a single path:

| Command | `data` shape | How to extract |
|---|---|---|
| `get_index_info` | object `{symbol_count, file_count, ..., semantic_search:{}}` | `data.symbol_count` |
| `find_symbol <name>` (found) | object `{symbol:{}, file_path, relationships:{}}` | `data.symbol.id`, `data.file_path` |
| `find_symbol <name>` (missing) | `null` (status: `not_found`) | read `hint` field |
| `retrieve search …` / `search_symbols` | array `[{symbol:{}, file_path, relationships:{}}, …]` | `data[N].symbol.id` |
| `mcp get_calls` / `find_callers` | array `[{id, name, kind, file_path, …}, …]` (flat — no `symbol` wrapper) | `data[N].id` |
| `search_documents` | array `[{chunk_id, collection, content_preview, source_path, …}, …]` | `data[N].source_path` |

- `status: "not_found"` → `data: null`; **read the `hint` field** — it tells
  you the next useful move (e.g. "try semantic_search_docs"). Don't retry the
  identical query.
- Most default-text outputs end with a `💡` line carrying the same nudge.
  `search_documents` is the exception — it has no trailing 💡.

## Common pitfalls

| Pitfall | Fix |
|---|---|
| Semantic search returns nothing | Threshold is 0.6; rephrase, drop jargon, or fall back to `search_symbols` / `rg`. |
| `get_index_info` says `semantic_search.enabled: false` | Misleading — the model loads on demand. Just call the semantic tool; only fall back if it errors. |
| `search_documents` previews contain `[1;36m...[0m` garbage | ANSI escape codes (search-term highlighting) appear in **both** default text *and* `--json content_preview` fields. For machine reads, strip with `sed 's/\x1b\[[0-9;]*m//g'`. For human reads, accept the ANSI (terminals render it). Tantivy lock-busy warnings on stderr separately need `2>/dev/null`. |
| `--fields=name,symbol_id,file_path` returns only `file_path` | `--fields` matches top-level JSON keys only, not nested paths. Use `--fields=symbol,file_path` and dig into `data[].symbol.id` afterward. |
| `analyze_impact` output worry | Often small (~20 lines). Just call it — check `data.items` length if it actually returns a lot, then narrow with `find_callers`. |
| Wrong symbol picked by name | Use `symbol_id:N` from a `--json --fields=name,symbol_id,file_path` search. |
| Macro / dynamic call missed | Codanna is static — verify with `rg` on suspicious gaps. |
| Re-running same query after `not_found` | Read the `hint` field (or 💡 line) instead. |
| Stale index after edits | `codanna index <dir>` or `codanna mcp … --watch`. |
| `openspec/` folder ignored | Add it as an `openspec` documents collection (per global CLAUDE.md). |
