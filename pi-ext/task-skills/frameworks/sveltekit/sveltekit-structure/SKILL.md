---
description: SvelteKit structure guidance. Use for routing, layouts, error handling, SSR, or svelte:boundary. Covers file naming, nested layouts, error boundaries, pending UI, and hydration.
metadata:
    github-path: sveltekit-structure
    github-ref: refs/heads/main
    github-repo: https://github.com/spences10/skills
    github-tree-sha: fd99d9b5614049af4ab9c98ec8c6092bf7cfec6b
    last_updated: "2026-05-31"
    verified_against: Svelte 5 official docs and sveltejs/svelte#18282
name: sveltekit-structure
---
# SvelteKit Structure

## Quick Start

**File types:** `+page.svelte` (page) | `+layout.svelte` (wrapper) |
`+error.svelte` (error boundary) | `+server.ts` (API endpoint)

**Routes:** `src/routes/about/+page.svelte` → `/about` |
`src/routes/posts/[id]/+page.svelte` → `/posts/123`

**Layouts:** Apply to all child routes. `+layout.svelte` at any level
wraps descendants.

## Example

```
src/routes/
├── +layout.svelte              # Root layout (all pages)
├── +page.svelte                # Homepage /
├── about/+page.svelte          # /about
└── dashboard/
    ├── +layout.svelte          # Dashboard layout (dashboard pages only)
    ├── +page.svelte            # /dashboard
    └── settings/+page.svelte   # /dashboard/settings
```

```svelte
<!-- +layout.svelte -->
<script>
	let { children } = $props();
</script>

<nav><!-- Navigation --></nav>
<main>{@render children()}</main>
<footer><!-- Footer --></footer>
```

## Reference Files

- [file-naming.md](references/file-naming.md) - File naming
  conventions
- [layout-patterns.md](references/layout-patterns.md) - Nested layouts
- [error-handling.md](references/error-handling.md) - +error.svelte
  placement
- [svelte-boundary.md](references/svelte-boundary.md) -
  Component-level error/pending
- [ssr-hydration.md](references/ssr-hydration.md) - SSR and
  browser-only code

## Notes

- Layouts: `{@render children()}` | Errors: +error.svelte _above_
  failing route
- Groups: `(name)` folders don't affect URL | Client-only: check
  `browser`
- **Last verified:** 2026-05-14

