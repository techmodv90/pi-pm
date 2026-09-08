package workitem

import (
	"database/sql"
	"strings"
	"testing"
)

// captureVoid runs fn with stdout captured and returns its error.
func captureVoid(t *testing.T, fn func() error) error {
	t.Helper()
	var err error
	captureStdout(t, func() { err = fn() })
	return err
}

func rowCount(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("count query %s: %v", query, err)
	}
	return count
}

func TestAddRelation(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	insertItem(t, db, "task-1", "task", nil)
	insertItem(t, db, "task-2", "task", nil)
	insertItem(t, db, "task-3", "task", nil)
	insertItem(t, db, "gate-1", "gate", nil)
	tests := []struct {
		name     string
		args     []string
		relation string
		wantErr  string
	}{
		{name: "usage", args: []string{"task-1"}, relation: "blocks", wantErr: "usage"},
		{name: "unknown item", args: []string{"ghost", "task-1"}, relation: "blocks", wantErr: "not found"},
		{name: "gates requires gate kind", args: []string{"task-1", "task-2"}, relation: "gates", wantErr: "is not a gate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := captureVoid(t, func() error { return AddRelation(db, tt.args, tt.relation) })
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("AddRelation(%v, %s) error = %v, want contains %q", tt.args, tt.relation, err, tt.wantErr)
			}
		})
	}
	t.Run("blocks inserted", func(t *testing.T) {
		if err := captureVoid(t, func() error { return AddRelation(db, []string{"task-2", "task-1"}, "blocks") }); err != nil {
			t.Fatal(err)
		}
		if got := rowCount(t, db, `SELECT COUNT(*) FROM work_item_relations WHERE work_item_id='task-2' AND relation_type='blocks' AND related_work_item_id='task-1'`); got != 1 {
			t.Errorf("blocks relation count = %d, want 1", got)
		}
	})
	t.Run("dependency cycle", func(t *testing.T) {
		if err := captureVoid(t, func() error { return AddRelation(db, []string{"task-3", "task-2"}, "blocks") }); err != nil {
			t.Fatal(err)
		}
		// task-3 -> task-2 -> task-1; adding task-1 blocks task-3 closes it.
		err := captureVoid(t, func() error { return AddRelation(db, []string{"task-1", "task-3"}, "blocks") })
		if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
			t.Fatalf("cycle error = %v, want dependency cycle", err)
		}
	})
}

func TestClaim(t *testing.T) {
	t.Parallel()
	t.Run("usage", func(t *testing.T) {
		db := testDB(t)
		err := captureVoid(t, func() error { return Claim(db, []string{"wi-1"}) })
		if err == nil || !strings.Contains(err.Error(), "usage") {
			t.Fatalf("Claim usage error = %v", err)
		}
	})
	t.Run("aggregates are not executable", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "epic-1", "epic", nil)
		err := captureVoid(t, func() error { return Claim(db, []string{"epic-1", "worker"}) })
		if err == nil || !strings.Contains(err.Error(), "not executable") {
			t.Fatalf("Claim epic error = %v, want not executable", err)
		}
	})
	t.Run("not ready when blocked", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "blocker", "task", nil)
		insertItem(t, db, "task-1", "task", nil)
		if _, err := db.Exec(`INSERT INTO work_item_relations (id, work_item_id, relation_type, related_work_item_id) VALUES ('wir-1','task-1','blocks','blocker')`); err != nil {
			t.Fatal(err)
		}
		err := captureVoid(t, func() error { return Claim(db, []string{"task-1", "worker"}) })
		if err == nil || !strings.Contains(err.Error(), "not ready") {
			t.Fatalf("Claim blocked error = %v, want not ready", err)
		}
	})
	t.Run("ready lean task claims", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "task-1", "task", nil)
		if err := captureVoid(t, func() error { return Claim(db, []string{"task-1", "worker-1"}) }); err != nil {
			t.Fatal(err)
		}
		row, err := queryOne(db, `SELECT status, claimed_by FROM work_items WHERE id='task-1'`)
		if err != nil {
			t.Fatal(err)
		}
		// Claim stamps ownership only; the status flip to in_progress is the
		// scheduler's SetStatus transition, so the row stays open here.
		if row["status"] != "open" || row["claimed_by"] != "worker-1" {
			t.Errorf("claimed row = %v, want open claimed by worker-1", row)
		}
	})
}

func TestReadySQL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		id       string
		kind     string
		over     map[string]any
		extraSQL []string
		ready    bool
	}{
		{name: "lean open task", id: "t1", kind: "task", ready: true},
		{name: "in_progress not ready", id: "t2", kind: "task", over: map[string]any{"claimed_at": "now", "claimed_by": "w"}, ready: false},
		{name: "deferred not ready", id: "t3", kind: "task", over: map[string]any{"deferred": 1}, ready: false},
		{name: "done not ready", id: "t4", kind: "task", over: map[string]any{"status": "done"}, ready: false},
		{name: "epic excluded", id: "e1", kind: "epic", ready: false},
		{name: "blocked by open blocker", id: "t5", kind: "task",
			extraSQL: []string{
				`INSERT INTO work_items (id, type, title, status) VALUES ('b5','task','blocker','open')`,
				`INSERT INTO work_item_relations (id, work_item_id, relation_type, related_work_item_id) VALUES ('wir-5','t5','blocks','b5')`,
			}, ready: false},
		{name: "done blocker does not block", id: "t6", kind: "task",
			extraSQL: []string{
				`INSERT INTO work_items (id, type, title, status) VALUES ('b6','task','blocker','done')`,
				`INSERT INTO work_item_relations (id, work_item_id, relation_type, related_work_item_id) VALUES ('wir-6','t6','blocks','b6')`,
			}, ready: true},
		{name: "open gate blocks", id: "t7", kind: "task",
			extraSQL: []string{
				`INSERT INTO work_items (id, type, title, status) VALUES ('g7','gate','gate','open')`,
				`INSERT INTO work_item_relations (id, work_item_id, relation_type, related_work_item_id) VALUES ('wir-7','t7','gates','g7')`,
			}, ready: false},
		{name: "pack-free materialized excluded from lean path", id: "t8", kind: "task",
			extraSQL: []string{`INSERT INTO work_item_materializations (work_item_id, root_work_item_id, checkpoint_id) VALUES ('t8','root','c')`}, ready: false},
		{name: "active pack ready", id: "t9", kind: "task",
			extraSQL: []string{`INSERT INTO work_item_instruction_packs (id, work_item_id, status, version, content_hash) VALUES ('p9','t9','active',1,'h')`}, ready: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testDB(t)
			insertItem(t, db, tt.id, tt.kind, tt.over)
			for _, statement := range tt.extraSQL {
				if _, err := db.Exec(statement); err != nil {
					t.Fatalf("fixture: %v", err)
				}
			}
			var ready int
			if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM work_items wi WHERE wi.id=? AND (`+ReadySQL+`))`, tt.id).Scan(&ready); err != nil {
				t.Fatal(err)
			}
			if (ready == 1) != tt.ready {
				t.Errorf("ReadySQL(%s) ready = %v, want %v", tt.id, ready == 1, tt.ready)
			}
		})
	}
}

func TestExecutionReset(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) *sql.DB {
		db := testDB(t)
		insertItem(t, db, "root-1", "epic", nil)
		insertItem(t, db, "task-1", "task", map[string]any{"status": "in_progress", "claimed_at": "now", "claimed_by": "w", "review_status": "failed", "review_notes": "note"})
		if _, err := db.Exec(`INSERT INTO work_item_materializations (work_item_id, root_work_item_id, checkpoint_id) VALUES ('task-1','root-1','c-1')`); err != nil {
			t.Fatal(err)
		}
		return db
	}
	t.Run("usage requires owner role", func(t *testing.T) {
		db := setup(t)
		err := captureVoid(t, func() error { return ExecutionReset(db, []string{"task-1"}) })
		if err == nil || !strings.Contains(err.Error(), "usage") {
			t.Fatalf("ExecutionReset usage error = %v", err)
		}
	})
	t.Run("terminal items rejected", func(t *testing.T) {
		db := setup(t)
		if _, err := db.Exec(`UPDATE work_items SET status='done' WHERE id='task-1'`); err != nil {
			t.Fatal(err)
		}
		err := captureVoid(t, func() error { return ExecutionReset(db, []string{"task-1", "owner"}) })
		if err == nil || !strings.Contains(err.Error(), "non-terminal") {
			t.Fatalf("ExecutionReset done error = %v, want non-terminal", err)
		}
	})
	t.Run("unmaterialized items rejected", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "task-2", "task", nil)
		err := captureVoid(t, func() error { return ExecutionReset(db, []string{"task-2", "owner"}) })
		if err == nil || !strings.Contains(err.Error(), "materialized") {
			t.Fatalf("ExecutionReset lean error = %v, want materialized", err)
		}
	})
	t.Run("active runs rejected", func(t *testing.T) {
		db := setup(t)
		if _, err := db.Exec(`INSERT INTO pipeline_runs (id, task_id, stage, status) VALUES ('pr-1','task-1','worker','running')`); err != nil {
			t.Fatal(err)
		}
		err := captureVoid(t, func() error { return ExecutionReset(db, []string{"task-1", "owner"}) })
		if err == nil || !strings.Contains(err.Error(), "no active pipeline runs") {
			t.Fatalf("ExecutionReset active-run error = %v, want no active pipeline runs", err)
		}
	})
	t.Run("resets execution binding and logs event", func(t *testing.T) {
		db := setup(t)
		if err := captureVoid(t, func() error { return ExecutionReset(db, []string{"task-1", "owner"}) }); err != nil {
			t.Fatal(err)
		}
		row, err := queryOne(db, `SELECT status, claimed_at, claimed_by, review_status, review_notes FROM work_items WHERE id='task-1'`)
		if err != nil {
			t.Fatal(err)
		}
		if row["status"] != "open" || row["claimed_at"] != "" || row["claimed_by"] != "" || row["review_status"] != "pending" || row["review_notes"] != "" {
			t.Errorf("reset row = %v, want open unclaimed pending", row)
		}
		if got := rowCount(t, db, `SELECT COUNT(*) FROM work_item_events WHERE work_item_id='task-1' AND event_type='execution_reset'`); got != 1 {
			t.Errorf("execution_reset events = %d, want 1", got)
		}
	})
}
