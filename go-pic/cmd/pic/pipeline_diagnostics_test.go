package main

// Tests for the pipeline-run diagnostics surfaces (RLB-GAP-005/006), split from
// the oversized pic_cli_test.go monolith.

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowPipelineShowReturnsFullRunRow(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Show run diagnostics", "--parent", func() string {
		epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Show parent"))
		return epic["id"].(string)
	}()))
	id := item["id"].(string)
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--base-commit", "deadbeef"))

	shown := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-show", claim["id"].(string)))
	if shown["id"] != claim["id"] || shown["status"] != "claimed" {
		t.Fatalf("pipeline-show row = %#v", shown)
	}
	if shown["base_commit"] != "deadbeef" || shown["error"] == nil {
		t.Fatalf("pipeline-show must expose base_commit and error columns: %#v", shown)
	}
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-show", "pr-missing"); !strings.Contains(out, "no pipeline run") {
		t.Fatalf("pipeline-show missing run = %s", out)
	}
}

func TestWorkflowStatusIncludesIntegrationEvidence(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Evidence parent"))
	feature := asObject(t, runPic(t, bin, root, home, "work-item", "create", "feature", "Evidence feature", "--parent", epic["id"].(string)))
	task := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Evidence child", "--parent", feature["id"].(string)))
	id := task["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")

	status := asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	if evidence, ok := status["integration_evidence"]; !ok || evidence != nil {
		t.Fatalf("lean task with no runs must report integration_evidence=null: %#v", status)
	}

	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker", "--base-commit", "base123"))
	runPic(t, bin, root, home, "workflow", "pipeline-complete", claim["id"].(string), claim["lease_token"].(string), "completed", "--result-json", `{"subagent_state":"completed"}`)
	runSQLite(t, dbPath, `UPDATE pipeline_runs SET artifact_saved_at=datetime('now'),integrated_patch_path='/tmp/patch',integrated_patch_hash='hash123' WHERE id='`+claim["id"].(string)+`'`)
	runSQLite(t, dbPath, `UPDATE pipeline_runs SET result_json='{"subagent_state":"completed","review_status":"passed"}' WHERE id='`+claim["id"].(string)+`'`)

	status = asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	evidence, _ := status["integration_evidence"].(map[string]any)
	if evidence == nil {
		t.Fatalf("integration_evidence missing: %#v", status)
	}
	if evidence["run_id"] != claim["id"] || evidence["integrated_patch_hash"] != "hash123" {
		t.Fatalf("integration_evidence = %#v", evidence)
	}
	if evidence["integrated_at"] != "" {
		t.Fatalf("pre-integration evidence must not fake integrated_at: %#v", evidence)
	}

	// Seed the passing review lineage and integrate: evidence must flip to the
	// full proof state (integrated_at set, review verdict recorded).
	reviewID := "pr-show-review"
	runSQLite(t, dbPath, `INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,candidate_run_id,candidate_patch_hash,result_json,completed_at) VALUES('`+reviewID+`','`+id+`','review',1,'completed','lease-review',datetime('now','+1 hour'),'`+claim["id"].(string)+`','hash123','{"review_status":"passed","candidate_run_id":"`+claim["id"].(string)+`","candidate_patch_hash":"hash123"}',datetime('now'))`)
	runPic(t, bin, root, home, "workflow", "pipeline-checkpoint", claim["id"].(string), claim["lease_token"].(string), "integrated")

	status = asObject(t, runPic(t, bin, root, home, "work-item", "workflow-status", id))
	evidence, _ = status["integration_evidence"].(map[string]any)
	if evidence == nil || evidence["integrated_at"] == "" || evidence["review_run_id"] != reviewID || evidence["review_verdict"] != "passed" {
		t.Fatalf("integrated evidence = %#v", evidence)
	}
}
