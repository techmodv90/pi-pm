package workitem

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
)

func AggregateVerify(db *sql.DB, args []string) error {
	if len(args) < 3 || !contains([]string{"passed", "failed", "partial", "blocked"}, args[1]) {
		return errors.New("usage: pic work-item aggregate-verify <id> <status> <summary> --actor-role contractor")
	}
	opts, err := parseOptions(args[3:])
	if err != nil || ValidateWorkflowActor(opts["actor-role"], "contractor") != nil {
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
func AggregateAccept(db *sql.DB, args []string) error {
	if len(args) < 4 || !contains([]string{"accepted", "rejected"}, args[2]) {
		return errors.New("usage: pic work-item aggregate-accept <id> <verification-report-id> <accepted|rejected> <notes> --actor-role owner")
	}
	opts, err := parseOptions(args[4:])
	if err != nil || ValidateWorkflowActor(opts["actor-role"], "owner") != nil {
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
func AggregateMergeResult(db *sql.DB, args []string) error {
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
func AggregateClose(db *sql.DB, args []string) error {
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
	item, err := ByID(db, args[0])
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
