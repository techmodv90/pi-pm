package workitem

import (
	"database/sql"
	"strings"
	"testing"
)

func TestApprovedCheckpointDecision(t *testing.T) {
	t.Parallel()
	if got := ApprovedCheckpointDecision("scan"); got != "accepted" {
		t.Errorf("ApprovedCheckpointDecision(scan) = %q, want accepted", got)
	}
	for _, stage := range []string{"vision", "blueprint", "contracts", "task_graph"} {
		if got := ApprovedCheckpointDecision(stage); got != "approved" {
			t.Errorf("ApprovedCheckpointDecision(%s) = %q, want approved", stage, got)
		}
	}
}

func TestIndexOfStage(t *testing.T) {
	t.Parallel()
	stages := []string{"scan", "vision", "task_graph"}
	if got := IndexOfStage(stages, "vision"); got != 1 {
		t.Errorf("IndexOfStage = %d, want 1", got)
	}
	if got := IndexOfStage(stages, "missing"); got != -1 {
		t.Errorf("IndexOfStage missing = %d, want -1", got)
	}
}

func TestIndexOfArtifactStage(t *testing.T) {
	t.Parallel()
	if got := indexOfArtifactStage("rri_t_scenarios"); got != 0 {
		t.Errorf("indexOfArtifactStage = %d, want 0", got)
	}
	if got := indexOfArtifactStage("vision"); got != -1 {
		t.Errorf("indexOfArtifactStage legacy stage = %d, want -1 (lean-only)", got)
	}
}

func TestWorkflowStatus(t *testing.T) {
	t.Run("usage", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "wi-1", "task", nil)
		err := captureVoid(t, func() error { return WorkflowStatus(db, nil) })
		if err == nil || !strings.Contains(err.Error(), "usage") {
			t.Fatalf("WorkflowStatus usage error = %v", err)
		}
	})
	t.Run("aggregate with children reports implement", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "epic-1", "epic", nil)
		insertItem(t, db, "task-1", "task", map[string]any{"parent_id": "epic-1"})
		status := captureStatus(t, db, "epic-1")
		if status["workflow_kind"] != "aggregate_delivery" || status["next_stage"] != "implement" {
			t.Errorf("status = %v, want aggregate_delivery implement", status)
		}
	})
	t.Run("verified descendants report aggregate_verification", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "epic-1", "epic", nil)
		insertItem(t, db, "task-1", "task", map[string]any{"parent_id": "epic-1", "status": "done"})
		status := captureStatus(t, db, "epic-1")
		if status["next_stage"] != "aggregate_verification" {
			t.Errorf("status = %v, want aggregate_verification", status)
		}
	})
	t.Run("executable items report execution state with evidence", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "task-1", "task", nil)
		status := captureStatus(t, db, "task-1")
		if status["workflow_kind"] != "execution" || status["next_stage"] != "instruction_pack" {
			t.Errorf("status = %v, want execution instruction_pack", status)
		}
		if _, ok := status["integration_evidence"]; !ok {
			t.Error("execution status must carry integration_evidence (RLB-GAP-006)")
		}
	})
	t.Run("childless aggregate reports implement", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "epic-1", "epic", nil)
		status := captureStatus(t, db, "epic-1")
		if status["next_stage"] != "implement" {
			t.Errorf("status = %v, want implement for childless aggregate", status)
		}
	})
	t.Run("delivery state drives aggregate stages", func(t *testing.T) {
		tests := []struct {
			name     string
			status   string
			report   string
			decision string
			mode     string
			want     string
		}{
			{name: "done aggregate", status: "done", report: "", decision: "", mode: "coordination", want: "done"},
			{name: "no verification report verifies empty descendants", status: "in_progress", report: "", decision: "", mode: "coordination", want: "aggregate_verification"},
			{name: "pending owner acceptance", status: "in_progress", report: "wivr-1", decision: "", mode: "coordination", want: "owner_acceptance"},
			{name: "accepted branch merge pending", status: "in_progress", report: "wivr-1", decision: "accepted", mode: "branch", want: "merge_pending"},
			{name: "accepted coordination stays acceptance", status: "in_progress", report: "wivr-1", decision: "accepted", mode: "coordination", want: "owner_acceptance"},
			{name: "rejected returns to verification", status: "in_progress", report: "wivr-1", decision: "rejected", mode: "branch", want: "aggregate_verification"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				db := testDB(t)
				insertItem(t, db, "feat-1", "feature", map[string]any{"status": tt.status})
				if _, err := db.Exec(`INSERT INTO work_item_delivery_states (work_item_id, integration_mode, verification_report_id) VALUES ('feat-1', ?, ?)`, tt.mode, tt.report); err != nil {
					t.Fatal(err)
				}
				if tt.decision != "" {
					if _, err := db.Exec(`INSERT INTO work_item_aggregate_owner_decisions (id, work_item_id, verification_report_id, decision) VALUES ('wiaod-1','feat-1','wivr-1',?)`, tt.decision); err != nil {
						t.Fatal(err)
					}
				}
				status := captureStatus(t, db, "feat-1")
				if status["next_stage"] != tt.want {
					t.Errorf("next_stage = %v, want %s", status["next_stage"], tt.want)
				}
				// Documented behavior: with no verification report the delivery branch
				// runs validateAggregateDescendants directly, and a childless feature
				// trivially has zero open descendants, so it reports verification.
			})
		}
	})
}

// captureStatus runs WorkflowStatus and decodes the printed JSON.
func captureStatus(t *testing.T, db *sql.DB, id string) map[string]any {
	t.Helper()
	var out string
	var err error
	out = captureStdout(t, func() { err = WorkflowStatus(db, []string{id}) })
	if err != nil {
		t.Fatalf("WorkflowStatus(%s): %v", id, err)
	}
	return decodeJSON(t, out)
}
