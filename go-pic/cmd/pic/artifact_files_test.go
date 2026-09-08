package main

import (
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
	makeArtifactsDirUnwritable(t, root)
	if info, err := os.Stat(artifactFilesDir(root)); err != nil || info.Mode().Perm() != 0o500 {
		t.Fatalf("unwritable variant perm = %v err=%v", info.Mode().Perm(), err)
	}
}
