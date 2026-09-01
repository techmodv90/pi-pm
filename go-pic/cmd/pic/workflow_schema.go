package main

import (
	"database/sql"

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

// Artifact stage taxonomy constraint: rri_t_scenarios is an additive retained
// scenario-list stage in both SQLite CHECK constraints; the original planning
// stage names and their gating behavior stay unchanged.
const workItemArtifactsTableSQL = `CREATE TABLE IF NOT EXISTS work_item_artifacts (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, stage TEXT NOT NULL CHECK(stage IN ('scan','rri','rri_t_scenarios','vision','blueprint','contracts','task_graph')), revision INTEGER NOT NULL CHECK(revision>0), content TEXT NOT NULL, content_hash TEXT NOT NULL, created_at TEXT DEFAULT (datetime('now')), UNIQUE(work_item_id,stage,revision))`

const workflowCheckpointsTableSQL = `CREATE TABLE IF NOT EXISTS workflow_checkpoints (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, stage TEXT NOT NULL CHECK(stage IN ('scan','rri','rri_t_scenarios','vision','blueprint','contracts','task_graph')), artifact_id TEXT NOT NULL, artifact_revision INTEGER NOT NULL CHECK(artifact_revision>0), content_hash TEXT NOT NULL, decision_type TEXT NOT NULL, created_at TEXT DEFAULT (datetime('now')), UNIQUE(work_item_id,stage,artifact_revision))`

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

func migrateEpicWorkflowSchema(db schemaDB) error {
	if !tableExists(db, "tasks") && !tableExists(db, "epics") {
		return nil
	}
	// Partial legacy states are supported: a database may carry either table
	// alone, so every tasks-specific probe is guarded by table existence.
	if tableExists(db, "tasks") {
		var epicNotNull int
		_ = db.QueryRow(`SELECT "notnull" FROM pragma_table_info('tasks') WHERE name='epic_id'`).Scan(&epicNotNull)
		if epicNotNull != 0 || !hasColumn(db, "tasks", "origin") || !hasColumn(db, "tasks", "revision") || hasColumn(db, "tasks", "refined") {
			if err := rebuildSchemaTable(db, "tasks", tasksTableSQL); err != nil {
				return err
			}
		}
		// The rebuilt tasks table declares epic_id REFERENCES epics(id), so a
		// dangling reference (partial state, or an epic that never existed)
		// would fail pragma_foreign_key_check. Normalize it to the empty
		// sentinel; the canonical Work Item import records the same row with a
		// null parent, and the legacy table stays inert history.
		if tableExists(db, "epics") {
			if _, err := db.Exec(`UPDATE tasks SET epic_id=NULL WHERE epic_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM epics WHERE epics.id=tasks.epic_id)`); err != nil {
				return err
			}
		} else if _, err := db.Exec(`UPDATE tasks SET epic_id=NULL WHERE epic_id IS NOT NULL`); err != nil {
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

// migrateArtifactStageSchema extends the additive rri_t_scenarios CHECK
// constraint on databases created before the stage existed. initDB runs on
// every command but CREATE TABLE IF NOT EXISTS never touches existing tables,
// so an existing project would keep rejecting rri_t_scenarios rows. The
// migration rebuilds only the two stage-constrained tables, maps the original
// artifact and checkpoint rows onto the widened schema, and leaves every other
// table alone. The post-migration initDB statement pass recreates the
// immutable-history triggers and lookup indexes on the rebuilt tables (they are
// dropped here first because the old objects would otherwise travel with the
// renamed legacy table and be dropped with it, or shadow the IF NOT EXISTS
// recreation).
func migrateArtifactStageSchema(db schemaDB) error {
	for table, createSQL := range map[string]string{
		"work_item_artifacts":  workItemArtifactsTableSQL,
		"workflow_checkpoints": workflowCheckpointsTableSQL,
	} {
		if !tableExists(db, table) {
			continue
		}
		var tableSQL string
		if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&tableSQL); err != nil {
			return err
		}
		if strings.Contains(tableSQL, "rri_t_scenarios") {
			continue
		}
		// Drop the artifact immutable triggers so the statement pass recreates
		// them on the rebuilt tables instead of leaving them on the renamed one.
		for _, trigger := range []string{"trg_work_item_artifact_immutable", "trg_work_item_artifact_delete_immutable"} {
			if _, err := db.Exec(`DROP TRIGGER IF EXISTS ` + trigger); err != nil {
				return err
			}
		}
		if err := rebuildSchemaTable(db, table, createSQL); err != nil {
			return fmt.Errorf("migrate %s stage CHECK: %w", table, err)
		}
	}
	return nil
}

// hasLegacySubjectForeignKey reports whether table still carries a REFERENCES
// tasks(id) or epics(id) constraint from the pre-Work-Item schema.
func hasLegacySubjectForeignKey(db schemaDB, table string) bool {
	var target string
	err := db.QueryRow(`SELECT "table" FROM pragma_foreign_key_list(?) WHERE "table" IN ('tasks','epics') LIMIT 1`, table).Scan(&target)
	return err == nil
}

func migrateLegacyWorkItems(db schemaDB) error {
	if !tableExists(db, "tasks") && !tableExists(db, "epics") {
		return nil
	}
	if _, err := db.Exec(workItemsTableSQL); err != nil {
		return err
	}
	if tableExists(db, "epics") {
		if _, err := db.Exec(`INSERT OR IGNORE INTO work_items(id,type,title,description,status,priority,created_at)
			SELECT id,'epic',title,description,status,'medium',created_at FROM epics`); err != nil {
			return err
		}
	}
	if tableExists(db, "tasks") {
		// A task whose epic was not imported (partial state or dangling
		// reference) migrates with a null parent instead of failing the FK.
		if _, err := db.Exec(`INSERT OR IGNORE INTO work_items(id,type,parent_id,title,description,status,priority,created_at)
			SELECT id,'task',CASE WHEN EXISTS(SELECT 1 FROM work_items parent WHERE parent.id=tasks.epic_id) THEN tasks.epic_id ELSE NULL END,title,description,status,priority,created_at FROM tasks`); err != nil {
			return err
		}
	}
	return nil
}

func ownerColumnNotNull(db schemaDB, table, column string) bool {
	var notNull int
	_ = db.QueryRow(`SELECT "notnull" FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&notNull)
	return notNull != 0
}

// rebuildSchemaTable rebuilds one table in place inside the caller's
// transaction: it renames the legacy table, creates the new shape, copies the
// shared columns, and verifies the row count before dropping the old table.
// The migration runner holds foreign_keys=OFF and legacy_alter_table=ON on the
// connection for the whole step, so no per-call pragma juggling is needed.
// columnExprs (variadic, at most one map) optionally overrides the copied
// expression for specific columns, e.g. to translate retired legacy values
// that would violate the rebuilt table's CHECK constraints.
func rebuildSchemaTable(db schemaDB, table, createSQL string, columnExprs ...map[string]string) error {
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
	if _, err = db.Exec(`ALTER TABLE "` + table + `" RENAME TO "` + old + `"`); err != nil {
		return err
	}
	if _, err = db.Exec(createSQL); err != nil {
		return err
	}
	newColumns, err := tableColumns(db, table)
	if err != nil {
		return err
	}
	oldSet := map[string]bool{}
	for _, column := range oldColumns {
		oldSet[column] = true
	}
	var exprs map[string]string
	if len(columnExprs) > 0 {
		exprs = columnExprs[0]
	}
	var names, exprsOut []string
	for _, column := range newColumns {
		if !oldSet[column] {
			continue
		}
		quoted := `"` + strings.ReplaceAll(column, `"`, `""`) + `"`
		names = append(names, quoted)
		if expr, ok := exprs[column]; ok {
			exprsOut = append(exprsOut, expr)
		} else {
			exprsOut = append(exprsOut, quoted)
		}
	}
	if len(names) > 0 {
		if _, err = db.Exec(`INSERT INTO "` + table + `" (` + strings.Join(names, ",") + `) SELECT ` + strings.Join(exprsOut, ",") + ` FROM "` + old + `"`); err != nil {
			return err
		}
	}
	var afterCount int
	if err = db.QueryRow(`SELECT COUNT(*) FROM "` + strings.ReplaceAll(table, `"`, `""`) + `"`).Scan(&afterCount); err != nil {
		return err
	}
	if afterCount != beforeCount {
		return fmt.Errorf("workflow migration %s copied %d/%d rows", table, afterCount, beforeCount)
	}
	if _, err = db.Exec(`DROP TABLE "` + old + `"`); err != nil {
		return err
	}
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
