package main

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/earendil-works/task-system/go-pic/internal/store"
	"github.com/earendil-works/task-system/go-pic/internal/work-item"
	"os"
)

func cmdWorkflow(args []string) error {
	if len(args) == 0 {
		return errors.New("workflow subcommand required")
	}
	if agent := os.Getenv("PI_TASK_AGENT_NAME"); agent != "" && !store.Contains([]string{"instruction-pack-render", "instruction-packs", "verifications", "events", "pipeline-runs", "pipeline-show", "pipeline-group", "profile-list", "profile-promotion-evaluate"}, args[0]) {
		return fmt.Errorf("%s cannot mutate workflow lifecycle through pic", agent)
	}
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	rest := args[1:]
	switch args[0] {
	case "instruction-pack-save":
		return workflowInstructionPackSave(db, rest)
	case "instruction-pack-render":
		return workflowInstructionPackRender(db, rest)
	case "instruction-packs":
		return workflowInstructionPacks(db, rest)
	case "completion-save":
		return workitem.CompletionSave(db, rest)
	case "verifications":
		return workflowVerificationList(db, rest)
	case "event-add":
		return workflowEventAdd(db, rest)
	case "events":
		return workflowList(db, rest, `SELECT * FROM work_item_events WHERE work_item_id=? ORDER BY created_at DESC`)
	case "escalation-save":
		return workflowEscalationSave(db, rest)
	case "escalation-resolve":
		return workflowEscalationResolve(db, rest)
	case "pipeline-claim":
		return workflowPipelineClaim(db, rest)
	case "pipeline-circuit-reset":
		return workflowPipelineCircuitReset(db, rest)
	case "pipeline-bind":
		return workflowPipelineBind(db, rest)
	case "pipeline-renew":
		return workflowPipelineRenew(db, rest)
	case "pipeline-model":
		return workflowPipelineModel(db, rest)
	case "pipeline-complete":
		return workflowPipelineComplete(db, rest)
	case "review-fix-block":
		return workflowReviewFixBlock(db, rest)
	case "review-decision":
		return workflowReviewDecision(db, rest)
	case "pipeline-runs":
		return workflowPipelineRuns(db, rest)
	case "pipeline-show":
		return workflowPipelineShow(db, rest)
	case "pipeline-active":
		return workflowPipelineActive(db, rest)
	case "pipeline-group":
		return workflowPipelineGroup(db, rest)
	case "pipeline-checkpoint":
		return workflowPipelineCheckpoint(db, rest)
	case "pipeline-pending":
		return workflowPipelinePending(db, rest)
	case "profile-list":
		return workflowProfileList(db, rest)
	case "profile-promotion-evaluate":
		return workflowProfilePromotionEvaluate(db, rest)
	case "import-apm":
		return cmdWorkflowImportApm(db, rest)
	default:
		return fmt.Errorf("unknown workflow subcommand: %s", args[0])
	}
}

func workflowVerificationList(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("verifications requires Work Item id")
	}
	return workflowList(db, args, `SELECT * FROM work_item_verification_reports WHERE work_item_id=? ORDER BY created_at DESC`)
}

func workflowList(db *sql.DB, args []string, query string) error {
	if len(args) < 1 {
		return errors.New("Work Item id required")
	}
	rows, err := store.QueryMaps(db, query, args[0])
	if err != nil {
		return err
	}
	store.WriteJSON(os.Stdout, rows)
	return nil
}

func ownerDecision(db store.Execer, workItemID, relatedType, relatedID, decisionType, decision, notes string) error {
	return store.AddEvent(db, workItemID, "owner_decision", "owner", notes, map[string]any{"decision_type": decisionType, "decision": decision, "related_type": relatedType, "related_id": relatedID})
}

func firstAny(values ...any) any {
	for _, value := range values {
		if value != nil && fmt.Sprint(value) != "" {
			return value
		}
	}
	return nil
}
