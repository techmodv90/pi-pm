package dashboard

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/earendil-works/task-system/go-pic/internal/project"
	"github.com/earendil-works/task-system/go-pic/internal/store"
	"github.com/earendil-works/task-system/go-pic/internal/work-item"
)

// Version is injected by the pic CLI entrypoint so the dashboard health
// payload reports the binary version.
var Version string

func Web(args []string) error {
	port, host := "4377", "127.0.0.1"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			i++
			if i >= len(args) {
				return errors.New("--port requires a value")
			}
			port = args[i]
		case "--host":
			i++
			if i >= len(args) {
				return errors.New("--host requires a value")
			}
			host = args[i]
		case "--unsafe-allow-network":
		default:
			return fmt.Errorf("unknown web option: %s", args[i])
		}
	}
	mux := http.NewServeMux()
	health := func(w http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(w, healthData())
	}
	mux.HandleFunc("/health", health)
	mux.HandleFunc("/healthz", health)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { HandleAPI(w, r) })
	mux.HandleFunc("/", serveDashboard)
	fmt.Fprintf(os.Stderr, "pic web listening on http://%s:%s\n", host, port)
	return http.ListenAndServe(host+":"+port, mux)
}

func healthData() map[string]any {
	return map[string]any{"ok": true, "implementation": "go", "version": Version, "dashboard_assets": dashboardBuildDir() != ""}
}

func serveDashboard(w http.ResponseWriter, r *http.Request) {
	buildDir := dashboardBuildDir()
	if buildDir == "" {
		http.Error(w, "dashboard assets not found", http.StatusNotFound)
		return
	}
	path := filepath.Join(buildDir, strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), string(os.PathSeparator)))
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	http.ServeFile(w, r, filepath.Join(buildDir, "index.html"))
}

func dashboardBuildDir() string {
	exe, _ := os.Executable()
	candidates := []string{
		os.Getenv("PIC_DASHBOARD_DIR"),
		filepath.Join(filepath.Dir(exe), "..", "web", "build"),
		filepath.Join("go-pic", "web", "build"),
		filepath.Join("web", "build"),
	}
	for _, dir := range candidates {
		if dir != "" {
			if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
				abs, _ := filepath.Abs(dir)
				return abs
			}
		}
	}
	return ""
}

func writeJSONResponse(w http.ResponseWriter, value any) { writeJSONStatus(w, http.StatusOK, value) }

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	data, _ := jsonMarshal(value)
	_, _ = w.Write(data)
}

func DecodeJSONBody(r *http.Request) (map[string]any, error) {
	defer r.Body.Close()
	const maxBytes = 65536
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return nil, errors.New("failed to read request body")
	}
	if len(data) == 0 {
		return nil, errors.New("request body is required")
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("request body exceeds %d byte limit", maxBytes)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, errors.New("invalid JSON in request body")
	}
	return body, nil
}

func ValidateString(value any, field string, minLength, maxLength int) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", field)
	}
	text = strings.TrimSpace(text)
	if len(text) < minLength {
		return "", fmt.Errorf("%s must be at least %d character(s)", field, minLength)
	}
	if len(text) > maxLength {
		return "", fmt.Errorf("%s must be at most %d characters", field, maxLength)
	}
	return text, nil
}

func ValidateEnum(value any, field string, allowed []string) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", field)
	}
	if !store.Contains(allowed, text) {
		return "", fmt.Errorf("%s must be one of: %s", field, strings.Join(allowed, ", "))
	}
	return text, nil
}

func jsonMarshal(value any) ([]byte, error) { return json.Marshal(value) }

var (
	projectSchemaMu          sync.Mutex
	initializedProjectSchema = map[string]bool{}
)

func openProjectDB(path string) (*sql.DB, error) {
	if path == "" {
		return nil, errors.New("No database path provided")
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("Database not found: %s", path)
		}
		return nil, err
	}
	path, _ = filepath.Abs(path)
	projectSchemaMu.Lock()
	if !initializedProjectSchema[path] {
		db, err := project.OpenSQLite(path)
		if err != nil {
			projectSchemaMu.Unlock()
			return nil, fmt.Errorf("Failed to open database %s: %w", path, err)
		}
		_ = db.Close()
		if err := project.InitDB(path); err != nil {
			projectSchemaMu.Unlock()
			return nil, fmt.Errorf("Failed to update database schema %s: %w", path, err)
		}
		initializedProjectSchema[path] = true
	}
	projectSchemaMu.Unlock()
	db, err := project.OpenSQLite(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to open database %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func closeProjectDB(db *sql.DB) {
	if db != nil {
		_ = db.Close()
	}
}

func workItemDetailForWeb(db *sql.DB, id string) (map[string]any, bool) {
	item, err := workitem.ByID(db, id)
	if err != nil {
		return nil, false
	}
	children, _ := store.QueryMaps(db, `SELECT `+workitem.Columns+` FROM work_items WHERE parent_id=? ORDER BY created_at,id`, id)
	descendants, _ := store.QueryMaps(db, `WITH RECURSIVE tree(id,depth) AS (
		SELECT id,1 FROM work_items WHERE parent_id=?
		UNION ALL SELECT wi.id,tree.depth+1 FROM work_items wi JOIN tree ON wi.parent_id=tree.id
	) SELECT `+workitem.Columns+`,tree.depth FROM work_items JOIN tree USING(id) ORDER BY tree.depth,created_at,id`, id)
	_ = workitem.AttachLabels(db, children)
	_ = workitem.AttachLabels(db, descendants)
	dependencies, _ := store.QueryMaps(db, `SELECT r.id,r.work_item_id,r.related_work_item_id AS depends_on_work_item_id,r.rationale,blocker.title,blocker.type,blocker.status FROM work_item_relations r JOIN work_items blocker ON blocker.id=r.related_work_item_id WHERE r.work_item_id=? AND r.relation_type='blocks'`, id)
	gates, _ := store.QueryMaps(db, `SELECT r.id,r.work_item_id,r.related_work_item_id AS gate_work_item_id,gate_item.title,gate_item.status FROM work_item_relations r JOIN work_items gate_item ON gate_item.id=r.related_work_item_id WHERE r.work_item_id=? AND r.relation_type='gates'`, id)
	artifacts, _ := store.QueryMaps(db, `SELECT * FROM work_item_artifacts WHERE work_item_id=? ORDER BY stage,revision DESC`, id)
	checkpoints, _ := store.QueryMaps(db, `SELECT * FROM workflow_checkpoints WHERE work_item_id=? ORDER BY created_at`, id)
	packs, _ := store.QueryMaps(db, `SELECT * FROM work_item_instruction_packs WHERE work_item_id=? ORDER BY version DESC`, id)
	completions, _ := store.QueryMaps(db, `SELECT * FROM work_item_completion_reports WHERE work_item_id=? ORDER BY datetime(created_at) DESC,rowid DESC`, id)
	verifications, _ := store.QueryMaps(db, `SELECT * FROM work_item_verification_reports WHERE work_item_id=? ORDER BY datetime(created_at) DESC,rowid DESC`, id)
	authorizations, _ := store.QueryMaps(db, `SELECT * FROM implementation_authorizations WHERE work_item_id=? ORDER BY created_at DESC,id DESC`, id)
	ready, _ := store.RowExists(db, `SELECT 1 FROM work_items wi WHERE wi.id=? AND `+workitem.ReadySQL, id)
	routingEvents, _ := store.QueryMaps(db, `SELECT event_type, created_at AS createdAt, summary, payload_json AS payloadJson FROM work_item_events WHERE work_item_id=? AND event_type='skill_family_routing' AND json_valid(payload_json) ORDER BY datetime(created_at) DESC, rowid DESC LIMIT 10`, id)
	return map[string]any{"workItem": item, "ready": ready, "children": children, "descendants": descendants, "dependencies": dependencies, "gates": gates, "artifacts": artifacts, "checkpoints": checkpoints, "instructionPacks": packs, "authorizations": authorizations, "completionReports": completions, "verificationReports": verifications, "routingEvents": routingEvents}, true
}

func HandleAPI(w http.ResponseWriter, r *http.Request) {
	registry := project.ReadRegistry()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 2 && parts[0] == "api" && parts[1] == "projects" && r.Method == http.MethodGet {
		projects := []map[string]any{}
		for _, project := range registry.Projects {
			projects = append(projects, webProject(project))
		}
		writeJSONResponse(w, map[string]any{"projects": projects})
		return
	}
	if len(parts) == 2 && parts[0] == "api" && parts[1] == "search" && r.Method == http.MethodGet {
		q := r.URL.Query().Get("q")
		if strings.TrimSpace(q) == "" {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": `Query parameter "q" is required`})
			return
		}
		writeJSONResponse(w, webSearch(registry, q))
		return
	}
	if len(parts) == 3 && parts[0] == "api" && parts[1] == "workflow" && parts[2] == "review-queue" && r.Method == http.MethodGet {
		webReviewQueue(w, registry)
		return
	}
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "projects" {
		http.NotFound(w, r)
		return
	}
	project, ok := registryProjectByID(registry, parts[2])
	if !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "Project not found"})
		return
	}
	db, err := openProjectDB(project.DBPath())
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer closeProjectDB(db)
	switch {
	case len(parts) == 5 && parts[3] == "work-items" && parts[4] == "labels" && r.Method == http.MethodGet:
		labels, err := store.QueryMaps(db, `SELECT label,COUNT(*) AS count FROM work_item_labels GROUP BY label ORDER BY label`)
		if err != nil {
			writeJSONStatus(w, 500, map[string]any{"error": err.Error()})
			return
		}
		writeJSONResponse(w, map[string]any{"labels": labels})
	case len(parts) == 5 && parts[3] == "work-items" && parts[4] == "ready" && r.Method == http.MethodGet:
		items, err := store.QueryMaps(db, `SELECT `+workitem.Columns+` FROM work_items wi WHERE `+workitem.ReadySQL+` ORDER BY created_at,id`)
		if err == nil {
			err = workitem.AttachLabels(db, items)
		}
		if err != nil {
			writeJSONStatus(w, 500, map[string]any{"error": err.Error()})
			return
		}
		writeJSONResponse(w, map[string]any{"workItems": items})
	case len(parts) == 4 && parts[3] == "work-items" && r.Method == http.MethodGet:
		filterArgs := []string{}
		for _, key := range []string{"label", "label-any"} {
			if value := r.URL.Query().Get(key); value != "" {
				filterArgs = append(filterArgs, "--"+key, value)
			}
		}
		items, err := workitem.List(db, filterArgs)
		if err != nil {
			writeJSONStatus(w, 400, map[string]any{"error": err.Error()})
			return
		}
		writeJSONResponse(w, map[string]any{"workItems": items})
	case len(parts) == 4 && parts[3] == "work-items" && r.Method == http.MethodPost:
		body, err := DecodeJSONBody(r)
		if err != nil {
			writeJSONStatus(w, 400, map[string]any{"error": err.Error()})
			return
		}
		itemType, title := store.PersistedText(body["type"]), strings.TrimSpace(store.PersistedText(body["title"]))
		if !store.Contains([]string{"epic", "feature", "task", "bug", "chore", "gate"}, itemType) || title == "" {
			writeJSONStatus(w, 400, map[string]any{"error": "type and title are required"})
			return
		}
		args := []string{itemType, title}
		for key, option := range map[string]string{"parent_id": "--parent", "description": "--description", "priority": "--priority"} {
			if value := store.PersistedText(body[key]); value != "" {
				args = append(args, option, value)
			}
		}
		if labels, ok := body["labels"].([]any); ok && len(labels) > 0 {
			values := make([]string, len(labels))
			for i := range labels {
				values[i] = store.PersistedText(labels[i])
			}
			args = append(args, "--labels", strings.Join(values, ","))
		}
		if err := workitem.Create(db, args); err != nil {
			writeJSONStatus(w, 400, map[string]any{"error": err.Error()})
			return
		}
		item, _ := store.QueryOne(db, `SELECT `+workitem.Columns+` FROM work_items ORDER BY rowid DESC LIMIT 1`)
		if item != nil {
			_ = workitem.AttachLabels(db, []map[string]any{item})
		}
		writeJSONResponse(w, map[string]any{"workItem": item})
	case len(parts) == 6 && parts[3] == "work-items" && parts[5] == "labels" && (r.Method == http.MethodPost || r.Method == http.MethodDelete):
		body, err := DecodeJSONBody(r)
		if err != nil {
			writeJSONStatus(w, 400, map[string]any{"error": err.Error()})
			return
		}
		labels, ok := body["labels"].([]any)
		if !ok || len(labels) == 0 {
			writeJSONStatus(w, 400, map[string]any{"error": "labels are required"})
			return
		}
		values := make([]string, len(labels))
		for i := range labels {
			values[i] = store.PersistedText(labels[i])
		}
		action := "add"
		if r.Method == http.MethodDelete {
			action = "remove"
		}
		if err := workitem.Label(db, []string{action, parts[4], strings.Join(values, ",")}); err != nil {
			writeJSONStatus(w, 400, map[string]any{"error": err.Error()})
			return
		}
		item, _ := workitem.ByID(db, parts[4])
		writeJSONResponse(w, map[string]any{"workItem": item})
	case len(parts) == 5 && parts[3] == "work-items" && r.Method == http.MethodGet:
		if detail, ok := workItemDetailForWeb(db, parts[4]); ok {
			writeJSONResponse(w, detail)
		} else {
			writeJSONStatus(w, 404, map[string]any{"error": "Work Item not found"})
		}
	case len(parts) == 6 && parts[3] == "work-items" && parts[5] == "status" && r.Method == http.MethodPatch:
		body, err := DecodeJSONBody(r)
		status := store.PersistedText(body["status"])
		if err != nil || !store.Contains([]string{"open", "in_progress", "done", "cancelled"}, status) {
			writeJSONStatus(w, 400, map[string]any{"error": "valid status is required"})
			return
		}
		item, err := workitem.SetStatus(db, parts[4], status)
		if err != nil {
			writeJSONStatus(w, 404, map[string]any{"error": err.Error()})
			return
		}
		writeJSONResponse(w, map[string]any{"workItem": item})
	case len(parts) == 4 && parts[3] == "summary" && r.Method == http.MethodGet:
		writeJSONResponse(w, projectSummary(db, project))
	case len(parts) == 4 && parts[3] == "skill-routing" && r.Method == http.MethodGet:
		writeJSONResponse(w, skillRoutingForWeb(db, project))
	case len(parts) == 4 && parts[3] == "activity" && r.Method == http.MethodGet:
		rows, _ := store.QueryMaps(db, `SELECT sa.session_id, sa.task_id AS work_item_id, COALESCE(wi.title, '') AS work_item_title, sa.status, sa.last_skill, sa.updated_at FROM session_activity sa LEFT JOIN work_items wi ON wi.id=sa.task_id WHERE sa.status='active' AND sa.task_id!='' AND datetime(sa.updated_at)>datetime('now','-30 seconds') ORDER BY datetime(sa.updated_at) DESC`)
		if rows == nil {
			rows = []map[string]any{}
		}
		writeJSONResponse(w, map[string]any{"activity": rows})
	default:
		http.NotFound(w, r)
	}
}

func webReviewQueue(w http.ResponseWriter, registry project.Registry) {
	items := []map[string]any{}
	for _, project := range registry.Projects {
		db, err := openProjectDB(project.DBPath())
		if err != nil {
			continue
		}
		rows, _ := store.QueryMaps(db, `SELECT id AS workItemId,title AS workItemTitle,type,review_status AS status,created_at AS createdAt FROM work_items WHERE review_status='pending' ORDER BY datetime(created_at) DESC`)
		_ = db.Close()
		for _, row := range rows {
			row["projectId"] = project.ID
			row["projectName"] = project.Name
			items = append(items, row)
		}
	}
	writeJSONResponse(w, map[string]any{"items": items})
}

func registryProjectByID(registry project.Registry, id string) (project.RegistryProject, bool) {
	return project.FindRegistryProject(registry, id)
}

func webProject(project project.RegistryProject) map[string]any {
	health := "ok"
	if _, err := os.Stat(project.DBPath()); err != nil {
		health = "missing_db"
	}
	return map[string]any{"id": project.ID, "name": project.Name, "rootPath": project.RootDir(), "databasePath": project.DBPath(), "changelogPath": project.ChangelogFile(), "health": health, "createdAt": project.Created(), "updatedAt": project.Updated()}
}

func projectSummary(db *sql.DB, project project.RegistryProject) map[string]any {
	statuses, _ := queryKeyCounts(db, `SELECT COALESCE(status,'open') as key,COUNT(*) as count FROM work_items GROUP BY status`)
	types, _ := queryKeyCounts(db, `SELECT type as key,COUNT(*) as count FROM work_items GROUP BY type`)
	priorities, _ := queryKeyCounts(db, `SELECT COALESCE(priority,'medium') as key,COUNT(*) as count FROM work_items GROUP BY priority`)
	reviews, _ := queryKeyCounts(db, `SELECT CASE WHEN review_status IS NULL OR review_status='' THEN 'none' ELSE review_status END as key,COUNT(*) as count FROM work_items GROUP BY key`)
	ready, _ := queryKeyCounts(db, `SELECT CASE WHEN `+workitem.ReadySQL+` THEN 'ready' ELSE 'blocked' END as key,COUNT(*) as count FROM work_items wi GROUP BY key`)
	var latest string
	_ = db.QueryRow(`SELECT COALESCE(MAX(created_at),'') FROM work_items`).Scan(&latest)
	return map[string]any{"projectId": project.ID, "projectName": project.Name, "rootPath": project.RootDir(), "health": "ok", "statusCounts": statuses, "typeCounts": types, "priorityCounts": priorities, "reviewCounts": reviews, "readinessCounts": ready, "latestActivity": latest}
}

func queryKeyCounts(db *sql.DB, query string) (map[string]int, error) {
	rows, err := store.QueryMaps(db, query)
	if err != nil {
		return map[string]int{}, err
	}
	counts := map[string]int{}
	for _, row := range rows {
		key, _ := row["key"].(string)
		counts[key] = store.ToInt(row["count"])
	}
	return counts, nil
}

// skillRoutingForWeb aggregates the scheduler's observe-mode skill_family_routing
// telemetry for the dashboard Routing tab. Every json_extract is guarded with
// json_valid via CASE (circuit-breaker precedent) so a malformed payload from
// any writer can never abort the whole query; json_each over a NULL argument
// simply yields no rows.
func skillRoutingForWeb(db *sql.DB, project project.RegistryProject) map[string]any {
	var total int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_events WHERE event_type='skill_family_routing'`).Scan(&total)
	matched, _ := store.QueryMaps(db, `SELECT je.value->>'$.id' AS family, je.value->'$.matched_by' AS matchedBy, COUNT(*) AS count
		FROM work_item_events, json_each(CASE WHEN json_valid(payload_json) THEN json_extract(payload_json,'$.matched_families') END) je
		WHERE event_type='skill_family_routing' GROUP BY 1,2 ORDER BY COUNT(*) DESC, family`)
	missing, _ := store.QueryMaps(db, `SELECT je.value AS missing, COUNT(*) AS count
		FROM work_item_events, json_each(CASE WHEN json_valid(payload_json) THEN json_extract(payload_json,'$.missing_families') END) je
		WHERE event_type='skill_family_routing' GROUP BY 1 ORDER BY COUNT(*) DESC, missing`)
	recent, _ := store.QueryMaps(db, `SELECT work_item_id AS workItemId, created_at AS createdAt,
			json_extract(CASE WHEN json_valid(payload_json) THEN payload_json END,'$.stage') AS stage,
			json_extract(CASE WHEN json_valid(payload_json) THEN payload_json END,'$.pack_id') AS packId,
			json_extract(CASE WHEN json_valid(payload_json) THEN payload_json END,'$.selected_families') AS selectedFamilies,
			json_extract(CASE WHEN json_valid(payload_json) THEN payload_json END,'$.matched_families') AS matchedFamilies,
			json_extract(CASE WHEN json_valid(payload_json) THEN payload_json END,'$.missing_families') AS missingFamilies,
			json_extract(CASE WHEN json_valid(payload_json) THEN payload_json END,'$.evidence_sources') AS evidenceSources
		FROM work_item_events WHERE event_type='skill_family_routing' AND json_valid(payload_json)
		ORDER BY datetime(created_at) DESC, rowid DESC LIMIT 50`)
	if matched == nil {
		matched = []map[string]any{}
	}
	if missing == nil {
		missing = []map[string]any{}
	}
	if recent == nil {
		recent = []map[string]any{}
	}
	return map[string]any{"projectId": project.ID, "projectName": project.Name, "totalEvents": total, "familyCounts": matched, "missingCounts": missing, "recentEvents": recent}
}

func webSearch(registry project.Registry, query string) map[string]any {
	results := []map[string]any{}
	if strings.TrimSpace(query) == "" {
		return map[string]any{"query": query, "results": results, "totalCount": 0}
	}
	like := "%" + query + "%"
	for _, project := range registry.Projects {
		db, err := openProjectDB(project.DBPath())
		if err != nil {
			continue
		}
		items, _ := store.QueryMaps(db, `SELECT type,id,title,description as content,parent_id as parentId FROM work_items WHERE title LIKE ? OR description LIKE ? LIMIT 40`, like, like)
		_ = db.Close()
		for _, row := range items {
			row["projectId"] = project.ID
			row["projectName"] = project.Name
			results = append(results, row)
		}
	}
	return map[string]any{"query": query, "results": results, "totalCount": len(results)}
}
