package main

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/earendil-works/task-system/go-pic/internal/project"
	"github.com/earendil-works/task-system/go-pic/internal/store"
	"github.com/earendil-works/task-system/go-pic/internal/work-item"
	"os"
)

func openDB() (*sql.DB, error) {
	cwd, _ := os.Getwd()
	dbPath := project.FindDB(cwd)
	if dbPath == "" {
		return nil, errors.New("No task database found. Run: pic init")
	}
	if err := project.InitDB(dbPath); err != nil {
		return nil, fmt.Errorf("update task database schema: %w", err)
	}
	db, err := project.OpenSQLite(dbPath)
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
	if ok, err := store.RowExists(db, `SELECT 1 FROM work_items WHERE id=?`, id); err != nil {
		return err
	} else if ok {
		item, err := workitem.ByID(db, id)
		if err != nil {
			return err
		}
		children, _ := store.QueryMaps(db, `SELECT `+workitem.Columns+` FROM work_items WHERE parent_id=? ORDER BY created_at,id`, id)
		dependencies, _ := store.QueryMaps(db, `SELECT r.id,r.work_item_id,r.related_work_item_id AS depends_on_work_item_id,r.rationale,blocker.title,blocker.type,blocker.status,blocker.review_status FROM work_item_relations r JOIN work_items blocker ON blocker.id=r.related_work_item_id WHERE r.work_item_id=? AND r.relation_type='blocks'`, id)
		relations, _ := store.QueryMaps(db, `SELECT r.*,related.title,related.type,related.status FROM work_item_relations r JOIN work_items related ON related.id=r.related_work_item_id WHERE r.work_item_id=? ORDER BY r.created_at,r.id`, id)
		artifacts, _ := store.QueryMaps(db, `SELECT * FROM work_item_artifacts WHERE work_item_id=? ORDER BY stage,revision DESC`, id)
		checkpoints, _ := store.QueryMaps(db, `SELECT * FROM workflow_checkpoints WHERE work_item_id=? ORDER BY created_at`, id)
		packs, _ := store.QueryMaps(db, `SELECT * FROM work_item_instruction_packs WHERE work_item_id=? ORDER BY version DESC`, id)
		materializations, _ := store.QueryMaps(db, `SELECT root_work_item_id,checkpoint_id,node_key,work_item_id FROM work_item_materializations WHERE work_item_id=?`, id)
		profiles, _ := store.QueryMaps(db, `SELECT * FROM work_item_profiles WHERE work_item_id=? ORDER BY profile_name,profile_version`, id)
		verificationReports, _ := store.QueryMaps(db, `SELECT * FROM work_item_verification_reports WHERE work_item_id=? ORDER BY datetime(created_at) DESC,rowid DESC`, id)
		completionReports, _ := store.QueryMaps(db, `SELECT * FROM work_item_completion_reports WHERE work_item_id=? ORDER BY datetime(created_at) DESC,rowid DESC`, id)
		ownerDecisions, _ := store.QueryMaps(db, `SELECT * FROM work_item_owner_decisions WHERE work_item_id=? ORDER BY datetime(created_at) DESC,rowid DESC`, id)
		requirements, _ := store.QueryMaps(db, `SELECT * FROM requirements WHERE task_id=? OR epic_id=? ORDER BY requirement_key,id`, id, id)
		planningOwnerDecisions, _ := store.QueryMaps(db, `SELECT * FROM owner_decisions WHERE task_id=? OR epic_id=? ORDER BY datetime(created_at),rowid`, id, id)
		escalations, _ := store.QueryMaps(db, `SELECT * FROM work_item_escalations WHERE work_item_id=? ORDER BY datetime(created_at),rowid`, id)
		ready, _ := store.RowExists(db, `SELECT 1 FROM work_items wi WHERE wi.id=? AND `+workitem.ReadySQL, id)
		var executionState any
		if store.Contains([]string{"task", "bug", "chore"}, fmt.Sprint(item["type"])) {
			state, _ := workitem.LoadExecutionState(db, id)
			executionState = state
		}
		store.WriteJSON(os.Stdout, map[string]any{"work_item": item, "ready": ready, "execution_state": executionState, "children": children, "dependencies": dependencies, "relations": relations, "artifacts": artifacts, "checkpoints": checkpoints, "instruction_packs": packs, "materializations": materializations, "profiles": profiles, "completion_reports": completionReports, "verification_reports": verificationReports, "owner_decisions": ownerDecisions, "requirements": requirements, "planning_owner_decisions": planningOwnerDecisions, "escalations": escalations})
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
	results, err := workitem.List(db, args)
	if err == nil {
		store.WriteJSON(os.Stdout, results)
	}
	return err
}
