package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
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
	cmd.Env = append(os.Environ(), "HOME="+home)
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
	cmd.Env = append(os.Environ(), "HOME="+home)
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
	cmd.Env = append(os.Environ(), "HOME="+home)
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
	counts := func() string {
		var value string
		if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM epics)||':'||(SELECT COUNT(*) FROM tasks)||':'||(SELECT COUNT(*) FROM work_items)`).Scan(&value); err != nil {
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
		t.Fatalf("removed commands mutated storage: before=%s after=%s", before, after)
	}
}

func TestShowPrefersCanonicalHybridWorkItem(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	runSQLite(t, filepath.Join(root, ".pi", "tasks.db"), `
		INSERT INTO tasks(id,title,status,review_status) VALUES('t-hybrid','Legacy','in_progress','pending');
		INSERT INTO work_items(id,type,title,status,review_status) VALUES('t-hybrid','task','Canonical','done','passed');
	`)

	shown := asObject(t, runPic(t, bin, root, home, "show", "t-hybrid"))
	item := asObject(t, shown["work_item"])
	if item["status"] != "done" || item["review_status"] != "passed" {
		t.Fatalf("generic show returned stale legacy lifecycle state: %#v", shown)
	}
}

func TestShowReportsCanonicalReadinessForHybridWorkItem(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	runSQLite(t, filepath.Join(root, ".pi", "tasks.db"), `
		INSERT INTO tasks(id,title,status) VALUES('t-blocker','Blocker','done'),('t-hybrid','Legacy','open');
		INSERT INTO work_items(id,type,title,status,review_status) VALUES('t-blocker','task','Blocker','done','passed'),('t-hybrid','task','Canonical','open','pending');
		INSERT INTO task_dependencies(id,task_id,depends_on_task_id) VALUES('td-hybrid','t-hybrid','t-blocker');
		INSERT INTO work_item_relations(id,work_item_id,relation_type,related_work_item_id) VALUES('wir-hybrid','t-hybrid','blocks','t-blocker');
	`)

	shown := asObject(t, runPic(t, bin, root, home, "show", "t-hybrid"))
	if shown["ready"] != false {
		t.Fatalf("hybrid Work Item without an active TIP is ready: %#v", shown)
	}
	dependencies := shown["dependencies"].([]any)
	dependency := asObject(t, dependencies[0])
	if dependency["status"] != "done" || dependency["review_status"] != "passed" {
		t.Fatalf("generic show returned stale legacy dependency state: %#v", dependency)
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

func TestLegacyInstructionPackBackfillsCanonicalWorkItemPack(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `
		INSERT INTO epics(id,title) VALUES('e-migrate','Legacy');
		INSERT INTO tasks(id,epic_id,title) VALUES('t-migrate','e-migrate','Legacy Task');
		INSERT INTO work_items(id,type,title) VALUES('t-migrate','task','Canonical Task');
		INSERT INTO task_instruction_packs(id,display_key,task_id,version,status,source_type,source_task_revision,goal,files_json,patterns_json,business_rules_json,validation_rules_json,error_handling_json,state_transitions_json,contract_obligations_json,constraints_json,verification_json,requirement_snapshots_json,content_hash,activated_at)
		VALUES('pack-migrate','TIP-MIGRATE','t-migrate',1,'active','standalone_task',1,'Ship it','[]','[]','[]','[]','[]','[]','[]','{}','[]','[]','hash-migrate',datetime('now'));
	`)

	for range 2 {
		shown := asObject(t, runPic(t, bin, root, home, "show", "t-migrate"))
		packs := shown["instruction_packs"].([]any)
		if len(packs) != 1 || asObject(t, packs[0])["id"] != "pack-migrate" || shown["ready"] != true {
			t.Fatalf("legacy TIP backfill = %#v", shown)
		}
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

	blueprint2 := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "blueprint", "revised blueprint"))
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
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, stage))
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
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, stage))
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
	pack := asObject(t, runPic(t, bin, root, home, "workflow", "instruction-pack-save", firstTaskID, "--source-type", "standalone_task", "--content-json", content, "--requirement-ids-json", `["req-m"]`))
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
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, stage))
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
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, stage))
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

func TestWorkItemRriFinalizePersistsCanonicalInterview(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "RRI finalization"))
	id := item["id"].(string)
	scan := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "scan", "scan"))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scan["id"].(string), "accepted")
	payload, _ := json.Marshal(map[string]any{
		"requirements": []map[string]any{{"key": "REQ-BASELINE", "priority": "tier1", "title": "Clean baseline", "description": "Verify one revision", "acceptanceCriteria": "Given release work is complete\nWhen verification starts\nThen the exact clean commit is recorded"}},
		"decisions":    []map[string]any{{"key": "release_baseline", "answer": "Require a clean committed baseline"}},
		"report":       "# RRI REPORT\n\nOwner confirmed.",
	})
	finalized := asObject(t, runPic(t, bin, root, home, "work-item", "rri-finalize", id, string(payload)))
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
	if err = db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=? AND stage='rri'`, id).Scan(&artifacts); err != nil || artifacts != 1 {
		t.Fatalf("RRI artifacts=%d err=%v", artifacts, err)
	}
	runPicError(t, bin, root, home, "work-item", "rri-finalize", id, string(payload))
	var requirementCount, decisionCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM requirements WHERE epic_id=?`, id).Scan(&requirementCount)
	_ = db.QueryRow(`SELECT COUNT(*) FROM owner_decisions WHERE epic_id=?`, id).Scan(&decisionCount)
	if requirementCount != 1 || decisionCount != 1 {
		t.Fatalf("duplicate finalization wrote requirements=%d decisions=%d", requirementCount, decisionCount)
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
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, stage))
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
	_, err := parseTaskPlanJSON("```task-plan-json\n" + plan + "\n```")
	if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestTaskPlanV2RequiresExplicitSkillFamilies(t *testing.T) {
	base := `{"version":2,"execution_policy":"strict_sequential","nodes":[{"key":"T01","name":"One","goal":"One","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"x",%s"files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"true","required":true}]}]}`
	if _, err := parseTaskPlanJSON("```task-plan-json\n" + strings.Replace(base, "%s", "", 1) + "\n```"); err == nil || !strings.Contains(err.Error(), "requires skillFamilies") {
		t.Fatalf("missing skillFamilies error = %v", err)
	}
	plan, err := parseTaskPlanJSON("```task-plan-json\n" + strings.Replace(base, "%s", `"skillFamilies":[],`, 1) + "\n```")
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
	base := instructionPackContent{
		Goal: "Change source", Files: []string{"src/main.ts"}, BusinessRules: []any{"rule"}, ValidationRules: []any{"rule"},
		ErrorHandling: []any{"rule"}, StateTransitions: []any{"rule"}, ContractObligations: []any{"rule"}, SchemaVersion: 2,
		Constraints:  map[string]any{"generated_files": []any{"test-results/**"}},
		Verification: []any{map[string]any{"command": "npm test", "required": true, "expected_writes": []any{"test-results/**"}}},
	}
	if err := validateInstructionPackContent(base); err != nil {
		t.Fatalf("valid verification contract rejected: %v", err)
	}
	failedGate := base
	failedGate.Verification = []any{map[string]any{"required": true}}
	if err := validateInstructionPackContent(failedGate); err == nil || !strings.Contains(err.Error(), "verification command") {
		t.Fatalf("verification without command accepted: %v", err)
	}
	missingSetup := base
	missingSetup.Verification = []any{map[string]any{"command": "npm test", "required": true, "requires": []any{"dev-server"}}}
	if err := validateInstructionPackContent(missingSetup); err == nil || !strings.Contains(err.Error(), "setup_commands") {
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
	runSQLite(t, dbPath, `INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES('wia-scan','`+id+`','scan',1,'<scan_report/>','scan-hash'); INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-scan','`+id+`','scan','wia-scan',1,'scan-hash','approved');`)

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
		runSQLite(t, dbPath, `
			INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,integrated_patch_path,integrated_patch_hash,artifact_saved_at,integrated_at,result_json,created_at,completed_at,advanced_at) VALUES('pr-verified','`+id+`','worker',1,'completed','lease-verified',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','verified.patch','verified-hash','2026-01-01 00:00:01','2026-01-01 00:00:02','{}','2026-01-01 00:00:01','2026-01-01 00:00:02','2026-01-01 00:00:02');
			INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,candidate_run_id,candidate_patch_hash,result_json,created_at,completed_at,advanced_at) VALUES('pr-verified-review','`+id+`','review',1,'completed','lease-review',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','pr-verified','verified-hash','{"review_status":"passed","candidate_run_id":"pr-verified","candidate_patch_hash":"verified-hash","notes":"passed","findings":[]}','2026-01-01 00:00:03','2026-01-01 00:00:03','2026-01-01 00:00:03');
			INSERT INTO work_item_completion_reports(id,work_item_id,pipeline_run_id,instruction_pack_id,instruction_pack_version,instruction_pack_hash,status,created_at) VALUES('wicr-verified','`+id+`','pr-verified','wip-`+suffix+`',1,'pack-`+suffix+`','done','2026-01-01 00:00:04');
			INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,integrated_patch_hash,artifact_saved_at,created_at,completed_at) VALUES('pr-noop-before','`+id+`','worker',2,'completed','lease-noop',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','empty-hash','2026-01-01 00:00:05','2026-01-01 00:00:05','2026-01-01 00:00:05');
			INSERT INTO work_item_verification_reports(id,work_item_id,completion_report_id,status,summary,verified_by_role,created_at) VALUES('wivr-verified','`+id+`','wicr-verified','passed','verified after retry','contractor','2026-01-01 00:00:06');`)
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
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-circuit-reset", id, "--reason", "runner repaired", "--change-type", "runner", "--evidence-json", `{"failed_run_id":"missing"}`, "--actor-role", "owner"); !strings.Contains(out, "terminal worker attempt") {
		t.Fatalf("reset without terminal attempt = %s", out)
	}
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--explicit-retry", "1"))
	runPic(t, bin, root, home, "work-item", "status", id, "in_progress")
	runPic(t, bin, root, home, "workflow", "pipeline-complete", claim["id"].(string), claim["lease_token"].(string), "failed", "--error", "runner failed before worker execution")
	if shown := asObject(t, runPic(t, bin, root, home, "show", id)); shown["ready"] != true || asObject(t, shown["work_item"])["status"] != "open" {
		t.Fatalf("terminal worker cleanup = %#v", shown)
	}
	runSQLite(t, dbPath, `UPDATE work_items SET status='in_progress' WHERE id='`+id+`'; INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,error,completed_at) VALUES('pr-failed','`+id+`','worker',2,'failed','lease-failed',datetime('now'),'wip-`+suffix+`',1,'pack-`+suffix+`','subagent child failed',datetime('now'));`)
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-circuit-reset", id, "--reason", "runner repaired", "--change-type", "runner", "--evidence-json", `{"failed_run_id":"pr-failed"}`); !strings.Contains(out, "actor_role=owner") {
		t.Fatalf("circuit reset without owner authority = %s", out)
	}

	reset := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-circuit-reset", id, "--reason", "runner repaired", "--change-type", "runner", "--evidence-json", `{"failed_run_id":"pr-failed"}`, "--actor-role", "owner"))
	if reset["event_type"] != "pipeline_circuit_reset" || !strings.Contains(reset["payload_json"].(string), `"change_type":"runner"`) {
		t.Fatalf("reset evidence = %#v", reset)
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
	runPic(t, bin, root, home, "workflow", "pipeline-circuit-reset", id, "--reason", "owner approved one corrected retry", "--change-type", "artifact", "--evidence-json", `{"failed_run_id":"pr-invalid-fix"}`, "--actor-role", "owner")
	claim = asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--review-fix", "1", "--explicit-retry", "1"))
	if claim["review_fix_cycle"] != float64(1) {
		t.Fatalf("owner reset did not start a fresh review-fix epoch: %#v", claim)
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
		runPic(t, bin, root, home, "workflow", "pipeline-complete", claim["id"].(string), claim["lease_token"].(string), "failed", "--error", fmt.Sprintf("attempt %d failed", i))
		if shown := asObject(t, runPic(t, bin, root, home, "show", id)); asObject(t, shown["work_item"])["status"] != "open" {
			t.Fatalf("attempt %d cleanup failed: %#v", i, shown)
		}
	}

	// Without a reset, the automatic retry limit must reject a new claim.
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"); !strings.Contains(out, "automatic worker retry limit reached") {
		t.Fatalf("expected retry limit rejection, got: %s", out)
	}

	// Owner-authorized circuit reset clears the epoch.
	runPic(t, bin, root, home, "workflow", "pipeline-circuit-reset", id, "--reason", "runner repaired", "--change-type", "runner", "--evidence-json", `{"failed_run_id":"all"}`, "--actor-role", "owner")

	// After reset, a new worker claim must succeed without --explicit-retry.
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	if claim["status"] != "claimed" {
		t.Fatalf("post-reset worker claim rejected: %#v", claim)
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

func TestLegacyTIPRemoval(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE tips(id TEXT PRIMARY KEY,task_id TEXT NOT NULL REFERENCES tasks(id),tip_key TEXT NOT NULL,title TEXT NOT NULL);
		CREATE TABLE tip_dependencies(id TEXT PRIMARY KEY,tip_id TEXT NOT NULL REFERENCES tips(id),depends_on_tip_id TEXT NOT NULL REFERENCES tips(id));
		CREATE TABLE tip_requirement_links(id TEXT PRIMARY KEY,tip_id TEXT NOT NULL REFERENCES tips(id),requirement_id TEXT NOT NULL REFERENCES requirements(id));
		ALTER TABLE completion_reports ADD COLUMN tip_id TEXT REFERENCES tips(id);
		ALTER TABLE verification_items ADD COLUMN tip_id TEXT REFERENCES tips(id);
		ALTER TABLE escalations ADD COLUMN tip_id TEXT REFERENCES tips(id);
		INSERT INTO epics(id,title) VALUES('e-legacy-tip','Legacy TIP');
		INSERT INTO tasks(id,epic_id,title) VALUES('t-legacy-tip','e-legacy-tip','Legacy TIP task');
		INSERT INTO requirements(id,task_id,requirement_key,title) VALUES('req-legacy-tip','t-legacy-tip','REQ-001','Requirement');
		INSERT INTO tips(id,task_id,tip_key,title) VALUES('tip-legacy','t-legacy-tip','TIP-001','Legacy pack');
		INSERT INTO tip_requirement_links(id,tip_id,requirement_id) VALUES('trl-legacy','tip-legacy','req-legacy-tip');
		INSERT INTO completion_reports(id,task_id,tip_id,status,summary) VALUES('cr-legacy-tip','t-legacy-tip','tip-legacy','done','completion survives');
		INSERT INTO verification_reports(id,task_id,status,summary) VALUES('vr-legacy-tip','t-legacy-tip','passed','verification survives');
		INSERT INTO verification_items(id,verification_report_id,requirement_id,tip_id,status,evidence) VALUES('vi-legacy-tip','vr-legacy-tip','req-legacy-tip','tip-legacy','pass','evidence survives');
		INSERT INTO escalations(id,task_id,tip_id,level,title) VALUES('esc-legacy-tip','t-legacy-tip','tip-legacy',1,'escalation survives');`)
	if err != nil {
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
	for _, table := range []string{"tips", "tip_dependencies", "tip_requirement_links"} {
		if tableExists(db, table) {
			t.Fatalf("legacy table %s still exists", table)
		}
	}
	for _, pair := range [][2]string{{"completion_reports", "tip_id"}, {"verification_items", "tip_id"}, {"escalations", "tip_id"}} {
		if hasColumn(db, pair[0], pair[1]) {
			t.Fatalf("legacy column %s.%s still exists", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{{"completion_reports", "cr-legacy-tip"}, {"verification_items", "vi-legacy-tip"}, {"escalations", "esc-legacy-tip"}} {
		if ok, err := rowExists(db, `SELECT 1 FROM `+pair[0]+` WHERE id=?`, pair[1]); err != nil || !ok {
			t.Fatalf("non-TIP row %s.%s was not preserved: %v", pair[0], pair[1], err)
		}
	}
}

func TestPipelineSchemaMigratesLegacyColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`DROP INDEX idx_completion_reports_pipeline_run`,
		`DROP INDEX idx_verification_reports_pipeline_run`,
		`DROP INDEX idx_pipeline_runs_pending`,
		`ALTER TABLE completion_reports DROP COLUMN pipeline_run_id`,
		`ALTER TABLE verification_reports DROP COLUMN pipeline_run_id`,
		`ALTER TABLE pipeline_runs DROP COLUMN integrated_patch_path`,
		`ALTER TABLE pipeline_runs DROP COLUMN integrated_patch_hash`,
		`ALTER TABLE pipeline_runs ADD COLUMN integrated_patch TEXT DEFAULT ''`,
		`ALTER TABLE pipeline_runs DROP COLUMN integrated_at`,
		`ALTER TABLE pipeline_runs DROP COLUMN artifact_saved_at`,
		`ALTER TABLE pipeline_runs DROP COLUMN advanced_at`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			t.Fatal(err)
		}
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
	if hasColumn(db, "pipeline_runs", "integrated_patch") {
		t.Fatal("obsolete inline integrated_patch column was not removed")
	}
	for _, pair := range [][2]string{{"completion_reports", "pipeline_run_id"}, {"verification_reports", "pipeline_run_id"}, {"pipeline_runs", "integrated_patch_path"}, {"pipeline_runs", "integrated_patch_hash"}, {"pipeline_runs", "integrated_at"}, {"pipeline_runs", "artifact_saved_at"}, {"pipeline_runs", "advanced_at"}} {
		if !hasColumn(db, pair[0], pair[1]) {
			t.Fatalf("missing migrated column %s.%s", pair[0], pair[1])
		}
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
		PRAGMA legacy_alter_table=OFF`); err != nil {
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
		DROP TABLE pipeline_runs__workflow_migration`); err != nil {
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

func TestRebuildMigratesLegacySchemaAndPreservesRows(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `PRAGMA foreign_keys=OFF; CREATE TABLE projects(id TEXT PRIMARY KEY,name TEXT,root_path TEXT,created_at TEXT); INSERT INTO projects VALUES('p','Legacy','`+root+`','2020-01-01'); ALTER TABLE epics ADD COLUMN project_id TEXT; UPDATE epics SET project_id='p';`)
	result := asObject(t, runPic(t, bin, root, home, "rebuild"))
	if result["rebuilt"] != true || result["removed_projects_table"] != true || result["removed_epic_project_id"] != true || result["backup_path"] == "" {
		t.Fatalf("rebuild = %#v", result)
	}
	if _, err := os.Stat(result["backup_path"].(string)); err != nil {
		t.Fatalf("backup missing: %v", err)
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
