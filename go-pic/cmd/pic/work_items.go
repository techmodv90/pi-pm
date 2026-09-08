package main

import (
	"errors"
	"fmt"
	"github.com/earendil-works/task-system/go-pic/internal/project"
	"github.com/earendil-works/task-system/go-pic/internal/store"
	"os"

	"github.com/earendil-works/task-system/go-pic/internal/work-item"
)

func cmdWorkItem(args []string) error {
	if len(args) == 0 {
		return errors.New("work-item subcommand required")
	}
	if agent := os.Getenv("PI_TASK_AGENT_NAME"); agent != "" && !store.Contains([]string{"list", "show", "artifact-save", "workflow-status"}, args[0]) {
		return fmt.Errorf("%s cannot mutate Work Item lifecycle through pic", agent)
	}
	db, err := project.OpenDB()
	if err != nil {
		return err
	}
	defer db.Close()
	switch args[0] {
	case "create":
		return workitem.Create(db, args[1:])
	case "list":
		rows, err := workitem.List(db, args[1:])
		if err == nil {
			store.WriteJSON(os.Stdout, rows)
		}
		return err
	case "label":
		return workitem.Label(db, args[1:])
	case "show":
		if len(args) != 2 {
			return errors.New("usage: pic work-item show <id>")
		}
		item, err := workitem.ByID(db, args[1])
		if err == nil {
			err = attachWorkItemGraph(db, item)
		}
		if err == nil {
			store.WriteJSON(os.Stdout, item)
		}
		return err
	case "update":
		return workitem.Update(db, args[1:])
	case "status":
		if len(args) != 3 || !store.Contains([]string{"open", "in_progress", "done", "cancelled"}, args[2]) {
			return errors.New("usage: pic work-item status <id> <open|in_progress|done|cancelled>")
		}
		item, err := workitem.SetStatus(db, args[1], args[2])
		if err == nil {
			store.WriteJSON(os.Stdout, item)
		}
		return err
	case "depend":
		return workitem.AddRelation(db, args[1:], "blocks")
	case "gate":
		return workitem.AddRelation(db, args[1:], "gates")
	case "relate":
		if len(args) != 4 {
			return errors.New("usage: pic work-item relate <work-item-id> <blocks|gates|related> <related-work-item-id>")
		}
		return workitem.AddRelation(db, []string{args[1], args[3]}, args[2])
	case "ready":
		rows, err := store.QueryMaps(db, `SELECT `+workitem.Columns+` FROM work_items wi WHERE `+workitem.ReadySQL+` ORDER BY created_at,id`)
		if err == nil {
			err = workitem.AttachLabels(db, rows)
		}
		if err == nil {
			store.WriteJSON(os.Stdout, rows)
		}
		return err
	case "claim":
		return workitem.Claim(db, args[1:])
	case "artifact-save":
		return workitem.SaveArtifact(db, args[1:])
	case "execution-reset":
		return workitem.ExecutionReset(db, args[1:])
	case "workflow-status":
		return workitem.WorkflowStatus(db, args[1:])
	case "authorize":
		return workitem.Authorize(db, args[1:])
	case "review":
		return workitem.Review(db, args[1:])
	case "completion-save":
		return workitem.CompletionSave(db, args[1:])
	case "verification-save":
		return workitem.VerificationSave(db, args[1:])
	case "accept":
		return workitem.Accept(db, args[1:])
	case "aggregate-verify":
		return workitem.AggregateVerify(db, args[1:])
	case "aggregate-accept":
		return workitem.AggregateAccept(db, args[1:])
	case "aggregate-merge-result":
		return workitem.AggregateMergeResult(db, args[1:])
	case "aggregate-close":
		return workitem.AggregateClose(db, args[1:])
	default:
		return fmt.Errorf("unknown work-item subcommand: %s", args[0])
	}
}
