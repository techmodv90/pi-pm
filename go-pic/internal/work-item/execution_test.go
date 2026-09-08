package workitem

import (
	"strings"
	"testing"
)

func TestLoadExecutionState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		runs       []string
		wantStage  string
		wantPipe   string
		wantReview string
	}{
		{
			name:      "fresh lean task waits for instruction pack",
			runs:      nil,
			wantStage: "instruction_pack", wantPipe: "", wantReview: "",
		},
		{
			name: "completed candidate moves to review",
			runs: []string{
				`INSERT INTO pipeline_runs (id, task_id, stage, status, artifact_saved_at, integrated_patch_hash) VALUES ('pr-1','wi-1','worker','completed','saved','h1')`,
			},
			wantStage: "review", wantPipe: "review", wantReview: "",
		},
		{
			name: "passed review moves to contractor_verification",
			runs: []string{
				`INSERT INTO pipeline_runs (id, task_id, stage, status, artifact_saved_at, integrated_patch_hash) VALUES ('pr-1','wi-1','worker','completed','saved','h1')`,
				`INSERT INTO pipeline_runs (id, task_id, stage, status, candidate_run_id, candidate_patch_hash, result_json) VALUES ('pr-2','wi-1','review','completed','pr-1','h1','{"review_status":"passed","candidate_patch_hash":"h1"}')`,
			},
			wantStage: "contractor_verification", wantPipe: "", wantReview: "passed",
		},
		{
			name: "failed review without owner approval returns to worker",
			runs: []string{
				`INSERT INTO pipeline_runs (id, task_id, stage, status, artifact_saved_at, integrated_patch_hash) VALUES ('pr-1','wi-1','worker','completed','saved','h1')`,
				`INSERT INTO pipeline_runs (id, task_id, stage, status, candidate_run_id, candidate_patch_hash, result_json) VALUES ('pr-2','wi-1','review','completed','pr-1','h1','{"review_status":"failed","candidate_patch_hash":"h1"}')`,
			},
			wantStage: "implement", wantPipe: "worker", wantReview: "failed",
		},
		{
			name: "failed review with owner approval waits for the owner",
			runs: []string{
				`INSERT INTO pipeline_runs (id, task_id, stage, status, artifact_saved_at, integrated_patch_hash) VALUES ('pr-1','wi-1','worker','completed','saved','h1')`,
				`INSERT INTO pipeline_runs (id, task_id, stage, status, candidate_run_id, candidate_patch_hash, result_json) VALUES ('pr-2','wi-1','review','completed','pr-1','h1','{"review_status":"failed","owner_approval_required":1,"candidate_patch_hash":"h1"}')`,
			},
			wantStage: "owner_approval", wantPipe: "", wantReview: "failed",
		},
		{
			name: "stale review hash is ignored",
			runs: []string{
				`INSERT INTO pipeline_runs (id, task_id, stage, status, artifact_saved_at, integrated_patch_hash) VALUES ('pr-1','wi-1','worker','completed','saved','h1')`,
				`INSERT INTO pipeline_runs (id, task_id, stage, status, candidate_run_id, candidate_patch_hash, result_json) VALUES ('pr-2','wi-1','review','completed','pr-1','stale','{"review_status":"passed","candidate_patch_hash":"stale"}')`,
			},
			wantStage: "review", wantPipe: "review", wantReview: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testDB(t)
			insertItem(t, db, "wi-1", "task", nil)
			for _, statement := range tt.runs {
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			state, err := LoadExecutionState(db, "wi-1")
			if err != nil {
				t.Fatal(err)
			}
			if state.NextStage != tt.wantStage || state.PipelineStage != tt.wantPipe || state.ReviewStatus != tt.wantReview {
				t.Errorf("state = %+v, want stage %s pipe %q review %q", state, tt.wantStage, tt.wantPipe, tt.wantReview)
			}
		})
	}
	t.Run("candidate id surfaces on the review path", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "wi-1", "task", nil)
		if _, err := db.Exec(`INSERT INTO pipeline_runs (id, task_id, stage, status, artifact_saved_at, integrated_patch_hash) VALUES ('pr-1','wi-1','worker','completed','saved','h1')`); err != nil {
			t.Fatal(err)
		}
		state, err := LoadExecutionState(db, "wi-1")
		if err != nil {
			t.Fatal(err)
		}
		if state.CandidateID != "pr-1" {
			t.Errorf("CandidateID = %q, want pr-1", state.CandidateID)
		}
	})
}

func TestFinalizeVerifiedExecutionState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		verdict   string
		decision  string
		wantStage string
		wantPipe  string
	}{
		{name: "unverified stays", verdict: "", decision: "", wantStage: "contractor_verification", wantPipe: ""},
		{name: "failed verification routes autofix", verdict: "failed", decision: "", wantStage: "implement", wantPipe: "autofix"},
		{name: "partial verification routes autofix", verdict: "partial", decision: "", wantStage: "implement", wantPipe: "autofix"},
		{name: "passed verification done", verdict: "passed", decision: "", wantStage: "done", wantPipe: ""},
		{name: "rejected owner decision reopens worker", verdict: "passed", decision: "rejected", wantStage: "implement", wantPipe: "worker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testDB(t)
			insertItem(t, db, "wi-1", "task", nil)
			if tt.decision != "" {
				if _, err := db.Exec(`INSERT INTO work_item_owner_decisions (id, work_item_id, completion_report_id, decision) VALUES ('wiod-1','wi-1','wicr-1',?)`, tt.decision); err != nil {
					t.Fatal(err)
				}
			}
			state := ExecutionState{NextStage: "contractor_verification", CompletionID: "wicr-1", VerificationStatus: tt.verdict}
			got := finalizeVerifiedExecutionState(db, "wi-1", state)
			if got.NextStage != tt.wantStage || got.PipelineStage != tt.wantPipe {
				t.Errorf("finalized state = %+v, want stage %s pipe %q", got, tt.wantStage, tt.wantPipe)
			}
			if tt.decision != "" && got.OwnerDecision != tt.decision {
				t.Errorf("OwnerDecision = %q, want %q", got.OwnerDecision, tt.decision)
			}
		})
	}
}

func TestValidateExecutableClosureForReport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		seed    []string
		report  string
		wantErr string
	}{
		{
			name:    "missing completion report",
			seed:    nil,
			wantErr: "passed review and current integrated Completion Report",
		},
		{
			name: "review not passed",
			seed: []string{
				`INSERT INTO work_item_verification_reports (id, work_item_id, completion_report_id, status) VALUES ('wivr-1','wi-1','wicr-1','passed')`,
			},
			wantErr: "passed review and current integrated Completion Report",
		},
		{
			name: "verification not passed",
			seed: []string{
				`INSERT INTO work_item_instruction_packs (id, work_item_id, status, version, content_hash) VALUES ('pack-1','wi-1','active',1,'h')`,
				`INSERT INTO pipeline_runs (id, task_id, stage, status, instruction_pack_id, instruction_pack_version, instruction_pack_hash, artifact_saved_at, integrated_at, integrated_patch_hash, candidate_patch_hash) VALUES ('pr-1','wi-1','worker','completed','pack-1',1,'h','saved','integrated','h1','h1')`,
				`INSERT INTO pipeline_runs (id, task_id, stage, status, instruction_pack_id, instruction_pack_version, instruction_pack_hash, candidate_run_id, candidate_patch_hash, result_json) VALUES ('pr-2','wi-1','review','completed','pack-1',1,'h','pr-1','h1','{"review_status":"passed","candidate_patch_hash":"h1"}')`,
				`INSERT INTO work_item_completion_reports (id, work_item_id, pipeline_run_id, instruction_pack_id, instruction_pack_version, instruction_pack_hash, status) VALUES ('wicr-1','wi-1','pr-1','pack-1',1,'h','done')`,
				`INSERT INTO work_item_verification_reports (id, work_item_id, completion_report_id, status) VALUES ('wivr-1','wi-1','wicr-1','failed')`,
			},
			wantErr: "passed contractor verification",
		},
		{
			name: "full closure passes",
			seed: []string{
				`INSERT INTO work_item_instruction_packs (id, work_item_id, status, version, content_hash) VALUES ('pack-1','wi-1','active',1,'h')`,
				`INSERT INTO pipeline_runs (id, task_id, stage, status, instruction_pack_id, instruction_pack_version, instruction_pack_hash, artifact_saved_at, integrated_at, integrated_patch_hash, candidate_patch_hash) VALUES ('pr-1','wi-1','worker','completed','pack-1',1,'h','saved','integrated','h1','h1')`,
				`INSERT INTO pipeline_runs (id, task_id, stage, status, instruction_pack_id, instruction_pack_version, instruction_pack_hash, candidate_run_id, candidate_patch_hash, result_json) VALUES ('pr-2','wi-1','review','completed','pack-1',1,'h','pr-1','h1','{"review_status":"passed","candidate_patch_hash":"h1"}')`,
				`INSERT INTO work_item_completion_reports (id, work_item_id, pipeline_run_id, instruction_pack_id, instruction_pack_version, instruction_pack_hash, status) VALUES ('wicr-1','wi-1','pr-1','pack-1',1,'h','done')`,
				`INSERT INTO work_item_verification_reports (id, work_item_id, completion_report_id, status) VALUES ('wivr-1','wi-1','wicr-1','passed')`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testDB(t)
			insertItem(t, db, "wi-1", "task", nil)
			for _, statement := range tt.seed {
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			err := validateExecutableClosureForReport(db, "wi-1", tt.report, false)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("closure unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("closure error = %v, want %q", err, tt.wantErr)
			}
		})
	}
	t.Run("expected report mismatch", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "wi-1", "task", nil)
		if err := validateExecutableClosureForReport(db, "wi-1", "wicr-other", false); err == nil || !strings.Contains(err.Error(), "passed review and current integrated Completion Report") {
			t.Fatalf("closure error = %v, want report mismatch", err)
		}
	})
}
