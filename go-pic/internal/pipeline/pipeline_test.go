package pipeline

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/earendil-works/task-system/go-pic/internal/project"

	_ "modernc.org/sqlite"
)

func TestUsageGates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		call    func(*sql.DB, []string) error
		wantErr string
	}{
		{name: "claim", args: []string{"wi-1"}, call: Claim, wantErr: "pipeline-claim requires task id and stage"},
		{name: "complete", args: []string{"pr-1", "lease"}, call: Complete, wantErr: "pipeline-complete requires run id, lease token, and status"},
		{name: "checkpoint", args: []string{"pr-1", "lease"}, call: Checkpoint, wantErr: "pipeline-checkpoint requires run id, lease token, and checkpoint"},
		{name: "checkpoint unknown", args: []string{"pr-1", "lease", "bogus"}, call: Checkpoint, wantErr: "invalid pipeline checkpoint"},
		{name: "bind", args: []string{"pr-1", "lease"}, call: Bind, wantErr: "pipeline-bind requires run id, lease token, and subagent run id"},
		{name: "renew", args: nil, call: Renew, wantErr: "pipeline-renew requires run id"},
		{name: "runs", args: nil, call: Runs, wantErr: "pipeline-runs requires task id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// arg-validation gates never reach SQL; a nil db is safe there.
			var db *sql.DB
			err := tt.call(db, tt.args)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want contains %q", err, tt.wantErr)
			}
		})
	}
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tasks.db")
	if err := project.InitDB(path); err != nil {
		t.Fatal(err)
	}
	db, err := project.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestExpireLeasesMarksStaleMutationRuns(t *testing.T) {
	db := testDB(t)
	insertItem := func(id string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO work_items(id,type,title,status) VALUES(?,'task','T','open')`, id); err != nil {
			t.Fatal(err)
		}
	}
	insertItem("wi-1")
	insertItem("wi-2")
	insertItem("wi-3")
	seed := func(id, taskID, stage, status, leaseExpiry string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at) VALUES(?,?,?,?,?,?,?)`,
			id, taskID, stage, 1, status, "lease-"+id, leaseExpiry); err != nil {
			t.Fatal(err)
		}
	}
	stale := time.Now().UTC().Add(-time.Hour).Format("2006-01-02 15:04:05")
	fresh := time.Now().UTC().Add(time.Hour).Format("2006-01-02 15:04:05")
	seed("pr-stale", "wi-1", "worker", "running", stale)
	seed("pr-fresh", "wi-2", "worker", "running", fresh)
	seed("pr-review", "wi-3", "review", "running", stale) // review is not a mutation stage
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// expireLeases with an empty task filter covers all three items.
	if err := expireLeases(tx, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	row := func(id string) string {
		t.Helper()
		var status string
		if err := db.QueryRow(`SELECT status FROM pipeline_runs WHERE id=?`, id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		return status
	}
	if got := row("pr-stale"); got != "expired" {
		t.Errorf("stale worker run = %q, want expired", got)
	}
	if got := row("pr-fresh"); got != "running" {
		t.Errorf("fresh worker run = %q, want running", got)
	}
	// The UPDATE pass expires any stale-leased run; the mutation-stage SELECT
	// only drives the work-item reopen side effect.
	if got := row("pr-review"); got != "expired" {
		t.Errorf("stale review run = %q, want expired", got)
	}
}

func TestListJSONRequiresWorkItemID(t *testing.T) {
	db := testDB(t)
	err := listJSON(db, nil, `SELECT * FROM pipeline_runs`)
	if err == nil || err.Error() != "Work Item id required" {
		t.Fatalf("listJSON error = %v", err)
	}
}
