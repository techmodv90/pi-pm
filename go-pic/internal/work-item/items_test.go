package workitem

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

func TestLabelPattern(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		label string
		want  bool
	}{
		{name: "simple", label: "v1.0", want: true},
		{name: "namespaced", label: "import:f3a8dac268e5", want: true},
		{name: "path-like", label: "p1/critical-path", want: true},
		{name: "must start alphanumeric", label: "_leading", want: false},
		{name: "no spaces", label: "has space", want: false},
		{name: "no uppercase", label: "UpperCase", want: false},
		{name: "empty", label: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := labelPattern.MatchString(tt.label); got != tt.want {
				t.Errorf("labelPattern.MatchString(%q) = %v, want %v", tt.label, got, tt.want)
			}
		})
	}
}

func TestParseLabels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		value   string
		want    []string
		wantErr string
	}{
		{name: "empty yields nil", value: "", want: nil},
		{name: "single", value: "v1.0", want: []string{"v1.0"}},
		{name: "deduplicates", value: "a,a,b", want: []string{"a", "b"}},
		{name: "invalid label", value: "a,Not Valid", wantErr: "invalid label"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLabels(tt.value)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseLabels(%q) error = %v, want contains %q", tt.value, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLabels(%q) unexpected error: %v", tt.value, err)
			}
			if !reflectEqual(got, tt.want) {
				t.Errorf("parseLabels(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func reflectEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAddLabelsIgnoresDuplicates(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := AddLabels(tx, "wi-1", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	// Re-adding must stay silent (INSERT OR IGNORE), not duplicate rows.
	if err := AddLabels(tx, "wi-1", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	labels, err := Labels(db, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflectEqual(labels, []string{"a", "b"}) {
		t.Errorf("Labels = %v, want [a b] in sorted order", labels)
	}
}

func TestAttachLabels(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	if _, err := db.Exec(`INSERT INTO work_item_labels VALUES('wi-1','x'),('wi-2','y')`); err != nil {
		t.Fatal(err)
	}
	items := []map[string]any{{"id": "wi-1"}, {"id": "wi-2"}, {"id": "wi-3"}}
	if err := AttachLabels(db, items); err != nil {
		t.Fatal(err)
	}
	if !reflectEqual(items[0]["labels"].([]string), []string{"x"}) || !reflectEqual(items[1]["labels"].([]string), []string{"y"}) {
		t.Errorf("attached labels = %v / %v", items[0]["labels"], items[1]["labels"])
	}
	if got := len(items[2]["labels"].([]string)); got != 0 {
		t.Errorf("unlabeled item got %d labels, want 0", got)
	}
}

func TestValidateWorkItemParent(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	insertItem(t, db, "epic-1", "epic", nil)
	insertItem(t, db, "task-1", "task", nil)
	insertItem(t, db, "feature-1", "feature", map[string]any{"parent_id": "epic-1"})
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	tests := []struct {
		name    string
		id      string
		parent  string
		wantErr string
	}{
		{name: "empty parent ok", id: "", parent: ""},
		{name: "epic parent ok", id: "task-9", parent: "epic-1"},
		{name: "feature parent ok", id: "task-9", parent: "feature-1"},
		{name: "missing parent", id: "", parent: "ghost", wantErr: "not found"},
		{name: "task cannot contain children", id: "", parent: "task-1", wantErr: "cannot contain children"},
		{name: "containment cycle", id: "epic-1", parent: "feature-1", wantErr: "containment cycle"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWorkItemParent(tx, tt.id, tt.parent)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateWorkItemParent(%q,%q) unexpected error: %v", tt.id, tt.parent, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateWorkItemParent(%q,%q) error = %v, want contains %q", tt.id, tt.parent, err, tt.wantErr)
			}
		})
	}
}

func TestSetStatusTransitions(t *testing.T) {
	t.Parallel()
	t.Run("not found", func(t *testing.T) {
		db := testDB(t)
		if _, err := SetStatus(db, "ghost", "open"); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("SetStatus ghost error = %v, want not found", err)
		}
	})
	t.Run("reopen done blocked", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "wi-1", "task", map[string]any{"status": "done"})
		if _, err := SetStatus(db, "wi-1", "open"); err == nil || !strings.Contains(err.Error(), "new TIP generation") {
			t.Fatalf("SetStatus done->open error = %v, want TIP generation gate", err)
		}
	})
	t.Run("open clears claim", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "wi-1", "task", map[string]any{"status": "in_progress", "claimed_at": "now", "claimed_by": "worker"})
		item, err := SetStatus(db, "wi-1", "open")
		if err != nil {
			t.Fatal(err)
		}
		if item["status"] != "open" || item["claimed_at"] != "" || item["claimed_by"] != "" {
			t.Errorf("item = %v, want open with cleared claim", item)
		}
	})
	t.Run("cancelled cascades to descendants and runs", func(t *testing.T) {
		db := testDB(t)
		insertItem(t, db, "epic-1", "epic", map[string]any{"status": "in_progress"})
		insertItem(t, db, "task-1", "task", map[string]any{"parent_id": "epic-1", "status": "in_progress", "claimed_at": "now", "claimed_by": "worker"})
		for _, run := range []string{"epic-1", "task-1"} {
			if _, err := db.Exec(`INSERT INTO pipeline_runs (id, task_id, stage, status) VALUES (?, ?, 'worker', 'running')`, "pr-"+run, run); err != nil {
				t.Fatal(err)
			}
		}
		item, err := SetStatus(db, "epic-1", "cancelled")
		if err != nil {
			t.Fatal(err)
		}
		if item["status"] != "cancelled" {
			t.Fatalf("status = %v, want cancelled", item["status"])
		}
		child, err := ByID(db, "task-1")
		if err != nil {
			t.Fatal(err)
		}
		if child["status"] != "cancelled" || child["claimed_at"] != "" {
			t.Errorf("child = %v, want cancelled with cleared claim", child)
		}
		var parentRunError, childRunError string
		if err := db.QueryRow(`SELECT error FROM pipeline_runs WHERE id='pr-epic-1'`).Scan(&parentRunError); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT error FROM pipeline_runs WHERE id='pr-task-1'`).Scan(&childRunError); err != nil {
			t.Fatal(err)
		}
		if parentRunError != "Work Item cancelled" || childRunError != "Parent Work Item cancelled" {
			t.Errorf("run errors = %q / %q, want direct vs cascade cancel messages", parentRunError, childRunError)
		}
	})
}

func TestCreate(t *testing.T) {
	db := testDB(t)
	insertItem(t, db, "epic-1", "epic", nil)
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "usage", args: []string{"task"}, wantErr: "usage"},
		{name: "unknown kind", args: []string{"widget", "t"}, wantErr: "usage"},
		{name: "invalid priority", args: []string{"task", "t", "--priority", "urgent"}, wantErr: "invalid priority"},
		{name: "invalid planning depth", args: []string{"task", "t", "--planning-depth", "mega"}, wantErr: "invalid planning depth"},
		{name: "invalid label", args: []string{"task", "t", "--labels", "Bad Label"}, wantErr: "invalid label"},
		{name: "parent not found", args: []string{"task", "t", "--parent", "ghost"}, wantErr: "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := captureCreate(t, db, tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Create(%v) error = %v, want contains %q", tt.args, err, tt.wantErr)
			}
		})
	}
	t.Run("success inherits parent labels", func(t *testing.T) {
		if _, err := db.Exec(`INSERT INTO work_item_labels VALUES('epic-1','inherited')`); err != nil {
			t.Fatal(err)
		}
		id := captureCreateID(t, db, []string{"task", "child task", "--parent", "epic-1", "--labels", "own", "--deferred", "true"})
		labels, err := Labels(db, id)
		if err != nil {
			t.Fatal(err)
		}
		if !reflectEqual(labels, []string{"inherited", "own"}) {
			t.Errorf("labels = %v, want inherited plus own", labels)
		}
		row, err := queryOne(db, `SELECT type, deferred, planning_depth, priority FROM work_items WHERE id=?`, id)
		if err != nil {
			t.Fatal(err)
		}
		if row["type"] != "task" || row["deferred"] != int64(1) || row["planning_depth"] != "full" || row["priority"] != "medium" {
			t.Errorf("created row = %v, want defaults applied", row)
		}
	})
}

// captureCreate runs Create with stdout captured and returns the error.
func captureCreate(t *testing.T, db *sql.DB, args []string) error {
	t.Helper()
	var err error
	captureStdout(t, func() { err = Create(db, args) })
	return err
}

// captureUpdate runs Update with stdout captured and returns the error.
func captureUpdate(t *testing.T, db *sql.DB, args []string) error {
	t.Helper()
	var err error
	captureStdout(t, func() { err = Update(db, args) })
	return err
}

// captureCreateID runs Create and decodes the printed item's id.
func captureCreateID(t *testing.T, db *sql.DB, args []string) string {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = Create(db, args) })
	if err != nil {
		t.Fatalf("Create(%v): %v", args, err)
	}
	var item map[string]any
	if err := json.Unmarshal([]byte(out), &item); err != nil {
		t.Fatalf("Create output not JSON: %v (%q)", err, out)
	}
	id, _ := item["id"].(string)
	if id == "" {
		t.Fatalf("Create output missing id: %q", out)
	}
	return id
}

func TestList(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	insertItem(t, db, "wi-a", "task", nil)
	insertItem(t, db, "wi-b", "task", nil)
	if _, err := db.Exec(`INSERT INTO work_item_labels VALUES('wi-a','red'),('wi-a','blue'),('wi-b','red')`); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "no filter lists all", args: nil, want: []string{"wi-a", "wi-b"}},
		{name: "label all", args: []string{"--label", "red,blue"}, want: []string{"wi-a"}},
		{name: "label any", args: []string{"--label-any", "blue"}, want: []string{"wi-a"}},
		{name: "label any matches either", args: []string{"--label-any", "blue,missing"}, want: []string{"wi-a"}},
		{name: "unknown label filters out", args: []string{"--label", "green"}, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := List(db, tt.args)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, row := range rows {
				got = append(got, row["id"].(string))
			}
			if !reflectEqual(got, tt.want) {
				t.Errorf("List(%v) ids = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	db := testDB(t)
	insertItem(t, db, "epic-1", "epic", nil)
	insertItem(t, db, "task-1", "task", nil)
	t.Run("no supported fields", func(t *testing.T) {
		if err := captureUpdate(t, db, []string{"task-1", "--colour", "red"}); err == nil || !strings.Contains(err.Error(), "no supported fields to update") {
			t.Fatalf("Update error = %v, want no supported fields", err)
		}
	})
	t.Run("unknown item", func(t *testing.T) {
		if err := captureUpdate(t, db, []string{"ghost", "--title", "x"}); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("Update error = %v, want not found", err)
		}
	})
	t.Run("title and parent update", func(t *testing.T) {
		if err := captureUpdate(t, db, []string{"task-1", "--title", "renamed", "--parent", "epic-1"}); err != nil {
			t.Fatal(err)
		}
		row, err := queryOne(db, `SELECT title, parent_id FROM work_items WHERE id='task-1'`)
		if err != nil {
			t.Fatal(err)
		}
		if row["title"] != "renamed" || row["parent_id"] != "epic-1" {
			t.Errorf("updated row = %v, want renamed under epic-1", row)
		}
	})
}

func TestByID(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	insertItem(t, db, "wi-1", "task", nil)
	item, err := ByID(db, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if item["id"] != "wi-1" || item["type"] != "task" {
		t.Errorf("ByID = %v, want the seeded row", item)
	}
	if _, ok := item["labels"].([]string); !ok {
		t.Errorf("ByID must attach labels, got %T", item["labels"])
	}
	if _, err := ByID(db, "ghost"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("ByID ghost error = %v, want not found", err)
	}
}
