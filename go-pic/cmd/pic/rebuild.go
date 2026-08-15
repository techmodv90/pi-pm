package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var rebuildTables = []string{"epics", "epic_events", "tasks", "task_events", "scan_reports", "rri_sessions", "requirements", "designs", "task_materializations", "task_instruction_packs", "instruction_pack_requirement_links", "completion_reports", "verification_reports", "verification_items", "escalations", "owner_decisions", "contract_operations", "contract_operation_targets", "effective_contract_snapshots", "effective_contract_entries", "contract_task_impacts", "task_dependencies", "task_phase_metadata", "pipeline_runs"}

func tableExists(db *sql.DB, name string) bool {
	var found string
	return db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found) == nil
}

func tableColumns(db *sql.DB, name string) ([]string, error) {
	rows, err := db.Query(`PRAGMA table_info("` + strings.ReplaceAll(name, `"`, `""`) + `")`)
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

func cmdRebuild() error {
	cwd, _ := os.Getwd()
	dbPath := findDB(cwd)
	if dbPath == "" {
		return fmt.Errorf("No task database found. Run: pic init")
	}
	root := filepath.Dir(filepath.Dir(dbPath))
	backup := dbPath + ".bak-" + strings.NewReplacer(":", "-", ".", "-").Replace(time.Now().UTC().Format(time.RFC3339Nano))
	db, err := openSQLite(dbPath)
	if err != nil {
		return err
	}
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		_ = db.Close()
		return fmt.Errorf("checkpoint database before rebuild: %w", err)
	}
	projectName := filepath.Base(root)
	if tableExists(db, "projects") {
		_ = db.QueryRow(`SELECT name FROM projects ORDER BY created_at LIMIT 1`).Scan(&projectName)
	}
	counts := map[string]int{}
	for _, table := range rebuildTables {
		if tableExists(db, table) {
			var count int
			_ = db.QueryRow(`SELECT COUNT(*) FROM "` + table + `"`).Scan(&count)
			counts[table] = count
		}
	}
	_ = db.Close()
	data, err := os.ReadFile(dbPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		return err
	}
	if _, err := upsertProject(projectName, root, dbPath); err != nil {
		return err
	}
	db, err = openSQLite(dbPath)
	if err != nil {
		return err
	}
	_, _ = db.Exec(`PRAGMA foreign_keys=OFF`)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, table := range rebuildTables {
		old := table + "__old_rebuild"
		if tableExists(db, old) {
			if _, err = tx.Exec(`DROP TABLE "` + old + `"`); err != nil {
				tx.Rollback()
				return err
			}
		}
		if tableExists(db, table) {
			if _, err = tx.Exec(`ALTER TABLE "` + table + `" RENAME TO "` + old + `"`); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	if tableExists(db, "projects") {
		if _, err = tx.Exec(`DROP TABLE projects`); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	_ = db.Close()
	if err = initDB(dbPath); err != nil {
		return err
	}
	db, err = openSQLite(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	_, _ = db.Exec(`PRAGMA foreign_keys=OFF`)
	tx, err = db.Begin()
	if err != nil {
		return err
	}
	copied := map[string]int{}
	for _, table := range rebuildTables {
		old := table + "__old_rebuild"
		if !tableExists(db, old) {
			continue
		}
		newCols, e := tableColumns(db, table)
		if e != nil {
			tx.Rollback()
			return e
		}
		oldCols, e := tableColumns(db, old)
		if e != nil {
			tx.Rollback()
			return e
		}
		oldSet := map[string]bool{}
		for _, c := range oldCols {
			oldSet[c] = true
		}
		shared := []string{}
		for _, c := range newCols {
			if oldSet[c] {
				shared = append(shared, `"`+strings.ReplaceAll(c, `"`, `""`)+`"`)
			}
		}
		if len(shared) > 0 {
			result, e := tx.Exec(`INSERT INTO "` + table + `" (` + strings.Join(shared, ",") + `) SELECT ` + strings.Join(shared, ",") + ` FROM "` + old + `"`)
			if e != nil {
				tx.Rollback()
				return e
			}
			n, _ := result.RowsAffected()
			copied[table] = int(n)
		}
	}
	for i := len(rebuildTables) - 1; i >= 0; i-- {
		old := rebuildTables[i] + "__old_rebuild"
		if tableExists(db, old) {
			if _, err = tx.Exec(`DROP TABLE "` + old + `"`); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	_, _ = db.Exec(`PRAGMA foreign_keys=ON`)
	epicCols, _ := tableColumns(db, "epics")
	scanCols, _ := tableColumns(db, "scan_reports")
	warnings := []string{}
	for table, count := range counts {
		if copied[table] != count {
			warnings = append(warnings, fmt.Sprintf("%s: copied %d/%d rows", table, copied[table], count))
		}
	}
	writeJSON(os.Stdout, map[string]any{"rebuilt": true, "db_path": dbPath, "backup_path": backup, "backup_note": "Backup retained at " + backup + ". Delete it manually when no longer needed.", "removed_projects_table": !tableExists(db, "projects"), "removed_epic_project_id": !contains(epicCols, "project_id"), "removed_scan_report_project_id": !contains(scanCols, "project_id"), "copied": copied, "warnings": warnings})
	return nil
}
