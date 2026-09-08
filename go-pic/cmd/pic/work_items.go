package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	if agent := os.Getenv("PI_TASK_AGENT_NAME"); agent != "" && !contains([]string{"list", "show", "artifact-save", "workflow-status"}, args[0]) {
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
			err = attachWorkItemGraph(db, item)
		}
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
	case "execution-reset":
		return workItemExecutionReset(db, args[1:])
	case "workflow-status":
		return workItemWorkflowStatus(db, args[1:])
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
	// State-driven routing mirrors the claim path: a lean task (no pack, no
	// materialization) binds its review to the latest completed pack-free
	// candidate run; legacy tasks keep the TIP-bound triple match.
	var legacyState int
	if err = db.QueryRow(`SELECT (SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=? AND status='active') + (SELECT COUNT(*) FROM work_item_materializations WHERE work_item_id=?)`, args[0], args[0]).Scan(&legacyState); err != nil {
		return err
	}
	query := `UPDATE work_items SET review_status=?,review_notes=? WHERE id=? AND type IN ('task','bug','chore') AND EXISTS (
		SELECT 1 FROM pipeline_runs review
		JOIN work_item_instruction_packs pack ON pack.work_item_id=review.task_id AND pack.status='active' AND pack.id=review.instruction_pack_id AND pack.version=review.instruction_pack_version AND pack.content_hash=review.instruction_pack_hash
		JOIN pipeline_runs candidate ON candidate.id=review.candidate_run_id AND candidate.task_id=review.task_id AND candidate.instruction_pack_id=review.instruction_pack_id AND candidate.instruction_pack_version=review.instruction_pack_version AND candidate.instruction_pack_hash=review.instruction_pack_hash AND candidate.integrated_patch_hash=review.candidate_patch_hash
		WHERE review.id=? AND review.task_id=work_items.id AND review.stage='review' AND review.status='completed' AND json_valid(review.result_json) AND json_extract(review.result_json,'$.review_status')=?
		AND candidate.rowid=(SELECT MAX(current.rowid) FROM pipeline_runs current WHERE current.task_id=review.task_id AND current.stage IN ('worker','autofix') AND current.status='completed' AND current.instruction_pack_id=review.instruction_pack_id AND current.instruction_pack_version=review.instruction_pack_version AND current.instruction_pack_hash=review.instruction_pack_hash AND current.artifact_saved_at<>''))`
	if legacyState == 0 {
		query = `UPDATE work_items SET review_status=?,review_notes=? WHERE id=? AND type IN ('task','bug','chore') AND EXISTS (
			SELECT 1 FROM pipeline_runs review
			JOIN pipeline_runs candidate ON candidate.id=review.candidate_run_id AND candidate.task_id=review.task_id AND candidate.integrated_patch_hash=review.candidate_patch_hash
			WHERE review.id=? AND review.task_id=work_items.id AND review.stage='review' AND review.status='completed' AND review.instruction_pack_id='' AND json_valid(review.result_json) AND json_extract(review.result_json,'$.review_status')=?
			AND candidate.rowid=(SELECT MAX(current.rowid) FROM pipeline_runs current WHERE current.task_id=review.task_id AND current.stage IN ('worker','autofix') AND current.status='completed' AND current.instruction_pack_id='' AND current.artifact_saved_at<>''))`
	}
	result, err := db.Exec(query, args[1], opts["notes"], args[0], opts["pipeline-run-id"], args[1])
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
	// Lean path: a task with no pack and no materialization closes with a
	// status flip plus a completed event — no completion-report row (its pack
	// columns are NOT NULL by design); the pipeline run is the evidence.
	var legacyState int
	if err = tx.QueryRow(`SELECT (SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=? AND status='active') + (SELECT COUNT(*) FROM work_item_materializations WHERE work_item_id=?)`, args[0], args[0]).Scan(&legacyState); err != nil {
		return err
	}
	if legacyState == 0 {
		if status != "done" {
			return errors.New("lean completion supports done; failures revert through pipeline-complete")
		}
		evidenceQuery := `SELECT 1 FROM pipeline_runs WHERE id=? AND task_id=? AND stage IN ('worker','autofix') AND status='completed' AND integrated_at<>'' AND integrated_patch_hash<>''`
		var ok int
		if err = tx.QueryRow(evidenceQuery, opts["pipeline-run-id"], args[0]).Scan(&ok); err != nil {
			return errors.New("lean completion requires a completed integrated pipeline run for this task")
		}
		result, err := tx.Exec(`UPDATE work_items SET status='done',claimed_at='',claimed_by='',review_status='passed' WHERE id=? AND type IN ('task','bug','chore')`, args[0])
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return errors.New("lean completion requires an executable Work Item")
		}
		payload, _ := json.Marshal(map[string]any{"pipeline_run_id": opts["pipeline-run-id"], "summary": opts["summary"]})
		if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,summary,payload_json) VALUES(?,?,'completed','contractor',?,?)`, "wie-"+shortID(), args[0], opts["summary"], string(payload)); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		return outputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, opts["pipeline-run-id"])
	}
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
	// Lean path: verification binds to the task and its completed pack-free run
	// (completion_report_id stays NULL); a failed verification reopens the task
	// and records a verification_failed event for fix routing without pack caps.
	var legacyState int
	if err = tx.QueryRow(`SELECT (SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=? AND status='active') + (SELECT COUNT(*) FROM work_item_materializations WHERE work_item_id=?)`, args[0], args[0]).Scan(&legacyState); err != nil {
		return err
	}
	if legacyState == 0 {
		var runOK int
		if err = tx.QueryRow(`SELECT 1 FROM pipeline_runs WHERE id=? AND task_id=? AND status='completed' AND instruction_pack_id=''`, args[1], args[0]).Scan(&runOK); err != nil {
			return errors.New("lean verification requires a completed pack-free pipeline run for this task")
		}
		id := "wivr-" + shortID()
		var pipelineHighWaterRowID int64
		if err = tx.QueryRow(`SELECT COALESCE(MAX(rowid),0) FROM pipeline_runs WHERE task_id=?`, args[0]).Scan(&pipelineHighWaterRowID); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO work_item_verification_reports(id,work_item_id,completion_report_id,status,summary,verified_by_role,pipeline_high_water_rowid) VALUES(?,?,NULL,?,?,?,?)`, id, args[0], args[2], args[3]+" (lean run "+args[1]+")", opts["actor-role"], pipelineHighWaterRowID); err != nil {
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
			if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,summary,payload_json) VALUES(?,?,'verification_failed','contractor',?,?)`, "wie-"+shortID(), args[0], args[3], `{"pipeline_run_id":"`+args[1]+`"}`); err != nil {
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		return outputOne(db, `SELECT * FROM work_item_verification_reports WHERE id=?`, id)
	}
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
		// Lean path: no pack — execution state tracks pack-free runs so the
		// scheduler advances worker → review → close on the description-verbatim
		// contract (mirrors the legacy state machine without pack bindings).
		var leanCandidate string
		_ = db.QueryRow(`SELECT id FROM pipeline_runs WHERE task_id=? AND stage IN ('worker','autofix') AND status='completed' AND instruction_pack_id='' AND artifact_saved_at<>'' AND integrated_patch_hash<>'' ORDER BY rowid DESC LIMIT 1`, id).Scan(&leanCandidate)
		if leanCandidate == "" {
			return state, nil
		}
		state.CandidateID = leanCandidate
		state.NextStage = "review"
		state.PipelineStage = "review"
		var leanReviewStatus string
		var leanOwnerApproval int
		_ = db.QueryRow(`SELECT COALESCE(json_extract(result_json,'$.review_status'),''),COALESCE(json_extract(result_json,'$.owner_approval_required'),0) FROM pipeline_runs WHERE task_id=? AND stage='review' AND status='completed' AND instruction_pack_id='' AND candidate_run_id=? AND json_valid(result_json) AND json_extract(result_json,'$.candidate_patch_hash')=(SELECT integrated_patch_hash FROM pipeline_runs WHERE id=?) ORDER BY rowid DESC LIMIT 1`, id, leanCandidate, leanCandidate).Scan(&leanReviewStatus, &leanOwnerApproval)
		state.ReviewStatus = leanReviewStatus
		state.OwnerApprovalRequired = leanOwnerApproval != 0
		if state.ReviewStatus == "failed" {
			if state.OwnerApprovalRequired {
				state.NextStage = "owner_approval"
				state.PipelineStage = ""
			} else {
				state.NextStage = "implement"
				state.PipelineStage = "worker"
			}
		} else if state.ReviewStatus == "passed" {
			state.NextStage = "contractor_verification"
			state.PipelineStage = ""
		}
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
) OR (
	-- Lean path: no legacy pipeline state at all; the description is the worker input.
	(SELECT COUNT(*) FROM work_item_instruction_packs p WHERE p.work_item_id=wi.id AND p.status='active')=0 AND NOT EXISTS (
		SELECT 1 FROM work_item_materializations m WHERE m.work_item_id=wi.id
	)
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

// Lean artifact stages: rri_t_scenarios is the one retained supplementary
// artifact (saved by the in-session contractor during /apm review; never
// gated by a workflow checkpoint).
var workItemStages = []string{"rri_t_scenarios"}

func workItemArtifactSave(db *sql.DB, args []string) error {
	if len(args) != 3 || !contains(workItemStages, args[1]) || args[2] == "" {
		return errors.New("usage: pic work-item artifact-save <id> <stage> <content>")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = workItemByIDTx(tx, args[0]); err != nil {
		return err
	}
	var revision int
	if err = tx.QueryRow(`SELECT COALESCE(MAX(revision),0)+1 FROM work_item_artifacts WHERE work_item_id=? AND stage=?`, args[0], args[1]).Scan(&revision); err != nil {
		return err
	}
	id, contentHash := "wia-"+shortID(), hashJSON(args[2])
	if _, err = tx.Exec(`INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES(?,?,?,?,?,?)`, id, args[0], args[1], revision, args[2], contentHash); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	writeJSON(os.Stdout, map[string]any{"id": id, "work_item_id": args[0], "stage": args[1], "revision": revision, "content_hash": contentHash})
	return nil
}

func approvedCheckpointDecision(stage string) string {
	if stage == "scan" {
		return "accepted"
	}
	return "approved"
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
	var childCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=?`, args[0]).Scan(&childCount)
	if childCount > 0 {
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
		return workItemExecutionStatus(db, args[0])
	}
	// Childless aggregate under the lean model: no planning flow remains, so
	// the only meaningful next step is gaining executable children.
	writeJSON(os.Stdout, withNextActions(map[string]any{"work_item_id": args[0], "workflow_kind": "aggregate_delivery", "next_stage": "implement"}))
	return nil
}

func indexOfStage(stages []string, stage string) int {
	for index, candidate := range stages {
		if candidate == stage {
			return index
		}
	}
	return -1
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
	// Integration proof surface (RLB-GAP-006): contractor verification needs
	// the persisted evidence that the reviewed candidate actually integrated —
	// integrated_at/hash plus the passing review lineage — without direct SQL.
	writeJSON(os.Stdout, withNextActions(map[string]any{"work_item_id": id, "workflow_kind": "execution", "next_stage": state.NextStage, "pipeline_stage": state.PipelineStage, "active_instruction_pack_id": state.PackID, "candidate_run_id": state.CandidateID, "review_status": state.ReviewStatus, "completion_report_id": state.CompletionID, "verification_status": state.VerificationStatus, "owner_decision": state.OwnerDecision, "current_task_graph": graph, "integration_evidence": workItemIntegrationEvidence(db, id)}))
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
		if scenario.ID == "" || scenario.Persona == "" || !validDimensions[scenario.Dimension] || !validStressAxes[scenario.StressAxis] || scenario.Procedure == "" || scenario.Evidence == "" || !validResults[scenario.Result] {
			return fmt.Errorf("invalid RRI-T scenario for requirement %s", scenario.RequirementID)
		}
		// Lean imported aggregates carry no requirements rows — their requirement
		// truth lives in the companion spec files (.feature scenarios / the
		// .tasks.md Nyquist mapping), re-read by the contractor at review time.
		// The DB gate applies only to planning-era aggregates that have rows.
		if scenario.RequirementID == "" {
			return fmt.Errorf("RRI-T scenario %s requires a requirement_id", scenario.ID)
		}
		if len(approved) > 0 && !approved[scenario.RequirementID] {
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
		if deferred.Persona == "" || !validDimensions[deferred.Dimension] || !validStressAxes[deferred.StressAxis] || strings.TrimSpace(deferred.Reason) == "" {
			return fmt.Errorf("invalid RRI-T not_applicable disposition for requirement %s", deferred.RequirementID)
		}
		if deferred.RequirementID == "" {
			return fmt.Errorf("RRI-T not_applicable disposition %s requires a requirement_id", deferred.ID)
		}
		if len(approved) > 0 && !approved[deferred.RequirementID] {
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
			// Re-verification rebinds the delivery evidence: a stale persisted
			// branch binding (e.g. verify ran from the wrong checkout) must be
			// recoverable, since no other transition can rebind it. Acceptance
			// still validates the unchanged head/base against the working tree.
			if opts["branch-name"] == "" || opts["head-commit"] == "" || opts["base-commit"] == "" {
				return errors.New("branch aggregate verification requires the bound branch name, head commit, and current base commit")
			}
			if _, err = tx.Exec(`UPDATE work_item_delivery_states SET branch_name=?, base_commit=?, verified_head=?, verification_report_id=?, merge_status='',merged_commit='',merge_error='',updated_at=datetime('now') WHERE work_item_id=?`, opts["branch-name"], opts["base-commit"], opts["head-commit"], id, args[0]); err != nil {
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
