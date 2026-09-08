package workitem

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func TestStagesIsLeanOnly(t *testing.T) {
	t.Parallel()
	if len(Stages) != 1 || Stages[0] != "rri_t_scenarios" {
		t.Errorf("Stages = %v, want only rri_t_scenarios (lean artifact surface)", Stages)
	}
}

func TestSaveArtifact(t *testing.T) {
	db := testDB(t)
	insertItem(t, db, "epic-1", "epic", nil)
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "usage", args: []string{"epic-1"}, wantErr: "usage"},
		{name: "unknown stage", args: []string{"epic-1", "vision", "content"}, wantErr: "usage"},
		{name: "empty content", args: []string{"epic-1", "rri_t_scenarios", ""}, wantErr: "usage"},
		{name: "unknown item", args: []string{"ghost", "rri_t_scenarios", "c"}, wantErr: "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := captureVoid(t, func() error { return SaveArtifact(db, tt.args) })
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("SaveArtifact(%v) error = %v, want contains %q", tt.args, err, tt.wantErr)
			}
		})
	}
	t.Run("revisions increment and hash pins content", func(t *testing.T) {
		first := captureSaveArtifact(t, db, "epic-1", "content one")
		second := captureSaveArtifact(t, db, "epic-1", "content two")
		if first["revision"] != float64(1) || second["revision"] != float64(2) {
			t.Errorf("revisions = %v / %v, want 1 then 2", first["revision"], second["revision"])
		}
		if first["content_hash"] == second["content_hash"] {
			t.Error("distinct contents must hash distinctly")
		}
		if first["content_hash"] != hashJSON("content one") {
			t.Errorf("content_hash = %v, want hashJSON(content)", first["content_hash"])
		}
	})
}

func captureSaveArtifact(t *testing.T, db *sql.DB, itemID, content string) map[string]any {
	t.Helper()
	var out string
	var err error
	out = captureStdout(t, func() { err = SaveArtifact(db, []string{itemID, "rri_t_scenarios", content}) })
	if err != nil {
		t.Fatalf("SaveArtifact: %v", err)
	}
	return decodeJSON(t, out)
}

func TestValidateRriTVerification(t *testing.T) {
	t.Parallel()
	scenarioMap := func(overrides map[string]any) map[string]any {
		scenario := map[string]any{
			"id": "SC-1", "persona": "Operator", "dimension": "D1", "stress_axis": "TIME",
			"requirement_id": "R01", "procedure": "run the tests", "evidence": "exit 0", "result": "PASS",
		}
		for key, value := range overrides {
			if value == nil {
				delete(scenario, key)
			} else {
				scenario[key] = value
			}
		}
		return scenario
	}
	validScenario := func(overrides map[string]any) string {
		return fmt.Sprintf(`{"scenarios":[%s],"not_applicable":[]}`, marshalJSON(t, scenarioMap(overrides)))
	}
	tests := []struct {
		name         string
		status       string
		content      string
		requirements bool // seed one approved requirement row R01
		wantErr      string
	}{
		{name: "invalid JSON", status: "passed", content: "{broken", wantErr: "invalid RRI-T evidence JSON"},
		{name: "missing scenario id", status: "passed", content: validScenario(map[string]any{"id": nil}), wantErr: "invalid RRI-T scenario"},
		{name: "missing persona", status: "passed", content: validScenario(map[string]any{"persona": nil}), wantErr: "invalid RRI-T scenario"},
		{name: "bad dimension", status: "passed", content: validScenario(map[string]any{"dimension": "D9"}), wantErr: "invalid RRI-T scenario"},
		{name: "bad stress axis", status: "passed", content: validScenario(map[string]any{"stress_axis": "LUCK"}), wantErr: "invalid RRI-T scenario"},
		{name: "missing procedure", status: "passed", content: validScenario(map[string]any{"procedure": nil}), wantErr: "invalid RRI-T scenario"},
		{name: "missing evidence", status: "passed", content: validScenario(map[string]any{"evidence": nil}), wantErr: "invalid RRI-T scenario"},
		{name: "bad result", status: "passed", content: validScenario(map[string]any{"result": "MAYBE"}), wantErr: "invalid RRI-T scenario"},
		{name: "empty requirement_id", status: "passed", content: validScenario(map[string]any{"requirement_id": ""}), wantErr: "requires a requirement_id"},
		{name: "lean aggregate accepts spec keys", status: "passed", content: validScenario(nil), requirements: false},
		{name: "planning aggregate rejects unknown requirement", status: "passed", content: validScenario(map[string]any{"requirement_id": "R99"}), requirements: true, wantErr: "invalid RRI-T scenario"},
		{name: "passed status rejects non-PASS result", status: "passed", content: validScenario(map[string]any{"result": "PAINFUL"}), wantErr: "requires remediation or owner deferral"},
		{name: "non-passed status accepts PAINFUL", status: "partial", content: validScenario(map[string]any{"result": "PAINFUL"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testDB(t)
			if tt.requirements {
				if _, err := db.Exec(`INSERT INTO requirements (id, task_id, requirement_key) VALUES ('req-1','agg-1','R01')`); err != nil {
					t.Fatal(err)
				}
			}
			err := validateRriTVerification(db, "agg-1", tt.status, tt.content)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateRriTVerification unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateRriTVerification error = %v, want contains %q", err, tt.wantErr)
			}
		})
	}
	t.Run("duplicate scenario identity", func(t *testing.T) {
		db := testDB(t)
		duplicate := marshalJSON(t, scenarioMap(nil))
		content := fmt.Sprintf(`{"scenarios":[%s,%s],"not_applicable":[]}`, duplicate, duplicate)
		if err := validateRriTVerification(db, "agg-1", "passed", content); err == nil || !strings.Contains(err.Error(), "duplicate RRI-T scenario") {
			t.Fatalf("duplicate error = %v, want duplicate RRI-T scenario", err)
		}
	})
	t.Run("id-less NA needs only a reason", func(t *testing.T) {
		db := testDB(t)
		content := `{"scenarios":[],"not_applicable":[{"reason":"owned by F2"}]}`
		if err := validateRriTVerification(db, "agg-1", "passed", content); err != nil {
			t.Fatalf("id-less NA with reason: %v", err)
		}
		if err := validateRriTVerification(db, "agg-1", "passed", `{"scenarios":[],"not_applicable":[{"reason":"   "}]} `); err == nil || !strings.Contains(err.Error(), "concrete reason") {
			t.Fatalf("blank reason error = %v, want concrete reason", err)
		}
	})
	t.Run("id-bearing NA requires full identity", func(t *testing.T) {
		db := testDB(t)
		content := `{"scenarios":[],"not_applicable":[{"id":"NA-1","reason":"no persona supplied"}]}`
		if err := validateRriTVerification(db, "agg-1", "passed", content); err == nil || !strings.Contains(err.Error(), "not_applicable") {
			t.Fatalf("id-bearing NA error = %v, want invalid not_applicable", err)
		}
	})
	t.Run("NA requirement scope follows the DB gate", func(t *testing.T) {
		db := testDB(t)
		if _, err := db.Exec(`INSERT INTO requirements (id, task_id, requirement_key) VALUES ('req-1','agg-1','R01')`); err != nil {
			t.Fatal(err)
		}
		content := `{"scenarios":[],"not_applicable":[{"id":"NA-1","persona":"Operator","dimension":"D2","stress_axis":"DATA","requirement_id":"R99","reason":"deferred"}]}`
		if err := validateRriTVerification(db, "agg-1", "passed", content); err == nil || !strings.Contains(err.Error(), "not_applicable") {
			t.Fatalf("NA outside scope error = %v, want invalid not_applicable", err)
		}
	})
}

func TestIntegrationEvidence(t *testing.T) {
	t.Parallel()
	t.Run("no runs yields nil", func(t *testing.T) {
		db := testDB(t)
		if got := IntegrationEvidence(db, "wi-1"); got != nil {
			t.Errorf("IntegrationEvidence = %v, want nil", got)
		}
	})
	t.Run("latest mutation run with review lineage", func(t *testing.T) {
		db := testDB(t)
		seed := []string{
			`INSERT INTO pipeline_runs (id, task_id, stage, status) VALUES ('pr-old','wi-1','worker','completed')`,
			`INSERT INTO pipeline_runs (id, task_id, stage, status, artifact_saved_at, integrated_at, integrated_patch_hash, integrated_patch_path)
				VALUES ('pr-new','wi-1','worker','completed','saved','integrated','abc123','.pi/patch')`,
			`INSERT INTO pipeline_runs (id, task_id, stage, status, candidate_run_id, candidate_patch_hash, result_json)
				VALUES ('pr-rev','wi-1','review','completed','pr-new','abc123','{"review_status":"passed"}')`,
		}
		for _, statement := range seed {
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
		evidence := IntegrationEvidence(db, "wi-1")
		if evidence["run_id"] != "pr-new" || evidence["integrated_at"] != "integrated" || evidence["integrated_patch_hash"] != "abc123" {
			t.Errorf("evidence = %v, want latest run pr-new fields", evidence)
		}
		if evidence["review_run_id"] != "pr-rev" || evidence["review_status"] != "completed" || evidence["review_verdict"] != "passed" {
			t.Errorf("review lineage = %v, want pr-rev passed verdict", evidence)
		}
	})
	t.Run("review mismatch hides lineage", func(t *testing.T) {
		db := testDB(t)
		if _, err := db.Exec(`INSERT INTO pipeline_runs (id, task_id, stage, status) VALUES ('pr-x','wi-1','worker','completed')`); err != nil {
			t.Fatal(err)
		}
		evidence := IntegrationEvidence(db, "wi-1")
		if _, ok := evidence["review_run_id"]; ok {
			t.Errorf("evidence = %v, want no review lineage without a matching review run", evidence)
		}
	})
}
