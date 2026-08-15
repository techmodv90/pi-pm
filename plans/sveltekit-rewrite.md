# Plan: Rewrite Web Dashboard to SvelteKit

## Goal
Replace the vanilla JS/HTML/CSS frontend (`cli/web-assets/`) with a SvelteKit app built with `adapter-static` for SPA mode, keeping the existing Node.js API backend unchanged.

## Architecture
- New SvelteKit app in `cli/web/` with SPA fallback (`adapter-static`, `fallback: 'index.html'`)
- SvelteKit's client-side router handles all navigation
- API calls go to the same origin (`/api/...`) via a typed fetch client
- Built output goes to `cli/web/build/`; the existing `static.ts` server is updated to serve from there
- Existing backend (`server.ts`, `api.ts`, `dashboard-service.ts`) stays untouched

## Tasks

### Task 1: Scaffold SvelteKit project
**Files**: `cli/web/package.json`, `cli/web/svelte.config.js`, `cli/web/vite.config.ts`, `cli/web/src/app.html`
**Steps**: Create directory, write config files, install dependencies

### Task 2: Port API client + stores
**Files**: `cli/web/src/lib/api.ts`, `cli/web/src/lib/stores.ts`
**Steps**: Typed fetch wrapper mirrored from current `api()`, Svelte writable stores for projects/tasks/filters

### Task 3: Port CSS + app.css
**Files**: `cli/web/src/app.css`
**Steps**: Move existing `app.css` into SvelteKit, add `select option` fix, remove `.view` styles

### Task 4: Create layout with sidebar
**Files**: `cli/web/src/routes/+layout.svelte`
**Steps**: Welcome page for no project, layout stores + sidebar fetching projects

### Task 5: Create welcome page
**Files**: `cli/web/src/routes/+page.svelte`
**Steps**: Simple welcome message, links to select project

### Task 6: Create dashboard page with tabs
**Files**: `cli/web/src/routes/dashboard/+page.svelte`
**Steps**: Stats grid, action bar (create epic/task), tab panels (tasks/epics/workflow), filter bar, create forms

### Task 7: Create task detail page
**Files**: `cli/web/src/routes/task/[id]/+page.svelte`
**Steps**: Task metadata, description/notes, checklist with toggle, feature gates, actions (status update, add item)

### Task 8: Create search page
**Files**: `cli/web/src/routes/search/+page.svelte`
**Steps**: Search results from query param, result cards, click to navigate

### Task 9: Update server to serve SvelteKit build
**Files**: `cli/src/web/static.ts`
**Steps**: Change `ASSETS_DIR` to point at `cli/web/build/`

### Task 10: Build and verify
**Steps**: Build SvelteKit, build CLI, restart dashboard, smoke test endpoints
