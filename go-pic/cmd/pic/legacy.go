package main

import "database/sql"

func hasColumn(db workflowStore, table, column string) bool {
	ok, _ := rowExists(db, `SELECT 1 FROM pragma_table_info(?) WHERE name = ?`, table, column)
	return ok
}

func legacyProjectID(db workflowStore) string {
	row, err := queryOne(db, `SELECT id FROM projects ORDER BY created_at LIMIT 1`)
	if err == nil {
		if id, ok := row["id"].(string); ok {
			return id
		}
	}
	project, _ := currentProject()
	return project.ID
}

func insertEpicRow(db workflowStore, id, title, description string) error {
	if hasColumn(db, "epics", "project_id") {
		_, err := db.Exec(`INSERT INTO epics (id, project_id, title, description) VALUES (?, ?, ?, ?)`, id, legacyProjectID(db), title, description)
		return err
	}
	_, err := db.Exec(`INSERT INTO epics (id, title, description) VALUES (?, ?, ?)`, id, title, description)
	return err
}

func insertScanReportRow(db *sql.DB, id, taskID, status, summary, techStack, architecture, commands, patterns, risks, raw string) error {
	if hasColumn(db, "scan_reports", "project_id") {
		_, err := db.Exec(`INSERT INTO scan_reports (id, task_id, project_id, status, summary, tech_stack_json, architecture_json, commands_json, patterns_json, risks_json, raw_report) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, taskID, legacyProjectID(db), status, summary, techStack, architecture, commands, patterns, risks, raw)
		return err
	}
	_, err := db.Exec(`INSERT INTO scan_reports (id, task_id, status, summary, tech_stack_json, architecture_json, commands_json, patterns_json, risks_json, raw_report) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, taskID, status, summary, techStack, architecture, commands, patterns, risks, raw)
	return err
}
