package pipeline

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/earendil-works/task-system/go-pic/internal/profile"
	stages "github.com/earendil-works/task-system/go-pic/internal/stage"
	"github.com/earendil-works/task-system/go-pic/internal/store"
	workitem "github.com/earendil-works/task-system/go-pic/internal/work-item"
)

const maxAutomaticWorkerAttempts = 3
const maxAutomaticAutofixAttempts = 3
const maxDeterministicWorkerFailuresPerContract = 1

// Pipeline run lifecycle. pipeline_runs rows are the scheduler's durable
// state machine: a claim creates the row with a lease, checkpoint/complete
// transitions record progress, and terminal states carry the artifacts the
// review and integration paths consume. All SQL lives here; the pi-ext
// scheduler drives it through the pic CLI.

func Claim(db *sql.DB, args []string) error {
	if len(args) < 2 {
		return errors.New("pipeline-claim requires task id and stage")
	}
	taskID, stage := args[0], args[1]
	if _, err := workitem.ByID(db, taskID); err != nil {
		return err
	}
	if !stages.Contains(stage) {
		return fmt.Errorf("invalid pipeline stage: %s", stage)
	}
	opts, err := store.ParseOptions(args[2:])
	if err != nil {
		return err
	}
	packID, packVersion, packHash := "", 0, ""
	candidateRunID, candidatePatchHash, reviewFixCycle := "", "", 0
	profileVersion, profileHash := 0, ""
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = expireLeases(tx, taskID, stage); err != nil {
		return err
	}
	// Open-escalation scheduling gate (GAP-138): no claim of any stage may proceed
	// while an unresolved escalation exists for this Work Item.
	// List the blocking IDs so contractors can resolve without hunting through
	// show output; escalation-resolve requires the exact wies-* ID.
	escalationRows, err := tx.Query(`SELECT id FROM work_item_escalations WHERE work_item_id=? AND status='open' ORDER BY datetime(created_at), rowid`, taskID)
	if err != nil {
		return err
	}
	var escalationIDs []string
	for escalationRows.Next() {
		var escalationID string
		if err = escalationRows.Scan(&escalationID); err != nil {
			escalationRows.Close()
			return err
		}
		escalationIDs = append(escalationIDs, escalationID)
	}
	escalationRows.Close()
	if err = escalationRows.Err(); err != nil {
		return err
	}
	if len(escalationIDs) > 0 {
		return fmt.Errorf("pipeline claim rejected: %d open escalation(s) require contractor resolution: %s", len(escalationIDs), strings.Join(escalationIDs, ", "))
	}
	// Resolve the versioned Plan/Implement/QA profile exactly once at the first
	// claim and bind this claim to the persisted profile version and hash.
	lifecycle := profile.LifecycleForStage(stage)
	if lifecycle == "" {
		return fmt.Errorf("invalid pipeline stage: %s", stage)
	}
	profiles, err := profile.Ensure(tx, taskID)
	if err != nil {
		return fmt.Errorf("pipeline claim rejected: resolve lifecycle profiles: %w", err)
	}
	currentProfile := profiles[lifecycle]
	if opts["profile-version"] != "" && opts["profile-version"] != strconv.Itoa(currentProfile.Version) {
		return fmt.Errorf("pipeline claim rejected: stale %s profile version %s (current %d)", lifecycle, opts["profile-version"], currentProfile.Version)
	}
	if opts["profile-hash"] != "" && opts["profile-hash"] != currentProfile.ContentHash {
		return errors.New("pipeline claim rejected: profile hash changed")
	}
	profileVersion, profileHash = currentProfile.Version, currentProfile.ContentHash
	if lifecycle == "plan" {
		planStages := currentProfile.Stages
		planningIndex := workitem.IndexOfStage(planStages, stage)
		if planningIndex < 0 {
			return fmt.Errorf("pipeline claim rejected: stage %s is not part of this Work Item planning profile", stage)
		}
		if planningIndex > 0 {
			for index, requiredStage := range planStages {
				var approved int
				// Claim gating reads only owner-decided checkpoints: a rejected
				// newer revision never clears (or blocks as) an approval.
				if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM workflow_checkpoints WHERE work_item_id=? AND stage=? AND decision_type=?)`, taskID, requiredStage, workitem.ApprovedCheckpointDecision(requiredStage)).Scan(&approved); err != nil {
					return err
				}
				if index < planningIndex && approved == 0 {
					return fmt.Errorf("pipeline claim rejected: current planning stage is %s", requiredStage)
				}
				if index == planningIndex {
					if approved != 0 {
						return fmt.Errorf("pipeline claim rejected: planning stage %s is already approved", stage)
					}
					break
				}
			}
		}
	}
	leanClaim := false
	if stage == "worker" || stage == "review" || stage == "autofix" {
		var activePacks, materializations int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, taskID).Scan(&activePacks); err != nil {
			return err
		}
		if err = tx.QueryRow(`SELECT COUNT(*) FROM work_item_materializations WHERE work_item_id=?`, taskID).Scan(&materializations); err != nil {
			return err
		}
		// State-driven claim routing: a Work Item carrying pack-based planning state
		// (active instruction pack or materialization) keeps the pack-based
		// gates; a Work Item with neither takes the lean path — description is the
		// worker input, no pack, no pack-keyed limiters (owner decision 2026-09-07).
		leanClaim = activePacks == 0 && materializations == 0
		if !leanClaim {
			if stage == "worker" {
				if err = prepareInstructionPackForFirstClaim(tx, taskID); err != nil {
					return fmt.Errorf("pipeline claim rejected: prepare instruction pack: %w", err)
				}
				// Re-count after TIP generation: the first-claim pack is created above.
				if err = tx.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, taskID).Scan(&activePacks); err != nil {
					return err
				}
			}
			if activePacks != 1 {
				return fmt.Errorf("Work Item %s requires exactly one active instruction pack", taskID)
			}
			eligibility := workitem.ReadySQL
			if stage == "review" || (stage == "worker" && opts["review-fix"] == "1") {
				eligibility = `wi.type IN ('task','bug','chore') AND wi.status IN ('open','in_progress') AND wi.deferred=0 AND wi.claimed_at='' AND NOT EXISTS (
					SELECT 1 FROM work_item_relations r JOIN work_items blocker ON blocker.id=r.related_work_item_id WHERE r.work_item_id=wi.id AND r.relation_type='blocks' AND blocker.status!='done'
				) AND NOT EXISTS (
					SELECT 1 FROM work_item_relations r JOIN work_items gate_item ON gate_item.id=r.related_work_item_id WHERE r.work_item_id=wi.id AND r.relation_type='gates' AND gate_item.status!='done'
				)`
			}
			var itemType string
			if err = tx.QueryRow(`SELECT type FROM work_items AS wi WHERE id=? AND `+eligibility, taskID).Scan(&itemType); err != nil {
				return errors.New("pipeline claim rejected: Work Item is not an authorized dependency-ready executable leaf")
			}
			if err = tx.QueryRow(`SELECT id,version,content_hash FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, taskID).Scan(&packID, &packVersion, &packHash); err != nil {
				return err
			}
			var rootID, materializationCheckpoint string
			err = tx.QueryRow(`SELECT root_work_item_id,checkpoint_id FROM work_item_materializations WHERE work_item_id=?`, taskID).Scan(&rootID, &materializationCheckpoint)
			if err == nil {
				var newerTaskGraph int
				if err = tx.QueryRow(`SELECT EXISTS(
				SELECT 1 FROM work_item_artifacts newer
				JOIN workflow_checkpoints approved ON approved.work_item_id=newer.work_item_id AND approved.stage='task_graph'
				WHERE newer.work_item_id=? AND newer.stage='task_graph' AND newer.revision>approved.artifact_revision
			)`, rootID).Scan(&newerTaskGraph); err != nil {
					return err
				}
				if newerTaskGraph != 0 {
					return errors.New("pipeline claim rejected: current task graph is not approved")
				}
				var authorized int
				if err = tx.QueryRow(`SELECT COUNT(*) FROM implementation_authorizations WHERE work_item_id=? AND task_graph_checkpoint_id=? AND revoked_at=''`, rootID, materializationCheckpoint).Scan(&authorized); err != nil {
					return err
				}
				var packCheckpoint string
				if err = tx.QueryRow(`SELECT checkpoint_id FROM work_item_instruction_packs WHERE id=?`, packID).Scan(&packCheckpoint); err != nil {
					return err
				}
				if packCheckpoint != materializationCheckpoint || authorized != 1 {
					return errors.New("pipeline claim rejected: active instruction pack is not bound to the authorized parent materialization")
				}
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if opts["instruction-pack-id"] != "" && opts["instruction-pack-id"] != packID {
				return errors.New("pipeline claim rejected: instruction pack changed")
			}
			if opts["instruction-pack-hash"] != "" && opts["instruction-pack-hash"] != packHash {
				return errors.New("pipeline claim rejected: instruction pack hash changed")
			}
		} else {
			// Lean path: description-verbatim worker input; no pack columns, no
			// pack-keyed circuit limiters (the activity log is the retry evidence).
			// Single-writer exclusion: first worker claim requires an unclaimed item;
			// review/autofix/review-fix rely on the shared active-run lease check.
			eligibility := `wi.type IN ('task','bug','chore') AND wi.status IN ('open','in_progress') AND wi.deferred=0 AND NOT EXISTS (
			SELECT 1 FROM work_item_relations r JOIN work_items blocker ON blocker.id=r.related_work_item_id WHERE r.work_item_id=wi.id AND r.relation_type='blocks' AND blocker.status!='done'
		) AND NOT EXISTS (
			SELECT 1 FROM work_item_relations r JOIN work_items gate_item ON gate_item.id=r.related_work_item_id WHERE r.work_item_id=wi.id AND r.relation_type='gates' AND gate_item.status!='done'
		)`
			if stage == "worker" && opts["review-fix"] != "1" {
				eligibility = `wi.type IN ('task','bug','chore') AND wi.status='open' AND wi.deferred=0 AND wi.claimed_at='' AND NOT EXISTS (
				SELECT 1 FROM work_item_relations r JOIN work_items blocker ON blocker.id=r.related_work_item_id WHERE r.work_item_id=wi.id AND r.relation_type='blocks' AND blocker.status!='done'
			) AND NOT EXISTS (
				SELECT 1 FROM work_item_relations r JOIN work_items gate_item ON gate_item.id=r.related_work_item_id WHERE r.work_item_id=wi.id AND r.relation_type='gates' AND gate_item.status!='done'
			)`
			}
			var itemType string
			if err = tx.QueryRow(`SELECT type FROM work_items AS wi WHERE id=? AND `+eligibility, taskID).Scan(&itemType); err != nil {
				return errors.New("pipeline claim rejected: Work Item is not a dependency-ready executable leaf")
			}
			if stage == "worker" {
				claimant := opts["claimant"]
				if claimant == "" {
					claimant = "contractor"
				}
				res, err := tx.Exec(`UPDATE work_items SET status='in_progress', claimed_at=datetime('now'), claimed_by=? WHERE id=? AND status IN ('open','in_progress')`, claimant, taskID)
				if err != nil {
					return err
				}
				if changed, _ := res.RowsAffected(); changed != 1 {
					return errors.New("lean claim rejected: Work Item is already claimed or closed")
				}
				if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,actor_model,summary) VALUES(?,?,'claimed','contractor',?,?)`, "wie-"+store.ShortID(), taskID, claimant, "lean claim: worker input is the stored description verbatim"); err != nil {
					return err
				}
			}
		}
	}
	leaseSeconds := 3600
	if opts["lease-seconds"] != "" {
		leaseSeconds, err = strconv.Atoi(opts["lease-seconds"])
		if err != nil || leaseSeconds < 1 {
			return errors.New("lease-seconds must be a positive integer")
		}
	}

	var activeID string
	err = tx.QueryRow(`SELECT id FROM pipeline_runs WHERE task_id=? AND stage=? AND status IN ('claimed','running') LIMIT 1`, taskID, stage).Scan(&activeID)
	if err == nil {
		return fmt.Errorf("pipeline stage already active: %s", activeID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if stage == "review" {
		err = tx.QueryRow(`SELECT id,integrated_patch_hash FROM (SELECT * FROM pipeline_runs WHERE task_id=? AND stage IN ('worker','autofix') AND status='completed' ORDER BY rowid DESC LIMIT 1) WHERE instruction_pack_id=? AND instruction_pack_version=? AND instruction_pack_hash=? AND artifact_saved_at<>'' AND integrated_patch_path<>'' AND integrated_patch_hash<>''`, taskID, packID, packVersion, packHash).Scan(&candidateRunID, &candidatePatchHash)
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("pipeline review requires validated candidate patch evidence for the active instruction pack")
		}
		if err != nil {
			return err
		}
	}
	if stage == "worker" || stage == "autofix" {
		var awaitingIntegrationID string
		err = tx.QueryRow(`SELECT id FROM pipeline_runs WHERE rowid=(SELECT rowid FROM pipeline_runs WHERE task_id=? AND stage IN ('worker','autofix') AND instruction_pack_hash=? ORDER BY rowid DESC LIMIT 1) AND status='completed' AND integrated_at='' AND advanced_at=''`, taskID, packHash).Scan(&awaitingIntegrationID)
		if err == nil {
			return fmt.Errorf("pipeline mutation claim rejected: completed mutation artifact awaiting integration: %s", awaitingIntegrationID)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if stage == "worker" {
		if opts["review-fix"] == "1" {
			var ownerApprovalRequired int
			if err = tx.QueryRow(`SELECT COALESCE(json_extract(result_json,'$.owner_approval_required'),0) FROM pipeline_runs WHERE task_id=? AND stage='review' AND status='completed' AND json_valid(result_json) AND json_extract(result_json,'$.review_status')='failed' ORDER BY rowid DESC LIMIT 1`, taskID).Scan(&ownerApprovalRequired); err == nil && ownerApprovalRequired != 0 {
				return errors.New("review-fix claim requires owner approval for a critical deviation")
			} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err = tx.QueryRow(`SELECT review.candidate_run_id,review.candidate_patch_hash FROM pipeline_runs review JOIN pipeline_runs candidate ON candidate.id=review.candidate_run_id WHERE review.task_id=? AND review.stage='review' AND review.status='completed' AND review.instruction_pack_id=? AND review.instruction_pack_version=? AND review.instruction_pack_hash=? AND json_valid(review.result_json) AND json_extract(review.result_json,'$.review_status')='failed' AND candidate.task_id=review.task_id AND candidate.stage IN ('worker','autofix') AND candidate.status='completed' AND candidate.instruction_pack_id=review.instruction_pack_id AND candidate.instruction_pack_version=review.instruction_pack_version AND candidate.instruction_pack_hash=review.instruction_pack_hash AND candidate.artifact_saved_at<>'' AND candidate.integrated_patch_path<>'' AND candidate.integrated_patch_hash=review.candidate_patch_hash AND candidate.rowid=(SELECT MAX(current.rowid) FROM pipeline_runs current WHERE current.task_id=review.task_id AND current.stage IN ('worker','autofix') AND current.status='completed' AND current.instruction_pack_id=review.instruction_pack_id AND current.instruction_pack_version=review.instruction_pack_version AND current.instruction_pack_hash=review.instruction_pack_hash AND current.artifact_saved_at<>'') ORDER BY review.rowid DESC LIMIT 1`, taskID, packID, packVersion, packHash).Scan(&candidateRunID, &candidatePatchHash); err != nil {
				return errors.New("review-fix claim requires a completed failed review verdict")
			}
			if candidateRunID == "" || candidatePatchHash == "" {
				return errors.New("review-fix claim requires a bound rejected candidate")
			}
			if !leanClaim {
				var unchangedFailures int
				if err = tx.QueryRow(`SELECT COUNT(*) FROM pipeline_runs WHERE task_id=? AND candidate_patch_hash=? AND error='review-fix produced the unchanged rejected candidate patch' AND attempt>COALESCE((SELECT MAX(CAST(json_extract(payload_json,'$.after_attempt') AS INTEGER)) FROM work_item_events WHERE work_item_id=? AND event_type IN ('pipeline_circuit_reset','owner_rejected_completion','owner_review_decision') AND actor_role='owner' AND json_valid(payload_json)),0)`, taskID, candidatePatchHash, taskID).Scan(&unchangedFailures); err != nil {
					return err
				}
				if unchangedFailures > 0 {
					return errors.New("review-fix circuit breaker open: rejected candidate already produced no progress; owner action or a new instruction pack is required")
				}
				if err = tx.QueryRow(`SELECT COALESCE(MAX(review_fix_cycle),0)+1 FROM pipeline_runs WHERE task_id=? AND instruction_pack_hash=? AND status!='cancelled' AND attempt>COALESCE((SELECT MAX(CAST(json_extract(payload_json,'$.after_attempt') AS INTEGER)) FROM work_item_events WHERE work_item_id=? AND event_type IN ('pipeline_circuit_reset','owner_rejected_completion','owner_review_decision') AND actor_role='owner' AND json_valid(payload_json)),0)`, taskID, packHash, taskID).Scan(&reviewFixCycle); err != nil {
					return err
				}
				if reviewFixCycle > 3 {
					return errors.New("review-fix cycle limit reached (3 attempts for the unchanged active instruction pack); owner action is required")
				}
			}
		}
		if !leanClaim {
			if opts["explicit-retry"] != "1" {
				var blockedReason, previousFingerprint string
				err = tx.QueryRow(`SELECT CASE WHEN json_valid(result_json) THEN json_extract(result_json,'$.failure_code') ELSE '' END,environment_fingerprint FROM pipeline_runs WHERE task_id=? AND stage='worker' AND instruction_pack_hash=? AND CASE WHEN json_valid(result_json) THEN json_extract(result_json,'$.failure_code') ELSE '' END IN ('environment_blocked','runner_protocol_invalid') ORDER BY attempt DESC LIMIT 1`, taskID, packHash).Scan(&blockedReason, &previousFingerprint)
				if err == nil {
					if blockedReason == "runner_protocol_invalid" || previousFingerprint == "" || previousFingerprint == opts["environment-fingerprint"] {
						return fmt.Errorf("automatic worker retry blocked by %s; correct the environment or runner, then explicitly retry", blockedReason)
					}
				}
				if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
			}
			var contractSnapshotFailures int
			if err = tx.QueryRow(`SELECT COUNT(*) FROM pipeline_runs WHERE task_id=? AND stage='worker' AND instruction_pack_hash=? AND CASE WHEN json_valid(result_json) THEN json_extract(result_json,'$.failure_code') ELSE '' END IN ('worker_output_invalid','worker_artifact_invalid','scheduler_owner_lost') AND attempt>COALESCE((SELECT MAX(CAST(json_extract(payload_json,'$.after_attempt') AS INTEGER)) FROM work_item_events WHERE work_item_id=? AND event_type IN ('pipeline_circuit_reset','owner_rejected_completion') AND actor_role='owner' AND json_valid(payload_json)),0)`, taskID, packHash, taskID).Scan(&contractSnapshotFailures); err != nil {
				return err
			}
			if contractSnapshotFailures >= maxDeterministicWorkerFailuresPerContract {
				return fmt.Errorf("worker circuit breaker open: %d deterministic failures for the unchanged active instruction pack; owner circuit reset with repair evidence is required", contractSnapshotFailures)
			}
		}
	}
	attempt := 1
	if err = tx.QueryRow(`SELECT COALESCE(MAX(attempt),0)+1 FROM pipeline_runs WHERE task_id=? AND stage=?`, taskID, stage).Scan(&attempt); err != nil {
		return err
	}
	if stage == "worker" && opts["explicit-retry"] != "1" && opts["review-fix"] != "1" && !leanClaim {
		var unchangedPackAttempts int
		// Unchanged-pack limiter invariant: only attempts that produced output
		// evidence (a completion/artifact or a classified failure_code) count
		// against the instruction content. Transient provider deaths record no
		// failure_code and must not exhaust retries, otherwise the item deadlocks:
		// failed claims roll back the generated TIP and pipeline-circuit-reset
		// then refuses for lack of an active pack.
		if err = tx.QueryRow(`SELECT COUNT(*) FROM pipeline_runs WHERE task_id=? AND stage='worker' AND instruction_pack_hash=? AND NOT (status IN ('failed','blocked','expired') AND CASE WHEN json_valid(result_json) THEN json_extract(result_json,'$.failure_code') ELSE '' END='') AND attempt>COALESCE((SELECT MAX(CAST(json_extract(payload_json,'$.after_attempt') AS INTEGER)) FROM work_item_events WHERE work_item_id=? AND event_type IN ('pipeline_circuit_reset','owner_rejected_completion') AND actor_role='owner' AND json_valid(payload_json)),0)`, taskID, packHash, taskID).Scan(&unchangedPackAttempts); err != nil {
			return err
		}
		if unchangedPackAttempts >= maxAutomaticWorkerAttempts {
			return fmt.Errorf("automatic worker retry limit reached (%d attempts for unchanged instruction pack); explicit retry requires --explicit-retry 1 after correcting the instruction, model, or runner", maxAutomaticWorkerAttempts)
		}
	}
	if stage == "autofix" && !leanClaim {
		var attempts int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM pipeline_runs WHERE task_id=? AND stage='autofix' AND instruction_pack_hash=? AND status IN ('completed','blocked')`, taskID, packHash).Scan(&attempts); err != nil {
			return err
		}
		if attempts >= maxAutomaticAutofixAttempts {
			return fmt.Errorf("autofix cycle limit reached (%d attempts for the unchanged active instruction pack); owner action is required", maxAutomaticAutofixAttempts)
		}
	}
	id, token := "pr-"+store.ShortID(), "lease-"+store.ShortID()
	if _, err = tx.Exec(`INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,agent_model,environment_fingerprint,base_commit,candidate_run_id,candidate_patch_hash,review_fix_cycle,profile_version,profile_hash) VALUES(?,?,?,?, 'claimed', ?, datetime('now', ?),?,?,?,?,?,?,?,?,?,?,?)`, id, taskID, stage, attempt, token, fmt.Sprintf("+%d seconds", leaseSeconds), packID, packVersion, packHash, opts["agent-model"], opts["environment-fingerprint"], opts["base-commit"], candidateRunID, candidatePatchHash, reviewFixCycle, profileVersion, profileHash); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return store.OutputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, id)
}
