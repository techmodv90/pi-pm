package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/earendil-works/task-system/go-pic/internal/dashboard"
	"github.com/earendil-works/task-system/go-pic/internal/project"
	"github.com/earendil-works/task-system/go-pic/internal/schema"
	"github.com/earendil-works/task-system/go-pic/internal/store"
	"github.com/earendil-works/task-system/go-pic/internal/tip"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildPic(t *testing.T) string {
	t.Helper()
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "pic")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = pkgDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}

func buildProductionPic(t *testing.T) string {
	t.Helper()
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "pic")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = pkgDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}

func runPic(t *testing.T, bin string, cwd string, home string, args ...string) any {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Env = append(clearedPiEnv(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pic %v failed: %v\n%s", args, err, out)
	}
	var result any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON from pic %v: %v\n%s", args, err, out)
	}
	return result
}

func runPicError(t *testing.T, bin string, cwd string, home string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Env = append(clearedPiEnv(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("pic %v unexpectedly succeeded: %s", args, out)
	}
	return string(out)
}

func webRequest(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	res := httptest.NewRecorder()
	dashboard.HandleAPI(res, req)
	return res
}

func runSQLite(t *testing.T, dbPath string, sql string) {
	t.Helper()
	cmd := exec.Command("sqlite3", dbPath, sql)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite failed: %v\n%s", err, out)
	}
}

func runMarkdown(t *testing.T, bin string, cwd string, home string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Env = append(clearedPiEnv(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pic %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func asObject(t *testing.T, value any) map[string]any {
	t.Helper()
	obj, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %#v", value)
	}
	return obj
}

func initProject(t *testing.T, bin string) (root string, home string) {
	t.Helper()
	root = t.TempDir()
	if realRoot, err := filepath.EvalSymlinks(root); err == nil {
		root = realRoot
	}
	home = t.TempDir()
	init := asObject(t, runPic(t, bin, root, home, "init", "--name", "demo"))
	if init["initialized"] != true {
		t.Fatalf("init initialized = %#v", init["initialized"])
	}
	return root, home
}

const validVisionArtifact = `{"project_name":"Task System","nature":{"interface":"CLI","lifecycle":"Pipeline","scale":"Team"},"dimensions":{"interface":"CLI","data_flow":"SQLite","user_model":"Owner and agents","lifecycle":"Pipeline","scale":"Team","state":"Persistent DB"},"architecture":{"entry_points":["pic"],"core_modules":["scheduler"],"data_layer":["SQLite"],"integration_points":[],"cross_cutting_concerns":["audit"],"connection_summary":"Commands drive persisted stages."},"user_flows":[{"user_type":"Owner","entry":"CLI","core_loop":"Review","edge_cases":["Rejection"],"exit":"Approval"}],"non_ui_direction":{"type":"CLI","decisions":["JSON output"]},"tech_stack":[{"layer":"Runtime","choice":"Go","rationale":"Existing","reuse":"Current"}]}`

const validBlueprintArtifact = `{"project_info":{"project":"Task System","nature":"CLI + pipeline + team","date":"2026-08-17"},"goals":{"primary_goal":"Reliable workflow","target_audience":"Owner and agents","key_message":"Every transition is durable"},"architecture":{"building_blocks":["CLI","Scheduler","SQLite"],"connection_summary":"CLI drives scheduler state","data_flow":"Inputs -> CLI -> SQLite"},"tech_stack":[{"layer":"Backend","choice":"Go","rationale":"Existing","reuse":"go-pic"}],"file_structure":[{"path":"go-pic/cmd/pic","purpose":"Workflow backend"}],"rri_requirements_matrix":[{"blueprint_section":"Lifecycle","requirements":["REQ-001"],"source_questions":["Q1"]}],"task_decomposition_preview":{"estimated_tasks":1,"tasks":[{"tip_id":"TIP-001","title":"Lifecycle","goal":"Enforce transitions"}],"estimated_effort_minutes":30}}`

const validContractArtifact = `{"project_name":"Task System","deliverables":[{"item":"Lifecycle","details":"Persisted workflow","requirements":["REQ-001"]}],"obligations":[{"id":"OBL-001","requirement_keys":["REQ-001"],"behavior":"Persist workflow state","acceptance":"Given a valid workflow\nWhen it is persisted\nThen the state is queryable"}],"tech_stack":[{"layer":"Backend","choice":"Go","rationale":"Existing stack"}],"task_graph_summary":{"tip_count":8,"estimated_minutes":240},"not_included":["Legacy migration"]}`

func planningArtifactContent(stage string) string {
	if stage == "vision" {
		return validVisionArtifact
	}
	if stage == "blueprint" {
		return validBlueprintArtifact
	}
	if stage == "contracts" {
		return validContractArtifact
	}
	return stage
}

func TestWebAPIUsesGlobalProjectRegistry(t *testing.T) {

	bin := buildPic(t)
	home := t.TempDir()
	rootA := t.TempDir()
	rootB := t.TempDir()
	runPic(t, bin, rootA, home, "init", "--name", "alpha")
	runPic(t, bin, rootB, home, "init", "--name", "beta")
	t.Setenv("HOME", home)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	res := httptest.NewRecorder()
	dashboard.HandleAPI(res, req)
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	projects := body["projects"].([]any)
	if len(projects) != 2 {
		t.Fatalf("projects = %#v", body)
	}

	projectID := projects[0].(map[string]any)["id"].(string)
	req = httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/summary", nil)
	res = httptest.NewRecorder()
	dashboard.HandleAPI(res, req)
	var summary map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary["projectId"] != projectID {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestVersionReportsGoImplementation(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	out := asObject(t, runPic(t, bin, t.TempDir(), t.TempDir(), "--version"))
	if out["implementation"] != "go" || out["sqlite"] != "modernc.org/sqlite" {
		t.Fatalf("--version = %#v", out)
	}
}

func TestInitAndProjectCommands(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	if _, err := os.Stat(filepath.Join(root, ".pi", "tasks.db")); err != nil {
		t.Fatalf("tasks.db not created: %v", err)
	}
	db, err := project.OpenSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if schema.TableExists(db, "task_items") {
		t.Fatal("fresh database created retired task_items table")
	}

	current := asObject(t, runPic(t, bin, root, home, "project", "current"))
	if current["name"] != "demo" {
		t.Fatalf("project current name = %#v", current["name"])
	}
	if current["root_path"] != root {
		t.Fatalf("project current root_path = %#v, want %q", current["root_path"], root)
	}
	if current["database_path"] != filepath.Join(root, ".pi", "tasks.db") {
		t.Fatalf("project current database_path = %#v", current["database_path"])
	}

	list, ok := runPic(t, bin, root, home, "project", "list").([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("project list = %#v", list)
	}
}

func TestWorkItemCommandCutover(t *testing.T) {
	t.Parallel()
	bin := buildProductionPic(t)
	root, home := initProject(t, bin)
	db, err := project.OpenSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	counts := func() int {
		var value int
		if err := db.QueryRow(`SELECT COUNT(*) FROM work_items`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := counts()
	for _, args := range [][]string{
		{"epic", "create", "removed"},
		{"task", "create", "removed"},
		{"task-item", "add", "missing", "removed"},
		{"feature", "start", "removed"},
		{"workflow", "repair-phases", "missing"},
	} {
		if output := runPicError(t, bin, root, home, args...); !strings.Contains(output, "unknown") {
			t.Fatalf("pic %v = %s", args, output)
		}
	}
	if after := counts(); after != before {
		t.Fatalf("removed commands mutated storage: before=%d after=%d", before, after)
	}
	for _, table := range []string{"epics", "tasks", "epic_events", "task_events", "scan_reports", "rri_sessions", "designs", "completion_reports", "task_instruction_packs", "verification_reports", "escalations"} {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != 0 {
			t.Fatalf("fresh database created legacy table %s", table)
		}
	}
}

func createTaskFixture(t *testing.T, bin, root, home string) (string, string) {
	t.Helper()
	epic := asObject(t, runPic(t, bin, root, home, "epic", "create", "Go Port", "--description", "Replace node pic"))
	task := asObject(t, runPic(t, bin, root, home, "task", "create", epic["id"].(string), "Port core commands", "--priority", "high", "--workflow-mode", "standard"))
	return epic["id"].(string), task["id"].(string)
}

func activateTestWorkItemTIP(t *testing.T, dbPath, id string) {
	t.Helper()
	suffix := strings.TrimPrefix(id, "wi-")
	runSQLite(t, dbPath, `INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES('wia-`+suffix+`','`+id+`','task_graph',1,'{}','graph-`+suffix+`'); INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-`+suffix+`','`+id+`','task_graph','wia-`+suffix+`',1,'graph-`+suffix+`','approved'); INSERT INTO work_item_instruction_packs(id,work_item_id,checkpoint_id,version,status,content_json,content_hash,activated_at) VALUES('wip-`+suffix+`','`+id+`','wic-`+suffix+`',1,'active','{}','pack-`+suffix+`',datetime('now'));`)
}

func TestWorkflowMigrationPreservesLegacyRows(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`PRAGMA foreign_keys=ON;
		CREATE TABLE epics(id TEXT PRIMARY KEY,title TEXT NOT NULL,description TEXT DEFAULT '',status TEXT DEFAULT 'open',created_at TEXT DEFAULT (datetime('now')));
		CREATE TABLE tasks(id TEXT PRIMARY KEY,epic_id TEXT NOT NULL REFERENCES epics(id),title TEXT NOT NULL,description TEXT DEFAULT '',status TEXT DEFAULT 'open',priority TEXT DEFAULT 'medium',created_at TEXT DEFAULT (datetime('now')));
		CREATE TABLE task_events(id TEXT PRIMARY KEY,task_id TEXT NOT NULL REFERENCES tasks(id),event_type TEXT NOT NULL,created_at TEXT DEFAULT (datetime('now')));
		INSERT INTO epics(id,title)VALUES('e1','Legacy');
		INSERT INTO tasks(id,epic_id,title)VALUES('t1','e1','Preserved');
		INSERT INTO task_events(id,task_id,event_type)VALUES('ev1','t1','legacy');`)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := project.InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err = project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tasks, events, violations int
	_ = db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id='t1' AND title='Preserved'`).Scan(&tasks)
	_ = db.QueryRow(`SELECT COUNT(*) FROM task_events WHERE id='ev1' AND task_id='t1'`).Scan(&events)
	_ = db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations)
	if tasks != 1 || events != 1 || violations != 0 {
		t.Fatalf("migration preserved tasks=%d events=%d violations=%d", tasks, events, violations)
	}
}

func TestWorkItemSchemaMigration(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`PRAGMA foreign_keys=ON;
		CREATE TABLE epics(id TEXT PRIMARY KEY,title TEXT NOT NULL,description TEXT DEFAULT '',status TEXT DEFAULT 'open',created_at TEXT DEFAULT (datetime('now')));
		CREATE TABLE tasks(id TEXT PRIMARY KEY,epic_id TEXT REFERENCES epics(id),title TEXT NOT NULL,description TEXT DEFAULT '',status TEXT DEFAULT 'open',priority TEXT DEFAULT 'medium',created_at TEXT DEFAULT (datetime('now')));
		CREATE TABLE task_events(id TEXT PRIMARY KEY,task_id TEXT NOT NULL REFERENCES tasks(id),event_type TEXT NOT NULL,payload_json TEXT DEFAULT '',created_at TEXT DEFAULT (datetime('now')));
		INSERT INTO epics(id,title,description,status,created_at) VALUES('e1','Legacy Epic','epic body','in_progress','2026-01-01 00:00:00');
		INSERT INTO tasks(id,epic_id,title,description,status,priority,created_at) VALUES('t1','e1','Legacy Task','task body','open','high','2026-01-02 00:00:00');
		INSERT INTO task_events(id,task_id,event_type,payload_json,created_at) VALUES('ev1','t1','legacy','{"unchanged":true}','2026-01-03 00:00:00');`)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := project.InitDB(dbPath); err != nil {
			t.Fatal(err)
		}
	}
	db, err = project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	resultRows, err := db.Query(`SELECT id,type,parent_id,title,description,status,priority,created_at FROM work_items ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer resultRows.Close()
	type migratedWorkItem struct {
		id, kind, title, description, status, priority, createdAt string
		parentID                                                  sql.NullString
	}
	items := []migratedWorkItem{}
	for resultRows.Next() {
		var item migratedWorkItem
		if err = resultRows.Scan(&item.id, &item.kind, &item.parentID, &item.title, &item.description, &item.status, &item.priority, &item.createdAt); err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	if err = resultRows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].id != "e1" || items[0].kind != "epic" || items[0].parentID.Valid || items[1].id != "t1" || items[1].kind != "task" || items[1].parentID.String != "e1" || items[1].priority != "high" {
		t.Fatalf("work item migration = %#v", items)
	}
	var eventPayload, eventCreated string
	if err = db.QueryRow(`SELECT payload_json,created_at FROM task_events WHERE id='ev1'`).Scan(&eventPayload, &eventCreated); err != nil {
		t.Fatal(err)
	}
	var violations int
	if err = db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
		t.Fatal(err)
	}
	if eventPayload != `{"unchanged":true}` || eventCreated != "2026-01-03 00:00:00" || violations != 0 {
		t.Fatalf("historical artifact payload=%q created=%q violations=%d", eventPayload, eventCreated, violations)
	}
}

func TestWorkItemCRUDAndContainment(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Canonical Epic"))
	feature := asObject(t, runPic(t, bin, root, home, "work-item", "create", "feature", "Nested Feature", "--parent", epic["id"].(string)))
	leaf := asObject(t, runPic(t, bin, root, home, "work-item", "create", "bug", "Executable Bug", "--parent", feature["id"].(string), "--priority", "high"))

	shown := asObject(t, runPic(t, bin, root, home, "work-item", "show", leaf["id"].(string)))
	if shown["type"] != "bug" || shown["parent_id"] != feature["id"] || shown["priority"] != "high" {
		t.Fatalf("shown work item = %#v", shown)
	}
	listed := runPic(t, bin, root, home, "work-item", "list").([]any)
	if len(listed) != 3 {
		t.Fatalf("listed work items = %#v", listed)
	}
	runPic(t, bin, root, home, "work-item", "update", feature["id"].(string), "--title", "Renamed Feature")
	runPic(t, bin, root, home, "work-item", "status", leaf["id"].(string), "in_progress")
	updated := asObject(t, runPic(t, bin, root, home, "work-item", "show", leaf["id"].(string)))
	if updated["status"] != "in_progress" {
		t.Fatalf("updated work item = %#v", updated)
	}

	if out := runPicError(t, bin, root, home, "work-item", "create", "task", "Invalid Child", "--parent", leaf["id"].(string)); !strings.Contains(out, "cannot contain children") {
		t.Fatalf("leaf parent error = %s", out)
	}
	if out := runPicError(t, bin, root, home, "work-item", "update", epic["id"].(string), "--parent", feature["id"].(string)); !strings.Contains(out, "containment cycle") {
		t.Fatalf("cycle error = %s", out)
	}
	unchanged := asObject(t, runPic(t, bin, root, home, "work-item", "show", epic["id"].(string)))
	if unchanged["parent_id"] != nil {
		t.Fatalf("cycle mutation persisted = %#v", unchanged)
	}
}

func TestWorkItemLabels(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	parent := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Labeled Epic", "--labels", "backend,release-v1"))
	child := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Inherited Task", "--parent", parent["id"].(string)))
	other := asObject(t, runPic(t, bin, root, home, "work-item", "create", "bug", "Other Bug", "--labels", "frontend"))

	if labels := child["labels"].([]any); len(labels) != 2 || labels[0] != "backend" || labels[1] != "release-v1" {
		t.Fatalf("inherited labels = %#v", labels)
	}
	runPic(t, bin, root, home, "work-item", "label", "add", child["id"].(string), "urgent,backend")
	runPic(t, bin, root, home, "work-item", "label", "add", child["id"].(string), "urgent")
	labels := runPic(t, bin, root, home, "work-item", "label", "list", child["id"].(string)).([]any)
	if len(labels) != 3 || labels[0] != "backend" || labels[1] != "release-v1" || labels[2] != "urgent" {
		t.Fatalf("labels after idempotent add = %#v", labels)
	}

	andRows := runPic(t, bin, root, home, "work-item", "list", "--label", "backend,urgent").([]any)
	if len(andRows) != 1 || asObject(t, andRows[0])["id"] != child["id"] {
		t.Fatalf("AND label filter = %#v", andRows)
	}
	orRows := runPic(t, bin, root, home, "work-item", "list", "--label-any", "frontend,urgent").([]any)
	if len(orRows) != 2 {
		t.Fatalf("OR label filter = %#v (other=%s)", orRows, other["id"])
	}

	all := runPic(t, bin, root, home, "work-item", "label", "list-all").([]any)
	if len(all) != 4 || asObject(t, all[0])["label"] != "backend" || asObject(t, all[0])["count"] != float64(2) {
		t.Fatalf("all labels = %#v", all)
	}
	runPic(t, bin, root, home, "work-item", "label", "remove", child["id"].(string), "urgent,missing")
	if out := runPicError(t, bin, root, home, "work-item", "label", "add", child["id"].(string), "Invalid Label"); !strings.Contains(out, "invalid label") {
		t.Fatalf("invalid label error = %s", out)
	}
}

func TestNativeWorkItemGenericShowUsesCanonicalShape(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)

	created := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Canonical detail"))
	shown := asObject(t, runPic(t, bin, root, home, "show", created["id"].(string)))
	if asObject(t, shown["work_item"])["id"] != created["id"] {
		t.Fatalf("generic show did not return canonical Work Item detail: %#v", shown)
	}
	// Lean path (owner decision 2026-09-07): a bare task with no legacy
	// pipeline state is ready — the description is the worker input.
	if shown["ready"] != true {
		t.Fatalf("lean standalone Work Item should be ready: %#v", shown["ready"])
	}
	for _, field := range []string{"children", "dependencies", "artifacts", "checkpoints", "instruction_packs", "verification_reports"} {
		if _, ok := shown[field].([]any); !ok {
			t.Fatalf("generic show field %s = %#v", field, shown[field])
		}
	}
}

func TestWorkItemReadinessAndClaim(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Delivery"))
	blocker := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Blocker", "--parent", epic["id"].(string)))
	blocked := asObject(t, runPic(t, bin, root, home, "work-item", "create", "bug", "Blocked", "--parent", epic["id"].(string)))
	gate := asObject(t, runPic(t, bin, root, home, "work-item", "create", "gate", "Approval", "--parent", epic["id"].(string)))
	gated := asObject(t, runPic(t, bin, root, home, "work-item", "create", "chore", "Gated", "--parent", epic["id"].(string)))
	deferred := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Deferred", "--parent", epic["id"].(string), "--deferred", "1"))
	for _, executable := range []map[string]any{blocker, blocked, gated, deferred} {
		activateTestWorkItemTIP(t, filepath.Join(root, ".pi", "tasks.db"), executable["id"].(string))
	}
	runPic(t, bin, root, home, "work-item", "depend", blocked["id"].(string), blocker["id"].(string))
	runPic(t, bin, root, home, "work-item", "gate", gated["id"].(string), gate["id"].(string))

	ready := runPic(t, bin, root, home, "work-item", "ready").([]any)
	if len(ready) != 1 || ready[0].(map[string]any)["id"] != blocker["id"] {
		t.Fatalf("initial ready = %#v", ready)
	}
	if out := runPicError(t, bin, root, home, "work-item", "claim", epic["id"].(string), "worker-1"); !strings.Contains(out, "not executable") {
		t.Fatalf("aggregate claim error = %s", out)
	}
	if out := runPicError(t, bin, root, home, "work-item", "claim", blocked["id"].(string), "worker-1"); !strings.Contains(out, "not ready") {
		t.Fatalf("blocked claim error = %s", out)
	}
	claimed := asObject(t, runPic(t, bin, root, home, "work-item", "claim", blocker["id"].(string), "worker-1"))
	if claimed["claimed_by"] != "worker-1" || claimed["claimed_at"] == "" {
		t.Fatalf("claim = %#v", claimed)
	}
	if out := runPicError(t, bin, root, home, "work-item", "claim", blocker["id"].(string), "worker-2"); !strings.Contains(out, "not ready") {
		t.Fatalf("double claim error = %s", out)
	}
	runSQLite(t, filepath.Join(root, ".pi", "tasks.db"), `UPDATE work_item_instruction_packs SET status='stale' WHERE work_item_id='`+blocker["id"].(string)+`'`)
	runPic(t, bin, root, home, "work-item", "status", blocker["id"].(string), "done")
	runPic(t, bin, root, home, "work-item", "status", gate["id"].(string), "done")
	ready = runPic(t, bin, root, home, "work-item", "ready").([]any)
	readyIDs := map[string]bool{}
	for _, item := range ready {
		readyIDs[item.(map[string]any)["id"].(string)] = true
	}
	if len(ready) != 2 || !readyIDs[blocked["id"].(string)] || !readyIDs[gated["id"].(string)] {
		t.Fatalf("unblocked ready = %#v; deferred=%s", ready, deferred["id"])
	}
}

func TestWorkItemRelateControlsReadinessByRelationType(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	blocker := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Blocker"))
	blocked := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Blocked"))
	related := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Related"))
	gate := asObject(t, runPic(t, bin, root, home, "work-item", "create", "gate", "Approval"))

	runPic(t, bin, root, home, "work-item", "relate", blocked["id"].(string), "blocks", blocker["id"].(string))
	runPic(t, bin, root, home, "work-item", "relate", blocked["id"].(string), "related", related["id"].(string))
	runPic(t, bin, root, home, "work-item", "relate", blocked["id"].(string), "gates", gate["id"].(string))
	if out := runPicError(t, bin, root, home, "work-item", "relate", blocked["id"].(string), "gates", related["id"].(string)); !strings.Contains(out, "is not a gate") {
		t.Fatalf("non-gate relation error = %s", out)
	}

	shown := asObject(t, runPic(t, bin, root, home, "show", blocked["id"].(string)))
	if shown["ready"] != false || len(shown["relations"].([]any)) != 3 {
		t.Fatalf("related Work Item detail = %#v", shown)
	}
}

func boolPtr(value bool) *bool { return &value }

// markedRriPayload builds a publishable marked frontier payload with an
// optional set of open_questions rows so publish-gate tests can vary only the
// frontier rows under test.
func markedRriPayload(t *testing.T, title string, questions []any) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"requirements": []map[string]any{{"key": "REQ-GATE", "priority": "tier1", "title": "Publish gate", "description": "Gate RRI publication", "acceptanceCriteria": "Given a marked RRI payload\nWhen rri-finalize runs\nThen the publish gate applies"}},
		"decisions":    []map[string]any{{"key": "gate_mode", "answer": "Gate marked reports"}},
		"report": map[string]any{
			"project_name": title, "generated": "2026-09-01", "rri_policy_version": 2,
			"requirements_matrix": []map[string]string{{"req_id": "REQ-GATE", "requirement": "Publish gate", "source": "RRI Q#1", "priority": "P0", "persona": "Developer"}},
			"auto_answered":       []map[string]string{}, "decisions_log": []map[string]string{{"decision": "Gate mode", "options_considered": "Gated vs legacy", "chosen": "Gated", "rationale": "Blocking frontier"}},
			"open_questions":    questions,
			"not_yet_specified": []map[string]string{},
			"out_of_scope":      []map[string]string{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func glossaryRriPayload(t *testing.T, requirementTitle, requirementDescription, decisionAnswer string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"requirements": []map[string]any{{"key": "REQ-GLOSSARY", "priority": "tier1", "title": requirementTitle, "description": requirementDescription, "acceptanceCriteria": "Given a resolved requirement\nWhen save_rri_interview runs\nThen the terminology guard applies"}},
		"decisions":    []map[string]any{{"key": "glossary_mode", "answer": decisionAnswer}},
		"report": map[string]any{
			"project_name": "Glossary guard", "generated": "2026-09-01", "rri_policy_version": 2,
			"requirements_matrix": []map[string]string{{"req_id": "REQ-GLOSSARY", "requirement": requirementTitle, "source": "RRI Q#1", "priority": "P0", "persona": "Developer"}},
			"auto_answered":       []map[string]string{}, "decisions_log": []map[string]string{{"decision": "Glossary mode", "options_considered": "Guarded vs legacy", "chosen": "Guarded", "rationale": "Canonical terminology"}},
			"open_questions": []any{}, "not_yet_specified": []map[string]string{}, "out_of_scope": []map[string]string{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

// glossaryApplyPayload is an RRI finalize payload whose resolved report
// carries one explicitly identified glossary update for approval-time
// application (REQ-F1-6).
func glossaryApplyPayload(t *testing.T) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"requirements": []map[string]any{{"key": "REQ-GLOSSARY-APPLY", "priority": "tier1", "title": "Vertical slice delivery", "description": "Deliver one vertical slice", "acceptanceCriteria": "Given a resolved requirement\nWhen approval runs\nThen the glossary gains the resolved terms"}},
		"decisions":    []map[string]any{{"key": "glossary_mode", "answer": "Gate marked reports"}},
		"report": map[string]any{
			"project_name": "Glossary approval", "generated": "2026-09-01", "rri_policy_version": 2,
			"requirements_matrix": []map[string]string{{"req_id": "REQ-GLOSSARY-APPLY", "requirement": "Vertical slice delivery", "source": "RRI Q#1", "priority": "P0", "persona": "Developer"}},
			"auto_answered":       []map[string]string{}, "decisions_log": []map[string]string{{"decision": "Glossary mode", "options_considered": "Guarded vs legacy", "chosen": "Guarded", "rationale": "Canonical terminology"}},
			"open_questions":    []map[string]any{{"id": "Q1", "question": "Which delivery shape?", "status": "resolved", "priority": "P2", "mode": "hitl", "blocks": false, "resolution": map[string]string{"answer": "One vertical slice", "source": "Owner confirm"}}},
			"not_yet_specified": []map[string]string{}, "out_of_scope": []map[string]string{},
			"glossary_updates": []map[string]string{{"term": "Delivery Aggregate", "definition": "The branch-owning delivery aggregate for one approved feature branch.", "avoid": "delivery bucket"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestAggregateWorkItemVerificationAndClosure(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Close Epic"))
	feature := asObject(t, runPic(t, bin, root, home, "work-item", "create", "feature", "Child Feature", "--parent", epic["id"].(string)))
	leaf := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Child Task", "--parent", feature["id"].(string)))
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria,status) VALUES('req-close','`+epic["id"].(string)+`','REQ-001','Required','Given x When y Then z','pending')`)
	if out := runPicError(t, bin, root, home, "work-item", "aggregate-verify", epic["id"].(string), "passed", "verified", "--actor-role", "contractor"); !strings.Contains(out, "open descendants") {
		t.Fatalf("open descendant verification error = %s", out)
	}
	runPic(t, bin, root, home, "work-item", "status", leaf["id"].(string), "done")
	runPic(t, bin, root, home, "work-item", "status", feature["id"].(string), "done")
	report := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epic["id"].(string), "passed", "verified", "--actor-role", "contractor"))
	if report["status"] != "passed" {
		t.Fatalf("aggregate report = %#v", report)
	}
	db, err := project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var requirementStatus string
	if err = db.QueryRow(`SELECT status FROM requirements WHERE id='req-close'`).Scan(&requirementStatus); err != nil || requirementStatus != "satisfied" {
		t.Fatalf("aggregate requirement status = %q, err=%v", requirementStatus, err)
	}
	runPic(t, bin, root, home, "work-item", "aggregate-accept", epic["id"].(string), report["id"].(string), "accepted", "owner accepts", "--actor-role", "owner")
	runPic(t, bin, root, home, "work-item", "aggregate-close", epic["id"].(string))
	closed := asObject(t, runPic(t, bin, root, home, "work-item", "show", epic["id"].(string)))
	if closed["status"] != "done" {
		t.Fatalf("closed aggregate = %#v", closed)
	}
}

func TestFailedAggregateVerificationCreatesCorrectiveBug(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Correct aggregate"))
	completed := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Completed child", "--parent", epic["id"].(string)))
	runPic(t, bin, root, home, "work-item", "status", completed["id"].(string), "done")
	report := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epic["id"].(string), "failed", "release check failed", "--actor-role", "contractor"))
	db, err := project.OpenSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var bugs, links, requirements int
	var completedStatus string
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=? AND type='bug' AND status='open'`, epic["id"]).Scan(&bugs)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_corrective_bugs WHERE verification_report_id=?`, report["id"]).Scan(&links)
	_ = db.QueryRow(`SELECT COUNT(*) FROM requirements WHERE task_id=(SELECT bug_work_item_id FROM work_item_corrective_bugs WHERE verification_report_id=?)`, report["id"]).Scan(&requirements)
	_ = db.QueryRow(`SELECT status FROM work_items WHERE id=?`, completed["id"]).Scan(&completedStatus)
	if bugs != 1 || links != 1 || requirements != 1 || completedStatus != "done" {
		t.Fatalf("corrective bugs=%d links=%d requirements=%d completed=%q report=%#v", bugs, links, requirements, completedStatus, report)
	}
}

func TestAggregateVerifyRebindsStaleDeliveryBranch(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	feature := asObject(t, runPic(t, bin, root, home, "work-item", "create", "feature", "Rebind Feature"))
	leaf := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Rebind Child", "--parent", feature["id"].(string)))
	featureID := feature["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runPic(t, bin, root, home, "work-item", "status", leaf["id"].(string), "done")
	runSQLite(t, dbPath, `INSERT INTO work_item_delivery_states(work_item_id,integration_mode,branch_name,base_branch,base_commit) VALUES('`+featureID+`','branch','feature/stale','develop','stale-base')`)

	// Re-verification rebinds the delivery evidence: branch_name, verified
	// head, and base commit all move to the branch the review actually ran on
	// (a stale binding must not wedge the aggregate out of every transition).
	report := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", featureID, "passed", "rebound to the reviewed delivery branch", "--actor-role", "contractor", "--branch-name", "feature/delivery", "--head-commit", "head-2", "--base-commit", "base-2"))
	db, err := project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var boundBranch, verifiedHead, baseCommit string
	if err := db.QueryRow(`SELECT branch_name,verified_head,base_commit FROM work_item_delivery_states WHERE work_item_id=?`, featureID).Scan(&boundBranch, &verifiedHead, &baseCommit); err != nil {
		t.Fatal(err)
	}
	if boundBranch != "feature/delivery" || verifiedHead != "head-2" || baseCommit != "base-2" {
		t.Fatalf("delivery state not rebound: branch=%q head=%q base=%q", boundBranch, verifiedHead, baseCommit)
	}

	// Acceptance then succeeds with the unchanged rebound evidence.
	decision := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-accept", featureID, report["id"].(string), "accepted", "ship it", "--actor-role", "owner", "--head-commit", "head-2", "--base-commit", "base-2"))
	if decision["decision"] != "accepted" {
		t.Fatalf("aggregate decision = %#v", decision)
	}
}

func TestAggregateDeliveryLifecycle(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	feature := asObject(t, runPic(t, bin, root, home, "work-item", "create", "feature", "Deliver Feature"))
	leaf := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Verified Child", "--parent", feature["id"].(string)))
	featureID := feature["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runPic(t, bin, root, home, "work-item", "status", leaf["id"].(string), "done")
	runSQLite(t, dbPath, `INSERT INTO work_item_delivery_states(work_item_id,integration_mode,branch_name,base_branch,base_commit) VALUES('`+featureID+`','branch','feature/delivery','develop','base-1')`)

	report := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", featureID, "passed", "aggregate checks passed", "--actor-role", "contractor", "--branch-name", "feature/delivery", "--head-commit", "head-1", "--base-commit", "base-1"))
	status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", featureID))
	if status["next_stage"] != "owner_acceptance" || status["verification_report_id"] != report["id"] {
		t.Fatalf("verified aggregate status = %#v", status)
	}
	runSQLite(t, dbPath, `INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES('wia-delivery-graph','`+featureID+`','task_graph',1,'{}','graph-hash'); INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-delivery-graph','`+featureID+`','task_graph','wia-delivery-graph',1,'graph-hash','approved');`)
	if out := runPicError(t, bin, root, home, "work-item", "aggregate-accept", featureID, report["id"].(string), "accepted", "ship it", "--actor-role", "owner", "--head-commit", "head-1", "--base-commit", "base-1"); !strings.Contains(out, "verification is stale") {
		t.Fatalf("stale aggregate verification was accepted: %s", out)
	}
	runSQLite(t, dbPath, `DELETE FROM workflow_checkpoints WHERE id='wic-delivery-graph'`)
	decision := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-accept", featureID, report["id"].(string), "accepted", "ship it", "--actor-role", "owner", "--head-commit", "head-1", "--base-commit", "base-1"))
	if decision["decision"] != "accepted" {
		t.Fatalf("aggregate decision = %#v", decision)
	}
	status = asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", featureID))
	if status["next_stage"] != "merge_pending" || status["merge_status"] != "merge_pending" {
		t.Fatalf("accepted branch aggregate status = %#v", status)
	}
	runPic(t, bin, root, home, "work-item", "aggregate-merge-result", featureID, "head-1", "blocked", "push denied")
	blocked := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", featureID))
	if blocked["next_stage"] != "merge_pending" || blocked["merge_status"] != "blocked" || blocked["merge_error"] != "push denied" {
		t.Fatalf("blocked merge status = %#v", blocked)
	}
	runPic(t, bin, root, home, "work-item", "aggregate-merge-result", featureID, "head-1", "merged", "merge-1")
	closed := asObject(t, runPic(t, bin, root, home, "work-item", "show", featureID))
	if closed["status"] != "done" {
		t.Fatalf("merged aggregate = %#v", closed)
	}

	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Coordinate Release"))
	epictask := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Epic Child", "--parent", epic["id"].(string)))
	runPic(t, bin, root, home, "work-item", "status", epictask["id"].(string), "done")
	epicReport := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epic["id"].(string), "passed", "aggregate checks passed", "--actor-role", "contractor", "--branch-name", "feature/ignored", "--head-commit", "head-epic", "--base-commit", "base-1"))
	epicStatus := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", epic["id"].(string)))
	if epicStatus["integration_mode"] != "coordination" || epicStatus["next_stage"] != "owner_acceptance" {
		t.Fatalf("epic default delivery mode = %#v", epicStatus)
	}
	runPic(t, bin, root, home, "work-item", "aggregate-accept", epic["id"].(string), epicReport["id"].(string), "accepted", "owner accepts", "--actor-role", "owner")
}

func TestTaskPlanRejectsDependencyCycle(t *testing.T) {
	t.Parallel()
	plan := `{"version":1,"execution_policy":"parallel_allowed","nodes":[{"key":"T01","name":"One","goal":"One","requirement_keys":["REQ-001"],"depends_on":["T02"],"priority":"P1","module":"x","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"true","required":true}]},{"key":"T02","name":"Two","goal":"Two","requirement_keys":["REQ-001"],"depends_on":["T01"],"priority":"P1","module":"x","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"true","required":true}]}]}`
	_, err := tip.ParseTaskPlanJSON("```task-plan-json\n" + plan + "\n```")
	if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestTaskPlanV2RequiresExplicitSkillFamilies(t *testing.T) {
	t.Parallel()
	base := `{"version":2,"execution_policy":"strict_sequential","nodes":[{"key":"T01","name":"One","goal":"One","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"x",%s"files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"true","required":true}]}]}`
	if _, err := tip.ParseTaskPlanJSON("```task-plan-json\n" + strings.Replace(base, "%s", "", 1) + "\n```"); err == nil || !strings.Contains(err.Error(), "requires skillFamilies") {
		t.Fatalf("missing skillFamilies error = %v", err)
	}
	plan, err := tip.ParseTaskPlanJSON("```task-plan-json\n" + strings.Replace(base, "%s", `"skillFamilies":[],`, 1) + "\n```")
	if err != nil || plan.Nodes[0].SkillFamilies == nil || len(*plan.Nodes[0].SkillFamilies) != 0 {
		t.Fatalf("explicit empty skillFamilies plan=%#v err=%v", plan, err)
	}
}

func TestFindDBFromGitWorktree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	if err := project.InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"commit", "--allow-empty", "-m", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	worktree := filepath.Join(t.TempDir(), "worktree")
	cmd := exec.Command("git", "worktree", "add", "--detach", worktree)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add failed: %v\n%s", err, out)
	}
	got, gotErr := filepath.EvalSymlinks(project.FindDB(worktree))
	want, wantErr := filepath.EvalSymlinks(dbPath)
	if gotErr != nil || wantErr != nil || got != want {
		t.Fatalf("project.FindDB(worktree) = %q (%v), want %q (%v)", got, gotErr, want, wantErr)
	}
}

func createActivePackFixture(t *testing.T, bin, root, home, taskID string) map[string]any {
	t.Helper()
	requirement := asObject(t, runPic(t, bin, root, home, "workflow", "requirement-add", taskID, "Complete work", "--key", "REQ-001", "--acceptance-criteria", "Given valid context\nWhen work runs\nThen it completes"))
	content := `{"schemaVersion":3,"skillFamilies":[],"goal":"Complete work.","files":["work.go"],"business_rules":["Complete the assigned work."],"validation_rules":["Not applicable: no input"],"error_handling":["Return errors."],"state_transitions":["Not applicable: no state"],"contract_obligations":["Preserve existing behavior."],"constraints":{"scope_roots":["work.go"]},"verification":[{"command":"go test ./...","required":true}]}`
	return asObject(t, runPic(t, bin, root, home, "workflow", "instruction-pack-save", taskID, "--source-type", "standalone_task", "--content-json", content, "--requirement-ids-json", `["`+requirement["id"].(string)+`"]`, "--activate", "1"))
}

func createApprovedTaskDesignFixture(t *testing.T, bin, root, home, taskID string) map[string]any {
	t.Helper()
	shown := asObject(t, runPic(t, bin, root, home, "task", "show", taskID))
	blueprint := fmt.Sprint(asObject(t, shown["task"])["description"])
	design := asObject(t, runPic(t, bin, root, home, "workflow", "design-save", taskID, "--blueprint", blueprint))
	return asObject(t, runPic(t, bin, root, home, "workflow", "design-status", taskID, "approved", "--design-id", design["id"].(string)))
}

func createLegacyVerificationFixture(t *testing.T, bin, root, home, taskID string) (string, string) {
	t.Helper()
	requirement := asObject(t, runPic(t, bin, root, home, "workflow", "requirement-add", taskID, "Verified behavior"))
	runPic(t, bin, root, home, "task", "update", taskID, "--status", "in_progress", "--review-status", "passed")
	runPic(t, bin, root, home, "workflow", "completion-save", taskID, "done", "--summary", "done", "--files-changed-json", `[]`, "--tests-run-json", `[]`, "--acceptance-results-json", `[]`, "--issues-json", `[]`, "--deviations-json", `[]`, "--suggestions-json", `[]`)
	requirementID := requirement["id"].(string)
	return requirementID, `[{"requirement_id":"` + requirementID + `","status":"pass","evidence":"go test"}]`
}

func TestValidateInstructionPackVerificationContract(t *testing.T) {
	t.Parallel()
	base := tip.InstructionPackContent{
		Goal: "Change source", Files: []string{"src/main.ts"}, BusinessRules: []any{"rule"}, ValidationRules: []any{"rule"},
		ErrorHandling: []any{"rule"}, StateTransitions: []any{"rule"}, ContractObligations: []any{"rule"}, SchemaVersion: 2,
		Constraints:  map[string]any{"generated_files": []any{"test-results/**"}},
		Verification: []any{map[string]any{"command": "npm test", "required": true, "expected_writes": []any{"test-results/**"}}},
	}
	if err := tip.ValidateInstructionPackContent(base); err != nil {
		t.Fatalf("valid verification contract rejected: %v", err)
	}
	failedGate := base
	failedGate.Verification = []any{map[string]any{"required": true}}
	if err := tip.ValidateInstructionPackContent(failedGate); err == nil || !strings.Contains(err.Error(), "verification command") {
		t.Fatalf("verification without command accepted: %v", err)
	}
	missingSetup := base
	missingSetup.Verification = []any{map[string]any{"command": "npm test", "required": true, "requires": []any{"dev-server"}}}
	if err := tip.ValidateInstructionPackContent(missingSetup); err == nil || !strings.Contains(err.Error(), "setup_commands") {
		t.Fatalf("service prerequisite without setup accepted: %v", err)
	}
}

func TestPipelineClaimAcceptsCurrentPlanningStageWithoutTIP(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Durable planning"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "rri"); !strings.Contains(out, "current planning stage is scan") {
		t.Fatalf("out-of-order RRI claim was not rejected: %s", out)
	}
	runSQLite(t, dbPath, `INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES('wia-scan','`+id+`','scan',1,'<scan_report/>','scan-hash'); INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-scan','`+id+`','scan','wia-scan',1,'scan-hash','accepted');`)

	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "rri"))
	if claim["stage"] != "rri" || claim["instruction_pack_id"] != "" {
		t.Fatalf("RRI planning claim = %#v", claim)
	}
}

func TestPipelineClaimBindsCanonicalWorkItemTIP(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Canonical pipeline leaf"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES('wia-pipeline','`+id+`','task_graph',1,'{}','graph-hash'); INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-pipeline','`+id+`','task_graph','wia-pipeline',1,'graph-hash','approved'); INSERT INTO work_item_instruction_packs(id,work_item_id,checkpoint_id,version,status,content_json,content_hash,activated_at) VALUES('wip-pipeline','`+id+`','wic-pipeline',1,'active','{}','pack-hash',datetime('now'));`)

	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	if claim["task_id"] != id || claim["instruction_pack_id"] != "wip-pipeline" || claim["instruction_pack_version"] != float64(1) || claim["instruction_pack_hash"] != "pack-hash" {
		t.Fatalf("canonical claim = %#v", claim)
	}
	if claim["effective_contract_snapshot_id"] != "" || claim["effective_contract_snapshot_hash"] != "" {
		t.Fatalf("canonical claim retained snapshot dependency = %#v", claim)
	}
}

func TestCurrentExecutionRejectsStaleReviewVerdict(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Candidate lineage"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	activateTestWorkItemTIP(t, dbPath, id)
	suffix := strings.TrimPrefix(id, "wi-")
	runSQLite(t, dbPath, `
		INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,integrated_patch_path,integrated_patch_hash,artifact_saved_at,integrated_at,completed_at,advanced_at) VALUES('pr-old','`+id+`','worker',1,'completed','lease-old',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','old.patch','old-hash',datetime('now'),datetime('now'),datetime('now'),datetime('now'));
		INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,candidate_run_id,candidate_patch_hash,result_json,completed_at,advanced_at) VALUES('pr-old-review','`+id+`','review',1,'completed','lease-old-review',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','pr-old','old-hash','{"review_status":"passed","candidate_run_id":"pr-old","candidate_patch_hash":"old-hash","notes":"old","findings":[]}',datetime('now'),datetime('now'));
		UPDATE work_item_instruction_packs SET status='stale' WHERE id='wip-`+suffix+`';
		INSERT INTO work_item_instruction_packs(id,work_item_id,checkpoint_id,version,status,content_json,content_hash,activated_at) VALUES('wip-current','`+id+`','wic-`+suffix+`',2,'active','{}','pack-current',datetime('now'));
		INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,integrated_patch_path,integrated_patch_hash,artifact_saved_at,integrated_at,completed_at) VALUES('pr-current','`+id+`','worker',2,'completed','lease-current',datetime('now'),'wip-current',2,'pack-current','current.patch','current-hash',datetime('now'),datetime('now'),datetime('now'));
		INSERT INTO work_item_completion_reports(id,work_item_id,pipeline_run_id,instruction_pack_id,instruction_pack_version,instruction_pack_hash,status) VALUES('wicr-current','`+id+`','pr-current','wip-current',2,'pack-current','done');
		UPDATE work_items SET review_status='passed' WHERE id='`+id+`';`)
	if out := runPicError(t, bin, root, home, "work-item", "verification-save", id, "wicr-current", "passed", "verified", "--actor-role", "contractor"); !strings.Contains(out, "passed review") {
		t.Fatalf("verification accepted stale review authority: %s", out)
	}
	status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["next_stage"] != "review" {
		t.Fatalf("stale review changed current workflow stage: %#v", status)
	}
}

func TestVerificationAfterAllPipelineActivityAnchorsCompletionReport(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (string, string, string, string) {
		t.Helper()
		bin := buildPic(t)
		root, home := initProject(t, bin)
		item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Verified legacy lineage"))
		id := item["id"].(string)
		dbPath := filepath.Join(root, ".pi", "tasks.db")
		activateTestWorkItemTIP(t, dbPath, id)
		suffix := strings.TrimPrefix(id, "wi-")
		// The seeded rows are migration-time legacy evidence, so the version
		// records are cleared and the next open performs the migration
		// reconciliation exactly as it would on a pre-migration database.
		runSQLite(t, dbPath, `
			INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,integrated_patch_path,integrated_patch_hash,artifact_saved_at,integrated_at,result_json,created_at,completed_at,advanced_at) VALUES('pr-verified','`+id+`','worker',1,'completed','lease-verified',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','verified.patch','verified-hash','2026-01-01 00:00:01','2026-01-01 00:00:02','{}','2026-01-01 00:00:01','2026-01-01 00:00:02','2026-01-01 00:00:02');
			INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,candidate_run_id,candidate_patch_hash,result_json,created_at,completed_at,advanced_at) VALUES('pr-verified-review','`+id+`','review',1,'completed','lease-review',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','pr-verified','verified-hash','{"review_status":"passed","candidate_run_id":"pr-verified","candidate_patch_hash":"verified-hash","notes":"passed","findings":[]}','2026-01-01 00:00:03','2026-01-01 00:00:03','2026-01-01 00:00:03');
			INSERT INTO work_item_completion_reports(id,work_item_id,pipeline_run_id,instruction_pack_id,instruction_pack_version,instruction_pack_hash,status,created_at) VALUES('wicr-verified','`+id+`','pr-verified','wip-`+suffix+`',1,'pack-`+suffix+`','done','2026-01-01 00:00:04');
			INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,integrated_patch_hash,artifact_saved_at,created_at,completed_at) VALUES('pr-noop-before','`+id+`','worker',2,'completed','lease-noop',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','empty-hash','2026-01-01 00:00:05','2026-01-01 00:00:05','2026-01-01 00:00:05');
			INSERT INTO work_item_verification_reports(id,work_item_id,completion_report_id,status,summary,verified_by_role,created_at) VALUES('wivr-verified','`+id+`','wicr-verified','passed','verified after retry','contractor','2026-01-01 00:00:06');
			DELETE FROM schema_migrations;`)
		return bin, root, home, id
	}

	t.Run("verification after retry anchors its report", func(t *testing.T) {
		bin, root, home, id := setup(t)
		status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
		if status["next_stage"] != "done" || status["pipeline_stage"] != "" || status["completion_report_id"] != "wicr-verified" {
			t.Fatalf("verified report lost to older retry: %#v", status)
		}
		shown := asObject(t, runPic(t, bin, root, home, "show", id))
		if asObject(t, shown["work_item"])["status"] != "done" || asObject(t, shown["work_item"])["review_status"] != "passed" {
			t.Fatalf("legacy verified child was not reconciled: %#v", shown)
		}
	})

	t.Run("pipeline activity after verification invalidates the report", func(t *testing.T) {
		bin, root, home, id := setup(t)
		dbPath := filepath.Join(root, ".pi", "tasks.db")
		suffix := strings.TrimPrefix(id, "wi-")
		runSQLite(t, dbPath, `INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,integrated_patch_hash,artifact_saved_at,created_at,completed_at) VALUES('pr-after-verification','`+id+`','worker',3,'completed','lease-after',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','after-hash','2026-01-01 00:00:07','2026-01-01 00:00:07','2026-01-01 00:00:07');`)
		status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
		if status["next_stage"] != "review" || status["completion_report_id"] != "" {
			t.Fatalf("post-verification activity retained acceptance authority: %#v", status)
		}
	})

	t.Run("legacy owner rejection remains retryable", func(t *testing.T) {
		bin, root, home, id := setup(t)
		runSQLite(t, filepath.Join(root, ".pi", "tasks.db"), `INSERT INTO work_item_owner_decisions(id,work_item_id,completion_report_id,decision,notes,decided_by_role) VALUES('wiod-rejected','`+id+`','wicr-verified','rejected','needs changes','owner'); UPDATE work_items SET status='open' WHERE id='`+id+`';`)
		shown := asObject(t, runPic(t, bin, root, home, "show", id))
		if asObject(t, shown["work_item"])["status"] != "open" {
			t.Fatalf("legacy owner rejection was auto-closed: %#v", shown)
		}
	})
}

func TestTIPRevisionInvalidatesActiveExecution(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Revise active execution"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,task_id,requirement_key,title,acceptance_criteria) VALUES('req-revision','`+id+`','REQ-REV','Revision','Given valid context
When work runs
Then it completes'); INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES('wia-revision','`+id+`','task_graph',1,'{}','graph-revision'); INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-revision','`+id+`','task_graph','wia-revision',1,'graph-revision','approved');`)
	content := `{"schemaVersion":3,"skillFamilies":[],"goal":"Complete work.","files":["work.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["work.go"]},"verification":[{"command":"go test ./...","required":true}]}`
	runPic(t, bin, root, home, "workflow", "instruction-pack-save", id, "--source-type", "standalone_task", "--content-json", content, "--requirement-ids-json", `["req-revision"]`, "--activate", "1")
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	runPic(t, bin, root, home, "work-item", "status", id, "in_progress")
	runSQLite(t, dbPath, `UPDATE work_items SET status='done' WHERE id='`+id+`'`)
	content = strings.Replace(content, "Complete work.", "Complete revised work.", 1)
	runPic(t, bin, root, home, "workflow", "instruction-pack-save", id, "--source-type", "standalone_task", "--content-json", content, "--requirement-ids-json", `["req-revision"]`, "--activate", "1")

	shown := asObject(t, runPic(t, bin, root, home, "show", id))
	if asObject(t, shown["work_item"])["status"] != "open" || shown["ready"] != true {
		t.Fatalf("TIP revision retained completed execution authority: %#v", shown)
	}
	runs := runPic(t, bin, root, home, "workflow", "pipeline-runs", id).([]any)
	if asObject(t, runs[0])["id"] != claim["id"] || asObject(t, runs[0])["status"] != "cancelled" {
		t.Fatalf("TIP revision retained active run: %#v", runs)
	}
}

func TestCancellationRevokesPipelineLease(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Cancel execution"))
	id := item["id"].(string)
	activateTestWorkItemTIP(t, filepath.Join(root, ".pi", "tasks.db"), id)
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	runPic(t, bin, root, home, "work-item", "status", id, "in_progress")
	runPic(t, bin, root, home, "work-item", "status", id, "cancelled")

	runs := runPic(t, bin, root, home, "workflow", "pipeline-runs", id).([]any)
	if asObject(t, runs[0])["status"] != "cancelled" {
		t.Fatalf("cancelled Work Item retained active lease: %#v", runs)
	}
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-complete", claim["id"].(string), claim["lease_token"].(string), "completed"); !strings.Contains(out, "stale or invalid lease") {
		t.Fatalf("late completion was not rejected: %s", out)
	}
	if out := runPicError(t, bin, root, home, "work-item", "status", id, "in_progress"); !strings.Contains(out, "new TIP generation") {
		t.Fatalf("cancelled Work Item resumed without a new generation: %s", out)
	}
}

func TestEpicCancellationCascadesToActiveDescendants(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Cancel delivery"))
	feature := asObject(t, runPic(t, bin, root, home, "work-item", "create", "feature", "Active area", "--parent", epic["id"].(string)))
	active := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Active child", "--parent", feature["id"].(string)))
	completed := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Completed child", "--parent", feature["id"].(string)))
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	activateTestWorkItemTIP(t, dbPath, active["id"].(string))
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", active["id"].(string), "worker"))
	runPic(t, bin, root, home, "work-item", "status", active["id"].(string), "in_progress")
	runSQLite(t, dbPath, `UPDATE work_items SET status='in_progress' WHERE id='`+feature["id"].(string)+`'; UPDATE work_items SET status='done' WHERE id='`+completed["id"].(string)+`';`)

	runPic(t, bin, root, home, "work-item", "status", epic["id"].(string), "cancelled")

	for id, expected := range map[string]string{
		epic["id"].(string):      "cancelled",
		feature["id"].(string):   "cancelled",
		active["id"].(string):    "cancelled",
		completed["id"].(string): "done",
	} {
		shown := asObject(t, runPic(t, bin, root, home, "show", id))
		if status := asObject(t, shown["work_item"])["status"]; status != expected {
			t.Fatalf("Work Item %s status=%v want=%s", id, status, expected)
		}
	}
	runs := runPic(t, bin, root, home, "workflow", "pipeline-runs", active["id"].(string)).([]any)
	if asObject(t, runs[0])["status"] != "cancelled" {
		t.Fatalf("descendant retained active pipeline: %#v", runs)
	}
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-complete", claim["id"].(string), claim["lease_token"].(string), "completed"); !strings.Contains(out, "stale or invalid lease") {
		t.Fatalf("descendant accepted late completion: %s", out)
	}
}

func TestExecutableOwnerAcceptanceIsRemoved(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Contractor-owned closure"))
	if out := runPicError(t, bin, root, home, "work-item", "accept", item["id"].(string), "unused", "rejected", "needs changes", "--actor-role", "owner"); !strings.Contains(out, "only to aggregate Work Items") {
		t.Fatalf("executable owner acceptance remained available: %s", out)
	}
}

func TestReadinessRelationsRejectTransitiveCycle(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	a := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "A"))["id"].(string)
	b := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "B"))["id"].(string)
	c := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "C"))["id"].(string)
	runPic(t, bin, root, home, "work-item", "depend", a, b)
	runPic(t, bin, root, home, "work-item", "depend", b, c)
	if out := runPicError(t, bin, root, home, "work-item", "depend", c, a); !strings.Contains(out, "dependency cycle") {
		t.Fatalf("transitive dependency cycle accepted: %s", out)
	}
}

func TestExpiredWorkerLeaseReopensWorkItem(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Expire worker"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	activateTestWorkItemTIP(t, dbPath, id)
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	runPic(t, bin, root, home, "work-item", "status", id, "in_progress")
	runSQLite(t, dbPath, `UPDATE pipeline_runs SET lease_expires_at=datetime('now','-1 second') WHERE id='`+claim["id"].(string)+`'`)
	runPic(t, bin, root, home, "workflow", "pipeline-active")
	shown := asObject(t, runPic(t, bin, root, home, "show", id))
	if asObject(t, shown["work_item"])["status"] != "open" || shown["ready"] != true {
		t.Fatalf("expired Worker left Work Item stranded: %#v", shown)
	}
}

func TestPipelinePendingRetiresStaleGenerations(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Bounded recovery"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	activateTestWorkItemTIP(t, dbPath, id)
	suffix := strings.TrimPrefix(id, "wi-")
	runSQLite(t, dbPath, `UPDATE work_item_instruction_packs SET status='stale' WHERE id='wip-`+suffix+`'; INSERT INTO work_item_instruction_packs(id,work_item_id,checkpoint_id,version,status,content_json,content_hash) VALUES('wip-current','`+id+`','wic-`+suffix+`',2,'active','{}','pack-current'); INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,error,completed_at) VALUES('pr-stale','`+id+`','worker',1,'blocked','lease-stale',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','old failure',datetime('now')); INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,error,completed_at) VALUES('pr-current','`+id+`','worker',2,'blocked','lease-current',datetime('now'),'wip-current',2,'pack-current','current failure',datetime('now'));`)

	pending := runPic(t, bin, root, home, "workflow", "pipeline-pending").([]any)
	if len(pending) != 1 || asObject(t, pending[0])["id"] != "pr-current" {
		t.Fatalf("pending recovery replayed stale generations: %#v", pending)
	}
	var staleAdvanced string
	db, err := project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.QueryRow(`SELECT advanced_at FROM pipeline_runs WHERE id='pr-stale'`).Scan(&staleAdvanced); err != nil || staleAdvanced == "" {
		t.Fatalf("stale generation was not retired: advanced=%q err=%v", staleAdvanced, err)
	}
}

func TestPipelineReviewClaimAcceptsInProgressCandidate(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Review handoff"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	activateTestWorkItemTIP(t, dbPath, id)
	worker := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	runPic(t, bin, root, home, "work-item", "status", id, "in_progress")
	runSQLite(t, dbPath, `UPDATE pipeline_runs SET status='completed',artifact_saved_at=datetime('now'),integrated_patch_path='candidate.patch',integrated_patch_hash='patch-hash',completed_at=datetime('now') WHERE id='`+worker["id"].(string)+`';`)

	review := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "review"))
	if review["candidate_run_id"] != worker["id"] || review["candidate_patch_hash"] != "patch-hash" {
		t.Fatalf("review claim = %#v", review)
	}
}

func TestPipelineReviewRetryAcceptsReopenedCandidate(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Review retry"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	activateTestWorkItemTIP(t, dbPath, id)
	worker := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	runSQLite(t, dbPath, `UPDATE pipeline_runs SET status='completed',artifact_saved_at=datetime('now'),integrated_patch_path='candidate.patch',integrated_patch_hash='patch-hash',completed_at=datetime('now') WHERE id='`+worker["id"].(string)+`';`)
	runPic(t, bin, root, home, "work-item", "status", id, "open")

	review := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "review"))
	if review["candidate_run_id"] != worker["id"] || review["candidate_patch_hash"] != "patch-hash" {
		t.Fatalf("review retry claim = %#v", review)
	}
}

func TestMaterializedChildClaimRequiresCurrentParentAuthorization(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	parent := asObject(t, runPic(t, bin, root, home, "work-item", "create", "feature", "Authorized graph"))
	child := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Materialized child", "--parent", parent["id"].(string)))
	parentID, childID := parent["id"].(string), child["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `
		INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES('wia-parent','`+parentID+`','task_graph',1,'{"version":3,"nodes":[]}','graph-hash');
		INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-parent','`+parentID+`','task_graph','wia-parent',1,'graph-hash','approved');
		INSERT INTO work_item_materializations(root_work_item_id,checkpoint_id,node_key,work_item_id) VALUES('`+parentID+`','wic-parent','T01','`+childID+`');
		INSERT INTO implementation_authorizations(id,work_item_id,task_graph_checkpoint_id,authorized_by) VALUES('wiauth-parent','`+parentID+`','wic-parent','owner');
		INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES('wia-other','`+parentID+`','task_graph',2,'{"version":3,"nodes":[]}','other-hash');
		INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-other','`+parentID+`','task_graph','wia-other',2,'other-hash','approved');
		INSERT INTO work_item_instruction_packs(id,work_item_id,checkpoint_id,version,status,content_json,content_hash) VALUES('wip-child','`+childID+`','wic-other',1,'active','{}','pack-hash');`)

	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", childID, "worker"); !strings.Contains(out, "active instruction pack is not bound to the authorized parent materialization") {
		t.Fatalf("mismatched materialization claim = %s", out)
	}
}

func TestCanonicalWorkItemReviewAndCompletionEvidence(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Canonical evidence leaf"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES('wia-evidence','`+id+`','task_graph',1,'{}','graph-hash'); INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-evidence','`+id+`','task_graph','wia-evidence',1,'graph-hash','approved'); INSERT INTO work_item_instruction_packs(id,work_item_id,checkpoint_id,version,status,content_json,content_hash,activated_at) VALUES('wip-evidence','`+id+`','wic-evidence',1,'active','{}','pack-hash',datetime('now')); INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,integrated_patch_path,integrated_patch_hash,artifact_saved_at,integrated_at) VALUES('pr-evidence','`+id+`','worker',1,'completed','lease-evidence',datetime('now','+1 hour'),'wip-evidence',1,'pack-hash','candidate.patch','patch-hash',datetime('now'),datetime('now')); INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,candidate_run_id,candidate_patch_hash,result_json,completed_at) VALUES('pr-evidence-review','`+id+`','review',1,'completed','lease-review',datetime('now','+1 hour'),'wip-evidence',1,'pack-hash','pr-evidence','patch-hash','{"review_status":"passed","candidate_run_id":"pr-evidence","candidate_patch_hash":"patch-hash"}',datetime('now'));`)

	review := asObject(t, runPic(t, bin, root, home, "work-item", "review", id, "passed", "--notes", "candidate accepted", "--pipeline-run-id", "pr-evidence-review"))
	if review["review_status"] != "passed" || review["review_notes"] != "candidate accepted" {
		t.Fatalf("canonical review = %#v", review)
	}
	report := asObject(t, runPic(t, bin, root, home, "work-item", "completion-save", id, "done", "--pipeline-run-id", "pr-evidence", "--summary", "integrated"))
	if report["instruction_pack_id"] != "wip-evidence" || report["instruction_pack_hash"] != "pack-hash" || report["pipeline_run_id"] != "pr-evidence" {
		t.Fatalf("canonical completion = %#v", report)
	}
	detail := asObject(t, runPic(t, bin, root, home, "show", id))
	if len(detail["completion_reports"].([]any)) != 1 {
		t.Fatalf("canonical detail = %#v", detail)
	}
}

func TestExecutableWorkItemLifecycleUsesTIPAndGuardedClosure(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Executable lifecycle"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES('wia-life','`+id+`','task_graph',1,'{}','graph-hash'); INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-life','`+id+`','task_graph','wia-life',1,'graph-hash','approved'); INSERT INTO work_item_instruction_packs(id,work_item_id,checkpoint_id,version,status,content_json,content_hash,activated_at) VALUES('wip-life','`+id+`','wic-life',1,'active','{}','pack-hash',datetime('now')); INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,integrated_patch_path,integrated_patch_hash,artifact_saved_at,integrated_at) VALUES('pr-life','`+id+`','worker',1,'completed','lease-life',datetime('now','+1 hour'),'wip-life',1,'pack-hash','candidate.patch','patch-hash',datetime('now'),datetime('now'));`)

	status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["workflow_kind"] != "execution" || status["next_stage"] != "review" || status["pipeline_stage"] != "review" {
		t.Fatalf("executable workflow status = %#v", status)
	}
	if out := runPicError(t, bin, root, home, "work-item", "status", id, "done"); !strings.Contains(out, "current integrated Completion Report") {
		t.Fatalf("unguarded done error = %s", out)
	}

	runSQLite(t, dbPath, `INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,candidate_run_id,candidate_patch_hash,result_json,completed_at) VALUES('pr-life-review','`+id+`','review',1,'completed','lease-review',datetime('now','+1 hour'),'wip-life',1,'pack-hash','pr-life','patch-hash','{"review_status":"passed","candidate_run_id":"pr-life","candidate_patch_hash":"patch-hash"}',datetime('now'));`)
	runPic(t, bin, root, home, "work-item", "review", id, "passed", "--notes", "candidate accepted", "--pipeline-run-id", "pr-life-review")
	completion := asObject(t, runPic(t, bin, root, home, "work-item", "completion-save", id, "done", "--pipeline-run-id", "pr-life", "--summary", "integrated"))
	if out := runPicError(t, bin, root, home, "work-item", "verification-save", id, "missing", "passed", "checks passed", "--actor-role", "contractor"); !strings.Contains(out, "current integrated Completion Report") {
		t.Fatalf("unbound verification error = %s", out)
	}

	if out := runPicError(t, bin, root, home, "work-item", "verification-save", id, completion["id"].(string), "passed", "checks passed"); !strings.Contains(out, "actor_role=contractor") {
		t.Fatalf("verification without contractor authority = %s", out)
	}
	child := exec.Command(bin, "work-item", "verification-save", id, completion["id"].(string), "passed", "checks passed", "--actor-role", "contractor")
	child.Dir = root
	child.Env = append(os.Environ(), "HOME="+home, "PI_TASK_AGENT_NAME=task-reviewer")
	if out, err := child.CombinedOutput(); err == nil || !strings.Contains(string(out), "cannot mutate Work Item lifecycle") {
		t.Fatalf("child agent assumed contractor authority: err=%v out=%s", err, out)
	}
	failed := asObject(t, runPic(t, bin, root, home, "work-item", "verification-save", id, completion["id"].(string), "failed", "bootstrap evidence failed", "--actor-role", "contractor"))
	if failed["status"] != "failed" {
		t.Fatalf("failed verification = %#v", failed)
	}
	retry := asObject(t, runPic(t, bin, root, home, "show", id))
	if asObject(t, retry["work_item"])["status"] != "open" || retry["ready"] != true {
		t.Fatalf("failed verification did not reopen executable = %#v", retry)
	}
	retryStatus := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if retryStatus["pipeline_stage"] != "autofix" {
		t.Fatalf("failed verification workflow = %#v", retryStatus)
	}
	verification := asObject(t, runPic(t, bin, root, home, "work-item", "verification-save", id, completion["id"].(string), "passed", "checks passed", "--actor-role", "contractor"))
	if verification["completion_report_id"] != completion["id"] || verification["verified_by_role"] != "contractor" {
		t.Fatalf("verification lineage = %#v", verification)
	}
	closed := asObject(t, runPic(t, bin, root, home, "show", id))
	if asObject(t, closed["work_item"])["status"] != "done" {
		t.Fatalf("verified Work Item = %#v", closed)
	}
	if len(closed["owner_decisions"].([]any)) != 0 {
		t.Fatalf("child unexpectedly has owner acceptance: %#v", closed["owner_decisions"])
	}
}

func TestPipelineCircuitResetRestoresCanonicalRunnerRetry(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Retry runner"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	activateTestWorkItemTIP(t, dbPath, id)
	suffix := strings.TrimPrefix(id, "wi-")
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-circuit-reset", id, "--reason", "runner repaired", "--change-type", "runner", "--evidence-json", `{"failed_run_id":"missing","changed_fingerprint":"runner-v1"}`, "--actor-role", "owner"); !strings.Contains(out, "terminal worker attempt") {
		t.Fatalf("reset without terminal attempt = %s", out)
	}
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--explicit-retry", "1"))
	runPic(t, bin, root, home, "work-item", "status", id, "in_progress")
	runPic(t, bin, root, home, "workflow", "pipeline-complete", claim["id"].(string), claim["lease_token"].(string), "failed", "--error", "runner failed before worker execution")
	if shown := asObject(t, runPic(t, bin, root, home, "show", id)); shown["ready"] != true || asObject(t, shown["work_item"])["status"] != "open" {
		t.Fatalf("terminal worker cleanup = %#v", shown)
	}
	runSQLite(t, dbPath, `UPDATE work_items SET status='in_progress' WHERE id='`+id+`'; INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,error,completed_at) VALUES('pr-failed','`+id+`','worker',2,'failed','lease-failed',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','subagent child failed',datetime('now'));`)
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-circuit-reset", id, "--reason", "runner repaired", "--change-type", "runner", "--evidence-json", `{"failed_run_id":"pr-failed","changed_fingerprint":"runner-v2"}`); !strings.Contains(out, "actor_role=owner") {
		t.Fatalf("circuit reset without owner authority = %s", out)
	}

	reset := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-circuit-reset", id, "--reason", "runner repaired", "--change-type", "runner", "--evidence-json", `{"failed_run_id":"pr-failed","changed_fingerprint":"runner-v2"}`, "--actor-role", "owner"))
	if reset["event_type"] != "pipeline_circuit_reset" || !strings.Contains(reset["payload_json"].(string), `"change_type":"runner"`) {
		t.Fatalf("reset evidence = %#v", reset)
	}
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-circuit-reset", id, "--reason", "same runner retry", "--change-type", "runner", "--evidence-json", `{"failed_run_id":"pr-failed","changed_fingerprint":"runner-v2"}`, "--actor-role", "owner"); !strings.Contains(out, "unchanged execution fingerprint") {
		t.Fatalf("unchanged reset accepted: %s", out)
	}
	shown := asObject(t, runPic(t, bin, root, home, "show", id))
	if shown["ready"] != true || asObject(t, shown["work_item"])["status"] != "open" {
		t.Fatalf("reset Work Item = %#v", shown)
	}
	if runs := runPic(t, bin, root, home, "workflow", "pipeline-runs", id).([]any); len(runs) != 2 || asObject(t, runs[0])["error"] != "subagent child failed" {
		t.Fatalf("failed run evidence = %#v", runs)
	}

	runSQLite(t, dbPath, `UPDATE work_items SET review_status='failed' WHERE id='`+id+`'; UPDATE pipeline_runs SET result_json='{"review_status":"failed","candidate_run_id":"pr-candidate","candidate_patch_hash":"patch-hash"}' WHERE id='pr-failed'; INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,integrated_patch_path,integrated_patch_hash,artifact_saved_at,completed_at,advanced_at) VALUES('pr-candidate','`+id+`','worker',3,'completed','lease-candidate',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','candidate.patch','patch-hash',datetime('now'),datetime('now'),datetime('now')); INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,candidate_run_id,candidate_patch_hash,review_fix_cycle,result_json,completed_at) VALUES('pr-invalid-fix','`+id+`','worker',4,'blocked','lease-invalid',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','pr-candidate','patch-hash',3,'{"failure_code":"runner_protocol_invalid"}',datetime('now')); UPDATE pipeline_runs SET stage='review',attempt=1,status='completed',candidate_run_id='pr-candidate',candidate_patch_hash='patch-hash' WHERE id='pr-failed'; UPDATE work_items SET status='open' WHERE id='`+id+`';`)
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--review-fix", "1", "--explicit-retry", "1"); !strings.Contains(out, "review-fix cycle limit reached") {
		t.Fatalf("protocol-invalid review fixes must consume retry budget: %s", out)
	}
	runSQLite(t, dbPath, `UPDATE pipeline_runs SET review_fix_cycle=1,error='review-fix produced the unchanged rejected candidate patch' WHERE id='pr-invalid-fix';`)
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--review-fix", "1", "--explicit-retry", "1"); !strings.Contains(out, "review-fix circuit breaker open") {
		t.Fatalf("unchanged rejected candidate must open circuit: %s", out)
	}
	runSQLite(t, dbPath, `UPDATE pipeline_runs SET result_json='{"failure_code":"worker_output_invalid"}',error='' WHERE id='pr-invalid-fix';`)
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--review-fix", "1", "--explicit-retry", "1"); !strings.Contains(out, "unchanged active instruction pack") || strings.Contains(out, "effective contract") {
		t.Fatalf("canonical circuit wording = %s", out)
	}
	runPic(t, bin, root, home, "workflow", "pipeline-circuit-reset", id, "--reason", "owner approved one corrected retry", "--change-type", "artifact", "--evidence-json", `{"failed_run_id":"pr-invalid-fix","changed_fingerprint":"artifact-v2"}`, "--actor-role", "owner")
	claim = asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--review-fix", "1", "--explicit-retry", "1"))
	if claim["review_fix_cycle"] != float64(1) {
		t.Fatalf("owner reset did not start a fresh review-fix epoch: %#v", claim)
	}
}

func TestReviewFixCapPersistsBlockedOwnerAction(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Review fix cap"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	activateTestWorkItemTIP(t, dbPath, id)
	suffix := strings.TrimPrefix(id, "wi-")
	// Seed a completed failed review bound to a completed mutation candidate with
	// three completed review-fix rounds (review_fix_cycle=3), so the round cap is hit.
	runSQLite(t, dbPath, `INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,integrated_patch_path,integrated_patch_hash,artifact_saved_at,completed_at,advanced_at) VALUES('pr-cand','`+id+`','worker',1,'completed','lease-cand',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','candidate.patch','patch-hash',datetime('now'),datetime('now'),datetime('now')); INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,candidate_run_id,candidate_patch_hash,review_fix_cycle,result_json,completed_at,advanced_at) VALUES('pr-rev','`+id+`','review',1,'completed','lease-rev',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','pr-cand','patch-hash',3,'{"review_status":"failed","candidate_run_id":"pr-cand","candidate_patch_hash":"patch-hash"}',datetime('now'),datetime('now')); UPDATE work_items SET review_status='failed' WHERE id='`+id+`';`)

	blocked := asObject(t, runPic(t, bin, root, home, "workflow", "review-fix-block", id, "--summary", "round cap reached: three fix rounds without a passed review"))
	if blocked["id"] != "pr-rev" {
		t.Fatalf("review-fix-block returned run = %#v", blocked)
	}
	// The failed review is now owner-approval-required durably, so the next fix
	// claim is rejected instead of relaunching, and it stays rejected across
	// reconciliation (repeated claim attempts).
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--review-fix", "1"); !strings.Contains(out, "requires owner approval") {
		t.Fatalf("first post-cap claim = %s", out)
	}
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--review-fix", "1"); !strings.Contains(out, "requires owner approval") {
		t.Fatalf("reconciled post-cap claim = %s", out)
	}
	runs := runPic(t, bin, root, home, "workflow", "pipeline-runs", id).([]any)
	reviewResult := ""
	for _, r := range runs {
		if asObject(t, r)["id"] == "pr-rev" {
			reviewResult = fmt.Sprint(asObject(t, r)["result_json"])
		}
	}
	if !strings.Contains(reviewResult, `"owner_approval_required":true`) || !strings.Contains(reviewResult, "round cap reached") {
		t.Fatalf("review result did not persist owner-action block: %s", reviewResult)
	}
	events := runPic(t, bin, root, home, "workflow", "events", id).([]any)
	foundRoundCap := false
	for _, ev := range events {
		if asObject(t, ev)["event_type"] == "review_fix_round_cap" {
			foundRoundCap = true
			break
		}
	}
	if !foundRoundCap {
		t.Fatalf("missing durable review_fix_round_cap owner-action event: %#v", events)
	}
}

func TestReviewDecisionFixClearsOwnerApprovalBlock(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Review decision fix"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	activateTestWorkItemTIP(t, dbPath, id)
	suffix := strings.TrimPrefix(id, "wi-")
	// Seed a completed worker candidate (attempt 2) and a completed failed review
	// that durably requires owner approval, mirroring a reviewer-flagged verdict.
	runSQLite(t, dbPath, `INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,integrated_patch_path,integrated_patch_hash,artifact_saved_at,completed_at,advanced_at) VALUES('pr-cand','`+id+`','worker',2,'completed','lease-cand',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','candidate.patch','patch-hash',datetime('now'),datetime('now'),datetime('now')); INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,candidate_run_id,candidate_patch_hash,result_json,completed_at) VALUES('pr-rev','`+id+`','review',1,'completed','lease-rev',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','pr-cand','patch-hash','{"review_status":"failed","candidate_run_id":"pr-cand","candidate_patch_hash":"patch-hash","owner_approval_required":true}',datetime('now')); UPDATE work_items SET review_status='failed' WHERE id='`+id+`';`)

	// The blocked review-fix claim is rejected while the flag stands.
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--review-fix", "1"); !strings.Contains(out, "requires owner approval") {
		t.Fatalf("pre-decision claim = %s", out)
	}
	// Non-owner actors are rejected.
	if out := runPicError(t, bin, root, home, "workflow", "review-decision", id, "pr-rev", "fix", "--notes", "n", "--actor-role", "contractor"); !strings.Contains(out, "actor_role=owner") {
		t.Fatalf("contractor decision = %s", out)
	}
	// Only the fix decision is modeled.
	if out := runPicError(t, bin, root, home, "workflow", "review-decision", id, "pr-rev", "deferred", "--notes", "n", "--actor-role", "owner"); !strings.Contains(out, "supports only decision 'fix'") {
		t.Fatalf("deferred decision = %s", out)
	}
	// A review without the durable flag is not a decision target.
	runSQLite(t, dbPath, `INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,candidate_run_id,candidate_patch_hash,result_json,completed_at) VALUES('pr-plain','`+id+`','review',2,'completed','lease-plain',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','pr-cand','patch-hash','{"review_status":"failed","candidate_run_id":"pr-cand","candidate_patch_hash":"patch-hash"}',datetime('now'));`)
	if out := runPicError(t, bin, root, home, "workflow", "review-decision", id, "pr-plain", "fix", "--notes", "n", "--actor-role", "owner"); !strings.Contains(out, "requires a completed failed review with owner_approval_required") {
		t.Fatalf("plain failed review decision = %s", out)
	}
	// The owner records the fix decision; the durable flag clears.
	decided := asObject(t, runPic(t, bin, root, home, "workflow", "review-decision", id, "pr-rev", "fix", "--notes", "owner directs a fix of the Important findings", "--actor-role", "owner"))
	if decided["id"] != "pr-rev" {
		t.Fatalf("review-decision returned run = %#v", decided)
	}
	if !strings.Contains(fmt.Sprint(decided["result_json"]), `"owner_approval_required":false`) {
		t.Fatalf("flag not cleared: %#v", decided)
	}
	events := runPic(t, bin, root, home, "workflow", "events", id).([]any)
	foundDecision := false
	for _, ev := range events {
		obj := asObject(t, ev)
		if obj["event_type"] == "owner_review_decision" && obj["actor_role"] == "owner" {
			foundDecision = true
		}
	}
	if !foundDecision {
		t.Fatalf("missing owner_review_decision audit event: %#v", events)
	}
	// The cleared flag plus the event's after_attempt baseline let the review-fix
	// claim proceed with a fresh cycle instead of staying blocked.
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--review-fix", "1"))
	if claim["review_fix_cycle"] != float64(1) {
		t.Fatalf("post-decision claim did not start a fresh review-fix epoch: %#v", claim)
	}
}

func TestPipelineCircuitResetClearsAutomaticWorkerRetryLimit(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Auto retry limit"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	activateTestWorkItemTIP(t, dbPath, id)

	// Simulate 3 failed worker attempts with the same instruction pack hash.
	for i := 1; i <= 3; i++ {
		claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--explicit-retry", "1"))
		runPic(t, bin, root, home, "work-item", "status", id, "in_progress")
		// no_progress_autofix is classified output evidence but sits outside both
		// the deterministic-breaker and environment-fingerprint gates, so these
		// attempts reach the unchanged-pack retry limiter this test exercises.
		runPic(t, bin, root, home, "workflow", "pipeline-complete", claim["id"].(string), claim["lease_token"].(string), "failed", "--error", fmt.Sprintf("attempt %d failed", i), "--result-json", `{"failure_code":"no_progress_autofix"}`)
		if shown := asObject(t, runPic(t, bin, root, home, "show", id)); asObject(t, shown["work_item"])["status"] != "open" {
			t.Fatalf("attempt %d cleanup failed: %#v", i, shown)
		}
	}

	// Without a reset, the automatic retry limit must reject a new claim.
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"); !strings.Contains(out, "automatic worker retry limit reached") {
		t.Fatalf("expected retry limit rejection, got: %s", out)
	}

	// Owner-authorized circuit reset clears the epoch.
	runPic(t, bin, root, home, "workflow", "pipeline-circuit-reset", id, "--reason", "runner repaired", "--change-type", "runner", "--evidence-json", `{"failed_run_id":"all","changed_fingerprint":"runner-v2"}`, "--actor-role", "owner")

	// After reset, a new worker claim must succeed without --explicit-retry.
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	if claim["status"] != "claimed" {
		t.Fatalf("post-reset worker claim rejected: %#v", claim)
	}
}

// Circuit reset must stay reachable when no active pack exists: a failed claim
// rolls back its generated TIP, so the limiter can deadlock an item whose packs
// are all stale/inactive (npvn.app wi-b83be214 incident). Reset falls back to
// the latest inactive pack.
func TestPipelineCircuitResetWorksWithoutActivePack(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Reset without active pack"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	activateTestWorkItemTIP(t, dbPath, id)

	// Three classified failures exhaust the unchanged-pack retry limit.
	for i := 1; i <= 3; i++ {
		claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--explicit-retry", "1"))
		runPic(t, bin, root, home, "work-item", "status", id, "in_progress")
		runPic(t, bin, root, home, "workflow", "pipeline-complete", claim["id"].(string), claim["lease_token"].(string), "failed", "--error", fmt.Sprintf("attempt %d failed", i), "--result-json", `{"failure_code":"no_progress_autofix"}`)
	}
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"); !strings.Contains(out, "automatic worker retry limit reached") {
		t.Fatalf("expected retry limit rejection, got: %s", out)
	}

	// Simulate the rolled-back claim: no active instruction pack remains.
	runSQLite(t, dbPath, `UPDATE work_item_instruction_packs SET status='inactive' WHERE work_item_id='`+id+`'`)

	// Owner reset must succeed despite zero active packs.
	event := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-circuit-reset", id, "--reason", "runner repaired after rollback", "--change-type", "runner", "--evidence-json", `{"failed_run_id":"all","changed_fingerprint":"runner-v2"}`, "--actor-role", "owner"))
	if event["event_type"] != "pipeline_circuit_reset" {
		t.Fatalf("circuit reset failed without active pack: %#v", event)
	}
}

func TestTransientWorkerDeathsDoNotExhaustUnchangedPackRetryLimit(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Transient deaths"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	activateTestWorkItemTIP(t, dbPath, id)

	// Three worker attempts that died without output (provider abort, timeout,
	// subagent child failure): no failure_code, no artifact. These are not
	// evidence about instruction content and must not trip the unchanged-pack
	// limiter, which would deadlock the item (circuit reset requires an active
	// pack; failed claims roll the generated pack back).
	for i := 1; i <= 3; i++ {
		claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
		runPic(t, bin, root, home, "work-item", "status", id, "in_progress")
		runPic(t, bin, root, home, "workflow", "pipeline-complete", claim["id"].(string), claim["lease_token"].(string), "failed", "--error", "subagent child failed")
	}

	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	if claim["status"] != "claimed" {
		t.Fatalf("transient deaths must not block a fresh worker claim: %#v", claim)
	}
}

func TestPipelineSchemaMigrationPreservesDependentObjects(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := project.InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE VIEW pipeline_run_ids AS SELECT id FROM pipeline_runs;
		CREATE TRIGGER pipeline_run_reference AFTER UPDATE OF status ON work_items BEGIN SELECT COUNT(*) FROM pipeline_runs; END;
		PRAGMA foreign_keys=OFF;
		PRAGMA legacy_alter_table=ON;
		ALTER TABLE pipeline_runs RENAME TO pipeline_runs_legacy_seed`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	legacySQL := strings.Replace(schema.PipelineRunsTableSQL, "REFERENCES work_items(id)", "REFERENCES tasks(id)", 1)
	legacySQL = strings.Replace(legacySQL, "'scan','worker','review','autofix'", "'scan','worker','review'", 1)
	if _, err = db.Exec(legacySQL); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO pipeline_runs SELECT * FROM pipeline_runs_legacy_seed;
		DROP TABLE pipeline_runs_legacy_seed;
		PRAGMA legacy_alter_table=OFF;
		DELETE FROM schema_migrations`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	// The degraded schema simulates a database from an older binary, which also
	// predates schema_migrations version records; clearing them makes the next
	// open re-run the migrations exactly as an upgrade would.
	if err := project.InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err = project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var staleObjects int
	if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE sql LIKE '%pipeline_runs__workflow_migration%'`).Scan(&staleObjects); err != nil {
		t.Fatal(err)
	}
	if staleObjects != 0 {
		t.Fatalf("temporary pipeline migration name remains in %d schema objects", staleObjects)
	}
	for _, name := range []string{"idx_pipeline_runs_task", "idx_pipeline_runs_active_stage", "pipeline_run_ids", "pipeline_run_reference"} {
		if ok, err := store.RowExists(db, `SELECT 1 FROM sqlite_master WHERE name=?`, name); err != nil || !ok {
			t.Fatalf("schema object %s missing after migration: %v", name, err)
		}
	}
}

func TestInitDBRepairsStalePipelineForeignKey(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := project.InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO work_items(id,type,title) VALUES('wi-migration','task','Migration');
		INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at) VALUES('pr-migration','wi-migration','worker',1,'completed','lease','2099-01-01');
		INSERT INTO work_item_completion_reports(id,work_item_id,pipeline_run_id,instruction_pack_id,instruction_pack_version,instruction_pack_hash,status) VALUES('wicr-migration','wi-migration','pr-migration','pack',1,'hash','done');
		PRAGMA foreign_keys=OFF;
		PRAGMA legacy_alter_table=OFF;
		ALTER TABLE pipeline_runs RENAME TO pipeline_runs__workflow_migration`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err = db.Exec(schema.PipelineRunsTableSQL); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO pipeline_runs SELECT * FROM pipeline_runs__workflow_migration;
		DROP TABLE pipeline_runs__workflow_migration;
		DELETE FROM schema_migrations`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	// Older-binary database simulation: clear version records so the next open
	// re-runs the migrations and repairs the stale foreign key.
	if err := project.InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err = project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var target string
	if err = db.QueryRow(`SELECT "table" FROM pragma_foreign_key_list('work_item_completion_reports') WHERE "from"='pipeline_run_id'`).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if target != "pipeline_runs" {
		t.Fatalf("pipeline_run_id foreign key target = %q, want pipeline_runs", target)
	}
	var reports, violations int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_completion_reports WHERE id='wicr-migration'`).Scan(&reports); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
		t.Fatal(err)
	}
	if reports != 1 || violations != 0 {
		t.Fatalf("repair preserved reports=%d, foreign key violations=%d", reports, violations)
	}
}

func TestWebAPISupportsDashboardContract(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	t.Setenv("HOME", home)
	project := asObject(t, runPic(t, bin, root, home, "project", "current"))
	projectID := project["id"].(string)

	res := webRequest(t, http.MethodPost, "/api/projects/"+projectID+"/work-items", map[string]any{"type": "epic", "title": "Web epic"})
	if res.Code != http.StatusOK {
		t.Fatalf("create epic: %d %s", res.Code, res.Body.String())
	}
	var epicBody map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &epicBody)
	epicID := epicBody["workItem"].(map[string]any)["id"].(string)
	res = webRequest(t, http.MethodPost, "/api/projects/"+projectID+"/work-items", map[string]any{"type": "task", "parent_id": epicID, "title": "Web task", "priority": "high"})
	if res.Code != http.StatusOK {
		t.Fatalf("create task: %d %s", res.Code, res.Body.String())
	}
	var taskBody map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &taskBody)
	taskID := taskBody["workItem"].(map[string]any)["id"].(string)
	res = webRequest(t, http.MethodPatch, "/api/projects/"+projectID+"/work-items/"+taskID+"/status", map[string]any{"status": "in_progress"})
	if res.Code != http.StatusOK {
		t.Fatalf("update status: %d %s", res.Code, res.Body.String())
	}
	res = webRequest(t, http.MethodGet, "/api/projects/"+projectID+"/work-items/"+epicID, nil)
	var detailBody map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &detailBody)
	if res.Code != http.StatusOK || len(detailBody["descendants"].([]any)) != 1 {
		t.Fatalf("work item detail: %d %s", res.Code, res.Body.String())
	}
	res = webRequest(t, http.MethodGet, "/api/projects/"+projectID+"/work-items/ready", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("ready work items: %d %s", res.Code, res.Body.String())
	}
	res = webRequest(t, http.MethodGet, "/api/workflow/review-queue", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("workflow queue: %d %s", res.Code, res.Body.String())
	}
	res = webRequest(t, http.MethodGet, "/api/projects/missing/summary", nil)
	if res.Code != http.StatusNotFound {
		t.Fatalf("missing project status = %d body=%s", res.Code, res.Body.String())
	}
}

func TestWebAPISkillRouting(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	t.Setenv("HOME", home)
	project := asObject(t, runPic(t, bin, root, home, "project", "current"))
	projectID := project["id"].(string)

	res := webRequest(t, http.MethodPost, "/api/projects/"+projectID+"/work-items", map[string]any{"type": "epic", "title": "Routing epic"})
	if res.Code != http.StatusOK {
		t.Fatalf("create epic: %d %s", res.Code, res.Body.String())
	}
	var epicBody map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &epicBody)
	epicID := epicBody["workItem"].(map[string]any)["id"].(string)
	res = webRequest(t, http.MethodPost, "/api/projects/"+projectID+"/work-items", map[string]any{"type": "task", "parent_id": epicID, "title": "Routing task"})
	if res.Code != http.StatusOK {
		t.Fatalf("create task: %d %s", res.Code, res.Body.String())
	}
	var taskBody map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &taskBody)
	taskID := taskBody["workItem"].(map[string]any)["id"].(string)

	payload := `{"stage":"worker","pack_id":"wip-1","selected_families":["languages/typescript"],"matched_families":[{"id":"languages/typescript","matched_by":[".ts"]},{"id":"frameworks/sveltekit","matched_by":["sveltekit"]}],"missing_families":["frameworks/sveltekit"],"evidence_sources":["pack_content","scan_artifact"]}`
	runPic(t, bin, root, home, "workflow", "event-add", taskID, "skill_family_routing", "--actor-role", "scheduler", "--summary", "Skill family routing (worker): matched 2, missing 1", "--payload-json", payload)
	// A malformed payload from a foreign writer must not break the aggregation
	// (json_valid guards) nor appear in recent events.
	runPic(t, bin, root, home, "workflow", "event-add", taskID, "skill_family_routing", "--actor-role", "scheduler", "--summary", "malformed payload", "--payload-json", "not json")

	res = webRequest(t, http.MethodGet, "/api/projects/"+projectID+"/skill-routing", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("skill-routing: %d %s", res.Code, res.Body.String())
	}
	var routing map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &routing)
	if routing["totalEvents"].(float64) != 2 {
		t.Fatalf("totalEvents = %v", routing["totalEvents"])
	}
	missing := routing["missingCounts"].([]any)
	if len(missing) != 1 || missing[0].(map[string]any)["missing"] != "frameworks/sveltekit" || missing[0].(map[string]any)["count"].(float64) != 1 {
		t.Fatalf("missingCounts = %v", routing["missingCounts"])
	}
	families := routing["familyCounts"].([]any)
	if len(families) != 2 {
		t.Fatalf("familyCounts = %v", routing["familyCounts"])
	}
	recent := routing["recentEvents"].([]any)
	if len(recent) != 1 {
		t.Fatalf("recentEvents = %v", routing["recentEvents"])
	}
	latest := recent[0].(map[string]any)
	if latest["stage"] != "worker" || latest["packId"] != "wip-1" || latest["workItemId"] != taskID {
		t.Fatalf("recent event = %v", latest)
	}
	if latest["missingFamilies"] != `["frameworks/sveltekit"]` {
		t.Fatalf("recent missingFamilies = %v", latest["missingFamilies"])
	}

	res = webRequest(t, http.MethodGet, "/api/projects/"+projectID+"/work-items/"+taskID, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("detail: %d %s", res.Code, res.Body.String())
	}
	var detail map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &detail)
	events := detail["routingEvents"].([]any)
	if len(events) != 1 {
		t.Fatalf("routingEvents = %v", detail["routingEvents"])
	}
}

func TestRemainingCommandGroups(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)

	task := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Go Port"))
	taskID := task["id"].(string)
	activity := asObject(t, runPic(t, bin, root, home, "activity", "update", "--session", "s1", "--task", taskID, "--status", "active"))
	if activity["ok"] != true {
		t.Fatalf("activity update = %#v", activity)
	}
	if rows := runPic(t, bin, root, home, "activity", "list").([]any); len(rows) != 1 {
		t.Fatalf("activity list = %#v", rows)
	}

	if rows := runPic(t, bin, root, home, "search", "Go").([]any); len(rows) == 0 {
		t.Fatalf("search returned no rows")
	}
}

func TestWorkItemEscalationLifecycle(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Escalation leaf"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	activateTestWorkItemTIP(t, dbPath, id)
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	runID := claim["id"].(string)
	report := `{"level":"L2","checked_sources":["active TIP","Contract obligations"],"summary":"two valid implementations diverge by tradeoff","questions":["which session store"],"options":["sqlite","memory"],"recommendation":"sqlite"}`

	if out := runPicError(t, bin, root, home, "workflow", "escalation-save", id, "--pipeline-run-id", runID, "--report-json", `{"level":"L2"}`); !strings.Contains(out, "checked_sources") {
		t.Fatalf("escalation-save accepted a report without checked_sources: %s", out)
	}
	escalation := asObject(t, runPic(t, bin, root, home, "workflow", "escalation-save", id, "--pipeline-run-id", runID, "--report-json", report))
	escalationID := escalation["id"].(string)
	if escalation["status"] != "open" || escalation["level"] != "L2" {
		t.Fatalf("escalation save = %#v", escalation)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var runStatus, packID, itemStatus string
	if err = db.QueryRow(`SELECT status,instruction_pack_id FROM pipeline_runs WHERE id=?`, runID).Scan(&runStatus, &packID); err != nil || runStatus != "blocked" || packID == "" {
		t.Fatalf("escalated run status=%q pack=%q err=%v", runStatus, packID, err)
	}
	if err = db.QueryRow(`SELECT status FROM work_items WHERE id=?`, id).Scan(&itemStatus); err != nil || itemStatus != "open" {
		t.Fatalf("escalated work item status=%q err=%v", itemStatus, err)
	}
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"); !strings.Contains(out, "open escalation") || !strings.Contains(out, escalationID) {
		t.Fatalf("claim not gated by open escalation with ID: %s", out)
	}
	if out := runPicError(t, bin, root, home, "workflow", "escalation-resolve", id, escalationID, `{"decision":"use sqlite"}`, "--actor-role", "owner"); !strings.Contains(out, "contractor") {
		t.Fatalf("non-contractor resolution accepted: %s", out)
	}
	resolved := asObject(t, runPic(t, bin, root, home, "workflow", "escalation-resolve", id, escalationID, `{"decision":"use sqlite"}`, "--actor-role", "contractor"))
	if resolved["status"] != "resolved" {
		t.Fatalf("resolution = %#v", resolved)
	}
	var events int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_events WHERE work_item_id=? AND event_type='escalation_resolved'`, id).Scan(&events); err != nil || events != 1 {
		t.Fatalf("escalation_resolved events=%d err=%v", events, err)
	}
	afterClaim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	if afterClaim["stage"] != "worker" {
		t.Fatalf("post-resolution claim = %#v", afterClaim)
	}
}

func TestRriTScenarioArtifact(t *testing.T) {
	t.Setenv("PI_TASK_AGENT_NAME", "")
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Scenario Epic"))
	id := epic["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")

	scenariosA := `{"methodology":"rri-t","personas":["End User"],"scenarios":[{"id":"SC-1","persona":"End User","dimension":"D1","stress_axis":"TIME","requirement_id":"REQ-001","procedure":"Run the helper flow","evidence":"go test ./...","result":"PASS"}]}`
	scenariosB := strings.Replace(scenariosA, "Run the helper flow", "Run the trimmed flow", 1)

	// Unknown artifact stages stay fail-closed: rejected before any row is written.
	if out := runPicError(t, bin, root, home, "work-item", "artifact-save", id, "bogus_stage", scenariosA); !strings.Contains(out, "usage") {
		t.Fatalf("unknown stage error = %s", out)
	}

	// Lean artifact-save rejects every legacy planning stage: only
	// rri_t_scenarios remains saveable.
	if out := runPicError(t, bin, root, home, "work-item", "artifact-save", id, "scan", "<scan_report/>"); !strings.Contains(out, "usage") {
		t.Fatalf("legacy stage still accepted = %s", out)
	}

	// Fresh SQLite schema and the artifact-save stage registry accept rri_t_scenarios.
	saved1 := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri_t_scenarios", scenariosA))
	if saved1["stage"] != "rri_t_scenarios" || saved1["revision"] != float64(1) || saved1["content_hash"] != store.HashJSON(scenariosA) {
		t.Fatalf("scenario artifact = %#v", saved1)
	}
	// The scenario save is supplementary: a childless epic reports the lean
	// aggregate stage, never a planning stage.
	if status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id)); status["next_stage"] != "implement" {
		t.Fatalf("scenario save gated workflow: %#v", status)
	}

	// The saved scenario list is owner-visible through the existing show path.
	shown := asObject(t, runPic(t, bin, root, home, "show", id))
	scenarioRows := 0
	for _, raw := range shown["artifacts"].([]any) {
		row := raw.(map[string]any)
		if row["stage"] == "rri_t_scenarios" {
			scenarioRows++
			if row["content"] != scenariosA || row["content_hash"] != store.HashJSON(scenariosA) {
				t.Fatalf("show scenario row = %#v", row)
			}
		}
	}
	if scenarioRows != 1 {
		t.Fatalf("show artifacts = %#v", shown["artifacts"])
	}

	// A later save creates a new immutable revision and cannot mutate the prior one.
	saved2 := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri_t_scenarios", scenariosB))
	if saved2["revision"] != float64(2) {
		t.Fatalf("second scenario artifact = %#v", saved2)
	}
	if out, err := exec.Command("sqlite3", dbPath, `UPDATE work_item_artifacts SET content='mutated' WHERE id='`+saved1["id"].(string)+`';`).CombinedOutput(); err == nil || !strings.Contains(string(out), "immutable") {
		t.Fatalf("scenario artifact mutation err=%v out=%s", err, out)
	}

	// A third save creates another immutable revision; prior revisions are
	// retained verbatim (no checkpoint invalidation exists in the lean model).
	_ = asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri_t_scenarios", scenariosB))
	db, err := project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Approved artifact history is retained, not deleted or rewritten.
	var artifacts, scenarioArtifacts int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=?`, id).Scan(&artifacts)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=? AND stage='rri_t_scenarios'`, id).Scan(&scenarioArtifacts)
	if artifacts != 3 || scenarioArtifacts != 3 {
		t.Fatalf("retained artifacts=%d scenario_artifacts=%d", artifacts, scenarioArtifacts)
	}
	var originalContent string
	if err = db.QueryRow(`SELECT content FROM work_item_artifacts WHERE id=?`, saved1["id"].(string)).Scan(&originalContent); err != nil || originalContent != scenariosA {
		t.Fatalf("revision 1 content = %q err=%v", originalContent, err)
	}
}

// TestRriTScenarioArtifactLegacySchemaMigration is the regression guard for the
// pre-change database path: initDB runs on every command but never rebuilds
// existing work_item_artifacts/workflow_checkpoints, so a project created
// before the additive stage still carries the old CHECK constraints. The test
// recreates those tables exactly as the old schema persisted them (old CHECK
// list, old lookup indexes, old immutable triggers) with owned planning rows,
// then verifies that the first rri_t_scenarios save migrates both tables,
// preserves every pre-existing row and immutable-history trigger, and keeps the
// existing planning checkpoints gate-compatible.
func TestRriTScenarioArtifactLegacySchemaMigration(t *testing.T) {
	t.Setenv("PI_TASK_AGENT_NAME", "")
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Migration Epic"))
	id := epic["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")

	// Simulate the pre-change schema: drop the fresh-schema tables and rebuild
	// them with the original CHECK constraints, triggers, and indexes, seeded
	// with the owned scan/rri planning history of the created epic.
	runSQLite(t, dbPath, fmt.Sprintf(`DROP TRIGGER IF EXISTS trg_work_item_artifact_immutable;
DROP TRIGGER IF EXISTS trg_work_item_artifact_delete_immutable;
DROP TABLE workflow_checkpoints;
DROP TABLE work_item_artifacts;
CREATE TABLE work_item_artifacts (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, stage TEXT NOT NULL CHECK(stage IN ('scan','rri','vision','blueprint','contracts','task_graph')), revision INTEGER NOT NULL CHECK(revision>0), content TEXT NOT NULL, content_hash TEXT NOT NULL, created_at TEXT DEFAULT (datetime('now')), UNIQUE(work_item_id,stage,revision));
CREATE TABLE workflow_checkpoints (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, stage TEXT NOT NULL CHECK(stage IN ('scan','rri','vision','blueprint','contracts','task_graph')), artifact_id TEXT NOT NULL, artifact_revision INTEGER NOT NULL CHECK(artifact_revision>0), content_hash TEXT NOT NULL, decision_type TEXT NOT NULL, created_at TEXT DEFAULT (datetime('now')), UNIQUE(work_item_id,stage,artifact_revision));
CREATE INDEX idx_work_item_artifacts_item_stage ON work_item_artifacts(work_item_id,stage,revision DESC);
CREATE INDEX idx_workflow_checkpoints_item_stage ON workflow_checkpoints(work_item_id,stage,artifact_revision DESC);
CREATE TRIGGER trg_work_item_artifact_immutable BEFORE UPDATE ON work_item_artifacts BEGIN SELECT RAISE(ABORT,'work item artifacts are immutable'); END;
CREATE TRIGGER trg_work_item_artifact_delete_immutable BEFORE DELETE ON work_item_artifacts WHEN EXISTS(SELECT 1 FROM workflow_checkpoints WHERE artifact_id=OLD.id) BEGIN SELECT RAISE(ABORT,'approved work item artifacts are immutable'); END;
INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES
  ('wia-legacy-scan','%s','scan',1,'<scan/>','h-legacy-scan'),
  ('wia-legacy-rri','%s','rri',1,'<rri/>','h-legacy-rri');
INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES
  ('wic-legacy-scan','%s','scan','wia-legacy-scan',1,'h-legacy-scan','accepted'),
  ('wic-legacy-rri','%s','rri','wia-legacy-rri',1,'h-legacy-rri','approved');
DELETE FROM schema_migrations;`, id, id, id, id))

	// The rebuilt tables simulate an older-binary database, so the version
	// records are cleared and the next pic command re-runs the widening
	// migration; the rri_t_scenarios save must now succeed.
	scenariosA := `{"methodology":"rri-t","personas":["End User"],"scenarios":[{"id":"SC-1","persona":"End User","dimension":"D1","stress_axis":"TIME","requirement_id":"REQ-001","procedure":"Run the helper flow","evidence":"go test ./...","result":"PASS"}]}`
	saved := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri_t_scenarios", scenariosA))
	if saved["stage"] != "rri_t_scenarios" || saved["revision"] != float64(1) {
		t.Fatalf("scenario artifact after legacy migration = %#v", saved)
	}

	// Both tables now carry the widened CHECK, every legacy row survived, and
	// the lookup indexes and immutable triggers were recreated on the fresh
	// tables rather than left behind on the renamed legacy ones.
	var artifactsSQL, checkpointsSQL string
	db, err := project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='work_item_artifacts'`).Scan(&artifactsSQL); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='workflow_checkpoints'`).Scan(&checkpointsSQL); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(artifactsSQL, "rri_t_scenarios") || !strings.Contains(checkpointsSQL, "rri_t_scenarios") {
		t.Fatalf("CHECK not widened: artifacts=%q checkpoints=%q", artifactsSQL, checkpointsSQL)
	}
	for _, expected := range []string{"wia-legacy-scan", "wia-legacy-rri", "wic-legacy-scan", "wic-legacy-rri", saved["id"].(string)} {
		table := "workflow_checkpoints"
		if strings.HasPrefix(expected, "wia-") {
			table = "work_item_artifacts"
		}
		var exists int
		if err = db.QueryRow(`SELECT COUNT(*) FROM "`+table+`" WHERE id=?`, expected).Scan(&exists); err != nil || exists != 1 {
			t.Fatalf("row %s preserved: count=%d err=%v", expected, exists, err)
		}
	}
	var triggerCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name IN ('trg_work_item_artifact_immutable','trg_work_item_artifact_delete_immutable') AND tbl_name='work_item_artifacts'`).Scan(&triggerCount)
	if triggerCount != 2 {
		t.Fatalf("immutable triggers on rebuilt table = %d", triggerCount)
	}
	for _, index := range []string{"idx_work_item_artifacts_item_stage", "idx_workflow_checkpoints_item_stage"} {
		var indexCount int
		_ = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=? AND tbl_name=?`, index, strings.TrimSuffix(strings.TrimPrefix(index, "idx_"), "_item_stage")).Scan(&indexCount)
		if indexCount != 1 {
			t.Fatalf("index %s not recreated", index)
		}
	}

	// The immutable-history invariants still hold after the migration: UPDATE
	// and approved DELETE are rejected, the legacy checkpoint lineage is intact.
	if out, err := exec.Command("sqlite3", dbPath, `UPDATE work_item_artifacts SET content='mutated' WHERE id='`+saved["id"].(string)+`';`).CombinedOutput(); err == nil || !strings.Contains(string(out), "immutable") {
		t.Fatalf("migrated scenario artifact mutation err=%v out=%s", err, out)
	}
	if out, err := exec.Command("sqlite3", dbPath, `DELETE FROM work_item_artifacts WHERE id='wia-legacy-scan';`).CombinedOutput(); err == nil || !strings.Contains(string(out), "immutable") {
		t.Fatalf("migrated approved artifact delete err=%v out=%s", err, out)
	}

	// Existing approved scan/rri checkpoints survive the migration as inert
	// history; the lean workflow reports the aggregate stage regardless.
	status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["next_stage"] != "implement" {
		t.Fatalf("legacy migration workflow status = %#v", status)
	}
	var artifacts, checkpointsCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=?`, id).Scan(&artifacts)
	_ = db.QueryRow(`SELECT COUNT(*) FROM workflow_checkpoints WHERE work_item_id=?`, id).Scan(&checkpointsCount)
	if artifacts != 3 || checkpointsCount != 2 {
		t.Fatalf("legacy retained artifacts=%d checkpoints=%d", artifacts, checkpointsCount)
	}
}

// TestRriTScenarioIdentityContract is the end-to-end guard for the RRI-T
// id-based scenario identity contract: graded scenarios deduplicate on
// (dimension|stress_axis|requirement_id|id) exactly like the TypeScript grading
// compiler, so two persisted scenarios that share persona, dimension, stress
// axis, and requirement but differ in id are distinct outcomes — while a
// duplicate deferred disposition (the same persisted scenario deferred twice via
// not_applicable) is rejected and the PASS/ACCEPTABLE/PAINFUL/FAIL result
// mapping stays unchanged.
func TestRriTScenarioIdentityContract(t *testing.T) {
	t.Setenv("PI_TASK_AGENT_NAME", "")
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Identity Epic"))
	id := epic["id"].(string)
	child := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Done child", "--parent", id))
	runPic(t, bin, root, home, "work-item", "status", child["id"].(string), "done")
	// RRI-T scenarios are requirement-bound: REQ-001 is an approved aggregate requirement.
	runSQLite(t, filepath.Join(root, ".pi", "tasks.db"), `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria,priority,status) VALUES('req-identity','`+id+`','REQ-001','Identity requirement','Given sc-1 and sc-2 When graded Then both count','tier1','pending')`)

	// Persist two scenarios sharing persona, dimension, stress axis, and
	// requirement but differing in id; the artifact is owner-visible and retained.
	scenarios := `{"methodology":"rri-t","personas":["QA / Tester"],"scenarios":[
		{"id":"SC-1","persona":"QA / Tester","dimension":"D3","stress_axis":"ERROR","requirement_id":"REQ-001","procedure":"Submit the empty form","remediation_hint":"assert inline error"},
		{"id":"SC-2","persona":"QA / Tester","dimension":"D3","stress_axis":"ERROR","requirement_id":"REQ-001","procedure":"Submit the malformed payload","remediation_hint":"assert rejection"}]}`
	saved := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri_t_scenarios", scenarios))
	if saved["stage"] != "rri_t_scenarios" {
		t.Fatalf("scenario artifact = %#v", saved)
	}

	// Both distinct id-based outcomes are accepted by aggregate verification.
	graded := `{"scenarios":[
		{"id":"SC-1","persona":"QA / Tester","dimension":"D3","stress_axis":"ERROR","requirement_id":"REQ-001","procedure":"Submit the empty form","evidence":"go test ./... passed","result":"PASS"},
		{"id":"SC-2","persona":"QA / Tester","dimension":"D3","stress_axis":"ERROR","requirement_id":"REQ-001","procedure":"Submit the malformed payload","evidence":"go test ./... passed","result":"PASS"}]}`
	report := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", id, "passed", "identity outcomes verified", "--actor-role", "contractor", "--rri-t-json", graded))
	if report["status"] != "passed" {
		t.Fatalf("distinct id-based outcomes rejected: %#v", report)
	}

	// A repeated graded identity is still rejected as a duplicate.
	duplicate := `{"scenarios":[
		{"id":"SC-1","persona":"QA / Tester","dimension":"D3","stress_axis":"ERROR","requirement_id":"REQ-001","procedure":"Submit the empty form","evidence":"ran","result":"PASS"},
		{"id":"SC-1","persona":"QA / Tester","dimension":"D3","stress_axis":"ERROR","requirement_id":"REQ-001","procedure":"Submit the empty form","evidence":"ran","result":"PASS"}]}`
	if out := runPicError(t, bin, root, home, "work-item", "aggregate-verify", id, "passed", "duplicate", "--actor-role", "contractor", "--rri-t-json", duplicate); !strings.Contains(out, "duplicate RRI-T scenario") {
		t.Fatalf("duplicate graded identity err = %s", out)
	}

	// A duplicate deferred disposition (the same persisted scenario deferred twice
	// via not_applicable) is rejected, so one scenario can be deferred at most once.
	deferred := `{"scenarios":[
		{"id":"SC-1","persona":"QA / Tester","dimension":"D3","stress_axis":"ERROR","requirement_id":"REQ-001","procedure":"Submit the empty form","evidence":"go test ./... passed","result":"PASS"},
		{"id":"SC-2","persona":"QA / Tester","dimension":"D3","stress_axis":"ERROR","requirement_id":"REQ-001","procedure":"Submit the malformed payload","evidence":"go test ./... passed","result":"PASS"}],"not_applicable":[
		{"id":"SC-1","persona":"QA / Tester","dimension":"D3","stress_axis":"ERROR","requirement_id":"REQ-001","reason":"cannot run against the integrated repo"},
		{"id":"SC-1","persona":"QA / Tester","dimension":"D3","stress_axis":"ERROR","requirement_id":"REQ-001","reason":"cannot run against the integrated repo"}]}`
	if out := runPicError(t, bin, root, home, "work-item", "aggregate-verify", id, "passed", "deferred duplicate", "--actor-role", "contractor", "--rri-t-json", deferred); !strings.Contains(out, "duplicate RRI-T scenario") {
		t.Fatalf("duplicate deferred disposition err = %s", out)
	}

	// The result mapping is unchanged: a PAINFUL result still blocks aggregate
	// passage until remediation or explicit owner deferral, and a FAIL result is
	// still rejected outright.
	painful := `{"scenarios":[{"id":"SC-1","persona":"QA / Tester","dimension":"D3","stress_axis":"ERROR","requirement_id":"REQ-001","procedure":"Submit the empty form","evidence":"observed friction","result":"PAINFUL"}]}`
	if out := runPicError(t, bin, root, home, "work-item", "aggregate-verify", id, "passed", "painful", "--actor-role", "contractor", "--rri-t-json", painful); !strings.Contains(out, "remediation or owner deferral") {
		t.Fatalf("PAINFUL passage err = %s", out)
	}
	failed := `{"scenarios":[{"id":"SC-1","persona":"QA / Tester","dimension":"D3","stress_axis":"ERROR","requirement_id":"REQ-001","procedure":"Submit the empty form","evidence":"broken","result":"FAIL"}]}`
	if out := runPicError(t, bin, root, home, "work-item", "aggregate-verify", id, "passed", "failed", "--actor-role", "contractor", "--rri-t-json", failed); !strings.Contains(out, "remediation or owner deferral") {
		t.Fatalf("FAIL passage err = %s", out)
	}
}

// TestRriTLeanAggregateSpecBoundScenario is the regression guard for the lean
// import model: imported aggregates carry no requirements rows (their
// requirement truth lives in the companion .feature/.tasks.md spec files, re-read
// by the contractor at review time), so aggregate verification must accept
// scenario requirement_ids bound to spec keys and must never consult an empty
// requirements set. The DB gate still applies to planning-era aggregates that
// have requirement rows.
func TestRriTLeanAggregateSpecBoundScenario(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epica := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Lean Epic"))
	id := epica["id"].(string)
	child := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Done lean child", "--parent", id))
	runPic(t, bin, root, home, "work-item", "status", child["id"].(string), "done")

	// No requirements rows exist for this aggregate; the scenario binds to the
	// spec-file requirement key from the Nyquist mapping table instead.
	scenarios := `{"methodology":"rri-t","personas":["End User"],"scenarios":[
		{"id":"SC-1","persona":"End User","dimension":"D1","stress_axis":"TIME","requirement_id":"R01","procedure":"Save an artifact and read the projected file","remediation_hint":"assert bytes"}]}`
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri_t_scenarios", scenarios)

	graded := `{"scenarios":[
		{"id":"SC-1","persona":"End User","dimension":"D1","stress_axis":"TIME","requirement_id":"R01","procedure":"Save an artifact and read the projected file","evidence":"TestArtifactSaveProjectsAllPlanningStages passed","result":"PASS"}]}`
	report := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", id, "passed", "spec-bound lean outcome verified", "--actor-role", "contractor", "--rri-t-json", graded))
	if report["status"] != "passed" {
		t.Fatalf("lean spec-bound scenario rejected: %#v", report)
	}

	// An empty requirement_id is still invalid even on a lean aggregate: the
	// scenario must name the spec requirement it grades.
	emptyReq := `{"scenarios":[
		{"id":"SC-2","persona":"End User","dimension":"D2","stress_axis":"DATA","requirement_id":"","procedure":"x","evidence":"ran","result":"PASS"}]}`
	if out := runPicError(t, bin, root, home, "work-item", "aggregate-verify", id, "passed", "empty requirement", "--actor-role", "contractor", "--rri-t-json", emptyReq); !strings.Contains(out, "requires a requirement_id") {
		t.Fatalf("empty requirement_id err = %s", out)
	}
}

func TestInstructionPackRendersContractInterfaces(t *testing.T) {
	t.Parallel()
	node := tip.TaskPlanDocumentNode{
		Key: "T01", Type: "task", Name: "Persist", RequirementKeys: []string{"REQ-001"},
		Provides: []string{"OBL-001"}, Consumes: []string{"OBL-002"}, EvidenceFor: []string{"OBL-001"}, ObligationKeys: []string{"OBL-001"},
		Files:         []string{"a.go"},
		Constraints:   map[string]any{"scope_roots": []any{"."}},
		Verification:  []any{map[string]any{"command": "go test ./..."}},
		BusinessRules: []any{"rule"}, ValidationRules: []any{"v"}, ErrorHandling: []any{"e"}, StateTransitions: []any{"s"}, ContractObligations: []any{"o"},
	}
	packBytes, _, err := tip.MaterializedInstructionPack(node, 3, map[string]tip.RequirementSnapshot{"REQ-001": {RequirementKey: "REQ-001", Title: "R", AcceptanceCriteria: "Given\nWhen\nThen"}})
	if err != nil {
		t.Fatal(err)
	}
	pack := map[string]any{"id": "wip-x", "version": 1, "status": "active", "content_hash": "sha256:x", "content_json": string(packBytes), "work_item_id": "wi-1", "work_item_title": "T", "work_item_type": "task", "priority": "medium"}
	if err := tip.ExpandCanonicalInstructionPack(pack); err != nil {
		t.Fatal(err)
	}
	rendered := tip.RenderInstructionPack(pack)
	if !strings.Contains(rendered, "## CONTRACT INTERFACES") || !strings.Contains(rendered, "OBL-001") || !strings.Contains(rendered, "consumes: OBL-002") {
		t.Fatalf("contract interfaces missing from TIP render: %s", rendered)
	}

	legacy := map[string]any{"id": "wip-y", "version": 1, "status": "active", "content_hash": "sha256:y", "content_json": `{"content":{"goal":"g","files":["f.go"],"business_rules":["b"],"validation_rules":["v"],"error_handling":["e"],"state_transitions":["s"],"contract_obligations":["o"],"constraints":{"k":"v"},"verification":[{"command":"c"}],"schemaVersion":2},"requirements":[]}`, "work_item_id": "wi-1", "work_item_title": "T", "work_item_type": "task", "priority": "medium"}
	if err := tip.ExpandCanonicalInstructionPack(legacy); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tip.RenderInstructionPack(legacy), "## CONTRACT INTERFACES") {
		t.Fatalf("legacy pack must not render an empty contract interfaces section")
	}
}

func TestSchemaMigrationsVersioned(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := project.InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	versions := func() []string {
		t.Helper()
		rows, err := db.Query(`SELECT name FROM schema_migrations ORDER BY version`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
		return names
	}
	fresh := versions()
	if len(fresh) == 0 {
		t.Fatal("fresh database recorded no schema migrations")
	}
	for _, name := range fresh {
		if strings.Contains(name, "legacy") {
			t.Fatalf("fresh database recorded legacy migration %s", name)
		}
	}
	var canonicalBaseline bool
	for _, name := range fresh {
		if name == "canonical_baseline" {
			canonicalBaseline = true
		}
	}
	if !canonicalBaseline {
		t.Fatalf("fresh database missing canonical_baseline migration: %v", fresh)
	}
	if err := project.InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	if again := versions(); strings.Join(again, ",") != strings.Join(fresh, ",") {
		t.Fatalf("second open changed recorded migrations: %v -> %v", fresh, again)
	}
	db.Close()

	legacyPath := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := project.OpenSQLite(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE epics (id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT DEFAULT 'open', created_at TEXT DEFAULT (datetime('now')))`,
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, epic_id TEXT, title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT DEFAULT 'open', priority TEXT DEFAULT 'medium', created_at TEXT DEFAULT (datetime('now')))`,
		`INSERT INTO epics(id,title) VALUES('e-old','Legacy Epic')`,
		`INSERT INTO tasks(id,epic_id,title) VALUES('t-old','e-old','Legacy Task')`,
	} {
		if _, err := legacy.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	legacy.Close()
	if err := project.InitDB(legacyPath); err != nil {
		t.Fatal(err)
	}
	db, err = project.OpenSQLite(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var epicRows, taskRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE id='e-old' AND type='epic'`).Scan(&epicRows); err != nil || epicRows != 1 {
		t.Fatalf("legacy epic migrated rows=%d err=%v", epicRows, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE id='t-old' AND type='task'`).Scan(&taskRows); err != nil || taskRows != 1 {
		t.Fatalf("legacy task migrated rows=%d err=%v", taskRows, err)
	}
	var legacyRecorded bool
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name='legacy_schema_bootstrap'`).Scan(&legacyRecorded); err != nil || !legacyRecorded {
		t.Fatalf("legacy migration not recorded err=%v", err)
	}
}

func TestPartialLegacyStateMigrates(t *testing.T) {
	t.Parallel()
	tasksOnly := filepath.Join(t.TempDir(), "tasks.db")
	db, err := project.OpenSQLite(tasksOnly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE tasks(id TEXT PRIMARY KEY, epic_id TEXT, title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT DEFAULT 'open', priority TEXT DEFAULT 'medium', created_at TEXT DEFAULT (datetime('now')));
		INSERT INTO tasks(id,epic_id,title) VALUES('t-part','e-missing','Orphan Task')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := project.InitDB(tasksOnly); err != nil {
		t.Fatalf("tasks-only database failed to migrate: %v", err)
	}
	db, err = project.OpenSQLite(tasksOnly)
	if err != nil {
		t.Fatal(err)
	}
	var taskRows, violations int
	if err := db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE id='t-part' AND type='task'`).Scan(&taskRows); err != nil || taskRows != 1 {
		t.Fatalf("tasks-only migration rows=%d err=%v", taskRows, err)
	}
	var parentNull bool
	if err := db.QueryRow(`SELECT parent_id IS NULL FROM work_items WHERE id='t-part'`).Scan(&parentNull); err != nil || !parentNull {
		t.Fatalf("orphan task parent must be null: parentNull=%v err=%v", parentNull, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("tasks-only migration violations=%d err=%v", violations, err)
	}
	db.Close()

	epicsOnly := filepath.Join(t.TempDir(), "epics.db")
	db, err = project.OpenSQLite(epicsOnly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE epics(id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT DEFAULT 'open', created_at TEXT DEFAULT (datetime('now')));
		INSERT INTO epics(id,title) VALUES('e-part','Lone Epic')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := project.InitDB(epicsOnly); err != nil {
		t.Fatalf("epics-only database failed to migrate: %v", err)
	}
	db, err = project.OpenSQLite(epicsOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var epicRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE id='e-part' AND type='epic'`).Scan(&epicRows); err != nil || epicRows != 1 {
		t.Fatalf("epics-only migration rows=%d err=%v", epicRows, err)
	}
}

func TestSchemaMigrationFailureInjectionRollsBack(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE epics(id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT DEFAULT 'open', created_at TEXT DEFAULT (datetime('now')));
		CREATE TABLE tasks(id TEXT PRIMARY KEY, epic_id TEXT, title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT DEFAULT 'open', priority TEXT DEFAULT 'medium', created_at TEXT DEFAULT (datetime('now')));
		INSERT INTO epics(id,title) VALUES('e-inject','Inject Epic');
		INSERT INTO tasks(id,epic_id,title) VALUES('t-inject','e-inject','Inject Task')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// A transactional step that performs REAL migration operations (the
	// pre-reconcile rebuild and legacy import) and then fails: the version must
	// stay unrecorded and every operation must roll back, including DDL.
	db, err = project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// The runner creates the version table before applying any step.
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT DEFAULT (datetime('now')))`); err != nil {
		t.Fatal(err)
	}
	poison := schema.Migration{Version: 99, Name: "poison_reconcile", Apply: func(db schema.DB) error {
		if err := schema.ReconcileLegacySchema(db); err != nil {
			return err
		}
		return errors.New("injected failure after reconcile operations")
	}}
	if err := schema.ApplyMigration(context.Background(), db, poison); err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("poison step error = %v", err)
	}
	var recorded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=99`).Scan(&recorded); err != nil || recorded != 0 {
		t.Fatalf("failed step recorded version: count=%d err=%v", recorded, err)
	}
	var workItemsTable int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='work_items'`).Scan(&workItemsTable); err != nil || workItemsTable != 0 {
		t.Fatalf("reconcile DDL rolled back: work_items tables=%d err=%v", workItemsTable, err)
	}
	var epicRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM epics WHERE id='e-inject'`).Scan(&epicRows); err != nil || epicRows != 1 {
		t.Fatalf("legacy epic row disturbed: rows=%d err=%v", epicRows, err)
	}
	db.Close()

	// A DDL-producing step that fails midway: the created table must roll back.
	db, err = project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ddlPoison := schema.Migration{Version: 98, Name: "poison_ddl", Apply: func(db schema.DB) error {
		if _, err := db.Exec(`CREATE TABLE zz_poison (id TEXT)`); err != nil {
			return err
		}
		return errors.New("injected DDL failure")
	}}
	if err := schema.ApplyMigration(context.Background(), db, ddlPoison); err == nil || !strings.Contains(err.Error(), "injected DDL failure") {
		t.Fatalf("ddl poison error = %v", err)
	}
	var poisonTable int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='zz_poison'`).Scan(&poisonTable); err != nil || poisonTable != 0 {
		t.Fatalf("DDL did not roll back: zz_poison tables=%d", poisonTable)
	}
	db.Close()

	// Retry after the failures: the real migration completes and migrates rows.
	if err := project.InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err = project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var migratedRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE id IN ('e-inject','t-inject')`).Scan(&migratedRows); err != nil || migratedRows != 2 {
		t.Fatalf("retry after failures migrated rows=%d err=%v", migratedRows, err)
	}
	var versions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&versions); err != nil || versions == 0 {
		t.Fatalf("retry recorded no versions: count=%d err=%v", versions, err)
	}
}

// Connection affinity constraint: foreign_keys and legacy_alter_table are
// connection-scoped pragmas, and database/sql gives no affinity between
// db.Exec and db.Begin. openSQLite's DSN enables foreign_keys on every new
// connection and the test pools no idle connections, so a pragma sent through
// the pool can never leak onto the transaction's connection — only a runner
// that pins one *sql.Conn observes foreign_keys=OFF and legacy_alter_table=ON
// inside the step's transaction.
func TestSchemaMigrationPragmasRunOnThePinnedConnection(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// The runner creates the version table before applying any step.
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT DEFAULT (datetime('now')))`); err != nil {
		t.Fatal(err)
	}
	db.SetMaxIdleConns(0)
	var foreignKeys, legacyAlterTable int
	probe := schema.Migration{Version: 97, Name: "pragma_affinity_probe", Apply: func(db schema.DB) error {
		if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
			return err
		}
		return db.QueryRow(`PRAGMA legacy_alter_table`).Scan(&legacyAlterTable)
	}}
	if err := schema.ApplyMigration(context.Background(), db, probe); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 0 {
		t.Fatalf("migration transaction saw foreign_keys=%d: pragmas did not run on the transaction's connection", foreignKeys)
	}
	if legacyAlterTable != 1 {
		t.Fatalf("migration transaction saw legacy_alter_table=%d: pragmas did not run on the transaction's connection", legacyAlterTable)
	}
}

// --- Decomposition Policy v2 fixtures and tests ---

const v2BlueprintArtifact = `{"decomposition_policy_version":2,"project_info":{"project":"Task System","nature":"CLI + pipeline + team","date":"2026-08-29"},"goals":{"primary_goal":"Reliable workflow","target_audience":"Owner and agents","key_message":"Every transition is durable"},"architecture":{"building_blocks":["CLI","Scheduler","SQLite"],"connection_summary":"CLI drives scheduler state","data_flow":"Inputs -> CLI -> SQLite"},"tech_stack":[{"layer":"Backend","choice":"Go","rationale":"Existing","reuse":"go-pic"}],"file_structure":[{"path":"go-pic/cmd/pic","purpose":"Workflow backend"}],"rri_requirements_matrix":[{"blueprint_section":"Lifecycle","requirements":["REQ-001"],"source_questions":["Q1"]},{"blueprint_section":"Delivery","requirements":["REQ-002"],"source_questions":["Q2"]}],"verification_seams":[{"id":"cli-materialize","surface":"pic work-item materialize against a temporary SQLite database","isolates":"materialization atomicity and idempotency","prior_art":"TestWorkItemGraphMaterialization"},{"id":"go-tests","surface":"go test ./... in the repository","isolates":"package-level behavior regressions"}]}`

func TestDecompositionProjectionMigration(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a database already migrated to the pre-v8 baseline: versions 1-4
	// and 6 recorded, legacy steps never carried, old-shape core tables.
	if _, err = db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT DEFAULT (datetime('now')));
		CREATE TABLE work_items (id TEXT PRIMARY KEY, type TEXT NOT NULL, parent_id TEXT, title TEXT NOT NULL, description TEXT DEFAULT '', priority TEXT DEFAULT 'medium', status TEXT DEFAULT 'open', created_at TEXT DEFAULT (datetime('now')));
		CREATE TABLE work_item_relations (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL, relation_type TEXT NOT NULL, related_work_item_id TEXT NOT NULL, created_at TEXT DEFAULT (datetime('now')));
		INSERT INTO work_items(id,type,title) VALUES('wi-old','epic','Old Epic');
		INSERT INTO work_item_relations(id,work_item_id,relation_type,related_work_item_id) VALUES('wir-old','wi-old','blocks','wi-old');
		INSERT INTO schema_migrations(version,name) VALUES(1,'pre_reconcile_schema'),(2,'artifact_stage_widening'),(3,'pipeline_columns_reconcile'),(4,'canonical_baseline'),(6,'canonical_backfills')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := project.InitDB(dbPath); err != nil {
		t.Fatalf("pre-v8 database failed to migrate: %v", err)
	}
	db, err = project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var recorded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=8`).Scan(&recorded); err != nil || recorded != 1 {
		t.Fatalf("migration 8 recorded=%d err=%v", recorded, err)
	}
	var mode, rationale string
	if err := db.QueryRow(`SELECT decomposition_mode FROM work_items WHERE id='wi-old'`).Scan(&mode); err != nil || mode != "vertical" {
		t.Fatalf("projection default mode=%q, want vertical: err=%v", mode, err)
	}
	if err := db.QueryRow(`SELECT rationale FROM work_item_relations WHERE id='wir-old'`).Scan(&rationale); err != nil || rationale != "" {
		t.Fatalf("rationale default=%q err=%v", rationale, err)
	}
	var revision int
	if err := db.QueryRow(`SELECT source_graph_revision FROM work_items WHERE id='wi-old'`).Scan(&revision); err != nil || revision != 0 {
		t.Fatalf("source revision default=%d err=%v", revision, err)
	}
	db.Close()
	// Once-semantics: a second open must not re-apply the additive columns.
	if err := project.InitDB(dbPath); err != nil {
		t.Fatalf("second open re-applied migration 8: %v", err)
	}
}

func querySQLiteColumn(t *testing.T, dbPath string, query string) string {
	t.Helper()
	out, err := exec.Command("sqlite3", dbPath, query).CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite query %q failed: %v\n%s", query, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestDependencyRelationsConvergentBackfill(t *testing.T) {
	t.Parallel()
	// The dependency-to-relations backfill must converge on every open, not
	// only when migration 6 first applies: post-migration APM imports write
	// task-graph edges into the retired work_item_dependencies table, and the
	// readiness SQL (workitem.ReadySQL) reads only work_item_relations. Without
	// the per-open backfill, dep-blocked leaves compute ready=true and the
	// scheduler launches them out of dependency order.
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := project.InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO work_items (id, type, title) VALUES
		('wi-dep-a','task','first'),
		('wi-dep-b','task','second'),
		('wi-dep-gate','task','gate holder')`); err != nil {
		db.Close()
		t.Fatalf("seed work items: %v", err)
	}
	// Simulate a post-migration import: dependency and gate edges with no
	// matching work_item_relations rows.
	if _, err := db.Exec(`INSERT INTO work_item_dependencies (id, work_item_id, depends_on_work_item_id) VALUES ('wid-test-1','wi-dep-b','wi-dep-a')`); err != nil {
		db.Close()
		t.Fatalf("seed dependency: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO work_item_gates (id, work_item_id, gate_work_item_id) VALUES ('wig-test-1','wi-dep-b','wi-dep-gate')`); err != nil {
		db.Close()
		t.Fatalf("seed gate: %v", err)
	}
	db.Close()

	// Re-open: the convergent backfill must surface both edges as relations.
	if err := project.InitDB(dbPath); err != nil {
		t.Fatalf("second initDB: %v", err)
	}
	db, err = project.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var blocks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM work_item_relations WHERE work_item_id='wi-dep-b' AND relation_type='blocks' AND related_work_item_id='wi-dep-a'`).Scan(&blocks); err != nil {
		t.Fatal(err)
	}
	if blocks != 1 {
		t.Fatalf("dependency edge not backfilled as blocks relation on reopen: count=%d", blocks)
	}
	var gates int
	if err := db.QueryRow(`SELECT COUNT(*) FROM work_item_relations WHERE work_item_id='wi-dep-b' AND relation_type='gates' AND related_work_item_id='wi-dep-gate'`).Scan(&gates); err != nil {
		t.Fatal(err)
	}
	if gates != 1 {
		t.Fatalf("gate edge not backfilled as gates relation on reopen: count=%d", gates)
	}
	// Re-running initDB again must stay idempotent (INSERT OR IGNORE).
	db.Close()
	if err := project.InitDB(dbPath); err != nil {
		t.Fatalf("third initDB: %v", err)
	}
}

func clearedPiEnv() []string {
	env := []string{}
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "PI_TASK_AGENT_NAME=") {
			continue
		}
		env = append(env, entry)
	}
	return env
}
