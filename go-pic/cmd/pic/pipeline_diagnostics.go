package main

// Pipeline run diagnostics and integration-proof surfaces extracted from the
// oversized pipeline.go / work_items.go monoliths (RLB-GAP-005/006 fixes).

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// workflowPipelineShow returns the full pipeline_runs row for one run id.
func workflowPipelineShow(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("pipeline-show requires pipeline run id")
	}
	// Terminal-run diagnostics surface (RLB-GAP-005): pipeline-active only
	// returns claimed/running runs, so blocked/failed/completed run error
	// reasons had no sanctioned read path.
	rows, err := queryMaps(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no pipeline run found: %s", args[0])
	}
	writeJSON(os.Stdout, rows[0])
	return nil
}

// workItemIntegrationEvidence answers "did the reviewed candidate actually
// integrate?" from persisted state only (RLB-GAP-006): integrated_at/hash plus
// the passing review lineage, so contractor verification needs no direct SQL.
func workItemIntegrationEvidence(db *sql.DB, id string) map[string]any {
	runs, err := queryMaps(db, `SELECT id, stage, status, artifact_saved_at, integrated_at, integrated_patch_hash, integrated_patch_path FROM pipeline_runs WHERE task_id=? AND stage IN ('worker','autofix') ORDER BY rowid DESC LIMIT 1`, id)
	if err != nil || len(runs) == 0 {
		return nil
	}
	run := runs[0]
	evidence := map[string]any{
		"run_id":                run["id"],
		"stage":                 run["stage"],
		"status":                run["status"],
		"artifact_saved_at":     run["artifact_saved_at"],
		"integrated_at":         run["integrated_at"],
		"integrated_patch_hash": run["integrated_patch_hash"],
		"integrated_patch_path": run["integrated_patch_path"],
	}
	reviews, err := queryMaps(db, `SELECT id, status, result_json FROM pipeline_runs WHERE task_id=? AND stage='review' AND candidate_run_id=? ORDER BY rowid DESC LIMIT 1`, id, run["id"])
	if err == nil && len(reviews) > 0 {
		evidence["review_run_id"] = reviews[0]["id"]
		evidence["review_status"] = reviews[0]["status"]
		var parsed struct {
			ReviewStatus string `json:"review_status"`
		}
		if json.Unmarshal([]byte(fmt.Sprint(reviews[0]["result_json"])), &parsed) == nil && parsed.ReviewStatus != "" {
			evidence["review_verdict"] = parsed.ReviewStatus
		}
	}
	return evidence
}
