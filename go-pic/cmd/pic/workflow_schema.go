package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

const tasksTableSQL = `CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY,
	epic_id TEXT REFERENCES epics(id),
	title TEXT NOT NULL,
	description TEXT DEFAULT '',
	notes TEXT DEFAULT '',
	origin TEXT DEFAULT 'manual' CHECK(origin IN ('manual','materialized')),
	revision INTEGER DEFAULT 1 CHECK(revision > 0),

	completed_by_model TEXT DEFAULT '',
	review_status TEXT DEFAULT '' CHECK(review_status IN ('','pending','passed','failed')),
	review_notes TEXT DEFAULT '',
	reviewed_instruction_pack_id TEXT DEFAULT '',
	workflow_mode TEXT DEFAULT 'standard' CHECK(workflow_mode IN ('quick','standard','designed','full')),
	workflow_confidence REAL DEFAULT 0,
	workflow_reason TEXT DEFAULT '',
	design_status TEXT DEFAULT '' CHECK(design_status IN ('','pending','approved','rejected')),
	owner_status TEXT DEFAULT '' CHECK(owner_status IN ('','pending','accepted','rejected')),
	status TEXT DEFAULT 'open' CHECK(status IN ('open','in_progress','done','cancelled')),
	priority TEXT DEFAULT 'medium' CHECK(priority IN ('low','medium','high')),
	created_at TEXT DEFAULT (datetime('now'))
)`

const workItemsTableSQL = `CREATE TABLE IF NOT EXISTS work_items (
	id TEXT PRIMARY KEY,
	type TEXT NOT NULL CHECK(type IN ('epic','feature','task','bug','chore','gate')),
	parent_id TEXT REFERENCES work_items(id),
	title TEXT NOT NULL,
	description TEXT DEFAULT '',
	status TEXT DEFAULT 'open' CHECK(status IN ('open','in_progress','done','cancelled')),
	priority TEXT DEFAULT 'medium' CHECK(priority IN ('low','medium','high')),
	deferred INTEGER NOT NULL DEFAULT 0 CHECK(deferred IN (0,1)),
	claimed_at TEXT DEFAULT '',
	claimed_by TEXT DEFAULT '',
	review_status TEXT DEFAULT 'pending' CHECK(review_status IN ('pending','passed','failed')),
	review_notes TEXT DEFAULT '',
	planning_depth TEXT DEFAULT 'full' CHECK(planning_depth IN ('quick','standard','designed','full')),
	created_at TEXT DEFAULT (datetime('now'))
)`

var ownedWorkflowTableSQL = map[string]string{
	// Canonical Work Item flow stores wi-/wip- IDs in task_id/epic_id, so these
	// tables must not carry legacy REFERENCES tasks(id)/epics(id) constraints;
	// migrateEpicWorkflowSchema rebuilds databases that still have them.
	"scan_reports":         `CREATE TABLE IF NOT EXISTS scan_reports (id TEXT PRIMARY KEY, task_id TEXT, epic_id TEXT, status TEXT DEFAULT 'completed' CHECK(status IN ('completed','partial','failed')), summary TEXT DEFAULT '', tech_stack_json TEXT DEFAULT '', architecture_json TEXT DEFAULT '', commands_json TEXT DEFAULT '', patterns_json TEXT DEFAULT '', risks_json TEXT DEFAULT '', raw_report TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')), CHECK((task_id IS NOT NULL) != (epic_id IS NOT NULL)))`,
	"rri_sessions":         `CREATE TABLE IF NOT EXISTS rri_sessions (id TEXT PRIMARY KEY, task_id TEXT, epic_id TEXT, status TEXT DEFAULT 'preparing' CHECK(status IN ('preparing','interviewing','awaiting_confirmation','completed','abandoned')), interview_state_json TEXT DEFAULT '', report_markdown TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')), updated_at TEXT DEFAULT (datetime('now')), completed_at TEXT DEFAULT '', CHECK((task_id IS NOT NULL) != (epic_id IS NOT NULL)))`,
	"requirements":         `CREATE TABLE IF NOT EXISTS requirements (id TEXT PRIMARY KEY, task_id TEXT, epic_id TEXT, rri_session_id TEXT, requirement_key TEXT NOT NULL, contract_key TEXT DEFAULT '', inherit_to_descendants INTEGER NOT NULL DEFAULT 0 CHECK(inherit_to_descendants IN (0,1)), persona TEXT DEFAULT '', priority TEXT DEFAULT 'tier2' CHECK(priority IN ('tier1','tier2','tier3')), title TEXT NOT NULL, description TEXT DEFAULT '', acceptance_criteria TEXT DEFAULT '', status TEXT DEFAULT 'pending' CHECK(status IN ('pending','satisfied','failed','deferred')), source TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')), CHECK((task_id IS NOT NULL) != (epic_id IS NOT NULL)))`,
	"designs":              `CREATE TABLE IF NOT EXISTS designs (id TEXT PRIMARY KEY, task_id TEXT, epic_id TEXT, version INTEGER NOT NULL, status TEXT DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected','superseded')), blueprint_markdown TEXT NOT NULL, contracts_markdown TEXT DEFAULT '', decisions_json TEXT DEFAULT '', risks_json TEXT DEFAULT '', created_by_role TEXT DEFAULT 'contractor', approved_at TEXT DEFAULT '', rejected_at TEXT DEFAULT '', rejection_reason TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')), CHECK((task_id IS NOT NULL) != (epic_id IS NOT NULL)))`,
	"verification_reports": `CREATE TABLE IF NOT EXISTS verification_reports (id TEXT PRIMARY KEY, task_id TEXT, epic_id TEXT, pipeline_run_id TEXT DEFAULT '', effective_contract_snapshot_id TEXT DEFAULT '', effective_contract_snapshot_hash TEXT DEFAULT '', status TEXT NOT NULL CHECK(status IN ('passed','failed','partial','blocked')), summary TEXT DEFAULT '', verified_by_role TEXT DEFAULT 'contractor', verified_by_model TEXT DEFAULT '', superseded_at TEXT DEFAULT '', superseded_by_report_id TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')), CHECK((task_id IS NOT NULL) != (epic_id IS NOT NULL)))`,
	"escalations":          `CREATE TABLE IF NOT EXISTS escalations (id TEXT PRIMARY KEY, task_id TEXT, epic_id TEXT, level INTEGER NOT NULL CHECK(level IN (1,2,3)), status TEXT DEFAULT 'open' CHECK(status IN ('open','resolved','cancelled')), title TEXT NOT NULL, description TEXT DEFAULT '', options_json TEXT DEFAULT '', recommendation TEXT DEFAULT '', decision TEXT DEFAULT '', resolved_by_role TEXT DEFAULT '', resolved_at TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')), CHECK((task_id IS NOT NULL) != (epic_id IS NOT NULL)))`,
	"owner_decisions":      `CREATE TABLE IF NOT EXISTS owner_decisions (id TEXT PRIMARY KEY, task_id TEXT, epic_id TEXT, related_type TEXT DEFAULT '', related_id TEXT DEFAULT '', decision_type TEXT NOT NULL, decision TEXT NOT NULL, notes TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')), CHECK((task_id IS NOT NULL) != (epic_id IS NOT NULL)))`,
}

func hasColumn(db workflowStore, table, column string) bool {
	ok, _ := rowExists(db, `SELECT 1 FROM pragma_table_info(?) WHERE name = ?`, table, column)
	return ok
}

func migrateEpicWorkflowSchema(db *sql.DB) error {
	if !tableExists(db, "tasks") {
		return nil
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	if _, err := db.Exec(`PRAGMA legacy_alter_table=ON`); err != nil {
		return err
	}
	defer db.Exec(`PRAGMA legacy_alter_table=OFF`)

	var epicNotNull int
	_ = db.QueryRow(`SELECT "notnull" FROM pragma_table_info('tasks') WHERE name='epic_id'`).Scan(&epicNotNull)
	if epicNotNull != 0 || !hasColumn(db, "tasks", "origin") || !hasColumn(db, "tasks", "revision") || hasColumn(db, "tasks", "refined") {
		if err := rebuildSchemaTable(db, "tasks", tasksTableSQL); err != nil {
			return err
		}
	}
	for table, createSQL := range ownedWorkflowTableSQL {
		if tableExists(db, table) && (!hasColumn(db, table, "epic_id") || ownerColumnNotNull(db, table, "task_id") || hasLegacySubjectForeignKey(db, table)) {
			if err := rebuildSchemaTable(db, table, createSQL); err != nil {
				return err
			}
		}
	}
	return migrateLegacyWorkItems(db)
}

// hasLegacySubjectForeignKey reports whether table still carries a REFERENCES
// tasks(id) or epics(id) constraint from the pre-Work-Item schema.
func hasLegacySubjectForeignKey(db *sql.DB, table string) bool {
	var target string
	err := db.QueryRow(`SELECT "table" FROM pragma_foreign_key_list(?) WHERE "table" IN ('tasks','epics') LIMIT 1`, table).Scan(&target)
	return err == nil
}

func migrateLegacyWorkItems(db *sql.DB) error {
	if _, err := db.Exec(workItemsTableSQL); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT OR IGNORE INTO work_items(id,type,title,description,status,priority,created_at)
		SELECT id,'epic',title,description,status,'medium',created_at FROM epics`); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT OR IGNORE INTO work_items(id,type,parent_id,title,description,status,priority,created_at)
		SELECT id,'task',epic_id,title,description,status,priority,created_at FROM tasks`); err != nil {
		return err
	}
	return tx.Commit()
}

func ownerColumnNotNull(db *sql.DB, table, column string) bool {
	var notNull int
	_ = db.QueryRow(`SELECT "notnull" FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&notNull)
	return notNull != 0
}

func workflowSubject(db *sql.DB, id string, opts map[string]string) (kind, column string, err error) {
	kind = firstNonEmpty(opts["subject-type"], "task")
	if kind == "task" {
		if err = requireTask(db, id); err != nil {
			return "", "", err
		}
		return kind, "task_id", nil
	}
	if kind == "epic" {
		if exists, queryErr := rowExists(db, `SELECT 1 FROM epics WHERE id=?`, id); queryErr != nil {
			return "", "", queryErr
		} else if !exists {
			return "", "", fmt.Errorf("Epic %s not found", id)
		}
		return kind, "epic_id", nil
	}
	return "", "", fmt.Errorf("invalid subject type: %s", kind)
}

func addWorkflowSubjectEvent(db workflowExecer, kind, id, eventType, role, summary string, payload any) error {
	data, _ := json.Marshal(payload)
	if kind == "task" {
		return addEvent(db, id, eventType, role, summary, payload)
	}
	_, err := db.Exec(`INSERT INTO epic_events(id,epic_id,event_type,actor_role,summary,payload_json) VALUES(?,?,?,?,?,?)`, "eev-"+shortID(), id, eventType, role, summary, string(data))
	return err
}

func rebuildSchemaTable(db *sql.DB, table, createSQL string) error {
	old := table + "__workflow_migration"
	if tableExists(db, old) {
		return fmt.Errorf("incomplete workflow migration: %s already exists", old)
	}
	oldColumns, err := tableColumns(db, table)
	if err != nil {
		return err
	}
	var beforeCount int
	if err = db.QueryRow(`SELECT COUNT(*) FROM "` + strings.ReplaceAll(table, `"`, `""`) + `"`).Scan(&beforeCount); err != nil {
		return err
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		return err
	}
	defer conn.Close()
	var foreignKeys, legacyAlterTable int
	if err = conn.QueryRowContext(context.Background(), `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		return err
	}
	if err = conn.QueryRowContext(context.Background(), `PRAGMA legacy_alter_table`).Scan(&legacyAlterTable); err != nil {
		return err
	}
	if _, err = conn.ExecContext(context.Background(), `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	if _, err = conn.ExecContext(context.Background(), `PRAGMA legacy_alter_table=ON`); err != nil {
		return err
	}
	restored := false
	defer func() {
		if !restored {
			_, _ = conn.ExecContext(context.Background(), pragmaEnabled("legacy_alter_table", legacyAlterTable))
			_, _ = conn.ExecContext(context.Background(), pragmaEnabled("foreign_keys", foreignKeys))
		}
	}()
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`ALTER TABLE "` + table + `" RENAME TO "` + old + `"`); err != nil {
		return err
	}
	if _, err = tx.Exec(createSQL); err != nil {
		return err
	}
	newColumns, err := tableColumnsInTx(tx, table)
	if err != nil {
		return err
	}
	oldSet := map[string]bool{}
	for _, column := range oldColumns {
		oldSet[column] = true
	}
	shared := []string{}
	for _, column := range newColumns {
		if oldSet[column] {
			shared = append(shared, `"`+strings.ReplaceAll(column, `"`, `""`)+`"`)
		}
	}
	if len(shared) > 0 {
		columns := strings.Join(shared, ",")
		if _, err = tx.Exec(`INSERT INTO "` + table + `" (` + columns + `) SELECT ` + columns + ` FROM "` + old + `"`); err != nil {
			return err
		}
	}
	var afterCount int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM "` + strings.ReplaceAll(table, `"`, `""`) + `"`).Scan(&afterCount); err != nil {
		return err
	}
	if afterCount != beforeCount {
		return fmt.Errorf("workflow migration %s copied %d/%d rows", table, afterCount, beforeCount)
	}
	if _, err = tx.Exec(`DROP TABLE "` + old + `"`); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if _, err = conn.ExecContext(context.Background(), pragmaEnabled("legacy_alter_table", legacyAlterTable)); err != nil {
		return err
	}
	if _, err = conn.ExecContext(context.Background(), pragmaEnabled("foreign_keys", foreignKeys)); err != nil {
		return err
	}
	restored = true
	return nil
}

func pragmaEnabled(name string, enabled int) string {
	if enabled != 0 {
		return "PRAGMA " + name + "=ON"
	}
	return "PRAGMA " + name + "=OFF"
}

func tableColumnsInTx(tx *sql.Tx, name string) ([]string, error) {
	rows, err := tx.Query(`PRAGMA table_info("` + strings.ReplaceAll(name, `"`, `""`) + `")`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := []string{}
	for rows.Next() {
		var cid, notNull, pk int
		var column, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &column, &kind, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}
