package main

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// profileTestDB opens a fresh in-process database and returns the work item id.
func profileTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	id := "wi-proftest"
	if _, err := db.Exec(`INSERT INTO work_items(id,type,title) VALUES(?,?,?)`, id, "epic", "Profile"); err != nil {
		t.Fatal(err)
	}
	return db, id
}

func countProfiles(t *testing.T, db *sql.DB, id string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM work_item_profiles WHERE work_item_id=?`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestProfileResolutionPersistsExactlyOnce(t *testing.T) {
	db, id := profileTestDB(t)
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := ensureWorkItemProfiles(tx, id)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, name := range lifecycleProfileNames {
		p, ok := profiles[name]
		if !ok {
			t.Fatalf("missing %s profile", name)
		}
		if p.Version != 1 || p.ContentHash == "" {
			t.Fatalf("%s profile not version-bound: %#v", name, p)
		}
	}
	if len(profiles["plan"].Stages) != 6 {
		t.Fatalf("aggregate plan stages = %v", profiles["plan"].Stages)
	}
	if got := profiles["implement"].Stages; len(got) != 1 || got[0] != "worker" {
		t.Fatalf("implement stages = %v", got)
	}
	if got := profiles["qa"].Stages; len(got) != 2 || got[0] != "review" || got[1] != "autofix" {
		t.Fatalf("qa stages = %v", got)
	}
	if countProfiles(t, db, id) != 3 {
		t.Fatalf("expected 3 persisted profiles, got %d", countProfiles(t, db, id))
	}
	firstPlanHash := profiles["plan"].ContentHash

	// Re-resolving must not create new rows or change the immutable binding.
	tx2, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	again, err := ensureWorkItemProfiles(tx2, id)
	if err != nil {
		tx2.Rollback()
		t.Fatal(err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatal(err)
	}
	if again["plan"].Version != 1 || again["plan"].ContentHash != firstPlanHash {
		t.Fatalf("profile re-resolved to a new identity: %#v", again["plan"])
	}
	if countProfiles(t, db, id) != 3 {
		t.Fatalf("profile resolution not idempotent: %d rows", countProfiles(t, db, id))
	}
}

func TestProfileResolutionRejectsUnknownDepth(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	// Simulate a stale or manually edited database that predates the planning
	// depth CHECK constraint, so an unknown depth can actually reach the
	// defensive resolver and must be rejected without partial profile rows.
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE work_items (id TEXT PRIMARY KEY, type TEXT NOT NULL, parent_id TEXT, title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT DEFAULT 'open', priority TEXT DEFAULT 'medium', deferred INTEGER NOT NULL DEFAULT 0, claimed_at TEXT DEFAULT '', claimed_by TEXT DEFAULT '', review_status TEXT DEFAULT 'pending', review_notes TEXT DEFAULT '', planning_depth TEXT DEFAULT 'full', created_at TEXT DEFAULT (datetime('now'))); INSERT INTO work_items(id,type,title,planning_depth) VALUES('wi-bad','epic','Bad','bogus');`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
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
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, err = ensureWorkItemProfiles(tx, "wi-bad")
	if err == nil || !strings.Contains(err.Error(), "invalid persisted planning depth") {
		tx.Rollback()
		t.Fatalf("expected depth validation error, got %v", err)
	}
	tx.Rollback()
	if countProfiles(t, db, "wi-bad") != 0 {
		t.Fatalf("rejected resolution left partial profile rows")
	}
}

func TestPlanningDepthSelectsAggregateStages(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expect := map[string][]string{
		"quick":    {"scan", "rri", "task_graph"},
		"standard": {"scan", "rri", "task_graph"},
		"designed": {"scan", "rri", "blueprint", "task_graph"},
		"full":     {"scan", "rri", "vision", "blueprint", "contracts", "task_graph"},
	}
	for depth, want := range expect {
		id := "wi-" + depth
		if _, err := db.Exec(`INSERT INTO work_items(id,type,title,planning_depth) VALUES(?,?,?,?)`, id, "epic", depth, depth); err != nil {
			t.Fatal(err)
		}
		stages, foundDepth, _, _, err := computePlanStagesForWorkItem(db, id)
		if err != nil {
			t.Fatal(err)
		}
		if foundDepth != depth || len(stages) != len(want) {
			t.Fatalf("depth %s stages = %v", depth, stages)
		}
		for i := range want {
			if stages[i] != want[i] {
				t.Fatalf("depth %s stages[%d] = %v, want %v", depth, i, stages, want)
			}
		}
		// RRI and Task Graph are always mandatory aggregates.
		if !contains(stages, "rri") || !contains(stages, "task_graph") {
			t.Fatalf("depth %s missing mandatory rri/task_graph: %v", depth, stages)
		}
	}
}

func TestPlanningDepthStandaloneIsFixedLeanProfile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, depth := range validPlanningDepths {
		id := "wi-lean-" + depth
		if _, err := db.Exec(`INSERT INTO work_items(id,type,title,planning_depth) VALUES(?,?,?,?)`, id, "task", depth, depth); err != nil {
			t.Fatal(err)
		}
		stages, err := planningStagesForWorkItem(db, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(stages) != 3 || stages[0] != "scan" || stages[1] != "rri" || stages[2] != "task_graph" {
			t.Fatalf("standalone depth %s stages = %v", depth, stages)
		}
	}
}

func TestPipelineStageProfileBinding(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Bind profile"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")

	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "scan"))
	if claim["stage"] != "scan" || toInt(claim["profile_version"]) != 1 {
		t.Fatalf("scan claim did not bind profile: %#v", claim)
	}
	if persistedText(claim["profile_hash"]) == "" {
		t.Fatalf("scan claim missing profile hash: %#v", claim)
	}
	expectedHash := persistedText(claim["profile_hash"])

	// Stale profile hash is rejected.
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "rri", "--profile-hash", "deadbeef"); !strings.Contains(out, "profile hash changed") {
		t.Fatalf("stale profile hash accepted: %s", out)
	}
	// Approve scan so rri is the eligible next planning stage.
	runSQLite(t, dbPath, `INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES('wia-bind-scan','`+id+`','scan',1,'<scan/>','scan-bind-hash'); INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-bind-scan','`+id+`','scan','wia-bind-scan',1,'scan-bind-hash','accepted');`)
	// Correct bound hash is accepted for the next eligible stage.
	bound := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "rri", "--profile-hash", expectedHash))
	if bound["profile_version"] != float64(1) {
		t.Fatalf("rri claim = %#v", bound)
	}

	// The three lifecycle profiles were persisted.
	if count := countProfiles(t, openSQLiteGo(t, dbPath), id); count != 3 {
		t.Fatalf("expected 3 persisted profiles, got %d", count)
	}
}

func openSQLiteGo(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestPipelineStageProfileMismatchRejected(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Mismatch", "--planning-depth", "standard"))
	id := item["id"].(string)

	// standard depth resolves to scan, rri, task_graph only.
	asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "scan"))
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "vision"); !strings.Contains(out, "not part of this Work Item planning profile") {
		t.Fatalf("out-of-profile vision stage accepted: %s", out)
	}
}

func TestPipelineStageInvalidPredecessorRejected(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Ordered"))
	id := item["id"].(string)
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "rri"); !strings.Contains(out, "current planning stage is scan") {
		t.Fatalf("invalid predecessor accepted: %s", out)
	}
}

func TestLegacyPipelineStageVocabularyMigrated(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	// Build a legacy pipeline_runs with the retired stage vocabulary
	// (scan,worker,review,qa,verify); the canonical CHECK rejects 'qa'/'verify',
	// so migration must translate them instead of failing the rebuild copy.
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE work_items (id TEXT PRIMARY KEY, type TEXT NOT NULL, parent_id TEXT, title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT DEFAULT 'open', priority TEXT DEFAULT 'medium', deferred INTEGER NOT NULL DEFAULT 0, claimed_at TEXT DEFAULT '', claimed_by TEXT DEFAULT '', review_status TEXT DEFAULT 'pending', review_notes TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')));
		INSERT INTO work_items(id,type,title,status) VALUES('wi-legacy','task','Legacy','in_progress');
		CREATE TABLE pipeline_runs (id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, stage TEXT NOT NULL CHECK(stage IN ('scan','worker','review','qa','verify')), attempt INTEGER NOT NULL, status TEXT NOT NULL DEFAULT 'claimed', lease_token TEXT NOT NULL, lease_expires_at TEXT NOT NULL, created_at TEXT DEFAULT (datetime('now')));
		INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at) VALUES
			('pr-legacy-qa','wi-legacy','qa',1,'failed','lease-qa',datetime('now','+1 hour')),
			('pr-legacy-verify','wi-legacy','verify',2,'failed','lease-verify',datetime('now','+1 hour'));	`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	if err := initDB(dbPath); err != nil {
		t.Fatalf("migration failed on legacy stage vocabulary: %v", err)
	}
	read, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	var stage string
	if err := read.QueryRow(`SELECT stage FROM pipeline_runs WHERE id='pr-legacy-qa'`).Scan(&stage); err != nil {
		t.Fatalf("legacy qa run lost after migration: %v", err)
	}
	if stage != "autofix" {
		t.Fatalf("legacy qa stage = %q, want autofix", stage)
	}
	if err := read.QueryRow(`SELECT stage FROM pipeline_runs WHERE id='pr-legacy-verify'`).Scan(&stage); err != nil {
		t.Fatalf("legacy verify run lost after migration: %v", err)
	}
	if stage != "review" {
		t.Fatalf("legacy verify stage = %q, want review", stage)
	}
	rows, err := queryMaps(read, `SELECT * FROM pipeline_runs WHERE task_id=?`, "wi-legacy")
	if err != nil || len(rows) != 2 {
		t.Fatalf("legacy runs not fully preserved: rows=%d err=%v", len(rows), err)
	}
}

func TestLegacyPipelineRowsRemainReadableAfterMigration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	// Build a legacy schema that predates the profile columns.
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE work_items (id TEXT PRIMARY KEY, type TEXT NOT NULL, parent_id TEXT, title TEXT NOT NULL, description TEXT DEFAULT '', status TEXT DEFAULT 'open', priority TEXT DEFAULT 'medium', deferred INTEGER NOT NULL DEFAULT 0, claimed_at TEXT DEFAULT '', claimed_by TEXT DEFAULT '', review_status TEXT DEFAULT 'pending', review_notes TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')));
		INSERT INTO work_items(id,type,title,status) VALUES('wi-legacy','epic','Legacy','in_progress');
		CREATE TABLE pipeline_runs (id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, stage TEXT NOT NULL, attempt INTEGER NOT NULL, status TEXT NOT NULL DEFAULT 'claimed', lease_token TEXT NOT NULL, lease_expires_at TEXT NOT NULL, created_at TEXT DEFAULT (datetime('now')));
		INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at) VALUES('pr-legacy','wi-legacy','worker',1,'claimed','lease-legacy',datetime('now','+1 hour'));`)
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
	if !hasColumn(read, "pipeline_runs", "profile_version") || !hasColumn(read, "pipeline_runs", "profile_hash") || !hasColumn(read, "work_items", "planning_depth") {
		t.Fatalf("migration did not add additive profile columns")
	}
	var status, stage string
	var profileVersion int
	if err := read.QueryRow(`SELECT status,stage,COALESCE(profile_version,0) FROM pipeline_runs WHERE id='pr-legacy'`).Scan(&status, &stage, &profileVersion); err != nil {
		t.Fatalf("legacy run lost after migration: %v", err)
	}
	if status != "claimed" || stage != "worker" || profileVersion != 0 {
		t.Fatalf("legacy run content not preserved: status=%s stage=%s profile_version=%d", status, stage, profileVersion)
	}
	// The legacy run remains visible through the canonical runs query.
	rows, err := queryMaps(read, `SELECT * FROM pipeline_runs WHERE task_id=?`, "wi-legacy")
	if err != nil || len(rows) != 1 {
		t.Fatalf("legacy run not readable: rows=%d err=%v", len(rows), err)
	}
	var depth string
	if err := read.QueryRow(`SELECT COALESCE(planning_depth,'') FROM work_items WHERE id='wi-legacy'`).Scan(&depth); err != nil || depth != "full" {
		t.Fatalf("migrated legacy depth = %q err=%v", depth, err)
	}
}

func TestLegacyPipelineProfileListReadOnly(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "feature", "List profiles"))
	id := item["id"].(string)
	// Resolution happens on first claim even without any stage completing.
	asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "scan"))
	out := runMarkdown(t, bin, root, home, "workflow", "profile-list", id)
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("profile-list not JSON: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("profile-list rows = %d", len(rows))
	}
}
