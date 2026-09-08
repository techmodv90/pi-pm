package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
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

// TestArtifactProjectionFailureIsBestEffort is the RED end-to-end test for
// best-effort projection failure semantics (Plan §API Contract, NC-2): when
// the artifacts directory is unwritable, the canonical save must still
// succeed with the work_item_artifacts row stored, the response must carry an
// empty file_path, a warning event artifact_projection_failed with payload
// {stage, revision, file_path, error} must record the failed projection, and
// no markdown file may exist at the deterministic path. The save integration
// (T009) does not exist yet, so the missing warning event and empty file_path
// fail first while the canonical save already succeeds.
func TestArtifactProjectionFailureIsBestEffort(t *testing.T) {
	bin := buildPic(t)
	root, home, id := initArtifactFileProject(t, bin)
	db := openArtifactProjectDB(t, root)

	// Unwritable artifacts dir: canonical SQLite stays writable, file
	// projection cannot.
	makeArtifactsDirUnwritable(t, root)

	artifact := saveArtifact(t, bin, root, home, id, "vision", validVisionArtifact)
	if artifact["revision"] != float64(1) {
		t.Fatalf("vision artifact = %#v", artifact)
	}
	// Canonical persistence is best-effort's anchor: the row must be stored.
	revision, content, contentHash := artifactFileRow(t, db, id, "vision")
	wantHash := hashJSON(validVisionArtifact)
	if revision != 1 || content != validVisionArtifact || contentHash != wantHash {
		t.Fatalf("vision artifact row = rev %d content %q hash %q, want hash %s", revision, content, contentHash, wantHash)
	}
	// NC-2: failed projection surfaces an empty file_path, not an error.
	if artifact["file_path"] != "" {
		t.Fatalf("response file_path = %v, want empty string", artifact["file_path"])
	}
	// NC-2: the failed projection is recorded as a warning event.
	assertWorkItemEventCount(t, db, id, "artifact_projection_failed", 1)
	var eventType, payloadJSON string
	if err := db.QueryRow(`SELECT event_type,payload_json FROM work_item_events WHERE work_item_id=? AND event_type='artifact_projection_failed' LIMIT 1`, id).Scan(&eventType, &payloadJSON); err != nil {
		t.Fatalf("artifact_projection_failed event: %v", err)
	}
	wantPath, err := artifactFilePath(root, id, "vision", 1)
	if err != nil {
		t.Fatalf("artifactFilePath: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("warning payload not JSON: %v (%s)", err, payloadJSON)
	}
	if payload["stage"] != "vision" || payload["revision"] != float64(1) || payload["file_path"] != wantPath {
		t.Fatalf("warning payload = %#v, want stage vision revision 1 file_path %s", payload, wantPath)
	}
	if errMsg, _ := payload["error"].(string); errMsg == "" {
		t.Fatalf("warning payload error detail missing: %#v", payload)
	}
	// NC-2: no projected markdown may exist at the deterministic path.
	if _, statErr := os.Stat(wantPath); !os.IsNotExist(statErr) {
		t.Fatalf("projected file exists at %s despite unwritable dir: stat err=%v", wantPath, statErr)
	}
}

// TestArtifactProjectionP95Under50ms is the bounded timing test for
// projection overhead (Plan §3.1 NC-6): the projection step added to every
// artifact-save — deterministic path construction plus the atomic markdown
// write plus the artifact_files binding insert — must stay under 50ms at the
// 95th percentile. The sample uses a realistic vision-sized artifact against
// a temp project tree and a temp SQLite database; the measured p95 is always
// printed to stdout (visible even when the test passes non-verbose), and the
// test fails when it reaches 50ms.
func TestArtifactProjectionP95Under50ms(t *testing.T) {
	root := t.TempDir()
	const iterations = 100
	const p95LimitMS = 50.0
	content := validVisionArtifact

	// Temp SQLite database holding only the artifact_files binding table so
	// the timed loop measures the real bindArtifactFile INSERT; foreign keys
	// stay off so no parent work_items row is required for the benchmark.
	db, err := sql.Open("sqlite", filepath.Join(root, "bench.db"))
	if err != nil {
		t.Fatalf("open bench db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(artifactFilesTableSQL); err != nil {
		t.Fatalf("create artifact_files table: %v", err)
	}

	durations := make([]float64, 0, iterations)
	for i := 1; i <= iterations; i++ {
		start := time.Now()
		path, err := artifactFilePath(root, "wi-p95bench", "vision", i)
		if err != nil {
			t.Fatalf("artifactFilePath(%d): %v", i, err)
		}
		if err := writeArtifactFileAtomic(path, content); err != nil {
			t.Fatalf("writeArtifactFileAtomic(%d): %v", i, err)
		}
		if err := bindArtifactFile(db, fmt.Sprintf("wia-p95-%d", i), "wi-p95bench", "vision", i, path, hashJSON(content)); err != nil {
			t.Fatalf("bindArtifactFile(%d): %v", i, err)
		}
		durations = append(durations, float64(time.Since(start).Microseconds())/1000.0)
	}

	sort.Float64s(durations)
	p95 := durations[int(float64(len(durations))*0.95)]
	fmt.Printf("projection overhead p95 = %.3fms over %d iterations (limit %.0fms)\n", p95, iterations, p95LimitMS)
	if p95 >= p95LimitMS {
		t.Fatalf("projection overhead p95 = %.3fms, want < %.0fms", p95, p95LimitMS)
	}
	// The projection must actually land the bytes: the last revision's file
	// must hold the exact content, keeping the timing test honest.
	got, err := os.ReadFile(filepath.Join(root, ".apm", "artifacts", "wi-p95bench", fmt.Sprintf("vision-r%d.md", iterations)))
	if err != nil {
		t.Fatalf("read projected file: %v", err)
	}
	if string(got) != content {
		t.Fatalf("projected bytes = %q, want exact content %q", got, content)
	}
}

// TestArtifactRevisionCreatesNewFile is the RED test for revision projection
// (Feature US3; Plan Flow invariant and immutable artifact model): saving
// revision 2 of the same work item + stage must project the revised bytes
// onto a NEW deterministic path (blueprint-r2.md) and never rewrite the
// prior revision's file — revision 1's bytes must remain exactly unchanged.
func TestArtifactRevisionCreatesNewFile(t *testing.T) {
	bin := buildPic(t)
	root, home, id := initArtifactFileProject(t, bin)
	db := openArtifactProjectDB(t, root)

	// Given: the work item has a blueprint revision 1 with its projected
	// markdown at the deterministic revision-1 path.
	first := saveArtifact(t, bin, root, home, id, "blueprint", validBlueprintArtifact)
	if first["revision"] != float64(1) {
		t.Fatalf("first blueprint artifact = %#v", first)
	}
	r1Path, err := artifactFilePath(root, id, "blueprint", 1)
	if err != nil {
		t.Fatalf("artifactFilePath revision 1: %v", err)
	}
	r1Bytes, err := os.ReadFile(r1Path)
	if err != nil {
		t.Fatalf("revision 1 markdown missing at %s: %v", r1Path, err)
	}
	if string(r1Bytes) != validBlueprintArtifact {
		t.Fatalf("revision 1 bytes = %q, want exact content %q", r1Bytes, validBlueprintArtifact)
	}

	// When: revision 2 is saved with revised content for the same work item
	// and stage.
	revised := strings.Replace(validBlueprintArtifact, "Reliable workflow", "Revised workflow", 1)
	if revised == validBlueprintArtifact {
		t.Fatal("revised content identical to revision 1; fixture must differ")
	}
	second := saveArtifact(t, bin, root, home, id, "blueprint", revised)
	if second["revision"] != float64(2) {
		t.Fatalf("second blueprint artifact = %#v, want revision 2", second)
	}

	// Then: revision 2 lands on a new deterministic path, not revision 1's.
	r2Path, err := artifactFilePath(root, id, "blueprint", 2)
	if err != nil {
		t.Fatalf("artifactFilePath revision 2: %v", err)
	}
	if r2Path == r1Path {
		t.Fatal("revision 2 path collides with revision 1 path")
	}
	if second["file_path"] != r2Path {
		t.Fatalf("revision 2 response file_path = %v, want %s", second["file_path"], r2Path)
	}
	r2Bytes, err := os.ReadFile(r2Path)
	if err != nil {
		t.Fatalf("revision 2 markdown missing at %s: %v", r2Path, err)
	}
	if string(r2Bytes) != revised {
		t.Fatalf("revision 2 bytes = %q, want revised content %q", r2Bytes, revised)
	}
	// And: revision 1's bytes are unchanged after the revision 2 save.
	r1After, err := os.ReadFile(r1Path)
	if err != nil {
		t.Fatalf("re-read revision 1 markdown: %v", err)
	}
	if string(r1After) != string(r1Bytes) {
		t.Fatalf("revision 1 bytes changed after revision 2 save: %q, want %q", r1After, r1Bytes)
	}
	// Canonical rows mirror both revisions, and each revision keeps its own
	// artifact_files binding at a distinct path.
	revision, content, contentHash := artifactFileRow(t, db, id, "blueprint")
	if revision != 2 || content != revised || contentHash != second["content_hash"] {
		t.Fatalf("latest blueprint row = rev %d content %q hash %q, want revision 2 hash %v", revision, content, contentHash, second["content_hash"])
	}
	var bindCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM artifact_files WHERE work_item_id=? AND stage=?`, id, "blueprint").Scan(&bindCount); err != nil {
		t.Fatalf("count blueprint bindings: %v", err)
	}
	if bindCount != 2 {
		t.Fatalf("artifact_files blueprint bindings = %d, want one per revision (2)", bindCount)
	}
	var boundPath string
	if err := db.QueryRow(`SELECT file_path FROM artifact_files WHERE work_item_id=? AND stage=? AND revision=1`, id, "blueprint").Scan(&boundPath); err != nil {
		t.Fatalf("revision 1 binding row: %v", err)
	}
	if boundPath != r1Path {
		t.Fatalf("revision 1 binding file_path = %q, want %q", boundPath, r1Path)
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

// TestArtifactFileConflictBlocksSave is the RED test for the conflict
// pre-flight (Feature US4; Plan §API Contract conflict pre-flight): when a
// file already exists at the deterministic revision-1 path with bytes that
// differ from the artifact content and no artifact row exists yet, the save
// must fail with an "artifact file conflict" error naming the path, leave
// the divergent bytes unchanged, and store no artifact row (pre-flight runs
// before any DB write, so a conflict strands no committed artifact).
func TestArtifactFileConflictBlocksSave(t *testing.T) {
	bin := buildPic(t)
	root, home, id := initArtifactFileProject(t, bin)
	db := openArtifactProjectDB(t, root)

	// Given: the planning chain up to contracts is approved (scan accepted,
	// rri/vision/blueprint approved) so the stage gates are satisfied, and a
	// divergent contracts-r1.md already exists at the deterministic path
	// while no contracts artifact row exists.
	contents := map[string]string{
		"scan":      "scan content",
		"rri":       "# RRI Report\n\nRequirement matrix follows.",
		"vision":    validVisionArtifact,
		"blueprint": validBlueprintArtifact,
	}
	for _, stage := range []string{"scan", "rri", "vision", "blueprint"} {
		artifact := saveArtifact(t, bin, root, home, id, stage, contents[stage])
		decision := "approved"
		if stage == "scan" {
			decision = "accepted"
		}
		runPic(t, bin, root, home, "work-item", "artifact-approve", id, stage, artifact["id"].(string), decision)
	}
	conflictPath, err := artifactFilePath(root, id, "contracts", 1)
	if err != nil {
		t.Fatalf("artifactFilePath contracts revision 1: %v", err)
	}
	if err := writeArtifactFileAtomic(conflictPath, "# Contracts\n"); err != nil {
		t.Fatalf("pre-create divergent markdown: %v", err)
	}
	preExisting, err := os.ReadFile(conflictPath)
	if err != nil {
		t.Fatalf("read pre-created markdown: %v", err)
	}

	// When: an artifact is saved for contracts revision 1 whose content
	// differs from the pre-created bytes on disk.
	output := runPicError(t, bin, root, home, "work-item", "artifact-save", id, "contracts", validContractArtifact)

	// Then: the operation fails with a conflict error naming the path.
	if !strings.Contains(output, "artifact file conflict") {
		t.Fatalf("save output missing conflict text: %q", output)
	}
	if !strings.Contains(output, conflictPath) {
		t.Fatalf("save output missing conflict path %s: %q", conflictPath, output)
	}
	// And: the divergent bytes at the path are unchanged.
	after, err := os.ReadFile(conflictPath)
	if err != nil {
		t.Fatalf("re-read divergent markdown: %v", err)
	}
	if string(after) != string(preExisting) {
		t.Fatalf("conflicting bytes changed after blocked save: %q, want %q", after, preExisting)
	}
	// And: no artifact row was stranded by the failed save.
	var rowCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=? AND stage=?`, id, "contracts").Scan(&rowCount); err != nil {
		t.Fatalf("count contracts artifact rows: %v", err)
	}
	if rowCount != 0 {
		t.Fatalf("work_item_artifacts contracts rows = %d, want 0 after conflict", rowCount)
	}
	var bindCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM artifact_files WHERE work_item_id=? AND stage=?`, id, "contracts").Scan(&bindCount); err != nil {
		t.Fatalf("count contracts bindings: %v", err)
	}
	if bindCount != 0 {
		t.Fatalf("artifact_files contracts bindings = %d, want 0 after conflict", bindCount)
	}
}

// TestArtifactFileIntegrityCheck is the RED test for the on-demand integrity
// check (Feature US5; Plan API Contract `pic work-item artifact-check`, NC-7
// on-demand hash comparison): for every bound artifact_files row of the work
// item, the check reports status ok when the file bytes hash to
// content_sha256, drift (with its file_path) when they no longer do, and
// missing when the bound file is absent. The artifact-check command is not
// routed yet, so the first failure is the missing command.
func TestArtifactFileIntegrityCheck(t *testing.T) {
	bin := buildPic(t)
	root, home, id := initArtifactFileProject(t, bin)

	// Given: three bound artifact_files rows for the work item whose file
	// bytes will be ok, drifted, and missing respectively; approvals follow
	// the scan→rri→vision chain order so the stage gates stay satisfied.
	okArtifact := saveArtifact(t, bin, root, home, id, "scan", "scan content")
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", okArtifact["id"].(string), "accepted")
	driftArtifact := saveArtifact(t, bin, root, home, id, "rri", "# RRI Report\n\nRequirement matrix follows.")
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "rri", driftArtifact["id"].(string), "approved")
	missingArtifact := saveArtifact(t, bin, root, home, id, "vision", validVisionArtifact)
	okPath := okArtifact["file_path"].(string)
	driftPath := driftArtifact["file_path"].(string)
	missingPath := missingArtifact["file_path"].(string)
	if okPath == "" || driftPath == "" || missingPath == "" {
		t.Fatalf("save responses missing file_path: %v %v %v", okArtifact["file_path"], driftArtifact["file_path"], missingArtifact["file_path"])
	}

	// When: the rri file bytes drift from content_sha256 and the vision file
	// is removed entirely; the scan file keeps its canonical bytes.
	if err := os.WriteFile(driftPath, []byte("tampered bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(missingPath); err != nil {
		t.Fatal(err)
	}

	// Then: artifact-check reports one entry per bound artifact with status
	// ok | drift | missing per NC-7, and the untouched artifact passes.
	report := asArray(t, runPic(t, bin, root, home, "work-item", "artifact-check", id))
	if len(report) != 3 {
		t.Fatalf("artifact-check returned %d entries, want 3: %#v", len(report), report)
	}
	entries := map[string]map[string]any{}
	for _, item := range report {
		row := asObject(t, item)
		// NC-7 response shape: every entry must carry string artifact_id,
		// file_path, and status fields; missing or non-string values fail.
		artifactID, okID := row["artifact_id"].(string)
		if !okID || artifactID == "" {
			t.Fatalf("artifact-check entry missing string artifact_id: %#v", row)
		}
		entryPath, okPathField := row["file_path"].(string)
		if !okPathField || entryPath == "" {
			t.Fatalf("artifact-check entry %s missing string file_path: %#v", artifactID, row)
		}
		if _, okStatus := row["status"].(string); !okStatus {
			t.Fatalf("artifact-check entry %s missing string status: %#v", artifactID, row)
		}
		entries[artifactID] = row
	}
	if row := entries[okArtifact["id"].(string)]; row == nil || row["status"] != "ok" || row["file_path"] != okPath {
		t.Fatalf("scan artifact check = %#v, want status ok with file_path %s", row, okPath)
	}
	if row := entries[driftArtifact["id"].(string)]; row == nil || row["status"] != "drift" || row["file_path"] != driftPath {
		t.Fatalf("rri artifact check = %#v, want status drift with file_path %s", row, driftPath)
	}
	if row := entries[missingArtifact["id"].(string)]; row == nil || row["status"] != "missing" || row["file_path"] != missingPath {
		t.Fatalf("vision artifact check = %#v, want status missing with file_path %s", row, missingPath)
	}
}

// TestArtifactFileBackfill is the RED test for historical backfill (Feature
// US6; Plan API Contract backfill recovery intent): a file backfill run for
// a work item whose artifact rows predate the artifact_files projection must
// bind every artifact row to an artifact_files row with content_sha256 equal
// to the canonical content_hash and project the exact stored content bytes
// at the deterministic path, overwriting any divergent pre-existing bytes.
// The artifact-backfill command is not routed yet (T014 deliberately skipped
// routing to avoid pre-satisfying this RED phase), so the first failure is
// the unknown command at the invocation point (T013/T014 RED precedent).
func TestArtifactFileBackfill(t *testing.T) {
	bin := buildPic(t)
	root, home, id := initArtifactFileProject(t, bin)
	db := openArtifactProjectDB(t, root)

	// Given: two artifact rows saved through the CLI and then reverted to a
	// pre-projection historical state — their artifact_files bindings are
	// deleted, the scan file is removed entirely, and the rri path holds
	// divergent pre-existing bytes that canonical content must overwrite.
	scanArtifact := saveArtifact(t, bin, root, home, id, "scan", "scan content")
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "scan", scanArtifact["id"].(string), "accepted")
	rriArtifact := saveArtifact(t, bin, root, home, id, "rri", "# RRI Report\n\nRequirement matrix follows.")
	if _, err := db.Exec(`DELETE FROM artifact_files WHERE work_item_id=?`, id); err != nil {
		t.Fatalf("reset artifact_files to historical state: %v", err)
	}
	scanPath, err := artifactFilePath(root, id, "scan", 1)
	if err != nil {
		t.Fatalf("artifactFilePath scan: %v", err)
	}
	if err := os.Remove(scanPath); err != nil {
		t.Fatalf("remove historical scan markdown: %v", err)
	}
	rriPath, err := artifactFilePath(root, id, "rri", 1)
	if err != nil {
		t.Fatalf("artifactFilePath rri: %v", err)
	}
	if err := os.WriteFile(rriPath, []byte("divergent pre-existing bytes"), 0o600); err != nil {
		t.Fatalf("pre-create divergent rri markdown: %v", err)
	}
	var bindCount, artifactCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM artifact_files WHERE work_item_id=?`, id).Scan(&bindCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=?`, id).Scan(&artifactCount); err != nil {
		t.Fatal(err)
	}
	if bindCount != 0 || artifactCount != 2 {
		t.Fatalf("historical fixture: artifact_files=%d work_item_artifacts=%d, want 0 unbound rows over 2 artifacts", bindCount, artifactCount)
	}

	// When: a file backfill is run for work_item_id id.
	runPic(t, bin, root, home, "work-item", "artifact-backfill", id)

	// Then: every artifact row is bound to an artifact_files row with
	// content_sha256 equal to content_hash, and each bound file holds the
	// exact stored content — canonical bytes overwrite the divergent rri
	// file and backfill the missing scan file.
	fixtures := []struct {
		stage    string
		artifact map[string]any
	}{
		{"scan", scanArtifact},
		{"rri", rriArtifact},
	}
	for _, fixture := range fixtures {
		artifactID := fixture.artifact["id"].(string)
		_, content, contentHash := artifactFileRow(t, db, id, fixture.stage)
		var boundPath, boundSHA string
		if err := db.QueryRow(`SELECT file_path,content_sha256 FROM artifact_files WHERE artifact_id=?`, artifactID).Scan(&boundPath, &boundSHA); err != nil {
			t.Fatalf("artifact %s (%s) not bound after backfill: %v", artifactID, fixture.stage, err)
		}
		wantPath, err := artifactFilePath(root, id, fixture.stage, 1)
		if err != nil {
			t.Fatalf("artifactFilePath %s: %v", fixture.stage, err)
		}
		if boundPath != wantPath {
			t.Fatalf("%s binding file_path = %q, want %q", fixture.stage, boundPath, wantPath)
		}
		if boundSHA != contentHash {
			t.Fatalf("%s binding content_sha256 = %q, want content_hash %q", fixture.stage, boundSHA, contentHash)
		}
		got, err := os.ReadFile(wantPath)
		if err != nil {
			t.Fatalf("%s markdown missing at %s after backfill: %v", fixture.stage, wantPath, err)
		}
		if string(got) != content {
			t.Fatalf("%s projected bytes = %q, want exact stored content %q", fixture.stage, got, content)
		}
	}
	// And: the backfill binds exactly one row per artifact — no strays.
	if err := db.QueryRow(`SELECT COUNT(*) FROM artifact_files WHERE work_item_id=?`, id).Scan(&bindCount); err != nil {
		t.Fatal(err)
	}
	if bindCount != 2 {
		t.Fatalf("artifact_files rows after backfill = %d, want one per artifact (2)", bindCount)
	}
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
