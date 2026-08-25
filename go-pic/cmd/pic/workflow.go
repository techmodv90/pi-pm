package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type workflowExecer interface {
	Exec(string, ...any) (sql.Result, error)
}
type workflowQueryer interface {
	QueryRow(string, ...any) *sql.Row
}
type workflowStore interface {
	databaseQueryer
	workflowExecer
}

func cmdWorkflow(args []string) error {
	if len(args) == 0 {
		return errors.New("workflow subcommand required")
	}
	if agent := os.Getenv("PI_TASK_AGENT_NAME"); agent != "" && !contains([]string{"instruction-pack-render", "instruction-packs", "verifications", "events", "pipeline-runs", "pipeline-group", "profile-list", "profile-promotion-evaluate"}, args[0]) {
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
		return workItemCompletionSave(db, rest)
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
	case "pipeline-runs":
		return workflowPipelineRuns(db, rest)
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
	rows, err := queryMaps(db, query, args[0])
	if err != nil {
		return err
	}
	writeJSON(os.Stdout, rows)
	return nil
}

func normalizeJSONText(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	var parsed any
	if json.Unmarshal([]byte(value), &parsed) != nil {
		return value
	}
	data, _ := json.Marshal(parsed)
	return string(data)
}

func parseOptions(args []string) (map[string]string, error) {
	opts := map[string]string{}
	for i := 0; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "--") {
			return nil, fmt.Errorf("unexpected argument: %s", args[i])
		}
		key := strings.TrimPrefix(args[i], "--")
		if key == "" || i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			return nil, fmt.Errorf("option --%s requires a value", key)
		}
		opts[key] = args[i+1]
		i++
	}
	return opts, nil
}

func outputOne(db databaseQueryer, query string, args ...any) error {
	row, err := queryOne(db, query, args...)
	if err != nil {
		return err
	}
	writeJSON(os.Stdout, row)
	return nil
}

func persistedText(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func addEvent(db workflowExecer, workItemID, eventType, role, summary string, payload any) error {
	return addEventWithModel(db, workItemID, eventType, role, "", summary, payload)
}

func addEventWithModel(db workflowExecer, workItemID, eventType, role, model, summary string, payload any) error {
	data, _ := json.Marshal(payload)
	_, err := db.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,actor_model,summary,payload_json) VALUES(?,?,?,?,?,?,?)`, "wie-"+shortID(), workItemID, eventType, role, model, summary, string(data))
	return err
}

func requireTask(db databaseQueryer, id string) error {
	if _, err := workItemByID(db.(*sql.DB), id); err != nil {
		return fmt.Errorf("Work Item %s not found", id)
	}
	return nil
}

func verificationText(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func ownerDecision(db workflowExecer, workItemID, relatedType, relatedID, decisionType, decision, notes string) error {
	return addEvent(db, workItemID, "owner_decision", "owner", notes, map[string]any{"decision_type": decisionType, "decision": decision, "related_type": relatedType, "related_id": relatedID})
}

func firstAny(values ...any) any {
	for _, value := range values {
		if value != nil && fmt.Sprint(value) != "" {
			return value
		}
	}
	return nil
}
