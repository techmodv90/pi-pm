package workitem

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Lean artifact stages: rri_t_scenarios is the one retained supplementary
// artifact (saved by the in-session contractor during /apm review; never
// gated by a workflow checkpoint).
var Stages = []string{"rri_t_scenarios"}

func SaveArtifact(db *sql.DB, args []string) error {
	if len(args) != 3 || !contains(Stages, args[1]) || args[2] == "" {
		return errors.New("usage: pic work-item artifact-save <id> <stage> <content>")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = byIDTx(tx, args[0]); err != nil {
		return err
	}
	var revision int
	if err = tx.QueryRow(`SELECT COALESCE(MAX(revision),0)+1 FROM work_item_artifacts WHERE work_item_id=? AND stage=?`, args[0], args[1]).Scan(&revision); err != nil {
		return err
	}
	id, contentHash := "wia-"+shortID(), hashJSON(args[2])
	if _, err = tx.Exec(`INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES(?,?,?,?,?,?)`, id, args[0], args[1], revision, args[2], contentHash); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	writeJSON(os.Stdout, map[string]any{"id": id, "work_item_id": args[0], "stage": args[1], "revision": revision, "content_hash": contentHash})
	return nil
}

// RRI-T scenario identity constraint: the canonical dedupe key is the id-based
// identity (dimension|stress_axis|requirement_id|id) shared with the TypeScript
// grading compiler — never the authoring persona — so two persisted scenarios may
// share persona, dimension, stress axis, and requirement while staying distinct
// by id. Deferred dispositions (the compiled not_applicable records) deduplicate
// on the same identity, so one persisted scenario can be deferred at most once.
func validateRriTVerification(db *sql.DB, workItemID, aggregateStatus, content string) error {
	var report struct {
		Scenarios []struct {
			ID            string `json:"id"`
			Persona       string `json:"persona"`
			Dimension     string `json:"dimension"`
			StressAxis    string `json:"stress_axis"`
			RequirementID string `json:"requirement_id"`
			Procedure     string `json:"procedure"`
			Evidence      string `json:"evidence"`
			Result        string `json:"result"`
		} `json:"scenarios"`
		NotApplicable []struct {
			ID            string `json:"id"`
			Persona       string `json:"persona"`
			Dimension     string `json:"dimension"`
			StressAxis    string `json:"stress_axis"`
			RequirementID string `json:"requirement_id"`
			Reason        string `json:"reason"`
		} `json:"not_applicable"`
	}
	if err := json.Unmarshal([]byte(content), &report); err != nil {
		return fmt.Errorf("invalid RRI-T evidence JSON: %w", err)
	}
	validDimensions := map[string]bool{"D1": true, "D2": true, "D3": true, "D4": true, "D5": true, "D6": true, "D7": true}
	validStressAxes := map[string]bool{"TIME": true, "DATA": true, "ERROR": true, "COLLABORATION": true, "EMERGENCY": true, "SCALE": true, "COMPLIANCE": true, "EVOLUTION": true}
	validResults := map[string]bool{"PASS": true, "ACCEPTABLE": true, "PAINFUL": true, "FAIL": true}
	rows, err := db.Query(`SELECT requirement_key FROM requirements WHERE (task_id=? OR epic_id=?)`, workItemID, workItemID)
	if err != nil {
		return err
	}
	defer rows.Close()
	approved := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return err
		}
		approved[key] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, scenario := range report.Scenarios {
		if scenario.ID == "" || scenario.Persona == "" || !validDimensions[scenario.Dimension] || !validStressAxes[scenario.StressAxis] || scenario.Procedure == "" || scenario.Evidence == "" || !validResults[scenario.Result] {
			return fmt.Errorf("invalid RRI-T scenario for requirement %s", scenario.RequirementID)
		}
		// Lean imported aggregates carry no requirements rows — their requirement
		// truth lives in the companion spec files (.feature scenarios / the
		// .tasks.md Nyquist mapping), re-read by the contractor at review time.
		// The DB gate applies only to planning-era aggregates that have rows.
		if scenario.RequirementID == "" {
			return fmt.Errorf("RRI-T scenario %s requires a requirement_id", scenario.ID)
		}
		if len(approved) > 0 && !approved[scenario.RequirementID] {
			return fmt.Errorf("invalid RRI-T scenario for requirement %s", scenario.RequirementID)
		}
		key := scenario.Dimension + "|" + scenario.StressAxis + "|" + scenario.RequirementID + "|" + scenario.ID
		if seen[key] {
			return fmt.Errorf("duplicate RRI-T scenario %s", key)
		}
		seen[key] = true
		if aggregateStatus == "passed" && scenario.Result != "PASS" {
			return fmt.Errorf("RRI-T %s result requires remediation or owner deferral before aggregate passage", scenario.Result)
		}
	}
	for _, deferred := range report.NotApplicable {
		if deferred.ID == "" {
			// Authored N/A topics carry no scenario identity; only the concrete reason is required.
			if strings.TrimSpace(deferred.Reason) == "" {
				return fmt.Errorf("RRI-T not_applicable disposition requires a concrete reason")
			}
			continue
		}
		if deferred.Persona == "" || !validDimensions[deferred.Dimension] || !validStressAxes[deferred.StressAxis] || strings.TrimSpace(deferred.Reason) == "" {
			return fmt.Errorf("invalid RRI-T not_applicable disposition for requirement %s", deferred.RequirementID)
		}
		if deferred.RequirementID == "" {
			return fmt.Errorf("RRI-T not_applicable disposition %s requires a requirement_id", deferred.ID)
		}
		if len(approved) > 0 && !approved[deferred.RequirementID] {
			return fmt.Errorf("invalid RRI-T not_applicable disposition for requirement %s", deferred.RequirementID)
		}
		key := deferred.Dimension + "|" + deferred.StressAxis + "|" + deferred.RequirementID + "|" + deferred.ID
		if seen[key] {
			return fmt.Errorf("duplicate RRI-T scenario %s", key)
		}
		seen[key] = true
	}
	return nil
}

// IntegrationEvidence answers "did the reviewed candidate actually
// integrate?" from persisted state only (RLB-GAP-006): integrated_at/hash plus
// the passing review lineage, so contractor verification needs no direct SQL.
func IntegrationEvidence(db *sql.DB, id string) map[string]any {
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
