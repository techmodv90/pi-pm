package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
)

func openDB() (*sql.DB, error) {
	cwd, _ := os.Getwd()
	dbPath := findDB(cwd)
	if dbPath == "" {
		return nil, errors.New("No task database found. Run: pic init")
	}
	if err := initDB(dbPath); err != nil {
		return nil, fmt.Errorf("update task database schema: %w", err)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func cmdShow(args []string) error {
	if len(args) < 1 {
		return errors.New("show requires id")
	}
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	id := args[0]
	if ok, err := rowExists(db, `SELECT 1 FROM work_items WHERE id=?`, id); err != nil {
		return err
	} else if ok {
		item, err := workItemByID(db, id)
		if err != nil {
			return err
		}
		children, _ := queryMaps(db, `SELECT `+workItemColumns+` FROM work_items WHERE parent_id=? ORDER BY created_at,id`, id)
		dependencies, _ := queryMaps(db, `SELECT r.id,r.work_item_id,r.related_work_item_id AS depends_on_work_item_id,blocker.title,blocker.type,blocker.status,blocker.review_status FROM work_item_relations r JOIN work_items blocker ON blocker.id=r.related_work_item_id WHERE r.work_item_id=? AND r.relation_type='blocks'`, id)
		relations, _ := queryMaps(db, `SELECT r.*,related.title,related.type,related.status FROM work_item_relations r JOIN work_items related ON related.id=r.related_work_item_id WHERE r.work_item_id=? ORDER BY r.created_at,r.id`, id)
		artifacts, _ := queryMaps(db, `SELECT * FROM work_item_artifacts WHERE work_item_id=? ORDER BY stage,revision DESC`, id)
		checkpoints, _ := queryMaps(db, `SELECT * FROM workflow_checkpoints WHERE work_item_id=? ORDER BY created_at`, id)
		packs, _ := queryMaps(db, `SELECT * FROM work_item_instruction_packs WHERE work_item_id=? ORDER BY version DESC`, id)
		verificationReports, _ := queryMaps(db, `SELECT * FROM work_item_verification_reports WHERE work_item_id=? ORDER BY datetime(created_at) DESC,rowid DESC`, id)
		completionReports, _ := queryMaps(db, `SELECT * FROM work_item_completion_reports WHERE work_item_id=? ORDER BY datetime(created_at) DESC,rowid DESC`, id)
		ownerDecisions, _ := queryMaps(db, `SELECT * FROM work_item_owner_decisions WHERE work_item_id=? ORDER BY datetime(created_at) DESC,rowid DESC`, id)
		requirements, _ := queryMaps(db, `SELECT * FROM requirements WHERE task_id=? OR epic_id=? ORDER BY requirement_key,id`, id, id)
		planningOwnerDecisions, _ := queryMaps(db, `SELECT * FROM owner_decisions WHERE task_id=? OR epic_id=? ORDER BY datetime(created_at),rowid`, id, id)
		ready, _ := rowExists(db, `SELECT 1 FROM work_items wi WHERE wi.id=? AND `+workItemReadySQL, id)
		var executionState any
		if contains([]string{"task", "bug", "chore"}, fmt.Sprint(item["type"])) {
			state, _ := loadWorkItemExecutionState(db, id)
			executionState = state
		}
		writeJSON(os.Stdout, map[string]any{"work_item": item, "ready": ready, "execution_state": executionState, "children": children, "dependencies": dependencies, "relations": relations, "artifacts": artifacts, "checkpoints": checkpoints, "instruction_packs": packs, "completion_reports": completionReports, "verification_reports": verificationReports, "owner_decisions": ownerDecisions, "requirements": requirements, "planning_owner_decisions": planningOwnerDecisions})
		return nil
	}
	return fmt.Errorf("Work Item %s not found", id)
}

func cmdList(args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	results, err := workItemList(db, args)
	if err == nil {
		writeJSON(os.Stdout, results)
	}
	return err
}

type databaseQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func queryOne(db databaseQueryer, query string, args ...any) (map[string]any, error) {
	rows, err := queryMaps(db, query, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("not found")
	}
	return rows[0], nil
}

func queryMaps(db databaseQueryer, query string, args ...any) ([]map[string]any, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, col := range cols {
			row[col] = normalizeDBValue(values[i])
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func normalizeDBValue(value any) any {
	switch v := value.(type) {
	case []byte:
		return string(v)
	case nil:
		return nil
	default:
		return v
	}
}

func rowExists(db workflowQueryer, query string, args ...any) (bool, error) {
	var one int
	err := db.QueryRow(query, args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func normalizeChoice(value string, allowed []string, fallback string) string {
	if contains(allowed, value) {
		return value
	}
	return fallback
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func toInt(value any) int {
	switch v := value.(type) {
	case int64:
		return int(v)
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}
