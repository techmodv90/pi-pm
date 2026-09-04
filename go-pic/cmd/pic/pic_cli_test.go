package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/earendil-works/task-system/go-pic/internal/tip"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
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
	handleAPI(res, req)
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
	handleAPI(res, req)
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
	handleAPI(res, req)
	var summary map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary["projectId"] != projectID {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestVersionReportsGoImplementation(t *testing.T) {
	bin := buildPic(t)
	out := asObject(t, runPic(t, bin, t.TempDir(), t.TempDir(), "--version"))
	if out["implementation"] != "go" || out["sqlite"] != "modernc.org/sqlite" {
		t.Fatalf("--version = %#v", out)
	}
}

func TestInitAndProjectCommands(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	if _, err := os.Stat(filepath.Join(root, ".pi", "tasks.db")); err != nil {
		t.Fatalf("tasks.db not created: %v", err)
	}
	db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if tableExists(db, "task_items") {
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
	bin := buildProductionPic(t)
	root, home := initProject(t, bin)
	db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
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
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := openSQLite(dbPath)
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
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err = openSQLite(dbPath)
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
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := openSQLite(dbPath)
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
		if err := initDB(dbPath); err != nil {
			t.Fatal(err)
		}
	}
	db, err = openSQLite(dbPath)
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
	bin := buildPic(t)
	root, home := initProject(t, bin)

	created := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Canonical detail"))
	shown := asObject(t, runPic(t, bin, root, home, "show", created["id"].(string)))
	if asObject(t, shown["work_item"])["id"] != created["id"] {
		t.Fatalf("generic show did not return canonical Work Item detail: %#v", shown)
	}
	if shown["ready"] != false {
		t.Fatalf("standalone Work Item without an active TIP is ready: %#v", shown["ready"])
	}
	for _, field := range []string{"children", "dependencies", "artifacts", "checkpoints", "instruction_packs", "verification_reports"} {
		if _, ok := shown[field].([]any); !ok {
			t.Fatalf("generic show field %s = %#v", field, shown[field])
		}
	}
}

func TestWorkItemReadinessAndClaim(t *testing.T) {
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

func TestWorkItemArtifactGateSequence(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Artifact Epic"))
	id := epic["id"].(string)
	stages := []string{"scan", "rri", "vision", "blueprint", "contracts", "task_graph"}
	for index, stage := range stages {
		content := stage + " content"
		if stage == "vision" {
			content = validVisionArtifact
		}
		if stage == "blueprint" {
			content = validBlueprintArtifact
		}
		if stage == "contracts" {
			content = validContractArtifact
		}
		if stage == "task_graph" {
			content = `{"version":3,"execution_policy":"strict_sequential","nodes":[{"key":"F01","type":"feature","name":"Area","requirement_keys":[],"depends_on":[]}]}`
		}
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, content))
		if artifact["revision"] != float64(1) || artifact["content_hash"] == "" {
			t.Fatalf("%s artifact = %#v", stage, artifact)
		}
		status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
		if status["next_stage"] != stage {
			t.Fatalf("before %s approval status = %#v", stage, status)
		}
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
		status = asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
		expected := "materialize"
		if index+1 < len(stages) {
			expected = stages[index+1]
		}
		if status["next_stage"] != expected {
			t.Fatalf("after %s approval status = %#v", stage, status)
		}
	}

	revisedBlueprint := strings.Replace(validBlueprintArtifact, "Reliable workflow", "Revised reliable workflow", 1)
	blueprint2 := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "blueprint", revisedBlueprint))
	if blueprint2["revision"] != float64(2) {
		t.Fatalf("revised blueprint = %#v", blueprint2)
	}
	status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["next_stage"] != "blueprint" {
		t.Fatalf("downstream invalidation status = %#v", status)
	}
	checkpoints := status["checkpoints"].(map[string]any)
	if checkpoints["scan"] != true || checkpoints["rri"] != true || checkpoints["vision"] != true || checkpoints["blueprint"] == true || checkpoints["contracts"] == true || checkpoints["task_graph"] == true {
		t.Fatalf("checkpoint invalidation = %#v", checkpoints)
	}
	if out := runPicError(t, bin, root, home, "work-item", "artifact-approve", id, "blueprint", "missing", "approved"); !strings.Contains(out, "not current") {
		t.Fatalf("unbound approval error = %s", out)
	}
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	cmd := exec.Command("sqlite3", dbPath, `UPDATE work_item_artifacts SET content='mutated' WHERE id='`+blueprint2["id"].(string)+`';`)
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "immutable") {
		t.Fatalf("artifact mutation err=%v out=%s", err, out)
	}
}

func TestApprovedTaskGraphValidation(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Graph Epic"))
	id := epic["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-1','`+id+`','REQ-001','Required','Given valid context
When work runs
Then it completes')`)
	for _, stage := range []string{"scan", "rri", "vision", "blueprint", "contracts"} {
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, planningArtifactContent(stage)))
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
	}
	graph := `{"version":3,"execution_policy":"parallel_allowed","nodes":[{"key":"F01","type":"feature","name":"Area","parent_key":"","requirement_keys":[],"depends_on":[]},{"key":"T01","type":"task","name":"Implement","parent_key":"F01","goal":"Implement requirement","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]}]}`
	artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graph))
	if out := asObject(t, runPic(t, bin, root, home, "work-item", "graph-validate", id)); out["valid"] != true || out["artifact_id"] != artifact["id"] || out["node_count"] != float64(2) {
		t.Fatalf("draft graph validation = %#v", out)
	}
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", artifact["id"].(string), "approved")

	invalid := strings.Replace(graph, `"REQ-001"`, `"REQ-404"`, 1)
	invalidArtifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", invalid))
	if out := runPicError(t, bin, root, home, "work-item", "graph-validate", id); !strings.Contains(out, "unknown requirement REQ-404") {
		t.Fatalf("unknown requirement error = %s; first=%s", out, artifact["id"])
	}
	if out := runPicError(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", invalidArtifact["id"].(string), "approved"); !strings.Contains(out, "task graph validation failed") {
		t.Fatalf("invalid graph approval error = %s", out)
	}
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-prose','`+id+`','REQ-002','Prose only','Work completes successfully')`)
	proseGraph := strings.Replace(graph, `"requirement_keys":["REQ-001"]`, `"requirement_keys":["REQ-001","REQ-002"]`, 1)
	proseArtifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", proseGraph))
	if out := runPicError(t, bin, root, home, "work-item", "graph-validate", id); !strings.Contains(out, "REQ-002 acceptance criteria require Given, When, and Then steps") {
		t.Fatalf("non-Gherkin graph error = %s", out)
	}
	if out := runPicError(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", proseArtifact["id"].(string), "approved"); !strings.Contains(out, "task graph validation failed") {
		t.Fatalf("non-Gherkin approval error = %s", out)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var workItems int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_items`).Scan(&workItems); err != nil || workItems != 1 {
		t.Fatalf("validation mutated work items: count=%d err=%v", workItems, err)
	}
}

func TestWorkItemGraphMaterialization(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Materialize Epic"))
	id := epic["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-m','`+id+`','REQ-001','Required','Given valid context
When work runs
Then it completes')`)
	for _, stage := range []string{"scan", "rri", "vision", "blueprint", "contracts"} {
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, planningArtifactContent(stage)))
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
	}
	graph := `{"version":3,"execution_policy":"strict_sequential","nodes":[{"key":"F01","type":"feature","name":"Area","requirement_keys":[],"depends_on":[]},{"key":"T01","type":"task","name":"First","parent_key":"F01","goal":"First","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]},{"key":"B01","type":"bug","name":"Second","parent_key":"F01","goal":"Second","requirement_keys":["REQ-001"],"depends_on":["T01"],"priority":"P0","module":"core","files":["y.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["y.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]}]}`
	artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graph))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", artifact["id"].(string), "approved")
	first := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))
	second := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))
	if first["created"] != float64(3) || second["created"] != float64(0) {
		t.Fatalf("materialization first=%#v second=%#v", first, second)
	}
	revised := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graph))
	if revised["revision"] != float64(2) {
		t.Fatalf("revised task graph = %#v", revised)
	}
	if status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id)); status["next_stage"] != "task_graph" {
		t.Fatalf("revised graph status = %#v", status)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var children, dependencies int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id IN (?,(SELECT id FROM work_items WHERE parent_id=? AND type='feature'))`, id, id).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_relations WHERE relation_type='blocks'`).Scan(&dependencies); err != nil {
		t.Fatal(err)
	}
	if children != 3 || dependencies != 1 {
		t.Fatalf("materialized children=%d dependencies=%d", children, dependencies)
	}
	var firstTaskID string
	if err = db.QueryRow(`SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND node_key='T01'`, id).Scan(&firstTaskID); err != nil {
		t.Fatal(err)
	}
	var eagerPacks int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE checkpoint_id=?`, first["checkpoint_id"]).Scan(&eagerPacks); err != nil || eagerPacks != 0 {
		t.Fatalf("materialization created %d eager TIPs, err=%v", eagerPacks, err)
	}
	content := `{"schemaVersion":3,"skillFamilies":[],"goal":"Correct materialized task","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"go test ./...","required":true}]}`
	pack := asObject(t, runPic(t, bin, root, home, "workflow", "instruction-pack-save", firstTaskID, "--source-type", "standalone_task", "--content-json", content, "--requirement-ids-json", `["req-m"]`, "--activate", "1"))
	if pack["checkpoint_id"] != first["checkpoint_id"] {
		t.Fatalf("materialized child pack checkpoint = %#v, materialization = %#v", pack, first)
	}
	var packsBefore int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=?`, firstTaskID).Scan(&packsBefore); err != nil {
		t.Fatal(err)
	}
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-prose','`+id+`','REQ-002','Prose only','Work completes successfully')`)
	if out := runPicError(t, bin, root, home, "workflow", "instruction-pack-save", firstTaskID, "--source-type", "standalone_task", "--content-json", content, "--requirement-ids-json", `["req-prose"]`); !strings.Contains(out, "REQ-002 acceptance criteria require Given, When, and Then steps") {
		t.Fatalf("non-Gherkin acceptance error = %s", out)
	}
	var packs int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=?`, firstTaskID).Scan(&packs); err != nil || packs != packsBefore {
		t.Fatalf("packs after rejected non-Gherkin save = %d, err=%v", packs, err)
	}

	repaired := asObject(t, runPic(t, bin, root, home, "work-item", "execution-reset", firstTaskID, "owner"))
	if repaired["id"] != firstTaskID || repaired["status"] != "open" {
		t.Fatalf("child execution reset = %#v", repaired)
	}
	var preservedMappings, preservedSiblings, activePacks int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_materializations WHERE root_work_item_id=?`, id).Scan(&preservedMappings)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=? OR parent_id IN (SELECT id FROM work_items WHERE parent_id=?)`, id, id).Scan(&preservedSiblings)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, firstTaskID).Scan(&activePacks)
	if preservedMappings != 3 || preservedSiblings != 3 || activePacks != 0 {
		t.Fatalf("child repair mappings=%d children=%d active_packs=%d", preservedMappings, preservedSiblings, activePacks)
	}
	if status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", firstTaskID)); status["next_stage"] != "instruction_pack" {
		t.Fatalf("child repair next stage = %#v", status)
	}

	reset := asObject(t, runPic(t, bin, root, home, "work-item", "planning-reset", firstTaskID, "owner"))
	if reset["id"] != id || reset["status"] != "open" {
		t.Fatalf("owner re-scope reset = %#v", reset)
	}
	var remainingChildren, mappings, graphCheckpoints, stalePacks int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=? OR parent_id IN (SELECT id FROM work_items WHERE parent_id=?)`, id, id).Scan(&remainingChildren)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_materializations WHERE root_work_item_id=?`, id).Scan(&mappings)
	_ = db.QueryRow(`SELECT COUNT(*) FROM workflow_checkpoints WHERE work_item_id=? AND stage='task_graph'`, id).Scan(&graphCheckpoints)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=?`, firstTaskID).Scan(&stalePacks)
	if remainingChildren != 0 || mappings != 0 || graphCheckpoints != 0 || stalePacks != 0 {
		t.Fatalf("owner re-scope retained children=%d mappings=%d task_graph_checkpoints=%d stale_packs=%d", remainingChildren, mappings, graphCheckpoints, stalePacks)
	}
	status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["next_stage"] != "scan" {
		t.Fatalf("owner re-scope next stage = %#v", status)
	}
	var staleArtifacts, staleCheckpoints, staleRequirements, staleDecisions int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=?`, id).Scan(&staleArtifacts)
	_ = db.QueryRow(`SELECT COUNT(*) FROM workflow_checkpoints WHERE work_item_id=?`, id).Scan(&staleCheckpoints)
	_ = db.QueryRow(`SELECT COUNT(*) FROM requirements WHERE epic_id=?`, id).Scan(&staleRequirements)
	_ = db.QueryRow(`SELECT COUNT(*) FROM owner_decisions WHERE epic_id=?`, id).Scan(&staleDecisions)
	if staleArtifacts != 0 || staleCheckpoints != 0 || staleRequirements != 0 || staleDecisions != 0 {
		t.Fatalf("owner re-scope retained artifacts=%d checkpoints=%d requirements=%d decisions=%d", staleArtifacts, staleCheckpoints, staleRequirements, staleDecisions)
	}
}

func TestPlanningResetInvalidatesApprovedLineageForRescan(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Stale planning"))
	id := item["id"].(string)
	for _, stage := range []string{"scan", "rri", "vision", "blueprint", "contracts"} {
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, planningArtifactContent(stage)))
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
	}
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-stale','`+id+`','REQ-STALE','Stale','Given stale context
When planning reruns
Then stale requirements are removed')`)
	reset := asObject(t, runPic(t, bin, root, home, "work-item", "planning-reset", id, "owner"))
	if reset["id"] != id || reset["status"] != "open" {
		t.Fatalf("planning reset = %#v", reset)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var artifacts, checkpoints, requirements int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=?`, id).Scan(&artifacts)
	_ = db.QueryRow(`SELECT COUNT(*) FROM workflow_checkpoints WHERE work_item_id=?`, id).Scan(&checkpoints)
	_ = db.QueryRow(`SELECT COUNT(*) FROM requirements WHERE epic_id=?`, id).Scan(&requirements)
	if artifacts != 0 || checkpoints != 0 || requirements != 0 {
		t.Fatalf("stale planning remained artifacts=%d checkpoints=%d requirements=%d", artifacts, checkpoints, requirements)
	}
	status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["next_stage"] != "scan" {
		t.Fatalf("planning reset next stage = %#v", status)
	}
}

func TestStandaloneWorkItemGeneratesTIPBeforeFirstClaim(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Standalone task"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,task_id,requirement_key,title,acceptance_criteria) VALUES('req-standalone','`+id+`','REQ-001','Required','Given valid context
When work runs
Then it completes')`)
	for _, stage := range []string{"scan", "rri"} {
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, planningArtifactContent(stage)))
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
	}
	graph := `{"version":3,"execution_policy":"strict_sequential","nodes":[{"key":"IMPLEMENT","type":"task","name":"Standalone task","goal":"Implement standalone task","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]}]}`
	artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graph))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", artifact["id"].(string), "approved")
	result := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))
	if result["created"] != float64(0) || result["total"] != float64(1) {
		t.Fatalf("standalone materialization = %#v", result)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var children, mappings, packs int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=?`, id).Scan(&children)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_materializations WHERE root_work_item_id=? AND work_item_id=?`, id, id).Scan(&mappings)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=?`, id).Scan(&packs)
	if children != 0 || mappings != 1 || packs != 0 {
		t.Fatalf("standalone children=%d mappings=%d eager packs=%d", children, mappings, packs)
	}
	authorized := asObject(t, runPic(t, bin, root, home, "work-item", "authorize", id, "owner"))
	if authorized["activated"] != float64(0) {
		t.Fatalf("standalone authorization = %#v", authorized)
	}
	ready := runPic(t, bin, root, home, "work-item", "ready").([]any)
	if len(ready) != 1 || ready[0].(map[string]any)["id"] != id {
		t.Fatalf("authorized item is not ready before TIP generation: %#v", ready)
	}
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	var generatedAcceptance, packID string
	if err = db.QueryRow(`SELECT id,json_extract(content_json,'$.requirements[0].acceptance_criteria') FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, id).Scan(&packID, &generatedAcceptance); err != nil {
		t.Fatal(err)
	}
	if claim["instruction_pack_id"] != packID || generatedAcceptance != "Given valid context\nWhen work runs\nThen it completes" {
		t.Fatalf("claim=%#v generated TIP=%s acceptance=%q", claim, packID, generatedAcceptance)
	}
}

func TestStandaloneGraphRevisionReusesRootWorkItem(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Parent task"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,task_id,requirement_key,title,acceptance_criteria) VALUES('req-stable','`+id+`','REQ-001','Required','Given valid context
When work runs
Then it completes')`)
	for _, stage := range []string{"scan", "rri"} {
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, planningArtifactContent(stage)))
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
	}
	graph := `{"version":3,"execution_policy":"strict_sequential","nodes":[{"key":"T01","type":"task","name":"Child","goal":"Implement child","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]}]}`
	firstArtifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graph))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", firstArtifact["id"].(string), "approved")
	first := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))
	secondArtifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graph))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", secondArtifact["id"].(string), "approved")
	second := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))
	if first["created"] != float64(0) || second["created"] != float64(0) {
		t.Fatalf("materialization first=%#v second=%#v", first, second)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var children, mappings int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=?`, id).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_materializations WHERE root_work_item_id=?`, id).Scan(&mappings); err != nil {
		t.Fatal(err)
	}
	if children != 0 || mappings != 2 {
		t.Fatalf("children=%d mappings=%d", children, mappings)
	}
}

func TestVisionArtifactRequiresStructuredJSON(t *testing.T) {
	if err := validateVisionReport(`{"project_name":"Incomplete"}`); err == nil || !strings.Contains(err.Error(), "nature") {
		t.Fatalf("expected incomplete Vision rejection, got %v", err)
	}
	valid := `{"project_name":"Task System","nature":{"interface":"CLI","lifecycle":"Pipeline","scale":"Team"},"dimensions":{"interface":"CLI","data_flow":"SQLite","user_model":"Owner and agents","lifecycle":"Pipeline","scale":"Team","state":"Persistent DB"},"architecture":{"entry_points":["pic"],"core_modules":["scheduler"],"data_layer":["SQLite"],"integration_points":[],"cross_cutting_concerns":["audit"],"connection_summary":"Commands drive persisted stages."},"user_flows":[{"user_type":"Owner","entry":"CLI","core_loop":"Review","edge_cases":["Rejection"],"exit":"Approval"}],"non_ui_direction":{"type":"CLI","decisions":["JSON output"]},"tech_stack":[{"layer":"Runtime","choice":"Go","rationale":"Existing","reuse":"Current"}]}`
	if err := validateVisionReport(valid); err != nil {
		t.Fatalf("expected valid Vision, got %v", err)
	}
}

func TestWorkItemRriFinalizeActorRole(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "RRI actor provenance"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	payload, _ := json.Marshal(map[string]any{
		"requirements": []map[string]any{{"key": "REQ-ACTOR", "priority": "tier1", "title": "Actor provenance", "description": "Record the validated actor", "acceptanceCriteria": "Given an authorized contractor actor role\nWhen rri-finalize runs\nThen the rri_finalized event records the validated role"}},
		"decisions":    []map[string]any{{"key": "actor_provenance", "answer": "Record the validated contractor role on rri_finalized"}},
		"report":       map[string]any{"project_name": "RRI actor provenance", "generated": "2026-08-17", "requirements_matrix": []map[string]string{{"req_id": "REQ-ACTOR", "requirement": "Actor provenance", "source": "RRI Q#1", "priority": "P0", "persona": "Developer"}}, "auto_answered": []map[string]string{}, "decisions_log": []map[string]string{{"decision": "Actor provenance", "options_considered": "Hardcoded vs validated role", "chosen": "Validated role", "rationale": "Audit provenance"}}, "open_questions": []map[string]string{}},
	})
	db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if out := runPicError(t, bin, root, home, "work-item", "rri-finalize", id, string(payload)); !strings.Contains(out, "invalid actor role") {
		t.Fatalf("missing actor role not rejected by validateWorkflowActor: %s", out)
	}
	if out := runPicError(t, bin, root, home, "work-item", "rri-finalize", id, string(payload), "--actor-role", "owner"); !strings.Contains(out, "invalid actor role") {
		t.Fatalf("non-contractor actor role not rejected: %s", out)
	}
	childCmd := exec.Command(bin, "work-item", "rri-finalize", id, string(payload), "--actor-role", "contractor")
	childCmd.Dir = root
	childCmd.Env = append(clearedPiEnv(), "HOME="+home, "PI_TASK_AGENT_NAME=worker-1")
	// Child agents are rejected by the cmdWorkItem lifecycle guard before
	// validateWorkflowActor runs; either rejection keeps the lifecycle
	// state machine closed to child-agent authority.
	if childOut, childErr := childCmd.CombinedOutput(); childErr == nil || !strings.Contains(string(childOut), "cannot mutate Work Item lifecycle through pic") {
		t.Fatalf("child-agent rri-finalize not rejected: err=%v out=%s", childErr, childOut)
	}
	var artifacts int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=? AND stage='rri'`, id).Scan(&artifacts); err != nil {
		t.Fatal(err)
	}
	if artifacts != 0 {
		t.Fatalf("authorization failures wrote %d RRI artifacts", artifacts)
	}

	finalized := asObject(t, runPic(t, bin, root, home, "work-item", "rri-finalize", id, string(payload), "--actor-role", "contractor"))
	if finalized["requirements"] != float64(1) || finalized["artifact_id"] == "" {
		t.Fatalf("finalized = %#v", finalized)
	}
	var role string
	if err = db.QueryRow(`SELECT actor_role FROM work_item_events WHERE work_item_id=? AND event_type='rri_finalized'`, id).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if role != "contractor" {
		t.Fatalf("rri_finalized actor_role=%q, want contractor", role)
	}
}

func TestWorkItemRriFinalizePersistsCanonicalInterview(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "RRI finalization"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", "# Disposable interview checkpoint")
	payload, _ := json.Marshal(map[string]any{
		"requirements": []map[string]any{{"key": "REQ-BASELINE", "priority": "tier1", "title": "Clean baseline", "description": "Verify one revision", "acceptanceCriteria": "Given release work is complete\nWhen verification starts\nThen the exact clean commit is recorded"}},
		"decisions":    []map[string]any{{"key": "release_baseline", "answer": "Require a clean committed baseline"}},
		"report":       map[string]any{"project_name": "RRI finalization", "generated": "2026-08-17", "requirements_matrix": []map[string]string{{"req_id": "REQ-BASELINE", "requirement": "Clean baseline", "source": "RRI Q#1", "priority": "P0", "persona": "Developer"}}, "auto_answered": []map[string]string{}, "decisions_log": []map[string]string{{"decision": "Release baseline", "options_considered": "Clean vs dirty", "chosen": "Clean", "rationale": "Reproducibility"}}, "open_questions": []map[string]string{}},
	})
	finalized := asObject(t, runPic(t, bin, root, home, "work-item", "rri-finalize", id, string(payload), "--actor-role", "contractor"))
	if finalized["requirements"] != float64(1) || finalized["decisions"] != float64(1) || finalized["artifact_id"] == "" {
		t.Fatalf("finalized = %#v", finalized)
	}
	shown := asObject(t, runPic(t, bin, root, home, "show", id))
	requirements := shown["requirements"].([]any)
	decisions := shown["planning_owner_decisions"].([]any)
	if len(requirements) != 1 || requirements[0].(map[string]any)["requirement_key"] != "REQ-BASELINE" || len(decisions) != 1 || decisions[0].(map[string]any)["decision_type"] != "release_baseline" {
		t.Fatalf("requirements=%#v decisions=%#v", requirements, decisions)
	}
	db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var artifacts int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=? AND stage='rri'`, id).Scan(&artifacts); err != nil || artifacts != 2 {
		t.Fatalf("RRI artifacts=%d err=%v", artifacts, err)
	}
	var legacyEpic int
	if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='epics'`).Scan(&legacyEpic); err != nil || legacyEpic != 0 {
		t.Fatalf("legacy Epic table exists=%d err=%v", legacyEpic, err)
	}
	var canonicalRevision int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=? AND stage='rri' AND revision=2`, id).Scan(&canonicalRevision); err != nil || canonicalRevision != 1 {
		t.Fatalf("canonical RRI revision 2 count=%d err=%v", canonicalRevision, err)
	}
	revised := asObject(t, runPic(t, bin, root, home, "work-item", "rri-finalize", id, string(payload), "--actor-role", "contractor"))
	if revised["revised"] != true {
		t.Fatalf("expected pre-approval RRI revision, got %#v", revised)
	}
	approved := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-approve", id, "rri", "current", "approved"))
	if approved["artifact_id"] != revised["artifact_id"] {
		t.Fatalf("current selector approved %v, want %v", approved["artifact_id"], revised["artifact_id"])
	}
	runPicError(t, bin, root, home, "work-item", "rri-finalize", id, string(payload), "--actor-role", "contractor")
	var requirementCount, decisionCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM requirements WHERE epic_id=?`, id).Scan(&requirementCount)
	_ = db.QueryRow(`SELECT COUNT(*) FROM owner_decisions WHERE epic_id=?`, id).Scan(&decisionCount)
	if requirementCount != 1 || decisionCount != 1 {
		t.Fatalf("duplicate finalization wrote requirements=%d decisions=%d", requirementCount, decisionCount)
	}
}

func TestRriReportValidationMarkerGate(t *testing.T) {
	legacy := rriReport{
		ProjectName: "Project", Generated: "2026-09-01",
		OpenQuestions: []rriOpenQuestion{{ID: "Q1", Question: "Legacy shape row"}},
	}
	if err := validateRriReport(legacy); err != nil {
		t.Fatalf("legacy open_questions row must stay valid: %v", err)
	}
	marked := rriReport{
		ProjectName: "Project", Generated: "2026-09-01", PolicyVersion: 2,
		RequirementsMatrix: []rriRequirementRow{}, AutoAnswered: []rriAutoAnswerRow{}, DecisionsLog: []rriDecisionRow{},
		NotYetSpecified: []rriNotYetSpecifiedRow{}, OutOfScope: []rriOutOfScopeRow{},
		OpenQuestions: []rriOpenQuestion{{ID: "Q1", Question: "Resolved frontier row", Status: "resolved", Priority: "P1", Mode: "hitl", Blocks: boolPtr(true), Resolution: &rriResolution{Answer: "Ship CLI first", Source: "Owner confirm"}}},
	}
	if err := validateRriReport(marked); err != nil {
		t.Fatalf("resolved frontier row with resolution must be accepted: %v", err)
	}
	openRow := marked
	openRow.OpenQuestions = []rriOpenQuestion{{ID: "Q2", Question: "Open frontier row", Status: "open", Priority: "P0", Mode: "afk", Blocks: boolPtr(false)}}
	if err := validateRriReport(openRow); err != nil {
		t.Fatalf("open frontier row without resolution must be accepted: %v", err)
	}
	cases := []struct {
		name string
		row  rriOpenQuestion
		want string
	}{{
		name: "missing status",
		row:  rriOpenQuestion{ID: "Q1", Question: "No status", Priority: "P1", Mode: "hitl", Blocks: boolPtr(true)},
		want: "requires status",
	}, {
		name: "invalid status",
		row:  rriOpenQuestion{ID: "Q1", Question: "Bad status", Status: "parked", Priority: "P1", Mode: "hitl", Blocks: boolPtr(true)},
		want: "invalid status",
	}, {
		name: "missing priority",
		row:  rriOpenQuestion{ID: "Q1", Question: "No priority", Status: "open", Mode: "hitl", Blocks: boolPtr(true)},
		want: "requires priority",
	}, {
		name: "invalid priority",
		row:  rriOpenQuestion{ID: "Q1", Question: "Bad priority", Status: "open", Priority: "P9", Mode: "hitl", Blocks: boolPtr(true)},
		want: "invalid priority",
	}, {
		name: "missing mode",
		row:  rriOpenQuestion{ID: "Q1", Question: "No mode", Status: "open", Priority: "P1", Blocks: boolPtr(true)},
		want: "requires mode",
	}, {
		name: "invalid mode",
		row:  rriOpenQuestion{ID: "Q1", Question: "Bad mode", Status: "open", Priority: "P1", Mode: "async", Blocks: boolPtr(true)},
		want: "invalid mode",
	}, {
		name: "missing blocks",
		row:  rriOpenQuestion{ID: "Q1", Question: "No blocks", Status: "open", Priority: "P1", Mode: "hitl"},
		want: "requires blocks",
	}, {
		name: "open with empty resolution object",
		row:  rriOpenQuestion{ID: "Q1", Question: "Open with empty resolution", Status: "open", Priority: "P0", Mode: "afk", Blocks: boolPtr(false), Resolution: &rriResolution{}},
		want: "requires resolution answer and source",
	}, {
		name: "resolved without resolution",
		row:  rriOpenQuestion{ID: "Q1", Question: "No resolution", Status: "resolved", Priority: "P1", Mode: "hitl", Blocks: boolPtr(true)},
		want: "requires resolution",
	}, {
		name: "deferred without source",
		row:  rriOpenQuestion{ID: "Q1", Question: "Partial resolution", Status: "deferred", Priority: "P2", Mode: "afk", Blocks: boolPtr(true), Resolution: &rriResolution{Answer: "Later"}},
		want: "requires resolution",
	}}
	for _, tc := range cases {
		report := marked
		report.OpenQuestions = []rriOpenQuestion{tc.row}
		err := validateRriReport(report)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected error containing %q, got %v", tc.name, tc.want, err)
		}
	}
	unsupported := marked
	unsupported.PolicyVersion = 3
	if err := validateRriReport(unsupported); err == nil || !strings.Contains(err.Error(), "unsupported rri_policy_version") {
		t.Fatalf("expected unsupported rri_policy_version rejection, got %v", err)
	}
}

func boolPtr(value bool) *bool { return &value }

// Typed unmarshalling rejects non-string open_questions id and question values
// before any validator runs, mirroring the field-type checks in
// reporting/rri-report.ts so the renderer never sees a non-string row field.
func TestRriOpenQuestionFieldTypesRejectedByUnmarshal(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "numeric id",
			raw:  `{"project_name":"Project","generated":"2026-09-01","open_questions":[{"id":7,"question":"Numeric id"}]}`,
			want: "cannot unmarshal number into Go struct field",
		},
		{
			name: "boolean question",
			raw:  `{"project_name":"Project","generated":"2026-09-01","open_questions":[{"id":"Q1","question":false}]}`,
			want: "cannot unmarshal bool into Go struct field",
		},
	}
	for _, tc := range cases {
		var report rriReport
		err := json.Unmarshal([]byte(tc.raw), &report)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected typed unmarshal rejection, got %v", tc.name, err)
		}
	}
}

// Dual-enforcer parity (REQ-F1-7, OB-F1-7): marker-gated required-array
// strictness must match the TypeScript validator in reporting/rri-report.ts,
// including the named missing-array defect in the error message.
func TestRriPolicy(t *testing.T) {
	markedBase := rriReport{
		ProjectName: "Project", Generated: "2026-09-01", PolicyVersion: 2,
		RequirementsMatrix: []rriRequirementRow{}, AutoAnswered: []rriAutoAnswerRow{},
		DecisionsLog: []rriDecisionRow{}, OpenQuestions: []rriOpenQuestion{},
		NotYetSpecified: []rriNotYetSpecifiedRow{}, OutOfScope: []rriOutOfScopeRow{},
	}
	if err := validateRriReport(markedBase); err != nil {
		t.Fatalf("marked complete report must proceed: %v", err)
	}
	// Marked reports with a missing required array are rejected, and the defect
	// names the missing array identically to the TypeScript validator.
	nilBySection := map[string]func(r *rriReport){
		"requirements_matrix": func(r *rriReport) { r.RequirementsMatrix = nil },
		"auto_answered":       func(r *rriReport) { r.AutoAnswered = nil },
		"decisions_log":       func(r *rriReport) { r.DecisionsLog = nil },
		"open_questions":      func(r *rriReport) { r.OpenQuestions = nil },
	}
	for section, clear := range nilBySection {
		report := markedBase
		clear(&report)
		err := validateRriReport(report)
		want := "marked RRI report is missing the " + section + " section"
		if err == nil || err.Error() != want {
			t.Fatalf("marked report missing %s: expected error %q, got %v", section, want, err)
		}
	}
	// Legacy reports retain their prior tolerated shape: missing arrays stay
	// valid under legacy rules, matching the TypeScript legacy tolerance.
	legacy := rriReport{ProjectName: "Project", Generated: "2026-09-01"}
	if err := validateRriReport(legacy); err != nil {
		t.Fatalf("legacy report with every array missing must stay valid: %v", err)
	}
	legacyPartial := rriReport{
		ProjectName: "Project", Generated: "2026-09-01",
		OpenQuestions: []rriOpenQuestion{{ID: "Q1", Question: "Legacy shape row"}},
	}
	if err := validateRriReport(legacyPartial); err != nil {
		t.Fatalf("legacy report with some arrays missing must stay valid: %v", err)
	}
}

// Stage-order parity (REQ-F1-7): the Go artifact-stage taxonomy must match the
// TypeScript scheduler's PLANNING_STAGE_ORDER in pi-ext/pipeline/stage-resolution.ts
// stage for stage, including the supplementary rri_t_scenarios entry.
func TestRriStageOrder(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tsSource, err := os.ReadFile(filepath.Join(pkgDir, "..", "..", "..", "pi-ext", "pipeline", "stage-resolution.ts"))
	if err != nil {
		t.Fatalf("read TypeScript stage order: %v", err)
	}
	match := regexp.MustCompile(`PLANNING_STAGE_ORDER: string\[\] = \[([^\]]*)\]`).FindSubmatch(tsSource)
	if match == nil {
		t.Fatal("PLANNING_STAGE_ORDER not found in pi-ext/pipeline/stage-resolution.ts")
	}
	var tsOrder []string
	for _, raw := range strings.Split(string(match[1]), ",") {
		entry := strings.Trim(strings.TrimSpace(raw), "\"")
		if entry != "" {
			tsOrder = append(tsOrder, entry)
		}
	}
	if !reflect.DeepEqual(workItemStages, tsOrder) {
		t.Fatalf("Go workItemStages %v must match TypeScript PLANNING_STAGE_ORDER %v", workItemStages, tsOrder)
	}
	if indexOfStage(workItemStages, "rri_t_scenarios") != indexOfStage(workItemStages, "rri")+1 {
		t.Fatalf("rri_t_scenarios must directly follow rri in both stage orders: %v", workItemStages)
	}
}

func TestRriScopeSections(t *testing.T) {
	markedBase := rriReport{
		ProjectName: "Project", Generated: "2026-09-01", PolicyVersion: 2,
		RequirementsMatrix: []rriRequirementRow{}, AutoAnswered: []rriAutoAnswerRow{},
		DecisionsLog: []rriDecisionRow{}, OpenQuestions: []rriOpenQuestion{},
		NotYetSpecified: []rriNotYetSpecifiedRow{},
		OutOfScope:      []rriOutOfScopeRow{},
	}
	if err := validateRriReport(markedBase); err != nil {
		t.Fatalf("marked report with empty scope sections must be accepted: %v", err)
	}
	filled := markedBase
	filled.NotYetSpecified = []rriNotYetSpecifiedRow{{Uncertainty: "Export formats", GraduationPath: "Resolve with the owner before contracts"}}
	filled.OutOfScope = []rriOutOfScopeRow{{Exclusion: "Cloud sync", Reason: "Outside the epic scope"}}
	if err := validateRriReport(filled); err != nil {
		t.Fatalf("marked report with filled scope sections must be accepted: %v", err)
	}
	cases := []struct {
		name string
		mut  func(r *rriReport)
		want string
	}{{
		name: "missing not_yet_specified",
		mut:  func(r *rriReport) { r.NotYetSpecified = nil },
		want: "requires the not_yet_specified section",
	}, {
		name: "missing out_of_scope",
		mut:  func(r *rriReport) { r.OutOfScope = nil },
		want: "requires the out_of_scope section",
	}, {
		name: "not_yet_specified row without graduation path",
		mut:  func(r *rriReport) { r.NotYetSpecified = []rriNotYetSpecifiedRow{{Uncertainty: "Export formats"}} },
		want: "require uncertainty and graduation_path",
	}, {
		name: "out_of_scope row without reason",
		mut:  func(r *rriReport) { r.OutOfScope = []rriOutOfScopeRow{{Exclusion: "Cloud sync"}} },
		want: "require exclusion and reason",
	}}
	for _, tc := range cases {
		report := markedBase
		tc.mut(&report)
		err := validateRriReport(report)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected error containing %q, got %v", tc.name, tc.want, err)
		}
	}
	legacy := rriReport{
		ProjectName: "Project", Generated: "2026-09-01",
		OpenQuestions: []rriOpenQuestion{{ID: "Q1", Question: "Legacy shape row"}},
	}
	if err := validateRriReport(legacy); err != nil {
		t.Fatalf("legacy report without scope sections must stay valid: %v", err)
	}
	// No Destination field exists in the schema: a destination key in the payload
	// is ignored at unmarshal time and never re-emitted by persistence.
	var withDestination rriReport
	if err := json.Unmarshal([]byte(`{"project_name":"Project","generated":"2026-09-01","rri_policy_version":2,"requirements_matrix":[],"auto_answered":[],"decisions_log":[],"open_questions":[],"not_yet_specified":[],"out_of_scope":[],"destination":"Work Item goals"}`), &withDestination); err != nil {
		t.Fatal(err)
	}
	if err := validateRriReport(withDestination); err != nil {
		t.Fatalf("destination payload must validate without a Destination field: %v", err)
	}
	if persisted, err := json.Marshal(withDestination); err != nil || strings.Contains(string(persisted), "destination") {
		t.Fatalf("persisted report must not carry a destination field: %s err=%v", persisted, err)
	}
}

func TestWorkItemRriFinalizeMarkedFrontierReportPersists(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Marked RRI finalization"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", "# Disposable interview checkpoint")
	payload, _ := json.Marshal(map[string]any{
		"requirements": []map[string]any{{"key": "REQ-FRONTIER", "priority": "tier1", "title": "Frontier persistence", "description": "Persist frontier rows", "acceptanceCriteria": "Given a marked RRI payload\nWhen rri-finalize runs\nThen the frontier rows persist"}},
		"decisions":    []map[string]any{{"key": "frontier_mode", "answer": "Adopt the frontier schema"}},
		"report": map[string]any{
			"project_name": "Marked RRI finalization", "generated": "2026-09-01", "rri_policy_version": 2,
			"requirements_matrix": []map[string]string{{"req_id": "REQ-FRONTIER", "requirement": "Frontier persistence", "source": "RRI Q#1", "priority": "P0", "persona": "Developer"}},
			"auto_answered":       []map[string]string{}, "decisions_log": []map[string]string{{"decision": "Frontier mode", "options_considered": "Legacy vs frontier", "chosen": "Frontier", "rationale": "Single contract"}},
			"open_questions":    []map[string]any{{"id": "Q1", "question": "Ship order", "status": "resolved", "priority": "P1", "mode": "hitl", "blocks": true, "resolution": map[string]string{"answer": "CLI first", "source": "Owner confirm"}}},
			"not_yet_specified": []map[string]string{{"uncertainty": "Export formats", "graduation_path": "Resolve with the owner before contracts"}},
			"out_of_scope":      []map[string]string{{"exclusion": "Cloud sync", "reason": "Outside the epic scope"}},
		},
	})
	finalized := asObject(t, runPic(t, bin, root, home, "work-item", "rri-finalize", id, string(payload), "--actor-role", "contractor"))
	if finalized["artifact_id"] == "" {
		t.Fatalf("finalized = %#v", finalized)
	}
	db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var content string
	if err = db.QueryRow(`SELECT content FROM work_item_artifacts WHERE work_item_id=? AND stage='rri' AND revision=2`, id).Scan(&content); err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err = json.Unmarshal([]byte(content), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted["rri_policy_version"] != float64(2) {
		t.Fatalf("persisted rri_policy_version = %#v", persisted["rri_policy_version"])
	}
	questions := persisted["open_questions"].([]any)
	row := questions[0].(map[string]any)
	if row["status"] != "resolved" || row["priority"] != "P1" || row["mode"] != "hitl" || row["blocks"] != true {
		t.Fatalf("persisted frontier row = %#v", row)
	}
	resolution := row["resolution"].(map[string]any)
	if resolution["answer"] != "CLI first" || resolution["source"] != "Owner confirm" {
		t.Fatalf("persisted resolution = %#v", resolution)
	}
	scopeRows := persisted["not_yet_specified"].([]any)
	if len(scopeRows) != 1 || scopeRows[0].(map[string]any)["uncertainty"] != "Export formats" || scopeRows[0].(map[string]any)["graduation_path"] != "Resolve with the owner before contracts" {
		t.Fatalf("persisted not_yet_specified = %#v", scopeRows)
	}
	excluded := persisted["out_of_scope"].([]any)
	if len(excluded) != 1 || excluded[0].(map[string]any)["exclusion"] != "Cloud sync" || excluded[0].(map[string]any)["reason"] != "Outside the epic scope" {
		t.Fatalf("persisted out_of_scope = %#v", excluded)
	}
	if _, hasDestination := persisted["destination"]; hasDestination {
		t.Fatalf("persisted report must not carry a destination field: %#v", persisted)
	}
}

func TestWorkItemRriFinalizeMarkedEmptyScopeSectionsPersist(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Empty scope finalization"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", "# Disposable interview checkpoint")
	payload, _ := json.Marshal(map[string]any{
		"requirements": []map[string]any{{"key": "REQ-SCOPE-EMPTY", "priority": "tier1", "title": "Scope sections persist", "description": "Persist empty scope sections", "acceptanceCriteria": "Given a marked report with empty scope sections\nWhen rri-finalize runs\nThen both scope keys persist"}},
		"decisions":    []map[string]any{},
		"report": map[string]any{
			"project_name": "Empty scope finalization", "generated": "2026-09-01", "rri_policy_version": 2,
			"requirements_matrix": []map[string]string{{"req_id": "REQ-SCOPE-EMPTY", "requirement": "Scope sections persist", "source": "RRI Q#1", "priority": "P0", "persona": "Developer"}},
			"auto_answered":       []map[string]string{}, "decisions_log": []map[string]string{},
			"open_questions":    []map[string]any{},
			"not_yet_specified": []map[string]string{},
			"out_of_scope":      []map[string]string{},
		},
	})
	finalized := asObject(t, runPic(t, bin, root, home, "work-item", "rri-finalize", id, string(payload), "--actor-role", "contractor"))
	if finalized["artifact_id"] == "" {
		t.Fatalf("finalized = %#v", finalized)
	}
	db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var content string
	if err = db.QueryRow(`SELECT content FROM work_item_artifacts WHERE work_item_id=? AND stage='rri' AND revision=2`, id).Scan(&content); err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err = json.Unmarshal([]byte(content), &persisted); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"not_yet_specified", "out_of_scope"} {
		rows, ok := persisted[key].([]any)
		if !ok || len(rows) != 0 {
			t.Fatalf("persisted %s must remain an empty array, got %#v", key, persisted[key])
		}
	}
}

func TestWorkItemRriFinalizeRejectsMalformedMarkedRows(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Rejected RRI finalization"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", "# Disposable interview checkpoint")
	base := map[string]any{
		"requirements": []map[string]any{{"key": "REQ-FRONTIER", "priority": "tier1", "title": "Frontier persistence", "description": "Persist frontier rows", "acceptanceCriteria": "Given a marked RRI payload\nWhen rri-finalize runs\nThen the frontier rows persist"}},
		"decisions":    []map[string]any{{"key": "frontier_mode", "answer": "Adopt the frontier schema"}},
		"report": map[string]any{
			"project_name": "Rejected RRI finalization", "generated": "2026-09-01", "rri_policy_version": 2,
			"requirements_matrix": []map[string]string{{"req_id": "REQ-FRONTIER", "requirement": "Frontier persistence", "source": "RRI Q#1", "priority": "P0", "persona": "Developer"}},
			"auto_answered":       []map[string]string{}, "decisions_log": []map[string]string{{"decision": "Frontier mode", "options_considered": "Legacy vs frontier", "chosen": "Frontier", "rationale": "Single contract"}},
			"open_questions": []map[string]any{},
		},
	}
	cases := []struct {
		name string
		row  map[string]any
		want string
	}{{
		name: "missing status",
		row:  map[string]any{"id": "Q1", "question": "No status", "priority": "P1", "mode": "hitl", "blocks": true},
		want: "requires status",
	}, {
		name: "invalid enum",
		row:  map[string]any{"id": "Q1", "question": "Bad status", "status": "parked", "priority": "P1", "mode": "hitl", "blocks": true},
		want: "invalid status",
	}, {
		name: "non-string question",
		row:  map[string]any{"id": "Q1", "question": false, "status": "open", "priority": "P0", "mode": "afk", "blocks": false},
		want: "requires valid JSON",
	}, {
		name: "resolved without resolution",
		row:  map[string]any{"id": "Q1", "question": "No resolution", "status": "resolved", "priority": "P1", "mode": "hitl", "blocks": true},
		want: "requires resolution",
	}}
	for _, tc := range cases {
		report := cloneJSONMap(t, base["report"])
		report["open_questions"] = []any{tc.row}
		payload, _ := json.Marshal(map[string]any{"requirements": base["requirements"], "decisions": base["decisions"], "report": report})
		out := runPicError(t, bin, root, home, "work-item", "rri-finalize", id, string(payload), "--actor-role", "contractor")
		if !strings.Contains(out, tc.want) {
			t.Fatalf("%s: expected error containing %q, got %s", tc.name, tc.want, out)
		}
	}
	db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var artifacts int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=? AND revision=2`, id).Scan(&artifacts); err != nil || artifacts != 0 {
		t.Fatalf("rejected finalization persisted artifacts=%d err=%v", artifacts, err)
	}
	// No artifact at any revision may appear from the rejected payload: the
	// validation failures above must leave canonical persistence unchanged.
	var anyArtifacts int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=?`, id).Scan(&anyArtifacts); err != nil || anyArtifacts != 2 {
		t.Fatalf("rejected finalization left unexpected artifacts=%d err=%v", anyArtifacts, err)
	}
}

func cloneJSONMap(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

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

func TestRriGlossaryApproval(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	contextPath := filepath.Join(root, "CONTEXT.md")
	original := "# Work Item Planning Context\n\n**Feature**:\nA coherent, demonstrable vertical slice of behavior.\n_Avoid_: frontend phase, backend phase\n"
	if err := os.WriteFile(contextPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	assertContextUnchanged := func(want string) {
		t.Helper()
		content, err := os.ReadFile(contextPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != want {
			t.Fatalf("CONTEXT.md = %q, want %q", string(content), want)
		}
	}
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Glossary approval"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	// The interview checkpoint is disposable and must never touch repository truth.
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", "# Disposable interview checkpoint")
	assertContextUnchanged(original)
	runPic(t, bin, root, home, "work-item", "rri-finalize", id, glossaryApplyPayload(t), "--actor-role", "contractor")
	// Publication resolves the interview but still must not modify CONTEXT.md.
	assertContextUnchanged(original)
	// A failed approval leaves repository truth unchanged.
	runPicError(t, bin, root, home, "work-item", "artifact-approve", id, "rri", "missing", "approved")
	assertContextUnchanged(original)
	approved := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-approve", id, "rri", "current", "approved"))
	if approved["glossary_updated"] != true {
		t.Fatalf("approved = %#v", approved)
	}
	want := original + "\n**Delivery Aggregate**:\nThe branch-owning delivery aggregate for one approved feature branch.\n_Avoid_: delivery bucket\n"
	assertContextUnchanged(want)

	// An approved RRI without glossary updates leaves CONTEXT.md untouched.
	second := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "No glossary updates"))
	id2 := second["id"].(string)
	scan2 := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id2, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id2, "scan", scan2["id"].(string), "accepted")
	plain, err := json.Marshal(map[string]any{
		"requirements": []map[string]any{{"key": "REQ-PLAIN", "priority": "tier1", "title": "Vertical slice delivery", "description": "Deliver one vertical slice", "acceptanceCriteria": "Given a resolved requirement\nWhen approval runs\nThen no glossary write happens"}},
		"decisions":    []map[string]any{{"key": "glossary_mode", "answer": "Gate marked reports"}},
		"report": map[string]any{
			"project_name": "No glossary updates", "generated": "2026-09-01", "rri_policy_version": 2,
			"requirements_matrix": []map[string]string{{"req_id": "REQ-PLAIN", "requirement": "Vertical slice delivery", "source": "RRI Q#1", "priority": "P0", "persona": "Developer"}},
			"auto_answered":       []map[string]string{}, "decisions_log": []map[string]string{{"decision": "Glossary mode", "options_considered": "Guarded vs legacy", "chosen": "Guarded", "rationale": "Canonical terminology"}},
			"open_questions": []any{}, "not_yet_specified": []map[string]string{}, "out_of_scope": []map[string]string{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runPic(t, bin, root, home, "work-item", "rri-finalize", id2, string(plain), "--actor-role", "contractor")
	approved2 := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-approve", id2, "rri", "current", "approved"))
	if approved2["glossary_updated"] != false {
		t.Fatalf("approved2 = %#v", approved2)
	}
	assertContextUnchanged(want)
}

// Approval-rejection constraint (REQ-F1-6): an RRI artifact whose
// glossary_updates section fails validation must not be owner-approved
// silently; the approval fails, reports the invalid data, and CONTEXT.md
// stays unchanged.
func TestRriGlossaryApprovalRejectsInvalidUpdates(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	contextPath := filepath.Join(root, "CONTEXT.md")
	original := "# Work Item Planning Context\n\n**Feature**:\nA coherent, demonstrable vertical slice of behavior.\n_Avoid_: frontend phase, backend phase\n"
	if err := os.WriteFile(contextPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Invalid glossary approval"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")

	invalidPayloads := []string{
		// Validation failure: a row without a usable definition.
		`{"project_name":"Invalid glossary","generated":"2026-09-01","rri_policy_version":2,"glossary_updates":[{"term":"Broken Term","definition":"   "}]}`,
		// Type failure: glossary_updates present but not an update array.
		`{"project_name":"Invalid glossary","generated":"2026-09-01","rri_policy_version":2,"glossary_updates":"not-an-array"}`,
	}
	for _, payload := range invalidPayloads {
		saved := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", payload))
		out := runPicError(t, bin, root, home, "work-item", "artifact-approve", id, "rri", saved["id"].(string), "approved")
		if !strings.Contains(out, "glossary_updates") {
			t.Fatalf("approval of invalid glossary_updates must report the invalid data, got: %s", out)
		}
		content, err := os.ReadFile(contextPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != original {
			t.Fatalf("rejected approval modified CONTEXT.md = %q, want %q", string(content), original)
		}
	}
}

// Repository-root constraint (REQ-F1-6): the approval-time glossary write must
// follow repository truth discovery, not the process working directory, so an
// approval run from a nested directory updates the canonical root CONTEXT.md
// and never creates a shadowing copy next to the caller.
func TestRriGlossaryApprovalFromNestedDirectory(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	contextPath := filepath.Join(root, "CONTEXT.md")
	original := "# Work Item Planning Context\n\n**Feature**:\nA coherent, demonstrable vertical slice of behavior.\n_Avoid_: frontend phase, backend phase\n"
	if err := os.WriteFile(contextPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "cmd", "pic")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	item := asObject(t, runPic(t, bin, nested, home, "work-item", "create", "epic", "Nested glossary approval"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, nested, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, nested, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	finalized := asObject(t, runPic(t, bin, nested, home, "work-item", "rri-finalize", id, glossaryApplyPayload(t), "--actor-role", "contractor"))
	if finalized["artifact_id"] == "" {
		t.Fatalf("finalized = %#v", finalized)
	}
	approved := asObject(t, runPic(t, bin, nested, home, "work-item", "artifact-approve", id, "rri", "current", "approved"))
	if approved["glossary_updated"] != true {
		t.Fatalf("approved = %#v", approved)
	}
	want := original + "\n**Delivery Aggregate**:\nThe branch-owning delivery aggregate for one approved feature branch.\n_Avoid_: delivery bucket\n"
	content, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != want {
		t.Fatalf("root CONTEXT.md = %q, want %q", string(content), want)
	}
	if _, err := os.Stat(filepath.Join(nested, "CONTEXT.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("approval from nested cwd must not create %s (stat err=%v)", filepath.Join(nested, "CONTEXT.md"), err)
	}
}

// Compensation constraint (REQ-F1-6): when a failed approval cannot restore
// the pre-write CONTEXT.md, the compensation closure must report its failure
// instead of leaving repository truth silently changed.
func TestRriGlossaryApprovalCompensationFailure(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	contextPath := filepath.Join(root, "CONTEXT.md")
	original := "# Work Item Planning Context\n\n**Feature**:\nA coherent, demonstrable vertical slice of behavior.\n_Avoid_: frontend phase, backend phase\n"
	if err := os.WriteFile(contextPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Glossary compensation"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	finalized := asObject(t, runPic(t, bin, root, home, "work-item", "rri-finalize", id, glossaryApplyPayload(t), "--actor-role", "contractor"))
	artifactID := finalized["artifact_id"].(string)

	// Run the writer in-process against the project so findRriTruthRoot
	// resolves the same temp root from this working directory.
	t.Chdir(root)
	db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	apply := func() func() error {
		t.Helper()
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		updated, restore, err := applyRriGlossaryApproval(tx, id, "rri", artifactID)
		if err != nil || !updated {
			t.Fatalf("applyRriGlossaryApproval = (%v, %v), want updated write", updated, err)
		}
		return restore
	}
	// A healthy compensation puts back the exact pre-write content.
	restore := apply()
	if err := restore(); err != nil {
		t.Fatalf("restore() = %v, want nil", err)
	}
	content, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != original {
		t.Fatalf("restored CONTEXT.md = %q, want %q", string(content), original)
	}
	// A blocked restore must surface its error instead of swallowing it.
	restore = apply()
	if err := os.Chmod(contextPath, 0o444); err != nil {
		t.Fatal(err)
	}
	if restoreErr := restore(); restoreErr == nil {
		t.Fatal("restore() = nil, want error when the pre-write content cannot be written back")
	}
	if err := os.Chmod(contextPath, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRriGlossaryConflictFailsClosed(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	// A frontend-phase requirement title contradicts CONTEXT.md's Feature
	// definition; this mirrors the real repository truth the guard reads.
	if err := os.WriteFile(filepath.Join(root, "CONTEXT.md"), []byte("# Work Item Planning Context\n\n**Feature**:\nA coherent, demonstrable vertical slice of behavior.\n_Avoid_: frontend phase, backend phase\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Glossary guard"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", "# Disposable interview checkpoint")
	out := runPicError(t, bin, root, home, "work-item", "rri-finalize", id, glossaryRriPayload(t, "Frontend phase delivery", "Deliver the frontend phase first", "Gate marked reports"), "--actor-role", "contractor")
	if !strings.Contains(out, "frontend phase") || !strings.Contains(out, "CONTEXT.md") || !strings.Contains(out, "Feature") {
		t.Fatalf("glossary conflict must name the term, canonical definition, and source document, got %s", out)
	}
	db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var artifacts, requirements, decisions, events int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=? AND stage='rri' AND revision=2`, id).Scan(&artifacts); err != nil || artifacts != 0 {
		t.Fatalf("conflicting save persisted artifacts=%d err=%v", artifacts, err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM requirements WHERE epic_id=?`, id).Scan(&requirements); err != nil || requirements != 0 {
		t.Fatalf("conflicting save persisted requirements=%d err=%v", requirements, err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM owner_decisions WHERE epic_id=?`, id).Scan(&decisions); err != nil || decisions != 0 {
		t.Fatalf("conflicting save persisted decisions=%d err=%v", decisions, err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_events WHERE work_item_id=? AND event_type='rri_finalized'`, id).Scan(&events); err != nil || events != 0 {
		t.Fatalf("conflicting save persisted events=%d err=%v", events, err)
	}
	// The same payload without the conflicting terminology proceeds normally.
	finalized := asObject(t, runPic(t, bin, root, home, "work-item", "rri-finalize", id, glossaryRriPayload(t, "Vertical slice delivery", "Deliver one vertical slice", "Gate marked reports"), "--actor-role", "contractor"))
	if finalized["requirements"] != float64(1) || finalized["artifact_id"] == "" {
		t.Fatalf("conflict-free save must proceed normally, got %#v", finalized)
	}
}

func TestRriGlossaryDecisionConflictFailsClosed(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	if err := os.WriteFile(filepath.Join(root, "CONTEXT.md"), []byte("# Work Item Planning Context\n\n**Requirement**:\nAn authoritative behavioral obligation.\n_Avoid_: wish\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Decision glossary guard"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", "# Disposable interview checkpoint")
	out := runPicError(t, bin, root, home, "work-item", "rri-finalize", id, glossaryRriPayload(t, "Vertical slice delivery", "Deliver one vertical slice", "Treat requirements as a wish"), "--actor-role", "contractor")
	if !strings.Contains(out, "decision glossary_mode") || !strings.Contains(out, "wish") {
		t.Fatalf("decision conflicts must use the same check with kind decision, got %s", out)
	}
}

func TestRriAdrConflictFailsClosed(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	if err := os.MkdirAll(filepath.Join(root, "docs", "adr"), 0o755); err != nil {
		t.Fatal(err)
	}
	adr := "# Rejected Planning Shape\n\n**Status**: accepted\n\nWe reject both one oversized Task per slice and removing child Code Review in favor of aggregate QA alone.\n"
	if err := os.WriteFile(filepath.Join(root, "docs", "adr", "0001-test.md"), []byte(adr), 0o644); err != nil {
		t.Fatal(err)
	}
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "ADR guard"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", "# Disposable interview checkpoint")
	out := runPicError(t, bin, root, home, "work-item", "rri-finalize", id, glossaryRriPayload(t, "Vertical slice delivery", "Deliver one vertical slice", "One oversized Task per slice"), "--actor-role", "contractor")
	if !strings.Contains(out, "one oversized Task per slice") || !strings.Contains(out, "0001-test.md") {
		t.Fatalf("ADR conflict must name the phrase and source document, got %s", out)
	}
	// An accepted ADR that constrains a practice without the word reject (the
	// ADR 0002 speculative-abstraction rule) must block a decision endorsing
	// exactly that practice.
	seams := "# Codebase Design At Module Seams\n\n**Status**: accepted\n\nAdd a Seam only when behavior actually varies; one implementation alone is not sufficient justification for a speculative abstraction.\n"
	if err := os.WriteFile(filepath.Join(root, "docs", "adr", "0002-seams.md"), []byte(seams), 0o644); err != nil {
		t.Fatal(err)
	}
	out = runPicError(t, bin, root, home, "work-item", "rri-finalize", id, glossaryRriPayload(t, "Vertical slice delivery", "Deliver one vertical slice", "adding a speculative abstraction with one implementation"), "--actor-role", "contractor")
	if !strings.Contains(out, "speculative abstraction") || !strings.Contains(out, "0002-seams.md") {
		t.Fatalf("accepted ADR constraint without reject wording must block the save, got %s", out)
	}
	// A non-accepted ADR stays advisory and must not block, even for phrases
	// only it rejects.
	if err := os.WriteFile(filepath.Join(root, "docs", "adr", "0003-draft.md"), []byte("# Draft\n\n**Status**: proposed\n\nWe reject adapter wands as Seam justification.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runPic(t, bin, root, home, "work-item", "rri-finalize", id, glossaryRriPayload(t, "Vertical slice delivery", "Deliver one vertical slice", "Adapter wands stay out"), "--actor-role", "contractor")
}

func TestRriGlossaryReportTextPersistsUnchanged(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	if err := os.WriteFile(filepath.Join(root, "CONTEXT.md"), []byte("# Work Item Planning Context\n\n**Feature**:\nA coherent, demonstrable vertical slice of behavior.\n_Avoid_: frontend phase\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Source text preservation"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", "# Disposable interview checkpoint")
	finalized := asObject(t, runPic(t, bin, root, home, "work-item", "rri-finalize", id, glossaryRriPayload(t, "Vertical  Slice   delivery", "Deliver one vertical slice", "Gate marked reports"), "--actor-role", "contractor"))
	db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var content string
	if err = db.QueryRow(`SELECT content FROM work_item_artifacts WHERE id=?`, finalized["artifact_id"].(string)).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, `"Vertical  Slice   delivery"`) {
		t.Fatalf("persisted report must preserve source text despite normalization, got %s", content)
	}
}

func TestRriGlossaryParsesRepositoryTruth(t *testing.T) {
	// The test binary runs in go-pic/cmd/pic, so the repository truth is three
	// levels up; prove the parsing conventions match the real documents.
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "CONTEXT.md"))
	if err != nil {
		t.Fatal(err)
	}
	terms := parseRriGlossaryAvoidTerms(string(content), "CONTEXT.md")
	var epicAvoid bool
	for _, term := range terms {
		if term.Canonical == "Epic" && term.Phrase == "one giant Task" {
			epicAvoid = true
		}
	}
	if !epicAvoid {
		t.Fatalf("real CONTEXT.md must yield the Epic avoid terms, got %#v", terms)
	}
	adr, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "adr", "0001-vertical-slice-groups-and-bite-sized-tasks.md"))
	if err != nil {
		t.Fatal(err)
	}
	adrTerms := parseRriAdrRejectedTerms(string(adr), "docs/adr/0001-vertical-slice-groups-and-bite-sized-tasks.md")
	var oversized bool
	for _, term := range adrTerms {
		if term.Phrase == "one oversized Task per slice" {
			oversized = true
		}
	}
	if !oversized {
		t.Fatalf("accepted ADR 0001 must yield its rejected phrase, got %#v", adrTerms)
	}
}

func TestRriAdrParsesSpeculativeAbstractionConstraint(t *testing.T) {
	// Accepted ADR 0002 forbids speculative abstractions whose only
	// justification is a single implementation; the parser must contribute
	// that constraint even though the sentence never uses the word reject.
	adr, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "adr", "0002-codebase-design-at-module-seams.md"))
	if err != nil {
		t.Fatal(err)
	}
	adrTerms := parseRriAdrRejectedTerms(string(adr), "docs/adr/0002-codebase-design-at-module-seams.md")
	var speculative bool
	for _, term := range adrTerms {
		if term.Phrase == "speculative abstraction" {
			speculative = true
		}
	}
	if !speculative {
		t.Fatalf("accepted ADR 0002 must yield its speculative abstraction constraint, got %#v", adrTerms)
	}
}

func TestRriPublishGateBlocksOpenP0P1Questions(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	for _, tc := range []struct {
		name    string
		row     map[string]any
		wantErr string
	}{
		{name: "open P0", row: map[string]any{"id": "Q1", "question": "Ship order", "status": "open", "priority": "P0", "mode": "hitl", "blocks": true}, wantErr: "open P0/P1 question Q1 (P0) remains unresolved: Ship order"},
		{name: "open P1", row: map[string]any{"id": "Q2", "question": "Auth mode", "status": "open", "priority": "P1", "mode": "hitl", "blocks": true}, wantErr: "open P0/P1 question Q2 (P1) remains unresolved: Auth mode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Publish gate "+tc.name))
			id := item["id"].(string)
			scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
			runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
			runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", "# Disposable interview checkpoint")
			out := runPicError(t, bin, root, home, "work-item", "rri-finalize", id, markedRriPayload(t, "Publish gate "+tc.name, []any{tc.row}), "--actor-role", "contractor")
			if !strings.Contains(out, tc.wantErr) {
				t.Fatalf("open P0/P1 finalization must name the open question, got %s", out)
			}
			db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var artifacts, decisions, events int
			if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=? AND stage='rri' AND revision=2`, id).Scan(&artifacts); err != nil || artifacts != 0 {
				t.Fatalf("rejected publication persisted artifacts=%d err=%v", artifacts, err)
			}
			if err = db.QueryRow(`SELECT COUNT(*) FROM owner_decisions WHERE epic_id=?`, id).Scan(&decisions); err != nil || decisions != 0 {
				t.Fatalf("rejected publication persisted owner decisions=%d err=%v", decisions, err)
			}
			if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_owner_decisions WHERE work_item_id=?`, id).Scan(&decisions); err != nil || decisions != 0 {
				t.Fatalf("rejected publication persisted work-item owner decisions=%d err=%v", decisions, err)
			}
			if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_events WHERE work_item_id=? AND event_type='rri_finalized'`, id).Scan(&events); err != nil || events != 0 {
				t.Fatalf("rejected publication persisted events=%d err=%v", events, err)
			}
		})
	}
	// Open P2 rows and legacy pre-marker rows must not trip the gate.
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Publish gate low priority"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", "# Disposable interview checkpoint")
	openP2 := map[string]any{"id": "Q3", "question": "Nice to have", "status": "open", "priority": "P2", "mode": "hitl", "blocks": false}
	runPic(t, bin, root, home, "work-item", "rri-finalize", id, markedRriPayload(t, "Publish gate low priority", []any{openP2}), "--actor-role", "contractor")
}

func TestRriDeferralReasonPersistence(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Deferral persistence"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", "# Disposable interview checkpoint")
	deferredP1 := map[string]any{"id": "Q1", "question": "Export formats", "status": "deferred", "priority": "P1", "mode": "hitl", "blocks": true, "resolution": map[string]string{"answer": "Owner deferred formats to the contracts stage", "source": "Owner decision 2026-09-01"}}
	deferredP2 := map[string]any{"id": "Q2", "question": "Nice to have", "status": "deferred", "priority": "P2", "mode": "afk", "blocks": false, "resolution": map[string]string{"answer": "Skip for now", "source": "Owner note"}}
	finalized := asObject(t, runPic(t, bin, root, home, "work-item", "rri-finalize", id, markedRriPayload(t, "Deferral persistence", []any{deferredP1, deferredP2}), "--actor-role", "contractor"))
	artifactID := finalized["artifact_id"].(string)
	db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var decision, questionID, artifactRef, notes string
	if err = db.QueryRow(`SELECT decision,question_id,rri_artifact_id,notes FROM work_item_owner_decisions WHERE work_item_id=? AND decision='deferred'`, id).Scan(&decision, &questionID, &artifactRef, &notes); err != nil {
		t.Fatalf("deferred P1 owner reason must persist durably in work_item_owner_decisions: %v", err)
	}
	if questionID != "Q1" || notes != "Owner deferred formats to the contracts stage" || artifactRef != artifactID {
		t.Fatalf("persisted deferral = decision %q question %q notes %q artifact %q", decision, questionID, notes, artifactRef)
	}
	// Only P0/P1 deferrals get the durable deferral record; the P2 deferral stays
	// report-only so the deferral projection tracks exactly the gated frontier.
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_owner_decisions WHERE work_item_id=? AND decision='deferred'`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("deferral records=%d err=%v, want exactly the P1 deferral", count, err)
	}
	// The deferral is available to planning review projections via the show
	// document's owner_decisions collection (core.go projects
	// work_item_owner_decisions there).
	shown := asObject(t, runPic(t, bin, root, home, "show", id))
	var projected bool
	for _, entry := range shown["owner_decisions"].([]any) {
		row := entry.(map[string]any)
		if row["decision"] == "deferred" && row["question_id"] == "Q1" && row["notes"] == "Owner deferred formats to the contracts stage" {
			projected = true
		}
	}
	if !projected {
		t.Fatalf("show owner_decisions projection must carry the deferral: %#v", shown["owner_decisions"])
	}
	// Re-finalizing replaces the deferral rows so a revision cannot strand stale
	// deferrals: Q1 resolves and a new P0 question defers with its own reason.
	resolvedQ1 := map[string]any{"id": "Q1", "question": "Export formats", "status": "resolved", "priority": "P1", "mode": "hitl", "blocks": true, "resolution": map[string]string{"answer": "CSV first", "source": "Owner"}}
	deferredP0 := map[string]any{"id": "Q10", "question": "Batch size", "status": "deferred", "priority": "P0", "mode": "hitl", "blocks": true, "resolution": map[string]string{"answer": "Owner deferred batch sizing to the contracts stage", "source": "Owner decision"}}
	revised := asObject(t, runPic(t, bin, root, home, "work-item", "rri-finalize", id, markedRriPayload(t, "Deferral persistence revised", []any{resolvedQ1, deferredP0}), "--actor-role", "contractor"))
	if revised["revised"] != true {
		t.Fatalf("re-finalization must take the revision path: %#v", revised)
	}
	var staleRows int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_owner_decisions WHERE work_item_id=? AND decision='deferred' AND question_id='Q1'`, id).Scan(&staleRows); err != nil || staleRows != 0 {
		t.Fatalf("stale deferral rows after revision=%d err=%v", staleRows, err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_owner_decisions WHERE work_item_id=? AND decision='deferred' AND question_id='Q10'`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("replacement deferral rows=%d err=%v, want exactly the revised P0 deferral", count, err)
	}
	// A deferred P0/P1 row without a real reason still rejects publication.
	item2 := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Deferral missing reason"))
	id2 := item2["id"].(string)
	scan2 := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id2, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id2, "scan", scan2["id"].(string), "accepted")
	runPic(t, bin, root, home, "work-item", "artifact-save", id2, "rri", "# Disposable interview checkpoint")
	blankReason := map[string]any{"id": "Q9", "question": "Auth mode", "status": "deferred", "priority": "P1", "mode": "hitl", "blocks": true, "resolution": map[string]string{"answer": "   ", "source": "Owner"}}
	out := runPicError(t, bin, root, home, "work-item", "rri-finalize", id2, markedRriPayload(t, "Deferral missing reason", []any{blankReason}), "--actor-role", "contractor")
	if !strings.Contains(out, "deferred P0/P1 question Q9 (P1) requires a non-empty owner deferral reason") {
		t.Fatalf("missing deferral reason must produce a concrete error, got %s", out)
	}
	var artifacts, deferralRows int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=? AND stage='rri' AND revision=2`, id2).Scan(&artifacts); err != nil || artifacts != 0 {
		t.Fatalf("rejected deferral persisted artifacts=%d err=%v", artifacts, err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_owner_decisions WHERE work_item_id=?`, id2).Scan(&deferralRows); err != nil || deferralRows != 0 {
		t.Fatalf("rejected deferral persisted owner decisions=%d err=%v", deferralRows, err)
	}
}

func TestInitDBWidensOwnerDecisionsForRriDeferrals(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a database from a binary predating the RRI deferral widening:
	// recreate the table in its legacy shape and clear migration records so the
	// next open re-runs the rebuild exactly as an upgrade would.
	legacySQL := strings.Replace(workItemOwnerDecisionsTableSQL, "completion_report_id TEXT REFERENCES", "completion_report_id TEXT NOT NULL REFERENCES", 1)
	legacySQL = strings.Replace(legacySQL, "decision IN ('accepted','rejected','deferred')", "decision IN ('accepted','rejected')", 1)
	legacySQL = strings.Replace(legacySQL, ", question_id TEXT NOT NULL DEFAULT '', rri_artifact_id TEXT NOT NULL DEFAULT ''", "", 1)
	if _, err = db.Exec(`PRAGMA foreign_keys=OFF;
		PRAGMA legacy_alter_table=ON;
		ALTER TABLE work_item_owner_decisions RENAME TO work_item_owner_decisions__workflow_migration`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err = db.Exec(legacySQL); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO work_items(id,type,title) VALUES('wi-deferral-mig','epic','Deferral migration');
		INSERT INTO work_item_owner_decisions(id,work_item_id,completion_report_id,decision,notes,decided_by_role) VALUES('wiod-legacy','wi-deferral-mig','','rejected','needs changes','owner');
		DROP TABLE work_item_owner_decisions__workflow_migration;
		DELETE FROM schema_migrations`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err = openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var decision, questionID, artifactRef, notes string
	if err = db.QueryRow(`SELECT decision,question_id,rri_artifact_id,notes FROM work_item_owner_decisions WHERE id='wiod-legacy'`).Scan(&decision, &questionID, &artifactRef, &notes); err != nil {
		t.Fatalf("legacy decision row must survive the widening rebuild: %v", err)
	}
	if decision != "rejected" || questionID != "" || artifactRef != "" || notes != "needs changes" {
		t.Fatalf("rebuilt legacy row = decision %q question %q artifact %q notes %q", decision, questionID, artifactRef, notes)
	}
	if _, err = db.Exec(`INSERT INTO work_item_owner_decisions(id,work_item_id,completion_report_id,decision,question_id,rri_artifact_id,notes,decided_by_role) VALUES('wiod-deferred','wi-deferral-mig',NULL,'deferred','Q1','wia-rri','Owner deferred formats','owner')`); err != nil {
		t.Fatalf("widened table must accept RRI deferral rows: %v", err)
	}
}

func TestImplementationAuthorization(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Authorize Epic"))
	id := epic["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-a','`+id+`','REQ-001','Required','Given valid context
When work runs
Then it completes')`)
	for _, stage := range []string{"scan", "rri", "vision", "blueprint", "contracts"} {
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, planningArtifactContent(stage)))
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
	}
	graph := `{"version":3,"execution_policy":"parallel_allowed","nodes":[{"key":"T01","type":"task","name":"Implement","goal":"Implement","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]}]}`
	artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graph))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", artifact["id"].(string), "approved")
	runPic(t, bin, root, home, "work-item", "materialize", id)
	if ready := runPic(t, bin, root, home, "work-item", "ready").([]any); len(ready) != 0 {
		t.Fatalf("materialized item ready before authorization = %#v", ready)
	}
	authorized := asObject(t, runPic(t, bin, root, home, "work-item", "authorize", id, "owner"))
	if authorized["activated"] != float64(0) || authorized["integration_mode"] != "coordination" {
		t.Fatalf("authorization = %#v", authorized)
	}
	if status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id)); status["next_stage"] != "implement" {
		t.Fatalf("post-authorization status = %#v", status)
	}
	ready := runPic(t, bin, root, home, "work-item", "ready").([]any)
	if len(ready) != 1 || ready[0].(map[string]any)["type"] != "task" {
		t.Fatalf("authorized ready = %#v", ready)
	}
	childID := ready[0].(map[string]any)["id"].(string)
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", childID, "worker"))
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var activePack, packCheckpoint string
	if err = db.QueryRow(`SELECT id,checkpoint_id FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, childID).Scan(&activePack, &packCheckpoint); err != nil {
		t.Fatal(err)
	}
	if claim["instruction_pack_id"] != activePack || packCheckpoint != authorized["checkpoint_id"] {
		t.Fatalf("aggregate child claim=%#v active pack=%s checkpoint=%s", claim, activePack, packCheckpoint)
	}
}

func TestTaskGraphValidationRejectsMissingRequirements(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Complete graph"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,task_id,requirement_key,title,acceptance_criteria) VALUES('req-one','`+id+`','REQ-001','One','Given one
When one runs
Then one completes'),('req-two','`+id+`','REQ-002','Two','Given two
When two runs
Then two completes')`)
	graph := `{"version":3,"execution_policy":"strict_sequential","nodes":[{"key":"T01","type":"task","name":"One","goal":"One","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]}]}`
	artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graph))
	runSQLite(t, dbPath, `INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-graph','`+id+`','task_graph','`+artifact["id"].(string)+`',1,'`+artifact["content_hash"].(string)+`','approved')`)

	if out := runPicError(t, bin, root, home, "work-item", "graph-validate", id); !strings.Contains(out, "missing requirements: REQ-002") {
		t.Fatalf("incomplete graph error = %s", out)
	}
}

func TestAggregateWorkItemVerificationAndClosure(t *testing.T) {
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
	db, err := openSQLite(dbPath)
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
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Correct aggregate"))
	completed := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Completed child", "--parent", epic["id"].(string)))
	runPic(t, bin, root, home, "work-item", "status", completed["id"].(string), "done")
	report := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epic["id"].(string), "failed", "release check failed", "--actor-role", "contractor"))
	db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
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

func TestAggregateDeliveryLifecycle(t *testing.T) {
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
	plan := `{"version":1,"execution_policy":"parallel_allowed","nodes":[{"key":"T01","name":"One","goal":"One","requirement_keys":["REQ-001"],"depends_on":["T02"],"priority":"P1","module":"x","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"true","required":true}]},{"key":"T02","name":"Two","goal":"Two","requirement_keys":["REQ-001"],"depends_on":["T01"],"priority":"P1","module":"x","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"true","required":true}]}]}`
	_, err := tip.ParseTaskPlanJSON("```task-plan-json\n" + plan + "\n```")
	if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestTaskPlanV2RequiresExplicitSkillFamilies(t *testing.T) {
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
	root := t.TempDir()
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	if err := initDB(dbPath); err != nil {
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
	got, gotErr := filepath.EvalSymlinks(findDB(worktree))
	want, wantErr := filepath.EvalSymlinks(dbPath)
	if gotErr != nil || wantErr != nil || got != want {
		t.Fatalf("findDB(worktree) = %q (%v), want %q (%v)", got, gotErr, want, wantErr)
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
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Contractor-owned closure"))
	if out := runPicError(t, bin, root, home, "work-item", "accept", item["id"].(string), "unused", "rejected", "needs changes", "--actor-role", "owner"); !strings.Contains(out, "only to aggregate Work Items") {
		t.Fatalf("executable owner acceptance remained available: %s", out)
	}
}

func TestReadinessRelationsRejectTransitiveCycle(t *testing.T) {
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
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.QueryRow(`SELECT advanced_at FROM pipeline_runs WHERE id='pr-stale'`).Scan(&staleAdvanced); err != nil || staleAdvanced == "" {
		t.Fatalf("stale generation was not retired: advanced=%q err=%v", staleAdvanced, err)
	}
}

func TestPipelineReviewClaimAcceptsInProgressCandidate(t *testing.T) {
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

func TestMaterializedChildInheritsParentRequirements(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	parent := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Parent"))
	child := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Child"))
	parentID := parent["id"].(string)
	childID := child["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	// Requirements linked to the parent only.
	runSQLite(t, dbPath, `INSERT INTO requirements(id,task_id,requirement_key,title,acceptance_criteria) VALUES('req-parent','`+parentID+`','REQ-PARENT','Parent requirement','Given valid context
When work runs
Then it completes')`)
	// Child task graph referencing the parent's requirement.
	graph := `{"version":3,"execution_policy":"strict_sequential","nodes":[{"key":"T01","type":"task","name":"Child","goal":"Child","requirement_keys":["REQ-PARENT"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"true","required":true}],"skillFamilies":[]}]}`
	artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", childID, "task_graph", graph))
	runSQLite(t, dbPath, `INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-child','`+childID+`','task_graph','`+artifact["id"].(string)+`',1,'`+artifact["content_hash"].(string)+`','approved'); INSERT INTO work_item_materializations(root_work_item_id,checkpoint_id,node_key,work_item_id) VALUES('`+parentID+`','wic-child','T01','`+childID+`')`)
	// Must not fail with "references unknown requirement".
	if out := runPic(t, bin, root, home, "work-item", "graph-validate", childID); strings.Contains(fmt.Sprint(out), "unknown requirement") {
		t.Fatalf("materialized child should inherit parent requirements: %v", out)
	}
}

func TestPipelineSchemaMigrationPreservesDependentObjects(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := openSQLite(dbPath)
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
	legacySQL := strings.Replace(pipelineRunsTableSQL, "REFERENCES work_items(id)", "REFERENCES tasks(id)", 1)
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
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err = openSQLite(dbPath)
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
		if ok, err := rowExists(db, `SELECT 1 FROM sqlite_master WHERE name=?`, name); err != nil || !ok {
			t.Fatalf("schema object %s missing after migration: %v", name, err)
		}
	}
}

func TestInitDBRepairsStalePipelineForeignKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := openSQLite(dbPath)
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
	if _, err = db.Exec(pipelineRunsTableSQL); err != nil {
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
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err = openSQLite(dbPath)
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

func approvePlanningTask(t *testing.T, bin, root, home, taskID string) {
	t.Helper()
	runPic(t, bin, root, home, "workflow", "scan-save", taskID, "--summary", "scanned")
	runPic(t, bin, root, home, "workflow", "rri-start", taskID)
	criteria := "Feature: Approved plan\nScenario: Implement requirement\nGiven an approved plan\nWhen implementation completes\nThen the requirement is satisfied"
	runPic(t, bin, root, home, "workflow", "requirement-add", taskID, "Requirement", "--acceptance-criteria", criteria)
	runPic(t, bin, root, home, "workflow", "rri-checkpoint-save", taskID, "--status", "awaiting_confirmation", "--interview-state-json", `{"owner_confirmed":true,"pending":[],"remaining_queue":[],"open_blockers":[]}`)
	rri := asObject(t, runPic(t, bin, root, home, "task", "show", taskID))["rri_sessions"].([]any)[0].(map[string]any)
	runPic(t, bin, root, home, "workflow", "owner-decision-add", taskID, "approve_rri", "approved", "--related-type", "rri", "--related-id", rri["id"].(string))
	runPic(t, bin, root, home, "workflow", "rri-report-save", taskID, "--report", "# RRI REPORT\n\nConfirmed")
	vision := asObject(t, runPic(t, bin, root, home, "workflow", "vision-save", taskID, "--summary", "Proposed from relevant scan"))
	runPic(t, bin, root, home, "workflow", "vision-status", taskID, "approved", "--vision-id", vision["id"].(string))
	runPic(t, bin, root, home, "workflow", "design-save", taskID, "--blueprint", "# Blueprint")
	runPic(t, bin, root, home, "workflow", "design-status", taskID, "approved")
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

func TestWorkItemPlanningAmendment(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Amend Epic"))
	id := epic["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-amend','`+id+`','REQ-001','Ports','Given the dev stack
When services boot
Then they listen on port 9173')`)
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria,contract_key) VALUES('req-keyed','`+id+`','REQ-002','Keyed','Given locked
When changed
Then refused','C-1')`)
	runSQLite(t, dbPath, `INSERT INTO owner_decisions(id,epic_id,decision_type,decision) VALUES('od-amend','`+id+`','port_prefix','dev ports use 9173')`)
	contract := strings.Replace(validContractArtifact, `"behavior":"Persist workflow state"`, `"behavior":"Persist workflow state; health endpoint serves on port 9173"`, 1)
	for _, stage := range []string{"scan", "rri", "vision", "blueprint"} {
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, planningArtifactContent(stage)))
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
	}
	contractArtifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "contracts", contract))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "contracts", contractArtifact["id"].(string), "approved")
	graph := `{"version":3,"execution_policy":"strict_sequential","nodes":[{"key":"F01","type":"feature","name":"Area","requirement_keys":[],"depends_on":[]},{"key":"T01","type":"task","name":"First","parent_key":"F01","goal":"First","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["serve on port 9173"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]},{"key":"B01","type":"bug","name":"Second","parent_key":"F01","goal":"Second","requirement_keys":["REQ-002"],"depends_on":["T01"],"priority":"P0","module":"core","files":["y.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["y.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]}]}`
	graphArtifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graph))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", graphArtifact["id"].(string), "approved")
	runPic(t, bin, root, home, "work-item", "materialize", id)

	var childID string
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.QueryRow(`SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND node_key='T01'`, id).Scan(&childID); err != nil {
		t.Fatal(err)
	}
	packContent := `{"schemaVersion":3,"skillFamilies":[],"goal":"Serve on port 9173","files":["x.go"],"business_rules":["port 9173"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"go test ./...","required":true}]}`
	pack := asObject(t, runPic(t, bin, root, home, "workflow", "instruction-pack-save", childID, "--source-type", "standalone_task", "--content-json", packContent, "--requirement-ids-json", `["req-amend"]`, "--activate", "1"))
	packID := pack["id"].(string)

	// Refusals: non-owner role, keyed immutable requirement, nothing to substitute.
	runPicError(t, bin, root, home, "work-item", "planning-amend", childID, "contractor", `{"reason":"r","substitutions":[{"old":"9173","new":"6173"}]}`)
	runPicError(t, bin, root, home, "work-item", "planning-amend", childID, "owner", `{"reason":"r","substitutions":[{"old":"locked","new":"6173"}]}`)

	result := asObject(t, runPic(t, bin, root, home, "work-item", "planning-amend", childID, "owner", `{"reason":"owner corrected dev ports from 9 prefix to 6 prefix","substitutions":[{"old":"9173","new":"6173"}]}`))
	changed := result["changed_stages"].([]any)
	foundContracts, foundGraph := false, false
	for _, entry := range changed {
		stage := entry.(map[string]any)["stage"].(string)
		if stage == "contracts" {
			foundContracts = true
		}
		if stage == "task_graph" {
			foundGraph = true
		}
	}
	if !foundContracts || !foundGraph || len(changed) != 2 {
		t.Fatalf("changed stages = %#v", changed)
	}

	var reqAcceptance string
	if err = db.QueryRow(`SELECT acceptance_criteria FROM requirements WHERE id='req-amend'`).Scan(&reqAcceptance); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reqAcceptance, "9173") || !strings.Contains(reqAcceptance, "6173") {
		t.Fatalf("requirement not amended: %q", reqAcceptance)
	}
	var decisionText string
	if err = db.QueryRow(`SELECT decision FROM owner_decisions WHERE id='od-amend'`).Scan(&decisionText); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(decisionText, "6173") {
		t.Fatalf("owner decision not amended: %q", decisionText)
	}
	var oldRevisions int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=? AND stage='contracts'`, id).Scan(&oldRevisions); err != nil {
		t.Fatal(err)
	}
	var checkpointRevision int
	var checkpointHash string
	var newContractHash string
	if err = db.QueryRow(`SELECT c.artifact_revision,c.content_hash,a.content_hash FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.work_item_id=? AND c.stage='contracts'`, id).Scan(&checkpointRevision, &checkpointHash, &newContractHash); err != nil {
		t.Fatal(err)
	}
	if checkpointRevision != 2 || checkpointHash != newContractHash || oldRevisions != 2 {
		t.Fatalf("contracts checkpoint revision=%d hash=%q artifact_rows=%d", checkpointRevision, checkpointHash, oldRevisions)
	}
	var amendedGraph string
	if err = db.QueryRow(`SELECT a.content FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.work_item_id=? AND c.stage='task_graph'`, id).Scan(&amendedGraph); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(amendedGraph, "9173") || !strings.Contains(amendedGraph, "6173") {
		t.Fatalf("task graph not amended")
	}
	var packStatus string
	if err = db.QueryRow(`SELECT status FROM work_item_instruction_packs WHERE id=?`, packID).Scan(&packStatus); err != nil {
		t.Fatal(err)
	}
	if packStatus != "stale" {
		t.Fatalf("pack status after amendment = %q", packStatus)
	}
	var events int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_events WHERE work_item_id=? AND event_type='planning_amendment'`, id).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("planning_amendment events = %d", events)
	}
	var children int
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=? OR parent_id IN (SELECT id FROM work_items WHERE parent_id=?)`, id, id).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if children != 3 {
		t.Fatalf("amendment disturbed materialized children: %d", children)
	}
	if status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id)); status["next_stage"] != "authorize" && status["next_stage"] != "implement" && status["next_stage"] != "instruction_pack" && status["next_stage"] != "aggregate_verification" {
		t.Fatalf("post-amendment aggregate status = %#v", status)
	}

	// Re-running with no remaining occurrences must be refused.
	runPicError(t, bin, root, home, "work-item", "planning-amend", childID, "owner", `{"reason":"r","substitutions":[{"old":"9173","new":"6173"}]}`)

	// Active pipeline runs block amendment.
	runSQLite(t, dbPath, `INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at) VALUES('pr-amend','`+childID+`','worker',1,'claimed','tok','2999-01-01T00:00:00Z')`)
	runPicError(t, bin, root, home, "work-item", "planning-amend", childID, "owner", `{"reason":"r","substitutions":[{"old":"6173","new":"7173"}]}`)
}

func TestWorkItemEscalationLifecycle(t *testing.T) {
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

func TestWorkItemPlanningAmendmentRetiresIntersectingEvidenceOnly(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Evidence Amend Epic"))
	id := epic["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-ev1','`+id+`','REQ-001','Ports','Given the dev stack
When services boot
Then they listen on port 9173')`)
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-ev2','`+id+`','REQ-002','Scaffold','Given a login route
When rendered
Then the scaffold mounts')`)
	for _, stage := range []string{"scan", "rri", "vision", "blueprint"} {
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, planningArtifactContent(stage)))
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
	}
	contract := strings.Replace(validContractArtifact, `"behavior":"Persist workflow state"`, `"behavior":"Persist workflow state; health endpoint serves on port 9173"`, 1)
	contractArtifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "contracts", contract))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "contracts", contractArtifact["id"].(string), "approved")
	graph := `{"version":3,"execution_policy":"strict_sequential","nodes":[{"key":"F01","type":"feature","name":"Area","requirement_keys":[],"depends_on":[]},{"key":"T01","type":"task","name":"First","parent_key":"F01","goal":"First","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["serve on port 9173"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]},{"key":"B01","type":"bug","name":"Second","parent_key":"F01","goal":"Second","requirement_keys":["REQ-002"],"depends_on":["T01"],"priority":"P0","module":"core","files":["y.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["y.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]}]}`
	graphArtifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graph))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", graphArtifact["id"].(string), "approved")
	runPic(t, bin, root, home, "work-item", "materialize", id)

	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var portChildID, scaffoldChildID string
	if err = db.QueryRow(`SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND node_key='T01'`, id).Scan(&portChildID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND node_key='B01'`, id).Scan(&scaffoldChildID); err != nil {
		t.Fatal(err)
	}
	// Verification evidence on the port child intersects the substitution target;
	// evidence on the scaffold child shares no substituted content.
	runSQLite(t, dbPath, `INSERT INTO work_item_verification_reports(id,work_item_id,status,summary,rri_t_json,verified_by_role) VALUES('wivr-port','`+portChildID+`','passed','ports migration verified','{"scenarios":[{"evidence":"binds port 9173"}]}','contractor')`)
	runSQLite(t, dbPath, `INSERT INTO work_item_verification_reports(id,work_item_id,status,summary,rri_t_json,verified_by_role) VALUES('wivr-scaffold','`+scaffoldChildID+`','passed','login scaffold verified','{"scenarios":[{"evidence":"scaffold mounts"}]}','contractor')`)

	result := asObject(t, runPic(t, bin, root, home, "work-item", "planning-amend", portChildID, "owner", `{"reason":"owner corrected dev ports from 9 prefix to 5 prefix","substitutions":[{"old":"9173","new":"55173"}]}`))
	if result["changed_stages"] == nil {
		t.Fatalf("amendment result missing changed_stages: %#v", result)
	}

	var retiredStatus, retiredSummary string
	if err = db.QueryRow(`SELECT status,summary FROM work_item_verification_reports WHERE id='wivr-port'`).Scan(&retiredStatus, &retiredSummary); err != nil {
		t.Fatal(err)
	}
	if retiredStatus != "blocked" || !strings.Contains(retiredSummary, "planning amendment") {
		t.Fatalf("intersecting evidence not retired: status=%q summary=%q", retiredStatus, retiredSummary)
	}
	var survivorStatus string
	if err = db.QueryRow(`SELECT status FROM work_item_verification_reports WHERE id='wivr-scaffold'`).Scan(&survivorStatus); err != nil {
		t.Fatal(err)
	}
	if survivorStatus != "passed" {
		t.Fatalf("non-intersecting evidence must keep its authority, got %q", survivorStatus)
	}
	var amendedGraph string
	if err = db.QueryRow(`SELECT a.content FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.work_item_id=? AND c.stage='task_graph'`, id).Scan(&amendedGraph); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(amendedGraph, "9173") || !strings.Contains(amendedGraph, "55173") {
		t.Fatalf("task graph not amended")
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

	// Existing planning stages keep their save/approve behavior unchanged.
	scanArtifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "<scan_report/>"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scanArtifact["id"].(string), "accepted")
	rriArtifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", "<rri_report/>"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "rri", rriArtifact["id"].(string), "approved")

	// Fresh SQLite schema and the artifact-save stage registry accept rri_t_scenarios.
	saved1 := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri_t_scenarios", scenariosA))
	if saved1["stage"] != "rri_t_scenarios" || saved1["revision"] != float64(1) || saved1["content_hash"] != hashJSON(scenariosA) {
		t.Fatalf("scenario artifact = %#v", saved1)
	}
	// The supplementary rri_t_scenarios stage is reported for owner visibility but
	// never becomes next_stage: the gated planning workflow still progresses normally.
	if status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id)); status["next_stage"] != "vision" {
		t.Fatalf("scenario save gated workflow: %#v", status)
	}

	// The saved scenario list is owner-visible through the existing show path.
	shown := asObject(t, runPic(t, bin, root, home, "show", id))
	scenarioRows := 0
	for _, raw := range shown["artifacts"].([]any) {
		row := raw.(map[string]any)
		if row["stage"] == "rri_t_scenarios" {
			scenarioRows++
			if row["content"] != scenariosA || row["content_hash"] != hashJSON(scenariosA) {
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

	// Approve the full downstream lineage, then re-save scenarios: only downstream
	// checkpoints (vision onward) are invalidated; scan/rri stay valid.
	runSQLite(t, dbPath, `INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES
		('wia-vision','`+id+`','vision',1,'<vision/>','h-vision'),
		('wia-blueprint','`+id+`','blueprint',1,'<blueprint/>','h-blueprint'),
		('wia-contracts','`+id+`','contracts',1,'<contracts/>','h-contracts'),
		('wia-graph','`+id+`','task_graph',1,'{}','h-graph');
		INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES
		('wic-vision','`+id+`','vision','wia-vision',1,'h-vision','approved'),
		('wic-blueprint','`+id+`','blueprint','wia-blueprint',1,'h-blueprint','approved'),
		('wic-contracts','`+id+`','contracts','wia-contracts',1,'h-contracts','approved'),
		('wic-graph','`+id+`','task_graph','wia-graph',1,'h-graph','approved')`)
	if status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id)); status["next_stage"] != "materialize" {
		t.Fatalf("full lineage status = %#v", status)
	}
	_ = asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri_t_scenarios", scenariosB))
	status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["next_stage"] != "vision" {
		t.Fatalf("downstream invalidation status = %#v", status)
	}
	checkpoints := status["checkpoints"].(map[string]any)
	if checkpoints["scan"] != true || checkpoints["rri"] != true || checkpoints["rri_t_scenarios"] == true || checkpoints["vision"] == true || checkpoints["blueprint"] == true || checkpoints["contracts"] == true || checkpoints["task_graph"] == true {
		t.Fatalf("checkpoint invalidation = %#v", checkpoints)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stage := range []string{"scan", "rri"} {
		var count int
		if err = db.QueryRow(`SELECT COUNT(*) FROM workflow_checkpoints WHERE work_item_id=? AND stage=?`, id, stage).Scan(&count); err != nil || count != 1 {
			t.Fatalf("upstream %s checkpoint count=%d err=%v", stage, count, err)
		}
	}
	for _, stage := range []string{"vision", "blueprint", "contracts", "task_graph"} {
		var count int
		if err = db.QueryRow(`SELECT COUNT(*) FROM workflow_checkpoints WHERE work_item_id=? AND stage=?`, id, stage).Scan(&count); err != nil || count != 0 {
			t.Fatalf("downstream %s checkpoint count=%d err=%v", stage, count, err)
		}
	}
	// Approved artifact history is retained, not deleted or rewritten.
	var artifacts, scenarioArtifacts int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=?`, id).Scan(&artifacts)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=? AND stage='rri_t_scenarios'`, id).Scan(&scenarioArtifacts)
	if artifacts != 9 || scenarioArtifacts != 3 {
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
	db, err := openSQLite(dbPath)
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

	// Existing approved scan/rri checkpoints survived the migration and still
	// gate the planning workflow forward (next_stage vision), never backwards.
	status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["next_stage"] != "vision" {
		t.Fatalf("legacy migration workflow status = %#v", status)
	}
	checkpoints := status["checkpoints"].(map[string]any)
	if checkpoints["scan"] != true || checkpoints["rri"] != true || checkpoints["rri_t_scenarios"] == true {
		t.Fatalf("legacy migration checkpoints = %#v", checkpoints)
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

func TestWorkflowStatusNextActionsAndCheckpointDecide(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Oracle Epic"))
	id := epic["id"].(string)

	status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["next_stage"] != "scan" {
		t.Fatalf("initial status = %#v", status)
	}
	actions, _ := status["next_actions"].([]any)
	if len(actions) == 0 {
		t.Fatal("scan next_actions missing")
	}
	first := asObject(t, actions[0])
	if first["id"] != "save_scan_artifact" || first["kind"] != "tool" || first["action"] != "save_work_item_artifact" || first["actor"] != "contractor" || !strings.Contains(fmt.Sprint(first["args"]), "scan") {
		t.Fatalf("scan first action = %#v", first)
	}
	approval := asObject(t, actions[1])
	if approval["id"] != "approve_scan_artifact" || approval["actor"] != "owner" {
		t.Fatalf("scan approval action = %#v", approval)
	}

	artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan content"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", "current", "accepted")
	_ = artifact
	for _, stage := range []string{"rri", "vision"} {
		content := stage + " content"
		if stage == "vision" {
			content = validVisionArtifact
		}
		runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, content)
	}
	out := asObject(t, runPic(t, bin, root, home, "work-item", "checkpoint-decide", id, "--decisions", "vision:approved,rri:approved"))
	decisions := out["decisions"].([]any)
	if len(decisions) != 2 {
		t.Fatalf("checkpoint-decide = %#v", out)
	}
	if fmt.Sprint(asObject(t, decisions[0])["stage"]) != "rri" || fmt.Sprint(asObject(t, decisions[1])["stage"]) != "vision" {
		t.Fatalf("decisions must process in planning-profile order: %#v", decisions)
	}
	status = asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["next_stage"] != "blueprint" {
		t.Fatalf("post-decide status = %#v", status)
	}
	actions, _ = status["next_actions"].([]any)
	if len(actions) == 0 || asObject(t, actions[0])["action"] != "save_blueprint_draft" || asObject(t, actions[len(actions)-1])["actor"] != "owner" {
		t.Fatalf("blueprint next_actions = %#v", actions)
	}

	runPic(t, bin, root, home, "work-item", "artifact-save", id, "contracts", validContractArtifact)
	if out := runPicError(t, bin, root, home, "work-item", "artifact-approve", id, "contracts", "current", "approved"); !strings.Contains(out, "Previous stage blueprint is not approved") || !strings.Contains(out, "Next: ") {
		t.Fatalf("out-of-order approval error = %s", out)
	}
	if out := runPicError(t, bin, root, home, "work-item", "checkpoint-decide", id, "--decisions", "contracts:accepted"); !strings.Contains(out, "requires decision approved") {
		t.Fatalf("bad decision error = %s", out)
	}
}

func TestInstructionPackRendersContractInterfaces(t *testing.T) {
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

func TestPlanningResetDryRunDoesNotMutate(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Dry Epic"))
	id := epic["id"].(string)
	stages := []string{"scan", "rri", "vision", "blueprint", "contracts", "task_graph"}
	for _, stage := range stages {
		content := stage + " content"
		if stage == "vision" {
			content = validVisionArtifact
		}
		if stage == "blueprint" {
			content = validBlueprintArtifact
		}
		if stage == "contracts" {
			content = validContractArtifact
		}
		if stage == "task_graph" {
			content = `{"version":3,"execution_policy":"strict_sequential","nodes":[{"key":"F01","type":"feature","name":"Area","requirement_keys":[],"depends_on":[]}]}`
		}
		runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, content)
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, "current", decision)
	}
	runPic(t, bin, root, home, "work-item", "materialize", id)
	status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["next_stage"] != "authorize" {
		t.Fatalf("pre-dry-run status = %#v", status)
	}

	// Seed descendant-owned records: the reset deletes child Work Items, and
	// foreign-key cascades retire everything they own, so the preview must
	// enumerate those cascaded targets too.
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	childDB, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var childID string
	if err := childDB.QueryRow(`SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND work_item_id<>?`, id, id).Scan(&childID); err != nil {
		t.Fatal(err)
	}
	childDB.Close()
	runSQLite(t, dbPath, `
		INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES('wia-child','`+childID+`','scan',1,'<scan/>','h-child');
		INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-child','`+childID+`','scan','wia-child',1,'h-child','accepted');
		INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at) VALUES('pr-child','`+childID+`','worker',1,'failed','lease-child','2026-01-01');
		INSERT INTO work_item_completion_reports(id,work_item_id,pipeline_run_id,instruction_pack_id,instruction_pack_version,instruction_pack_hash,status) VALUES('wicr-child','`+childID+`','pr-child','wip-child',1,'h-child-pack','done');
		INSERT INTO work_item_verification_reports(id,work_item_id,checkpoint_id,completion_report_id,status,summary,verified_by_role) VALUES('wivr-child','`+childID+`','','wicr-child','passed','child verified','contractor');
		INSERT INTO work_item_labels(work_item_id,label) VALUES('`+childID+`','area:core');
		INSERT INTO work_item_dependencies(id,work_item_id,depends_on_work_item_id) VALUES('wid-child','`+childID+`','`+id+`');
		INSERT INTO work_item_gates(id,work_item_id,gate_work_item_id) VALUES('wig-child','`+childID+`','`+id+`');
		INSERT INTO work_item_relations(id,work_item_id,relation_type,related_work_item_id) VALUES('wir-child','`+childID+`','blocks','`+id+`');
		INSERT INTO implementation_authorizations(id,work_item_id,task_graph_checkpoint_id,authorized_by) VALUES('wimpl-child','`+childID+`','wic-child','owner');
		INSERT INTO work_item_escalations(id,work_item_id,pipeline_run_id,instruction_pack_id,instruction_pack_version,instruction_pack_hash,level,status,report_json) VALUES('wiem-child','`+childID+`','pr-child','wip-child',1,'h-child-pack','L2','open','{}');
		INSERT INTO work_item_owner_decisions(id,work_item_id,completion_report_id,decision) VALUES('wiod-child','`+childID+`','wicr-child','rejected');
		INSERT INTO work_item_aggregate_owner_decisions(id,work_item_id,verification_report_id,decision) VALUES('waod-child','`+childID+`','wivr-child','accepted');
		INSERT INTO work_item_delivery_states(work_item_id,integration_mode) VALUES('`+childID+`','branch');
		INSERT INTO work_item_events(id,work_item_id,event_type,summary) VALUES('wiev-child','`+childID+`','note','seeded');
		INSERT INTO work_item_profiles(id,work_item_id,profile_name,profile_version,planning_depth,stages_json,content_hash) VALUES('wiprof-child','`+childID+`','qa',99,'full','[]','h-child-profile');
		INSERT INTO work_items(id,type,title) VALUES('wi-bug-child','bug','Child Bug');
		INSERT INTO work_item_corrective_bugs(verification_report_id,bug_work_item_id,owner_approval_required) VALUES('wivr-child','wi-bug-child',1);
		INSERT INTO work_items(id,type,title) VALUES('wi-grandchild','task','Grandchild Task');
		INSERT INTO work_item_materializations(root_work_item_id,checkpoint_id,node_key,work_item_id) VALUES('`+childID+`','wic-child','G01','wi-grandchild');`)

	// The reset retires every materialization row rooted at the target (its own
	// cleanup) plus rows rooted at a materialized descendant (nested root,
	// retired by the work_items cascade). Capture that exact set before the dry
	// run so the preview can be compared against it.
	matDB, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var expectedMaterializationRows int
	if err := matDB.QueryRow(`SELECT COUNT(*) FROM work_item_materializations
		WHERE root_work_item_id=?
		OR root_work_item_id IN (SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND work_item_id<>?)`, id, id, id).Scan(&expectedMaterializationRows); err != nil {
		t.Fatal(err)
	}
	matDB.Close()
	if expectedMaterializationRows < 2 {
		t.Fatalf("expected the child and grandchild materialization rows, got %d", expectedMaterializationRows)
	}

	dry := asObject(t, runPic(t, bin, root, home, "work-item", "planning-reset", id, "owner", "--dry-run"))
	if dry["dry_run"] != true || dry["retired_materializations"] != float64(1) {
		t.Fatalf("dry run = %#v", dry)
	}
	descendantArtifacts := dry["descendant_artifacts"].([]any)
	if len(descendantArtifacts) != 1 || asObject(t, descendantArtifacts[0])["id"] != "wia-child" {
		t.Fatalf("descendant artifacts = %#v", descendantArtifacts)
	}
	descendantCheckpoints := dry["descendant_checkpoints"].([]any)
	if len(descendantCheckpoints) != 1 || asObject(t, descendantCheckpoints[0])["id"] != "wic-child" {
		t.Fatalf("descendant checkpoints = %#v", descendantCheckpoints)
	}
	descendantCompletions := dry["descendant_completion_reports"].([]any)
	if len(descendantCompletions) != 1 || asObject(t, descendantCompletions[0])["id"] != "wicr-child" {
		t.Fatalf("descendant completion reports = %#v", descendantCompletions)
	}
	descendantVerifications := dry["descendant_verification_reports"].([]any)
	if len(descendantVerifications) != 1 || asObject(t, descendantVerifications[0])["id"] != "wivr-child" {
		t.Fatalf("descendant verification reports = %#v", descendantVerifications)
	}
	// Every child-owned cascade table the reset retires with the descendants is
	// previewed by name, each row carrying its owning Work Item.
	descendantCascadeExpectations := []struct {
		key, field, value string
	}{
		{"descendant_pipeline_runs", "id", "pr-child"},
		{"descendant_labels", "label", "area:core"},
		{"descendant_dependencies", "id", "wid-child"},
		{"descendant_gates", "id", "wig-child"},
		{"descendant_relations", "id", "wir-child"},
		{"descendant_authorizations", "id", "wimpl-child"},
		{"descendant_escalations", "id", "wiem-child"},
		{"descendant_owner_decisions", "id", "wiod-child"},
		{"descendant_aggregate_decisions", "id", "waod-child"},
		{"descendant_delivery_states", "integration_mode", "branch"},
		{"descendant_events", "id", "wiev-child"},
		{"descendant_profiles", "profile_name", "qa"},
	}
	for _, expected := range descendantCascadeExpectations {
		entries, ok := dry[expected.key].([]any)
		if !ok || len(entries) != 1 {
			t.Fatalf("dry-run %s = %#v", expected.key, dry[expected.key])
		}
		entry := asObject(t, entries[0])
		if fmt.Sprint(entry[expected.field]) != expected.value || fmt.Sprint(entry["work_item_id"]) != childID {
			t.Fatalf("dry-run %s entry = %#v, want %s=%s owned by %s", expected.key, entry, expected.field, expected.value, childID)
		}
	}
	// Materialization rows: the preview must enumerate exactly the set the reset
	// retires — the target-rooted rows its own cleanup deletes (including the
	// target's own node, which the dependents list excludes) plus rows rooted at
	// a materialized descendant, which die through the work_items cascade.
	materializationRows, matOK := dry["retired_materialization_rows"].([]any)
	if !matOK || len(materializationRows) != expectedMaterializationRows {
		t.Fatalf("retired materialization rows = %#v, want %d", dry["retired_materialization_rows"], expectedMaterializationRows)
	}
	epicRooted := false
	grandchildRooted := false
	for _, entry := range materializationRows {
		row := asObject(t, entry)
		if row["root_work_item_id"] == id && row["work_item_id"] == childID {
			epicRooted = true
		}
		if row["root_work_item_id"] == childID && row["work_item_id"] == "wi-grandchild" && row["node_key"] == "G01" {
			grandchildRooted = true
		}
	}
	if !epicRooted || !grandchildRooted {
		t.Fatalf("retired materialization rows missing the seeded targets: %#v", materializationRows)
	}
	// Corrective bugs are second-order targets: the seeded row retires with the
	// child's verification report, while the bug Work Item itself survives.
	correctiveEntries, ok := dry["descendant_corrective_bugs"].([]any)
	if !ok || len(correctiveEntries) != 1 {
		t.Fatalf("descendant corrective bugs = %#v", dry["descendant_corrective_bugs"])
	}
	corrective := asObject(t, correctiveEntries[0])
	if corrective["verification_report_id"] != "wivr-child" || corrective["bug_work_item_id"] != "wi-bug-child" {
		t.Fatalf("descendant corrective bugs entry = %#v", corrective)
	}
	verifyDB, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var seededChildArtifact int
	if err := verifyDB.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE id='wia-child'`).Scan(&seededChildArtifact); err != nil || seededChildArtifact != 1 {
		t.Fatalf("dry run mutated descendant rows: count=%d err=%v", seededChildArtifact, err)
	}
	var seededChildRuns int
	if err := verifyDB.QueryRow(`SELECT COUNT(*) FROM pipeline_runs WHERE id='pr-child'`).Scan(&seededChildRuns); err != nil || seededChildRuns != 1 {
		t.Fatalf("dry run mutated descendant pipeline runs: count=%d err=%v", seededChildRuns, err)
	}
	verifyDB.Close()
	if dry["checkpoints"] != float64(6) {
		t.Fatalf("dry run checkpoints = %#v", dry)
	}
	// The preview must name the exact invalidation targets, not just counts.
	artifactStages := map[string]bool{}
	for _, entry := range dry["artifacts"].([]any) {
		artifact := asObject(t, entry)
		artifactStages[fmt.Sprint(artifact["stage"])] = true
		if artifact["content_hash"] == "" || artifact["revision"] == nil {
			t.Fatalf("dry-run artifact entry missing revision/hash: %#v", artifact)
		}
	}
	for _, stage := range stages {
		if !artifactStages[stage] {
			t.Fatalf("dry-run artifacts missing stage %s: %v", stage, artifactStages)
		}
	}
	checkpointStages := map[string]bool{}
	for _, entry := range dry["checkpoints_list"].([]any) {
		checkpointStages[fmt.Sprint(asObject(t, entry)["stage"])] = true
	}
	if len(checkpointStages) != len(stages) {
		t.Fatalf("dry-run checkpoint list = %v", checkpointStages)
	}
	var tipCount int
	for _, entry := range dry["instruction_packs"].([]any) {
		pack := asObject(t, entry)
		if pack["content_hash"] == "" || pack["version"] == nil {
			t.Fatalf("dry-run pack entry missing lineage: %#v", pack)
		}
		tipCount++
	}
	_ = tipCount
	var dependentID string
	for _, entry := range dry["dependents"].([]any) {
		dependentID = fmt.Sprint(asObject(t, entry)["work_item_id"])
	}
	if dependentID == "" {
		t.Fatalf("dry-run dependents empty: %#v", dry["dependents"])
	}
	status = asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["next_stage"] != "authorize" {
		t.Fatalf("dry run mutated workflow state: %#v", status)
	}

	runPic(t, bin, root, home, "work-item", "planning-reset", id, "owner")
	status = asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["next_stage"] != "scan" {
		t.Fatalf("post-reset status = %#v", status)
	}
	// The actual reset must retire exactly what the preview named: every seeded
	// cascade row dies with the child, and the bug Work Item survives (it is not
	// a materialized descendant).
	verifyDB, err = openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer verifyDB.Close()
	resetChecks := []struct {
		query string
		want  int
	}{
		{`SELECT COUNT(*) FROM work_items WHERE id='` + childID + `'`, 0},
		{`SELECT COUNT(*) FROM work_items WHERE id='wi-bug-child'`, 1},
		{`SELECT COUNT(*) FROM work_items WHERE id='wi-grandchild'`, 1},
		{`SELECT COUNT(*) FROM pipeline_runs WHERE id='pr-child'`, 0},
		{`SELECT COUNT(*) FROM work_item_materializations WHERE root_work_item_id='` + id + `'`, 0},
		{`SELECT COUNT(*) FROM work_item_materializations WHERE root_work_item_id='` + childID + `'`, 0},
		{`SELECT COUNT(*) FROM work_item_artifacts WHERE id='wia-child'`, 0},
		{`SELECT COUNT(*) FROM workflow_checkpoints WHERE id='wic-child'`, 0},
		{`SELECT COUNT(*) FROM work_item_completion_reports WHERE id='wicr-child'`, 0},
		{`SELECT COUNT(*) FROM work_item_verification_reports WHERE id='wivr-child'`, 0},
		{`SELECT COUNT(*) FROM work_item_escalations WHERE id='wiem-child'`, 0},
		{`SELECT COUNT(*) FROM work_item_owner_decisions WHERE id='wiod-child'`, 0},
		{`SELECT COUNT(*) FROM work_item_aggregate_owner_decisions WHERE id='waod-child'`, 0},
		{`SELECT COUNT(*) FROM work_item_events WHERE id='wiev-child'`, 0},
		{`SELECT COUNT(*) FROM work_item_profiles WHERE id='wiprof-child'`, 0},
		{`SELECT COUNT(*) FROM work_item_labels WHERE label='area:core'`, 0},
		{`SELECT COUNT(*) FROM work_item_dependencies WHERE id='wid-child'`, 0},
		{`SELECT COUNT(*) FROM work_item_gates WHERE id='wig-child'`, 0},
		{`SELECT COUNT(*) FROM work_item_relations WHERE id='wir-child'`, 0},
		{`SELECT COUNT(*) FROM implementation_authorizations WHERE id='wimpl-child'`, 0},
		{`SELECT COUNT(*) FROM work_item_delivery_states WHERE work_item_id='` + childID + `'`, 0},
		{`SELECT COUNT(*) FROM work_item_corrective_bugs WHERE verification_report_id='wivr-child'`, 0},
	}
	for _, check := range resetChecks {
		var count int
		if err := verifyDB.QueryRow(check.query).Scan(&count); err != nil || count != check.want {
			t.Fatalf("post-reset cascade check failed: %s => %d (want %d) err=%v", check.query, count, check.want, err)
		}
	}
}

func TestSchemaMigrationsVersioned(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := openSQLite(dbPath)
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
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	if again := versions(); strings.Join(again, ",") != strings.Join(fresh, ",") {
		t.Fatalf("second open changed recorded migrations: %v -> %v", fresh, again)
	}
	db.Close()

	legacyPath := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := openSQLite(legacyPath)
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
	if err := initDB(legacyPath); err != nil {
		t.Fatal(err)
	}
	db, err = openSQLite(legacyPath)
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
	tasksOnly := filepath.Join(t.TempDir(), "tasks.db")
	db, err := openSQLite(tasksOnly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE tasks(id TEXT PRIMARY KEY, epic_id TEXT, title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT DEFAULT 'open', priority TEXT DEFAULT 'medium', created_at TEXT DEFAULT (datetime('now')));
		INSERT INTO tasks(id,epic_id,title) VALUES('t-part','e-missing','Orphan Task')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := initDB(tasksOnly); err != nil {
		t.Fatalf("tasks-only database failed to migrate: %v", err)
	}
	db, err = openSQLite(tasksOnly)
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
	db, err = openSQLite(epicsOnly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE epics(id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT DEFAULT 'open', created_at TEXT DEFAULT (datetime('now')));
		INSERT INTO epics(id,title) VALUES('e-part','Lone Epic')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := initDB(epicsOnly); err != nil {
		t.Fatalf("epics-only database failed to migrate: %v", err)
	}
	db, err = openSQLite(epicsOnly)
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
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := openSQLite(dbPath)
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
	db, err = openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// The runner creates the version table before applying any step.
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT DEFAULT (datetime('now')))`); err != nil {
		t.Fatal(err)
	}
	poison := schemaMigration{version: 99, name: "poison_reconcile", apply: func(db schemaDB) error {
		if err := reconcileLegacySchema(db); err != nil {
			return err
		}
		return errors.New("injected failure after reconcile operations")
	}}
	if err := applySchemaMigration(context.Background(), db, poison); err == nil || !strings.Contains(err.Error(), "injected failure") {
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
	db, err = openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ddlPoison := schemaMigration{version: 98, name: "poison_ddl", apply: func(db schemaDB) error {
		if _, err := db.Exec(`CREATE TABLE zz_poison (id TEXT)`); err != nil {
			return err
		}
		return errors.New("injected DDL failure")
	}}
	if err := applySchemaMigration(context.Background(), db, ddlPoison); err == nil || !strings.Contains(err.Error(), "injected DDL failure") {
		t.Fatalf("ddl poison error = %v", err)
	}
	var poisonTable int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='zz_poison'`).Scan(&poisonTable); err != nil || poisonTable != 0 {
		t.Fatalf("DDL did not roll back: zz_poison tables=%d", poisonTable)
	}
	db.Close()

	// Retry after the failures: the real migration completes and migrates rows.
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err = openSQLite(dbPath)
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
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := openSQLite(dbPath)
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
	probe := schemaMigration{version: 97, name: "pragma_affinity_probe", apply: func(db schemaDB) error {
		if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
			return err
		}
		return db.QueryRow(`PRAGMA legacy_alter_table`).Scan(&legacyAlterTable)
	}}
	if err := applySchemaMigration(context.Background(), db, probe); err != nil {
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

// v2TaskGraph builds the policy-v2 happy-path graph bound to the given approved
// Contract lineage via source_contract.
func v2TaskGraph(contractArtifactID string, contractRevision int, contractContentHash string) string {
	return fmt.Sprintf(`{"version":3,"execution_policy":"strict_sequential","decomposition_policy_version":2,"source_contract":{"artifact_id":%q,"revision":%d,"content_hash":%q},"nodes":[`+
		`{"key":"F01","type":"feature","name":"Area","requirement_keys":[],"depends_on":[]},`+
		`{"key":"S01","type":"task","name":"Shared schema","parent_key":"F01","goal":"Widen the schema contract","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"seam":"cli-materialize","obligation_keys":["OB-003"],"command":"go test ./cmd/pic -run TestMaterialize","expected":"shared schema contract holds"}],"skillFamilies":[],"decomposition_mode":"shared_contract","exception_reason":"widens the schema consumed by CLI, scheduler, and dashboard","provides":["OB-003"],"consumes":[],"evidence_for":[],"obligation_keys":["OB-003"]},`+
		`{"key":"W01","type":"task","name":"Widen","parent_key":"F01","goal":"Expand the covered surface","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["w.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["w.go"]},"verification":[{"seam":"go-tests","requirement_keys":["REQ-001"],"command":"go test ./cmd/pic -run TestWiden","expected":"expanded surface covered"}],"skillFamilies":[],"decomposition_mode":"wide_refactor","exception_reason":"touches every call site before the contract stabilizes","paired_contract_node":"P01","provides":[],"consumes":[],"evidence_for":[],"obligation_keys":[]},`+
		`{"key":"T01","type":"task","name":"Implement","parent_key":"F01","goal":"Implement requirement","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"seam":"go-tests","requirement_keys":["REQ-001"],"command":"go test ./...","expected":"requirement behavior proven"}],"skillFamilies":[],"provides":["OB-001"],"consumes":[],"evidence_for":["OB-001"],"obligation_keys":["OB-001"]},`+
		`{"key":"T02","type":"bug","name":"Consume","parent_key":"F01","goal":"Consume shared schema","requirement_keys":["REQ-002"],"depends_on":["S01"],"priority":"P0","module":"core","files":["y.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["y.go"]},"verification":[{"seam":"cli-materialize","obligation_keys":["OB-002"],"command":"go test ./cmd/pic -run TestDelivery","expected":"delivery verified"}],"skillFamilies":[],"acceptance":"Given an approved graph\nWhen materialization runs\nThen projections commit atomically","provides":["OB-002"],"consumes":["OB-003"],"evidence_for":["OB-002","OB-003"],"obligation_keys":["OB-002","OB-003"],"depends_on_rationale":{"S01":"consumes the persisted schema contract S01 establishes"}},`+
		`{"key":"P01","type":"task","name":"Contract cleanup","parent_key":"F01","goal":"Contract the widened surface","requirement_keys":["REQ-001"],"depends_on":["W01"],"priority":"P1","module":"core","files":["w.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["w.go"]},"verification":[{"seam":"go-tests","requirement_keys":["REQ-001"],"command":"go test ./cmd/pic -run TestCleanup","expected":"cleanup verified"}],"skillFamilies":[],"provides":[],"consumes":[],"evidence_for":[],"obligation_keys":[],"depends_on_rationale":{"W01":"contracts the expansion W01 performs"}},`+
		`{"key":"G01","type":"gate","name":"Integration gate","goal":"Verify the integrated delivery","requirement_keys":[],"depends_on":[],"decomposition_mode":"integration_gate","exception_reason":"verifies the integrated aggregate at the highest seam","obligation_keys":["OB-002"],"verification":[{"seam":"cli-materialize","obligation_keys":["OB-002"],"command":"go test ./cmd/pic -run TestDelivery","expected":"integrated delivery verified"}]}`+
		`]}`, contractArtifactID, contractRevision, contractContentHash)
}

// v2StandaloneGraph is a policy-v1 standalone graph: the standalone planning
// profile carries no Blueprint/Contract predecessors, so policy v1 applies.
const v2StandaloneGraph = `{"version":3,"execution_policy":"strict_sequential","nodes":[{"key":"IMPL","type":"task","name":"Standalone","goal":"Implement standalone","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]}]}`

// v2StandaloneGraphV2Policy is a policy-v2 standalone graph with a well-formed
// but fake source_contract; saving it must fail closed because the standalone
// planning profile has no approved Contract to bind.
const v2StandaloneGraphV2Policy = `{"version":3,"execution_policy":"strict_sequential","decomposition_policy_version":2,"source_contract":{"artifact_id":"wia-phantom","revision":1,"content_hash":"sha256:phantom"},"nodes":[{"key":"IMPL","type":"task","name":"Standalone","goal":"Implement standalone","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"seam":"go-tests","requirement_keys":["REQ-001"],"command":"go test ./...","expected":"behavior proven"}],"skillFamilies":[]}]}`

func v2ContractArtifact(artifactID string, revision int, contentHash string) string {
	return fmt.Sprintf(`{"decomposition_policy_version":2,"obligation_schema_version":2,"project_name":"Task System","source_blueprint":{"artifact_id":%q,"revision":%d,"content_hash":%q},"deliverables":[{"item":"Lifecycle","details":"Persisted workflow","requirements":["REQ-001"]},{"item":"Delivery","details":"Verified delivery","requirements":["REQ-002"]}],"obligations":[{"id":"OB-001","requirement_keys":["REQ-001"],"behavior":"Persist workflow state","acceptance":"Given a valid workflow\nWhen it is persisted\nThen the state is queryable","class":"data_invariant","seam":"cli-materialize"},{"id":"OB-002","requirement_keys":["REQ-002"],"behavior":"Verify delivery","acceptance":"Given verified work\nWhen the owner reviews it\nThen the decision is recorded","class":"user_behavior","seam":"cli-materialize"},{"id":"OB-003","requirement_keys":["REQ-001"],"behavior":"Shared schema contract","acceptance":"Given a consumer node\nWhen it consumes the shared schema\nThen the provider contract holds","class":"interface_contract","seam":"cli-materialize"}],"tech_stack":[{"layer":"Backend","choice":"Go","rationale":"Existing stack"}],"task_graph_summary":{"tip_count":6,"estimated_minutes":240},"not_included":["Legacy migration"]}`, artifactID, revision, contentHash)
}

// approveV2Blueprint drives a full-depth Work Item through scan, rri, vision,
// and a policy-v2 Blueprint approval, returning the approved Blueprint row.
func approveV2Blueprint(t *testing.T, bin, root, home, id string) map[string]any {
	t.Helper()
	for _, stage := range []string{"scan", "rri", "vision"} {
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, planningArtifactContent(stage)))
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
	}
	blueprint := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "blueprint", v2BlueprintArtifact))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "blueprint", blueprint["id"].(string), "approved")
	return blueprint
}

// seedV2Requirements adds the two Gherkin-backed requirements the v2 fixtures
// reference, directly in the store like the other graph tests.
func seedV2Requirements(t *testing.T, dbPath, epicID string) {
	t.Helper()
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-v2a-`+epicID+`','`+epicID+`','REQ-001','Required','Given valid context
When work runs
Then it completes')`)
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-v2b-`+epicID+`','`+epicID+`','REQ-002','Delivery','Given verified work
When the owner reviews it
Then the decision is recorded')`)
}

func TestBlueprintArtifactPolicySchemas(t *testing.T) {
	if err := validateBlueprintReport(validBlueprintArtifact); err != nil {
		t.Fatalf("v1 blueprint fixture must validate unchanged: %v", err)
	}
	if err := validateBlueprintReport(v2BlueprintArtifact); err != nil {
		t.Fatalf("v2 blueprint fixture must validate: %v", err)
	}
	noSeams := strings.Replace(v2BlueprintArtifact, `"verification_seams"`, `"declared_seams"`, 1)
	if err := validateBlueprintReport(noSeams); err == nil || !strings.Contains(err.Error(), "at least one verification seam") {
		t.Fatalf("v2 blueprint without seams = %v", err)
	}
	duplicate := strings.Replace(v2BlueprintArtifact, `{"id":"go-tests"`, `{"id":"cli-materialize"`, 1)
	if err := validateBlueprintReport(duplicate); err == nil || !strings.Contains(err.Error(), "unique non-empty ids") {
		t.Fatalf("v2 blueprint duplicate seam = %v", err)
	}
	emptySurface := strings.Replace(v2BlueprintArtifact, `"surface":"go test ./... in the repository"`, `"surface":""`, 1)
	if err := validateBlueprintReport(emptySurface); err == nil || !strings.Contains(err.Error(), "surface and isolates") {
		t.Fatalf("v2 blueprint empty seam surface = %v", err)
	}
	v1WithoutPreview := strings.Replace(validBlueprintArtifact, `"task_decomposition_preview"`, `"retired_preview"`, 1)
	if err := validateBlueprintReport(v1WithoutPreview); err == nil || !strings.Contains(err.Error(), "task preview") {
		t.Fatalf("v1 blueprint without preview = %v", err)
	}
	if err := validateBlueprintReport(strings.Replace(v2BlueprintArtifact, `"decomposition_policy_version":2`, `"decomposition_policy_version":3`, 1)); err == nil || !strings.Contains(err.Error(), "unsupported decomposition_policy_version 3") {
		t.Fatalf("unsupported policy version = %v", err)
	}
	// The additive schema_version 2.1 marker gates excluded_keys on top of
	// policy v2; the decomposition_policy_version field itself stays 2.
	if err := validateBlueprintReport(strings.Replace(v2BlueprintArtifact, `"decomposition_policy_version":2`, `"decomposition_policy_version":2,"schema_version":2.1,"implementation_decisions":`+v21BlueprintDecisions+`,"deferrals":[],"not_yet_specified":[],"out_of_scope":[],"adr_candidates":[]`, 1)); err != nil {
		t.Fatalf("v2 blueprint with v2.1 marker and no excluded_keys must validate: %v", err)
	}
	// The marker is a shape commitment: a marked artifact without the required
	// implementation_decisions section is rejected, mirroring TS.
	if err := validateBlueprintReport(strings.Replace(v2BlueprintArtifact, `"decomposition_policy_version":2`, `"decomposition_policy_version":2,"schema_version":2.1`, 1)); err == nil || !strings.Contains(err.Error(), "implementation_decisions must be an array") {
		t.Fatalf("v2.1 blueprint without implementation_decisions = %v", err)
	}
	// An empty implementation_decisions array is equally rejected.
	if err := validateBlueprintReport(strings.Replace(v2BlueprintArtifact, `"decomposition_policy_version":2`, `"decomposition_policy_version":2,"schema_version":2.1,"implementation_decisions":[],"deferrals":[],"not_yet_specified":[],"out_of_scope":[],"adr_candidates":[]`, 1)); err == nil || !strings.Contains(err.Error(), "implementation_decisions must be a non-empty array") {
		t.Fatalf("v2.1 blueprint with empty implementation_decisions = %v", err)
	}
	if err := validateBlueprintReport(v21BlueprintWithExcludedKeys("Cloud sync", "")); err == nil || !strings.Contains(err.Error(), "excluded_keys require non-empty keys") {
		t.Fatalf("v2.1 blueprint with empty excluded key = %v", err)
	}
}

// v21BlueprintDecisions is the well-shaped implementation_decisions every
// v2.1-marked fixture carries, since the marker commits the artifact to all
// v2.1 sections (Go/TS shape parity, wi-dbeba706).
const v21BlueprintDecisions = `[{"decision":"Mirror TS v2.1 shape validation","rationale":"Dual-enforcer parity","alternatives_considered":["Owner deferral","TS-only guard"]}]`

// v21BlueprintFull builds a v2.1-marked blueprint carrying every v2.1 section
// with the given raw implementation_decisions JSON, so shape variants can be
// probed against the Go validator (corrective bug wi-dbeba706).
func v21BlueprintFull(decisions string) string {
	extra := `"schema_version":2.1,` +
		`"implementation_decisions":` + decisions + `,` +
		`"adr_candidates":[{"context":"Shape parity","choice":"Mirror TS shape rules","reason":"Canonical saves must reject the same malformed sections"}],` +
		`"excluded_keys":["Cloud sync"],` +
		`"deferrals":[{"question":"SQ-1","resolution":"Owner decision at Blueprint"}],` +
		`"not_yet_specified":[],` +
		`"out_of_scope":[{"exclusion":"Cloud sync","reason":"Outside epic scope"}]`
	return strings.Replace(v2BlueprintArtifact, `"decomposition_policy_version":2`, `"decomposition_policy_version":2,`+extra, 1)
}

func TestBlueprintV21ShapeParity(t *testing.T) {
	validDecisions := `[{"decision":"Mirror TS v2.1 shape validation","rationale":"Dual-enforcer parity","alternatives_considered":["Owner deferral","TS-only guard"]}]`
	if err := validateBlueprintReport(v21BlueprintFull(validDecisions)); err != nil {
		t.Fatalf("complete v2.1 blueprint must validate: %v", err)
	}
	if err := validateBlueprintReport(v2BlueprintArtifact); err != nil {
		t.Fatalf("legacy markerless v2 blueprint must keep validating: %v", err)
	}
	cases := []struct {
		name      string
		mutate    func(content string) string
		wantError string
	}{
		{"empty decisions", func(c string) string { return strings.Replace(c, validDecisions, "[]", 1) }, "implementation_decisions must be a non-empty array"},
		{"missing decisions", func(c string) string {
			return strings.Replace(c, `"implementation_decisions":`+validDecisions+`,`, "", 1)
		}, "implementation_decisions must be an array"},
		{"null decisions", func(c string) string { return strings.Replace(c, validDecisions, "null", 1) }, "implementation_decisions must be an array"},
		{"non-object decision row", func(c string) string { return strings.Replace(c, validDecisions, `[["decision"]]`, 1) }, "implementation_decisions rows must be objects"},
		{"empty decision", func(c string) string {
			return strings.Replace(c, `"decision":"Mirror TS v2.1 shape validation"`, `"decision":""`, 1)
		}, "implementation_decisions.decision must be a non-empty string"},
		{"missing rationale", func(c string) string { return strings.Replace(c, `"rationale":"Dual-enforcer parity",`, "", 1) }, "implementation_decisions.rationale must be a non-empty string"},
		{"empty alternatives", func(c string) string { return strings.Replace(c, `["Owner deferral","TS-only guard"]`, "[]", 1) }, "alternatives_considered must be a non-empty array of non-empty strings"},
		{"non-string alternative", func(c string) string { return strings.Replace(c, `["Owner deferral","TS-only guard"]`, `["a",3]`, 1) }, "alternatives_considered must be a non-empty array of non-empty strings"},
		{"missing out_of_scope", func(c string) string {
			return strings.Replace(c, `"out_of_scope":[{"exclusion":"Cloud sync","reason":"Outside epic scope"}],`, "", 1)
		}, "out_of_scope must be an array"},
		{"null out_of_scope", func(c string) string {
			return strings.Replace(c, `"out_of_scope":[{"exclusion":"Cloud sync","reason":"Outside epic scope"}]`, `"out_of_scope":null`, 1)
		}, "out_of_scope must be an array"},
		{"string schema marker", func(c string) string { return strings.Replace(c, `"schema_version":2.1`, `"schema_version":"2.1"`, 1) }, "schema_version must be the numeric marker 2.1"},
		{"null schema marker", func(c string) string { return strings.Replace(c, `"schema_version":2.1`, `"schema_version":null`, 1) }, "schema_version must be the numeric marker 2.1"},
		{"deferral row missing resolution", func(c string) string {
			return strings.Replace(c, `"resolution":"Owner decision at Blueprint"`, `"resolution":" "`, 1)
		}, "deferrals.resolution must be a non-empty string"},
		{"adr row missing reason", func(c string) string {
			return strings.Replace(c, `"reason":"Canonical saves must reject the same malformed sections"`, `"reason":" "`, 1)
		}, "adr_candidates.reason must be a non-empty string"},
		{"out_of_scope row empty exclusion", func(c string) string { return strings.Replace(c, `"exclusion":"Cloud sync"`, `"exclusion":" "`, 1) }, "out_of_scope.exclusion must be a non-empty string"},
		{"not_yet_specified row empty graduation path", func(c string) string {
			return strings.Replace(c, `"not_yet_specified":[]`, `"not_yet_specified":[{"uncertainty":"u","graduation_path":" "}]`, 1)
		}, "not_yet_specified.graduation_path must be a non-empty string"},
	}
	base := v21BlueprintFull(validDecisions)
	for _, tc := range cases {
		err := validateBlueprintReport(tc.mutate(base))
		if err == nil || !strings.Contains(err.Error(), tc.wantError) {
			t.Fatalf("%s: err = %v, want containing %q", tc.name, err, tc.wantError)
		}
	}
}

// v21BlueprintWithExcludedKeys splices the additive schema_version 2.1 marker,
// the required implementation_decisions, and excluded_keys into the policy-v2
// fixture, matching the production marker shape where
// decomposition_policy_version stays 2.
func v21BlueprintWithExcludedKeys(keys ...string) string {
	emcoded, _ := json.Marshal(keys)
	scope := `"deferrals":[],"not_yet_specified":[],"out_of_scope":[],"adr_candidates":[]`
	return strings.Replace(v2BlueprintArtifact, `"decomposition_policy_version":2`, `"decomposition_policy_version":2,"schema_version":2.1,"implementation_decisions":`+v21BlueprintDecisions+`,"excluded_keys":`+string(emcoded)+`,`+scope, 1)
}

func TestBlueprintExcludedKeysBinding(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	id := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Excluded Keys Epic"))["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")

	// Build the approved RRI referent: scan accepted, RRI approved with
	// out_of_scope rows, vision approved.
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	rri := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "rri", `{"out_of_scope":[{"exclusion":"Cloud sync","reason":"Outside the epic scope"},{"exclusion":"Legacy import","reason":"Deferred to a later epic"}]}`))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "rri", rri["id"].(string), "approved")
	vision := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "vision", validVisionArtifact))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "vision", vision["id"].(string), "approved")

	blueprintRows := func(t *testing.T) int {
		t.Helper()
		db, err := openSQLite(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var count int
		if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=? AND stage='blueprint'`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	// Known RRI exclusion keys save and persist.
	known := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "blueprint", v21BlueprintWithExcludedKeys("Cloud sync", "Legacy import")))
	if known["revision"] != float64(1) {
		t.Fatalf("first v2.1 blueprint revision = %#v", known["revision"])
	}

	// Malformed v2.1 implementation_decisions fail closed with a field error
	// and leave no new artifact row (Go/TS shape parity, wi-dbeba706).
	malformed := strings.Replace(v21BlueprintWithExcludedKeys("Cloud sync"), v21BlueprintDecisions, "[]", 1)
	if out := runPicError(t, bin, root, home, "work-item", "artifact-save", id, "blueprint", malformed); !strings.Contains(out, "implementation_decisions must be a non-empty array") {
		t.Fatalf("malformed implementation_decisions save = %q", out)
	}
	if rows := blueprintRows(t); rows != 1 {
		t.Fatalf("blueprint rows after malformed save = %d, want 1", rows)
	}

	// Unknown exclusion keys fail closed naming the key and leave no new row.
	if out := runPicError(t, bin, root, home, "work-item", "artifact-save", id, "blueprint", v21BlueprintWithExcludedKeys("Cloud sync", "Phantom export")); !strings.Contains(out, "Phantom export") {
		t.Fatalf("dangling excluded key error = %s", out)
	}
	if rows := blueprintRows(t); rows != 1 {
		t.Fatalf("failed save persisted %d blueprint rows, want 1", rows)
	}

	// Legacy policy-v2 content without v2.1 fields still saves unchanged.
	legacy := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "blueprint", v2BlueprintArtifact))
	if legacy["revision"] != float64(2) {
		t.Fatalf("legacy v2 blueprint revision = %#v", legacy["revision"])
	}

	// Without an approved RRI referent, nonempty excluded_keys fail closed.
	orphan := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Orphan Exclusions Epic"))
	orphanID := orphan["id"].(string)
	if out := runPicError(t, bin, root, home, "work-item", "artifact-save", orphanID, "blueprint", v21BlueprintWithExcludedKeys("Cloud sync")); !strings.Contains(out, "approved RRI out_of_scope referent") {
		t.Fatalf("missing referent error = %s", out)
	}

	// Stale-predecessor lifecycle: a Blueprint saved against an older RRI must
	// fail closed at approval after a newer RRI retires the exclusion key.
	stale := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Stale Predecessor Epic"))
	staleID := stale["id"].(string)
	staleScan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", staleID, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", staleID, "scan", staleScan["id"].(string), "accepted")
	rriV1 := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", staleID, "rri", `{"out_of_scope":[{"exclusion":"Cloud sync","reason":"Outside the epic scope"}]}`))
	runPic(t, bin, root, home, "work-item", "artifact-approve", staleID, "rri", rriV1["id"].(string), "approved")
	staleVision := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", staleID, "vision", validVisionArtifact))
	runPic(t, bin, root, home, "work-item", "artifact-approve", staleID, "vision", staleVision["id"].(string), "approved")
	staleBlueprint := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", staleID, "blueprint", v21BlueprintWithExcludedKeys("Cloud sync")))

	// Approving a newer RRI drops the vision and blueprint checkpoints but
	// leaves the artifacts; the unchanged Blueprint must not re-approve.
	rriV2 := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", staleID, "rri", `{"out_of_scope":[{"exclusion":"Local cache","reason":"Deferred to a later epic"}]}`))
	runPic(t, bin, root, home, "work-item", "artifact-approve", staleID, "rri", rriV2["id"].(string), "approved")
	runPic(t, bin, root, home, "work-item", "artifact-approve", staleID, "vision", "current", "approved")
	if out := runPicError(t, bin, root, home, "work-item", "artifact-approve", staleID, "blueprint", staleBlueprint["id"].(string), "approved"); !strings.Contains(out, "Cloud sync") {
		t.Fatalf("stale blueprint approval error = %s", out)
	}

	// A corrected Blueprint matching the newer RRI referent still approves.
	corrected := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", staleID, "blueprint", v21BlueprintWithExcludedKeys("Local cache")))
	runPic(t, bin, root, home, "work-item", "artifact-approve", staleID, "blueprint", corrected["id"].(string), "approved")

	// A policy-v2 Contract binds to an approved v2.1 Blueprint: the seam
	// authority must accept the v2.1 marker, not only exact policy v2.
	contractContent := v2ContractArtifact(corrected["id"].(string), int(corrected["revision"].(float64)), corrected["content_hash"].(string))
	contract := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", staleID, "contracts", contractContent))
	runPic(t, bin, root, home, "work-item", "artifact-approve", staleID, "contracts", contract["id"].(string), "approved")
}

func TestContractArtifactPolicySchemas(t *testing.T) {
	if err := validateContractReport(validContractArtifact); err != nil {
		t.Fatalf("v1 contract fixture must validate unchanged: %v", err)
	}
	if err := validateContractReport(v2ContractArtifact("wia-x", 1, "sha256:abc")); err != nil {
		t.Fatalf("v2 contract fixture must validate: %v", err)
	}
	badClass := strings.Replace(v2ContractArtifact("wia-x", 1, "sha256:abc"), `"class":"data_invariant"`, `"class":"performance"`, 1)
	if err := validateContractReport(badClass); err == nil || !strings.Contains(err.Error(), "decomposition class") {
		t.Fatalf("v2 contract bad class = %v", err)
	}
	missingSeam := strings.Replace(v2ContractArtifact("wia-x", 1, "sha256:abc"), `"seam":"cli-materialize"`, `"seam":""`, 1)
	if err := validateContractReport(missingSeam); err == nil || !strings.Contains(err.Error(), "requires a verification seam") {
		t.Fatalf("v2 contract missing seam = %v", err)
	}
	unbound := strings.Replace(v2ContractArtifact("wia-x", 1, "sha256:abc"), `"source_blueprint":{"artifact_id":"wia-x","revision":1,"content_hash":"sha256:abc"},`, ``, 1)
	if err := validateContractReport(unbound); err == nil || !strings.Contains(err.Error(), "source_blueprint") {
		t.Fatalf("v2 contract without blueprint binding = %v", err)
	}
	if err := validateContractReport(strings.Replace(v2ContractArtifact("wia-x", 1, "sha256:abc"), `"decomposition_policy_version":2`, `"decomposition_policy_version":3`, 1)); err == nil || !strings.Contains(err.Error(), "unsupported decomposition_policy_version 3") {
		t.Fatalf("unsupported policy version = %v", err)
	}
}

func TestDecompositionPolicyApprovalChain(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Policy v2 Epic"))
	id := epic["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	seedV2Requirements(t, dbPath, id)
	blueprint := approveV2Blueprint(t, bin, root, home, id)
	contractContent := v2ContractArtifact(blueprint["id"].(string), int(blueprint["revision"].(float64)), blueprint["content_hash"].(string))
	contract := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "contracts", contractContent))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "contracts", contract["id"].(string), "approved")

	graphJSON := v2TaskGraph(contract["id"].(string), int(contract["revision"].(float64)), contract["content_hash"].(string))
	graph := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graphJSON))
	validated := asObject(t, runPic(t, bin, root, home, "work-item", "graph-validate", id))
	if validated["valid"] != true || validated["decomposition_policy_version"] != float64(2) {
		t.Fatalf("v2 graph validation = %#v", validated)
	}
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", graph["id"].(string), "approved")

	reject := func(name, graph, needle string) {
		t.Helper()
		runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graph)
		if out := runPicError(t, bin, root, home, "work-item", "graph-validate", id); !strings.Contains(out, needle) {
			t.Fatalf("%s: error = %s, want substring %q", name, out, needle)
		}
	}
	// Missing edge rationale.
	bad := strings.Replace(graphJSON, `"depends_on_rationale":{"S01":"consumes the persisted schema contract S01 establishes"}`, `"depends_on_rationale":{"S01":""}`, 1)
	reject("empty edge rationale", bad, "requires a non-empty depends_on_rationale")
	// Verification gate without a seam.
	bad = strings.Replace(graphJSON, `"seam":"go-tests","requirement_keys":["REQ-001"],"command":"go test ./..."`, `"requirement_keys":["REQ-001"],"command":"go test ./..."`, 1)
	reject("gate without seam", bad, "requires a seam")
	// Verification gate seam the Blueprint does not declare.
	bad = strings.Replace(graphJSON, `"seam":"go-tests","requirement_keys":["REQ-001"],"command":"go test ./..."`, `"seam":"e2e-browser","requirement_keys":["REQ-001"],"command":"go test ./..."`, 1)
	reject("undeclared seam", bad, "which the approved Blueprint does not declare")
	// Verification gate without requirement or obligation keys.
	bad = strings.Replace(graphJSON, `"seam":"go-tests","requirement_keys":["REQ-001"],"command":"go test ./..."`, `"seam":"go-tests","command":"go test ./..."`, 1)
	reject("gate without keys", bad, "at least one requirement or obligation key")
	// Verification gate without expected evidence.
	bad = strings.Replace(graphJSON, `"command":"go test ./...","expected":"requirement behavior proven"`, `"command":"go test ./..."`, 1)
	reject("gate without expected", bad, "executable command and expected evidence")
	// Verification gate referencing an unknown Contract obligation.
	bad = strings.Replace(graphJSON, `"seam":"cli-materialize","obligation_keys":["OB-003"]`, `"seam":"cli-materialize","obligation_keys":["OB-404"]`, 1)
	reject("unknown obligation", bad, "unknown Contract obligation OB-404")
	// A second provider node for the same obligation makes provenance ambiguous.
	bad = strings.Replace(graphJSON, `"provides":["OB-001"]`, `"provides":["OB-001","OB-002"]`, 1)
	reject("duplicate obligation provider", bad, "must have exactly one provider node, found 2")
	// An integration-gate node of any type must carry a verification entry.
	bad = strings.Replace(graphJSON, `"verification":[{"seam":"cli-materialize","obligation_keys":["OB-002"],"command":"go test ./cmd/pic -run TestDelivery","expected":"integrated delivery verified"}]`, `"verification":[]`, 1)
	reject("integration gate without verification", bad, "G01 requires at least one seam-bound verification entry")
	// Shared-contract node without a downstream consumer (the consumer drops both
	// the dependency and the consume so the Contract obligation graph stays valid).
	bad = strings.Replace(graphJSON, `"depends_on":["S01"],"priority":"P0"`, `"depends_on":[],"priority":"P0"`, 1)
	bad = strings.Replace(bad, `"depends_on_rationale":{"S01":"consumes the persisted schema contract S01 establishes"},`, ``, 1)
	bad = strings.Replace(bad, `"consumes":["OB-003"]`, `"consumes":[]`, 1)
	reject("shared contract without consumer", bad, "no downstream consumer depending on it")
	// Wide refactor without a paired contract node.
	bad = strings.Replace(graphJSON, `"paired_contract_node":"P01",`, ``, 1)
	reject("wide refactor without pair", bad, "wide_refactor requires paired_contract_node")
	// Wide refactor whose declaring node is outside the paired node's closure.
	bad = strings.Replace(graphJSON, `"depends_on":["W01"],"priority":"P1","module":"core","files":["w.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["w.go"]},"verification":[{"seam":"go-tests","requirement_keys":["REQ-001"],"command":"go test ./cmd/pic -run TestCleanup","expected":"cleanup verified"}],"skillFamilies":[],"provides":[],"consumes":[],"evidence_for":[],"obligation_keys":[],"depends_on_rationale":{"W01":"contracts the expansion W01 performs"}`, `"depends_on":[],"priority":"P1","module":"core","files":["w.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["w.go"]},"verification":[{"seam":"go-tests","requirement_keys":["REQ-001"],"command":"go test ./cmd/pic -run TestCleanup","expected":"cleanup verified"}],"skillFamilies":[],"provides":[],"consumes":[],"evidence_for":[],"obligation_keys":[]`, 1)
	reject("wide refactor closure", bad, "depends_on closure of its paired contract node P01")
	// Integration gate on a node without obligation or requirement coverage.
	bad = strings.Replace(graphJSON, `"key":"F01","type":"feature","name":"Area","requirement_keys":[],"depends_on":[]`, `"key":"F01","type":"feature","name":"Area","requirement_keys":[],"depends_on":[],"decomposition_mode":"integration_gate","exception_reason":"aggregate verification gate"`, 1)
	reject("integration gate without coverage", bad, "must list the obligations or requirements it verifies")
	// Unknown mode.
	bad = strings.Replace(graphJSON, `"decomposition_mode":"shared_contract"`, `"decomposition_mode":"horizontal"`, 1)
	reject("unknown mode", bad, "unknown decomposition_mode")
	// Exception mode without a reason.
	bad = strings.Replace(graphJSON, `"exception_reason":"widens the schema consumed by CLI, scheduler, and dashboard"`, `"exception_reason":""`, 1)
	reject("missing exception reason", bad, "without exception_reason")
	// Multi-requirement node without node-level acceptance.
	multiRequirement := strings.Replace(graphJSON, `"requirement_keys":["REQ-002"],"depends_on":["S01"]`, `"requirement_keys":["REQ-001","REQ-002"],"depends_on":["S01"]`, 1)
	bad = strings.Replace(multiRequirement, `"acceptance":"Given an approved graph\nWhen materialization runs\nThen projections commit atomically",`, ``, 1)
	reject("composed node without acceptance", bad, "requires node-level acceptance")
	// Node-authored acceptance must be Gherkin.
	bad = strings.Replace(graphJSON, `"acceptance":"Given an approved graph\nWhen materialization runs\nThen projections commit atomically"`, `"acceptance":"projections commit atomically"`, 1)
	reject("non-gherkin acceptance", bad, "acceptance require Given, When, and Then steps")
	// Unsupported policy version on a graph.
	bad = strings.Replace(graphJSON, `"decomposition_policy_version":2`, `"decomposition_policy_version":3`, 1)
	reject("unsupported policy version", bad, "unsupported decomposition_policy_version 3")

	// The graph binds the exact approved Contract lineage: a stale hash or a
	// wrong predecessor id fails closed at save time.
	bad = strings.Replace(graphJSON, contract["content_hash"].(string), "sha256:stale", 1)
	if out := runPicError(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", bad); !strings.Contains(out, "must bind the approved Contract") {
		t.Fatalf("stale contract binding = %s", out)
	}
	bad = strings.Replace(graphJSON, `"source_contract":{"artifact_id":"`+contract["id"].(string)+`"`, `"source_contract":{"artifact_id":"wia-wrong-lineage"`, 1)
	if out := runPicError(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", bad); !strings.Contains(out, "must bind the approved Contract") {
		t.Fatalf("wrong-lineage contract binding = %s", out)
	}
	bad = strings.Replace(graphJSON, `"source_contract":{"artifact_id":"`+contract["id"].(string)+`","revision":`+fmt.Sprint(int(contract["revision"].(float64)))+`,"content_hash":"`+contract["content_hash"].(string)+`"},`, ``, 1)
	if out := runPicError(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", bad); !strings.Contains(out, "source_contract") {
		t.Fatalf("missing source_contract = %s", out)
	}

	// Contract binding is re-checked against the approved Blueprint at save time.
	unbound := strings.Replace(contractContent, blueprint["content_hash"].(string), "sha256:stale", 1)
	if out := runPicError(t, bin, root, home, "work-item", "artifact-save", id, "contracts", unbound); !strings.Contains(out, "must bind the approved Blueprint") {
		t.Fatalf("stale blueprint binding = %s", out)
	}
	unbound = strings.Replace(contractContent, `"class":"user_behavior","seam":"cli-materialize"`, `"class":"user_behavior","seam":"e2e-browser"`, 1)
	if out := runPicError(t, bin, root, home, "work-item", "artifact-save", id, "contracts", unbound); !strings.Contains(out, "does not declare") {
		t.Fatalf("undeclared obligation seam = %s", out)
	}
	// A v2 Contract without any approved Blueprint fails closed at save time.
	fresh := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Unbound Contract Epic"))
	if out := runPicError(t, bin, root, home, "work-item", "artifact-save", fresh["id"].(string), "contracts", v2ContractArtifact("wia-x", 1, "sha256:abc")); !strings.Contains(out, "requires an approved Blueprint on the same planning lineage") {
		t.Fatalf("v2 contract without blueprint = %s", out)
	}
	// A v1 Contract chain keeps validating under v1 rules.
	runPic(t, bin, root, home, "work-item", "artifact-save", fresh["id"].(string), "contracts", validContractArtifact)

	// A policy-v2 graph on a Work Item without Blueprint/Contract predecessors
	// (standalone profile) is rejected instead of skipping seam authority.
	standalone := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "No Seam Authority"))
	runSQLite(t, dbPath, `INSERT INTO requirements(id,task_id,requirement_key,title,acceptance_criteria) VALUES('req-nsa','`+standalone["id"].(string)+`','REQ-S1','Required','Given valid context
When work runs
Then it completes')`)
	if out := runPicError(t, bin, root, home, "work-item", "artifact-save", standalone["id"].(string), "task_graph", v2StandaloneGraphV2Policy); !strings.Contains(out, "requires an approved Contract on the same planning lineage") {
		t.Fatalf("standalone v2 graph without seam authority = %s", out)
	}
}

func TestDecompositionPolicyV1Unchanged(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Policy v1 Epic"))
	id := epic["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-v1','`+id+`','REQ-001','Required','Given valid context
When work runs
Then it completes')`)
	for _, stage := range []string{"scan", "rri", "vision", "blueprint", "contracts"} {
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, planningArtifactContent(stage)))
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
	}
	// The v1 graph has no decomposition fields at all and must still approve.
	graph := `{"version":3,"execution_policy":"strict_sequential","nodes":[{"key":"F01","type":"feature","name":"Area","requirement_keys":[],"depends_on":[]},{"key":"T01","type":"task","name":"Implement","parent_key":"F01","goal":"Implement requirement","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]}]}`
	artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graph))
	validated := asObject(t, runPic(t, bin, root, home, "work-item", "graph-validate", id))
	if validated["valid"] != true || validated["decomposition_policy_version"] != float64(0) {
		t.Fatalf("v1 graph validation = %#v", validated)
	}
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", artifact["id"].(string), "approved")
	materialized := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))
	if materialized["created"] != float64(2) {
		t.Fatalf("v1 materialization = %#v", materialized)
	}
}

func TestDecompositionProjectionMaterialization(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Projection Epic"))
	id := epic["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	seedV2Requirements(t, dbPath, id)
	blueprint := approveV2Blueprint(t, bin, root, home, id)
	contractContent := v2ContractArtifact(blueprint["id"].(string), int(blueprint["revision"].(float64)), blueprint["content_hash"].(string))
	contract := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "contracts", contractContent))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "contracts", contract["id"].(string), "approved")
	graphJSON := v2TaskGraph(contract["id"].(string), int(contract["revision"].(float64)), contract["content_hash"].(string))
	graph := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graphJSON))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", graph["id"].(string), "approved")
	materialized := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))
	if materialized["created"] != float64(7) {
		t.Fatalf("v2 materialization = %#v", materialized)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// The aggregate root epic is not a projection; each node row is, and every
	// one carries the exact source graph lineage.
	nodeProjection := func(key string) (string, string, string, string, int, string) {
		t.Helper()
		var mode, reason, paired, artifactID, contentHash string
		var revision int
		if err := db.QueryRow(`SELECT wi.decomposition_mode,wi.decomposition_reason,wi.paired_contract_node,wi.source_graph_artifact_id,wi.source_graph_revision,wi.source_graph_content_hash FROM work_items wi JOIN work_item_materializations m ON m.work_item_id=wi.id WHERE m.root_work_item_id=? AND m.node_key=?`, id, key).Scan(&mode, &reason, &paired, &artifactID, &revision, &contentHash); err != nil {
			t.Fatal(err)
		}
		return mode, reason, paired, artifactID, revision, contentHash
	}
	var epicMode string
	if err := db.QueryRow(`SELECT decomposition_mode FROM work_items WHERE id=?`, id).Scan(&epicMode); err != nil || epicMode != "vertical" {
		t.Fatalf("aggregate root carries the column default mode=%q err=%v", epicMode, err)
	}
	featureMode, _, _, lineageArtifact, lineageRevision, lineageHash := nodeProjection("F01")
	if featureMode != "vertical" {
		t.Fatalf("feature projection mode = %q, want normalized vertical", featureMode)
	}
	// An omitted v2 mode is normalized to vertical before persistence.
	t01Mode, _, _, _, _, _ := nodeProjection("T01")
	if t01Mode != "vertical" {
		t.Fatalf("omitted v2 mode persisted as %q, want vertical", t01Mode)
	}
	gateMode, _, _, _, _, _ := nodeProjection("G01")
	if gateMode != "integration_gate" {
		t.Fatalf("integration gate projection mode = %q", gateMode)
	}
	widenedMode, widenedReason, widenedPaired, _, _, _ := nodeProjection("W01")
	if widenedMode != "wide_refactor" || widenedPaired != "P01" || !strings.Contains(widenedReason, "call site") {
		t.Fatalf("wide refactor projection = %q/%q/%q", widenedMode, widenedReason, widenedPaired)
	}
	sharedMode, sharedReason, _, _, _, _ := nodeProjection("S01")
	if sharedMode != "shared_contract" || !strings.Contains(sharedReason, "schema consumed by") {
		t.Fatalf("shared contract projection = %q/%q", sharedMode, sharedReason)
	}
	var rationale, relatedID string
	if err := db.QueryRow(`SELECT r.rationale,r.related_work_item_id FROM work_item_relations r JOIN work_item_materializations m ON m.work_item_id=r.work_item_id WHERE m.root_work_item_id=? AND m.node_key='T02' AND r.relation_type='blocks'`, id).Scan(&rationale, &relatedID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rationale, "consumes the persisted schema contract") {
		t.Fatalf("edge rationale = %q", rationale)
	}
	var providerNode string
	if err := db.QueryRow(`SELECT node_key FROM work_item_materializations WHERE work_item_id=? AND root_work_item_id=?`, relatedID, id).Scan(&providerNode); err != nil || providerNode != "S01" {
		t.Fatalf("rationale target node = %q err=%v", providerNode, err)
	}
	if lineageArtifact != graph["id"].(string) || lineageRevision != int(graph["revision"].(float64)) || lineageHash != graph["content_hash"].(string) {
		t.Fatalf("source lineage = %s@%d (%s), want %s@%v (%s)", lineageArtifact, lineageRevision, lineageHash, graph["id"], graph["revision"], graph["content_hash"])
	}
	var t02ID, w01ID string
	if err := db.QueryRow(`SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND node_key='T02'`, id).Scan(&t02ID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND node_key='W01'`, id).Scan(&w01ID); err != nil {
		t.Fatal(err)
	}

	// pic show exposes the projection metadata and edge rationale.
	shown := asObject(t, runPic(t, bin, root, home, "show", w01ID))
	item := shown["work_item"].(map[string]any)
	if item["decomposition_mode"] != "wide_refactor" || item["paired_contract_node"] != "P01" || item["source_graph_artifact_id"] != graph["id"].(string) {
		t.Fatalf("pic show work_item projection = %#v", item)
	}
	shownConsumer := asObject(t, runPic(t, bin, root, home, "show", t02ID))
	dependencies := shownConsumer["dependencies"].([]any)
	foundRationale := false
	for _, entry := range dependencies {
		dependency := entry.(map[string]any)
		if dependency["rationale"] != nil && strings.Contains(fmt.Sprint(dependency["rationale"]), "consumes the persisted schema contract") {
			foundRationale = true
		}
	}
	if !foundRationale {
		t.Fatalf("pic show dependencies missing rationale: %#v", dependencies)
	}

	// Standalone Work Items (no blueprint stage) keep v2 rules without seam
	// binding and record their own projection.
	task := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Standalone v2"))
	taskID := task["id"].(string)
	runSQLite(t, dbPath, `INSERT INTO requirements(id,task_id,requirement_key,title,acceptance_criteria) VALUES('req-standalone','`+taskID+`','REQ-001','Required','Given valid context
When work runs
Then it completes')`)
	for _, stage := range []string{"scan", "rri"} {
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", taskID, stage, planningArtifactContent(stage)))
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", taskID, stage, artifact["id"].(string), decision)
	}
	standalone := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", taskID, "task_graph", v2StandaloneGraph))
	runPic(t, bin, root, home, "work-item", "artifact-approve", taskID, "task_graph", standalone["id"].(string), "approved")
	if out := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", taskID)); out["total"] != float64(1) {
		t.Fatalf("standalone materialization = %#v", out)
	}
	// The standalone projection is the Work Item itself.
	var taskMode, taskArtifact string
	var taskRevision int
	if err := db.QueryRow(`SELECT decomposition_mode,source_graph_artifact_id,source_graph_revision FROM work_items WHERE id=?`, taskID).Scan(&taskMode, &taskArtifact, &taskRevision); err != nil {
		t.Fatal(err)
	}
	if taskMode != "vertical" || taskArtifact != standalone["id"].(string) || taskRevision != 1 {
		t.Fatalf("standalone projection = mode %q lineage %s@%d", taskMode, taskArtifact, taskRevision)
	}

	// The frozen TIP resolves a single-requirement node's acceptance explicitly:
	// T01 authored no acceptance, so content.acceptance carries REQ-001's criteria.
	runPic(t, bin, root, home, "work-item", "authorize", id, "owner")
	var t01ID string
	if err := db.QueryRow(`SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND node_key='T01'`, id).Scan(&t01ID); err != nil {
		t.Fatal(err)
	}
	runPic(t, bin, root, home, "workflow", "pipeline-claim", t01ID, "worker")
	var frozenAcceptance string
	if err := db.QueryRow(`SELECT json_extract(content_json,'$.content.acceptance') FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, t01ID).Scan(&frozenAcceptance); err != nil {
		t.Fatal(err)
	}
	if frozenAcceptance != "Given valid context\nWhen work runs\nThen it completes" {
		t.Fatalf("frozen TIP acceptance = %q, want the resolved requirement acceptance", frozenAcceptance)
	}
}

func TestDecompositionProjectionMigration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := openSQLite(dbPath)
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
	if err := initDB(dbPath); err != nil {
		t.Fatalf("pre-v8 database failed to migrate: %v", err)
	}
	db, err = openSQLite(dbPath)
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
	if err := initDB(dbPath); err != nil {
		t.Fatalf("second open re-applied migration 8: %v", err)
	}
}

func TestTaskGraphApprovalCheckpointQuestions(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Approval Questions"))
	id := epic["id"].(string)
	runSQLite(t, filepath.Join(root, ".pi", "tasks.db"), `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-q','`+id+`','REQ-001','Required','Given valid context
When work runs
Then it completes')`)
	for _, stage := range []string{"scan", "rri", "vision", "blueprint", "contracts"} {
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, planningArtifactContent(stage)))
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
	}
	status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["next_stage"] != "task_graph" {
		t.Fatalf("next_stage = %#v, want task_graph", status["next_stage"])
	}
	questions, ok := status["checkpoint_questions"].([]any)
	if !ok || len(questions) != 5 {
		t.Fatalf("checkpoint_questions = %#v", status["checkpoint_questions"])
	}
	for _, question := range []string{"too coarse or too fine", "independently meaningful verification", "genuinely gate execution", "horizontal exceptions justified", "merge or split"} {
		found := false
		for _, entry := range questions {
			if strings.Contains(fmt.Sprint(entry), question) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("checkpoint_questions missing %q: %#v", question, questions)
		}
	}
	// Other stages do not carry the Task Graph granularity questions.
	early := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Early Stage"))
	if status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", early["id"].(string))); status["next_stage"] == "task_graph" {
		t.Fatalf("early stage unexpectedly at task_graph: %#v", status)
	} else if status["checkpoint_questions"] != nil {
		t.Fatalf("non-task_graph stage carries checkpoint_questions: %#v", status["checkpoint_questions"])
	}
}

func TestRejectedCheckpointsDoNotSupplyPlanningAuthority(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Rejected Authority"))
	id := epic["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	seedV2Requirements(t, dbPath, id)
	blueprint := approveV2Blueprint(t, bin, root, home, id)
	contractContent := v2ContractArtifact(blueprint["id"].(string), int(blueprint["revision"].(float64)), blueprint["content_hash"].(string))
	contract := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "contracts", contractContent))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "contracts", contract["id"].(string), "approved")

	// A newer REJECTED Contract revision never supplies obligation or lineage
	// authority: re-seed the approved revision-1 checkpoint next to a rejected
	// revision-2 checkpoint and the graph must keep binding revision 1.
	contract2 := strings.Replace(contractContent, "Persist workflow state", "Rewritten behavior", 1)
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "contracts", contract2)
	runSQLite(t, dbPath, `INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES
		('wic-c1-reapproved','`+id+`','contracts','`+contract["id"].(string)+`',1,'`+contract["content_hash"].(string)+`','approved'),
		('wic-c2-rejected','`+id+`','contracts','wia-c2',2,'sha256:rejected','rejected')`)
	graphJSON := v2TaskGraph(contract["id"].(string), 1, contract["content_hash"].(string))
	graph := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graphJSON))
	if validated := asObject(t, runPic(t, bin, root, home, "work-item", "graph-validate", id)); validated["valid"] != true {
		t.Fatalf("graph must bind the last approved contract: %#v", validated)
	}
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", graph["id"].(string), "approved")
	if materialized := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id)); materialized["created"] != float64(7) {
		t.Fatalf("materialization against the approved lineage = %#v", materialized)
	}

	// A rejected NEWER Task Graph checkpoint takes no authority either: saving
	// revision 2 leaves the approved revision-1 checkpoint in place (materiali-
	// zations block its deletion), so re-materialization reuses revision 1.
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graphJSON)
	runSQLite(t, dbPath, `INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES
		('wic-g2-rejected','`+id+`','task_graph','`+graph["id"].(string)+`',2,'`+graph["content_hash"].(string)+`','rejected')`)
	if again := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id)); again["created"] != float64(0) || again["total"] != float64(7) {
		t.Fatalf("rejected graph checkpoint must not re-materialize: %#v", again)
	}
	rejectedDB, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var rejectedMappings int
	if err := rejectedDB.QueryRow(`SELECT COUNT(*) FROM work_item_materializations WHERE checkpoint_id='wic-g2-rejected'`).Scan(&rejectedMappings); err != nil {
		t.Fatal(err)
	}
	rejectedDB.Close()
	if rejectedMappings != 0 {
		t.Fatalf("rejected checkpoint gained %d materialization mappings", rejectedMappings)
	}

	// Fail closed: with ONLY a rejected Blueprint checkpoint, a policy-v2
	// Contract cannot save and the contracts stage cannot clear its
	// predecessor gate.
	other := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Rejected Only"))
	otherID := other["id"].(string)
	seedV2Requirements(t, dbPath, otherID)
	rejectedBlueprint := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", otherID, "blueprint", v2BlueprintArtifact))
	runSQLite(t, dbPath, `INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES
		('wic-b-rejected','`+otherID+`','blueprint','`+rejectedBlueprint["id"].(string)+`',1,'`+rejectedBlueprint["content_hash"].(string)+`','rejected')`)
	if out := runPicError(t, bin, root, home, "work-item", "artifact-save", otherID, "contracts", v2ContractArtifact("wia-x", 1, "sha256:abc")); !strings.Contains(out, "requires an approved Blueprint on the same planning lineage") {
		t.Fatalf("rejected blueprint must not authorize a v2 contract: %s", out)
	}
	// A v1 contract can still save, but the predecessor gate refuses approval
	// because the blueprint stage holds only a rejected checkpoint.
	runPic(t, bin, root, home, "work-item", "artifact-save", otherID, "contracts", validContractArtifact)
	if out := runPicError(t, bin, root, home, "work-item", "artifact-approve", otherID, "contracts", "current", "approved"); !strings.Contains(out, "Previous stage blueprint is not approved") {
		t.Fatalf("rejected blueprint must not clear the predecessor gate: %s", out)
	}

	// Predecessor fallback: the last APPROVED checkpoint clears the gate even
	// when a newer revision of the same stage was rejected.
	fallback := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Predecessor Fallback"))
	fallbackID := fallback["id"].(string)
	seedV2Requirements(t, dbPath, fallbackID)
	for _, stage := range []string{"scan", "rri", "vision"} {
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", fallbackID, stage, planningArtifactContent(stage)))
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", fallbackID, stage, artifact["id"].(string), decision)
	}
	bp1 := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", fallbackID, "blueprint", v2BlueprintArtifact))
	runPic(t, bin, root, home, "work-item", "artifact-approve", fallbackID, "blueprint", bp1["id"].(string), "approved")
	revised := strings.Replace(v2BlueprintArtifact, "Reliable workflow", "Revised reliable workflow", 1)
	bp2 := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", fallbackID, "blueprint", revised))
	runSQLite(t, dbPath, `INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES
		('wic-fb1-approved','`+fallbackID+`','blueprint','`+bp1["id"].(string)+`',1,'`+bp1["content_hash"].(string)+`','approved'),
		('wic-fb2-rejected','`+fallbackID+`','blueprint','`+bp2["id"].(string)+`',2,'`+bp2["content_hash"].(string)+`','rejected')`)
	runPic(t, bin, root, home, "work-item", "artifact-save", fallbackID, "contracts", validContractArtifact)
	runPic(t, bin, root, home, "work-item", "artifact-approve", fallbackID, "contracts", "current", "approved")
}

// querySQLiteColumn runs one SQLite query through the sqlite3 CLI and returns
// its trimmed stdout, so tests can assert persisted evidence without mocks.
func querySQLiteColumn(t *testing.T, dbPath string, query string) string {
	t.Helper()
	out, err := exec.Command("sqlite3", dbPath, query).CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite query %q failed: %v\n%s", query, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestBlueprintAnnotationEvidence (OB-F3-2/OB-F3-3, go-artifact-approve seam):
// a real temporary SQLite database proves that terminal annotation dispositions
// are recorded as durable approval evidence inside the approval checkpoint,
// that incomplete evidence is rejected before any canonical mutation, and that
// the Go PI_TASK_AGENT_NAME rejection still defends approval behind the new
// gate.
func TestBlueprintAnnotationEvidence(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Annotation Evidence"))
	id := epic["id"].(string)
	for _, stage := range []string{"scan", "rri", "vision"} {
		content := stage + " content"
		if stage == "vision" {
			content = validVisionArtifact
		}
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, content))
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
	}
	blueprint := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "blueprint", validBlueprintArtifact))
	blueprintID := blueprint["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")

	// Incomplete evidence (empty evidence, bad resolution, duplicates) is
	// rejected before any canonical save or checkpoint mutation.
	if out := runPicError(t, bin, root, home, "work-item", "artifact-approve", id, "blueprint", blueprintID, "approved", "--dispositions-json", `[{"annotation":"tighten seam 2","resolution":"addressed","evidence":""}]`); !strings.Contains(out, "nonempty evidence") {
		t.Fatalf("empty evidence disposition = %s", out)
	}
	if out := runPicError(t, bin, root, home, "work-item", "artifact-approve", id, "blueprint", blueprintID, "approved", "--dispositions-json", `[{"annotation":"tighten seam 2","resolution":"noted","evidence":"e"}]`); !strings.Contains(out, "addressed or waived") {
		t.Fatalf("nonterminal resolution = %s", out)
	}
	if out := runPicError(t, bin, root, home, "work-item", "artifact-approve", id, "blueprint", blueprintID, "approved", "--dispositions-json", `[{"annotation":"a","resolution":"addressed","evidence":"e"},{"annotation":"a","resolution":"waived","evidence":"dup"}]`); !strings.Contains(out, "duplicate disposition") {
		t.Fatalf("duplicate disposition = %s", out)
	}
	if count := querySQLiteColumn(t, dbPath, `SELECT COUNT(*) FROM workflow_checkpoints WHERE work_item_id='`+id+`' AND stage='blueprint'`); count != "0" {
		t.Fatalf("rejected evidence attempts must not record a checkpoint, got %s", count)
	}

	// Successful approval records both terminal dispositions as durable
	// evidence attached to the approved Blueprint checkpoint.
	valid := `[{"annotation":"tighten seam 2","resolution":"addressed","evidence":"seam 2 rewritten"},{"annotation":"rename the gate","resolution":"waived","evidence":"owner deferred naming"}]`
	approved := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-approve", id, "blueprint", blueprintID, "approved", "--dispositions-json", valid))
	if approved["dispositions_recorded"] != float64(2) {
		t.Fatalf("approved dispositions = %#v", approved)
	}
	evidence := querySQLiteColumn(t, dbPath, `SELECT dispositions_json FROM workflow_checkpoints WHERE work_item_id='`+id+`' AND stage='blueprint' AND decision_type='approved'`)
	for _, fragment := range []string{"tighten seam 2", "addressed", "seam 2 rewritten", "rename the gate", "waived", "owner deferred naming"} {
		if !strings.Contains(evidence, fragment) {
			t.Fatalf("checkpoint evidence %q missing %q", evidence, fragment)
		}
	}

	// Immutable evidence: the same artifact revision can never be approved
	// again, so no duplicate evidence rows can accumulate behind the checkpoint.
	if out := runPicError(t, bin, root, home, "work-item", "artifact-approve", id, "blueprint", blueprintID, "approved", "--dispositions-json", valid); !strings.Contains(out, "UNIQUE constraint failed") {
		t.Fatalf("re-approval of the same revision = %s", out)
	}

	// Revised blueprint round: each approval binds its own complete evidence.
	revised := strings.Replace(validBlueprintArtifact, "Reliable workflow", "Revised reliable workflow", 1)
	blueprint2 := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "blueprint", revised))
	approved2 := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-approve", id, "blueprint", blueprint2["id"].(string), "approved", "--dispositions-json", `[{"annotation":"tighten seam 2","resolution":"addressed","evidence":"seam 2 rewritten"}]`))
	if approved2["dispositions_recorded"] != float64(1) {
		t.Fatalf("revised approval dispositions = %#v", approved2)
	}
	// Evidence survives and remains queryable after cleanup: the approved
	// Blueprint checkpoint still carries its dispositions even though no
	// runtime draft or plan file remains (revision 2's save invalidates the
	// revision 1 checkpoint by the established downstream-invalidation rules,
	// so exactly the current approval's evidence row remains).
	rows := querySQLiteColumn(t, dbPath, `SELECT COUNT(*) FROM workflow_checkpoints WHERE work_item_id='`+id+`' AND stage='blueprint' AND decision_type='approved' AND dispositions_json!=''`)
	if rows != "1" {
		t.Fatalf("durable evidence checkpoints = %s", rows)
	}

	// Child-agent rejection: the existing Go PI_TASK_AGENT_NAME defense still
	// rejects the approval regardless of any actor_role the child supplies.
	child := exec.Command(bin, "work-item", "artifact-approve", id, "blueprint", blueprintID, "approved", "--actor-role", "owner", "--dispositions-json", valid)
	child.Dir = root
	child.Env = append(clearedPiEnv(), "HOME="+home, "PI_TASK_AGENT_NAME=task-reviewer")
	if out, err := child.CombinedOutput(); err == nil || !strings.Contains(string(out), "cannot mutate Work Item lifecycle") {
		t.Fatalf("child-agent approval attempt: err=%v out=%s", err, out)
	}
}
