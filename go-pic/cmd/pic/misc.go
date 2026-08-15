package main

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
)

func cmdActivity(args []string) error {
	if len(args) == 0 {
		return errors.New("activity subcommand required")
	}
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	switch args[0] {
	case "update":
		opts, err := parseOptions(args[1:])
		if err != nil {
			return err
		}
		session := opts["session"]
		if session == "" {
			return errors.New("--session is required")
		}
		_, err = db.Exec(`INSERT INTO session_activity (session_id, task_id, status, current_step_label, last_skill, updated_at) VALUES (?, ?, ?, ?, ?, datetime('now')) ON CONFLICT(session_id) DO UPDATE SET task_id = COALESCE(NULLIF(excluded.task_id, ''), task_id), status = excluded.status, current_step_label = COALESCE(NULLIF(excluded.current_step_label, ''), current_step_label), last_skill = COALESCE(NULLIF(excluded.last_skill, ''), last_skill), updated_at = datetime('now')`, session, opts["task"], firstNonEmpty(opts["status"], "active"), opts["step"], opts["skill"])
		if err != nil {
			return err
		}
		writeJSON(os.Stdout, map[string]any{"ok": true})
		return nil
	case "list":
		rows, err := queryMaps(db, `SELECT sa.session_id,sa.task_id,COALESCE(wi.title,'') as task_title,sa.status,COALESCE(children.done,0) as done,COALESCE(children.total,0) as total,sa.last_skill,sa.updated_at FROM session_activity sa LEFT JOIN work_items wi ON wi.id=sa.task_id AND sa.task_id!='' LEFT JOIN (SELECT parent_id,SUM(CASE WHEN status='done' THEN 1 ELSE 0 END) as done,COUNT(*) as total FROM work_items WHERE parent_id IS NOT NULL GROUP BY parent_id) children ON children.parent_id=sa.task_id WHERE sa.status='active' AND sa.task_id!='' AND datetime(sa.updated_at)>datetime('now','-30 seconds') ORDER BY datetime(sa.updated_at) DESC`)
		if err != nil {
			return err
		}
		if rows == nil {
			rows = []map[string]any{}
		}
		writeJSON(os.Stdout, rows)
		return nil
	default:
		return fmt.Errorf("unknown activity subcommand: %s", args[0])
	}
}

func cmdSearch(args []string) error {
	if len(args) < 1 {
		return errors.New("search requires query")
	}
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	q := "%" + strings.ToLower(args[0]) + "%"
	results, _ := queryMaps(db, `SELECT type,id,title,status,priority,parent_id FROM work_items WHERE lower(title) LIKE ? OR lower(description) LIKE ? ORDER BY created_at,id`, q, q)
	writeJSON(os.Stdout, results)
	return nil
}

func cmdMarkdown(args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	targetType, id, query := "list", "", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--work-item":
			i++
			if i >= len(args) {
				return errors.New("--work-item requires a value")
			}
			targetType, id = "work-item", args[i]
		case "--search":
			i++
			if i >= len(args) {
				return errors.New("--search requires a value")
			}
			targetType, query = "search", args[i]
		default:
			return fmt.Errorf("unknown markdown option: %s", args[i])
		}
	}
	text, err := markdownText(db, targetType, id, query)
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

func markdownText(db *sql.DB, targetType, id, query string) (string, error) {
	switch targetType {
	case "work-item":
		item, err := queryOne(db, `SELECT * FROM work_items WHERE id=?`, id)
		if err != nil {
			return fmt.Sprintf("# Error: Work Item %s not found", id), nil
		}
		children, _ := queryMaps(db, `SELECT * FROM work_items WHERE parent_id=? ORDER BY created_at,id`, id)
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\nType: %s\nStatus: %s\n\n", item["title"], item["type"], item["status"])
		for _, child := range children {
			fmt.Fprintf(&b, "- %s (%s)\n", child["title"], child["status"])
		}
		return b.String(), nil
	case "search":
		return fmt.Sprintf("Search results for %q:\n", query), nil
	default:
		items, _ := queryMaps(db, `SELECT * FROM work_items ORDER BY created_at DESC`)
		var b strings.Builder
		b.WriteString("# Work Items\n\n")
		for _, item := range items {
			fmt.Fprintf(&b, "- %s (%s)\n", item["title"], item["status"])
		}
		return b.String(), nil
	}
}

func cmdWeb(args []string) error {
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
	mux.HandleFunc("/api/", handleAPI)
	mux.HandleFunc("/", serveDashboard)
	fmt.Fprintf(os.Stderr, "pic web listening on http://%s:%s\n", host, port)
	return http.ListenAndServe(host+":"+port, mux)
}

func healthData() map[string]any {
	return map[string]any{"ok": true, "implementation": "go", "version": picVersion, "dashboard_assets": dashboardBuildDir() != ""}
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

func decodeJSONBody(r *http.Request) (map[string]any, error) {
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

func validateString(value any, field string, minLength, maxLength int) (string, error) {
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

func validateEnum(value any, field string, allowed []string) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", field)
	}
	if !contains(allowed, text) {
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
		db, err := openSQLite(path)
		if err != nil {
			projectSchemaMu.Unlock()
			return nil, fmt.Errorf("Failed to open database %s: %w", path, err)
		}
		_ = db.Close()
		if err := initDB(path); err != nil {
			projectSchemaMu.Unlock()
			return nil, fmt.Errorf("Failed to update database schema %s: %w", path, err)
		}
		initializedProjectSchema[path] = true
	}
	projectSchemaMu.Unlock()
	db, err := openSQLite(path)
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
	item, err := workItemByID(db, id)
	if err != nil {
		return nil, false
	}
	children, _ := queryMaps(db, `SELECT `+workItemColumns+` FROM work_items WHERE parent_id=? ORDER BY created_at,id`, id)
	descendants, _ := queryMaps(db, `WITH RECURSIVE tree(id,depth) AS (
		SELECT id,1 FROM work_items WHERE parent_id=?
		UNION ALL SELECT wi.id,tree.depth+1 FROM work_items wi JOIN tree ON wi.parent_id=tree.id
	) SELECT `+workItemColumns+`,tree.depth FROM work_items JOIN tree USING(id) ORDER BY tree.depth,created_at,id`, id)
	_ = attachWorkItemLabels(db, children)
	_ = attachWorkItemLabels(db, descendants)
	dependencies, _ := queryMaps(db, `SELECT r.id,r.work_item_id,r.related_work_item_id AS depends_on_work_item_id,blocker.title,blocker.type,blocker.status FROM work_item_relations r JOIN work_items blocker ON blocker.id=r.related_work_item_id WHERE r.work_item_id=? AND r.relation_type='blocks'`, id)
	gates, _ := queryMaps(db, `SELECT r.id,r.work_item_id,r.related_work_item_id AS gate_work_item_id,gate_item.title,gate_item.status FROM work_item_relations r JOIN work_items gate_item ON gate_item.id=r.related_work_item_id WHERE r.work_item_id=? AND r.relation_type='gates'`, id)
	artifacts, _ := queryMaps(db, `SELECT * FROM work_item_artifacts WHERE work_item_id=? ORDER BY stage,revision DESC`, id)
	checkpoints, _ := queryMaps(db, `SELECT * FROM workflow_checkpoints WHERE work_item_id=? ORDER BY created_at`, id)
	packs, _ := queryMaps(db, `SELECT * FROM work_item_instruction_packs WHERE work_item_id=? ORDER BY version DESC`, id)
	completions, _ := queryMaps(db, `SELECT * FROM work_item_completion_reports WHERE work_item_id=? ORDER BY datetime(created_at) DESC,rowid DESC`, id)
	verifications, _ := queryMaps(db, `SELECT * FROM work_item_verification_reports WHERE work_item_id=? ORDER BY datetime(created_at) DESC,rowid DESC`, id)
	authorizations, _ := queryMaps(db, `SELECT * FROM implementation_authorizations WHERE work_item_id=? ORDER BY created_at DESC,id DESC`, id)
	ready, _ := rowExists(db, `SELECT 1 FROM work_items wi WHERE wi.id=? AND `+workItemReadySQL, id)
	return map[string]any{"workItem": item, "ready": ready, "children": children, "descendants": descendants, "dependencies": dependencies, "gates": gates, "artifacts": artifacts, "checkpoints": checkpoints, "instructionPacks": packs, "authorizations": authorizations, "completionReports": completions, "verificationReports": verifications}, true
}

func handleAPI(w http.ResponseWriter, r *http.Request) {
	registry := readRegistry()
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
	db, err := openProjectDB(project.databasePath())
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer closeProjectDB(db)
	switch {
	case len(parts) == 5 && parts[3] == "work-items" && parts[4] == "labels" && r.Method == http.MethodGet:
		labels, err := queryMaps(db, `SELECT label,COUNT(*) AS count FROM work_item_labels GROUP BY label ORDER BY label`)
		if err != nil {
			writeJSONStatus(w, 500, map[string]any{"error": err.Error()})
			return
		}
		writeJSONResponse(w, map[string]any{"labels": labels})
	case len(parts) == 5 && parts[3] == "work-items" && parts[4] == "ready" && r.Method == http.MethodGet:
		items, err := queryMaps(db, `SELECT `+workItemColumns+` FROM work_items wi WHERE `+workItemReadySQL+` ORDER BY created_at,id`)
		if err == nil {
			err = attachWorkItemLabels(db, items)
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
		items, err := workItemList(db, filterArgs)
		if err != nil {
			writeJSONStatus(w, 400, map[string]any{"error": err.Error()})
			return
		}
		writeJSONResponse(w, map[string]any{"workItems": items})
	case len(parts) == 4 && parts[3] == "work-items" && r.Method == http.MethodPost:
		body, err := decodeJSONBody(r)
		if err != nil {
			writeJSONStatus(w, 400, map[string]any{"error": err.Error()})
			return
		}
		itemType, title := persistedText(body["type"]), strings.TrimSpace(persistedText(body["title"]))
		if !contains([]string{"epic", "feature", "task", "bug", "chore", "gate"}, itemType) || title == "" {
			writeJSONStatus(w, 400, map[string]any{"error": "type and title are required"})
			return
		}
		args := []string{itemType, title}
		for key, option := range map[string]string{"parent_id": "--parent", "description": "--description", "priority": "--priority"} {
			if value := persistedText(body[key]); value != "" {
				args = append(args, option, value)
			}
		}
		if labels, ok := body["labels"].([]any); ok && len(labels) > 0 {
			values := make([]string, len(labels))
			for i := range labels {
				values[i] = persistedText(labels[i])
			}
			args = append(args, "--labels", strings.Join(values, ","))
		}
		if err := workItemCreate(db, args); err != nil {
			writeJSONStatus(w, 400, map[string]any{"error": err.Error()})
			return
		}
		item, _ := queryOne(db, `SELECT `+workItemColumns+` FROM work_items ORDER BY rowid DESC LIMIT 1`)
		if item != nil {
			_ = attachWorkItemLabels(db, []map[string]any{item})
		}
		writeJSONResponse(w, map[string]any{"workItem": item})
	case len(parts) == 6 && parts[3] == "work-items" && parts[5] == "labels" && (r.Method == http.MethodPost || r.Method == http.MethodDelete):
		body, err := decodeJSONBody(r)
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
			values[i] = persistedText(labels[i])
		}
		action := "add"
		if r.Method == http.MethodDelete {
			action = "remove"
		}
		if err := workItemLabel(db, []string{action, parts[4], strings.Join(values, ",")}); err != nil {
			writeJSONStatus(w, 400, map[string]any{"error": err.Error()})
			return
		}
		item, _ := workItemByID(db, parts[4])
		writeJSONResponse(w, map[string]any{"workItem": item})
	case len(parts) == 5 && parts[3] == "work-items" && r.Method == http.MethodGet:
		if detail, ok := workItemDetailForWeb(db, parts[4]); ok {
			writeJSONResponse(w, detail)
		} else {
			writeJSONStatus(w, 404, map[string]any{"error": "Work Item not found"})
		}
	case len(parts) == 6 && parts[3] == "work-items" && parts[5] == "status" && r.Method == http.MethodPatch:
		body, err := decodeJSONBody(r)
		status := persistedText(body["status"])
		if err != nil || !contains([]string{"open", "in_progress", "done", "cancelled"}, status) {
			writeJSONStatus(w, 400, map[string]any{"error": "valid status is required"})
			return
		}
		item, err := workItemSetStatus(db, parts[4], status)
		if err != nil {
			writeJSONStatus(w, 404, map[string]any{"error": err.Error()})
			return
		}
		writeJSONResponse(w, map[string]any{"workItem": item})
	case len(parts) == 4 && parts[3] == "summary" && r.Method == http.MethodGet:
		writeJSONResponse(w, projectSummary(db, project))
	case len(parts) == 4 && parts[3] == "activity" && r.Method == http.MethodGet:
		rows, _ := queryMaps(db, `SELECT sa.session_id, sa.task_id AS work_item_id, COALESCE(wi.title, '') AS work_item_title, sa.status, sa.last_skill, sa.updated_at FROM session_activity sa LEFT JOIN work_items wi ON wi.id=sa.task_id WHERE sa.status='active' AND sa.task_id!='' AND datetime(sa.updated_at)>datetime('now','-30 seconds') ORDER BY datetime(sa.updated_at) DESC`)
		if rows == nil {
			rows = []map[string]any{}
		}
		writeJSONResponse(w, map[string]any{"activity": rows})
	default:
		http.NotFound(w, r)
	}
}

func webReviewQueue(w http.ResponseWriter, registry projectRegistry) {
	items := []map[string]any{}
	for _, project := range registry.Projects {
		db, err := openProjectDB(project.databasePath())
		if err != nil {
			continue
		}
		rows, _ := queryMaps(db, `SELECT id AS workItemId,title AS workItemTitle,type,review_status AS status,created_at AS createdAt FROM work_items WHERE review_status='pending' ORDER BY datetime(created_at) DESC`)
		_ = db.Close()
		for _, row := range rows {
			row["projectId"] = project.ID
			row["projectName"] = project.Name
			items = append(items, row)
		}
	}
	writeJSONResponse(w, map[string]any{"items": items})
}

func registryProjectByID(registry projectRegistry, id string) (registryProject, bool) {
	return findRegistryProject(registry, id)
}

func webProject(project registryProject) map[string]any {
	health := "ok"
	if _, err := os.Stat(project.databasePath()); err != nil {
		health = "missing_db"
	}
	return map[string]any{"id": project.ID, "name": project.Name, "rootPath": project.rootPath(), "databasePath": project.databasePath(), "changelogPath": project.changelogPath(), "health": health, "createdAt": project.createdAt(), "updatedAt": project.updatedAt()}
}

func projectSummary(db *sql.DB, project registryProject) map[string]any {
	statuses, _ := queryKeyCounts(db, `SELECT COALESCE(status,'open') as key,COUNT(*) as count FROM work_items GROUP BY status`)
	types, _ := queryKeyCounts(db, `SELECT type as key,COUNT(*) as count FROM work_items GROUP BY type`)
	priorities, _ := queryKeyCounts(db, `SELECT COALESCE(priority,'medium') as key,COUNT(*) as count FROM work_items GROUP BY priority`)
	reviews, _ := queryKeyCounts(db, `SELECT CASE WHEN review_status IS NULL OR review_status='' THEN 'none' ELSE review_status END as key,COUNT(*) as count FROM work_items GROUP BY key`)
	ready, _ := queryKeyCounts(db, `SELECT CASE WHEN `+workItemReadySQL+` THEN 'ready' ELSE 'blocked' END as key,COUNT(*) as count FROM work_items wi GROUP BY key`)
	var latest string
	_ = db.QueryRow(`SELECT COALESCE(MAX(created_at),'') FROM work_items`).Scan(&latest)
	return map[string]any{"projectId": project.ID, "projectName": project.Name, "rootPath": project.rootPath(), "health": "ok", "statusCounts": statuses, "typeCounts": types, "priorityCounts": priorities, "reviewCounts": reviews, "readinessCounts": ready, "latestActivity": latest}
}

func queryKeyCounts(db *sql.DB, query string) (map[string]int, error) {
	rows, err := queryMaps(db, query)
	if err != nil {
		return map[string]int{}, err
	}
	counts := map[string]int{}
	for _, row := range rows {
		key, _ := row["key"].(string)
		counts[key] = toInt(row["count"])
	}
	return counts, nil
}

func webSearch(registry projectRegistry, query string) map[string]any {
	results := []map[string]any{}
	if strings.TrimSpace(query) == "" {
		return map[string]any{"query": query, "results": results, "totalCount": 0}
	}
	like := "%" + query + "%"
	for _, project := range registry.Projects {
		db, err := openProjectDB(project.databasePath())
		if err != nil {
			continue
		}
		items, _ := queryMaps(db, `SELECT type,id,title,description as content,parent_id as parentId FROM work_items WHERE title LIKE ? OR description LIKE ? LIMIT 40`, like, like)
		_ = db.Close()
		for _, row := range items {
			row["projectId"] = project.ID
			row["projectName"] = project.Name
			results = append(results, row)
		}
	}
	return map[string]any{"query": query, "results": results, "totalCount": len(results)}
}

func workflowEventAdd(db *sql.DB, args []string) error {
	if len(args) < 2 {
		return errors.New("event-add requires Work Item id and event type")
	}
	opts, err := parseOptions(args[2:])
	if err != nil {
		return err
	}
	workItemID, eventType := args[0], args[1]
	if _, err := workItemByID(db, workItemID); err != nil {
		return err
	}
	if eventType == "verify_completed" {
		return errors.New("verify_completed events are managed by verification-save")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if eventType == "implementation_started" || eventType == "review_started" {
		if _, err = tx.Exec(`UPDATE work_items SET status='in_progress',review_status='pending' WHERE id=?`, workItemID); err != nil {
			return err
		}
	} else if eventType == "review_failed" {
		if _, err = tx.Exec(`UPDATE work_items SET status='in_progress',review_status='failed' WHERE id=?`, workItemID); err != nil {
			return err
		}
	}
	id := "wie-" + shortID()
	if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,actor_model,summary,payload_json) VALUES(?,?,?,?,?,?,?)`, id, workItemID, eventType, opts["actor-role"], opts["actor-model"], opts["summary"], normalizeJSONText(opts["payload-json"])); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return outputOne(db, `SELECT * FROM work_item_events WHERE id=?`, id)
}
