# Plan: Localhost Web App for Centralized Project Task Management

## Goal
Build a local-only web dashboard that centralizes task-system management across all registered projects while keeping each project’s existing `.pi/tasks.db` and `.pi/task-system/projects.json` as the source of truth.

## Architecture
Add a `pic web` command in the CLI package that starts an HTTP server bound to `127.0.0.1` by default. The server reads a global project registry, opens each project’s existing SQLite task database read-only for dashboard endpoints, and uses existing command/service logic for safe mutations. The first milestone should be a dependency-light server-rendered/static UI so the feature can ship inside the existing `cli` package without introducing a frontend build pipeline.

## Current Codebase Context

- CLI package: `cli/`
- CLI entrypoint: `cli/src/cli.ts`
- SQLite helpers and schema: `cli/src/db.ts`
- Project metadata helpers: `cli/src/commands/projects.ts`
- Epic commands: `cli/src/commands/epics.ts`
- Task commands: `cli/src/commands/tasks.ts`
- Search command: `cli/src/commands/search.ts`
- Existing package scripts: `cli/package.json`
- Current central-looking registry file in this repo: `projects.json`
- Existing per-project metadata format: `.pi/task-system/projects.json`

## Key Design Decisions

1. **Source of truth stays per project**
   - Do not move task data into a new central database.
   - Each project keeps its own `.pi/tasks.db`.
   - The web app only coordinates reads/writes across those project databases.

2. **Global registry is a pointer list, not duplicated task data**
   - Store only project metadata and database paths centrally.
   - Recommended default: `~/.pi/task-system/projects.json`.
   - Continue supporting per-project `.pi/task-system/projects.json` for compatibility.

3. **Localhost-only by default**
   - Bind to `127.0.0.1`, not `0.0.0.0`.
   - Include optional `--host`, but warn or reject non-local hosts unless `--unsafe-allow-network` is passed.

4. **Use existing command logic for writes**
   - Reads can use dashboard query helpers.
   - Writes should reuse `cmdEpicCreate`, `cmdTaskCreate`, `cmdTaskUpdateStatus`, task item helpers, workflow report helpers, etc.
   - This avoids divergent validation rules.

5. **Ship without a frontend framework first**
   - MVP uses Node HTTP server + static HTML/CSS/JS.
   - Add React/Vite only if UI complexity grows later.

## Milestone 1: Web Server Skeleton

### Task 1: Add web module folder and shared web types

**Files**:
- Create: `cli/src/web/types.ts`

**Steps**:
1. Create `cli/src/web/types.ts`.
2. Add typed request/response DTOs for projects, epics, tasks, task summaries, dashboard metrics, and API errors.
3. Include comments for every exported function/type as required by project conventions.
4. Run:
   ```bash
   cd cli && npm run build
   ```
5. Expected output:
   - `dist/cli.mjs` is rebuilt.
   - No TypeScript/esbuild errors.

**Expected file content outline**:
```ts
export interface WebProjectSummary { ... }
export interface WebTaskSummary { ... }
export interface WebDashboardSummary { ... }
export interface WebApiError { error: string; details?: string }
```

**Commit message**:
```text
feat: add web dashboard dto types
```

---

### Task 2: Add web server configuration parser

**Files**:
- Create: `cli/src/web/config.ts`
- Modify: `cli/src/cli.ts`

**Steps**:
1. Create `cli/src/web/config.ts`.
2. Implement `normalizeWebHost(host: string, unsafeAllowNetwork: boolean)`.
3. Implement `parseWebPort(value: string | undefined)`.
4. Reject invalid ports outside `1..65535`.
5. Default host to `127.0.0.1`.
6. Default port to `4377`.
7. Add a placeholder `pic web` command in `cli/src/cli.ts` that only resolves config and prints JSON for now.
8. Run:
   ```bash
   cd cli && npm run build
   node dist/cli.mjs web --port 4377
   ```
9. Expected output:
   ```json
   {"host":"127.0.0.1","port":4377}
   ```

**Security acceptance criteria**:
- `pic web --host 0.0.0.0` fails unless `--unsafe-allow-network` is passed.
- `pic web --port 99999` fails.

**Commit message**:
```text
feat: add web server config parsing
```

---

### Task 3: Add minimal HTTP server

**Files**:
- Create: `cli/src/web/server.ts`
- Modify: `cli/src/cli.ts`

**Steps**:
1. Create `cli/src/web/server.ts` using Node built-in `http`.
2. Implement `startWebServer(options)`.
3. Add routes:
   - `GET /healthz`
   - `GET /api/version`
   - fallback `404` JSON response
4. Wire `pic web` to call `startWebServer`.
5. Run:
   ```bash
   cd cli && npm run build
   node dist/cli.mjs web --port 4377
   curl -s http://127.0.0.1:4377/healthz
   ```
6. Expected output:
   ```json
   {"ok":true}
   ```

**Implementation notes**:
- Keep `server.ts` focused on transport/routing.
- Do not put SQLite query logic in `server.ts`.

**Commit message**:
```text
feat: add localhost web server
```

---

## Milestone 2: Project Registry and Discovery

### Task 4: Add central registry helpers

**Files**:
- Create: `cli/src/web/project-registry.ts`
- Test: `cli/web-project-registry.test.mjs`
- Modify: `cli/package.json`

**Steps**:
1. Create `cli/src/web/project-registry.ts`.
2. Implement `globalProjectRegistryPath(homeDir = homedir())` returning:
   ```text
   ~/.pi/task-system/projects.json
   ```
3. Implement `readProjectRegistry(path)`.
4. Implement `writeProjectRegistry(path, registry)`.
5. Implement `upsertRegistryProject(registry, project)`.
6. Add Node test file `cli/web-project-registry.test.mjs`.
7. Update `cli/package.json` test script to include the new test.
8. Run:
   ```bash
   cd cli && npm test
   ```
9. Expected output:
   - Existing `repair-phases.test.mjs` passes.
   - New registry tests pass.

**Registry format**:
```json
{
  "projects": [
    {
      "id": "proj-example",
      "name": "example",
      "root_path": "/absolute/project/path",
      "database_path": "/absolute/project/path/.pi/tasks.db",
      "changelog_path": "/absolute/project/path/CHANGELOG.md",
      "created_at": "2026-06-20T00:00:00.000Z",
      "updated_at": "2026-06-20T00:00:00.000Z"
    }
  ],
  "current_project_id": "proj-example"
}
```

**Commit message**:
```text
feat: add global project registry helpers
```

---

### Task 5: Register current project during `pic init`

**Files**:
- Modify: `cli/src/cli.ts`
- Modify: `cli/src/web/project-registry.ts`
- Test: `cli/web-project-registry.test.mjs`

**Steps**:
1. After `pic init` creates per-project metadata with `cmdProjectCreate`, also upsert the project into the global registry.
2. Add helper `registerProjectInGlobalRegistry(project, registryPath?)`.
3. Ensure failures to write the global registry are surfaced in CLI JSON under a non-fatal `registry_warning` field rather than breaking project initialization.
4. Add tests for idempotent upsert.
5. Run:
   ```bash
   cd cli && npm test
   ```
6. Expected output:
   - Tests pass.
   - Re-running registration does not duplicate the project.

**Commit message**:
```text
feat: register initialized projects globally
```

---

### Task 6: Add explicit project registration command

**Files**:
- Modify: `cli/src/cli.ts`
- Modify: `cli/src/commands/projects.ts` or create `cli/src/web/project-registration.ts`
- Test: `cli/web-project-registry.test.mjs`

**Steps**:
1. Add command:
   ```bash
   pic project register --root <path>
   ```
2. Resolve the root path.
3. Verify `<root>/.pi/tasks.db` exists.
4. Read or create `<root>/.pi/task-system/projects.json`.
5. Upsert the project into the global registry.
6. Return JSON with the registered project.
7. Run:
   ```bash
   cd cli && npm run build
   node dist/cli.mjs project register --root /path/to/project
   ```
8. Expected output shape:
   ```json
   {"registered":true,"project":{"id":"proj-...","name":"..."}}
   ```

**Commit message**:
```text
feat: add project register command
```

---

### Task 7: Add optional project scan command

**Files**:
- Create: `cli/src/web/project-discovery.ts`
- Modify: `cli/src/cli.ts`
- Test: `cli/web-project-discovery.test.mjs`
- Modify: `cli/package.json`

**Steps**:
1. Create `cli/src/web/project-discovery.ts`.
2. Implement `findTaskProjects(searchRoot, maxDepth)` that finds directories containing `.pi/tasks.db`.
3. Exclude heavy folders:
   - `node_modules`
   - `.git`
   - `.next`
   - `dist`
   - `build`
   - `.turbo`
4. Add command:
   ```bash
   pic project scan --root <path> --max-depth 4
   ```
5. Return discovered project roots without mutating by default.
6. Add `--register` to upsert discovered projects.
7. Run:
   ```bash
   cd cli && npm test
   ```
8. Expected output:
   - Discovery tests pass with temp directories.

**Commit message**:
```text
feat: add task project discovery
```

---

## Milestone 3: Read-Only Dashboard APIs

### Task 8: Add safe per-project database opener

**Files**:
- Create: `cli/src/web/project-db.ts`
- Test: `cli/web-project-db.test.mjs`
- Modify: `cli/package.json`

**Steps**:
1. Create `cli/src/web/project-db.ts`.
2. Implement `openProjectDb(project)`.
3. Validate that `database_path` exists.
4. Open SQLite with existing `openDb(project.database_path)` so migrations are applied consistently.
5. Implement `closeProjectDb(db)`.
6. Add tests with a temporary initialized database.
7. Run:
   ```bash
   cd cli && npm test
   ```
8. Expected output:
   - Database helper tests pass.

**Commit message**:
```text
feat: add web project database helper
```

---

### Task 9: Add dashboard query service

**Files**:
- Create: `cli/src/web/dashboard-service.ts`
- Test: `cli/web-dashboard-service.test.mjs`
- Modify: `cli/package.json`

**Steps**:
1. Create `cli/src/web/dashboard-service.ts`.
2. Implement `listDashboardProjects(registry)`.
3. Implement `getProjectDashboard(project)` returning:
   - epic counts by status
   - task counts by status
   - task counts by priority
   - review status counts
   - latest verification status counts
   - latest updated/created timestamps when available
4. Implement `listProjectTasks(project, filters)` with filters:
   - `status`
   - `priority`
   - `review_status`
   - `workflow_mode`
   - `epic_id`
   - `q`
5. Keep SQL parameterized.
6. Add tests against temp SQLite fixtures.
7. Run:
   ```bash
   cd cli && npm test
   ```
8. Expected output:
   - Dashboard service tests pass.

**Commit message**:
```text
feat: add dashboard query service
```

---

### Task 10: Add JSON API router

**Files**:
- Create: `cli/src/web/api.ts`
- Modify: `cli/src/web/server.ts`
- Test: `cli/web-api.test.mjs`
- Modify: `cli/package.json`

**Steps**:
1. Create `cli/src/web/api.ts`.
2. Implement route dispatcher for:
   - `GET /api/projects`
   - `GET /api/projects/:project_id/summary`
   - `GET /api/projects/:project_id/epics`
   - `GET /api/projects/:project_id/tasks`
   - `GET /api/projects/:project_id/tasks/:task_id`
   - `GET /api/search?q=...`
3. Add helper for JSON responses.
4. Add helper for API errors with appropriate HTTP status codes.
5. Wire `server.ts` to delegate `/api/*` requests to `api.ts`.
6. Add tests for route matching and error responses.
7. Run:
   ```bash
   cd cli && npm test
   ```
8. Expected output:
   - API tests pass.

**Commit message**:
```text
feat: add dashboard read api
```

---

### Task 11: Add cross-project search endpoint

**Files**:
- Modify: `cli/src/web/dashboard-service.ts`
- Modify: `cli/src/web/api.ts`
- Test: `cli/web-dashboard-service.test.mjs`

**Steps**:
1. Implement `searchAcrossProjects(projects, query)`.
2. Search project names, epics, tasks, and task items.
3. Reuse existing `fuzzySearch` from `cli/src/search.ts` where practical.
4. Include project metadata on each result.
5. Add route:
   ```text
   GET /api/search?q=<query>
   ```
6. Run:
   ```bash
   cd cli && npm test
   ```
7. Expected output:
   - Search returns results grouped by project.
   - Empty query returns a `400` error.

**Commit message**:
```text
feat: add cross project task search
```

---

## Milestone 4: Static Web UI MVP

### Task 12: Add static asset serving

**Files**:
- Create: `cli/src/web/static.ts`
- Create: `cli/src/web/assets/index.html`
- Create: `cli/src/web/assets/app.css`
- Create: `cli/src/web/assets/app.js`
- Modify: `cli/src/web/server.ts`

**Steps**:
1. Add static file serving for `/`, `/app.css`, and `/app.js`.
2. Restrict served files to known assets only; do not expose arbitrary filesystem paths.
3. Return correct content types:
   - `text/html; charset=utf-8`
   - `text/css; charset=utf-8`
   - `application/javascript; charset=utf-8`
4. Run:
   ```bash
   cd cli && npm run build
   node dist/cli.mjs web --port 4377
   curl -s http://127.0.0.1:4377/ | head
   ```
5. Expected output:
   - HTML document starts with `<!doctype html>`.

**Commit message**:
```text
feat: serve dashboard static assets
```

---

### Task 13: Build project overview UI

**Files**:
- Modify: `cli/src/web/assets/index.html`
- Modify: `cli/src/web/assets/app.css`
- Modify: `cli/src/web/assets/app.js`

**Steps**:
1. Add layout:
   - header
   - sidebar project list
   - main dashboard area
   - status cards
2. On load, fetch `/api/projects`.
3. Render project names, root paths, and health indicators.
4. On project click, fetch `/api/projects/:project_id/summary`.
5. Render counts by status and priority.
6. Run manual verification:
   ```bash
   cd cli && npm run build
   node dist/cli.mjs web --port 4377
   open http://127.0.0.1:4377
   ```
7. Expected result:
   - Browser shows all registered projects.
   - Clicking a project updates summary cards.

**Commit message**:
```text
feat: add dashboard project overview ui
```

---

### Task 14: Build task table UI

**Files**:
- Modify: `cli/src/web/assets/index.html`
- Modify: `cli/src/web/assets/app.css`
- Modify: `cli/src/web/assets/app.js`

**Steps**:
1. Add filters:
   - status
   - priority
   - review status
   - text query
2. Fetch `/api/projects/:project_id/tasks` when filters change.
3. Render table columns:
   - title
   - epic
   - status
   - priority
   - workflow mode
   - review status
   - created date
4. Add empty state and loading state.
5. Run manual verification:
   ```bash
   cd cli && npm run build
   node dist/cli.mjs web --port 4377
   open http://127.0.0.1:4377
   ```
6. Expected result:
   - Task table loads and filters without page refresh.

**Commit message**:
```text
feat: add dashboard task table ui
```

---

### Task 15: Build task details UI

**Files**:
- Modify: `cli/src/web/assets/index.html`
- Modify: `cli/src/web/assets/app.css`
- Modify: `cli/src/web/assets/app.js`

**Steps**:
1. Add task details panel or modal.
2. Fetch `/api/projects/:project_id/tasks/:task_id` on row click.
3. Render:
   - task description
   - notes
   - task items
   - dependencies
   - requirements
   - designs
   - TIPs
   - completion reports
   - verification reports
   - events
4. Keep long JSON sections collapsible.
5. Run manual verification.
6. Expected result:
   - Clicking a task displays workflow details without leaving the page.

**Commit message**:
```text
feat: add task details dashboard ui
```

---

### Task 16: Build global search UI

**Files**:
- Modify: `cli/src/web/assets/index.html`
- Modify: `cli/src/web/assets/app.css`
- Modify: `cli/src/web/assets/app.js`

**Steps**:
1. Add global search input in the header.
2. Debounce input by 250ms.
3. Fetch `/api/search?q=...`.
4. Render results grouped by project and type.
5. Clicking a result selects project and opens task/epic detail where possible.
6. Run manual verification.
7. Expected result:
   - Search finds tasks across multiple registered projects.

**Commit message**:
```text
feat: add global dashboard search ui
```

---

## Milestone 5: Safe Write APIs

### Task 17: Add request body parsing and validation helpers

**Files**:
- Create: `cli/src/web/request.ts`
- Test: `cli/web-request.test.mjs`
- Modify: `cli/package.json`

**Steps**:
1. Create `cli/src/web/request.ts`.
2. Implement `readJsonBody(req, maxBytes)`.
3. Reject bodies larger than 64KB for MVP write routes.
4. Return `400` for invalid JSON.
5. Implement simple validation helpers for strings and enums.
6. Add tests for valid JSON, invalid JSON, and body limit.
7. Run:
   ```bash
   cd cli && npm test
   ```
8. Expected output:
   - Request helper tests pass.

**Security acceptance criteria**:
- No `eval()`.
- No arbitrary shell execution.
- All SQL remains parameterized.

**Commit message**:
```text
feat: add web request validation helpers
```

---

### Task 18: Add epic write endpoints

**Files**:
- Modify: `cli/src/web/api.ts`
- Test: `cli/web-api.test.mjs`

**Steps**:
1. Add endpoints:
   - `POST /api/projects/:project_id/epics`
   - `PATCH /api/projects/:project_id/epics/:epic_id/status`
2. Reuse `cmdEpicCreate` and `cmdEpicUpdateStatus`.
3. Validate `title` is non-empty and reasonably bounded, e.g. max 300 chars.
4. Validate status enum.
5. Add API tests.
6. Run:
   ```bash
   cd cli && npm test
   ```
7. Expected output:
   - Epic write endpoint tests pass.

**Commit message**:
```text
feat: add dashboard epic write api
```

---

### Task 19: Add task write endpoints

**Files**:
- Modify: `cli/src/web/api.ts`
- Test: `cli/web-api.test.mjs`

**Steps**:
1. Add endpoints:
   - `POST /api/projects/:project_id/tasks`
   - `PATCH /api/projects/:project_id/tasks/:task_id/status`
   - `PATCH /api/projects/:project_id/tasks/:task_id`
2. Reuse:
   - `cmdTaskCreate`
   - `cmdTaskUpdateStatus`
   - existing update logic from `cli/src/cli.ts`, extracted if needed
3. Validate priority/status/workflow enums.
4. Preserve verification gate for marking tasks `done`.
5. Add API tests for:
   - successful create
   - invalid epic
   - invalid priority fallback or rejection, depending on chosen behavior
   - cannot mark done without passed verification
6. Run:
   ```bash
   cd cli && npm test
   ```
7. Expected output:
   - Task write endpoint tests pass.

**Commit message**:
```text
feat: add dashboard task write api
```

---

### Task 20: Add task item write endpoints

**Files**:
- Modify: `cli/src/web/api.ts`
- Test: `cli/web-api.test.mjs`

**Steps**:
1. Add endpoints:
   - `POST /api/projects/:project_id/tasks/:task_id/items`
   - `PATCH /api/projects/:project_id/task-items/:item_id/toggle`
   - `DELETE /api/projects/:project_id/task-items/:item_id`
2. Reuse:
   - `cmdTaskItemAdd`
   - `cmdTaskItemToggle`
   - `cmdTaskItemDelete`
3. Add tests.
4. Run:
   ```bash
   cd cli && npm test
   ```
5. Expected output:
   - Task item endpoint tests pass.

**Commit message**:
```text
feat: add dashboard task item write api
```

---

### Task 21: Add write UI controls

**Files**:
- Modify: `cli/src/web/assets/index.html`
- Modify: `cli/src/web/assets/app.css`
- Modify: `cli/src/web/assets/app.js`

**Steps**:
1. Add create epic form.
2. Add create task form.
3. Add status dropdown for tasks.
4. Add task item add/toggle controls in task details.
5. Show server validation errors inline.
6. Refresh relevant dashboard data after successful writes.
7. Run manual verification.
8. Expected result:
   - User can create epics/tasks and update task status from the browser.

**Commit message**:
```text
feat: add dashboard write controls
```

---

## Milestone 6: Workflow Artifact Views

### Task 22: Add workflow summary endpoints

**Files**:
- Modify: `cli/src/web/dashboard-service.ts`
- Modify: `cli/src/web/api.ts`
- Test: `cli/web-dashboard-service.test.mjs`

**Steps**:
1. Add query helpers for:
   - review queue
   - verification queue
   - open escalations
   - blocked TIPs
   - tasks missing completion evidence
2. Add routes:
   - `GET /api/workflow/review-queue`
   - `GET /api/workflow/verification-queue`
   - `GET /api/workflow/escalations`
   - `GET /api/workflow/blocked`
3. Include project metadata for each returned item.
4. Add tests.
5. Run:
   ```bash
   cd cli && npm test
   ```
6. Expected output:
   - Workflow endpoint tests pass.

**Commit message**:
```text
feat: add workflow dashboard endpoints
```

---

### Task 23: Add workflow dashboard UI

**Files**:
- Modify: `cli/src/web/assets/index.html`
- Modify: `cli/src/web/assets/app.css`
- Modify: `cli/src/web/assets/app.js`

**Steps**:
1. Add top-level tabs:
   - Projects
   - Tasks
   - Review Queue
   - Verification Queue
   - Escalations
2. Render workflow queue data from the new endpoints.
3. Add links from queue rows to task details.
4. Run manual verification.
5. Expected result:
   - User can see cross-project review/verification/escalation queues.

**Commit message**:
```text
feat: add workflow dashboard ui
```

---

## Milestone 7: Live Refresh and UX Polish

### Task 24: Add polling-based refresh

**Files**:
- Modify: `cli/src/web/assets/app.js`
- Modify: `cli/src/web/assets/index.html`

**Steps**:
1. Add refresh interval selector:
   - off
   - 5s
   - 15s
   - 60s
2. Poll only visible data.
3. Preserve selected project and filters during refresh.
4. Run manual verification.
5. Expected result:
   - Dashboard updates without full page reload.

**Commit message**:
```text
feat: add dashboard auto refresh
```

---

### Task 25: Add accessibility and keyboard polish

**Files**:
- Modify: `cli/src/web/assets/index.html`
- Modify: `cli/src/web/assets/app.css`
- Modify: `cli/src/web/assets/app.js`

**Steps**:
1. Add semantic landmarks.
2. Ensure controls have labels.
3. Add visible focus states.
4. Add keyboard shortcuts:
   - `/` focus search
   - `r` refresh current view
   - `Escape` close details modal
5. Run manual browser verification.
6. Expected result:
   - Dashboard is usable by keyboard.

**Commit message**:
```text
feat: improve dashboard accessibility
```

---

## Milestone 8: Documentation and Packaging

### Task 26: Document web dashboard usage

**Files**:
- Modify: `docs/V1.0.0.md` or create `docs/web-dashboard.md`
- Modify: `cli/package.json` if package metadata should mention `pic web`

**Steps**:
1. Document how to start the dashboard:
   ```bash
   pic web
   ```
2. Document host/port options:
   ```bash
   pic web --port 4377
   pic web --host 127.0.0.1 --port 4377
   ```
3. Document project registration:
   ```bash
   pic project register --root /path/to/project
   pic project scan --root ~/src --register
   ```
4. Document security behavior.
5. Document troubleshooting:
   - missing project
   - missing database
   - port already in use
   - cannot mark task done without verification
6. Run:
   ```bash
   cd cli && npm test && npm run build
   ```
7. Expected output:
   - Tests pass.
   - CLI builds.

**Commit message**:
```text
docs: add web dashboard documentation
```

---

### Task 27: Add final regression checks

**Files**:
- No required source files unless failures are found.

**Steps**:
1. Run full CLI test/build:
   ```bash
   cd cli && npm test && npm run build
   ```
2. Run extension checks to ensure no task-system integration broke:
   ```bash
   cd pi-ext && npm test && npm run check
   ```
3. Start the web server:
   ```bash
   cd cli && node dist/cli.mjs web --port 4377
   ```
4. Smoke test endpoints:
   ```bash
   curl -s http://127.0.0.1:4377/healthz
   curl -s http://127.0.0.1:4377/api/projects
   ```
5. Open browser:
   ```bash
   open http://127.0.0.1:4377
   ```
6. Verify:
   - project list appears
   - task list appears
   - task detail opens
   - search works
   - create task works
   - status update respects verification gate

**Expected output**:
- CLI tests pass.
- Extension tests pass.
- TypeScript check passes.
- Web server starts on localhost.
- Smoke endpoints return JSON.

**Commit message**:
```text
test: add final dashboard regression coverage
```

---

## Suggested API Contract

### `GET /api/projects`

Response:
```json
{
  "projects": [
    {
      "id": "proj-example",
      "name": "example",
      "root_path": "/path/to/example",
      "database_path": "/path/to/example/.pi/tasks.db",
      "health": "ok"
    }
  ]
}
```

### `GET /api/projects/:project_id/summary`

Response:
```json
{
  "project_id": "proj-example",
  "epics": { "open": 1, "in_progress": 0, "done": 2, "cancelled": 0 },
  "tasks": { "open": 4, "in_progress": 1, "done": 8, "cancelled": 0 },
  "priorities": { "high": 1, "medium": 7, "low": 5 },
  "reviews": { "pending": 2, "passed": 4, "failed": 1, "none": 6 },
  "verification": { "passed": 3, "failed": 1, "partial": 0, "blocked": 1, "missing": 8 }
}
```

### `GET /api/projects/:project_id/tasks?status=open&q=dashboard`

Response:
```json
{
  "tasks": [
    {
      "id": "t-example",
      "title": "Build dashboard",
      "epic_id": "epic-example",
      "epic_title": "Web UI",
      "status": "open",
      "priority": "medium",
      "workflow_mode": "standard",
      "review_status": "",
      "created_at": "2026-06-20 12:00:00"
    }
  ]
}
```

### `POST /api/projects/:project_id/tasks`

Request:
```json
{
  "epic_id": "epic-example",
  "title": "Add task table",
  "description": "Render tasks for selected project",
  "priority": "medium",
  "workflow_mode": "standard"
}
```

Response:
```json
{
  "id": "t-example",
  "epic_id": "epic-example",
  "title": "Add task table",
  "status": "open",
  "priority": "medium"
}
```

## Security Checklist

- Bind to `127.0.0.1` by default.
- Reject unsafe network bind unless explicitly allowed.
- Do not expose arbitrary filesystem reads through static file serving.
- Do not execute shell commands from API requests.
- Parameterize all SQL queries.
- Validate all JSON request bodies.
- Add body size limits.
- Preserve existing verification gate before task completion.
- Treat global registry writes as metadata updates only.

## Testing Strategy

### Unit Tests
- Registry path/read/write/upsert.
- Project discovery with temp directories.
- Request validation and JSON body parsing.
- Dashboard query service with temp SQLite databases.
- API routing and error responses.

### Integration Tests
- Initialize temp projects with `initDb`.
- Register multiple projects.
- Start web API handlers in-process or call handler functions directly.
- Verify cross-project listing/search.

### Manual Smoke Tests
- `pic web` starts server.
- Browser loads dashboard.
- Project list renders.
- Task filters work.
- Task detail opens.
- Create/update flows work.
- Verification gate blocks invalid `done` transition.

## Risks and Mitigations

1. **Multiple processes writing the same SQLite database**
   - Mitigation: keep writes short, use existing SQLite behavior, avoid long transactions.

2. **Registry becomes stale**
   - Mitigation: API returns per-project health, project scan can refresh, missing databases show clear warnings.

3. **Frontend grows too complex for plain JS**
   - Mitigation: start simple; migrate assets to React/Vite only after MVP proves need.

4. **Security exposure from local server**
   - Mitigation: localhost-only default, unsafe network bind requires explicit flag, no shell execution.

5. **Validation drift between CLI and web writes**
   - Mitigation: reuse command functions and extract shared update helpers when needed.

## Recommended First Implementation Slice

If we want the smallest useful version, implement only these tasks first:

1. Task 2: web config parser
2. Task 3: minimal HTTP server
3. Task 4: global registry helpers
4. Task 8: project database opener
5. Task 9: dashboard query service
6. Task 10: read-only API router
7. Task 12: static asset serving
8. Task 13: project overview UI
9. Task 14: task table UI

This produces a read-only localhost dashboard before any browser writes are introduced.
