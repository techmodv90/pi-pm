package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// approvePlanningStage saves and approves one planning artifact for a Work Item.
// Scan is accepted; every later aggregate/standalone stage is approved.
func approvePlanningStage(t *testing.T, bin, root, home, id, stage, content string) {
	t.Helper()
	artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, content))
	decision := "approved"
	if stage == "scan" {
		decision = "accepted"
	}
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
}

// planAggregateThroughGraph drives an aggregate to an approved task graph.
func planAggregateThroughGraph(t *testing.T, bin, root, home, id string, stages []string, graph string) {
	t.Helper()
	for _, stage := range stages {
		approvePlanningStage(t, bin, root, home, id, stage, planningArtifactContent(stage))
	}
	approvePlanningStage(t, bin, root, home, id, "task_graph", graph)
}

// planStandaloneThroughGraph drives a standalone executable through the lean
// scan -> rri -> one-node task_graph path.
func planStandaloneThroughGraph(t *testing.T, bin, root, home, id, graph string) {
	t.Helper()
	planAggregateThroughGraph(t, bin, root, home, id, []string{"scan", "rri"}, graph)
}

// standaloneOneNodeGraph returns a valid schema-v3 one-node standalone graph.
func standaloneOneNodeGraph(kind, goal string) string {
	return `{"version":3,"execution_policy":"strict_sequential","nodes":[{"key":"IMPLEMENT","type":"` + kind + `","name":"Standalone","goal":"` + goal + `","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["x.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["x.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]}]}`
}

// TestAggregatePlanning verifies REQ-AGGREGATE-DEPTH and the depth-controlled
// aggregate materialization and owner-gated authorization path.
func TestAggregatePlanning(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Aggregate depth", "--planning-depth", "designed"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-agg','`+id+`','REQ-001','Required','Given valid context
When work runs
Then it completes')`)

	// Designed aggregate depth selects scan, rri, blueprint, task_graph only;
	// RRI and Task Graph are mandatory, Vision is out of profile.
	stages, err := planningStagesForWorkItem(openSQLiteGo(t, dbPath), id)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(stages, "rri") || !contains(stages, "task_graph") {
		t.Fatalf("designed aggregate missing mandatory rri/task_graph: %v", stages)
	}
	if contains(stages, "vision") || contains(stages, "contracts") {
		t.Fatalf("designed aggregate depth leaked aggregate-only stages: %v", stages)
	}

	// A Vision draft is saved but its approval is a gate bypass and must be
	// rejected by the depth profile.
	vision := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "vision", planningArtifactContent("vision")))
	if out := runPicError(t, bin, root, home, "work-item", "artifact-approve", id, "vision", vision["id"].(string), "approved"); !strings.Contains(out, "not part of this Work Item planning profile") {
		t.Fatalf("vision approved outside designed depth: %s", out)
	}

	// Materialize a depth-controlled aggregate DAG with a dependency edge.
	graph := `{"version":3,"execution_policy":"strict_sequential","nodes":[
		{"key":"T01","type":"task","name":"Child A","goal":"Implement A","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["a.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["a.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]},
		{"key":"T02","type":"task","name":"Child B","goal":"Implement B","requirement_keys":["REQ-001"],"depends_on":["T01"],"priority":"P1","module":"core","files":["b.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["b.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]}
	]}`
	planAggregateThroughGraph(t, bin, root, home, id, []string{"scan", "rri", "blueprint"}, graph)
	mat := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))
	if mat["created"] != float64(2) || mat["total"] != float64(2) {
		t.Fatalf("aggregate materialization = %#v", mat)
	}
	db := openSQLiteGo(t, dbPath)
	var children int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=?`, id).Scan(&children)
	if children != 2 {
		t.Fatalf("aggregate children = %d, want 2", children)
	}
	relations, err := queryMaps(db, `SELECT * FROM work_item_relations WHERE relation_type='blocks'`)
	if err != nil || len(relations) != 1 {
		t.Fatalf("aggregate dependency edges = %d, err=%v", len(relations), err)
	}

	// Owner authorization is a separate gate; a non-owner actor is rejected.
	if out := runPicError(t, bin, root, home, "work-item", "authorize", id, "contractor"); !strings.Contains(out, "actor_role=owner") {
		t.Fatalf("non-owner authorization accepted: %s", out)
	}
	auth := asObject(t, runPic(t, bin, root, home, "work-item", "authorize", id, "owner"))
	if auth["work_item_id"] != id {
		t.Fatalf("aggregate authorization = %#v", auth)
	}
	status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if status["workflow_kind"] != "aggregate_delivery" {
		t.Fatalf("aggregate workflow status = %#v", status)
	}
}

// TestStandalonePlanning verifies REQ-STANDALONE-PATH: a standalone Task, Bug,
// or Chore retains its identity and uses only the focused lean planning stages.
func TestStandalonePlanning(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	for _, kind := range []string{"task", "bug", "chore"} {
		item := asObject(t, runPic(t, bin, root, home, "work-item", "create", kind, "Standalone "+kind))
		id := item["id"].(string)
		dbPath := filepath.Join(root, ".pi", "tasks.db")
		runSQLite(t, dbPath, `INSERT INTO requirements(id,task_id,requirement_key,title,acceptance_criteria) VALUES('req-`+kind+`','`+id+`','REQ-001','Required','Given valid context
When work runs
Then it completes')`)

		// The lean profile is fixed for standalone executables: Scan, RRI, and a
		// one-node Task Graph with no aggregate-only stages.
		stages, err := planningStagesForWorkItem(openSQLiteGo(t, dbPath), id)
		if err != nil {
			t.Fatal(err)
		}
		if len(stages) != 3 || stages[0] != "scan" || stages[1] != "rri" || stages[2] != "task_graph" {
			t.Fatalf("%s standalone lean profile = %v", kind, stages)
		}

		// The lean path completes without aggregate-only planning stages.
		planStandaloneThroughGraph(t, bin, root, home, id, standaloneOneNodeGraph(kind, "Implement "+kind))
		mat := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))
		if mat["work_item_id"] != id || mat["created"] != float64(0) || mat["total"] != float64(1) {
			t.Fatalf("%s standalone materialization = %#v", kind, mat)
		}
		// Identity is preserved: no child decomposition, root id unchanged.
		db := openSQLiteGo(t, dbPath)
		var count int
		_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=?`, id).Scan(&count)
		if count != 0 {
			t.Fatalf("%s standalone produced %d children", kind, count)
		}
	}
}

// TestStandaloneMaterialization verifies identity-preserving one-node
// materialization, duplicate-idempotence, and multi-node rejection.
func TestStandaloneMaterialization(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Standalone mat"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,task_id,requirement_key,title,acceptance_criteria) VALUES('req-mat','`+id+`','REQ-001','Required','Given valid context
When work runs
Then it completes')`)
	planStandaloneThroughGraph(t, bin, root, home, id, standaloneOneNodeGraph("task", "Implement mat"))

	first := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))
	if first["created"] != float64(0) || first["total"] != float64(1) || first["work_item_id"] != id {
		t.Fatalf("first standalone materialize = %#v", first)
	}

	// A revised one-node graph reuses the canonical root identity rather than
	// creating a replacement Work Item.
	for range []int{1, 2} {
		graph := standaloneOneNodeGraph("task", "Implement mat")
		artifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graph))
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", artifact["id"].(string), "approved")
		got := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))
		if got["created"] != float64(0) || got["total"] != float64(1) {
			t.Fatalf("duplicate standalone materialize = %#v", got)
		}
	}
	db := openSQLiteGo(t, dbPath)
	var mappings, children int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_materializations WHERE root_work_item_id=?`, id).Scan(&mappings)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=?`, id).Scan(&children)
	if mappings == 0 || children != 0 {
		t.Fatalf("duplicate materialization mappings=%d children=%d", mappings, children)
	}
	var distinct int
	_ = db.QueryRow(`SELECT COUNT(DISTINCT work_item_id) FROM work_item_materializations WHERE root_work_item_id=?`, id).Scan(&distinct)
	if distinct != 1 {
		t.Fatalf("canonical identity not reused: %d distinct work_item_ids", distinct)
	}

	// A multi-node standalone graph (child decomposition) must be rejected
	// before it can be approved or materialized.
	multi := `{"version":3,"execution_policy":"strict_sequential","nodes":[
		{"key":"T01","type":"task","name":"One","goal":"One","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["a.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["a.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]},
		{"key":"T02","type":"task","name":"Two","goal":"Two","requirement_keys":["REQ-001"],"depends_on":["T01"],"priority":"P1","module":"core","files":["b.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["b.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]}
	]}`
	multiArtifact := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", multi))
	if out := runPicError(t, bin, root, home, "work-item", "artifact-approve", id, "task_graph", multiArtifact["id"].(string), "approved"); !strings.Contains(out, "exactly one matching executable node") {
		t.Fatalf("multi-node standalone graph not rejected: %s", out)
	}
}

// TestInstructionPackLineage verifies that the first Worker claim freezes the
// authorized instruction-pack lineage for a standalone executable.
func TestInstructionPackLineage(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Standalone lineage"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,task_id,requirement_key,title,acceptance_criteria) VALUES('req-lineage','`+id+`','REQ-001','Required','Given valid context
When work runs
Then it completes')`)
	planStandaloneThroughGraph(t, bin, root, home, id, standaloneOneNodeGraph("task", "Implement lineage"))
	asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))
	authorized := asObject(t, runPic(t, bin, root, home, "work-item", "authorize", id, "owner"))
	if authorized["work_item_id"] != id {
		t.Fatalf("standalone authorization = %#v", authorized)
	}

	ready := runPic(t, bin, root, home, "work-item", "ready").([]any)
	if len(ready) != 1 || ready[0].(map[string]any)["id"] != id {
		t.Fatalf("authorized standalone not ready before first claim: %#v", ready)
	}

	// First Worker claim freezes the authorized instruction-pack lineage.
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	packID := fmt.Sprint(claim["instruction_pack_id"])
	packVersion := claim["instruction_pack_version"]
	if packID == "" || packVersion == nil {
		t.Fatalf("first claim did not freeze a pack: %#v", claim)
	}

	db := openSQLiteGo(t, dbPath)
	var firstPackID string
	var packStatus string
	if err := db.QueryRow(`SELECT id,status FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, id).Scan(&firstPackID, &packStatus); err != nil {
		t.Fatalf("no active pack after first claim: %v", err)
	}
	if firstPackID != packID || packStatus != "active" {
		t.Fatalf("lineage mismatch: active=%s frozen=%s status=%s", firstPackID, packID, packStatus)
	}

	// The freeze is singular: exactly one active pack belongs to the Work Item
	// (the lineage is not duplicated on subsequent lifecycle activity).
	var activeCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, id).Scan(&activeCount)
	if activeCount != 1 {
		t.Fatalf("instruction pack lineage not singular: active=%d", activeCount)
	}

	// The frozen content fulfills the requirement (authorized lineage preserved).
	var contentJSON string
	if err := db.QueryRow(`SELECT content_json FROM work_item_instruction_packs WHERE id=?`, packID).Scan(&contentJSON); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Content      instructionPackContent `json:"content"`
		Requirements []requirementSnapshot  `json:"requirements"`
	}
	if err := json.Unmarshal([]byte(contentJSON), &envelope); err != nil {
		t.Fatalf("frozen pack content not canonical: %v", err)
	}
	if envelope.Content.Goal != "Implement lineage" || len(envelope.Requirements) != 1 {
		t.Fatalf("frozen pack content = %#v", envelope)
	}
}

func TestMigrationDropsLegacyRequirementForeignKeys(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// Pre-Work-Item schema: requirements rows carry REFERENCES tasks(id) even
	// though the canonical flow stores wi- identifiers in task_id/epic_id.
	_, err = db.Exec(`CREATE TABLE epics (id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT DEFAULT 'open', created_at TEXT DEFAULT (datetime('now')));
		CREATE TABLE tasks (id TEXT PRIMARY KEY, epic_id TEXT REFERENCES epics(id), title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT DEFAULT 'open', priority TEXT DEFAULT 'medium', created_at TEXT DEFAULT (datetime('now')));
		CREATE TABLE work_items (id TEXT PRIMARY KEY, type TEXT NOT NULL, parent_id TEXT, title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT DEFAULT 'open', priority TEXT DEFAULT 'medium', deferred INTEGER NOT NULL DEFAULT 0, claimed_at TEXT DEFAULT '', claimed_by TEXT DEFAULT '', review_status TEXT DEFAULT 'pending', review_notes TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')));
		CREATE TABLE requirements (id TEXT PRIMARY KEY, task_id TEXT REFERENCES tasks(id) ON DELETE CASCADE, epic_id TEXT REFERENCES epics(id) ON DELETE CASCADE, requirement_key TEXT NOT NULL, title TEXT NOT NULL, description TEXT DEFAULT '', acceptance_criteria TEXT DEFAULT '', status TEXT DEFAULT 'pending', created_at TEXT DEFAULT (datetime('now')));
		INSERT INTO work_items(id,type,title,status) VALUES('wi-old','epic','Old','in_progress');
		INSERT INTO tasks(id,title) VALUES('t-old','Legacy Task');
		INSERT INTO requirements(id,task_id,requirement_key,title) VALUES('req-old','t-old','REQ-OLD','Legacy');`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	read, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	if hasLegacySubjectForeignKey(read, "requirements") || hasLegacySubjectForeignKey(read, "owner_decisions") {
		t.Fatalf("legacy subject foreign keys survived migration on requirements=%v owner_decisions=%v", hasLegacySubjectForeignKey(read, "requirements"), hasLegacySubjectForeignKey(read, "owner_decisions"))
	}
	var key, title string
	if err := read.QueryRow(`SELECT requirement_key,title FROM requirements WHERE id='req-old'`).Scan(&key, &title); err != nil {
		t.Fatalf("legacy requirement lost after FK rebuild: %v", err)
	}
	if key != "REQ-OLD" || title != "Legacy" {
		t.Fatalf("legacy requirement content not preserved: %s %s", key, title)
	}
	// The canonical corrective-bug path inserts requirements bound to a
	// Work Item ID that has no legacy tasks row; this only works when the
	// legacy foreign key is gone.
	if _, err := read.Exec(`INSERT INTO requirements(id,task_id,requirement_key,title,acceptance_criteria) VALUES('req-wi','wi-old','REQ-WI','Bound','Given x
When y
Then z')`); err != nil {
		t.Fatalf("work-item-bound requirement rejected after migration: %v", err)
	}
}
