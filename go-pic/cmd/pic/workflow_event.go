package main

import (
	"database/sql"
	"errors"

	"github.com/earendil-works/task-system/go-pic/internal/store"
	"github.com/earendil-works/task-system/go-pic/internal/work-item"
)

func workflowEventAdd(db *sql.DB, args []string) error {
	if len(args) < 2 {
		return errors.New("event-add requires Work Item id and event type")
	}
	opts, err := store.ParseOptions(args[2:])
	if err != nil {
		return err
	}
	workItemID, eventType := args[0], args[1]
	if _, err := workitem.ByID(db, workItemID); err != nil {
		return err
	}
	if eventType == "verify_completed" {
		return errors.New("verify_completed events are managed by verification-save")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if eventType == "implementation_started" || eventType == "review_started" {
		if _, err = tx.Exec(`UPDATE work_items SET status='in_progress',review_status='pending' WHERE id=?`, workItemID); err != nil {
			return err
		}
	} else if eventType == "review_failed" {
		if _, err = tx.Exec(`UPDATE work_items SET status='in_progress',review_status='failed' WHERE id=?`, workItemID); err != nil {
			return err
		}
	}
	id := "wie-" + store.ShortID()
	if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,actor_model,summary,payload_json) VALUES(?,?,?,?,?,?,?)`, id, workItemID, eventType, opts["actor-role"], opts["actor-model"], opts["summary"], store.NormalizeJSONText(opts["payload-json"])); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return store.OutputOne(db, `SELECT * FROM work_item_events WHERE id=?`, id)
}
