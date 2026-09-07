package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// Artifact file projection test fixtures: artifact-save flows run against a
// real temp project root (initProject), with .apm/artifacts writable or
// unwritable variants, alongside the temp-SQLite database the pic binary
// creates under <root>/.pi/tasks.db.

func artifactFilesDir(root string) string {
	return filepath.Join(root, ".apm", "artifacts")
}

// makeArtifactsDirUnwritable flips .apm/artifacts to read-only so file
// projection fails while SQLite stays writable; the cleanup restores the
// permission so t.TempDir cleanup can remove the tree.
func makeArtifactsDirUnwritable(t *testing.T, root string) {
	t.Helper()
	dir := artifactFilesDir(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
}

// openArtifactProjectDB opens the temp SQLite database the pic binary
// created under <root>/.pi/tasks.db; cleanup closes the handle.
func openArtifactProjectDB(t *testing.T, root string) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// artifactFileRow reads the latest revision of a stage's work_item_artifacts
// row for the work item persisted in the project's temp SQLite database.
func artifactFileRow(t *testing.T, db *sql.DB, workItemID string, stage string) (revision int, content string, contentHash string) {
	t.Helper()
	err := db.QueryRow(`SELECT revision,content,content_hash FROM work_item_artifacts WHERE work_item_id=? AND stage=? ORDER BY revision DESC LIMIT 1`, workItemID, stage).Scan(&revision, &content, &contentHash)
	if err != nil {
		t.Fatalf("artifact row %s/%s: %v", workItemID, stage, err)
	}
	return revision, content, contentHash
}

// assertWorkItemEventCount asserts how many work_item_events rows were
// recorded for the work item; an empty eventType counts every event type.
func assertWorkItemEventCount(t *testing.T, db *sql.DB, workItemID string, eventType string, want int) {
	t.Helper()
	query := `SELECT COUNT(*) FROM work_item_events WHERE work_item_id=?`
	args := []any{workItemID}
	if eventType != "" {
		query += ` AND event_type=?`
		args = append(args, eventType)
	}
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("event %q count = %d, want %d", eventType, count, want)
	}
}

func initArtifactFileProject(t *testing.T, bin string) (root string, home string, workItemID string) {
	t.Helper()
	root, home = initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Artifact Files"))
	return root, home, epic["id"].(string)
}

func saveArtifact(t *testing.T, bin string, root string, home string, id string, stage string, content string) map[string]any {
	t.Helper()
	return asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, stage, content))
}

func TestArtifactFileProjectFixture(t *testing.T) {
	bin := buildPic(t)
	root, home, id := initArtifactFileProject(t, bin)
	artifact := saveArtifact(t, bin, root, home, id, "scan", "scan content")
	if artifact["revision"] != float64(1) {
		t.Fatalf("scan artifact = %#v", artifact)
	}
	db := openArtifactProjectDB(t, root)
	revision, content, contentHash := artifactFileRow(t, db, id, "scan")
	if revision != 1 || content != "scan content" || contentHash == "" {
		t.Fatalf("scan artifact row = rev %d content %q hash %q", revision, content, contentHash)
	}
	// create/artifact-save record no work_item_events; event assertions start
	// from this empty-log baseline.
	assertWorkItemEventCount(t, db, id, "", 0)
	makeArtifactsDirUnwritable(t, root)
	if info, err := os.Stat(artifactFilesDir(root)); err != nil || info.Mode().Perm() != 0o500 {
		t.Fatalf("unwritable variant perm = %v err=%v", info.Mode().Perm(), err)
	}
}
