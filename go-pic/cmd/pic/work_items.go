package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/earendil-works/task-system/go-pic/internal/tip"
)

const workItemColumns = `id,type,parent_id,title,description,status,priority,deferred,claimed_at,claimed_by,review_status,review_notes,planning_depth,created_at,decomposition_mode,decomposition_reason,paired_contract_node,source_graph_artifact_id,source_graph_revision,source_graph_content_hash`

var workItemLabelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]{0,254}$`)

func cmdWorkItem(args []string) error {
	if len(args) == 0 {
		return errors.New("work-item subcommand required")
	}
	if agent := os.Getenv("PI_TASK_AGENT_NAME"); agent != "" && !contains([]string{"list", "show", "artifact-save", "workflow-status", "graph-validate"}, args[0]) {
		return fmt.Errorf("%s cannot mutate Work Item lifecycle through pic", agent)
	}
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	switch args[0] {
	case "create":
		return workItemCreate(db, args[1:])
	case "list":
		rows, err := workItemList(db, args[1:])
		if err == nil {
			writeJSON(os.Stdout, rows)
		}
		return err
	case "label":
		return workItemLabel(db, args[1:])
	case "show":
		if len(args) != 2 {
			return errors.New("usage: pic work-item show <id>")
		}
		item, err := workItemByID(db, args[1])
		if err == nil {
			writeJSON(os.Stdout, item)
		}
		return err
	case "update":
		return workItemUpdate(db, args[1:])
	case "status":
		if len(args) != 3 || !contains([]string{"open", "in_progress", "done", "cancelled"}, args[2]) {
			return errors.New("usage: pic work-item status <id> <open|in_progress|done|cancelled>")
		}
		item, err := workItemSetStatus(db, args[1], args[2])
		if err == nil {
			writeJSON(os.Stdout, item)
		}
		return err
	case "depend":
		return addWorkItemRelation(db, args[1:], "blocks")
	case "gate":
		return addWorkItemRelation(db, args[1:], "gates")
	case "relate":
		if len(args) != 4 {
			return errors.New("usage: pic work-item relate <work-item-id> <blocks|gates|related> <related-work-item-id>")
		}
		return addWorkItemRelation(db, []string{args[1], args[3]}, args[2])
	case "ready":
		rows, err := queryMaps(db, `SELECT `+workItemColumns+` FROM work_items wi WHERE `+workItemReadySQL+` ORDER BY created_at,id`)
		if err == nil {
			err = attachWorkItemLabels(db, rows)
		}
		if err == nil {
			writeJSON(os.Stdout, rows)
		}
		return err
	case "claim":
		return workItemClaim(db, args[1:])
	case "artifact-save":
		return workItemArtifactSave(db, args[1:])
	case "rri-finalize":
		return workItemRriFinalize(db, args[1:])
	case "artifact-approve":
		return workItemArtifactApprove(db, args[1:])
	case "checkpoint-decide":
		return workItemCheckpointDecide(db, args[1:])
	case "planning-reset":
		return workItemPlanningReset(db, args[1:])
	case "planning-amend":
		return workItemPlanningAmend(db, args[1:])
	case "execution-reset":
		return workItemExecutionReset(db, args[1:])
	case "approve-deviations":
		return workItemApproveDeviations(db, args[1:])
	case "scan-reject":
		return workItemScanReject(db, args[1:])
	case "scan-rejection":
		return workItemScanRejection(db, args[1:])
	case "workflow-status":
		return workItemWorkflowStatus(db, args[1:])
	case "graph-validate":
		return workItemGraphValidate(db, args[1:])
	case "materialize":
		return workItemMaterialize(db, args[1:])
	case "authorize":
		return workItemAuthorize(db, args[1:])
	case "review":
		return workItemReview(db, args[1:])
	case "completion-save":
		return workItemCompletionSave(db, args[1:])
	case "verification-save":
		return workItemVerificationSave(db, args[1:])
	case "accept":
		return workItemAccept(db, args[1:])
	case "aggregate-verify":
		return workItemAggregateVerify(db, args[1:])
	case "aggregate-accept":
		return workItemAggregateAccept(db, args[1:])
	case "aggregate-merge-result":
		return workItemAggregateMergeResult(db, args[1:])
	case "aggregate-close":
		return workItemAggregateClose(db, args[1:])
	default:
		return fmt.Errorf("unknown work-item subcommand: %s", args[0])
	}
}

// Contract deviation approval: preserve planning and renew only execution authority.
func workItemApproveDeviations(db *sql.DB, args []string) error {
	if len(args) < 3 || args[1] != "owner" {
		return errors.New("usage: pic work-item approve-deviations <id> owner <requirement-id>...")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	item, err := workItemByIDTx(tx, args[0])
	if err != nil {
		return err
	}
	if item["status"] == "done" || item["status"] == "cancelled" {
		return errors.New("deviation approval requires a non-terminal Work Item")
	}
	contractTaskID := args[0]
	if err = tx.QueryRow(`SELECT root_work_item_id FROM work_item_materializations WHERE work_item_id=? ORDER BY rowid DESC LIMIT 1`, args[0]).Scan(&contractTaskID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var contractType string
	if err = tx.QueryRow(`SELECT type FROM work_items WHERE id=?`, contractTaskID).Scan(&contractType); err != nil {
		return fmt.Errorf("materialization root %s not found: %w", contractTaskID, err)
	}
	subjectColumn := "task_id"
	if contractType == "epic" || contractType == "feature" {
		subjectColumn = "epic_id"
	}
	var packContent string
	if err = tx.QueryRow(`SELECT content_json FROM work_item_instruction_packs WHERE work_item_id=? AND status IN ('active','stale','superseded') ORDER BY version DESC LIMIT 1`, args[0]).Scan(&packContent); err != nil {
		return fmt.Errorf("instruction pack lineage for Work Item %s not found", args[0])
	}
	var pack struct {
		Requirements []struct {
			ID  string `json:"requirement_id"`
			Key string `json:"requirement_key"`
		} `json:"requirements"`
	}
	var envelope struct {
		Requirements []struct {
			ID  string `json:"requirement_id"`
			Key string `json:"requirement_key"`
		} `json:"requirements"`
	}
	if err = json.Unmarshal([]byte(packContent), &envelope); err != nil {
		return fmt.Errorf("active instruction pack is invalid: %w", err)
	}
	pack.Requirements = envelope.Requirements
	for _, key := range args[2:] {
		var requirementID string
		for _, requirement := range pack.Requirements {
			if requirement.ID == key || requirement.Key == key {
				requirementID = requirement.ID
				break
			}
		}
		if requirementID == "" {
			return fmt.Errorf("requirement %s is not in the active instruction pack", key)
		}
		if err = tx.QueryRow(`SELECT id FROM requirements WHERE id=? OR requirement_key=?`, requirementID, key).Scan(&requirementID); err != nil {
			return fmt.Errorf("authoritative requirement %s not found", key)
		}
		var existing string
		err = tx.QueryRow(`SELECT o.id FROM contract_operations o JOIN contract_operation_targets t ON t.operation_id=o.id WHERE o.`+subjectColumn+`=? AND o.operation_type='defer' AND o.status='approved' AND o.owner_decision_id<>'' AND t.requirement_id=?`, contractTaskID, requirementID).Scan(&existing)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		operationID, decisionID := "cop-"+shortID(), "od-"+shortID()
		if _, err = tx.Exec(`INSERT INTO owner_decisions(id,`+subjectColumn+`,related_type,related_id,decision_type,decision,notes) VALUES(?,?, 'contract_operation',?, 'approve_contract_operation','approved',?)`, decisionID, contractTaskID, operationID, "Owner-approved deferment"); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO contract_operations(id,`+subjectColumn+`,operation_type,status,resume_condition,completed_task_impact,owner_decision_id,approved_at) VALUES(?,?, 'defer','approved','owner_reactivation','none',?,datetime('now'))`, operationID, contractTaskID, decisionID); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO contract_operation_targets(operation_id,requirement_id) VALUES(?,?)`, operationID, requirementID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`UPDATE implementation_authorizations SET revoked_at=datetime('now') WHERE work_item_id=? AND revoked_at=''`, args[0]); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE work_item_instruction_packs SET status='stale' WHERE work_item_id=? AND status='active'`, args[0]); err != nil {
		return err
	}
	if err = addEvent(tx, args[0], "contract_deviations_approved", "owner", "Owner-approved contract deviations; execution authority requires renewal", map[string]any{"requirement_ids": args[2:]}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return outputOne(db, `SELECT * FROM work_items WHERE id=?`, args[0])
}

func workItemSetStatus(db *sql.DB, id, status string) (map[string]any, error) {
	if status == "done" {
		var managed int
		if err := db.QueryRow(`SELECT COUNT(*) FROM work_items wi JOIN work_item_instruction_packs p ON p.work_item_id=wi.id AND p.status='active' WHERE wi.id=? AND wi.type IN ('task','bug','chore')`, id).Scan(&managed); err != nil {
			return nil, err
		}
		if managed > 0 {
			if err := validateExecutableClosure(db, id); err != nil {
				return nil, err
			}
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRow(`SELECT status FROM work_items WHERE id=?`, id).Scan(&current); err != nil {
		return nil, fmt.Errorf("Work Item %s not found", id)
	}
	if (current == "done" || current == "cancelled") && status != current {
		return nil, errors.New("completed or cancelled managed work requires a new TIP generation before reopening")
	}
	if status == "in_progress" {
		var managed, active int
		_ = tx.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, id).Scan(&managed)
		_ = tx.QueryRow(`SELECT COUNT(*) FROM pipeline_runs WHERE task_id=? AND stage IN ('worker','autofix') AND status IN ('claimed','running')`, id).Scan(&active)
		if managed > 0 && active != 1 {
			return nil, errors.New("managed Work Item requires one active mutation claim before entering in_progress")
		}
	}
	if status == "cancelled" {
		if _, err = tx.Exec(`UPDATE pipeline_runs SET status='cancelled',error='Work Item cancelled',updated_at=datetime('now'),completed_at=datetime('now') WHERE task_id=? AND status IN ('claimed','running')`, id); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(`WITH RECURSIVE descendants(id) AS (SELECT id FROM work_items WHERE parent_id=? UNION ALL SELECT wi.id FROM work_items wi JOIN descendants d ON wi.parent_id=d.id) UPDATE pipeline_runs SET status='cancelled',error='Parent Work Item cancelled',updated_at=datetime('now'),completed_at=datetime('now') WHERE task_id IN (SELECT id FROM descendants) AND status IN ('claimed','running')`, id); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(`WITH RECURSIVE descendants(id) AS (SELECT id FROM work_items WHERE parent_id=? UNION ALL SELECT wi.id FROM work_items wi JOIN descendants d ON wi.parent_id=d.id) UPDATE work_items SET status='cancelled',claimed_at='',claimed_by='' WHERE id IN (SELECT id FROM descendants) AND status NOT IN ('done','cancelled')`, id); err != nil {
			return nil, err
		}
	}
	result, err := tx.Exec(`UPDATE work_items SET status=?,claimed_at=CASE WHEN ? IN ('open','cancelled') THEN '' ELSE claimed_at END,claimed_by=CASE WHEN ? IN ('open','cancelled') THEN '' ELSE claimed_by END WHERE id=?`, status, status, status, id)
	if err != nil {
		return nil, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return nil, fmt.Errorf("Work Item %s not found", id)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return workItemByID(db, id)
}

func parseWorkItemLabels(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	seen := map[string]bool{}
	labels := []string{}
	for _, label := range strings.Split(value, ",") {
		if !workItemLabelPattern.MatchString(label) {
			return nil, fmt.Errorf("invalid label: %s", label)
		}
		if !seen[label] {
			seen[label] = true
			labels = append(labels, label)
		}
	}
	return labels, nil
}

func addWorkItemLabels(tx *sql.Tx, id string, labels []string) error {
	for _, label := range labels {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO work_item_labels(work_item_id,label) VALUES(?,?)`, id, label); err != nil {
			return err
		}
	}
	return nil
}

func workItemLabels(db *sql.DB, id string) ([]string, error) {
	rows, err := db.Query(`SELECT label FROM work_item_labels WHERE work_item_id=? ORDER BY label`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := []string{}
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

func attachWorkItemLabels(db *sql.DB, items []map[string]any) error {
	for _, item := range items {
		labels, err := workItemLabels(db, item["id"].(string))
		if err != nil {
			return err
		}
		item["labels"] = labels
	}
	return nil
}

func workItemLabel(db *sql.DB, args []string) error {
	if len(args) == 1 && args[0] == "list-all" {
		rows, err := queryMaps(db, `SELECT label,COUNT(*) AS count FROM work_item_labels GROUP BY label ORDER BY label`)
		if err == nil {
			writeJSON(os.Stdout, rows)
		}
		return err
	}
	if len(args) < 2 || !contains([]string{"add", "remove", "list"}, args[0]) {
		return errors.New("usage: pic work-item label <add|remove|list> <id> [labels] | label list-all")
	}
	if _, err := workItemByID(db, args[1]); err != nil {
		return err
	}
	if args[0] == "list" {
		labels, err := workItemLabels(db, args[1])
		if err == nil {
			writeJSON(os.Stdout, labels)
		}
		return err
	}
	if len(args) != 3 {
		return fmt.Errorf("usage: pic work-item label %s <id> <a,b>", args[0])
	}
	labels, err := parseWorkItemLabels(args[2])
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if args[0] == "add" {
		err = addWorkItemLabels(tx, args[1], labels)
	} else {
		for _, label := range labels {
			if _, err = tx.Exec(`DELETE FROM work_item_labels WHERE work_item_id=? AND label=?`, args[1], label); err != nil {
				return err
			}
		}
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	item, err := workItemByID(db, args[1])
	if err == nil {
		writeJSON(os.Stdout, item)
	}
	return err
}

func workItemList(db *sql.DB, args []string) ([]map[string]any, error) {
	opts, err := parseOptions(args)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + workItemColumns + ` FROM work_items wi WHERE 1=1`
	values := []any{}
	for _, filter := range []struct {
		name string
		all  bool
	}{{"label", true}, {"label-any", false}} {
		labels, err := parseWorkItemLabels(opts[filter.name])
		if err != nil {
			return nil, err
		}
		if len(labels) == 0 {
			continue
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(labels)), ",")
		query += ` AND (SELECT COUNT(DISTINCT label) FROM work_item_labels WHERE work_item_id=wi.id AND label IN (` + placeholders + `)) `
		for _, label := range labels {
			values = append(values, label)
		}
		if filter.all {
			query += `= ?`
			values = append(values, len(labels))
		} else {
			query += `> 0`
		}
	}
	rows, err := queryMaps(db, query+` ORDER BY created_at,id`, values...)
	if err != nil {
		return nil, err
	}
	if err = attachWorkItemLabels(db, rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func workItemCreate(db *sql.DB, args []string) error {
	if len(args) < 2 || !contains([]string{"epic", "feature", "task", "bug", "chore", "gate"}, args[0]) {
		return errors.New("usage: pic work-item create <epic|feature|task|bug|chore|gate> <title> [--parent <id>] [--description <text>] [--priority <level>] [--labels <a,b>] [--planning-depth <level>]")
	}
	opts, err := parseOptions(args[2:])
	if err != nil {
		return err
	}
	priority := firstNonEmpty(opts["priority"], "medium")
	if !contains([]string{"low", "medium", "high"}, priority) {
		return fmt.Errorf("invalid priority: %s", priority)
	}
	planningDepth := firstNonEmpty(opts["planning-depth"], "full")
	if !validPlanningDepth(planningDepth) {
		return fmt.Errorf("invalid planning depth %s: must be quick|standard|designed|full", planningDepth)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	parent := opts["parent"]
	labels, err := parseWorkItemLabels(opts["labels"])
	if err != nil {
		return err
	}
	if err = validateWorkItemParent(tx, "", parent); err != nil {
		return err
	}
	id := "wi-" + shortID()
	deferred := 0
	if opts["deferred"] == "1" || opts["deferred"] == "true" {
		deferred = 1
	}
	if _, err = tx.Exec(`INSERT INTO work_items(id,type,parent_id,title,description,priority,deferred,planning_depth) VALUES(?,?,NULLIF(?,''),?,?,?,?,?)`, id, args[0], parent, args[1], opts["description"], priority, deferred, planningDepth); err != nil {
		return err
	}
	if parent != "" {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO work_item_labels(work_item_id,label) SELECT ?,label FROM work_item_labels WHERE work_item_id=?`, id, parent); err != nil {
			return err
		}
	}
	if err = addWorkItemLabels(tx, id, labels); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	item, err := workItemByID(db, id)
	if err == nil {
		writeJSON(os.Stdout, item)
	}
	return err
}

func workItemReview(db *sql.DB, args []string) error {
	if len(args) < 2 || !contains([]string{"pending", "passed", "failed"}, args[1]) {
		return errors.New("usage: pic work-item review <id> <pending|passed|failed> [--notes <text>]")
	}
	opts, err := parseOptions(args[2:])
	if err != nil {
		return err
	}
	if opts["pipeline-run-id"] == "" {
		return errors.New("review requires --pipeline-run-id bound to the current candidate")
	}
	result, err := db.Exec(`UPDATE work_items SET review_status=?,review_notes=? WHERE id=? AND type IN ('task','bug','chore') AND EXISTS (
		SELECT 1 FROM pipeline_runs review
		JOIN work_item_instruction_packs pack ON pack.work_item_id=review.task_id AND pack.status='active' AND pack.id=review.instruction_pack_id AND pack.version=review.instruction_pack_version AND pack.content_hash=review.instruction_pack_hash
		JOIN pipeline_runs candidate ON candidate.id=review.candidate_run_id AND candidate.task_id=review.task_id AND candidate.instruction_pack_id=review.instruction_pack_id AND candidate.instruction_pack_version=review.instruction_pack_version AND candidate.instruction_pack_hash=review.instruction_pack_hash AND candidate.integrated_patch_hash=review.candidate_patch_hash
		WHERE review.id=? AND review.task_id=work_items.id AND review.stage='review' AND review.status='completed' AND json_valid(review.result_json) AND json_extract(review.result_json,'$.review_status')=?
		AND candidate.rowid=(SELECT MAX(current.rowid) FROM pipeline_runs current WHERE current.task_id=review.task_id AND current.stage IN ('worker','autofix') AND current.status='completed' AND current.instruction_pack_id=review.instruction_pack_id AND current.instruction_pack_version=review.instruction_pack_version AND current.instruction_pack_hash=review.instruction_pack_hash AND current.artifact_saved_at<>''))`, args[1], opts["notes"], args[0], opts["pipeline-run-id"], args[1])
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return errors.New("review requires an executable Work Item")
	}
	return outputOne(db, `SELECT `+workItemColumns+` FROM work_items WHERE id=?`, args[0])
}

func workItemCompletionSave(db *sql.DB, args []string) error {
	if len(args) < 2 || !contains([]string{"done", "partial", "blocked"}, args[1]) {
		return errors.New("usage: pic work-item completion-save <id> <done|partial|blocked> --pipeline-run-id <id> [--summary <text>] [--report-markdown <text>]")
	}
	status := args[1]
	opts, err := parseOptions(args[2:])
	if err != nil || opts["pipeline-run-id"] == "" {
		return errors.New("completion-save requires --pipeline-run-id")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var packID, packHash string
	var packVersion int
	query := `SELECT p.id,p.version,p.content_hash FROM work_item_instruction_packs p JOIN pipeline_runs r ON r.instruction_pack_id=p.id AND r.instruction_pack_version=p.version AND r.instruction_pack_hash=p.content_hash WHERE p.work_item_id=? AND p.status='active' AND r.id=? AND r.task_id=p.work_item_id AND r.stage IN ('worker','autofix') AND r.status='completed'`
	if status == "done" {
		query += ` AND r.integrated_at<>'' AND r.integrated_patch_hash<>''`
	}
	if err = tx.QueryRow(query, args[0], opts["pipeline-run-id"]).Scan(&packID, &packVersion, &packHash); err != nil {
		return errors.New("completion requires pipeline evidence bound to the active Work Item TIP")
	}
	id := "wicr-" + shortID()
	if _, err = tx.Exec(`INSERT INTO work_item_completion_reports(id,work_item_id,pipeline_run_id,instruction_pack_id,instruction_pack_version,instruction_pack_hash,status,summary,report_markdown) VALUES(?,?,?,?,?,?,?,?,?)`, id, args[0], opts["pipeline-run-id"], packID, packVersion, packHash, status, opts["summary"], opts["report-markdown"]); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return outputOne(db, `SELECT * FROM work_item_completion_reports WHERE id=?`, id)
}

func workItemVerificationSave(db *sql.DB, args []string) error {
	if len(args) < 4 || !contains([]string{"passed", "failed", "partial", "blocked"}, args[2]) {
		return errors.New("usage: pic work-item verification-save <id> <completion-report-id> <passed|failed|partial|blocked> <summary> --actor-role contractor")
	}
	opts, err := parseOptions(args[4:])
	if err != nil || validateWorkflowActor(opts["actor-role"], "contractor") != nil {
		return errors.New("verification requires actor_role=contractor")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentCompletion string
	err = tx.QueryRow(`SELECT c.id FROM work_item_completion_reports c JOIN work_item_instruction_packs p ON p.id=c.instruction_pack_id AND p.work_item_id=c.work_item_id AND p.version=c.instruction_pack_version AND p.content_hash=c.instruction_pack_hash AND p.status='active' JOIN pipeline_runs r ON r.id=c.pipeline_run_id AND r.task_id=c.work_item_id AND r.integrated_at<>'' AND r.integrated_patch_hash<>'' WHERE c.work_item_id=? AND c.status='done' ORDER BY datetime(c.created_at) DESC,c.rowid DESC LIMIT 1`, args[0]).Scan(&currentCompletion)
	if err != nil || currentCompletion != args[1] {
		return errors.New("verification requires the current integrated Completion Report bound to the active TIP")
	}
	state, stateErr := loadWorkItemExecutionState(tx, args[0])
	if stateErr != nil || state.CompletionID != args[1] || state.ReviewStatus != "passed" {
		return errors.New("verification requires a passed review for the current integrated Completion Report")
	}
	id := "wivr-" + shortID()
	var pipelineHighWaterRowID int64
	if err = tx.QueryRow(`SELECT COALESCE(MAX(rowid),0) FROM pipeline_runs WHERE task_id=?`, args[0]).Scan(&pipelineHighWaterRowID); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO work_item_verification_reports(id,work_item_id,completion_report_id,status,summary,verified_by_role,pipeline_high_water_rowid) VALUES(?,?,?,?,?,?,?)`, id, args[0], args[1], args[2], args[3], opts["actor-role"], pipelineHighWaterRowID); err != nil {
		return err
	}
	if args[2] == "passed" {
		if _, err = tx.Exec(`UPDATE work_items SET status='done',claimed_at='',claimed_by='',review_status='passed' WHERE id=? AND type IN ('task','bug','chore')`, args[0]); err != nil {
			return err
		}
	} else {
		if _, err = tx.Exec(`UPDATE work_items SET status='open',claimed_at='',claimed_by='' WHERE id=? AND type IN ('task','bug','chore')`, args[0]); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return outputOne(db, `SELECT * FROM work_item_verification_reports WHERE id=?`, id)
}

func workItemAccept(db *sql.DB, args []string) error {
	if len(args) < 4 || !contains([]string{"accepted", "rejected"}, args[2]) {
		return errors.New("usage: pic work-item accept <id> <completion-report-id> <accepted|rejected> <notes> --actor-role owner")
	}
	opts, err := parseOptions(args[4:])
	if err != nil || validateWorkflowActor(opts["actor-role"], "owner") != nil {
		return errors.New("acceptance requires actor_role=owner")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var workItemType string
	if err = tx.QueryRow(`SELECT type FROM work_items WHERE id=?`, args[0]).Scan(&workItemType); err != nil {
		return fmt.Errorf("Work Item %s not found", args[0])
	}
	if contains([]string{"task", "bug", "chore"}, workItemType) {
		return errors.New("owner acceptance applies only to aggregate Work Items; passed contractor verification closes executable children")
	}
	var existingDecision int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM work_item_owner_decisions WHERE work_item_id=? AND completion_report_id=?`, args[0], args[1]).Scan(&existingDecision); err != nil {
		return err
	}
	if existingDecision != 0 {
		return errors.New("owner decision already recorded; a corrected Completion Report is required")
	}
	if err = validateExecutableClosureForReport(tx, args[0], args[1], false); err != nil {
		return err
	}
	decisionID := "wiod-" + shortID()
	if _, err = tx.Exec(`INSERT INTO work_item_owner_decisions(id,work_item_id,completion_report_id,decision,notes,decided_by_role) VALUES(?,?,?,?,?,?)`, decisionID, args[0], args[1], args[2], args[3], opts["actor-role"]); err != nil {
		return err
	}
	if args[2] == "accepted" {
		result, updateErr := tx.Exec(`UPDATE work_items SET status='done',claimed_at='',claimed_by='',review_status='passed' WHERE id=? AND type IN ('task','bug','chore')`, args[0])
		if updateErr != nil {
			return updateErr
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return errors.New("acceptance requires an executable Work Item")
		}
	} else {
		if _, err = tx.Exec(`UPDATE work_items SET status='open',claimed_at='',claimed_by='',review_status='pending',review_notes=? WHERE id=? AND type IN ('task','bug','chore')`, args[3], args[0]); err != nil {
			return err
		}
		var attempt int
		if err = tx.QueryRow(`SELECT r.attempt FROM work_item_completion_reports c JOIN pipeline_runs r ON r.id=c.pipeline_run_id WHERE c.id=? AND c.work_item_id=?`, args[1], args[0]).Scan(&attempt); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"after_attempt": attempt, "completion_report_id": args[1]})
		if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,summary,payload_json) VALUES(?,?,'owner_rejected_completion','owner',?,?)`, "wie-"+shortID(), args[0], args[3], string(payload)); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	item, err := workItemByID(db, args[0])
	if err == nil {
		writeJSON(os.Stdout, item)
	}
	return err
}

func validateExecutableClosure(db workflowStore, id string) error {
	return validateExecutableClosureForReport(db, id, "", false)
}

func validateExecutableClosureForReport(db workflowStore, id, expectedCompletion string, requireAcceptance bool) error {
	state, err := loadWorkItemExecutionState(db, id)
	if err != nil || state.ReviewStatus != "passed" || state.CompletionID == "" || (expectedCompletion != "" && state.CompletionID != expectedCompletion) {
		return errors.New("completion requires a passed review and current integrated Completion Report")
	}
	if state.VerificationStatus != "passed" {
		return errors.New("completion requires passed contractor verification")
	}
	if requireAcceptance && state.OwnerDecision != "accepted" {
		return errors.New("completion requires owner acceptance")
	}
	return nil
}

type workItemExecutionState struct {
	PackID                string `json:"active_instruction_pack_id"`
	CandidateID           string `json:"candidate_run_id"`
	ReviewStatus          string `json:"review_status"`
	OwnerApprovalRequired bool   `json:"owner_approval_required"`
	CompletionID          string `json:"completion_report_id"`
	VerificationStatus    string `json:"verification_status"`
	OwnerDecision         string `json:"owner_decision"`
	NextStage             string `json:"next_stage"`
	PipelineStage         string `json:"pipeline_stage"`
}

func loadWorkItemExecutionState(db databaseQueryer, id string) (workItemExecutionState, error) {
	state := workItemExecutionState{NextStage: "instruction_pack"}
	var packVersion int
	var packHash string
	err := db.QueryRow(`SELECT id,version,content_hash FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, id).Scan(&state.PackID, &packVersion, &packHash)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	state.NextStage = "implement"
	state.PipelineStage = "worker"
	var verifiedCandidate, verifiedCompletion, verifiedStatus string
	if err := db.QueryRow(`SELECT c.pipeline_run_id,c.id,v.status
		FROM work_item_verification_reports v
		JOIN work_item_completion_reports c ON c.id=v.completion_report_id AND c.work_item_id=v.work_item_id AND c.instruction_pack_id=? AND c.instruction_pack_version=? AND c.instruction_pack_hash=? AND c.status='done'
		JOIN pipeline_runs candidate ON candidate.id=c.pipeline_run_id AND candidate.task_id=c.work_item_id AND candidate.integrated_at<>'' AND candidate.integrated_patch_hash<>''
		JOIN pipeline_runs review ON review.task_id=c.work_item_id AND review.stage='review' AND review.status='completed' AND review.candidate_run_id=c.pipeline_run_id AND json_valid(review.result_json) AND json_extract(review.result_json,'$.review_status')='passed' AND json_extract(review.result_json,'$.candidate_patch_hash')=candidate.integrated_patch_hash
		WHERE v.work_item_id=? AND (
			v.pipeline_high_water_rowid=0 AND NOT EXISTS (SELECT 1 FROM pipeline_runs later WHERE later.task_id=v.work_item_id AND later.instruction_pack_id=? AND later.instruction_pack_version=? AND later.instruction_pack_hash=? AND datetime(later.created_at)>datetime(v.created_at))
			OR v.pipeline_high_water_rowid>0 AND NOT EXISTS (SELECT 1 FROM pipeline_runs later WHERE later.task_id=v.work_item_id AND later.rowid>v.pipeline_high_water_rowid)
		) ORDER BY v.rowid DESC LIMIT 1`, state.PackID, packVersion, packHash, id, state.PackID, packVersion, packHash).Scan(&verifiedCandidate, &verifiedCompletion, &verifiedStatus); err == nil {
		state.CandidateID = verifiedCandidate
		state.ReviewStatus = "passed"
		state.CompletionID = verifiedCompletion
		state.VerificationStatus = verifiedStatus
		state.NextStage = "contractor_verification"
		state.PipelineStage = ""
		return finalizeVerifiedExecutionState(db, id, state), nil
	}
	_ = db.QueryRow(`SELECT id FROM pipeline_runs WHERE task_id=? AND stage IN ('worker','autofix') AND status='completed' AND instruction_pack_id=? AND instruction_pack_version=? AND instruction_pack_hash=? AND artifact_saved_at<>'' ORDER BY rowid DESC LIMIT 1`, id, state.PackID, packVersion, packHash).Scan(&state.CandidateID)
	if state.CandidateID == "" {
		return state, nil
	}
	state.NextStage = "review"
	state.PipelineStage = "review"
	var ownerApprovalRequired int
	_ = db.QueryRow(`SELECT json_extract(result_json,'$.review_status'),COALESCE(json_extract(result_json,'$.owner_approval_required'),0) FROM pipeline_runs WHERE task_id=? AND stage='review' AND status='completed' AND instruction_pack_id=? AND instruction_pack_version=? AND instruction_pack_hash=? AND candidate_run_id=? AND json_valid(result_json) AND json_extract(result_json,'$.candidate_patch_hash')=(SELECT integrated_patch_hash FROM pipeline_runs WHERE id=?) ORDER BY rowid DESC LIMIT 1`, id, state.PackID, packVersion, packHash, state.CandidateID, state.CandidateID).Scan(&state.ReviewStatus, &ownerApprovalRequired)
	state.OwnerApprovalRequired = ownerApprovalRequired != 0
	if state.ReviewStatus != "passed" {
		if state.ReviewStatus == "failed" && state.OwnerApprovalRequired {
			state.NextStage = "owner_approval"
			state.PipelineStage = ""
		} else if state.ReviewStatus == "failed" {
			state.NextStage = "implement"
			state.PipelineStage = "worker"
		}
		return state, nil
	}
	_ = db.QueryRow(`SELECT c.id FROM work_item_completion_reports c JOIN pipeline_runs r ON r.id=c.pipeline_run_id AND r.id=? AND r.integrated_at<>'' AND r.integrated_patch_hash<>'' WHERE c.work_item_id=? AND c.instruction_pack_id=? AND c.instruction_pack_version=? AND c.instruction_pack_hash=? AND c.status='done' ORDER BY c.rowid DESC LIMIT 1`, state.CandidateID, id, state.PackID, packVersion, packHash).Scan(&state.CompletionID)
	if state.CompletionID == "" {
		return state, nil
	}
	state.NextStage = "contractor_verification"
	state.PipelineStage = ""
	_ = db.QueryRow(`SELECT status FROM work_item_verification_reports WHERE work_item_id=? AND completion_report_id=? ORDER BY rowid DESC LIMIT 1`, id, state.CompletionID).Scan(&state.VerificationStatus)
	return finalizeVerifiedExecutionState(db, id, state), nil
}

func finalizeVerifiedExecutionState(db databaseQueryer, id string, state workItemExecutionState) workItemExecutionState {
	if state.VerificationStatus != "passed" {
		if state.VerificationStatus == "failed" || state.VerificationStatus == "partial" {
			state.NextStage = "implement"
			state.PipelineStage = "autofix"
		}
		return state
	}
	state.NextStage = "done"
	_ = db.QueryRow(`SELECT decision FROM work_item_owner_decisions WHERE work_item_id=? AND completion_report_id=? ORDER BY rowid DESC LIMIT 1`, id, state.CompletionID).Scan(&state.OwnerDecision)
	if state.OwnerDecision == "rejected" {
		state.NextStage = "implement"
		state.PipelineStage = "worker"
	}
	return state
}

const workItemReadySQL = `wi.type IN ('task','bug','chore') AND wi.status='open' AND wi.deferred=0 AND wi.claimed_at='' AND ((
	SELECT COUNT(*) FROM work_item_instruction_packs p WHERE p.work_item_id=wi.id AND p.status='active'
)=1 OR EXISTS (
	SELECT 1 FROM work_item_materializations m JOIN implementation_authorizations a ON a.work_item_id=m.root_work_item_id AND a.task_graph_checkpoint_id=m.checkpoint_id AND a.revoked_at='' WHERE m.work_item_id=wi.id
)) AND NOT EXISTS (
	SELECT 1 FROM work_item_relations r JOIN work_items blocker ON blocker.id=r.related_work_item_id WHERE r.work_item_id=wi.id AND r.relation_type='blocks' AND blocker.status!='done'
) AND NOT EXISTS (
	SELECT 1 FROM work_item_relations r JOIN work_items gate_item ON gate_item.id=r.related_work_item_id WHERE r.work_item_id=wi.id AND r.relation_type='gates' AND gate_item.status!='done'
)`

func addWorkItemRelation(db *sql.DB, args []string, relationType string) error {
	if len(args) != 2 {
		return errors.New("usage: pic work-item relate <work-item-id> <blocks|gates|related> <related-work-item-id>")
	}
	if !contains([]string{"blocks", "gates", "related"}, relationType) {
		return fmt.Errorf("invalid relation type: %s", relationType)
	}
	if _, err := workItemByID(db, args[0]); err != nil {
		return err
	}
	blocker, err := workItemByID(db, args[1])
	if err != nil {
		return err
	}
	if relationType == "gates" && blocker["type"] != "gate" {
		return fmt.Errorf("Work Item %s is not a gate", args[1])
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if relationType != "related" {
		var cycle int
		err = tx.QueryRow(`WITH RECURSIVE dependencies(id) AS (
			SELECT ? UNION SELECT r.related_work_item_id FROM work_item_relations r JOIN dependencies d ON r.work_item_id=d.id WHERE r.relation_type IN ('blocks','gates')
		) SELECT EXISTS(SELECT 1 FROM dependencies WHERE id=?)`, args[1], args[0]).Scan(&cycle)
		if err != nil {
			return err
		}
		if cycle != 0 {
			return errors.New("dependency cycle")
		}
	}
	_, err = tx.Exec(`INSERT INTO work_item_relations(id,work_item_id,relation_type,related_work_item_id) VALUES(?,?,?,?)`, "wir-"+shortID(), args[0], relationType, args[1])
	if err == nil {
		err = tx.Commit()
	}
	if err == nil {
		writeJSON(os.Stdout, map[string]any{"work_item_id": args[0], "relation_type": relationType, "related_work_item_id": args[1]})
	}
	return err
}

func workItemClaim(db *sql.DB, args []string) error {
	if len(args) != 2 || args[1] == "" {
		return errors.New("usage: pic work-item claim <id> <claimant>")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind string
	if err = tx.QueryRow(`SELECT type FROM work_items WHERE id=?`, args[0]).Scan(&kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("Work Item %s not found", args[0])
		}
		return err
	}
	if kind == "epic" || kind == "feature" || kind == "gate" {
		return fmt.Errorf("%s Work Item is not executable", kind)
	}
	result, err := tx.Exec(`UPDATE work_items AS wi SET claimed_at=datetime('now'),claimed_by=? WHERE wi.id=? AND `+workItemReadySQL, args[1], args[0])
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("Work Item %s is not ready", args[0])
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	item, err := workItemByID(db, args[0])
	if err == nil {
		writeJSON(os.Stdout, item)
	}
	return err
}

var workItemStages = []string{"scan", "rri", "rri_t_scenarios", "vision", "blueprint", "contracts", "task_graph"}

type rriFinalization struct {
	Requirements []struct {
		Key                string `json:"key"`
		Priority           string `json:"priority"`
		Title              string `json:"title"`
		Description        string `json:"description"`
		AcceptanceCriteria string `json:"acceptanceCriteria"`
	} `json:"requirements"`
	Decisions []struct {
		Key    string `json:"key"`
		Answer string `json:"answer"`
	} `json:"decisions"`
	Report rriReport `json:"report"`
}

type rriReport struct {
	ProjectName        string                  `json:"project_name"`
	Generated          string                  `json:"generated"`
	PolicyVersion      int                     `json:"rri_policy_version,omitempty"`
	RequirementsMatrix []rriRequirementRow     `json:"requirements_matrix"`
	AutoAnswered       []rriAutoAnswerRow      `json:"auto_answered"`
	DecisionsLog       []rriDecisionRow        `json:"decisions_log"`
	OpenQuestions      []rriOpenQuestion       `json:"open_questions"`
	NotYetSpecified    []rriNotYetSpecifiedRow `json:"not_yet_specified,omitempty"`
	OutOfScope         []rriOutOfScopeRow      `json:"out_of_scope,omitempty"`
	GlossaryUpdates    []rriGlossaryUpdate     `json:"glossary_updates,omitempty"`
}

// rriGlossaryUpdate is one explicitly identified CONTEXT.md glossary entry
// (REQ-F1-6): the owner approves the RRI artifact before the entry reaches
// repository truth.
type rriGlossaryUpdate struct {
	Term       string `json:"term"`
	Definition string `json:"definition"`
	Avoid      string `json:"avoid,omitempty"`
}
type rriRequirementRow struct {
	ReqID       string `json:"req_id"`
	Requirement string `json:"requirement"`
	Source      string `json:"source"`
	Priority    string `json:"priority"`
	Persona     string `json:"persona"`
}
type rriAutoAnswerRow struct {
	Topic      string `json:"topic"`
	Details    string `json:"details"`
	Resolution string `json:"resolution"`
}
type rriDecisionRow struct {
	Decision          string `json:"decision"`
	OptionsConsidered string `json:"options_considered"`
	Chosen            string `json:"chosen"`
	Rationale         string `json:"rationale"`
}
type rriOpenQuestion struct {
	ID         string         `json:"id"`
	Question   string         `json:"question"`
	Status     string         `json:"status,omitempty"`
	Priority   string         `json:"priority,omitempty"`
	Mode       string         `json:"mode,omitempty"`
	Blocks     *bool          `json:"blocks,omitempty"`
	Resolution *rriResolution `json:"resolution,omitempty"`
}
type rriResolution struct {
	Answer string `json:"answer"`
	Source string `json:"source"`
}
type rriNotYetSpecifiedRow struct {
	Uncertainty    string `json:"uncertainty"`
	GraduationPath string `json:"graduation_path"`
}
type rriOutOfScopeRow struct {
	Exclusion string `json:"exclusion"`
	Reason    string `json:"reason"`
}

// validateRriGlossaryUpdates enforces the glossary-update section shared by
// RRI finalization and approval-time application: every entry needs a term and
// a definition, and terms are unique so the approved write is deterministic.
func validateRriGlossaryUpdates(updates []rriGlossaryUpdate) error {
	seen := map[string]bool{}
	for _, row := range updates {
		if strings.TrimSpace(row.Term) == "" || strings.TrimSpace(row.Definition) == "" {
			return errors.New("RRI glossary_updates rows require term and definition")
		}
		if seen[row.Term] {
			return fmt.Errorf("RRI glossary_updates contains duplicate term %q", row.Term)
		}
		seen[row.Term] = true
	}
	return nil
}

// Marker-gated frontier schema for open_questions: reports carrying rri_policy_version 2
// require the frontier fields, while pre-marker reports stay valid under the legacy shape.
var (
	rriOpenQuestionStatuses   = []string{"open", "resolved", "deferred"}
	rriOpenQuestionPriorities = []string{"P0", "P1", "P2", "P3"}
	rriOpenQuestionModes      = []string{"afk", "hitl"}
)

// marshalRriReport persists the report. Marked reports (rri_policy_version >= 2)
// must carry both scope sections in the artifact even when empty, but plain
// json.Marshal drops empty slices via omitempty; legacy reports keep the keys
// omitted. Merge the scope keys back in for marked reports.
func marshalRriReport(report rriReport) ([]byte, error) {
	base, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	if report.PolicyVersion < 2 {
		return base, nil
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, err
	}
	if merged["not_yet_specified"], err = json.Marshal(report.NotYetSpecified); err != nil {
		return nil, err
	}
	if merged["out_of_scope"], err = json.Marshal(report.OutOfScope); err != nil {
		return nil, err
	}
	return json.Marshal(merged)
}

func validateRriReport(report rriReport) error {
	if report.PolicyVersion > 2 {
		return fmt.Errorf("unsupported rri_policy_version %d", report.PolicyVersion)
	}
	if strings.TrimSpace(report.ProjectName) == "" || strings.TrimSpace(report.Generated) == "" {
		return errors.New("RRI report requires project_name and generated")
	}
	// Marker-gated required-array parity (OB-F1-7): marked reports must carry
	// every core array and the failure names the missing section exactly like
	// the TypeScript validator; legacy reports keep their pre-marker tolerance
	// for absent sections (nil slices stay valid). A missing array unmarshals
	// to nil, so nil is the rejected state.
	if report.PolicyVersion >= 2 {
		for _, missing := range []struct {
			present bool
			name    string
		}{
			{report.RequirementsMatrix != nil, "requirements_matrix"},
			{report.AutoAnswered != nil, "auto_answered"},
			{report.DecisionsLog != nil, "decisions_log"},
			{report.OpenQuestions != nil, "open_questions"},
		} {
			if !missing.present {
				return fmt.Errorf("marked RRI report is missing the %s section", missing.name)
			}
		}
	}
	for _, row := range report.RequirementsMatrix {
		if row.ReqID == "" || row.Requirement == "" || row.Source == "" || row.Priority == "" || row.Persona == "" {
			return errors.New("RRI requirements_matrix rows require req_id, requirement, source, priority, and persona")
		}
	}
	for _, row := range report.AutoAnswered {
		if row.Topic == "" || row.Details == "" || row.Resolution == "" {
			return errors.New("RRI auto_answered rows require topic, details, and resolution")
		}
	}
	for _, row := range report.DecisionsLog {
		if row.Decision == "" || row.OptionsConsidered == "" || row.Chosen == "" || row.Rationale == "" {
			return errors.New("RRI decisions_log rows require decision, options_considered, chosen, and rationale")
		}
	}
	for _, row := range report.OpenQuestions {
		if row.ID == "" || row.Question == "" {
			return errors.New("RRI open_questions rows require id and question")
		}
		if report.PolicyVersion < 2 {
			continue
		}
		if row.Status == "" {
			return fmt.Errorf("RRI open_questions row %s requires status", row.ID)
		}
		if !contains(rriOpenQuestionStatuses, row.Status) {
			return fmt.Errorf("RRI open_questions row %s has invalid status %s", row.ID, row.Status)
		}
		if row.Priority == "" {
			return fmt.Errorf("RRI open_questions row %s requires priority", row.ID)
		}
		if !contains(rriOpenQuestionPriorities, row.Priority) {
			return fmt.Errorf("RRI open_questions row %s has invalid priority %s", row.ID, row.Priority)
		}
		if row.Mode == "" {
			return fmt.Errorf("RRI open_questions row %s requires mode", row.ID)
		}
		if !contains(rriOpenQuestionModes, row.Mode) {
			return fmt.Errorf("RRI open_questions row %s has invalid mode %s", row.ID, row.Mode)
		}
		if row.Blocks == nil {
			return fmt.Errorf("RRI open_questions row %s requires blocks", row.ID)
		}
		if row.Status != "open" && (row.Resolution == nil || row.Resolution.Answer == "" || row.Resolution.Source == "") {
			return fmt.Errorf("RRI open_questions row %s requires resolution with answer and source when status is resolved or deferred", row.ID)
		}
	}
	// Scope sections are marker-gated like the open_questions frontier fields:
	// marked reports must carry both, legacy reports stay valid without them.
	// A missing section unmarshals to a nil slice, so nil is the rejected state.
	if report.PolicyVersion >= 2 {
		if report.NotYetSpecified == nil {
			return errors.New("marked RRI report requires the not_yet_specified section")
		}
		for _, row := range report.NotYetSpecified {
			if row.Uncertainty == "" || row.GraduationPath == "" {
				return errors.New("RRI not_yet_specified rows require uncertainty and graduation_path")
			}
		}
		if report.OutOfScope == nil {
			return errors.New("marked RRI report requires the out_of_scope section")
		}
		for _, row := range report.OutOfScope {
			if row.Exclusion == "" || row.Reason == "" {
				return errors.New("RRI out_of_scope rows require exclusion and reason")
			}
		}
	}
	if err := validateRriGlossaryUpdates(report.GlossaryUpdates); err != nil {
		return err
	}
	return nil
}

func validateRriReportConsistency(payload rriFinalization) error {
	if len(payload.Requirements) != len(payload.Report.RequirementsMatrix) {
		return errors.New("RRI report requirements_matrix must match the confirmed requirements")
	}
	for _, requirement := range payload.Requirements {
		matched := false
		for _, row := range payload.Report.RequirementsMatrix {
			if row.ReqID == requirement.Key && row.Requirement == requirement.Title {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("RRI report does not describe confirmed requirement %s", requirement.Key)
		}
	}
	if len(payload.Decisions) != len(payload.Report.DecisionsLog) {
		return errors.New("RRI decisions_log must match the confirmed decisions")
	}
	return nil
}

// RRI publish gate (REQ-F1-3): for marked frontier reports (rri_policy_version 2)
// an open P0/P1 question rejects publication and a deferred P0/P1 question
// requires a non-empty owner deferral reason. Status and priority are checked
// together, so open P2/P3 rows and legacy pre-marker reports never block.
func validateRriPublishGate(report rriReport) error {
	if report.PolicyVersion < 2 {
		return nil
	}
	for _, row := range report.OpenQuestions {
		if row.Priority != "P0" && row.Priority != "P1" {
			continue
		}
		if row.Status == "open" {
			return fmt.Errorf("RRI publication blocked: open P0/P1 question %s (%s) remains unresolved: %s", row.ID, row.Priority, row.Question)
		}
		if row.Status == "deferred" && (row.Resolution == nil || strings.TrimSpace(row.Resolution.Answer) == "") {
			return fmt.Errorf("RRI publication blocked: deferred P0/P1 question %s (%s) requires a non-empty owner deferral reason", row.ID, row.Priority)
		}
	}
	return nil
}

// insertRriDeferralDecisions persists each reasoned deferred P0/P1 question as
// a durable owner decision row in work_item_owner_decisions (the
// contract-required deferral home for REQ-F1-3): decision='deferred' with the
// question/artifact linkage and the owner-recorded reason in notes, so Blueprint
// review projections reading the show document's owner_decisions collection can
// surface the deferral. The publish gate guarantees the reason is non-empty.
func insertRriDeferralDecisions(tx *sql.Tx, workItemID, artifactID string, report rriReport) error {
	if report.PolicyVersion < 2 {
		return nil
	}
	for _, row := range report.OpenQuestions {
		if (row.Priority != "P0" && row.Priority != "P1") || row.Status != "deferred" {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO work_item_owner_decisions(id,work_item_id,completion_report_id,decision,question_id,rri_artifact_id,notes,decided_by_role) VALUES(?,?,NULL,'deferred',?,?,?,'owner')`, "wiod-"+shortID(), workItemID, row.ID, artifactID, row.Resolution.Answer); err != nil {
			return err
		}
	}
	return nil
}

// RRI glossary/ADR conflict guard (REQ-F1-5): marked RRI payloads fail closed
// when resolved requirement or decision terminology contradicts the repository
// glossary (CONTEXT.md) or accepted ADR terms (docs/adr/*.md). Comparison
// normalizes case and whitespace only, so the persisted report keeps its
// source text; a conflict returns before any artifact, requirement, decision,
// or event is written, leaving the previous state untouched.
type rriForbiddenTerm struct {
	Phrase    string // conflicting terminology in the source document's spelling
	Canonical string // glossary term whose definition the phrase contradicts (empty for ADR rejections)
	Source    string // document path the constraint comes from
}

func validateRriTerminology(payload rriFinalization) error {
	if payload.Report.PolicyVersion < 2 {
		return nil
	}
	terms, err := loadRriForbiddenTerms()
	if err != nil {
		return err
	}
	for _, requirement := range payload.Requirements {
		for _, text := range []string{requirement.Title, requirement.Description, requirement.AcceptanceCriteria} {
			if err := checkRriTerminology(terms, "requirement", requirement.Key, text); err != nil {
				return err
			}
		}
	}
	for _, decision := range payload.Decisions {
		if err := checkRriTerminology(terms, "decision", decision.Key, decision.Answer); err != nil {
			return err
		}
	}
	return nil
}

func checkRriTerminology(terms []rriForbiddenTerm, kind, key, text string) error {
	normalized := strings.Join(strings.Fields(text), " ")
	if normalized == "" {
		return nil
	}
	for _, term := range terms {
		pattern, err := regexp.Compile(`(?i)\b` + regexp.QuoteMeta(strings.Join(strings.Fields(term.Phrase), " ")) + `\b`)
		if err != nil {
			return fmt.Errorf("RRI terminology check failed for constraint from %s: %w", term.Source, err)
		}
		if !pattern.MatchString(normalized) {
			continue
		}
		if term.Canonical != "" {
			return fmt.Errorf("RRI save blocked: %s %s uses %q, which contradicts the glossary definition of %s (source: %s)", kind, key, term.Phrase, term.Canonical, term.Source)
		}
		return fmt.Errorf("RRI save blocked: %s %s uses %q, which is rejected by accepted ADR %s", kind, key, term.Phrase, term.Source)
	}
	return nil
}

func loadRriForbiddenTerms() ([]rriForbiddenTerm, error) {
	root, err := findRriTruthRoot()
	if err != nil || root == "" {
		return nil, err
	}
	var terms []rriForbiddenTerm
	glossary, err := os.ReadFile(filepath.Join(root, "CONTEXT.md"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("RRI terminology check failed to read repository truth: %w", err)
	}
	if err == nil {
		terms = append(terms, parseRriGlossaryAvoidTerms(string(glossary), "CONTEXT.md")...)
	}
	adrFiles, err := filepath.Glob(filepath.Join(root, "docs", "adr", "*.md"))
	if err != nil {
		return nil, fmt.Errorf("RRI terminology check failed to read repository truth: %w", err)
	}
	for _, adrPath := range adrFiles {
		content, err := os.ReadFile(adrPath)
		if err != nil {
			return nil, fmt.Errorf("RRI terminology check failed to read repository truth: %w", err)
		}
		// Only accepted architecture decisions constrain terminology; drafts
		// and proposed records stay advisory.
		if !strings.Contains(string(content), "**Status**: accepted") {
			continue
		}
		rel, err := filepath.Rel(root, adrPath)
		if err != nil {
			rel = adrPath
		}
		terms = append(terms, parseRriAdrRejectedTerms(string(content), rel)...)
	}
	return terms, nil
}

// findRriTruthRoot walks up from the working directory to the first directory
// carrying the repository glossary (CONTEXT.md) or ADR directory, mirroring the
// upward resolution of findDB. An empty result means the project defines no
// repository truth, which leaves marked saves unguarded rather than blocked;
// truth that exists but cannot be read fails closed in loadRriForbiddenTerms.
func findRriTruthRoot() (string, error) {
	dir, err := filepath.Abs(".")
	if err != nil {
		return "", err
	}
	for current := dir; ; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, "CONTEXT.md")); err == nil {
			return current, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("RRI terminology check failed to read repository truth: %w", err)
		}
		adrDir := filepath.Join(current, "docs", "adr")
		entries, err := os.ReadDir(adrDir)
		if err == nil {
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".md") {
					return current, nil
				}
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("RRI terminology check failed to read repository truth: %w", err)
		}
		if filepath.Dir(current) == current {
			return "", nil
		}
	}
}

var (
	rriGlossaryTermPattern  = regexp.MustCompile(`^\*\*([^*]+)\*\*:\s*$`)
	rriGlossaryAvoidPattern = regexp.MustCompile(`^_Avoid_:\s*(.+?)\s*$`)
)

// parseRriGlossaryAvoidTerms extracts every _Avoid_ phrase from CONTEXT.md
// entries; each phrase is conflicting terminology for the canonical term that
// owns the entry, so a payload using it contradicts that definition.
func parseRriGlossaryAvoidTerms(content, source string) []rriForbiddenTerm {
	var terms []rriForbiddenTerm
	canonical := ""
	for _, line := range strings.Split(content, "\n") {
		if match := rriGlossaryTermPattern.FindStringSubmatch(line); match != nil {
			canonical = strings.TrimSpace(match[1])
			continue
		}
		match := rriGlossaryAvoidPattern.FindStringSubmatch(line)
		if match == nil || canonical == "" {
			continue
		}
		for _, phrase := range strings.Split(match[1], ",") {
			phrase = strings.TrimSpace(phrase)
			if phrase != "" {
				terms = append(terms, rriForbiddenTerm{Phrase: phrase, Canonical: canonical, Source: source})
			}
		}
	}
	return terms
}

var (
	rriAdrRejectPattern        = regexp.MustCompile(`(?i)\breject\b`)
	rriAdrJustificationPattern = regexp.MustCompile(`(?i)\b(?:not\s+sufficient|insufficient)\s+justification\s+for\b`)
)

// parseRriAdrRejectedTerms extracts the explicit rejected practices from an
// accepted ADR: a sentence containing "reject" contributes the phrases that
// follow it, split on " and ", and a sentence stating something "is not
// sufficient justification for" a practice contributes that practice (the
// ADR 0002 constraint forbidding speculative abstractions justified only by a
// single implementation). Parsing stays conservative so ADR prose only adds a
// constraint when the document states one explicitly, and the two-word
// minimum keeps generic single-word terms from blocking every RRI save.
func parseRriAdrRejectedTerms(content, source string) []rriForbiddenTerm {
	var terms []rriForbiddenTerm
	for _, sentence := range strings.Split(content, ".") {
		for _, pattern := range []*regexp.Regexp{rriAdrJustificationPattern, rriAdrRejectPattern} {
			for _, loc := range pattern.FindAllStringIndex(sentence, -1) {
				phrase := trimRriAdrPhraseLeadIn(strings.TrimSpace(sentence[loc[1]:]))
				for _, part := range strings.Split(phrase, " and ") {
					part = strings.TrimSpace(part)
					if len(strings.Fields(part)) >= 2 {
						terms = append(terms, rriForbiddenTerm{Phrase: part, Source: source})
					}
				}
			}
		}
	}
	return terms
}

// trimRriAdrPhraseLeadIn strips enumerators and articles so the forbidden
// phrase names the practice itself rather than its grammatical lead-in.
func trimRriAdrPhraseLeadIn(phrase string) string {
	for {
		trimmed := phrase
		for _, prefix := range []string{"both ", "a ", "an ", "the "} {
			trimmed = strings.TrimPrefix(trimmed, prefix)
		}
		if trimmed == phrase {
			return phrase
		}
		phrase = trimmed
	}
}

func workItemRriFinalize(db *sql.DB, args []string) error {
	if len(args) < 2 {
		return errors.New("usage: pic work-item rri-finalize <id> <payload-json> --actor-role contractor")
	}
	opts, err := parseOptions(args[2:])
	if err != nil {
		return err
	}
	if err := validateWorkflowActor(opts["actor-role"], "contractor"); err != nil {
		return err
	}
	actorRole := opts["actor-role"]
	var payload rriFinalization
	if err := json.Unmarshal([]byte(args[1]), &payload); err != nil || len(payload.Requirements) == 0 {
		return errors.New("RRI finalization requires valid JSON with requirements, decisions, and report")
	}
	if err := validateRriReport(payload.Report); err != nil {
		return err
	}
	if err := validateRriReportConsistency(payload); err != nil {
		return err
	}
	if err := validateRriPublishGate(payload.Report); err != nil {
		return err
	}
	// Glossary/ADR conflicts are pure terminology checks, so they run with the
	// other pre-flight validators before the transaction opens; any conflict
	// therefore commits no partial state by construction.
	if err := validateRriTerminology(payload); err != nil {
		return err
	}
	seenRequirements, seenDecisions := map[string]bool{}, map[string]bool{}
	for _, requirement := range payload.Requirements {
		if requirement.Key == "" || requirement.Title == "" || !contains([]string{"tier1", "tier2", "tier3"}, requirement.Priority) || seenRequirements[requirement.Key] {
			return fmt.Errorf("invalid or duplicate RRI requirement %q", requirement.Key)
		}
		if err := validateGherkinSteps(requirement.AcceptanceCriteria); err != nil {
			return fmt.Errorf("%s acceptance criteria %w", requirement.Key, err)
		}
		seenRequirements[requirement.Key] = true
	}
	for _, decision := range payload.Decisions {
		if decision.Key == "" || strings.TrimSpace(decision.Answer) == "" || seenDecisions[decision.Key] {
			return fmt.Errorf("invalid or duplicate RRI decision %q", decision.Key)
		}
		seenDecisions[decision.Key] = true
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	item, err := workItemByIDTx(tx, args[0])
	if err != nil {
		return err
	}
	itemType := fmt.Sprint(item["type"])
	subjectColumn := "task_id"
	if itemType == "epic" || itemType == "feature" {
		subjectColumn = "epic_id"
	}
	var scanApproved, finalized, revision int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM workflow_checkpoints WHERE work_item_id=? AND stage='scan' AND decision_type='accepted'`, args[0]).Scan(&scanApproved); err != nil || scanApproved != 1 {
		return errors.New("RRI finalization requires one approved Scan checkpoint")
	}
	if err = tx.QueryRow(`SELECT COUNT(*) FROM work_item_events finalized
		WHERE finalized.work_item_id=? AND finalized.event_type='rri_finalized'
		AND NOT EXISTS (
			SELECT 1 FROM work_item_events reset
			WHERE reset.work_item_id=finalized.work_item_id AND reset.event_type='planning_reset'
			AND datetime(reset.created_at) >= datetime(finalized.created_at)
		)`, args[0]).Scan(&finalized); err != nil {
		return err
	}
	if finalized != 0 {
		var approved int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM workflow_checkpoints WHERE work_item_id=? AND stage='rri' AND decision_type='approved'`, args[0]).Scan(&approved); err != nil {
			return err
		}
		if approved != 0 {
			return errors.New("RRI finalization already approved; reset planning before revising it")
		}
		reportJSON, marshalErr := marshalRriReport(payload.Report)
		if marshalErr != nil {
			return marshalErr
		}
		artifactID, contentHash := "wia-"+shortID(), hashJSON(string(reportJSON))
		if err = tx.QueryRow(`SELECT COALESCE(MAX(revision),0)+1 FROM work_item_artifacts WHERE work_item_id=? AND stage='rri'`, args[0]).Scan(&revision); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES(?,?, 'rri',?,?,?)`, artifactID, args[0], revision, string(reportJSON), contentHash); err != nil {
			return err
		}
		if err = addEvent(tx, args[0], "rri_report_revised", actorRole, "Owner-confirmed RRI report revised before approval", map[string]any{"artifact_id": artifactID}); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM requirements WHERE `+subjectColumn+`=? AND source IN (SELECT id FROM work_item_artifacts WHERE work_item_id=? AND stage='rri')`, args[0], args[0]); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM owner_decisions WHERE `+subjectColumn+`=? AND related_type='rri'`, args[0]); err != nil {
			return err
		}
		// Deferral rows live in work_item_owner_decisions with decision='deferred',
		// a value only this RRI path writes (owner acceptance validates
		// accepted|rejected), so this delete cannot touch review decisions.
		if _, err = tx.Exec(`DELETE FROM work_item_owner_decisions WHERE work_item_id=? AND decision='deferred'`, args[0]); err != nil {
			return err
		}
		inherit := 0
		if subjectColumn == "epic_id" {
			inherit = 1
		}
		for _, requirement := range payload.Requirements {
			query := `INSERT INTO requirements(id,` + subjectColumn + `,requirement_key,inherit_to_descendants,priority,title,description,acceptance_criteria,source) VALUES(?,?,?,?,?,?,?,?,?)`
			if _, err = tx.Exec(query, "req-"+shortID(), args[0], requirement.Key, inherit, requirement.Priority, requirement.Title, requirement.Description, requirement.AcceptanceCriteria, artifactID); err != nil {
				return err
			}
		}
		for _, decision := range payload.Decisions {
			query := `INSERT INTO owner_decisions(id,` + subjectColumn + `,related_type,related_id,decision_type,decision) VALUES(?,?, 'rri',?,?,?)`
			if _, err = tx.Exec(query, "od-"+shortID(), args[0], artifactID, decision.Key, decision.Answer); err != nil {
				return err
			}
		}
		if err = insertRriDeferralDecisions(tx, args[0], artifactID, payload.Report); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		writeJSON(os.Stdout, map[string]any{"artifact_id": artifactID, "work_item_id": args[0], "stage": "rri", "content_hash": contentHash, "requirements": len(payload.Requirements), "decisions": len(payload.Decisions), "revised": true})
		return nil
	}
	if err = tx.QueryRow(`SELECT COALESCE(MAX(revision),0)+1 FROM work_item_artifacts WHERE work_item_id=? AND stage='rri'`, args[0]).Scan(&revision); err != nil {
		return err
	}
	reportJSON, err := marshalRriReport(payload.Report)
	if err != nil {
		return err
	}
	artifactID, contentHash := "wia-"+shortID(), hashJSON(string(reportJSON))
	if _, err = tx.Exec(`INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES(?,?, 'rri',?,?,?)`, artifactID, args[0], revision, string(reportJSON), contentHash); err != nil {
		return err
	}
	inherit := 0
	if subjectColumn == "epic_id" {
		inherit = 1
	}
	for _, requirement := range payload.Requirements {
		query := `INSERT INTO requirements(id,` + subjectColumn + `,requirement_key,inherit_to_descendants,priority,title,description,acceptance_criteria,source) VALUES(?,?,?,?,?,?,?,?,?)`
		if _, err = tx.Exec(query, "req-"+shortID(), args[0], requirement.Key, inherit, requirement.Priority, requirement.Title, requirement.Description, requirement.AcceptanceCriteria, artifactID); err != nil {
			return err
		}
	}
	for _, decision := range payload.Decisions {
		query := `INSERT INTO owner_decisions(id,` + subjectColumn + `,related_type,related_id,decision_type,decision) VALUES(?,?, 'rri',?,?,?)`
		if _, err = tx.Exec(query, "od-"+shortID(), args[0], artifactID, decision.Key, decision.Answer); err != nil {
			return err
		}
	}
	if err = insertRriDeferralDecisions(tx, args[0], artifactID, payload.Report); err != nil {
		return err
	}
	if err = addEvent(tx, args[0], "rri_finalized", actorRole, "Owner-confirmed RRI requirements and decisions persisted", map[string]any{"artifact_id": artifactID, "requirements": len(payload.Requirements), "decisions": len(payload.Decisions)}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	writeJSON(os.Stdout, map[string]any{"artifact_id": artifactID, "work_item_id": args[0], "stage": "rri", "content_hash": contentHash, "requirements": len(payload.Requirements), "decisions": len(payload.Decisions)})
	return nil
}

func workItemArtifactSave(db *sql.DB, args []string) error {
	if len(args) != 3 || !contains(workItemStages, args[1]) || args[2] == "" {
		return errors.New("usage: pic work-item artifact-save <id> <stage> <content>")
	}
	if err := validateChildArtifactStage(os.Getenv("PI_TASK_AGENT_NAME"), args[1]); err != nil {
		return err
	}
	if args[1] == "vision" {
		if err := validateVisionReport(args[2]); err != nil {
			return err
		}
	}
	if args[1] == "blueprint" {
		if err := validateBlueprintReport(args[2]); err != nil {
			return err
		}
	}
	if args[1] == "contracts" {
		if err := validateContractReport(args[2]); err != nil {
			return err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = workItemByIDTx(tx, args[0]); err != nil {
		return err
	}
	if args[1] == "contracts" {
		// Cross-artifact seam binding needs the approved Blueprint, so it runs
		// inside the transaction instead of the pure pre-flight above.
		if err = validateContractPolicyBinding(tx, args[0], args[2]); err != nil {
			return err
		}
	}
	if args[1] == "task_graph" {
		// The Task Graph's own predecessor binding is checked at save so a
		// stale or wrong-lineage source_contract fails immediately; full graph
		// validation stays at validate/approve to preserve the draft flow.
		if err = validateTaskGraphSourceContractBindingJSON(tx, args[0], args[2]); err != nil {
			return err
		}
	}
	if args[1] == "blueprint" {
		// Cross-artifact excluded_keys binding needs the approved RRI referent,
		// so it runs inside the transaction instead of the pure pre-flight above.
		if err = validateBlueprintExcludedKeysBinding(tx, args[0], args[2]); err != nil {
			return err
		}
	}
	var revision int
	if err = tx.QueryRow(`SELECT COALESCE(MAX(revision),0)+1 FROM work_item_artifacts WHERE work_item_id=? AND stage=?`, args[0], args[1]).Scan(&revision); err != nil {
		return err
	}
	id, contentHash := "wia-"+shortID(), hashJSON(args[2])
	if _, err = tx.Exec(`INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES(?,?,?,?,?,?)`, id, args[0], args[1], revision, args[2], contentHash); err != nil {
		return err
	}
	stageIndex := indexOfWorkItemStage(args[1])
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(workItemStages)-stageIndex), ",")
	values := []any{args[0]}
	for _, stage := range workItemStages[stageIndex:] {
		values = append(values, stage)
	}
	if _, err = tx.Exec(`DELETE FROM workflow_checkpoints WHERE work_item_id=? AND stage IN (`+placeholders+`) AND NOT EXISTS (SELECT 1 FROM work_item_instruction_packs WHERE checkpoint_id=workflow_checkpoints.id) AND NOT EXISTS (SELECT 1 FROM work_item_materializations WHERE checkpoint_id=workflow_checkpoints.id) AND NOT EXISTS (SELECT 1 FROM implementation_authorizations WHERE task_graph_checkpoint_id=workflow_checkpoints.id)`, values...); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	writeJSON(os.Stdout, map[string]any{"id": id, "work_item_id": args[0], "stage": args[1], "revision": revision, "content_hash": contentHash})
	return nil
}

func validateChildArtifactStage(agent, stage string) error {
	if agent == "" {
		return nil
	}
	allowed := map[string][]string{
		"task-scout":   {"scan"},
		"task-rri":     {"rri"},
		"task-planner": {"blueprint", "task_graph"},
	}
	if contains(allowed[agent], stage) {
		return nil
	}
	return fmt.Errorf("%s cannot save %s artifacts", agent, stage)
}

type visionReport struct {
	ProjectName string `json:"project_name"`
	Nature      struct {
		Interface string `json:"interface"`
		Lifecycle string `json:"lifecycle"`
		Scale     string `json:"scale"`
	} `json:"nature"`
	Dimensions struct {
		Interface string `json:"interface"`
		DataFlow  string `json:"data_flow"`
		UserModel string `json:"user_model"`
		Lifecycle string `json:"lifecycle"`
		Scale     string `json:"scale"`
		State     string `json:"state"`
	} `json:"dimensions"`
	Architecture struct {
		EntryPoints          []string `json:"entry_points"`
		CoreModules          []string `json:"core_modules"`
		DataLayer            []string `json:"data_layer"`
		IntegrationPoints    []string `json:"integration_points"`
		CrossCuttingConcerns []string `json:"cross_cutting_concerns"`
		ConnectionSummary    string   `json:"connection_summary"`
	} `json:"architecture"`
	UserFlows []struct {
		UserType  string   `json:"user_type"`
		Entry     string   `json:"entry"`
		CoreLoop  string   `json:"core_loop"`
		EdgeCases []string `json:"edge_cases"`
		Exit      string   `json:"exit"`
	} `json:"user_flows"`
	DesignDirection *struct {
		LayoutASCII  string `json:"layout_ascii"`
		FontPairing  string `json:"font_pairing"`
		PrimaryColor string `json:"primary_color"`
		Density      string `json:"density"`
		Motion       string `json:"motion"`
		Rationale    string `json:"rationale"`
	} `json:"design_direction"`
	NonUIDirection *struct {
		Type      string   `json:"type"`
		Decisions []string `json:"decisions"`
	} `json:"non_ui_direction"`
	TechStack []struct {
		Layer     string `json:"layer"`
		Choice    string `json:"choice"`
		Rationale string `json:"rationale"`
		Reuse     string `json:"reuse"`
	} `json:"tech_stack"`
}

func validateVisionReport(content string) error {
	var report visionReport
	if err := json.Unmarshal([]byte(content), &report); err != nil {
		return errors.New("Vision artifact must be valid JSON")
	}
	if report.ProjectName == "" || report.Nature.Interface == "" || report.Nature.Lifecycle == "" || report.Nature.Scale == "" {
		return errors.New("Vision requires project_name and nature")
	}
	if report.Dimensions.Interface == "" || report.Dimensions.DataFlow == "" || report.Dimensions.UserModel == "" || report.Dimensions.Lifecycle == "" || report.Dimensions.Scale == "" || report.Dimensions.State == "" {
		return errors.New("Vision requires all project dimensions")
	}
	if len(report.Architecture.EntryPoints) == 0 || len(report.Architecture.CoreModules) == 0 || len(report.Architecture.DataLayer) == 0 || len(report.Architecture.CrossCuttingConcerns) == 0 || report.Architecture.ConnectionSummary == "" {
		return errors.New("Vision requires complete architecture")
	}
	if len(report.UserFlows) == 0 || len(report.TechStack) == 0 || (report.DesignDirection == nil && report.NonUIDirection == nil) {
		return errors.New("Vision requires user flows, tech stack, and a UI or non-UI direction")
	}
	for _, flow := range report.UserFlows {
		if flow.UserType == "" || flow.Entry == "" || flow.CoreLoop == "" || len(flow.EdgeCases) == 0 || flow.Exit == "" {
			return errors.New("Vision user flows are incomplete")
		}
	}
	for _, stack := range report.TechStack {
		if stack.Layer == "" || stack.Choice == "" || stack.Rationale == "" || stack.Reuse == "" {
			return errors.New("Vision tech stack rows are incomplete")
		}
	}
	if report.NonUIDirection != nil && (report.NonUIDirection.Type == "" || len(report.NonUIDirection.Decisions) == 0) {
		return errors.New("Vision non-UI direction is incomplete")
	}
	return nil
}

// obligationClasses is the primary-class enum for decomposition policy v2
// Contract obligations. Hybrids pick the dominant class.
var obligationClasses = []string{"user_behavior", "data_invariant", "interface_contract", "security", "migration_rule", "operational_rule", "integration_gate"}

func validateContractReport(content string) error {
	var report struct {
		ProjectName  string `json:"project_name"`
		Deliverables []struct {
			Item         string   `json:"item"`
			Details      string   `json:"details"`
			Requirements []string `json:"requirements"`
		} `json:"deliverables"`
		TechStack []any `json:"tech_stack"`
		Summary   struct {
			Tips    int `json:"tip_count"`
			Minutes int `json:"estimated_minutes"`
		} `json:"task_graph_summary"`
		NotIncluded []string                 `json:"not_included"`
		Obligations []tip.ContractObligation `json:"obligations"`
		SourceBlueprint struct {
			ArtifactID  string `json:"artifact_id"`
			Revision    int    `json:"revision"`
			ContentHash string `json:"content_hash"`
		} `json:"source_blueprint"`
		DecompositionPolicyVersion int `json:"decomposition_policy_version"`
	}
	if err := json.Unmarshal([]byte(content), &report); err != nil || report.ProjectName == "" || len(report.Deliverables) == 0 || len(report.TechStack) == 0 || len(report.NotIncluded) == 0 || report.Summary.Tips < 1 || report.Summary.Minutes < 1 {
		return errors.New("Contract artifact must contain valid project, deliverables, stack, task graph summary, and exclusions")
	}
	if report.DecompositionPolicyVersion < 0 || report.DecompositionPolicyVersion > 2 {
		return fmt.Errorf("unsupported decomposition_policy_version %d", report.DecompositionPolicyVersion)
	}
	for _, item := range report.Deliverables {
		if item.Item == "" || item.Details == "" || len(item.Requirements) == 0 {
			return errors.New("Contract deliverables are incomplete")
		}
	}
	if len(report.Obligations) == 0 {
		return errors.New("Contract obligations are required; decompose each non-deferred requirement into atomic behavior with Given/When/Then acceptance")
	}
	seen := map[string]bool{}
	for _, obligation := range report.Obligations {
		if obligation.ID == "" || seen[obligation.ID] || len(obligation.RequirementKeys) == 0 || obligation.Behavior == "" || obligation.Acceptance == "" {
			return errors.New("Contract obligations are incomplete or duplicated")
		}
		seen[obligation.ID] = true
		if err := validateGherkinSteps(obligation.Acceptance); err != nil {
			return fmt.Errorf("Contract obligation %s acceptance %w", obligation.ID, err)
		}
		if report.DecompositionPolicyVersion == 2 {
			if !contains(obligationClasses, obligation.Class) {
				return fmt.Errorf("Contract obligation %s requires a decomposition class from %s", obligation.ID, strings.Join(obligationClasses, "|"))
			}
			if strings.TrimSpace(obligation.Seam) == "" {
				return fmt.Errorf("Contract obligation %s requires a verification seam", obligation.ID)
			}
		}
	}
	if report.DecompositionPolicyVersion == 2 && (report.SourceBlueprint.ArtifactID == "" || report.SourceBlueprint.Revision < 1 || report.SourceBlueprint.ContentHash == "") {
		return errors.New("Contract policy v2 must bind the approved Blueprint artifact id, revision, and content hash in source_blueprint")
	}
	return nil
}

// validateContractPolicyBinding fails a decomposition-policy-v2 Contract closed
// unless it binds the exact approved Blueprint on the same planning lineage and
// every obligation seam references a Blueprint-declared verification seam. It
// runs inside the caller's transaction at both the Contract save and approve
// paths; policy v1 content passes through unchanged.
func validateContractPolicyBinding(db databaseQueryer, workItemID, content string) error {
	var report struct {
		DecompositionPolicyVersion int `json:"decomposition_policy_version"`
		SourceBlueprint            struct {
			ArtifactID  string `json:"artifact_id"`
			Revision    int    `json:"revision"`
			ContentHash string `json:"content_hash"`
		} `json:"source_blueprint"`
		Obligations []tip.ContractObligation `json:"obligations"`
	}
	if err := json.Unmarshal([]byte(content), &report); err != nil {
		return nil // invalid JSON is rejected by validateContractReport
	}
	if report.DecompositionPolicyVersion != 2 {
		return nil
	}
	var artifactID, contentHash, blueprintContent string
	var revision int
	if err := db.QueryRow(`SELECT c.artifact_id,c.artifact_revision,c.content_hash,a.content FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.work_item_id=? AND c.stage='blueprint' AND c.decision_type='approved' ORDER BY c.artifact_revision DESC LIMIT 1`, workItemID).Scan(&artifactID, &revision, &contentHash, &blueprintContent); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("Contract policy v2 requires an approved Blueprint on the same planning lineage")
		}
		return err
	}
	if report.SourceBlueprint.ArtifactID != artifactID || report.SourceBlueprint.Revision != revision || report.SourceBlueprint.ContentHash != contentHash {
		return fmt.Errorf("Contract policy v2 must bind the approved Blueprint %s@%d (%s), got %s@%d (%s)", artifactID, revision, contentHash, report.SourceBlueprint.ArtifactID, report.SourceBlueprint.Revision, report.SourceBlueprint.ContentHash)
	}
	seams, err := blueprintSeamSet(blueprintContent)
	if err != nil {
		return err
	}
	for _, obligation := range report.Obligations {
		if !seams[obligation.Seam] {
			return fmt.Errorf("Contract obligation %s references seam %q which the approved Blueprint does not declare", obligation.ID, obligation.Seam)
		}
	}
	return nil
}

// verificationSeam mirrors the Blueprint verification_seams entries of
// decomposition policy v2. The full schema lives in internal/tip; this local
// shape keeps artifact validation independent of the TIP pack domain.
type verificationSeam struct {
	ID       string `json:"id"`
	Surface  string `json:"surface"`
	Isolates string `json:"isolates"`
	PriorArt string `json:"prior_art"`
}

// blueprintSeamSet parses the declared verification seams of a policy-v2
// Blueprint content blob, including the additive schema_version 2.1 marker.
func blueprintSeamSet(content string) (map[string]bool, error) {
	var blueprint struct {
		DecompositionPolicyVersion float64            `json:"decomposition_policy_version"`
		SchemaVersion              float64            `json:"schema_version"`
		VerificationSeams          []verificationSeam `json:"verification_seams"`
	}
	v21 := blueprint.SchemaVersion == 2.1 && (blueprint.DecompositionPolicyVersion == 0 || blueprint.DecompositionPolicyVersion == 2)
	if err := json.Unmarshal([]byte(content), &blueprint); err != nil || !(blueprint.DecompositionPolicyVersion == 2 || v21) {
		return nil, errors.New("approved Blueprint is not decomposition policy v2 with verification seams")
	}
	seams := map[string]bool{}
	for _, seam := range blueprint.VerificationSeams {
		if seam.ID != "" {
			seams[seam.ID] = true
		}
	}
	if len(seams) == 0 {
		return nil, errors.New("approved Blueprint policy v2 declares no verification seams")
	}
	return seams, nil
}

func validateVerificationSeams(seams []verificationSeam) error {
	if len(seams) == 0 {
		return errors.New("Blueprint policy v2 requires at least one verification seam")
	}
	seen := map[string]bool{}
	for _, seam := range seams {
		if seam.ID == "" || seen[seam.ID] {
			return errors.New("Blueprint verification seams require unique non-empty ids")
		}
		if strings.TrimSpace(seam.Surface) == "" || strings.TrimSpace(seam.Isolates) == "" {
			return fmt.Errorf("Blueprint verification seam %s requires surface and isolates", seam.ID)
		}
		seen[seam.ID] = true
	}
	return nil
}

// validateBlueprintExcludedKeysBinding fails a v2.1 Blueprint closed unless
// every excluded_keys entry matches an exclusion key in the newest approved
// RRI out_of_scope artifact on the same planning lineage. The v2.1 schema is
// the additive schema_version marker on policy v2, not a new policy version.
// It runs inside the caller's transaction at the Blueprint save and approve
// paths; legacy policy v1 and v2 content pass through unchanged.
func validateBlueprintExcludedKeysBinding(db databaseQueryer, workItemID, content string) error {
	var report struct {
		DecompositionPolicyVersion float64  `json:"decomposition_policy_version"`
		SchemaVersion              float64  `json:"schema_version"`
		ExcludedKeys               []string `json:"excluded_keys"`
	}
	if err := json.Unmarshal([]byte(content), &report); err != nil {
		return nil // invalid JSON is rejected by validateBlueprintReport
	}
	v21 := report.SchemaVersion == 2.1 && (report.DecompositionPolicyVersion == 0 || report.DecompositionPolicyVersion == 2)
	if !v21 || len(report.ExcludedKeys) == 0 {
		return nil
	}
	var rriContent string
	if err := db.QueryRow(`SELECT a.content FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.work_item_id=? AND c.stage='rri' AND c.decision_type='approved' ORDER BY c.artifact_revision DESC LIMIT 1`, workItemID).Scan(&rriContent); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("Blueprint policy v2.1 requires an approved RRI out_of_scope referent for excluded_keys")
		}
		return err
	}
	var rri struct {
		OutOfScope []struct {
			Exclusion string `json:"exclusion"`
		} `json:"out_of_scope"`
	}
	if err := json.Unmarshal([]byte(rriContent), &rri); err != nil {
		return fmt.Errorf("approved RRI out_of_scope referent is unreadable: %w", err)
	}
	keys := map[string]bool{}
	for _, row := range rri.OutOfScope {
		keys[row.Exclusion] = true
	}
	for _, key := range report.ExcludedKeys {
		if !keys[key] {
			return fmt.Errorf("Blueprint excluded key %q is not declared in the approved RRI out_of_scope", key)
		}
	}
	return nil
}

func validateBlueprintReport(content string) error {
	var report struct {
		ProjectInfo   map[string]any `json:"project_info"`
		Goals         map[string]any `json:"goals"`
		Architecture  map[string]any `json:"architecture"`
		TechStack     []any          `json:"tech_stack"`
		FileStructure []any          `json:"file_structure"`
		Requirements  []any          `json:"rri_requirements_matrix"`
		Preview       struct {
			EstimatedTasks  int   `json:"estimated_tasks"`
			Tasks           []any `json:"tasks"`
			EstimatedEffort int   `json:"estimated_effort_minutes"`
		} `json:"task_decomposition_preview"`
		DecompositionPolicyVersion int                `json:"decomposition_policy_version"`
		SchemaVersion              float64            `json:"schema_version"`
		VerificationSeams          []verificationSeam `json:"verification_seams"`
		ExcludedKeys               []string           `json:"excluded_keys"`
	}
	if err := json.Unmarshal([]byte(content), &report); err != nil {
		return errors.New("Blueprint artifact must be valid JSON")
	}
	if report.DecompositionPolicyVersion < 0 || report.DecompositionPolicyVersion > 2 {
		return fmt.Errorf("unsupported decomposition_policy_version %d", report.DecompositionPolicyVersion)
	}
	if len(report.ProjectInfo) == 0 || len(report.Goals) == 0 || len(report.Architecture) == 0 {
		return errors.New("Blueprint requires project_info, goals, and architecture")
	}
	if len(report.TechStack) == 0 || len(report.FileStructure) == 0 || len(report.Requirements) == 0 {
		return errors.New("Blueprint requires complete stack, file structure, and RRI matrix")
	}
	if report.DecompositionPolicyVersion == 2 {
		// Policy v2 is the solution spec plus owner-approved verification seams;
		// the task_decomposition_preview is retired (Contract's task_graph_summary
		// keeps the early cost signal).
		if report.SchemaVersion == 2.1 {
			for _, key := range report.ExcludedKeys {
				if strings.TrimSpace(key) == "" {
					return errors.New("Blueprint policy v2.1 excluded_keys require non-empty keys")
				}
			}
		}
		return validateVerificationSeams(report.VerificationSeams)
	}
	if len(report.Preview.Tasks) == 0 || report.Preview.EstimatedTasks != len(report.Preview.Tasks) || report.Preview.EstimatedEffort < 1 {
		return errors.New("Blueprint requires complete stack, file structure, RRI matrix, and task preview")
	}
	return nil
}

// approvedCheckpointDecision names the owner decision that makes a checkpoint
// the planning authority for a stage: Scan is owner-accepted, every other
// planning stage is owner-approved. Authority-selection queries must filter on
// this decision so a newer rejected checkpoint can never supply seams,
// obligations, lineage, or predecessor clearance — the newest APPROVED
// checkpoint of the stage wins.
func approvedCheckpointDecision(stage string) string {
	if stage == "scan" {
		return "accepted"
	}
	return "approved"
}

func workItemPlanningReset(db *sql.DB, args []string) error {
	if len(args) < 2 || len(args) > 3 || args[1] != "owner" {
		return errors.New("usage: pic work-item planning-reset <id> owner [--dry-run]")
	}
	dryRun := len(args) == 3 && args[2] == "--dry-run"
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	targetID := args[0]
	if err = tx.QueryRow(`SELECT root_work_item_id FROM work_item_materializations WHERE work_item_id=?`, args[0]).Scan(&targetID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	item, err := workItemByIDTx(tx, targetID)
	if err != nil {
		return err
	}
	if item["status"] == "cancelled" || item["status"] == "done" {
		return errors.New("planning reset requires a non-terminal Work Item")
	}
	var count, materialized int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM work_item_materializations WHERE root_work_item_id=?`, targetID).Scan(&materialized); err != nil {
		return err
	}
	if err = tx.QueryRow(`WITH RECURSIVE execution(id) AS (
		SELECT ? UNION ALL SELECT wi.id FROM work_items wi JOIN execution parent ON wi.parent_id=parent.id
	) SELECT COUNT(*) FROM pipeline_runs WHERE task_id IN (SELECT id FROM execution) AND status IN ('claimed','running')`, targetID).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return errors.New("planning reset requires no active pipeline runs")
	}
	var artifacts, runs int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM work_item_artifacts WHERE work_item_id=?`, targetID).Scan(&artifacts); err != nil {
		return err
	}
	if err = tx.QueryRow(`SELECT COUNT(*) FROM pipeline_runs WHERE task_id=?`, targetID).Scan(&runs); err != nil {
		return err
	}
	var checkpoints int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM workflow_checkpoints WHERE work_item_id=?`, targetID).Scan(&checkpoints); err != nil {
		return err
	}
	if dryRun {
		// Invalidation blast-radius preview: name the exact targets a reset
		// would retire, using the same lineage the DELETE statements below
		// operate on, without mutating any persisted state.
		artifactEntries := []map[string]any{}
		artifactRows, err := tx.Query(`SELECT stage,id,revision,content_hash FROM work_item_artifacts WHERE work_item_id=? ORDER BY stage,revision`, targetID)
		if err != nil {
			return err
		}
		for artifactRows.Next() {
			var stage, artifactID, contentHash string
			var revision int
			if err := artifactRows.Scan(&stage, &artifactID, &revision, &contentHash); err != nil {
				artifactRows.Close()
				return err
			}
			artifactEntries = append(artifactEntries, map[string]any{"stage": stage, "id": artifactID, "revision": revision, "content_hash": contentHash})
		}
		if err := artifactRows.Err(); err != nil {
			artifactRows.Close()
			return err
		}
		artifactRows.Close()
		checkpointEntries := []map[string]any{}
		checkpointRows, err := tx.Query(`SELECT id,stage,artifact_id,artifact_revision FROM workflow_checkpoints WHERE work_item_id=? ORDER BY stage`, targetID)
		if err != nil {
			return err
		}
		for checkpointRows.Next() {
			var checkpointID, stage, artifactID string
			var revision int
			if err := checkpointRows.Scan(&checkpointID, &stage, &artifactID, &revision); err != nil {
				checkpointRows.Close()
				return err
			}
			checkpointEntries = append(checkpointEntries, map[string]any{"id": checkpointID, "stage": stage, "artifact_id": artifactID, "artifact_revision": revision})
		}
		if err := checkpointRows.Err(); err != nil {
			checkpointRows.Close()
			return err
		}
		checkpointRows.Close()
		packEntries := []map[string]any{}
		packRows, err := tx.Query(`SELECT id,version,content_hash,status FROM work_item_instruction_packs WHERE work_item_id=? ORDER BY version`, targetID)
		if err != nil {
			return err
		}
		for packRows.Next() {
			var packID, contentHash, status string
			var version int
			if err := packRows.Scan(&packID, &version, &contentHash, &status); err != nil {
				packRows.Close()
				return err
			}
			packEntries = append(packEntries, map[string]any{"id": packID, "version": version, "content_hash": contentHash, "status": status})
		}
		if err := packRows.Err(); err != nil {
			packRows.Close()
			return err
		}
		packRows.Close()
		dependentEntries := []map[string]any{}
		dependentRows, err := tx.Query(`SELECT work_item_id,node_key FROM work_item_materializations WHERE root_work_item_id=? AND work_item_id<>? ORDER BY node_key`, targetID, targetID)
		if err != nil {
			return err
		}
		for dependentRows.Next() {
			var childID, nodeKey string
			if err := dependentRows.Scan(&childID, &nodeKey); err != nil {
				dependentRows.Close()
				return err
			}
			dependentEntries = append(dependentEntries, map[string]any{"work_item_id": childID, "node_key": nodeKey})
		}
		if err := dependentRows.Err(); err != nil {
			dependentRows.Close()
			return err
		}
		dependentRows.Close()
		// The DELETE below cascades: every record a retired child Work Item owns
		// is retired with it, so the preview enumerates every descendant-owned
		// cascade target explicitly — the planning lineage the reset exists to
		// invalidate plus every other child-owned table. Each spec's ownerColumn
		// is the table's FK into work_items: rows whose owner is a materialized
		// descendant are exactly the rows the DELETE retires. Corrective bugs are
		// handled separately below because they are second-order targets.
		descendantPreviews := []struct {
			key, table, ownerColumn, ownerKey, order string
			fields                                   []string
		}{
			{"descendant_artifacts", "work_item_artifacts", "work_item_id", "work_item_id", " ORDER BY stage", []string{"stage", "id", "revision", "content_hash"}},
			{"descendant_checkpoints", "workflow_checkpoints", "work_item_id", "work_item_id", " ORDER BY stage", []string{"id", "stage", "artifact_id", "artifact_revision"}},
			{"descendant_instruction_packs", "work_item_instruction_packs", "work_item_id", "work_item_id", " ORDER BY version", []string{"id", "version", "content_hash", "status"}},
			{"descendant_completion_reports", "work_item_completion_reports", "work_item_id", "work_item_id", "", []string{"id", "status", "pipeline_run_id"}},
			{"descendant_verification_reports", "work_item_verification_reports", "work_item_id", "work_item_id", "", []string{"id", "status", "completion_report_id"}},
			{"descendant_pipeline_runs", "pipeline_runs", "task_id", "work_item_id", " ORDER BY stage,attempt", []string{"id", "stage", "attempt", "status"}},
			{"descendant_labels", "work_item_labels", "work_item_id", "work_item_id", " ORDER BY label", []string{"label"}},
			{"descendant_dependencies", "work_item_dependencies", "work_item_id", "work_item_id", " ORDER BY id", []string{"id", "depends_on_work_item_id"}},
			{"descendant_gates", "work_item_gates", "work_item_id", "work_item_id", " ORDER BY id", []string{"id", "gate_work_item_id"}},
			{"descendant_relations", "work_item_relations", "work_item_id", "work_item_id", " ORDER BY id", []string{"id", "relation_type", "related_work_item_id"}},
			{"descendant_authorizations", "implementation_authorizations", "work_item_id", "work_item_id", " ORDER BY id", []string{"id", "task_graph_checkpoint_id", "authorized_by"}},
			{"descendant_escalations", "work_item_escalations", "work_item_id", "work_item_id", " ORDER BY id", []string{"id", "level", "status", "pipeline_run_id"}},
			{"descendant_owner_decisions", "work_item_owner_decisions", "work_item_id", "work_item_id", " ORDER BY id", []string{"id", "decision", "completion_report_id"}},
			{"descendant_aggregate_decisions", "work_item_aggregate_owner_decisions", "work_item_id", "work_item_id", " ORDER BY id", []string{"id", "decision", "verification_report_id"}},
			{"descendant_delivery_states", "work_item_delivery_states", "work_item_id", "work_item_id", " ORDER BY work_item_id", []string{"integration_mode", "branch_name", "merge_status"}},
			{"descendant_events", "work_item_events", "work_item_id", "work_item_id", " ORDER BY id", []string{"id", "event_type", "summary"}},
			{"descendant_profiles", "work_item_profiles", "work_item_id", "work_item_id", " ORDER BY profile_name", []string{"profile_name"}},
		}
		descendantRecords := map[string]any{}
		for _, preview := range descendantPreviews {
			entries, err := dryRunCascadeRows(tx, preview.table, preview.ownerColumn, preview.ownerKey, preview.fields, preview.order, targetID)
			if err != nil {
				return err
			}
			descendantRecords[preview.key] = entries
		}
		// Materialization rows are retired twice over: the reset's own cleanup
		// deletes every row rooted at the target — including the target's own
		// node, which the dependent list above excludes — and any row rooted at
		// a materialized descendant (a nested materialization root) dies with
		// that descendant through the work_items cascade. The preview
		// enumerates both sets exactly.
		retiredMaterializationRows := []map[string]any{}
		materializationRows, err := tx.Query(`SELECT root_work_item_id,checkpoint_id,node_key,work_item_id FROM work_item_materializations
			WHERE root_work_item_id=?
			OR root_work_item_id IN (SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND work_item_id<>?)
			ORDER BY node_key`, targetID, targetID, targetID)
		if err != nil {
			return err
		}
		for materializationRows.Next() {
			var rootID, checkpointID, nodeKey, nodeID string
			if err := materializationRows.Scan(&rootID, &checkpointID, &nodeKey, &nodeID); err != nil {
				materializationRows.Close()
				return err
			}
			retiredMaterializationRows = append(retiredMaterializationRows, map[string]any{"root_work_item_id": rootID, "checkpoint_id": checkpointID, "node_key": nodeKey, "work_item_id": nodeID})
		}
		if err := materializationRows.Err(); err != nil {
			materializationRows.Close()
			return err
		}
		materializationRows.Close()
		// Corrective bugs are second-order cascade targets: the row dies with its
		// verification report (or with the bug Work Item itself, if that bug is a
		// materialized descendant). The bug Work Item itself survives a reset
		// unless it is a descendant, so the preview names it rather than implying
		// it is retired.
		descendantCorrectiveBugs := []map[string]any{}
		correctiveRows, err := tx.Query(`SELECT verification_report_id,owner_approval_required,bug_work_item_id FROM work_item_corrective_bugs
			WHERE bug_work_item_id IN (SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND work_item_id<>?)
			OR verification_report_id IN (SELECT id FROM work_item_verification_reports WHERE work_item_id IN (SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND work_item_id<>?))`,
			targetID, targetID, targetID, targetID)
		if err != nil {
			return err
		}
		for correctiveRows.Next() {
			var reportID, bugID string
			var ownerApproval int
			if err := correctiveRows.Scan(&reportID, &ownerApproval, &bugID); err != nil {
				correctiveRows.Close()
				return err
			}
			descendantCorrectiveBugs = append(descendantCorrectiveBugs, map[string]any{"verification_report_id": reportID, "owner_approval_required": ownerApproval, "bug_work_item_id": bugID})
		}
		if err := correctiveRows.Err(); err != nil {
			correctiveRows.Close()
			return err
		}
		correctiveRows.Close()
		output := map[string]any{"work_item_id": targetID, "dry_run": true, "artifacts": artifactEntries, "checkpoints_list": checkpointEntries, "instruction_packs": packEntries, "dependents": dependentEntries, "retired_materialization_rows": retiredMaterializationRows, "descendant_corrective_bugs": descendantCorrectiveBugs, "artifacts_count": artifacts, "pipeline_runs": runs, "retired_materializations": materialized, "checkpoints": checkpoints, "next_stage_after_reset": "scan"}
		for key, entries := range descendantRecords {
			output[key] = entries
		}
		writeJSON(os.Stdout, output)
		return nil
	}
	if materialized > 0 {
		// Owner re-scope transition: implemented code staled the Scan, so every derived
		// artifact is stale too; retire the authorized DAG and the entire planning lineage.
		if _, err = tx.Exec(`DELETE FROM work_items WHERE id IN (SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND work_item_id<>?)`, targetID, targetID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM implementation_authorizations WHERE work_item_id=?`, targetID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM work_item_materializations WHERE root_work_item_id=?`, targetID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM workflow_checkpoints WHERE work_item_id=?`, targetID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM work_item_artifacts WHERE work_item_id=?`, targetID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM requirements WHERE epic_id=? OR task_id=?`, targetID, targetID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM owner_decisions WHERE epic_id=? OR task_id=?`, targetID, targetID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM pipeline_runs WHERE task_id=?`, targetID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM work_item_delivery_states WHERE work_item_id=?`, targetID); err != nil {
			return err
		}
	} else {
		// Owner re-scope transition: invalidate the entire stale planning lineage so Scan can run again.
		if _, err = tx.Exec(`DELETE FROM workflow_checkpoints WHERE work_item_id=?`, targetID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM work_item_artifacts WHERE work_item_id=?`, targetID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM requirements WHERE epic_id=? OR task_id=?`, targetID, targetID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM owner_decisions WHERE epic_id=? OR task_id=?`, targetID, targetID); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM pipeline_runs WHERE task_id=?`, targetID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`UPDATE work_items SET status='open',claimed_at='',claimed_by='',review_status='pending',review_notes='' WHERE id=?`, targetID); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]int{"artifacts": artifacts, "pipeline_runs": runs, "retired_materializations": materialized})
	summary := "Owner invalidated stale planning lineage and reset Scan for re-scope"
	if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,summary,payload_json) VALUES(?,?,?,?,?,?)`, "wie-"+shortID(), targetID, "planning_reset", "owner", summary, string(payload)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return outputOne(db, `SELECT `+workItemColumns+` FROM work_items WHERE id=?`, targetID)
}

// Owner-only bounded amendment: exact old→new substitutions across approved planning
// lineage. Resolves the dual-source-of-truth hazard of injecting corrected values
// (e.g. a port change) while frozen artifacts still state the old value, without a full re-scope.


// dryRunCascadeRows previews the exact rows one child-owned table contributes
// to a planning reset: every row whose ownerColumn holds a materialized
// descendant of the target is retired by the DELETE's foreign-key cascade. Each
// previewed row carries ownerKey so it names the Work Item that owned it.
func dryRunCascadeRows(db *sql.Tx, table, ownerColumn, ownerKey string, fields []string, order, targetID string) ([]map[string]any, error) {
	quoted := make([]string, 0, len(fields)+1)
	for _, field := range fields {
		quoted = append(quoted, `"`+strings.ReplaceAll(field, `"`, `""`)+`"`)
	}
	owner := `"` + strings.ReplaceAll(ownerColumn, `"`, `""`) + `"`
	quoted = append(quoted, owner)
	query := `SELECT ` + strings.Join(quoted, ",") + ` FROM "` + strings.ReplaceAll(table, `"`, `""`) + `" WHERE ` + owner +
		` IN (SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND work_item_id<>?)` + order
	rows, err := db.Query(query, targetID, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(fields)+1)
		destinations := make([]any, len(fields)+1)
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}
		entry := map[string]any{}
		for i, field := range fields {
			entry[field] = dryRunValue(values[i])
		}
		entry[ownerKey] = dryRunValue(values[len(fields)])
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func dryRunValue(value any) any {
	if raw, ok := value.([]byte); ok {
		return string(raw)
	}
	return value
}

func workItemPlanningAmend(db *sql.DB, args []string) error {
	if len(args) != 3 || args[1] != "owner" {
		return errors.New("usage: pic work-item planning-amend <id> owner <amendment-json>")
	}
	var payload struct {
		Reason        string `json:"reason"`
		Substitutions []struct {
			Old string `json:"old"`
			New string `json:"new"`
		} `json:"substitutions"`
	}
	if err := json.Unmarshal([]byte(args[2]), &payload); err != nil {
		return fmt.Errorf("invalid amendment JSON: %w", err)
	}
	if strings.TrimSpace(payload.Reason) == "" || len(payload.Substitutions) == 0 {
		return errors.New("amendment requires a reason and at least one old/new substitution")
	}
	for _, sub := range payload.Substitutions {
		if sub.Old == "" || sub.Old == sub.New {
			return errors.New("each substitution requires nonempty old different from new")
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	targetID := args[0]
	if err = tx.QueryRow(`SELECT root_work_item_id FROM work_item_materializations WHERE work_item_id=?`, args[0]).Scan(&targetID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	item, err := workItemByIDTx(tx, targetID)
	if err != nil {
		return err
	}
	if item["status"] == "cancelled" || item["status"] == "done" {
		return errors.New("planning amendment requires a non-terminal Work Item")
	}
	descendantCTE := `WITH RECURSIVE execution(id) AS (
		SELECT ? UNION ALL SELECT wi.id FROM work_items wi JOIN execution parent ON wi.parent_id=parent.id
	)`
	var activeRuns int
	if err = tx.QueryRow(descendantCTE+` SELECT COUNT(*) FROM pipeline_runs WHERE task_id IN (SELECT id FROM execution) AND status IN ('claimed','running')`, targetID).Scan(&activeRuns); err != nil {
		return err
	}
	if activeRuns != 0 {
		return errors.New("planning amendment requires no active pipeline runs")
	}
	// Scoped evidence invalidation (GAP-139): verification evidence whose own surface
	// text intersects a substitution loses its authority (retired as blocked history);
	// non-intersecting evidence survives so bounded amendments stay usable after
	// children close instead of collapsing into a full planning reset.
	type evidenceRow struct{ id, text string }
	evidenceRows, err := tx.Query(descendantCTE+` SELECT v.id, COALESCE(v.summary,'')||char(10)||COALESCE(v.rri_t_json,'')||char(10)||COALESCE(c.summary,'')||char(10)||COALESCE(c.report_markdown,'') FROM work_item_verification_reports v LEFT JOIN work_item_completion_reports c ON c.id=v.completion_report_id WHERE v.work_item_id IN (SELECT id FROM execution)`, targetID)
	if err != nil {
		return err
	}
	var affectedEvidence []evidenceRow
	for evidenceRows.Next() {
		var row evidenceRow
		if err = evidenceRows.Scan(&row.id, &row.text); err != nil {
			evidenceRows.Close()
			return err
		}
		for _, sub := range payload.Substitutions {
			if strings.Contains(row.text, sub.Old) {
				affectedEvidence = append(affectedEvidence, row)
				break
			}
		}
	}
	evidenceRows.Close()
	for _, row := range affectedEvidence {
		marker := " [superseded by planning amendment " + shortID() + ": verified content contains an amended value]"
		if _, err = tx.Exec(`UPDATE work_item_verification_reports SET status='blocked', summary=summary||? WHERE id=? AND status='passed'`, marker, row.id); err != nil {
			return err
		}
	}
	for _, sub := range payload.Substitutions {
		var keyed []string
		rows, err := tx.Query(`SELECT requirement_key FROM requirements WHERE (epic_id=? OR task_id=?) AND contract_key!='' AND (instr(title,?)>0 OR instr(description,?)>0 OR instr(acceptance_criteria,?)>0)`, targetID, targetID, sub.Old, sub.Old, sub.Old)
		if err != nil {
			return err
		}
		for rows.Next() {
			var key string
			if err = rows.Scan(&key); err != nil {
				rows.Close()
				return err
			}
			keyed = append(keyed, key)
		}
		rows.Close()
		if len(keyed) > 0 {
			return fmt.Errorf("contract-keyed requirements %v are immutable and contain %q; replan instead", keyed, sub.Old)
		}
	}
	type stageChange struct {
		Stage        string `json:"stage"`
		ArtifactID   string `json:"artifact_id"`
		FromRevision int    `json:"from_revision"`
		ToRevision   int    `json:"to_revision"`
	}
	var changedStages []stageChange
	checkpoints, err := tx.Query(`SELECT c.id,c.stage,a.id,a.revision,a.content FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.work_item_id=? ORDER BY c.rowid`, targetID)
	if err != nil {
		return err
	}
	type checkpointBinding struct {
		id, stage, artifactID, content string
		revision                       int
	}
	var bindings []checkpointBinding
	for checkpoints.Next() {
		var b checkpointBinding
		if err = checkpoints.Scan(&b.id, &b.stage, &b.artifactID, &b.revision, &b.content); err != nil {
			checkpoints.Close()
			return err
		}
		bindings = append(bindings, b)
	}
	checkpoints.Close()
	for _, b := range bindings {
		updated := b.content
		for _, sub := range payload.Substitutions {
			updated = strings.ReplaceAll(updated, sub.Old, sub.New)
		}
		if updated == b.content {
			continue
		}
		var nextRevision int
		if err = tx.QueryRow(`SELECT COALESCE(MAX(revision),0)+1 FROM work_item_artifacts WHERE work_item_id=? AND stage=?`, targetID, b.stage).Scan(&nextRevision); err != nil {
			return err
		}
		newID, newHash := "wia-"+shortID(), hashJSON(updated)
		if _, err = tx.Exec(`INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES(?,?,?,?,?,?)`, newID, targetID, b.stage, nextRevision, updated, newHash); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE workflow_checkpoints SET artifact_id=?,artifact_revision=?,content_hash=? WHERE id=?`, newID, nextRevision, newHash, b.id); err != nil {
			return err
		}
		changedStages = append(changedStages, stageChange{Stage: b.stage, ArtifactID: newID, FromRevision: b.revision, ToRevision: nextRevision})
	}
	applyToColumns := func(table, idColumn string, columns []string) (int, error) {
		query := `SELECT id,` + strings.Join(columns, ",") + ` FROM ` + table + ` WHERE epic_id=? OR task_id=?`
		rows, err := tx.Query(query, targetID, targetID)
		if err != nil {
			return 0, err
		}
		defer rows.Close()
		changed := 0
		for rows.Next() {
			vals := make([]any, len(columns))
			ptrs := make([]any, len(columns))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			var rowID string
			dest := append([]any{&rowID}, ptrs...)
			if err = rows.Scan(dest...); err != nil {
				return 0, err
			}
			updates := map[string]string{}
			rowChanged := false
			for i, col := range columns {
				text, _ := vals[i].(string)
				updatedText := text
				for _, sub := range payload.Substitutions {
					updatedText = strings.ReplaceAll(updatedText, sub.Old, sub.New)
				}
				if updatedText != text {
					updates[col] = updatedText
					rowChanged = true
				}
			}
			if rowChanged {
				sets, args := "", []any{}
				for col, val := range updates {
					if sets != "" {
						sets += ","
					}
					sets += col + "=?"
					args = append(args, val)
				}
				args = append(args, rowID)
				if _, err = tx.Exec(`UPDATE `+table+` SET `+sets+` WHERE id=?`, args...); err != nil {
					return 0, err
				}
				changed++
			}
		}
		return changed, rows.Err()
	}
	requirementsChanged, err := applyToColumns("requirements", "", []string{"title", "description", "acceptance_criteria"})
	if err != nil {
		return err
	}
	decisionsChanged, err := applyToColumns("owner_decisions", "", []string{"decision", "notes"})
	if err != nil {
		return err
	}
	if len(changedStages) == 0 && requirementsChanged == 0 && decisionsChanged == 0 {
		return errors.New("no occurrence of any substitution target found in approved planning lineage")
	}
	packIDs := []any{}
	selectArgs := []any{targetID}
	packQuery := descendantCTE + ` SELECT DISTINCT p.id FROM work_item_instruction_packs p WHERE p.work_item_id IN (SELECT id FROM execution) AND p.status IN ('active','inactive') AND (`
	conditions := []string{}
	for _, sub := range payload.Substitutions {
		conditions = append(conditions, "instr(p.content_json,?)>0")
		selectArgs = append(selectArgs, sub.Old)
	}
	packQuery += strings.Join(conditions, " OR ") + ")"
	packRows, err := tx.Query(packQuery, selectArgs...)
	if err != nil {
		return err
	}
	for packRows.Next() {
		var packID string
		if err = packRows.Scan(&packID); err != nil {
			packRows.Close()
			return err
		}
		packIDs = append(packIDs, packID)
	}
	packRows.Close()
	packsStaled := 0
	if len(packIDs) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(packIDs)), ",")
		res, err := tx.Exec(`UPDATE work_item_instruction_packs SET status='stale',stale_at=datetime('now') WHERE status IN ('active','inactive') AND id IN (`+placeholders+`)`, packIDs...)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		packsStaled = int(affected)
	}
	substitutionPayload, _ := json.Marshal(payload.Substitutions)
	eventPayload, _ := json.Marshal(map[string]any{
		"reason":               payload.Reason,
		"substitutions":        json.RawMessage(substitutionPayload),
		"changed_stages":       changedStages,
		"requirements_changed": requirementsChanged,
		"decisions_changed":    decisionsChanged,
		"packs_staled":         packsStaled,
		"evidence_retired":     len(affectedEvidence),
	})
	summary := fmt.Sprintf("Owner amended planning lineage: %d artifact stages superseded, %d requirements and %d decisions substituted, %d instruction packs staled, %d verification reports retired", len(changedStages), requirementsChanged, decisionsChanged, packsStaled, len(affectedEvidence))
	if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,summary,payload_json) VALUES(?,?,?,?,?,?)`, "wie-"+shortID(), targetID, "planning_amendment", "owner", summary, string(eventPayload)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	writeJSON(os.Stdout, map[string]any{"work_item_id": targetID, "reason": payload.Reason, "changed_stages": changedStages, "requirements_changed": requirementsChanged, "decisions_changed": decisionsChanged, "packs_staled": packsStaled})
	return nil
}

func workItemExecutionReset(db *sql.DB, args []string) error {
	if len(args) != 2 || args[1] != "owner" {
		return errors.New("usage: pic work-item execution-reset <id> owner")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	item, err := workItemByIDTx(tx, args[0])
	if err != nil {
		return err
	}
	if item["status"] == "cancelled" || item["status"] == "done" {
		return errors.New("execution reset requires a non-terminal Work Item")
	}
	var rootID, checkpointID string
	if err = tx.QueryRow(`SELECT root_work_item_id,checkpoint_id FROM work_item_materializations WHERE work_item_id=? ORDER BY rowid DESC LIMIT 1`, args[0]).Scan(&rootID, &checkpointID); err != nil {
		return errors.New("execution reset requires a materialized child Work Item")
	}
	var active int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM pipeline_runs WHERE task_id=? AND status IN ('claimed','running')`, args[0]).Scan(&active); err != nil {
		return err
	}
	if active != 0 {
		return errors.New("execution reset requires no active pipeline runs")
	}
	if _, err = tx.Exec(`UPDATE implementation_authorizations SET revoked_at=datetime('now') WHERE work_item_id=? AND revoked_at=''`, args[0]); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE work_item_instruction_packs SET status='stale',stale_at=datetime('now') WHERE work_item_id=? AND status='active'`, args[0]); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE work_items SET status='open',claimed_at='',claimed_by='',review_status='pending',review_notes='' WHERE id=?`, args[0]); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"root_work_item_id": rootID, "task_graph_checkpoint_id": checkpointID})
	if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,summary,payload_json) VALUES(?,?,?,?,?,?)`, "wie-"+shortID(), args[0], "execution_reset", "owner", "Owner reset this child execution binding; Task Graph and sibling Work Items preserved", string(payload)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return outputOne(db, `SELECT `+workItemColumns+` FROM work_items WHERE id=?`, args[0])
}

func workItemScanReject(db *sql.DB, args []string) error {
	if len(args) != 3 || args[1] != "contractor" || strings.TrimSpace(args[2]) == "" {
		return errors.New("usage: pic work-item scan-reject <id> contractor <reason>")
	}
	if _, err := workItemByID(db, args[0]); err != nil {
		return err
	}
	id, reason := "wie-"+shortID(), strings.TrimSpace(args[2])
	if _, err := db.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,summary,payload_json) VALUES(?,?,?,?,?,?)`, id, args[0], "scan_report_rejected", args[1], reason, "{}"); err != nil {
		return err
	}
	writeJSON(os.Stdout, map[string]any{"id": id, "work_item_id": args[0], "rejected": true, "reason": reason})
	return nil
}

func workItemScanRejection(db *sql.DB, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: pic work-item scan-rejection <id>")
	}
	rows, err := queryMaps(db, `SELECT event_type,id,summary,created_at FROM work_item_events WHERE work_item_id=? AND event_type IN ('scan_report_rejected','planning_reset') ORDER BY created_at DESC,id DESC LIMIT 1`, args[0])
	if err != nil {
		return err
	}
	if len(rows) == 0 || rows[0]["event_type"] != "scan_report_rejected" {
		writeJSON(os.Stdout, map[string]any{"rejected": false})
		return nil
	}
	writeJSON(os.Stdout, map[string]any{"rejected": true, "id": rows[0]["id"], "reason": rows[0]["summary"], "created_at": rows[0]["created_at"]})
	return nil
}

func workItemArtifactApprove(db *sql.DB, args []string) error {
	if len(args) != 4 || !contains(workItemStages, args[1]) {
		return errors.New("usage: pic work-item artifact-approve <id> <stage> <artifact-id> <accepted|approved>")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	artifactID, revision, contentHash, err := approveWorkItemArtifactTx(tx, args[0], args[1], args[2], args[3])
	if err != nil {
		return err
	}
	// Glossary constraint (REQ-F1-6): the CONTEXT.md update attaches only to
	// this owner approval mutation, never to interview checkpointing or
	// publication; a write failure fails the approval transaction so lifecycle
	// state and repository truth stay aligned.
	glossaryUpdated, restoreGlossary, err := applyRriGlossaryApproval(tx, args[0], args[1], artifactID)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		if restoreGlossary != nil {
			// A compensation failure means the rolled-back lifecycle state and
			// CONTEXT.md have diverged, so it must be reported alongside the
			// commit error rather than swallowed.
			if restoreErr := restoreGlossary(); restoreErr != nil {
				return fmt.Errorf("%w (approval rolled back, but restoring the pre-write CONTEXT.md also failed: %v)", err, restoreErr)
			}
		}
		return err
	}
	writeJSON(os.Stdout, map[string]any{"work_item_id": args[0], "stage": args[1], "artifact_id": artifactID, "revision": revision, "content_hash": contentHash, "decision": args[3], "glossary_updated": glossaryUpdated})
	return nil
}

// applyRriGlossaryApproval (REQ-F1-6) is the sole CONTEXT.md glossary writer:
// it runs only when an owner-approved RRI checkpoint is recorded and applies
// the approved artifact's explicitly identified glossary_updates with an
// atomic temp-file rename so unrelated content is preserved. The target is
// resolved through findRriTruthRoot so an approval run from a nested working
// directory updates the same canonical repository glossary the terminology
// guard reads instead of shadowing it. Artifacts without a glossary_updates
// section skip the write but still approve; artifacts whose section fails
// validation are rejected. The returned restore closure
// compensates a commit failure by putting back the exact pre-write content
// and surfaces its own failure when repository truth cannot be restored.
func applyRriGlossaryApproval(tx *sql.Tx, workItemID, stage, artifactID string) (bool, func() error, error) {
	if stage != "rri" {
		return false, nil, nil
	}
	var content string
	if err := tx.QueryRow(`SELECT content FROM work_item_artifacts WHERE id=? AND work_item_id=? AND stage='rri'`, artifactID, workItemID).Scan(&content); err != nil {
		return false, nil, err
	}
	updates, err := rriArtifactGlossaryUpdates(content)
	if err != nil {
		// An artifact that carries a glossary_updates section but fails
		// validation must not slip through approval silently: reject the
		// approval instead of skipping the write.
		return false, nil, fmt.Errorf("invalid glossary_updates in approved RRI artifact: %w", err)
	}
	if len(updates) == 0 {
		// Prose interview checkpoints and artifacts without a glossary_updates
		// section have no update to apply: the approval proceeds with
		// repository truth untouched.
		return false, nil, nil
	}
	truthRoot, err := findRriTruthRoot()
	if err != nil {
		return false, nil, err
	}
	if truthRoot == "" {
		// Fail closed: without a discovered truth root there is no canonical
		// glossary to update, and writing next to the working directory would
		// shadow repository truth instead of updating it.
		return false, nil, errors.New("RRI glossary approval found no repository truth root (CONTEXT.md) to update")
	}
	glossaryPath := filepath.Join(truthRoot, "CONTEXT.md")
	previous, err := os.ReadFile(glossaryPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, nil, err
	}
	existed := err == nil
	updated := renderRriGlossaryEntries(string(previous), updates)
	temp := glossaryPath + ".tmp-" + shortID()
	if err := os.WriteFile(temp, []byte(updated), 0o644); err != nil {
		return false, nil, err
	}
	if err := os.Rename(temp, glossaryPath); err != nil {
		os.Remove(temp)
		return false, nil, err
	}
	restore := func() error {
		if existed {
			return os.WriteFile(glossaryPath, previous, 0o644)
		}
		return os.Remove(glossaryPath)
	}
	return true, restore, nil
}

// rriArtifactGlossaryUpdates extracts the explicitly identified glossary
// updates from an approved RRI artifact. Prose artifacts (interview
// checkpoints) and reports without the section return no updates; a JSON
// report whose glossary_updates fails validation returns an error so the
// caller can reject the approval rather than apply unvalidated terms.
func rriArtifactGlossaryUpdates(content string) ([]rriGlossaryUpdate, error) {
	var report map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &report); err != nil {
		return nil, nil
	}
	raw, ok := report["glossary_updates"]
	if !ok || strings.TrimSpace(string(raw)) == "null" {
		return nil, nil
	}
	var updates []rriGlossaryUpdate
	if err := json.Unmarshal(raw, &updates); err != nil {
		return nil, err
	}
	if err := validateRriGlossaryUpdates(updates); err != nil {
		return nil, err
	}
	return updates, nil
}

// renderRriGlossaryEntries appends glossary entries in the canonical CONTEXT.md
// entry shape (**Term**: / definition / _Avoid_: phrases) after the existing
// content, leaving every unrelated byte untouched.
func renderRriGlossaryEntries(existing string, updates []rriGlossaryUpdate) string {
	var builder strings.Builder
	builder.WriteString(existing)
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		builder.WriteString("\n")
	}
	for _, row := range updates {
		builder.WriteString("\n**" + row.Term + "**:\n" + row.Definition + "\n")
		if avoid := strings.TrimSpace(row.Avoid); avoid != "" {
			builder.WriteString("_Avoid_: " + avoid + "\n")
		}
	}
	return builder.String()
}

// approveWorkItemArtifactTx records one stage checkpoint inside a caller-owned
// transaction so batched owner decisions (checkpoint-decide) share the exact
// validation and predecessor rules of single approvals. artifactRef may be
// "current" to bind the stage's latest artifact revision.
func approveWorkItemArtifactTx(tx *sql.Tx, workItemID, stage, artifactRef, decision string) (string, int, string, error) {
	expectedDecision := "approved"
	if stage == "scan" {
		expectedDecision = "accepted"
	}
	if decision != expectedDecision {
		return "", 0, "", fmt.Errorf("%s requires decision %s", stage, expectedDecision)
	}
	artifactID := artifactRef
	if artifactID == "current" {
		if err := tx.QueryRow(`SELECT id FROM work_item_artifacts WHERE work_item_id=? AND stage=? ORDER BY revision DESC LIMIT 1`, workItemID, stage).Scan(&artifactID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", 0, "", fmt.Errorf("No current %s artifact", stage)
			}
			return "", 0, "", err
		}
	}
	var revision int
	var contentHash string
	err := tx.QueryRow(`SELECT revision,content_hash FROM work_item_artifacts WHERE id=? AND work_item_id=? AND stage=? AND revision=(SELECT MAX(revision) FROM work_item_artifacts WHERE work_item_id=? AND stage=?)`, artifactID, workItemID, stage, workItemID, stage).Scan(&revision, &contentHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, "", fmt.Errorf("Artifact %s is not current", artifactID)
		}
		return "", 0, "", err
	}
	stages, err := planningStagesForWorkItem(tx, workItemID)
	if err != nil {
		return "", 0, "", err
	}
	stageIndex := indexOfStage(stages, stage)
	if stageIndex < 0 {
		return "", 0, "", fmt.Errorf("stage %s is not part of this Work Item planning profile", stage)
	}
	if stageIndex > 0 {
		var previous int
		// The predecessor counts as approved only with its owner decision
		// recorded; a rejected newer checkpoint never clears the gate.
		if err = tx.QueryRow(`SELECT COUNT(*) FROM workflow_checkpoints WHERE work_item_id=? AND stage=? AND decision_type=?`, workItemID, stages[stageIndex-1], approvedCheckpointDecision(stages[stageIndex-1])).Scan(&previous); err != nil || previous != 1 {
			return "", 0, "", fmt.Errorf("Previous stage %s is not approved; %s", stages[stageIndex-1], nextActionHint(stages[stageIndex-1]))
		}
	}
	if stage == "task_graph" {
		var graphContent string
		if err = tx.QueryRow(`SELECT content FROM work_item_artifacts WHERE id=? AND work_item_id=?`, artifactID, workItemID).Scan(&graphContent); err != nil {
			return "", 0, "", err
		}
		if _, err = validateTaskGraphArtifact(tx, workItemID, graphContent); err != nil {
			return "", 0, "", fmt.Errorf("task graph validation failed: %w", err)
		}
	}
	if stage == "blueprint" {
		// Re-bind at approval: a newer approved RRI between Blueprint save and
		// approval must not leave stale excluded_keys references unvalidated.
		var blueprintContent string
		if err = tx.QueryRow(`SELECT content FROM work_item_artifacts WHERE id=? AND work_item_id=?`, artifactID, workItemID).Scan(&blueprintContent); err != nil {
			return "", 0, "", err
		}
		if err = validateBlueprintExcludedKeysBinding(tx, workItemID, blueprintContent); err != nil {
			return "", 0, "", err
		}
	}
	if stage == "contracts" {
		// Re-bind at approval: a Blueprint re-approval between Contract save and
		// approval must not leave the Contract bound to a retired lineage.
		var contractContent string
		if err = tx.QueryRow(`SELECT content FROM work_item_artifacts WHERE id=? AND work_item_id=?`, artifactID, workItemID).Scan(&contractContent); err != nil {
			return "", 0, "", err
		}
		if err = validateContractPolicyBinding(tx, workItemID, contractContent); err != nil {
			return "", 0, "", err
		}
	}
	if _, err = tx.Exec(`INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES(?,?,?,?,?,?,?)`, "wic-"+shortID(), workItemID, stage, artifactID, revision, contentHash, decision); err != nil {
		return "", 0, "", err
	}
	return artifactID, revision, contentHash, nil
}

func workItemWorkflowStatus(db *sql.DB, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: pic work-item workflow-status <id>")
	}
	item, err := workItemByID(db, args[0])
	if err != nil {
		return err
	}
	if status, ok, statusErr := aggregateDeliveryWorkflowStatus(db, args[0]); statusErr != nil {
		return statusErr
	} else if ok {
		writeJSON(os.Stdout, withNextActions(status))
		return nil
	}
	var childCount, authorizationCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=?`, args[0]).Scan(&childCount)
	_ = db.QueryRow(`SELECT COUNT(*) FROM implementation_authorizations WHERE work_item_id=? AND revoked_at=''`, args[0]).Scan(&authorizationCount)
	if childCount > 0 && authorizationCount > 0 {
		next := "implement"
		tx, beginErr := db.Begin()
		if beginErr != nil {
			return beginErr
		}
		if validateAggregateDescendants(tx, args[0]) == nil {
			next = "aggregate_verification"
		}
		_ = tx.Rollback()
		writeJSON(os.Stdout, withNextActions(map[string]any{"work_item_id": args[0], "workflow_kind": "aggregate_delivery", "next_stage": next}))
		return nil
	}
	if contains([]string{"task", "bug", "chore"}, fmt.Sprint(item["type"])) {
		if fmt.Sprint(item["parent_id"]) == "" {
			var active int
			_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, args[0]).Scan(&active)
			if active == 0 {
				return workItemStandalonePlanningStatus(db, args[0])
			}
		}
		return workItemExecutionStatus(db, args[0])
	}
	checkpoints := map[string]any{}
	next := "materialize"
	for _, stage := range workItemStages {
		approved, err := rowExists(db, `SELECT 1 FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.work_item_id=? AND c.stage=? AND c.decision_type='`+approvedCheckpointDecision(stage)+`' AND a.revision=(SELECT MAX(revision) FROM work_item_artifacts WHERE work_item_id=? AND stage=?)`, args[0], stage, args[0], stage)
		if err != nil {
			return err
		}
		checkpoints[stage] = approved
		// rri_t_scenarios is a supplementary retained scenario artifact: it is
		// reported for owner visibility but never gates the planning workflow.
		if stage == "rri_t_scenarios" {
			continue
		}
		if !approved && next == "materialize" {
			next = stage
		}
	}
	if next == "materialize" {
		var checkpointID string
		_ = db.QueryRow(`SELECT id FROM workflow_checkpoints WHERE work_item_id=? AND stage='task_graph' AND decision_type='approved' ORDER BY artifact_revision DESC LIMIT 1`, args[0]).Scan(&checkpointID)
		var materialized, active int
		_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_materializations WHERE root_work_item_id=? AND checkpoint_id=?`, args[0], checkpointID).Scan(&materialized)
		_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE checkpoint_id=? AND status='active'`, checkpointID).Scan(&active)
		if materialized > 0 {
			next = "authorize"
		}
		if active > 0 {
			next = "implement"
		}
	}
	writeJSON(os.Stdout, withNextActions(map[string]any{"work_item_id": args[0], "next_stage": next, "checkpoints": checkpoints}))
	return nil
}

func planningStagesForWorkItem(db databaseQueryer, id string) ([]string, error) {
	stages, _, _, _, err := computePlanStagesForWorkItem(db, id)
	return stages, err
}

func indexOfStage(stages []string, stage string) int {
	for index, candidate := range stages {
		if candidate == stage {
			return index
		}
	}
	return -1
}

func workItemStandalonePlanningStatus(db *sql.DB, id string) error {
	next := "materialize"
	checkpoints := map[string]any{}
	for _, stage := range []string{"scan", "rri", "task_graph"} {
		approved, err := rowExists(db, `SELECT 1 FROM workflow_checkpoints WHERE work_item_id=? AND stage=? AND decision_type='`+approvedCheckpointDecision(stage)+`'`, id, stage)
		if err != nil {
			return err
		}
		checkpoints[stage] = approved
		if !approved && next == "materialize" {
			next = stage
		}
	}
	if next == "materialize" {
		var mappings int
		_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_materializations WHERE root_work_item_id=? AND work_item_id=?`, id, id).Scan(&mappings)
		if mappings > 0 {
			next = "authorize"
		}
	}
	writeJSON(os.Stdout, withNextActions(map[string]any{"work_item_id": id, "workflow_kind": "standalone_plan", "next_stage": next, "checkpoints": checkpoints}))
	return nil
}

func workItemExecutionStatus(db *sql.DB, id string) error {
	state, err := loadWorkItemExecutionState(db, id)
	if err != nil {
		return err
	}
	graph := map[string]any{}
	var artifactID, checkpointID, decision string
	var revision int
	if db.QueryRow(`SELECT a.id,a.revision,c.id,c.decision_type FROM work_item_artifacts a LEFT JOIN workflow_checkpoints c ON c.artifact_id=a.id AND c.artifact_revision=a.revision WHERE a.work_item_id=(SELECT COALESCE(parent_id,id) FROM work_items WHERE id=?) AND a.stage='task_graph' ORDER BY a.revision DESC LIMIT 1`, id).Scan(&artifactID, &revision, &checkpointID, &decision) == nil {
		graph = map[string]any{"artifact_id": artifactID, "revision": revision, "checkpoint_id": checkpointID, "decision": decision}
	}
	writeJSON(os.Stdout, withNextActions(map[string]any{"work_item_id": id, "workflow_kind": "execution", "next_stage": state.NextStage, "pipeline_stage": state.PipelineStage, "active_instruction_pack_id": state.PackID, "candidate_run_id": state.CandidateID, "review_status": state.ReviewStatus, "completion_report_id": state.CompletionID, "verification_status": state.VerificationStatus, "owner_decision": state.OwnerDecision, "current_task_graph": graph}))
	return nil
}

func indexOfWorkItemStage(stage string) int {
	for index, candidate := range workItemStages {
		if candidate == stage {
			return index
		}
	}
	return -1
}

func aggregateDeliveryWorkflowStatus(db *sql.DB, id string) (map[string]any, bool, error) {
	delivery, err := queryMaps(db, `SELECT * FROM work_item_delivery_states WHERE work_item_id=?`, id)
	if err != nil || len(delivery) == 0 {
		return nil, false, err
	}
	state := delivery[0]
	next := "implement"
	var itemStatus string
	if err = db.QueryRow(`SELECT status FROM work_items WHERE id=?`, id).Scan(&itemStatus); err != nil {
		return nil, false, err
	}
	if itemStatus == "done" {
		next = "done"
	} else if fmt.Sprint(state["verification_report_id"]) != "" {
		var decision string
		_ = db.QueryRow(`SELECT decision FROM work_item_aggregate_owner_decisions WHERE work_item_id=? AND verification_report_id=? ORDER BY rowid DESC LIMIT 1`, id, state["verification_report_id"]).Scan(&decision)
		if decision == "accepted" && fmt.Sprint(state["integration_mode"]) == "branch" {
			next = "merge_pending"
		} else if decision == "rejected" {
			next = "aggregate_verification"
		} else {
			next = "owner_acceptance"
		}
	} else {
		tx, beginErr := db.Begin()
		if beginErr != nil {
			return nil, false, beginErr
		}
		if validateAggregateDescendants(tx, id) == nil {
			next = "aggregate_verification"
		}
		_ = tx.Rollback()
	}
	state["work_item_id"] = id
	state["workflow_kind"] = "aggregate_delivery"
	state["next_stage"] = next
	return state, true, nil
}

// RRI-T scenario identity constraint: the canonical dedupe key is the id-based
// identity (dimension|stress_axis|requirement_id|id) shared with the TypeScript
// grading compiler — never the authoring persona — so two persisted scenarios may
// share persona, dimension, stress axis, and requirement while staying distinct
// by id. Deferred dispositions (the compiled not_applicable records) deduplicate
// on the same identity, so one persisted scenario can be deferred at most once.
func validateRriTVerification(db *sql.DB, workItemID, aggregateStatus, content string) error {
	var report struct {
		Scenarios []struct {
			ID            string `json:"id"`
			Persona       string `json:"persona"`
			Dimension     string `json:"dimension"`
			StressAxis    string `json:"stress_axis"`
			RequirementID string `json:"requirement_id"`
			Procedure     string `json:"procedure"`
			Evidence      string `json:"evidence"`
			Result        string `json:"result"`
		} `json:"scenarios"`
		NotApplicable []struct {
			ID            string `json:"id"`
			Persona       string `json:"persona"`
			Dimension     string `json:"dimension"`
			StressAxis    string `json:"stress_axis"`
			RequirementID string `json:"requirement_id"`
			Reason        string `json:"reason"`
		} `json:"not_applicable"`
	}
	if err := json.Unmarshal([]byte(content), &report); err != nil {
		return fmt.Errorf("invalid RRI-T evidence JSON: %w", err)
	}
	validDimensions := map[string]bool{"D1": true, "D2": true, "D3": true, "D4": true, "D5": true, "D6": true, "D7": true}
	validStressAxes := map[string]bool{"TIME": true, "DATA": true, "ERROR": true, "COLLABORATION": true, "EMERGENCY": true, "SCALE": true, "COMPLIANCE": true, "EVOLUTION": true}
	validResults := map[string]bool{"PASS": true, "ACCEPTABLE": true, "PAINFUL": true, "FAIL": true}
	rows, err := db.Query(`SELECT requirement_key FROM requirements WHERE (task_id=? OR epic_id=?)`, workItemID, workItemID)
	if err != nil {
		return err
	}
	defer rows.Close()
	approved := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return err
		}
		approved[key] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, scenario := range report.Scenarios {
		if scenario.ID == "" || scenario.Persona == "" || !validDimensions[scenario.Dimension] || !validStressAxes[scenario.StressAxis] || !approved[scenario.RequirementID] || scenario.Procedure == "" || scenario.Evidence == "" || !validResults[scenario.Result] {
			return fmt.Errorf("invalid RRI-T scenario for requirement %s", scenario.RequirementID)
		}
		key := scenario.Dimension + "|" + scenario.StressAxis + "|" + scenario.RequirementID + "|" + scenario.ID
		if seen[key] {
			return fmt.Errorf("duplicate RRI-T scenario %s", key)
		}
		seen[key] = true
		if aggregateStatus == "passed" && scenario.Result != "PASS" {
			return fmt.Errorf("RRI-T %s result requires remediation or owner deferral before aggregate passage", scenario.Result)
		}
	}
	for _, deferred := range report.NotApplicable {
		if deferred.ID == "" {
			// Authored N/A topics carry no scenario identity; only the concrete reason is required.
			if strings.TrimSpace(deferred.Reason) == "" {
				return fmt.Errorf("RRI-T not_applicable disposition requires a concrete reason")
			}
			continue
		}
		if deferred.Persona == "" || !validDimensions[deferred.Dimension] || !validStressAxes[deferred.StressAxis] || !approved[deferred.RequirementID] || strings.TrimSpace(deferred.Reason) == "" {
			return fmt.Errorf("invalid RRI-T not_applicable disposition for requirement %s", deferred.RequirementID)
		}
		key := deferred.Dimension + "|" + deferred.StressAxis + "|" + deferred.RequirementID + "|" + deferred.ID
		if seen[key] {
			return fmt.Errorf("duplicate RRI-T scenario %s", key)
		}
		seen[key] = true
	}
	return nil
}

func workItemAggregateVerify(db *sql.DB, args []string) error {
	if len(args) < 3 || !contains([]string{"passed", "failed", "partial", "blocked"}, args[1]) {
		return errors.New("usage: pic work-item aggregate-verify <id> <status> <summary> --actor-role contractor")
	}
	opts, err := parseOptions(args[3:])
	if err != nil || validateWorkflowActor(opts["actor-role"], "contractor") != nil {
		return errors.New("aggregate verification requires actor_role=contractor")
	}
	rriTJSON := opts["rri-t-json"]
	if rriTJSON != "" {
		if err := validateRriTVerification(db, args[0], args[1], rriTJSON); err != nil {
			return err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = validateAggregateDescendants(tx, args[0]); err != nil && args[1] == "passed" {
		return err
	}
	checkpointID := ""
	_ = tx.QueryRow(`SELECT id FROM workflow_checkpoints WHERE work_item_id=? AND stage='task_graph' AND decision_type='approved' ORDER BY artifact_revision DESC LIMIT 1`, args[0]).Scan(&checkpointID)
	id := "wivr-" + shortID()
	if _, err = tx.Exec(`INSERT INTO work_item_verification_reports(id,work_item_id,checkpoint_id,status,summary,verified_by_role,rri_t_json) VALUES(?,?,?,?,?,?,?)`, id, args[0], checkpointID, args[1], args[2], opts["actor-role"], rriTJSON); err != nil {
		return err
	}
	correctiveBugID := ""
	approvalRequired := 0
	if args[1] != "passed" {
		// Ambiguous retry after an atomic result already linked a Bug returns the
		// existing linked Bug instead of creating a duplicate. Dedup on a stable
		// identity (the aggregate's unresolved corrective Bug for the same
		// work_item_id and status) because the freshly-random report id can never
		// match across invocations.
		existing := ""
		err = tx.QueryRow(`SELECT c.bug_work_item_id,c.owner_approval_required
			FROM work_item_corrective_bugs c
			JOIN work_item_verification_reports r ON r.id=c.verification_report_id
			JOIN work_items b ON b.id=c.bug_work_item_id
			WHERE r.work_item_id=? AND r.status=? AND b.status NOT IN ('done','cancelled')
			ORDER BY c.created_at DESC LIMIT 1`, args[0], args[1]).Scan(&existing, &approvalRequired)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			correctiveBugID = existing
		} else {
			correctiveBugID = "wi-" + shortID()
			if _, err = tx.Exec(`INSERT INTO work_items(id,type,parent_id,title,description,priority) VALUES(?,'bug',?,?,?,'high')`, correctiveBugID, args[0], "Correct aggregate verification failure", args[2]); err != nil {
				return err
			}
			requirementID := "req-" + shortID()
			requirementKey := "CORRECTIVE-" + strings.ToUpper(strings.TrimPrefix(id, "wivr-"))
			acceptance := "Given aggregate verification report " + id + " is not passed\nWhen the corrective work is implemented and verified\nThen the aggregate verification failure is resolved"
			if _, err = tx.Exec(`INSERT INTO requirements(id,task_id,requirement_key,title,description,acceptance_criteria,priority,source) VALUES(?,?,?,?,?,?,'tier1',?)`, requirementID, correctiveBugID, requirementKey, "Resolve aggregate verification failure", args[2], acceptance, id); err != nil {
				return fmt.Errorf("create corrective requirement: %w", err)
			}
			// Partial and blocked outcomes need no owner approval before scheduling;
			// failed outcomes retain evidence and wait for an explicit owner decision.
			approvalRequired = 0
			if args[1] == "failed" {
				approvalRequired = 1
			}
			if _, err = tx.Exec(`INSERT INTO work_item_corrective_bugs(verification_report_id,bug_work_item_id,owner_approval_required) VALUES(?,?,?)`, id, correctiveBugID, approvalRequired); err != nil {
				return fmt.Errorf("link corrective bug: %w", err)
			}
			if _, err = tx.Exec(`INSERT INTO work_item_relations(id,work_item_id,relation_type,related_work_item_id) VALUES(?,?,'related',?)`, "wir-"+shortID(), args[0], correctiveBugID); err != nil {
				return fmt.Errorf("relate corrective bug: %w", err)
			}
			eventType, eventSummary := "corrective_scheduled", "Corrective Bug scheduled automatically (owner notified)"
			if args[1] == "failed" {
				eventType, eventSummary = "corrective_owner_decision_pending", "Corrective Bug awaits explicit owner decision"
			}
			if err = addEvent(tx, args[0], eventType, opts["actor-role"], eventSummary, map[string]any{"verification_report_id": id, "corrective_bug_id": correctiveBugID, "status": args[1], "owner_approval_required": approvalRequired, "summary": args[2]}); err != nil {
				return fmt.Errorf("record corrective owner notification: %w", err)
			}
		}
	}
	if args[1] == "passed" {
		if _, err = tx.Exec(`UPDATE requirements SET status='satisfied' WHERE (epic_id=? OR task_id=?) AND status='pending'`, args[0], args[0]); err != nil {
			return err
		}
		if err = validateAggregateWorkItem(tx, args[0]); err != nil {
			return err
		}
		var mode, branchName string
		deliveryErr := tx.QueryRow(`SELECT integration_mode,branch_name FROM work_item_delivery_states WHERE work_item_id=?`, args[0]).Scan(&mode, &branchName)
		if errors.Is(deliveryErr, sql.ErrNoRows) {
			var kind string
			var childCount, branchLabel int
			if err = tx.QueryRow(`SELECT type FROM work_items WHERE id=?`, args[0]).Scan(&kind); err != nil {
				return err
			}
			_ = tx.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=?`, args[0]).Scan(&childCount)
			_ = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM work_item_labels WHERE work_item_id=? AND label='integration:branch')`, args[0]).Scan(&branchLabel)
			mode = "coordination"
			if branchLabel != 0 || opts["branch-name"] != "" && (kind == "feature" || contains([]string{"task", "bug", "chore"}, kind) && childCount > 0) {
				mode = "branch"
			}
			var branchAncestor int
			if err = tx.QueryRow(`WITH RECURSIVE ancestors(id,parent_id) AS (
				SELECT id,parent_id FROM work_items WHERE id=(SELECT parent_id FROM work_items WHERE id=?)
				UNION ALL SELECT wi.id,wi.parent_id FROM work_items wi JOIN ancestors a ON wi.id=a.parent_id
			) SELECT EXISTS(SELECT 1 FROM ancestors a JOIN work_item_delivery_states d ON d.work_item_id=a.id WHERE d.integration_mode='branch')`, args[0]).Scan(&branchAncestor); err != nil {
				return err
			}
			if branchAncestor != 0 {
				if branchLabel != 0 {
					return errors.New("nested aggregate cannot own a branch beneath a branch-owning ancestor")
				}
				mode = "coordination"
			}
			if _, err = tx.Exec(`INSERT INTO work_item_delivery_states(work_item_id,integration_mode,branch_name,base_branch,base_commit) VALUES(?,?,?,?,?)`, args[0], mode, opts["branch-name"], "develop", opts["base-commit"]); err != nil {
				return err
			}
			branchName = opts["branch-name"]
		} else if deliveryErr != nil {
			return deliveryErr
		}
		if mode == "branch" {
			if opts["branch-name"] == "" || opts["head-commit"] == "" || opts["base-commit"] == "" || opts["branch-name"] != branchName {
				return errors.New("branch aggregate verification requires the bound branch name, head commit, and current base commit")
			}
			if _, err = tx.Exec(`UPDATE work_item_delivery_states SET base_commit=?,verified_head=?,verification_report_id=?,merge_status='',merged_commit='',merge_error='',updated_at=datetime('now') WHERE work_item_id=?`, opts["base-commit"], opts["head-commit"], id, args[0]); err != nil {
				return err
			}
		} else if _, err = tx.Exec(`UPDATE work_item_delivery_states SET verification_report_id=?,merge_status='',merged_commit='',merge_error='',updated_at=datetime('now') WHERE work_item_id=?`, id, args[0]); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	writeJSON(os.Stdout, map[string]any{"id": id, "work_item_id": args[0], "checkpoint_id": checkpointID, "status": args[1], "summary": args[2], "corrective_bug_id": correctiveBugID, "owner_approval_required": approvalRequired})
	return nil
}

func workItemAggregateAccept(db *sql.DB, args []string) error {
	if len(args) < 4 || !contains([]string{"accepted", "rejected"}, args[2]) {
		return errors.New("usage: pic work-item aggregate-accept <id> <verification-report-id> <accepted|rejected> <notes> --actor-role owner")
	}
	opts, err := parseOptions(args[4:])
	if err != nil || validateWorkflowActor(opts["actor-role"], "owner") != nil {
		return errors.New("aggregate acceptance requires actor_role=owner")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = validateAggregateWorkItem(tx, args[0]); err != nil {
		return err
	}
	var reportID, reportStatus, reportCheckpoint, currentCheckpoint, deliveryReport, mode, verifiedHead, baseCommit string
	if err = tx.QueryRow(`SELECT id,status,checkpoint_id FROM work_item_verification_reports WHERE work_item_id=? ORDER BY datetime(created_at) DESC,rowid DESC LIMIT 1`, args[0]).Scan(&reportID, &reportStatus, &reportCheckpoint); err != nil || reportID != args[1] || reportStatus != "passed" {
		return errors.New("aggregate acceptance requires the current passed aggregate verification")
	}
	_ = tx.QueryRow(`SELECT id FROM workflow_checkpoints WHERE work_item_id=? AND stage='task_graph' AND decision_type='approved' ORDER BY artifact_revision DESC LIMIT 1`, args[0]).Scan(&currentCheckpoint)
	if reportCheckpoint != currentCheckpoint {
		return errors.New("aggregate verification is stale for the current task graph")
	}
	if err = tx.QueryRow(`SELECT integration_mode,verification_report_id,verified_head,base_commit FROM work_item_delivery_states WHERE work_item_id=?`, args[0]).Scan(&mode, &deliveryReport, &verifiedHead, &baseCommit); err != nil || deliveryReport != reportID {
		return errors.New("aggregate acceptance requires verification bound to the current delivery state")
	}
	if mode == "branch" && (opts["head-commit"] != verifiedHead || opts["base-commit"] != baseCommit) {
		return errors.New("aggregate acceptance requires the unchanged verified delivery head and base commit")
	}
	var decisions int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM work_item_aggregate_owner_decisions WHERE work_item_id=? AND verification_report_id=?`, args[0], reportID).Scan(&decisions); err != nil {
		return err
	}
	if decisions != 0 {
		return errors.New("aggregate owner decision already recorded; fresh aggregate verification is required")
	}
	id := "wiaod-" + shortID()
	if _, err = tx.Exec(`INSERT INTO work_item_aggregate_owner_decisions(id,work_item_id,verification_report_id,decision,notes,decided_by_role) VALUES(?,?,?,?,?,?)`, id, args[0], reportID, args[2], args[3], opts["actor-role"]); err != nil {
		return err
	}
	if args[2] == "accepted" && mode == "coordination" {
		_, err = tx.Exec(`UPDATE work_items SET status='done' WHERE id=?`, args[0])
	} else if args[2] == "accepted" {
		_, err = tx.Exec(`UPDATE work_items SET status='in_progress' WHERE id=?`, args[0])
		if err == nil {
			_, err = tx.Exec(`UPDATE work_item_delivery_states SET merge_status='merge_pending',merge_error='',updated_at=datetime('now') WHERE work_item_id=?`, args[0])
		}
	} else {
		_, err = tx.Exec(`UPDATE work_items SET status='open' WHERE id=?`, args[0])
		if err == nil {
			_, err = tx.Exec(`UPDATE work_item_delivery_states SET merge_status='blocked',merge_error=?,updated_at=datetime('now') WHERE work_item_id=?`, args[3], args[0])
		}
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return outputOne(db, `SELECT * FROM work_item_aggregate_owner_decisions WHERE id=?`, id)
}

func workItemAggregateMergeResult(db *sql.DB, args []string) error {
	if len(args) != 4 || !contains([]string{"merged", "blocked"}, args[2]) || args[1] == "" || args[3] == "" {
		return errors.New("usage: pic work-item aggregate-merge-result <id> <verified-head> <merged|blocked> <merge-commit|error>")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var verifiedHead, reportID string
	if err = tx.QueryRow(`SELECT verified_head,verification_report_id FROM work_item_delivery_states WHERE work_item_id=? AND integration_mode='branch'`, args[0]).Scan(&verifiedHead, &reportID); err != nil || verifiedHead != args[1] {
		return errors.New("merge result does not match the verified delivery head")
	}
	var decision string
	if err = tx.QueryRow(`SELECT decision FROM work_item_aggregate_owner_decisions WHERE work_item_id=? AND verification_report_id=? ORDER BY rowid DESC LIMIT 1`, args[0], reportID).Scan(&decision); err != nil || decision != "accepted" {
		return errors.New("merge requires current aggregate owner acceptance")
	}
	if args[2] == "merged" {
		if _, err = tx.Exec(`UPDATE work_item_delivery_states SET merge_status='merged',merged_commit=?,merge_error='',updated_at=datetime('now') WHERE work_item_id=?`, args[3], args[0]); err == nil {
			_, err = tx.Exec(`UPDATE work_items SET status='done' WHERE id=?`, args[0])
		}
	} else {
		_, err = tx.Exec(`UPDATE work_item_delivery_states SET merge_status='blocked',merge_error=?,updated_at=datetime('now') WHERE work_item_id=?`, args[3], args[0])
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return outputOne(db, `SELECT * FROM work_item_delivery_states WHERE work_item_id=?`, args[0])
}

func workItemAggregateClose(db *sql.DB, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: pic work-item aggregate-close <id>")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = validateAggregateWorkItem(tx, args[0]); err != nil {
		return err
	}
	var reportCheckpoint, reportStatus, currentCheckpoint string
	if err = tx.QueryRow(`SELECT checkpoint_id,status FROM work_item_verification_reports WHERE work_item_id=? ORDER BY datetime(created_at) DESC,rowid DESC LIMIT 1`, args[0]).Scan(&reportCheckpoint, &reportStatus); err != nil || reportStatus != "passed" {
		return errors.New("current passed aggregate verification required")
	}
	_ = tx.QueryRow(`SELECT id FROM workflow_checkpoints WHERE work_item_id=? AND stage='task_graph' AND decision_type='approved' ORDER BY artifact_revision DESC LIMIT 1`, args[0]).Scan(&currentCheckpoint)
	if reportCheckpoint != currentCheckpoint {
		return errors.New("aggregate verification is stale")
	}
	var mode, deliveryReport, mergeStatus, decision string
	if err = tx.QueryRow(`SELECT integration_mode,verification_report_id,merge_status FROM work_item_delivery_states WHERE work_item_id=?`, args[0]).Scan(&mode, &deliveryReport, &mergeStatus); err != nil || deliveryReport == "" {
		return errors.New("aggregate closure requires current delivery verification")
	}
	if err = tx.QueryRow(`SELECT decision FROM work_item_aggregate_owner_decisions WHERE work_item_id=? AND verification_report_id=? ORDER BY rowid DESC LIMIT 1`, args[0], deliveryReport).Scan(&decision); err != nil || decision != "accepted" {
		return errors.New("aggregate closure requires owner acceptance")
	}
	if mode == "branch" && mergeStatus != "merged" {
		return errors.New("branch aggregate closure requires confirmed merge evidence")
	}
	result, err := tx.Exec(`UPDATE work_items SET status='done' WHERE id=? AND type IN ('epic','feature')`, args[0])
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("Work Item %s is not an aggregate", args[0])
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	item, err := workItemByID(db, args[0])
	if err == nil {
		writeJSON(os.Stdout, item)
	}
	return err
}

func validateAggregateWorkItem(tx *sql.Tx, id string) error {
	if err := validateAggregateDescendants(tx, id); err != nil {
		return err
	}
	var unmet int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM requirements WHERE (epic_id=? OR task_id=?) AND status NOT IN ('satisfied','deferred')`, id, id).Scan(&unmet); err != nil {
		return err
	}
	if unmet != 0 {
		return fmt.Errorf("aggregate has %d unmet requirements", unmet)
	}
	return nil
}

func validateAggregateDescendants(tx *sql.Tx, id string) error {
	var kind string
	if err := tx.QueryRow(`SELECT type FROM work_items WHERE id=?`, id).Scan(&kind); err != nil {
		return fmt.Errorf("Work Item %s not found", id)
	}
	if kind != "epic" && kind != "feature" {
		var children int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=?`, id).Scan(&children); err != nil || children == 0 {
			return fmt.Errorf("Work Item %s is not an aggregate", id)
		}
	}
	var openDescendants int
	if err := tx.QueryRow(`WITH RECURSIVE descendants(id,status) AS (
		SELECT id,status FROM work_items WHERE parent_id=? UNION ALL SELECT wi.id,wi.status FROM work_items wi JOIN descendants d ON wi.parent_id=d.id
	) SELECT COUNT(*) FROM descendants WHERE status NOT IN ('done','cancelled')`, id).Scan(&openDescendants); err != nil {
		return err
	}
	if openDescendants != 0 {
		return fmt.Errorf("aggregate has %d open descendants", openDescendants)
	}
	return nil
}

func workItemGraphValidate(db *sql.DB, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: pic work-item graph-validate <id>")
	}
	var artifactID, content, contentHash string
	var revision int
	if err := db.QueryRow(`SELECT id,revision,content,content_hash FROM work_item_artifacts WHERE work_item_id=? AND stage='task_graph' ORDER BY revision DESC LIMIT 1`, args[0]).Scan(&artifactID, &revision, &content, &contentHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("no task graph draft has been saved")
		}
		return err
	}
	plan, err := validateTaskGraphArtifact(db, args[0], content)
	if err != nil {
		return err
	}
	writeJSON(os.Stdout, map[string]any{"valid": true, "work_item_id": args[0], "artifact_id": artifactID, "revision": revision, "content_hash": contentHash, "node_count": len(plan.Nodes), "decomposition_policy_version": plan.DecompositionPolicyVersion})
	return nil
}

func validateTaskGraphArtifact(db databaseQueryer, workItemID, content string) (tip.TaskPlanDocument, error) {
	plan, err := tip.ParseTaskPlanJSON("```task-plan-json\n" + content + "\n```")
	if err != nil {
		return tip.TaskPlanDocument{}, err
	}
	coverage, err := validateTaskGraphRequirementCoverage(db, workItemID, plan)
	if err != nil {
		return tip.TaskPlanDocument{}, err
	}
	if err := validateTaskGraphObligations(db, workItemID, plan); err != nil {
		return tip.TaskPlanDocument{}, err
	}
	if err := validateTaskGraphDecompositionPolicy(db, workItemID, plan, coverage); err != nil {
		return tip.TaskPlanDocument{}, err
	}
	var kind, parentID string
	if err = db.QueryRow(`SELECT type,COALESCE(parent_id,'') FROM work_items WHERE id=?`, workItemID).Scan(&kind, &parentID); err != nil {
		return tip.TaskPlanDocument{}, err
	}
	if contains([]string{"task", "bug", "chore"}, kind) && parentID == "" && (len(plan.Nodes) != 1 || plan.Nodes[0].Type != kind || plan.Nodes[0].ParentKey != "" || len(plan.Nodes[0].DependsOn) != 0) {
		return tip.TaskPlanDocument{}, errors.New("standalone task graph requires exactly one matching executable node without parent or dependencies")
	}
	return plan, nil
}

func validateTaskGraphObligations(db databaseQueryer, workItemID string, plan tip.TaskPlanDocument) error {
	var contractContent string
	if err := db.QueryRow(`SELECT a.content FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.work_item_id=? AND c.stage='contracts' AND c.decision_type='approved' ORDER BY c.artifact_revision DESC LIMIT 1`, workItemID).Scan(&contractContent); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("approved Contract is required before Task Graph obligation validation: %w", err)
	}
	var contract tip.ContractDocument
	bindingFieldsPresent := false
	for _, node := range plan.Nodes {
		if node.Type != "feature" && node.Type != "gate" && (node.Provides != nil || node.Consumes != nil || node.EvidenceFor != nil || node.ObligationKeys != nil) {
			bindingFieldsPresent = true
			break
		}
	}
	if err := json.Unmarshal([]byte(contractContent), &contract); err != nil {
		return fmt.Errorf("approved Contract obligation graph is invalid: %w", err)
	}
	if len(contract.Obligations) == 0 {
		return errors.New("approved Contract has no obligation graph; redraft the Contract with atomic obligations")
	}
	if contract.ObligationSchemaVersion != 2 {
		return nil
	}
	if !bindingFieldsPresent {
		return errors.New("Task Graph nodes must bind Contract obligations with provides, consumes, evidence_for, and obligation_keys")
	}
	obligations := map[string]tip.ContractObligation{}
	for _, obligation := range contract.Obligations {
		if obligation.ID == "" || obligations[obligation.ID].ID != "" {
			return fmt.Errorf("Contract obligation %s is duplicated", obligation.ID)
		}
		obligations[obligation.ID] = obligation
	}
	providers := map[string][]string{}
	evidence := map[string][]string{}
	for _, node := range plan.Nodes {
		if node.Type == "feature" || node.Type == "gate" {
			continue
		}
		for _, key := range node.Provides {
			providers[key] = append(providers[key], node.Key)
		}
		for _, key := range node.EvidenceFor {
			evidence[key] = append(evidence[key], node.Key)
		}
	}
	for _, node := range plan.Nodes {
		if node.Type == "feature" || node.Type == "gate" {
			continue
		}
		if node.Provides == nil || node.Consumes == nil || node.EvidenceFor == nil || node.ObligationKeys == nil {
			return fmt.Errorf("%s requires provides, consumes, evidence_for, and obligation_keys", node.Key)
		}
		for _, key := range node.ObligationKeys {
			if _, ok := obligations[key]; !ok {
				return fmt.Errorf("%s references unknown Contract obligation %s", node.Key, key)
			}
		}
		for _, key := range node.Provides {
			if _, ok := obligations[key]; !ok {
				return fmt.Errorf("%s provides unknown Contract obligation %s", node.Key, key)
			}
		}
		for _, key := range node.EvidenceFor {
			if _, ok := obligations[key]; !ok {
				return fmt.Errorf("%s evidences unknown Contract obligation %s", node.Key, key)
			}
		}
		for _, key := range node.Consumes {
			providerNodes := providers[key]
			if len(providerNodes) == 0 {
				return fmt.Errorf("%s consumes obligation %s without a provider", node.Key, key)
			}
			for _, provider := range providerNodes {
				if provider == node.Key || !contains(node.DependsOn, provider) {
					return fmt.Errorf("%s consumes obligation %s but does not depend on provider %s", node.Key, key, provider)
				}
			}
		}
	}
	for key := range obligations {
		// Provenance constraint: exactly one node produces each Contract
		// obligation. Zero providers leave the obligation unimplemented and
		// multiple providers make dependency and evidence provenance ambiguous.
		if len(providers[key]) != 1 {
			return fmt.Errorf("Contract obligation %s must have exactly one provider node, found %d", key, len(providers[key]))
		}
		if len(evidence[key]) == 0 {
			return fmt.Errorf("Contract obligation %s has no evidence node", key)
		}
	}
	return nil
}

// decompositionModes is the decomposition policy v2 node-mode enum. Absent mode
// means vertical; every other mode is an explicit, justified exception.
var decompositionModes = []string{"vertical", "shared_contract", "wide_refactor", "integration_gate"}

// decompositionModeOf resolves the persisted projection mode: policy default
// for an absent mode is vertical, so an omitted v2 mode never persists as an
// empty value.
func decompositionModeOf(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return "vertical"
	}
	return mode
}

// validateTaskGraphDecompositionPolicy enforces the decomposition policy the
// task graph declares. Policy 0/1 documents run the v1 rules unchanged; policy
// 2 adds the vertical-by-default, edge-rationale, effective-acceptance, and
// seam-bound verification rules. Unsupported versions fail closed.
func validateTaskGraphDecompositionPolicy(db databaseQueryer, workItemID string, plan tip.TaskPlanDocument, requirements map[string]tip.RequirementSnapshot) error {
	if plan.DecompositionPolicyVersion > 2 {
		return fmt.Errorf("unsupported decomposition_policy_version %d", plan.DecompositionPolicyVersion)
	}
	if plan.DecompositionPolicyVersion != 2 {
		return nil
	}
	return validateTaskGraphPolicyV2(db, workItemID, plan, requirements)
}

func validateTaskGraphPolicyV2(db databaseQueryer, workItemID string, plan tip.TaskPlanDocument, requirements map[string]tip.RequirementSnapshot) error {
	nodes := map[string]tip.TaskPlanDocumentNode{}
	for _, node := range plan.Nodes {
		nodes[node.Key] = node
	}
	// dependsClosure returns every node transitively reachable through DependsOn.
	dependsClosure := func(key string) map[string]bool {
		closure := map[string]bool{}
		var visit func(string)
		visit = func(current string) {
			for _, dependency := range nodes[current].DependsOn {
				if !closure[dependency] {
					closure[dependency] = true
					visit(dependency)
				}
			}
		}
		visit(key)
		return closure
	}
	// Seam and obligation authority: policy v2 chains require approved Blueprint
	// and Contract predecessors, so the planning profile must carry both stages.
	// Profiles without them (quick/standard, standalone) have no seam authority
	// to bind against — a v2 graph there is rejected instead of silently
	// skipping cross-artifact binding; those graphs stay on policy v1.
	stages, err := planningStagesForWorkItem(db, workItemID)
	if err != nil {
		return err
	}
	missingStages := []string{}
	if !contains(stages, "blueprint") {
		missingStages = append(missingStages, "blueprint")
	}
	if !contains(stages, "contracts") {
		missingStages = append(missingStages, "contracts")
	}
	if len(missingStages) > 0 {
		return fmt.Errorf("Task Graph policy v2 requires approved Blueprint and Contract predecessors, but this Work Item's planning profile has no %s stage; keep the graph on policy v1 or re-plan with a deeper profile", strings.Join(missingStages, " and "))
	}
	// Seam binding applies only when the planning profile carries a Blueprint
	// stage; profiles without one have no seam authority to bind against.
	seams, err := approvedBlueprintSeams(db, workItemID)
	if err != nil {
		return err
	}
	// Exact predecessor lineage: the graph must bind the approved Contract.
	if err := validateTaskGraphSourceContractBinding(db, workItemID, plan.DecompositionPolicyVersion, plan.SourceContract); err != nil {
		return err
	}
	obligationIDs := map[string]bool{}
	var contractContent string
	if err := db.QueryRow(`SELECT a.content FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.work_item_id=? AND c.stage='contracts' AND c.decision_type='approved' ORDER BY c.artifact_revision DESC LIMIT 1`, workItemID).Scan(&contractContent); err == nil {
		var contract tip.ContractDocument
		if json.Unmarshal([]byte(contractContent), &contract) == nil {
			for _, obligation := range contract.Obligations {
				obligationIDs[obligation.ID] = true
			}
		}
	}
	for _, node := range plan.Nodes {
		mode := node.DecompositionMode
		if mode == "" {
			mode = "vertical"
		}
		if !contains(decompositionModes, mode) {
			return fmt.Errorf("%s has unknown decomposition_mode %s (expected one of %s)", node.Key, node.DecompositionMode, strings.Join(decompositionModes, "|"))
		}
		if mode != "vertical" && strings.TrimSpace(node.ExceptionReason) == "" {
			return fmt.Errorf("%s uses decomposition_mode %s without exception_reason", node.Key, mode)
		}
		switch mode {
		case "wide_refactor":
			if node.PairedContractNode == "" {
				return fmt.Errorf("%s wide_refactor requires paired_contract_node", node.Key)
			}
			if _, ok := nodes[node.PairedContractNode]; !ok {
				return fmt.Errorf("%s wide_refactor references unknown paired_contract_node %s", node.Key, node.PairedContractNode)
			}
			if !dependsClosure(node.PairedContractNode)[node.Key] {
				return fmt.Errorf("%s must be in the depends_on closure of its paired contract node %s", node.Key, node.PairedContractNode)
			}
		case "shared_contract":
			if len(node.Provides) == 0 {
				return fmt.Errorf("%s shared_contract must provide the shared contract keys", node.Key)
			}
			consumer := false
			for _, other := range plan.Nodes {
				if other.Key != node.Key && contains(other.DependsOn, node.Key) {
					consumer = true
					break
				}
			}
			if !consumer {
				return fmt.Errorf("%s shared_contract has no downstream consumer depending on it", node.Key)
			}
		case "integration_gate":
			if len(node.ObligationKeys) == 0 && len(node.RequirementKeys) == 0 {
				return fmt.Errorf("%s integration_gate must list the obligations or requirements it verifies", node.Key)
			}
		}
		for _, dependency := range node.DependsOn {
			if strings.TrimSpace(node.DependsOnRationale[dependency]) == "" {
				return fmt.Errorf("%s depends_on %s requires a non-empty depends_on_rationale entry", node.Key, dependency)
			}
		}
		// Acceptance and seam-bound verification rules bind executable leaves;
		// untyped nodes default to executable tasks (same default as materialization).
		kind := node.Type
		if kind == "" {
			kind = "task"
		}
		executable := contains([]string{"task", "bug", "chore"}, kind)
		// integration_gate is a verification-only node regardless of Work Item
		// type: it must carry valid seam-bound verification entries even though
		// it is not an executable leaf. Other non-executable nodes are skipped.
		if !executable && mode != "integration_gate" {
			continue
		}
		if executable && strings.TrimSpace(node.Acceptance) != "" {
			if err := validateGherkinSteps(node.Acceptance); err != nil {
				return fmt.Errorf("%s acceptance %w", node.Key, err)
			}
		} else if executable && len(node.RequirementKeys) != 1 {
			return fmt.Errorf("%s composes %d requirements and requires node-level acceptance with Given, When, and Then steps; a single-requirement node resolves its acceptance from that requirement", node.Key, len(node.RequirementKeys))
		}
		if len(node.Verification) == 0 {
			return fmt.Errorf("%s requires at least one seam-bound verification entry", node.Key)
		}
		for index, raw := range node.Verification {
			gate, ok := tip.ParseVerificationGate(raw)
			if !ok {
				return fmt.Errorf("%s verification gate %d must be an object", node.Key, index+1)
			}
			if strings.TrimSpace(gate.Seam) == "" {
				return fmt.Errorf("%s verification gate %d requires a seam", node.Key, index+1)
			}
			if len(gate.RequirementKeys) == 0 && len(gate.ObligationKeys) == 0 {
				return fmt.Errorf("%s verification gate %d requires at least one requirement or obligation key", node.Key, index+1)
			}
			if strings.TrimSpace(gate.Command) == "" || strings.TrimSpace(gate.Expected) == "" {
				return fmt.Errorf("%s verification gate %d requires an executable command and expected evidence", node.Key, index+1)
			}
			for _, key := range gate.RequirementKeys {
				if _, ok := requirements[strings.ToUpper(key)]; !ok {
					return fmt.Errorf("%s verification gate %d references unknown requirement %s", node.Key, index+1, key)
				}
			}
			for _, key := range gate.ObligationKeys {
				if len(obligationIDs) == 0 {
					return fmt.Errorf("%s verification gate %d references obligation %s but no approved Contract exists on this Work Item", node.Key, index+1, key)
				}
				if !obligationIDs[key] {
					return fmt.Errorf("%s verification gate %d references unknown Contract obligation %s", node.Key, index+1, key)
				}
			}
			if seams != nil && !seams[gate.Seam] {
				return fmt.Errorf("%s verification gate %d references seam %q which the approved Blueprint does not declare", node.Key, index+1, gate.Seam)
			}
		}
	}
	return nil
}

// approvedBlueprintSeams returns the declared verification seams of the
// approved Blueprint on the same planning lineage. The caller gates on the
// planning profile carrying a Blueprint stage; reaching this function without
// one fails closed on the missing approval.
func approvedBlueprintSeams(db databaseQueryer, workItemID string) (map[string]bool, error) {
	stages, err := planningStagesForWorkItem(db, workItemID)
	if err != nil {
		return nil, err
	}
	if !contains(stages, "blueprint") {
		return nil, errors.New("Task Graph policy v2 requires an approved Blueprint with verification seams")
	}
	var content string
	if err := db.QueryRow(`SELECT a.content FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.work_item_id=? AND c.stage='blueprint' AND c.decision_type='approved' ORDER BY c.artifact_revision DESC LIMIT 1`, workItemID).Scan(&content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("Task Graph policy v2 requires an approved Blueprint with verification seams")
		}
		return nil, err
	}
	return blueprintSeamSet(content)
}

// validateTaskGraphSourceContractBinding fails a decomposition-policy-v2 Task
// Graph closed unless it binds the exact approved Contract lineage — artifact
// id, revision, and content hash — mirroring the Contract's source_blueprint
// binding. v1 graphs pass through unchanged.
func validateTaskGraphSourceContractBinding(db databaseQueryer, workItemID string, policyVersion int, binding *tip.ArtifactLineage) error {
	if policyVersion != 2 {
		return nil
	}
	if binding == nil || binding.ArtifactID == "" || binding.Revision < 1 || binding.ContentHash == "" {
		return errors.New("Task Graph policy v2 must bind the approved Contract artifact id, revision, and content hash in source_contract")
	}
	var artifactID, contentHash string
	var revision int
	if err := db.QueryRow(`SELECT c.artifact_id,c.artifact_revision,c.content_hash FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.work_item_id=? AND c.stage='contracts' AND c.decision_type='approved' ORDER BY c.artifact_revision DESC LIMIT 1`, workItemID).Scan(&artifactID, &revision, &contentHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("Task Graph policy v2 requires an approved Contract on the same planning lineage")
		}
		return err
	}
	if binding.ArtifactID != artifactID || binding.Revision != revision || binding.ContentHash != contentHash {
		return fmt.Errorf("Task Graph policy v2 must bind the approved Contract %s@%d (%s), got %s@%d (%s)", artifactID, revision, contentHash, binding.ArtifactID, binding.Revision, binding.ContentHash)
	}
	return nil
}

// validateTaskGraphSourceContractBindingJSON parses the minimal binding fields
// from a raw task-graph artifact for the save path, which validates lineage
// without running full graph validation (drafts may still be incomplete).
func validateTaskGraphSourceContractBindingJSON(db databaseQueryer, workItemID, content string) error {
	var document struct {
		DecompositionPolicyVersion int                  `json:"decomposition_policy_version"`
		SourceContract             *tip.ArtifactLineage `json:"source_contract"`
	}
	if err := json.Unmarshal([]byte(content), &document); err != nil {
		return nil // malformed JSON is rejected by full graph validation
	}
	return validateTaskGraphSourceContractBinding(db, workItemID, document.DecompositionPolicyVersion, document.SourceContract)
}

func validateTaskGraphRequirementCoverage(db databaseQueryer, workItemID string, plan tip.TaskPlanDocument) (map[string]tip.RequirementSnapshot, error) {
	// Materialized children inherit requirement coverage from their root parent.
	requirements, err := queryMaps(db, `SELECT id,requirement_key,title,description,acceptance_criteria FROM requirements WHERE (epic_id=? OR task_id=? OR task_id IN (SELECT root_work_item_id FROM work_item_materializations WHERE work_item_id=?)) AND status!='deferred'`, workItemID, workItemID, workItemID)
	if err != nil {
		return nil, err
	}
	known, covered := map[string]tip.RequirementSnapshot{}, map[string]bool{}
	for _, requirement := range requirements {
		key := fmt.Sprint(requirement["requirement_key"])
		if err := validateGherkinSteps(fmt.Sprint(requirement["acceptance_criteria"])); err != nil {
			return nil, fmt.Errorf("%s acceptance criteria %w", key, err)
		}
		snapshot := tip.RequirementSnapshot{RequirementID: fmt.Sprint(requirement["id"]), RequirementKey: key, Title: fmt.Sprint(requirement["title"]), Description: fmt.Sprint(requirement["description"]), AcceptanceCriteria: fmt.Sprint(requirement["acceptance_criteria"])}
		snapshot.SourceHash = hashJSON(map[string]any{"id": snapshot.RequirementID, "key": snapshot.RequirementKey, "title": snapshot.Title, "description": snapshot.Description, "acceptance_criteria": snapshot.AcceptanceCriteria})
		known[strings.ToUpper(key)] = snapshot
	}
	for _, node := range plan.Nodes {
		if len(node.RequirementKeys) > 2 {
			return nil, fmt.Errorf("%s has more than two requirement_keys; split the node", node.Key)
		}
		for _, key := range node.RequirementKeys {
			normalized := strings.ToUpper(key)
			if _, ok := known[normalized]; !ok {
				return nil, fmt.Errorf("%s references unknown requirement %s", node.Key, key)
			}
			covered[normalized] = true
		}
	}
	missing := []string{}
	for normalized, requirement := range known {
		if !covered[normalized] {
			missing = append(missing, requirement.RequirementKey)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return nil, fmt.Errorf("task graph missing requirements: %s", strings.Join(missing, ", "))
	}
	return known, nil
}

func workItemMaterialize(db *sql.DB, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: pic work-item materialize <id>")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var checkpointID, graphArtifactID, graphContentHash, content string
	var graphRevision int
	if err = tx.QueryRow(`SELECT c.id,c.artifact_id,c.artifact_revision,c.content_hash,a.content FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.work_item_id=? AND c.stage='task_graph' AND c.decision_type='approved' ORDER BY c.artifact_revision DESC LIMIT 1`, args[0]).Scan(&checkpointID, &graphArtifactID, &graphRevision, &graphContentHash, &content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("current task graph is not approved")
		}
		return err
	}
	// Materialization re-runs the full approval-time validation — including the
	// decomposition policy rules — so a graph can never materialize under rules
	// it was not approved against.
	plan, err := validateTaskGraphArtifact(tx, args[0], content)
	if err != nil {
		return err
	}
	var rootKind, rootParent string
	if err = tx.QueryRow(`SELECT type,COALESCE(parent_id,'') FROM work_items WHERE id=?`, args[0]).Scan(&rootKind, &rootParent); err != nil {
		return err
	}
	if contains([]string{"task", "bug", "chore"}, rootKind) && rootParent == "" {
		if len(plan.Nodes) != 1 || plan.Nodes[0].Type != rootKind || plan.Nodes[0].ParentKey != "" || len(plan.Nodes[0].DependsOn) != 0 {
			return errors.New("standalone task graph requires exactly one matching executable node without parent or dependencies")
		}
		var existing string
		_ = tx.QueryRow(`SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND checkpoint_id=? AND node_key=?`, args[0], checkpointID, plan.Nodes[0].Key).Scan(&existing)
		if existing == "" {
			if _, err = tx.Exec(`INSERT INTO work_item_materializations(root_work_item_id,checkpoint_id,node_key,work_item_id) VALUES(?,?,?,?)`, args[0], checkpointID, plan.Nodes[0].Key, args[0]); err != nil {
				return err
			}
		}
		// The standalone projection is the Work Item itself; refresh its source
		// lineage whenever this checkpoint materializes (idempotent per revision).
		if _, err = tx.Exec(`UPDATE work_items SET decomposition_mode=?,decomposition_reason=?,paired_contract_node=?,source_graph_artifact_id=?,source_graph_revision=?,source_graph_content_hash=? WHERE id=?`, decompositionModeOf(plan.Nodes[0].DecompositionMode), plan.Nodes[0].ExceptionReason, plan.Nodes[0].PairedContractNode, graphArtifactID, graphRevision, graphContentHash, args[0]); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		reused := 0
		if existing != "" {
			reused = 1
		}
		writeJSON(os.Stdout, map[string]any{"work_item_id": args[0], "checkpoint_id": checkpointID, "created": 0, "reused": reused, "total": 1})
		return nil
	}
	ids := map[string]string{}
	created, reused := 0, 0
	for _, node := range plan.Nodes {
		var existing string
		_ = tx.QueryRow(`SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND checkpoint_id=? AND node_key=?`, args[0], checkpointID, node.Key).Scan(&existing)
		if existing != "" {
			ids[node.Key] = existing
			continue
		}
		_ = tx.QueryRow(`SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND node_key=? ORDER BY created_at DESC,rowid DESC LIMIT 1`, args[0], node.Key).Scan(&existing)
		if existing != "" {
			if _, err = tx.Exec(`INSERT INTO work_item_materializations(root_work_item_id,checkpoint_id,node_key,work_item_id) VALUES(?,?,?,?)`, args[0], checkpointID, node.Key, existing); err != nil {
				return err
			}
			ids[node.Key], reused = existing, reused+1
			continue
		}
		parentID := args[0]
		if node.ParentKey != "" {
			parentID = ids[node.ParentKey]
			if parentID == "" {
				return fmt.Errorf("parent %s must precede child %s", node.ParentKey, node.Key)
			}
		}
		kind := node.Type
		if kind == "" {
			kind = "task"
		}
		priority := map[string]string{"P0": "high", "P1": "medium", "P2": "low"}[strings.ToUpper(node.Priority)]
		if priority == "" {
			priority = "medium"
		}
		workItemID := "wi-" + shortID()
		if _, err = tx.Exec(`INSERT INTO work_items(id,type,parent_id,title,description,priority,decomposition_mode,decomposition_reason,paired_contract_node,source_graph_artifact_id,source_graph_revision,source_graph_content_hash) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, workItemID, kind, parentID, node.Name, node.Goal, priority, decompositionModeOf(node.DecompositionMode), node.ExceptionReason, node.PairedContractNode, graphArtifactID, graphRevision, graphContentHash); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO work_item_materializations(root_work_item_id,checkpoint_id,node_key,work_item_id) VALUES(?,?,?,?)`, args[0], checkpointID, node.Key, workItemID); err != nil {
			return err
		}
		ids[node.Key], created = workItemID, created+1
	}
	for _, node := range plan.Nodes {
		for _, dependency := range node.DependsOn {
			if _, err = tx.Exec(`INSERT OR IGNORE INTO work_item_relations(id,work_item_id,relation_type,related_work_item_id,rationale) VALUES(?,?,'blocks',?,?)`, "wir-"+shortID(), ids[node.Key], ids[dependency], node.DependsOnRationale[dependency]); err != nil {
				return err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	writeJSON(os.Stdout, map[string]any{"work_item_id": args[0], "checkpoint_id": checkpointID, "created": created, "reused": reused, "total": len(plan.Nodes)})
	return nil
}

func workItemAuthorize(db *sql.DB, args []string) error {
	if len(args) < 2 || args[1] == "" {
		return errors.New("usage: pic work-item authorize <id> <owner> [--branch-name <branch> --base-branch <branch> --base-commit <sha>]")
	}
	if err := validateWorkflowActor(args[1], "owner"); err != nil {
		return errors.New("implementation authorization requires actor_role=owner")
	}
	opts, err := parseOptions(args[2:])
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var checkpointID, content string
	if err = tx.QueryRow(`SELECT c.id,a.content FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.work_item_id=? AND c.stage='task_graph' AND c.decision_type='approved' ORDER BY c.artifact_revision DESC LIMIT 1`, args[0]).Scan(&checkpointID, &content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("current task graph is not approved")
		}
		return err
	}
	plan, err := tip.ParseTaskPlanJSON("```task-plan-json\n" + content + "\n```")
	if err != nil {
		return err
	}
	if _, err = validateTaskGraphRequirementCoverage(tx, args[0], plan); err != nil {
		return err
	}
	var materialized int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM work_item_materializations WHERE root_work_item_id=? AND checkpoint_id=?`, args[0], checkpointID).Scan(&materialized); err != nil {
		return err
	}
	if materialized == 0 {
		return errors.New("current task graph is not materialized")
	}
	if _, err = tx.Exec(`UPDATE implementation_authorizations SET revoked_at=datetime('now') WHERE work_item_id=? AND revoked_at='' AND task_graph_checkpoint_id!=?`, args[0], checkpointID); err != nil {
		return err
	}
	var authorizationID string
	err = tx.QueryRow(`SELECT id FROM implementation_authorizations WHERE work_item_id=? AND task_graph_checkpoint_id=? AND revoked_at='' ORDER BY created_at DESC,rowid DESC LIMIT 1`, args[0], checkpointID).Scan(&authorizationID)
	if errors.Is(err, sql.ErrNoRows) {
		authorizationID = "wiauth-" + shortID()
		if _, err = tx.Exec(`INSERT INTO implementation_authorizations(id,work_item_id,task_graph_checkpoint_id,authorized_by) VALUES(?,?,?,?)`, authorizationID, args[0], checkpointID, args[1]); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE implementation_authorizations SET revoked_at=datetime('now') WHERE work_item_id=? AND task_graph_checkpoint_id=? AND revoked_at='' AND id!=?`, args[0], checkpointID, authorizationID); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE work_item_instruction_packs SET status='stale',stale_at=datetime('now') WHERE status='active' AND checkpoint_id!=? AND work_item_id IN (SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=?)`, checkpointID, args[0]); err != nil {
		return err
	}

	var kind string
	if err = tx.QueryRow(`SELECT type FROM work_items WHERE id=?`, args[0]).Scan(&kind); err != nil {
		return err
	}
	var branchLabel, coordinationLabel, childCount int
	_ = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM work_item_labels WHERE work_item_id=? AND label='integration:branch')`, args[0]).Scan(&branchLabel)
	_ = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM work_item_labels WHERE work_item_id=? AND label='integration:coordination')`, args[0]).Scan(&coordinationLabel)
	if branchLabel != 0 && coordinationLabel != 0 {
		return errors.New("aggregate cannot have both integration:branch and integration:coordination labels")
	}
	_ = tx.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=?`, args[0]).Scan(&childCount)
	mode := "coordination"
	if branchLabel != 0 || (coordinationLabel == 0 && (kind == "feature" || (contains([]string{"task", "bug", "chore"}, kind) && childCount > 0))) {
		mode = "branch"
	}
	var branchAncestor int
	if err = tx.QueryRow(`WITH RECURSIVE ancestors(id,parent_id) AS (
		SELECT id,parent_id FROM work_items WHERE id=(SELECT parent_id FROM work_items WHERE id=?)
		UNION ALL SELECT wi.id,wi.parent_id FROM work_items wi JOIN ancestors a ON wi.id=a.parent_id
	) SELECT EXISTS(SELECT 1 FROM ancestors a JOIN work_item_delivery_states d ON d.work_item_id=a.id WHERE d.integration_mode='branch')`, args[0]).Scan(&branchAncestor); err != nil {
		return err
	}
	if branchAncestor != 0 {
		if branchLabel != 0 {
			return errors.New("nested aggregate cannot own a branch beneath a branch-owning ancestor")
		}
		mode = "coordination"
	}
	branchName, baseBranch, baseCommit := "", "develop", ""
	if mode == "branch" {
		branchName, baseBranch, baseCommit = opts["branch-name"], opts["base-branch"], opts["base-commit"]
		if branchName == "" || baseBranch == "" || baseCommit == "" || branchName == "HEAD" || branchName == baseBranch {
			return errors.New("branch-owning aggregate authorization requires a non-base branch, base branch, and base commit")
		}
	}
	var existingMode, existingBranch, existingBase string
	deliveryErr := tx.QueryRow(`SELECT integration_mode,branch_name,base_branch FROM work_item_delivery_states WHERE work_item_id=?`, args[0]).Scan(&existingMode, &existingBranch, &existingBase)
	if deliveryErr == nil && (existingMode != mode || existingBranch != branchName || existingBase != baseBranch) {
		return errors.New("delivery authority is already bound to a different integration mode or branch")
	}
	if errors.Is(deliveryErr, sql.ErrNoRows) {
		if _, err = tx.Exec(`INSERT INTO work_item_delivery_states(work_item_id,integration_mode,branch_name,base_branch,base_commit) VALUES(?,?,?,?,?)`, args[0], mode, branchName, baseBranch, baseCommit); err != nil {
			return err
		}
	} else if deliveryErr != nil {
		return deliveryErr
	}
	activated := int64(0)
	if err = tx.Commit(); err != nil {
		return err
	}
	writeJSON(os.Stdout, map[string]any{"id": authorizationID, "work_item_id": args[0], "checkpoint_id": checkpointID, "activated": activated, "integration_mode": mode, "branch_name": branchName, "base_branch": baseBranch, "base_commit": baseCommit})
	return nil
}

func validateWorkflowActor(actual, expected string) error {
	if actual != expected {
		return errors.New("invalid actor role")
	}
	if os.Getenv("PI_TASK_AGENT_NAME") != "" {
		return errors.New("child agents cannot assume workflow authority")
	}
	return nil
}

func workItemUpdate(db *sql.DB, args []string) error {
	if len(args) < 2 {
		return errors.New("usage: pic work-item update <id> [--title <text>] [--description <text>] [--parent <id>]")
	}
	opts, err := parseOptions(args[1:])
	if err != nil {
		return err
	}
	if len(opts) == 0 {
		return errors.New("no fields to update")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = workItemByIDTx(tx, args[0]); err != nil {
		return err
	}
	if parent, ok := opts["parent"]; ok {
		if err = validateWorkItemParent(tx, args[0], parent); err != nil {
			return err
		}
	}
	sets, values := []string{}, []any{}
	for _, field := range []string{"title", "description", "priority"} {
		if value, ok := opts[field]; ok {
			sets = append(sets, field+"=?")
			values = append(values, value)
		}
	}
	if parent, ok := opts["parent"]; ok {
		sets = append(sets, "parent_id=NULLIF(?,'')")
		values = append(values, parent)
	}
	if len(sets) == 0 {
		return errors.New("no supported fields to update")
	}
	values = append(values, args[0])
	if _, err = tx.Exec(`UPDATE work_items SET `+strings.Join(sets, ",")+` WHERE id=?`, values...); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	item, err := workItemByID(db, args[0])
	if err == nil {
		writeJSON(os.Stdout, item)
	}
	return err
}

func validateWorkItemParent(tx *sql.Tx, id, parentID string) error {
	if parentID == "" {
		return nil
	}
	var kind string
	if err := tx.QueryRow(`SELECT type FROM work_items WHERE id=?`, parentID).Scan(&kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("Parent Work Item %s not found", parentID)
		}
		return err
	}
	if kind != "epic" && kind != "feature" {
		return fmt.Errorf("%s Work Items cannot contain children", kind)
	}
	if id == "" {
		return nil
	}
	var cycle int
	err := tx.QueryRow(`WITH RECURSIVE ancestors(id) AS (
		SELECT ? UNION ALL SELECT parent_id FROM work_items JOIN ancestors ON work_items.id=ancestors.id WHERE parent_id IS NOT NULL
	) SELECT EXISTS(SELECT 1 FROM ancestors WHERE id=?)`, parentID, id).Scan(&cycle)
	if err != nil {
		return err
	}
	if cycle != 0 {
		return errors.New("containment cycle")
	}
	return nil
}

func workItemByID(db *sql.DB, id string) (map[string]any, error) {
	rows, err := queryMaps(db, `SELECT `+workItemColumns+` FROM work_items WHERE id=?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("Work Item %s not found", id)
	}
	if err := attachWorkItemLabels(db, rows); err != nil {
		return nil, err
	}
	return rows[0], nil
}

func workItemByIDTx(tx *sql.Tx, id string) (map[string]any, error) {
	rows, err := queryMaps(tx, `SELECT `+workItemColumns+` FROM work_items WHERE id=?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("Work Item %s not found", id)
	}
	return rows[0], nil
}
