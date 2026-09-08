package workitem

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

func Review(db *sql.DB, args []string) error {
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
	return outputOne(db, `SELECT `+Columns+` FROM work_items WHERE id=?`, args[0])
}
func CompletionSave(db *sql.DB, args []string) error {
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
func VerificationSave(db *sql.DB, args []string) error {
	if len(args) < 4 || !contains([]string{"passed", "failed", "partial", "blocked"}, args[2]) {
		return errors.New("usage: pic work-item verification-save <id> <completion-report-id> <passed|failed|partial|blocked> <summary> --actor-role contractor")
	}
	opts, err := parseOptions(args[4:])
	if err != nil || ValidateWorkflowActor(opts["actor-role"], "contractor") != nil {
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
	state, stateErr := LoadExecutionState(tx, args[0])
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
func Accept(db *sql.DB, args []string) error {
	if len(args) < 4 || !contains([]string{"accepted", "rejected"}, args[2]) {
		return errors.New("usage: pic work-item accept <id> <completion-report-id> <accepted|rejected> <notes> --actor-role owner")
	}
	opts, err := parseOptions(args[4:])
	if err != nil || ValidateWorkflowActor(opts["actor-role"], "owner") != nil {
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
	item, err := ByID(db, args[0])
	if err == nil {
		writeJSON(os.Stdout, item)
	}
	return err
}
func validateExecutableClosure(db Store, id string) error {
	return validateExecutableClosureForReport(db, id, "", false)
}
func validateExecutableClosureForReport(db Store, id, expectedCompletion string, requireAcceptance bool) error {
	state, err := LoadExecutionState(db, id)
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

type ExecutionState struct {
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

func LoadExecutionState(db Queryer, id string) (ExecutionState, error) {
	state := ExecutionState{NextStage: "instruction_pack"}
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
func finalizeVerifiedExecutionState(db Queryer, id string, state ExecutionState) ExecutionState {
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
