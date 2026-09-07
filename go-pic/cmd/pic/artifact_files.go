package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// workItemIDPattern guards artifact path construction: only ratified
// work-item ids (^wi-[a-z0-9]+$) may reach filepath.Join so a hostile id
// cannot escape the artifacts directory.
var workItemIDPattern = regexp.MustCompile(`^wi-[a-z0-9]+$`)

// artifactFilePath returns the deterministic artifact markdown path
// <root>/.apm/artifacts/<work_item>/<stage>-r<revision>.md.
func artifactFilePath(root string, workItemID string, stage string, revision int) (string, error) {
	if !workItemIDPattern.MatchString(workItemID) {
		return "", fmt.Errorf("invalid work item id %q", workItemID)
	}
	return filepath.Join(root, ".apm", "artifacts", workItemID, fmt.Sprintf("%s-r%d.md", stage, revision)), nil
}

// artifactProjectRoot resolves the current project root the same way the
// CLI does (findDB upward walk), so deterministic artifact paths anchor to
// <project>/.apm/artifacts regardless of the invocation directory.
func artifactProjectRoot() string {
	cwd, _ := os.Getwd()
	dbPath := findDB(cwd)
	if dbPath == "" {
		return cwd
	}
	return filepath.Dir(filepath.Dir(dbPath))
}

// artifactFileHashMatches reports whether the file at path already holds
// exactly the given content (sha256 comparison). A missing file is an error.
func artifactFileHashMatches(path string, content string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256([]byte(content))
	existingSum := sha256.Sum256(existing)
	return string(sum[:]) == string(existingSum[:]), nil
}

// bindArtifactFile records the artifact_files binding row for a projected
// markdown file. A single INSERT is its own transaction; the caller treats
// failure as a best-effort projection failure (warning event, empty path).
func bindArtifactFile(db *sql.DB, artifactID, workItemID, stage string, revision int, filePath, contentSHA256 string) error {
	_, err := db.Exec(`INSERT INTO artifact_files(id,artifact_id,work_item_id,stage,revision,file_path,content_sha256) VALUES(?,?,?,?,?,?,?)`, "wiaf-"+shortID(), artifactID, workItemID, stage, revision, filePath, contentSHA256)
	return err
}

// writeArtifactFileAtomic writes content to path via a 0600 temp file in the
// target directory followed by os.Rename, creating parent directories with
// 0700. Atomic rename means no partial artifact is ever visible at path.
func writeArtifactFileAtomic(path string, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".artifact-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write([]byte(content)); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
