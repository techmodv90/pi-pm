package main

import (
	"path/filepath"
	"testing"
)

// seedLegacyPipelineState inserts the deprecated-pipeline state (approved
// task_graph checkpoint, self-materialization, authorization, active
// instruction pack) directly into the test store so the legacy claim route
// can be exercised without walking the whole planning pipeline.
func seedLegacyPipelineState(t *testing.T, dbPath, id string) {
	t.Helper()
	runSQLite(t, dbPath, `
INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('cp-legacy','`+id+`','task_graph','art-legacy',1,'legacy-hash','approved');
INSERT INTO work_item_materializations(root_work_item_id,checkpoint_id,node_key,work_item_id) VALUES('`+id+`','cp-legacy','N1','`+id+`');
INSERT INTO implementation_authorizations(id,work_item_id,task_graph_checkpoint_id,authorized_by) VALUES('ia-legacy','`+id+`','cp-legacy','owner');
INSERT INTO work_item_instruction_packs(id,work_item_id,checkpoint_id,version,status,content_json,content_hash,activated_at) VALUES('pk-legacy','`+id+`','cp-legacy',1,'active','{}','legacy-pack-hash','2026-01-01 00:00:00');`)
}

func TestPipelineClaimFixture(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	dbPath := filepath.Join(root, ".pi", "tasks.db")

	bare := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Lean task"))
	bareID := bare["id"].(string)

	legacy := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Legacy task"))
	legacyID := legacy["id"].(string)
	seedLegacyPipelineState(t, dbPath, legacyID)

	db := openSQLiteGo(t, dbPath)
	var barePacks, bareMats, legacyPacks, legacyMats int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=?),(SELECT COUNT(*) FROM work_item_materializations WHERE work_item_id=?)`, bareID, bareID).Scan(&barePacks, &bareMats); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=?),(SELECT COUNT(*) FROM work_item_materializations WHERE work_item_id=?)`, legacyID, legacyID).Scan(&legacyPacks, &legacyMats); err != nil {
		t.Fatal(err)
	}
	if barePacks != 0 || bareMats != 0 {
		t.Fatalf("bare task must carry no legacy state: packs=%d mats=%d", barePacks, bareMats)
	}
	if legacyPacks != 1 || legacyMats != 1 {
		t.Fatalf("legacy seed incomplete: packs=%d mats=%d", legacyPacks, legacyMats)
	}
}

func TestLeanClaimRouting(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	dbPath := filepath.Join(root, ".pi", "tasks.db")

	legacy := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Legacy task"))
	legacyID := legacy["id"].(string)
	seedLegacyPipelineState(t, dbPath, legacyID)
	// Legacy-state task: the legacy claim path must still succeed unchanged.
	asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", legacyID, "worker"))

	// Bare task: no pack, no materialization — must take the lean branch, not
	// be rejected by the exactly-one-active-pack gate.
	bare := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Lean task"))
	bareID := bare["id"].(string)
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", bareID, "worker"))
	if claim["task_id"] != bareID || claim["stage"] != "worker" {
		t.Fatalf("lean claim output = %#v", claim)
	}
}

func TestLeanClaimClaim(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Lean task"))
	id := item["id"].(string)

	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--claimant", "contractor"))

	db := openSQLiteGo(t, dbPath)
	var status, claimedAt, claimedBy string
	if err := db.QueryRow(`SELECT status,claimed_at,claimed_by FROM work_items WHERE id=?`, id).Scan(&status, &claimedAt, &claimedBy); err != nil {
		t.Fatal(err)
	}
	if status != "in_progress" || claimedAt == "" || claimedBy != "contractor" {
		t.Fatalf("lean claim state = status=%q claimed_at=%q claimed_by=%q", status, claimedAt, claimedBy)
	}
	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM work_item_events WHERE work_item_id=? AND event_type='claimed'`, id).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("claimed events = %d, want 1", events)
	}
	runID := claim["id"].(string)
	var packID, packHash string
	if err := db.QueryRow(`SELECT instruction_pack_id,instruction_pack_hash FROM pipeline_runs WHERE id=?`, runID).Scan(&packID, &packHash); err != nil {
		t.Fatal(err)
	}
	if packID != "" || packHash != "" {
		t.Fatalf("lean run must carry no pack columns: id=%q hash=%q", packID, packHash)
	}
	// No pack, checkpoint, or materialization rows may be created by a lean claim.
	var packs, checkpoints, mats int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=?),(SELECT COUNT(*) FROM workflow_checkpoints WHERE work_item_id=?),(SELECT COUNT(*) FROM work_item_materializations WHERE work_item_id=?)`, id, id, id).Scan(&packs, &checkpoints, &mats); err != nil {
		t.Fatal(err)
	}
	if packs != 0 || checkpoints != 0 || mats != 0 {
		t.Fatalf("lean claim created legacy state: packs=%d checkpoints=%d mats=%d", packs, checkpoints, mats)
	}
}

func TestLeanClaimSingleWriter(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Lean task"))
	id := item["id"].(string)

	asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	errOut := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "worker")
	if errOut == "" {
		t.Fatal("second lean claim must be rejected")
	}

	db := openSQLiteGo(t, dbPath)
	var runs, events int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM pipeline_runs WHERE task_id=? AND stage='worker'),(SELECT COUNT(*) FROM work_item_events WHERE work_item_id=? AND event_type='claimed')`, id, id).Scan(&runs, &events); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || events != 1 {
		t.Fatalf("rejected second claim must write nothing: runs=%d events=%d", runs, events)
	}
}
