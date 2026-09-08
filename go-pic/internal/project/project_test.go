package project

import (
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/earendil-works/task-system/go-pic/internal/schema"
)

func TestInitDBCreatesWorkItemSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	if err := InitDB(path); err != nil {
		t.Fatal(err)
	}
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"work_items", "work_item_artifacts", "work_item_events", "pipeline_runs", "schema_migrations"} {
		if !schema.TableExists(db, table) {
			t.Errorf("table %s missing after InitDB", table)
		}
	}
}

func TestFirstNonEmptyAndRealpathOrAbs(t *testing.T) {
	t.Parallel()
	if FirstNonEmpty("", "x", "y") != "x" {
		t.Error("FirstNonEmpty skips empty values")
	}
	if FirstNonEmpty() != "" {
		t.Error("FirstNonEmpty empty input returns empty string")
	}
	dir := t.TempDir()
	if got := RealpathOrAbs(dir); !strings.HasSuffix(got, filepath.Base(dir)) {
		t.Errorf("RealpathOrAbs = %q", got)
	}
	if got := RealpathOrAbs(filepath.Join(dir, "missing")); !filepath.IsAbs(got) {
		t.Errorf("RealpathOrAbs missing path = %q, want absolute", got)
	}
}

func TestRegistryRoundtrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := ReadRegistry(); len(got.Projects) != 0 {
		t.Fatalf("fresh registry = %#v", got)
	}
	root := t.TempDir()
	original := Registry{Projects: []RegistryProject{mk("p1", "demo", root)}, CurrentProjectID: "p1"}
	if err := WriteRegistry(original); err != nil {
		t.Fatal(err)
	}
	got := ReadRegistry()
	if len(got.Projects) != 1 || got.CurrentProjectID != "p1" || got.Projects[0].Name != "demo" {
		t.Fatalf("roundtrip = %#v", got)
	}
}

func mk(id, name, root string) RegistryProject {
	return RegistryProject{ID: id, Name: name, RootPath: root, DatabasePath: filepath.Join(root, ".pi", "tasks.db"), CreatedAt: "2026-01-01", UpdatedAt: "2026-01-02"}
}

func TestUpsertDeduplicatesByRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if _, err := UpsertProject("one", root, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertProject("one", root, ""); err != nil {
		t.Fatal(err)
	}
	if got := ReadRegistry(); len(got.Projects) != 1 {
		t.Fatalf("projects = %#v, want single deduped entry", got.Projects)
	}
}

func TestFindRegistryProject(t *testing.T) {
	t.Parallel()
	registry := Registry{Projects: []RegistryProject{mk("p1", "demo", "/tmp/demo")}}
	if got, ok := FindRegistryProject(registry, "p1"); !ok || got.Name != "demo" {
		t.Fatalf("by id = %#v %v", got, ok)
	}
	if got, ok := FindRegistryProject(registry, "/tmp/demo"); !ok {
		t.Fatalf("by root = %#v %v", got, ok)
	}
	if _, ok := FindRegistryProject(registry, "p2"); ok {
		t.Fatal("missing id must not match")
	}
}

func TestOpenSQLiteForeignKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	if err := InitDB(path); err != nil {
		t.Fatal(err)
	}
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var foreignKeys int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1 on every pooled connection", foreignKeys)
	}
}
