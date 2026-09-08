package pipeline

// Pipeline run lifecycle. pipeline_runs rows are the scheduler's durable
// state machine: a claim creates the row with a lease, checkpoint/complete
// transitions record progress, and terminal states carry the artifacts the
// review and integration paths consume. All SQL lives here; the pi-ext
// scheduler drives it through the pic CLI.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/earendil-works/task-system/go-pic/internal/store"
	workitem "github.com/earendil-works/task-system/go-pic/internal/work-item"
)

func EscalationSave(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("usage: pic workflow escalation-save <task-id> --pipeline-run-id <id> --report-json <json>")
	}
	taskID := args[0]
	opts, err := store.ParseOptions(args[1:])
	if err != nil || opts["pipeline-run-id"] == "" || opts["report-json"] == "" {
		return errors.New("escalation-save requires --pipeline-run-id and --report-json")
	}
	var report map[string]any
	if err = json.Unmarshal([]byte(opts["report-json"]), &report); err != nil {
		return fmt.Errorf("escalation report must be valid JSON: %w", err)
	}
	level, _ := report["level"].(string)
	if level != "L2" && level != "L3" {
		return errors.New("escalation report requires level L2 or L3")
	}
	// Presence-only audit floor: the artifact-contradiction test stays auditable.
	checkedSources, _ := report["checked_sources"].([]any)
	if len(checkedSources) == 0 {
		return errors.New("escalation report requires a nonempty checked_sources list")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var packID, packHash string
	var packVersion int
	query := `SELECT p.id,p.version,p.content_hash FROM work_item_instruction_packs p JOIN pipeline_runs r ON r.instruction_pack_id=p.id AND r.instruction_pack_version=p.version AND r.instruction_pack_hash=p.content_hash WHERE p.work_item_id=? AND p.status='active' AND r.id=? AND r.task_id=p.work_item_id AND r.stage IN ('worker','autofix') AND r.status IN ('claimed','running')`
	if err = tx.QueryRow(query, taskID, opts["pipeline-run-id"]).Scan(&packID, &packVersion, &packHash); err != nil {
		return errors.New("escalation requires an active worker run bound to the Work Item TIP")
	}
	id := "wies-" + store.ShortID()
	if _, err = tx.Exec(`INSERT INTO work_item_escalations(id,work_item_id,pipeline_run_id,instruction_pack_id,instruction_pack_version,instruction_pack_hash,level,status,report_json) VALUES(?,?,?,?,?,?,?,'open',?)`, id, taskID, opts["pipeline-run-id"], packID, packVersion, packHash, level, store.NormalizeJSONText(opts["report-json"])); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE pipeline_runs SET status='blocked',error=?,updated_at=datetime('now'),completed_at=datetime('now') WHERE id=? AND status IN ('claimed','running')`, "escalation: "+level, opts["pipeline-run-id"])
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("escalation rejected: run is not claimable")
	}
	if _, err = tx.Exec(`UPDATE work_items SET status='open',claimed_at='',claimed_by='' WHERE id=? AND status='in_progress'`, taskID); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,summary,payload_json) VALUES(?,?,'worker_escalated','worker',?,?)`, "wie-"+store.ShortID(), taskID, "worker escalated "+level+" on TIP "+packID, store.NormalizeJSONText(opts["report-json"])); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return store.OutputOne(db, `SELECT * FROM work_item_escalations WHERE id=?`, id)
}

func EscalationResolve(db *sql.DB, args []string) error {
	if len(args) < 3 {
		return errors.New("usage: pic workflow escalation-resolve <task-id> <escalation-id> <resolution-json> --actor-role contractor")
	}
	taskID, escalationID := args[0], args[1]
	opts, err := store.ParseOptions(args[3:])
	if err != nil || workitem.ValidateWorkflowActor(opts["actor-role"], "contractor") != nil {
		return errors.New("escalation resolution requires actor_role=contractor")
	}
	var resolution map[string]any
	if err = json.Unmarshal([]byte(args[2]), &resolution); err != nil {
		return fmt.Errorf("escalation resolution must be valid JSON: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var packID string
	if err = tx.QueryRow(`SELECT instruction_pack_id FROM work_item_escalations WHERE id=? AND work_item_id=? AND status='open'`, escalationID, taskID).Scan(&packID); err != nil {
		return errors.New("escalation resolution requires an open escalation for this Work Item")
	}
	result, err := tx.Exec(`UPDATE work_item_escalations SET status='resolved',resolution_json=?,resolved_by=?,resolved_at=datetime('now') WHERE id=? AND status='open'`, store.NormalizeJSONText(args[2]), opts["actor-role"], escalationID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("escalation resolution rejected: already resolved")
	}
	if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,summary,payload_json) VALUES(?,?,'escalation_resolved','contractor',?,?)`, "wie-"+store.ShortID(), taskID, "escalation "+escalationID+" resolved on TIP "+packID, store.NormalizeJSONText(args[2])); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return store.OutputOne(db, `SELECT * FROM work_item_escalations WHERE id=?`, escalationID)
}

// Round-cap blocking constraint: when a bounded review-fix loop exhausts its
// round budget (three completed fix rounds without a passed review), the next
// fix must not relaunch automatically. This command makes that block durable by
// elevating the latest completed failed review to require owner action (the same
// durable signal the claim and workflow-status gates already honor for
// owner-approval-required verdicts) and appending a persistent owner-action
// event with the synthesized summary, so the block survives reconciliation.

func ReviewFixBlock(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("usage: pic workflow review-fix-block <task-id> [--summary <text>]")
	}
	opts, err := store.ParseOptions(args[1:])
	if err != nil {
		return err
	}
	taskID := args[0]
	summary := opts["summary"]
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var reviewID string
	err = tx.QueryRow(`SELECT id FROM pipeline_runs WHERE task_id=? AND stage='review' AND status='completed' AND json_valid(result_json) AND json_extract(result_json,'$.review_status')='failed' ORDER BY rowid DESC LIMIT 1`, taskID).Scan(&reviewID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("review-fix block requires a completed failed review for the task")
	}
	if err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE pipeline_runs SET result_json=json_set(result_json, '$.owner_approval_required', json('true'), '$.review_fix_round_cap', ?), updated_at=datetime('now') WHERE id=? AND status='completed' AND stage='review' AND json_valid(result_json)`, summary, reviewID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("review-fix block rejected: completed failed review not mutable")
	}
	if err = store.AddEvent(tx, taskID, "review_fix_round_cap", "orchestrator", summary, map[string]any{"pipeline_run_id": reviewID, "owner_action_required": true, "summary": summary}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return store.OutputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, reviewID)
}

// Owner review-decision constraint: a failed review carrying
// owner_approval_required=true durably stops the scheduler (the review-fix claim
// gate and nextPipelineStage both honor the flag), and only
// ReviewFixBlock ever set that flag — so a reviewer-flagged verdict had
// no owner resolution instrument. This command records the owner's fix decision:
// it validates the flagged review, clears the durable flag, and appends an
// owner_review_decision audit event whose after_attempt baseline resets the
// review-fix cycle counters.

func ReviewDecision(db *sql.DB, args []string) error {
	if len(args) < 3 {
		return errors.New("usage: pic workflow review-decision <task-id> <review-run-id> fix --notes <text> --actor-role owner")
	}
	taskID, reviewID, decision := args[0], args[1], args[2]
	opts, err := store.ParseOptions(args[3:])
	if err != nil {
		return err
	}
	if workitem.ValidateWorkflowActor(opts["actor-role"], "owner") != nil {
		return errors.New("review-decision requires actor_role=owner")
	}
	if decision != "fix" {
		return errors.New("review-decision supports only decision 'fix'; deferral has no execution state model")
	}
	if opts["notes"] == "" {
		return errors.New("review-decision requires --notes")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var candidateRunID string
	err = tx.QueryRow(`SELECT json_extract(result_json,'$.candidate_run_id') FROM pipeline_runs WHERE id=? AND task_id=? AND stage='review' AND status='completed' AND json_valid(result_json) AND json_extract(result_json,'$.review_status')='failed' AND COALESCE(json_extract(result_json,'$.owner_approval_required'),0)!=0`, reviewID, taskID).Scan(&candidateRunID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("review-decision requires a completed failed review with owner_approval_required for this Work Item")
	}
	if err != nil {
		return err
	}
	var candidateAttempt int
	if candidateRunID != "" {
		_ = tx.QueryRow(`SELECT attempt FROM pipeline_runs WHERE id=? AND task_id=?`, candidateRunID, taskID).Scan(&candidateAttempt)
	}
	result, err := tx.Exec(`UPDATE pipeline_runs SET result_json=json_set(result_json, '$.owner_approval_required', json('false')), updated_at=datetime('now') WHERE id=? AND status='completed' AND stage='review' AND json_valid(result_json)`, reviewID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("review-decision rejected: completed failed review not mutable")
	}
	if err = store.AddEvent(tx, taskID, "owner_review_decision", "owner", opts["notes"], map[string]any{"pipeline_run_id": reviewID, "decision": decision, "candidate_run_id": candidateRunID, "after_attempt": candidateAttempt}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return store.OutputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, reviewID)
}
