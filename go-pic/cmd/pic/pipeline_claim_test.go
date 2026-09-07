package main

import (
	"path/filepath"
	"strings"
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

func TestLeanClaimEndToEnd(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	dbPath := filepath.Join(root, ".pi", "tasks.db")

	// Legacy task reaches its legacy gates in the same store.
	legacy := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Legacy task"))
	legacyID := legacy["id"].(string)
	seedLegacyPipelineState(t, dbPath, legacyID)
	legacyClaim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", legacyID, "worker"))
	if legacyClaim["instruction_pack_id"] != "pk-legacy" {
		t.Fatalf("legacy claim must bind the active pack: %#v", legacyClaim["instruction_pack_id"])
	}

	// Lean task: claim, then read back through pic show.
	bare := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Lean task"))
	bareID := bare["id"].(string)
	asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", bareID, "worker", "--claimant", "worker-a"))

	shown := asObject(t, runPic(t, bin, root, home, "show", bareID))
	db := openSQLiteGo(t, dbPath)
	wi := asObject(t, shown["work_item"])
	if wi["status"] != "in_progress" || wi["claimed_by"] != "worker-a" {
		t.Fatalf("show readback = status=%v claimed_by=%v", wi["status"], wi["claimed_by"])
	}
	if claimedAt, _ := wi["claimed_at"].(string); claimedAt == "" {
		t.Fatalf("show readback missing claimed_at: %#v", wi["claimed_at"])
	}
	// The activity log (work_item_events) is not part of the show payload;
	// assert the claimed event directly in the store.
	var claimEvents int
	if err := db.QueryRow(`SELECT COUNT(*) FROM work_item_events WHERE work_item_id=? AND event_type='claimed'`, bareID).Scan(&claimEvents); err != nil {
		t.Fatal(err)
	}
	if claimEvents != 1 {
		t.Fatalf("activity log claimed events = %d, want 1", claimEvents)
	}
}

// leanWorkerRunEvidence marks a claimed lean worker run completed and
// integrated, mirroring the canonical evidence fixture without pack columns.
func leanWorkerRunEvidence(t *testing.T, dbPath, runID string) {
	t.Helper()
	runSQLite(t, dbPath, `UPDATE pipeline_runs SET status='completed',artifact_saved_at=datetime('now'),integrated_patch_path='lean.patch',integrated_patch_hash='lean-hash',integrated_at=datetime('now'),completed_at=datetime('now') WHERE id='`+runID+`';`)
}

func TestLeanClaimCompletion(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Lean task"))
	id := item["id"].(string)
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	runID := claim["id"].(string)
	leanWorkerRunEvidence(t, dbPath, runID)

	completed := asObject(t, runPic(t, bin, root, home, "work-item", "completion-save", id, "done", "--pipeline-run-id", runID, "--summary", "lean done"))
	if completed["id"] != runID {
		t.Fatalf("lean completion output = %#v", completed["id"])
	}

	db := openSQLiteGo(t, dbPath)
	var status string
	if err := db.QueryRow(`SELECT status FROM work_items WHERE id=?`, id).Scan(&status); err != nil || status != "done" {
		t.Fatalf("lean completion status=%q err=%v", status, err)
	}
	var events, reports int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM work_item_events WHERE work_item_id=? AND event_type='completed'),(SELECT COUNT(*) FROM work_item_completion_reports WHERE work_item_id=?)`, id, id).Scan(&events, &reports); err != nil {
		t.Fatal(err)
	}
	if events != 1 || reports != 0 {
		t.Fatalf("lean completion events=%d reports=%d, want 1/0", events, reports)
	}
}

func TestLeanClaimReviewVerification(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Lean task"))
	id := item["id"].(string)
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	runID := claim["id"].(string)
	leanWorkerRunEvidence(t, dbPath, runID)

	// Review claim (lean) on the completed candidate, then a failed verdict.
	reviewClaim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "review"))
	reviewRunID := reviewClaim["id"].(string)
	reviewClaim["id"] = reviewRunID
	runSQLite(t, dbPath, `UPDATE pipeline_runs SET status='completed',result_json='{"review_status":"failed","candidate_run_id":"`+runID+`","candidate_patch_hash":"lean-hash"}',completed_at=datetime('now') WHERE id='`+reviewRunID+`';`)
	asObject(t, runPic(t, bin, root, home, "work-item", "review", id, "failed", "--notes", "needs work", "--pipeline-run-id", reviewRunID))

	// A failed review routes a fix attempt without pack-hash cycle caps.
	fixClaim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--review-fix", "1"))
	if fixClaim["candidate_run_id"] != runID {
		t.Fatalf("lean review-fix must bind the rejected candidate: %#v", fixClaim["candidate_run_id"])
	}

	// Pass the review, complete, and verify: records reference task+run without
	// pack columns.
	runSQLite(t, dbPath, `UPDATE pipeline_runs SET result_json='{"review_status":"passed","candidate_run_id":"`+runID+`","candidate_patch_hash":"lean-hash"}' WHERE id='`+reviewRunID+`';`)
	asObject(t, runPic(t, bin, root, home, "work-item", "review", id, "passed", "--notes", "accepted", "--pipeline-run-id", reviewRunID))
	asObject(t, runPic(t, bin, root, home, "work-item", "completion-save", id, "done", "--pipeline-run-id", runID, "--summary", "lean done"))
	verified := asObject(t, runPic(t, bin, root, home, "work-item", "verification-save", id, reviewRunID, "passed", "lean checks passed", "--actor-role", "contractor"))

	db := openSQLiteGo(t, dbPath)
	var status string
	if err := db.QueryRow(`SELECT status FROM work_items WHERE id=?`, id).Scan(&status); err != nil || status != "done" {
		t.Fatalf("post-verification status=%q err=%v", status, err)
	}
	var crID string
	if err := db.QueryRow(`SELECT COALESCE(completion_report_id,'') FROM work_item_verification_reports WHERE id=?`, verified["id"].(string)).Scan(&crID); err != nil {
		t.Fatal(err)
	}
	if crID != "" {
		t.Fatalf("lean verification must not reference a completion report: %q", crID)
	}
	if !strings.Contains(verified["summary"].(string), reviewRunID) {
		t.Fatalf("lean verification must reference the run: %#v", verified["summary"])
	}
}
