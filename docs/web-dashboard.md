# Web Dashboard (`pic web`)

The pic task-system includes a localhost web dashboard for centralized project management.

## Quick Start

```bash
pic web
```

Open http://127.0.0.1:4377 in your browser.

## Options

| Option | Default | Description |
|--------|---------|-------------|
| `--port <number>` | `4377` | HTTP port to listen on |
| `--host <host>` | `127.0.0.1` | Host to bind to |
| `--unsafe-allow-network` | — | Allow binding to non-loopback addresses |

## Security

- Binds to `127.0.0.1` (localhost) by default
- Non-local bindings require `--unsafe-allow-network`
- No authentication (local-only access)
- All SQL queries are parameterized
- No arbitrary filesystem access beyond configured project databases
- No shell execution from API requests

## Project Registration

Projects need to be registered in the global registry before they appear in the dashboard.

### Automatic Registration

Run `pic init` inside any project directory. This creates the task database,
project metadata, and automatically registers the project in the global
registry at `~/.pi/task-system/projects.json`.

### Manual Registration

```bash
pic project register --root /path/to/project
```

### Scan for Projects

Scan a directory tree for projects that already have a task database:

```bash
pic project scan --root ~/src --register
```

The `--register` flag automatically registers discovered projects.

## API Endpoints

### Read Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects` | List all registered projects |
| GET | `/api/projects/:id/summary` | Dashboard summary for a project |
| GET | `/api/projects/:id/epics` | List epics with task counts |
| GET | `/api/projects/:id/tasks` | List tasks (with filters) |
| GET | `/api/projects/:id/tasks/:task_id` | Full task detail |
| GET | `/api/search?q=...` | Cross-project search |
| GET | `/api/workflow/review-queue` | Tasks pending review |
| GET | `/api/workflow/verification-queue` | Done tasks missing verification |
| GET | `/api/workflow/escalations` | Open escalations |
| GET | `/api/workflow/blocked` | Blocked tasks |

### Task List Filters

- `?status=open|in_progress|done|cancelled`
- `?priority=high|medium|low`
- `?review_status=pending|passed|failed|none`
- `?workflow_mode=quick|standard|designed|full`
- `?epic_id=<epic-id>`
- `?q=<text>`

### Write Endpoints

| Method | Path | Body |
|--------|------|------|
| POST | `/api/projects/:id/epics` | `{ "title": "...", "description": "..." }` |
| PATCH | `/api/projects/:id/epics/:eid/status` | `{ "status": "..." }` |
| POST | `/api/projects/:id/tasks` | `{ "title": "...", "epic_id": "...", "priority": "..." }` |
| PATCH | `/api/projects/:id/tasks/:tid/status` | `{ "status": "..." }` |
| PATCH | `/api/projects/:id/tasks/:tid` | `{ "title": "...", "description": "...", "priority": "...", "notes": "..." }` |
| POST | `/api/projects/:id/tasks/:tid/items` | `{ "content": "..." }` |
| PATCH | `/api/projects/:id/task-items/:iid/toggle` | `{}` |
| DELETE | `/api/projects/:id/task-items/:iid` | — |

## Troubleshooting

### Missing Projects

If your project doesn't appear in the dashboard:

1. Make sure `pic init` was run:
   ```bash
   cd /path/to/project
   pic init
   ```

2. Verify the global registry:
   ```bash
   cat ~/.pi/task-system/projects.json
   ```

3. Register manually:
   ```bash
   pic project register --root /path/to/project
   ```

### "Database not found" Errors

The dashboard shows `missing_db` for projects whose database files have been
deleted or moved. If the database exists but still shows as missing, check
the project path in the registry:

```bash
cat ~/.pi/task-system/projects.json
```

### Port Already in Use

```bash
pic web --port 4378
```

## Architecture

```
User browser → localhost:4377 → Node HTTP server
                                    ├── GET /healthz, /api/version
                                    ├── Static assets (/, /app.css, /app.js)
                                    └── /api/* → dispatchApiRequest()
                                                    ├── readProjectRegistry()
                                                    ├── openProjectDb(path)
                                                    ├── dashboard queries
                                                    ├── write commands
                                                    └── closeProjectDb()
```

### Data Flow

1. Global registry at `~/.pi/task-system/projects.json` lists all known projects
2. Each project entry points to its per-project `.pi/tasks.db`
3. Read endpoints open the database, run queries, close it
4. Write endpoints open the database, validate the request, perform the SQL
   operation, close it
5. Per-project databases remain the authoritative source of truth
