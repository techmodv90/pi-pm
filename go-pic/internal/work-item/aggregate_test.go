package workitem

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/earendil-works/task-system/go-pic/internal/tip"
)

func TestValidateAggregateDescendants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		target  string
		items   map[string]string // id -> status
		parents map[string]string // id -> parent
		wantErr string
	}{
		{
			name: "all descendants done", target: "epic-1",
			items:   map[string]string{"epic-1": "open", "task-1": "done"},
			parents: map[string]string{"task-1": "epic-1"},
		},
		{
			name: "cancelled descendants count as closed", target: "epic-1",
			items:   map[string]string{"epic-1": "open", "task-1": "cancelled"},
			parents: map[string]string{"task-1": "epic-1"},
		},
		{
			name: "open descendant blocks", target: "epic-1",
			items:   map[string]string{"epic-1": "open", "task-1": "done", "task-2": "open"},
			parents: map[string]string{"task-1": "epic-1", "task-2": "epic-1"},
			wantErr: "aggregate has 1 open descendants",
		},
		{
			name: "grandchild depth counted", target: "epic-1",
			items:   map[string]string{"epic-1": "open", "feat-1": "done", "task-1": "in_progress"},
			parents: map[string]string{"feat-1": "epic-1", "task-1": "feat-1"},
			wantErr: "aggregate has 1 open descendants",
		},
		{
			name: "missing aggregate", target: "ghost",
			items:   map[string]string{},
			parents: map[string]string{},
			wantErr: "not found",
		},
		{
			name: "childless executable is not an aggregate", target: "task-9",
			items:   map[string]string{"task-9": "open"},
			parents: map[string]string{},
			wantErr: "not an aggregate",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testDB(t)
			for id, status := range tt.items {
				overrides := map[string]any{"status": status}
				if parent, ok := tt.parents[id]; ok {
					overrides["parent_id"] = parent
				}
				kind := "task"
				if strings.HasPrefix(id, "epic") {
					kind = "epic"
				} else if strings.HasPrefix(id, "feat") {
					kind = "feature"
				}
				insertItem(t, db, id, kind, overrides)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			err = validateAggregateDescendants(tx, tt.target)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateAggregateDescendants unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateAggregateDescendants error = %v, want contains %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAggregateWorkItem(t *testing.T) {
	t.Parallel()
	t.Run("unmet requirements block", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "epic-1", "epic", nil)
		insertItem(t, db, "task-1", "task", map[string]any{"parent_id": "epic-1", "status": "done"})
		if _, err := db.Exec(`INSERT INTO requirements (id, epic_id, requirement_key, status) VALUES ('req-1','epic-1','R01','approved')`); err != nil {
			t.Fatal(err)
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		err = validateAggregateWorkItem(tx, "epic-1")
		if err == nil || !strings.Contains(err.Error(), "unmet requirements") {
			t.Fatalf("error = %v, want unmet requirements", err)
		}
	})
	t.Run("satisfied requirements pass", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "epic-1", "epic", nil)
		insertItem(t, db, "task-1", "task", map[string]any{"parent_id": "epic-1", "status": "done"})
		if _, err := db.Exec(`INSERT INTO requirements (id, epic_id, requirement_key, status) VALUES ('req-1','epic-1','R01','satisfied')`); err != nil {
			t.Fatal(err)
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := validateAggregateWorkItem(tx, "epic-1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestValidateWorkflowActor(t *testing.T) {
	t.Parallel()
	if err := ValidateWorkflowActor("", "contractor"); err == nil {
		t.Error("empty actor must fail the role gate")
	}
	if err := ValidateWorkflowActor("contractor", "contractor"); err != nil {
		t.Errorf("matching actor: %v", err)
	}
	if err := ValidateWorkflowActor("owner", "contractor"); err == nil {
		t.Error("role mismatch must fail")
	}
}

func TestValidateTaskGraphRequirementCoverage(t *testing.T) {
	t.Parallel()
	seedRequirement := func(t *testing.T, db *sql.DB, criteria string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO requirements (id, task_id, requirement_key, acceptance_criteria) VALUES ('req-1','wi-1','R01',?)`, criteria); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("covered requirement snapshots", func(t *testing.T) {
		db := testDB(t)
		seedRequirement(t, db, "Given a task\nWhen it runs\nThen it closes")
		plan := tip.TaskPlanDocument{Nodes: []tip.TaskPlanDocumentNode{{Key: "T1", RequirementKeys: []string{"r01"}}}}
		snapshots, err := ValidateTaskGraphRequirementCoverage(db, "wi-1", plan)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, ok := snapshots["R01"]
		if !ok {
			t.Fatalf("snapshots = %v, want R01", snapshots)
		}
		if snapshot.RequirementKey != "R01" || snapshot.SourceHash == "" {
			t.Errorf("snapshot = %+v, want key and source hash", snapshot)
		}
	})
	t.Run("unknown requirement rejected", func(t *testing.T) {
		db := testDB(t)
		seedRequirement(t, db, "Given a task\nWhen it runs\nThen it closes")
		plan := tip.TaskPlanDocument{Nodes: []tip.TaskPlanDocumentNode{{Key: "T1", RequirementKeys: []string{"R99"}}}}
		if _, err := ValidateTaskGraphRequirementCoverage(db, "wi-1", plan); err == nil || !strings.Contains(err.Error(), "unknown requirement") {
			t.Fatalf("error = %v, want unknown requirement", err)
		}
	})
	t.Run("non-behavioral acceptance rejected", func(t *testing.T) {
		db := testDB(t)
		seedRequirement(t, db, "the task works well")
		plan := tip.TaskPlanDocument{Nodes: []tip.TaskPlanDocumentNode{{Key: "T1", RequirementKeys: []string{"R01"}}}}
		if _, err := ValidateTaskGraphRequirementCoverage(db, "wi-1", plan); err == nil || !strings.Contains(err.Error(), "acceptance criteria") {
			t.Fatalf("error = %v, want acceptance criteria gate", err)
		}
	})
	t.Run("node splitting enforced above two keys", func(t *testing.T) {
		db := testDB(t)
		seedRequirement(t, db, "Given a task\nWhen it runs\nThen it closes")
		plan := tip.TaskPlanDocument{Nodes: []tip.TaskPlanDocumentNode{{Key: "T1", RequirementKeys: []string{"R01", "R01", "R01"}}}}
		if _, err := ValidateTaskGraphRequirementCoverage(db, "wi-1", plan); err == nil || !strings.Contains(err.Error(), "more than two requirement_keys") {
			t.Fatalf("error = %v, want split guidance", err)
		}
	})
}
