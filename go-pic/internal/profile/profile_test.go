package profile

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/earendil-works/task-system/go-pic/internal/project"

	_ "modernc.org/sqlite"
)

func TestLifecycleForStage(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"scan": "plan", "rri": "plan", "vision": "plan", "blueprint": "plan",
		"contracts": "plan", "task_graph": "plan",
		"worker": "implement", "review": "qa", "autofix": "qa",
		"unknown": "",
	}
	for stage, want := range tests {
		if got := LifecycleForStage(stage); got != want {
			t.Errorf("LifecycleForStage(%q) = %q, want %q", stage, got, want)
		}
	}
}

func TestPlanStagesForProfile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		kind   string
		parent string
		depth  string
		want   []string
	}{
		{name: "orphan executable gets minimal plan", kind: "task", parent: "", depth: "standard", want: []string{"scan", "rri", "task_graph"}},
		{name: "full depth", kind: "epic", parent: "", depth: "full", want: []string{"scan", "rri", "vision", "blueprint", "contracts", "task_graph"}},
		{name: "designed depth", kind: "epic", parent: "", depth: "designed", want: []string{"scan", "rri", "blueprint", "task_graph"}},
		{name: "quick depth", kind: "feature", parent: "epic-1", depth: "quick", want: []string{"scan", "rri", "task_graph"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := PlanStagesForProfile(tt.kind, tt.parent, tt.depth); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PlanStagesForProfile = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContentHashDeterministic(t *testing.T) {
	t.Parallel()
	first := ContentHash("plan", 1, "full", []string{"scan", "rri", "task_graph"})
	second := ContentHash("plan", 1, "full", []string{"scan", "rri", "task_graph"})
	if first != second || first == "" {
		t.Fatalf("hash = %q / %q, want equal non-empty", first, second)
	}
	if ContentHash("plan", 2, "full", []string{"scan", "rri", "task_graph"}) == first {
		t.Error("hash must depend on version")
	}
}

func TestEnsureAndComputePlanStages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	if err := project.InitDB(path); err != nil {
		t.Fatal(err)
	}
	db, err := project.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO work_items(id,type,title,status,planning_depth) VALUES('wi-1','task','T','open','full')`); err != nil {
		t.Fatal(err)
	}
	stages, depth, version, hash, err := ComputePlanStages(db, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if depth != "full" || version != 0 || hash != "" {
		t.Fatalf("fresh compute = %v %v %v, want full/0/empty", stages, depth, version)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := Ensure(tx, "wi-1"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	stages, depth, version, hash, err = ComputePlanStages(db, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if version != 1 || hash == "" || depth != "full" {
		t.Fatalf("post-ensure compute = stages %v depth %q version %d hash %q", stages, depth, version, hash)
	}
}

func TestDepthInfoMissingItem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	if err := project.InitDB(path); err != nil {
		t.Fatal(err)
	}
	db, err := project.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, _, _, err := DepthInfo(db, "wi-ghost"); err == nil {
		t.Fatal("DepthInfo ghost item must fail")
	}
}
