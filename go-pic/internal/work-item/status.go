package workitem

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
)

func ApprovedCheckpointDecision(stage string) string {
	if stage == "scan" {
		return "accepted"
	}
	return "approved"
}
func WorkflowStatus(db *sql.DB, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: pic work-item workflow-status <id>")
	}
	item, err := ByID(db, args[0])
	if err != nil {
		return err
	}
	if status, ok, statusErr := aggregateDeliveryWorkflowStatus(db, args[0]); statusErr != nil {
		return statusErr
	} else if ok {
		writeJSON(os.Stdout, WithNextActions(status))
		return nil
	}
	var childCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=?`, args[0]).Scan(&childCount)
	if childCount > 0 {
		next := "implement"
		tx, beginErr := db.Begin()
		if beginErr != nil {
			return beginErr
		}
		if validateAggregateDescendants(tx, args[0]) == nil {
			next = "aggregate_verification"
		}
		_ = tx.Rollback()
		writeJSON(os.Stdout, WithNextActions(map[string]any{"work_item_id": args[0], "workflow_kind": "aggregate_delivery", "next_stage": next}))
		return nil
	}
	if contains([]string{"task", "bug", "chore"}, fmt.Sprint(item["type"])) {
		return executionStatus(db, args[0])
	}
	// Childless aggregate under the lean model: no planning flow remains, so
	// the only meaningful next step is gaining executable children.
	writeJSON(os.Stdout, WithNextActions(map[string]any{"work_item_id": args[0], "workflow_kind": "aggregate_delivery", "next_stage": "implement"}))
	return nil
}
func IndexOfStage(stages []string, stage string) int {
	for index, candidate := range stages {
		if candidate == stage {
			return index
		}
	}
	return -1
}
func executionStatus(db *sql.DB, id string) error {
	state, err := LoadExecutionState(db, id)
	if err != nil {
		return err
	}
	graph := map[string]any{}
	var artifactID, checkpointID, decision string
	var revision int
	if db.QueryRow(`SELECT a.id,a.revision,c.id,c.decision_type FROM work_item_artifacts a LEFT JOIN workflow_checkpoints c ON c.artifact_id=a.id AND c.artifact_revision=a.revision WHERE a.work_item_id=(SELECT COALESCE(parent_id,id) FROM work_items WHERE id=?) AND a.stage='task_graph' ORDER BY a.revision DESC LIMIT 1`, id).Scan(&artifactID, &revision, &checkpointID, &decision) == nil {
		graph = map[string]any{"artifact_id": artifactID, "revision": revision, "checkpoint_id": checkpointID, "decision": decision}
	}
	// Integration proof surface (RLB-GAP-006): contractor verification needs
	// the persisted evidence that the reviewed candidate actually integrated —
	// integrated_at/hash plus the passing review lineage — without direct SQL.
	writeJSON(os.Stdout, WithNextActions(map[string]any{"work_item_id": id, "workflow_kind": "execution", "next_stage": state.NextStage, "pipeline_stage": state.PipelineStage, "active_instruction_pack_id": state.PackID, "candidate_run_id": state.CandidateID, "review_status": state.ReviewStatus, "completion_report_id": state.CompletionID, "verification_status": state.VerificationStatus, "owner_decision": state.OwnerDecision, "current_task_graph": graph, "integration_evidence": IntegrationEvidence(db, id)}))
	return nil
}
func indexOfArtifactStage(stage string) int {
	for index, candidate := range Stages {
		if candidate == stage {
			return index
		}
	}
	return -1
}
func aggregateDeliveryWorkflowStatus(db *sql.DB, id string) (map[string]any, bool, error) {
	delivery, err := queryMaps(db, `SELECT * FROM work_item_delivery_states WHERE work_item_id=?`, id)
	if err != nil || len(delivery) == 0 {
		return nil, false, err
	}
	state := delivery[0]
	next := "implement"
	var itemStatus string
	if err = db.QueryRow(`SELECT status FROM work_items WHERE id=?`, id).Scan(&itemStatus); err != nil {
		return nil, false, err
	}
	if itemStatus == "done" {
		next = "done"
	} else if fmt.Sprint(state["verification_report_id"]) != "" {
		var decision string
		_ = db.QueryRow(`SELECT decision FROM work_item_aggregate_owner_decisions WHERE work_item_id=? AND verification_report_id=? ORDER BY rowid DESC LIMIT 1`, id, state["verification_report_id"]).Scan(&decision)
		if decision == "accepted" && fmt.Sprint(state["integration_mode"]) == "branch" {
			next = "merge_pending"
		} else if decision == "rejected" {
			next = "aggregate_verification"
		} else {
			next = "owner_acceptance"
		}
	} else {
		tx, beginErr := db.Begin()
		if beginErr != nil {
			return nil, false, beginErr
		}
		if validateAggregateDescendants(tx, id) == nil {
			next = "aggregate_verification"
		}
		_ = tx.Rollback()
	}
	state["work_item_id"] = id
	state["workflow_kind"] = "aggregate_delivery"
	state["next_stage"] = next
	return state, true, nil
}

// RRI-T scenario identity constraint: the canonical dedupe key is the id-based
// identity (dimension|stress_axis|requirement_id|id) shared with the TypeScript
// grading compiler — never the authoring persona — so two persisted scenarios may
// share persona, dimension, stress axis, and requirement while staying distinct
// by id. Deferred dispositions (the compiled not_applicable records) deduplicate
// on the same identity, so one persisted scenario can be deferred at most once.
