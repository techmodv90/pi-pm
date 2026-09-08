package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/earendil-works/task-system/go-pic/internal/project"
	"github.com/earendil-works/task-system/go-pic/internal/store"
	"github.com/earendil-works/task-system/go-pic/internal/work-item"
	"os"

	"github.com/earendil-works/task-system/go-pic/internal/tip"
)

// workflowInstructionPackSave parses the CLI request and delegates persistence
// to the tip package; the CLI remains the only lifecycle mutation surface.
func workflowInstructionPackSave(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("instruction-pack-save requires Work Item id")
	}
	workItemID := args[0]
	if _, err := workitem.ByID(db, workItemID); err != nil {
		return err
	}
	opts, err := store.ParseOptions(args[1:])
	if err != nil {
		return err
	}
	var requirementIDs []string
	if err := json.Unmarshal([]byte(project.FirstNonEmpty(opts["requirement-ids-json"], "[]")), &requirementIDs); err != nil {
		return fmt.Errorf("requirement-ids-json: %w", err)
	}
	pack, err := tip.SaveInstructionPack(db, workItemID, tip.SaveInput{
		ContentJSON:        opts["content-json"],
		RequirementIDs:     requirementIDs,
		Activate:           opts["activate"] == "1" || opts["activate"] == "true",
		ValidateAcceptance: validateGherkinSteps,
	})
	if err != nil {
		return err
	}
	store.WriteJSON(os.Stdout, pack)
	return nil
}

func workflowInstructionPackRender(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("instruction-pack-render requires Work Item id")
	}
	pack, err := store.QueryOne(db, `SELECT p.*,w.title AS work_item_title,w.type AS work_item_type,w.priority FROM work_item_instruction_packs p JOIN work_items w ON w.id=p.work_item_id WHERE p.work_item_id=? AND p.status='active'`, args[0])
	if err != nil {
		return fmt.Errorf("active instruction pack for Work Item %s not found", args[0])
	}
	if err = tip.ExpandCanonicalInstructionPack(pack); err != nil {
		return err
	}
	_, err = fmt.Fprint(os.Stdout, tip.RenderInstructionPack(pack))
	return err
}

func workflowInstructionPacks(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("instruction-packs requires Work Item id")
	}
	return workflowList(db, args, `SELECT * FROM work_item_instruction_packs WHERE work_item_id=? ORDER BY version DESC`)
}
