package workitem

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

// testDB opens an in-memory SQLite database with the minimal schema the
// package's SQL touches. The real schema lives in cmd/pic's migrations, which
// package tests cannot import; these fixtures pin only the columns the
// work-item store reads and writes.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	schema := []string{
		`CREATE TABLE work_items (
			id TEXT PRIMARY KEY, type TEXT NOT NULL, parent_id TEXT, title TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'open',
			priority TEXT NOT NULL DEFAULT 'medium', deferred INTEGER NOT NULL DEFAULT 0,
			claimed_at TEXT NOT NULL DEFAULT '', claimed_by TEXT NOT NULL DEFAULT '',
			review_status TEXT NOT NULL DEFAULT 'pending', review_notes TEXT NOT NULL DEFAULT '',
			planning_depth TEXT NOT NULL DEFAULT 'full', created_at TEXT NOT NULL DEFAULT (datetime('now')),
			decomposition_mode TEXT NOT NULL DEFAULT '', decomposition_reason TEXT NOT NULL DEFAULT '',
			paired_contract_node TEXT NOT NULL DEFAULT '', source_graph_artifact_id TEXT NOT NULL DEFAULT '',
			source_graph_revision INTEGER NOT NULL DEFAULT 0, source_graph_content_hash TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE work_item_labels (work_item_id TEXT NOT NULL, label TEXT NOT NULL, PRIMARY KEY(work_item_id, label))`,
		`CREATE TABLE work_item_relations (
			id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL, relation_type TEXT NOT NULL,
			related_work_item_id TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT (datetime('now')))`,
		`CREATE TABLE work_item_events (
			id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL, event_type TEXT NOT NULL,
			actor_role TEXT NOT NULL, actor_model TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL DEFAULT (datetime('now')))`,
		`CREATE TABLE work_item_instruction_packs (
			id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL, status TEXT NOT NULL,
			version INTEGER NOT NULL DEFAULT 1, content_hash TEXT NOT NULL DEFAULT '', stale_at TEXT)`,
		`CREATE TABLE work_item_materializations (work_item_id TEXT NOT NULL, root_work_item_id TEXT NOT NULL, checkpoint_id TEXT NOT NULL)`,
		`CREATE TABLE implementation_authorizations (
			id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL, task_graph_checkpoint_id TEXT NOT NULL DEFAULT '',
			revoked_at TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE pipeline_runs (
			id TEXT PRIMARY KEY, task_id TEXT NOT NULL, stage TEXT NOT NULL, status TEXT NOT NULL,
			attempt INTEGER NOT NULL DEFAULT 1, candidate_run_id TEXT NOT NULL DEFAULT '',
			candidate_patch_hash TEXT NOT NULL DEFAULT '', instruction_pack_id TEXT NOT NULL DEFAULT '',
			instruction_pack_version INTEGER NOT NULL DEFAULT 0, instruction_pack_hash TEXT NOT NULL DEFAULT '',
			artifact_saved_at TEXT NOT NULL DEFAULT '', integrated_at TEXT NOT NULL DEFAULT '',
			integrated_patch_hash TEXT NOT NULL DEFAULT '', integrated_patch_path TEXT NOT NULL DEFAULT '',
			result_json TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')), updated_at TEXT, completed_at TEXT)`,
		`CREATE TABLE work_item_artifacts (
			id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL, stage TEXT NOT NULL, revision INTEGER NOT NULL,
			content TEXT NOT NULL, content_hash TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT (datetime('now')))`,
		`CREATE TABLE work_item_verification_reports (
			id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL, completion_report_id TEXT,
			checkpoint_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, summary TEXT NOT NULL DEFAULT '',
			verified_by_role TEXT NOT NULL DEFAULT '', pipeline_high_water_rowid INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now')))`,
		`CREATE TABLE work_item_completion_reports (
			id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL, pipeline_run_id TEXT NOT NULL,
			instruction_pack_id TEXT NOT NULL, instruction_pack_version INTEGER NOT NULL,
			instruction_pack_hash TEXT NOT NULL, status TEXT NOT NULL, summary TEXT NOT NULL DEFAULT '',
			report_markdown TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT (datetime('now')))`,
		`CREATE TABLE work_item_owner_decisions (
			id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL, completion_report_id TEXT NOT NULL,
			decision TEXT NOT NULL, notes TEXT NOT NULL DEFAULT '', decided_by_role TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')))`,
		`CREATE TABLE requirements (
			id TEXT PRIMARY KEY, task_id TEXT, epic_id TEXT, requirement_key TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '',
			acceptance_criteria TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'approved')`,
		`CREATE TABLE workflow_checkpoints (
			id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL, stage TEXT NOT NULL, artifact_id TEXT NOT NULL DEFAULT '',
			artifact_revision INTEGER NOT NULL DEFAULT 0, decision_type TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')))`,
		`CREATE TABLE work_item_delivery_states (
			work_item_id TEXT PRIMARY KEY, integration_mode TEXT NOT NULL DEFAULT 'coordination',
			branch_name TEXT NOT NULL DEFAULT '', base_branch TEXT NOT NULL DEFAULT '',
			base_commit TEXT NOT NULL DEFAULT '', verified_head TEXT NOT NULL DEFAULT '',
			verification_report_id TEXT NOT NULL DEFAULT '', merge_status TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')))`,
		`CREATE TABLE work_item_aggregate_owner_decisions (
			id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL, verification_report_id TEXT NOT NULL,
			decision TEXT NOT NULL, notes TEXT NOT NULL DEFAULT '', decided_by_role TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')))`,
	}
	for _, statement := range schema {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("fixture schema: %v", err)
		}
	}
	return db
}

// insertItem seeds one work_items row; overrides map column names (only the
// mutable subset tests need) to values.
func insertItem(t *testing.T, db *sql.DB, id, kind string, overrides map[string]any) {
	t.Helper()
	allowed := map[string]bool{"parent_id": true, "status": true, "claimed_at": true, "claimed_by": true, "review_status": true, "review_notes": true, "deferred": true, "planning_depth": true}
	columns, values := "id, type, title", []any{id, kind, "title " + id}
	for column, value := range overrides {
		if !allowed[column] {
			t.Fatalf("insertItem override not allowed: %s", column)
		}
		columns += ", " + column
		values = append(values, value)
	}
	if _, err := db.Exec(`INSERT INTO work_items (`+columns+`) VALUES (`+strings.TrimSuffix(strings.Repeat("?,", len(values)), ",")+`)`, values...); err != nil {
		t.Fatalf("insert item %s: %v", id, err)
	}
}

// stdoutMu serializes os.Stdout redirection: parallel tests share the
// process-global stream, so captures must not interleave.
var stdoutMu sync.Mutex

// captureStdout redirects os.Stdout for the duration of fn and returns what
// the code under test printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	original := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = write
	done := make(chan string)
	go func() {
		data := make([]byte, 4096)
		n, _ := read.Read(data)
		done <- string(data[:n])
	}()
	fn()
	write.Close()
	os.Stdout = original
	return <-done
}

// decodeJSON parses one JSON object from captured output.
func decodeJSON(t *testing.T, out string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(out), &value); err != nil {
		t.Fatalf("output not JSON object: %v (%q)", err, out)
	}
	return value
}

// marshalJSON serializes a value for embedding in fixture content.
func marshalJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return string(data)
}
