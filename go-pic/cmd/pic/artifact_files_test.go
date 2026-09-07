package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// TestArtifactSaveProjectsAllPlanningStages is the RED end-to-end test for
// artifact markdown projection (Plan §3.1, R01+R02, NC-1/NC-3/NC-4): every
// planning-stage save must project the exact content bytes onto the
// deterministic path <project>/.apm/artifacts/<work_item>/<stage>-r<revision>.md
// and record an artifact_files binding row, in addition to returning file_path
// in the save response. The save integration (T009) does not exist yet, so the
// first missing file or binding row fails.
func TestArtifactSaveProjectsAllPlanningStages(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Projection Epic"))
	id := epic["id"].(string)

	scenarios := `{"methodology":"rri-t","personas":["End User"],"scenarios":[{"id":"SC-1","persona":"End User","dimension":"D1","stress_axis":"TIME","requirement_id":"REQ-001","procedure":"Run the helper flow","evidence":"go test ./...","result":"PASS"}]}`
	graph := `{"version":3,"execution_policy":"strict_sequential","nodes":[{"key":"F01","type":"feature","name":"Area","requirement_keys":[],"depends_on":[]}]}`
	contents := map[string]string{
		"scan":            "scan content",
		"rri":             "# RRI Report\n\nRequirement matrix follows.",
		"rri_t_scenarios": scenarios,
		"vision":          validVisionArtifact,
		"blueprint":       validBlueprintArtifact,
		"contracts":       validContractArtifact,
		"task_graph":      graph,
	}

	db := openArtifactProjectDB(t, root)
	// Gated stages keep their canonical pre-flight: blueprint approval is
	// required before contracts save, so approvals mirror the planning flow.
	for _, stage := range workItemStages {
		content := contents[stage]
		artifact := saveArtifact(t, bin, root, home, id, stage, content)
		if artifact["revision"] != float64(1) {
			t.Fatalf("%s artifact = %#v", stage, artifact)
		}
		wantHash := hashJSON(content)
		if artifact["content_hash"] != wantHash {
			t.Fatalf("%s content_hash = %v, want %s", stage, artifact["content_hash"], wantHash)
		}
		_, rowContent, contentHash := artifactFileRow(t, db, id, stage)
		if rowContent != content || contentHash != wantHash {
			t.Fatalf("%s artifact row content/hash = %q/%q, want hash %s", stage, rowContent, contentHash, wantHash)
		}
		// NC-4: deterministic projection path per stage and revision.
		wantPath := filepath.Join(root, ".apm", "artifacts", id, fmt.Sprintf("%s-r%d.md", stage, 1))
		got, err := os.ReadFile(wantPath)
		if err != nil {
			t.Fatalf("%s projected markdown missing at %s: %v", stage, wantPath, err)
		}
		if string(got) != content {
			t.Fatalf("%s projected bytes = %q, want exact content %q", stage, got, content)
		}
		// Binding row: artifact_id -> file_path with content_sha256 equal to the
		// canonical content_hash (artifacts.dbml artifact_files table).
		var bindArtifactID, bindWorkItemID, bindStage, bindPath, bindSHA string
		var bindRevision int
		if err := db.QueryRow(`SELECT artifact_id,work_item_id,stage,revision,file_path,content_sha256 FROM artifact_files WHERE work_item_id=? AND stage=?`, id, stage).Scan(&bindArtifactID, &bindWorkItemID, &bindStage, &bindRevision, &bindPath, &bindSHA); err != nil {
			t.Fatalf("%s artifact_files binding row: %v", stage, err)
		}
		if bindArtifactID != artifact["id"] || bindWorkItemID != id || bindStage != stage || bindRevision != 1 || bindPath != wantPath || bindSHA != wantHash {
			t.Fatalf("%s artifact_files binding = %s/%s/%s/%d path %q sha %q, want artifact %v path %s sha %s", stage, bindArtifactID, bindWorkItemID, bindStage, bindRevision, bindPath, bindSHA, artifact["id"], wantPath, wantHash)
		}
		// NC-5: the save response surfaces the projection path.
		if artifact["file_path"] != wantPath {
			t.Fatalf("%s response file_path = %v, want %s", stage, artifact["file_path"], wantPath)
		}
		if stage == "rri_t_scenarios" || stage == "task_graph" {
			// rri_t_scenarios is gated out of next_stage progression and the
			// draft graph needs no approval for projection coverage.
			continue
		}
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
	}
}

func TestArtifactFilePath(t *testing.T) {
	// Ratified path scheme (ArtifactMarkdownFile.plan.md, NC-4):
	// <project>/.apm/artifacts/<work_item>/<stage>-r<revision>.md
	t.Run("deterministic", func(t *testing.T) {
		root := t.TempDir()
		path, err := artifactFilePath(root, "wi-abc123", "scan", 3)
		if err != nil {
			t.Fatalf("artifactFilePath: %v", err)
		}
		want := filepath.Join(root, ".apm", "artifacts", "wi-abc123", "scan-r3.md")
		if path != want {
			t.Fatalf("artifactFilePath = %q, want %q", path, want)
		}
		again, err := artifactFilePath(root, "wi-abc123", "scan", 3)
		if err != nil {
			t.Fatalf("artifactFilePath repeat: %v", err)
		}
		if again != path {
			t.Fatalf("artifactFilePath not deterministic: %q vs %q", again, path)
		}
		// Distinct stage/revision/work item each land on their own path.
		other, err := artifactFilePath(root, "wi-abc123", "vision", 3)
		if err != nil {
			t.Fatalf("artifactFilePath vision: %v", err)
		}
		if other == path {
			t.Fatalf("different stage collided: %q", other)
		}
		rev, err := artifactFilePath(root, "wi-abc123", "scan", 4)
		if err != nil {
			t.Fatalf("artifactFilePath rev4: %v", err)
		}
		if rev == path {
			t.Fatalf("different revision collided: %q", rev)
		}
		item, err := artifactFilePath(root, "wi-zzz", "scan", 3)
		if err != nil {
			t.Fatalf("artifactFilePath wi-zzz: %v", err)
		}
		if item == path {
			t.Fatalf("different work item collided: %q", item)
		}
	})

	t.Run("accepts-valid-work-item-ids", func(t *testing.T) {
		root := t.TempDir()
		for _, id := range []string{"wi-a", "wi-0", "wi-9x", "wi-b71a2f0a"} {
			if _, err := artifactFilePath(root, id, "scan", 1); err != nil {
				t.Errorf("artifactFilePath(%q): %v", id, err)
			}
		}
	})

	// IDs are validated against ^wi-[a-z0-9]+$ before any path is built so a
	// hostile or malformed id cannot escape the artifacts directory.
	t.Run("rejects-invalid-work-item-ids", func(t *testing.T) {
		root := t.TempDir()
		for _, id := range []string{"", "wi", "wi-", "WI-abc", "wi-ABC", "epic-123", "wia-1", "wi-a b", "wi-a/b", "wi-..", "wi-a\t"} {
			path, err := artifactFilePath(root, id, "scan", 1)
			if err == nil {
				t.Errorf("artifactFilePath(%q) = %q, want error", id, path)
			}
		}
	})
}

func TestArtifactFileHash(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "scan-r1.md")
	if err := os.WriteFile(path, []byte("scan content"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("matching-content", func(t *testing.T) {
		matches, err := artifactFileHashMatches(path, "scan content")
		if err != nil {
			t.Fatalf("artifactFileHashMatches: %v", err)
		}
		if !matches {
			t.Fatal("artifactFileHashMatches(matching bytes) = false, want true")
		}
	})

	// Drift detection per NC-7: file bytes whose sha256 differs from the
	// candidate content must compare unequal.
	t.Run("divergent-content", func(t *testing.T) {
		matches, err := artifactFileHashMatches(path, "different content")
		if err != nil {
			t.Fatalf("artifactFileHashMatches: %v", err)
		}
		if matches {
			t.Fatal("artifactFileHashMatches(divergent bytes) = true, want false")
		}
	})

	t.Run("missing-file", func(t *testing.T) {
		if _, err := artifactFileHashMatches(filepath.Join(root, "absent.md"), "scan content"); err == nil {
			t.Fatal("artifactFileHashMatches(missing file) = nil error, want error")
		}
	})
}

func TestArtifactFileAtomicWrite(t *testing.T) {
	t.Run("creates-file-and-parent-dirs", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, ".apm", "artifacts", "wi-abc123", "scan-r1.md")
		if err := writeArtifactFileAtomic(path, "scan content"); err != nil {
			t.Fatalf("writeArtifactFileAtomic: %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read projected file: %v", err)
		}
		if string(got) != "scan content" {
			t.Fatalf("file content = %q, want %q", got, "scan content")
		}
	})

	// Precedent (writeArtifactFile in the plan): temp file 0600 inside a 0700
	// artifacts tree, committed via rename.
	t.Run("permissions", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, ".apm", "artifacts", "wi-abc123", "scan-r1.md")
		if err := writeArtifactFileAtomic(path, "scan content"); err != nil {
			t.Fatalf("writeArtifactFileAtomic: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("file perm = %v, want 0600", info.Mode().Perm())
		}
		dirInfo, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if dirInfo.Mode().Perm() != 0o700 {
			t.Fatalf("dir perm = %v, want 0700", dirInfo.Mode().Perm())
		}
	})

	// Atomic semantics: no truncated or partial artifact may ever be visible
	// at the deterministic path, and no temp residue may remain.
	t.Run("leaves-no-temp-files", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, ".apm", "artifacts", "wi-abc123")
		path := filepath.Join(dir, "scan-r1.md")
		if err := writeArtifactFileAtomic(path, "scan content"); err != nil {
			t.Fatalf("writeArtifactFileAtomic: %v", err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "scan-r1.md" {
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			t.Fatalf("dir entries = %v, want only scan-r1.md", names)
		}
	})

	t.Run("replaces-existing-file-entirely", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, ".apm", "artifacts", "wi-abc123", "scan-r1.md")
		if err := writeArtifactFileAtomic(path, strings.Repeat("long", 1000)); err != nil {
			t.Fatalf("writeArtifactFileAtomic long: %v", err)
		}
		if err := writeArtifactFileAtomic(path, "short"); err != nil {
			t.Fatalf("writeArtifactFileAtomic short: %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "short" {
			t.Fatalf("file content = %q, want %q", got, "short")
		}
	})
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
