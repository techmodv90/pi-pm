package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

var pipelineStages = []string{"scan", "rri", "vision", "blueprint", "contracts", "task_graph", "worker", "review", "autofix"}
var pipelineTerminalStatuses = []string{"completed", "failed", "blocked", "cancelled"}

const pipelineRunsTableSQL = `CREATE TABLE IF NOT EXISTS pipeline_runs (id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, stage TEXT NOT NULL CHECK(stage IN ('scan','rri','vision','blueprint','contracts','task_graph','worker','review','autofix')), attempt INTEGER NOT NULL, status TEXT NOT NULL DEFAULT 'claimed' CHECK(status IN ('claimed','running','completed','failed','blocked','cancelled','expired')), lease_token TEXT NOT NULL, lease_expires_at TEXT NOT NULL, instruction_pack_id TEXT DEFAULT '', instruction_pack_version INTEGER DEFAULT 0, instruction_pack_hash TEXT DEFAULT '', effective_contract_snapshot_id TEXT DEFAULT '', effective_contract_snapshot_hash TEXT DEFAULT '', agent_model TEXT DEFAULT '', environment_fingerprint TEXT DEFAULT '', base_commit TEXT DEFAULT '', subagent_run_id TEXT DEFAULT '', child_index INTEGER DEFAULT 0, async_dir TEXT DEFAULT '', result_json TEXT DEFAULT '', error TEXT DEFAULT '', integrated_patch_path TEXT DEFAULT '', integrated_patch_hash TEXT DEFAULT '', integrated_at TEXT DEFAULT '', artifact_saved_at TEXT DEFAULT '', candidate_run_id TEXT DEFAULT '', candidate_patch_hash TEXT DEFAULT '', review_fix_cycle INTEGER DEFAULT 0, profile_version INTEGER DEFAULT 0, profile_hash TEXT DEFAULT '', advanced_at TEXT DEFAULT '', migration_status TEXT DEFAULT 'legacy', created_at TEXT DEFAULT (datetime('now')), updated_at TEXT DEFAULT (datetime('now')), completed_at TEXT DEFAULT '', UNIQUE(task_id, stage, attempt))`

const workItemEscalationsTableSQL = `CREATE TABLE IF NOT EXISTS work_item_escalations (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, pipeline_run_id TEXT NOT NULL UNIQUE REFERENCES pipeline_runs(id), instruction_pack_id TEXT NOT NULL, instruction_pack_version INTEGER NOT NULL, instruction_pack_hash TEXT NOT NULL, level TEXT NOT NULL CHECK(level IN ('L2','L3')), status TEXT NOT NULL CHECK(status IN ('open','resolved')), report_json TEXT NOT NULL, resolution_json TEXT DEFAULT '', resolved_by TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')), resolved_at TEXT DEFAULT '')`

const workItemCompletionReportsTableSQL = `CREATE TABLE IF NOT EXISTS work_item_completion_reports (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, pipeline_run_id TEXT NOT NULL UNIQUE REFERENCES pipeline_runs(id), instruction_pack_id TEXT NOT NULL, instruction_pack_version INTEGER NOT NULL, instruction_pack_hash TEXT NOT NULL, status TEXT NOT NULL CHECK(status IN ('done','partial','blocked')), summary TEXT DEFAULT '', report_markdown TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')))`

const maxAutomaticWorkerAttempts = 3
const maxAutomaticAutofixAttempts = 3
const maxDeterministicWorkerFailuresPerContract = 1

func prepareInstructionPackForFirstClaim(tx *sql.Tx, taskID string) error {
	var rootID, checkpointID, nodeKey string
	err := tx.QueryRow(`SELECT m.root_work_item_id,m.checkpoint_id,m.node_key FROM work_item_materializations m JOIN implementation_authorizations a ON a.work_item_id=m.root_work_item_id AND a.task_graph_checkpoint_id=m.checkpoint_id AND a.revoked_at='' WHERE m.work_item_id=? ORDER BY m.rowid DESC LIMIT 1`, taskID).Scan(&rootID, &checkpointID, &nodeKey)
	if errors.Is(err, sql.ErrNoRows) {
		var active int
		if countErr := tx.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, taskID).Scan(&active); countErr != nil {
			return countErr
		}
		if active == 1 {
			return nil
		}
		return err
	}
	if err != nil {
		return err
	}
	var activeCheckpoint string
	err = tx.QueryRow(`SELECT checkpoint_id FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, taskID).Scan(&activeCheckpoint)
	if err == nil && activeCheckpoint == checkpointID {
		return nil
	}
	if err == nil {
		return errors.New("active instruction pack is not bound to the authorized parent materialization")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var existing string
	err = tx.QueryRow(`SELECT id FROM work_item_instruction_packs WHERE work_item_id=? AND checkpoint_id=? AND status='inactive' ORDER BY version DESC LIMIT 1`, taskID, checkpointID).Scan(&existing)
	if err == nil {
		return activateWorkItemInstructionPack(tx, existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var content string
	if err = tx.QueryRow(`SELECT a.content FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.id=?`, checkpointID).Scan(&content); err != nil {
		return err
	}
	plan, err := parseTaskPlanJSON("```task-plan-json\n" + content + "\n```")
	if err != nil {
		return err
	}
	var node *taskPlanDocumentNode
	for index := range plan.Nodes {
		if plan.Nodes[index].Key == nodeKey {
			node = &plan.Nodes[index]
			break
		}
	}
	if node == nil {
		return fmt.Errorf("materialized node %s is missing from the approved task graph", nodeKey)
	}
	requirements, err := validateTaskGraphRequirementCoverage(tx, rootID, plan)
	if err != nil {
		return err
	}
	packContent, contentHash, err := materializedInstructionPack(*node, plan.Version, requirements)
	if err != nil {
		return err
	}
	var version int
	if err = tx.QueryRow(`SELECT COALESCE(MAX(version),0)+1 FROM work_item_instruction_packs WHERE work_item_id=?`, taskID).Scan(&version); err != nil {
		return err
	}
	packID := "wip-" + shortID()
	if _, err = tx.Exec(`INSERT INTO work_item_instruction_packs(id,work_item_id,checkpoint_id,version,status,content_json,content_hash) VALUES(?,?,?,?,'inactive',?,?)`, packID, taskID, checkpointID, version, string(packContent), contentHash); err != nil {
		return err
	}
	return activateWorkItemInstructionPack(tx, packID)
}

func workflowPipelineClaim(db *sql.DB, args []string) error {
	if len(args) < 2 {
		return errors.New("pipeline-claim requires task id and stage")
	}
	taskID, stage := args[0], args[1]
	if _, err := workItemByID(db, taskID); err != nil {
		return err
	}
	if !contains(pipelineStages, stage) {
		return fmt.Errorf("invalid pipeline stage: %s", stage)
	}
	opts, err := parseOptions(args[2:])
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
	if err = expirePipelineLeases(tx, taskID, stage); err != nil {
		return err
	}
	// Open-escalation scheduling gate (GAP-138): no claim of any stage may proceed
	// while an unresolved escalation exists for this Work Item.
	var openEscalations int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM work_item_escalations WHERE work_item_id=? AND status='open'`, taskID).Scan(&openEscalations); err != nil {
		return err
	}
	if openEscalations > 0 {
		return fmt.Errorf("pipeline claim rejected: %d open escalation(s) require contractor resolution", openEscalations)
	}
	// Resolve the versioned Plan/Implement/QA profile exactly once at the first
	// claim and bind this claim to the persisted profile version and hash.
	lifecycle := lifecycleForStage(stage)
	if lifecycle == "" {
		return fmt.Errorf("invalid pipeline stage: %s", stage)
	}
	profiles, err := ensureWorkItemProfiles(tx, taskID)
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
		planningIndex := indexOfStage(planStages, stage)
		if planningIndex < 0 {
			return fmt.Errorf("pipeline claim rejected: stage %s is not part of this Work Item planning profile", stage)
		}
		if planningIndex > 0 {
			for index, requiredStage := range planStages {
				var approved int
				if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM workflow_checkpoints WHERE work_item_id=? AND stage=?)`, taskID, requiredStage).Scan(&approved); err != nil {
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
	if stage == "worker" || stage == "review" || stage == "autofix" {
		if stage == "worker" {
			if err = prepareInstructionPackForFirstClaim(tx, taskID); err != nil {
				return fmt.Errorf("pipeline claim rejected: prepare instruction pack: %w", err)
			}
		}
		var activePacks int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, taskID).Scan(&activePacks); err != nil {
			return err
		}
		if activePacks != 1 {
			return fmt.Errorf("Work Item %s requires exactly one active instruction pack", taskID)
		}
		eligibility := workItemReadySQL
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
			var unchangedFailures int
			if err = tx.QueryRow(`SELECT COUNT(*) FROM pipeline_runs WHERE task_id=? AND candidate_patch_hash=? AND error='review-fix produced the unchanged rejected candidate patch' AND attempt>COALESCE((SELECT MAX(CAST(json_extract(payload_json,'$.after_attempt') AS INTEGER)) FROM work_item_events WHERE work_item_id=? AND event_type IN ('pipeline_circuit_reset','owner_rejected_completion') AND actor_role='owner' AND json_valid(payload_json)),0)`, taskID, candidatePatchHash, taskID).Scan(&unchangedFailures); err != nil {
				return err
			}
			if unchangedFailures > 0 {
				return errors.New("review-fix circuit breaker open: rejected candidate already produced no progress; owner action or a new instruction pack is required")
			}
			if err = tx.QueryRow(`SELECT COALESCE(MAX(review_fix_cycle),0)+1 FROM pipeline_runs WHERE task_id=? AND instruction_pack_hash=? AND status!='cancelled' AND attempt>COALESCE((SELECT MAX(CAST(json_extract(payload_json,'$.after_attempt') AS INTEGER)) FROM work_item_events WHERE work_item_id=? AND event_type IN ('pipeline_circuit_reset','owner_rejected_completion') AND actor_role='owner' AND json_valid(payload_json)),0)`, taskID, packHash, taskID).Scan(&reviewFixCycle); err != nil {
				return err
			}
			if reviewFixCycle > 3 {
				return errors.New("review-fix cycle limit reached (3 attempts for the unchanged active instruction pack); owner action is required")
			}
		}
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
	attempt := 1
	if err = tx.QueryRow(`SELECT COALESCE(MAX(attempt),0)+1 FROM pipeline_runs WHERE task_id=? AND stage=?`, taskID, stage).Scan(&attempt); err != nil {
		return err
	}
	if stage == "worker" && opts["explicit-retry"] != "1" && opts["review-fix"] != "1" {
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
	if stage == "autofix" {
		var attempts int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM pipeline_runs WHERE task_id=? AND stage='autofix' AND instruction_pack_hash=? AND status IN ('completed','blocked')`, taskID, packHash).Scan(&attempts); err != nil {
			return err
		}
		if attempts >= maxAutomaticAutofixAttempts {
			return fmt.Errorf("autofix cycle limit reached (%d attempts for the unchanged active instruction pack); owner action is required", maxAutomaticAutofixAttempts)
		}
	}
	id, token := "pr-"+shortID(), "lease-"+shortID()
	if _, err = tx.Exec(`INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,agent_model,environment_fingerprint,base_commit,candidate_run_id,candidate_patch_hash,review_fix_cycle,profile_version,profile_hash) VALUES(?,?,?,?, 'claimed', ?, datetime('now', ?),?,?,?,?,?,?,?,?,?,?,?)`, id, taskID, stage, attempt, token, fmt.Sprintf("+%d seconds", leaseSeconds), packID, packVersion, packHash, opts["agent-model"], opts["environment-fingerprint"], opts["base-commit"], candidateRunID, candidatePatchHash, reviewFixCycle, profileVersion, profileHash); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return outputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, id)
}

func workflowPipelineCircuitReset(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("pipeline-circuit-reset requires task id")
	}
	taskID := args[0]
	opts, err := parseOptions(args[1:])
	if err != nil {
		return err
	}
	if validateWorkflowActor(opts["actor-role"], "owner") != nil {
		return errors.New("pipeline circuit reset requires actor_role=owner")
	}
	if opts["reason"] == "" {
		return errors.New("pipeline-circuit-reset requires --reason")
	}
	if !contains([]string{"contract", "environment", "runner", "artifact"}, opts["change-type"]) {
		return errors.New("pipeline-circuit-reset requires --change-type contract|environment|runner|artifact")
	}
	var evidence map[string]any
	if opts["evidence-json"] == "" || json.Unmarshal([]byte(opts["evidence-json"]), &evidence) != nil || len(evidence) == 0 {
		return errors.New("pipeline-circuit-reset requires non-empty --evidence-json")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	pack, err := queryOne(tx, `SELECT * FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, taskID)
	if err != nil {
		return errors.New("pipeline circuit reset requires one active instruction pack")
	}
	snapshotHash := persistedText(pack["content_hash"])
	if snapshotHash == "" {
		return errors.New("pipeline circuit reset requires an active instruction pack hash")
	}
	// No-progress reset invariant: resetting the counter must identify the changed
	// execution input, otherwise the same TIP/environment can loop indefinitely.
	changedFingerprint, _ := evidence["changed_fingerprint"].(string)
	if changedFingerprint == "" {
		return errors.New("pipeline-circuit-reset evidence requires changed_fingerprint")
	}
	var previousFingerprint string
	if err = tx.QueryRow(`SELECT json_extract(payload_json,'$.changed_fingerprint') FROM work_item_events WHERE work_item_id=? AND event_type='pipeline_circuit_reset' ORDER BY rowid DESC LIMIT 1`, taskID).Scan(&previousFingerprint); err == nil && previousFingerprint == changedFingerprint {
		return errors.New("pipeline circuit reset rejected: unchanged execution fingerprint")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var attempt int
	if err = tx.QueryRow(`SELECT attempt FROM pipeline_runs WHERE task_id=? AND stage='worker' AND status IN ('failed','blocked','cancelled','expired') ORDER BY attempt DESC LIMIT 1`, taskID).Scan(&attempt); err != nil {
		return errors.New("pipeline circuit reset requires a terminal worker attempt")
	}
	id := "wie-" + shortID()
	decisionMetadata := map[string]any{"after_attempt": attempt, "change_type": opts["change-type"], "changed_fingerprint": changedFingerprint, "evidence": evidence, "reason": opts["reason"]}
	metadataJSON, _ := json.Marshal(decisionMetadata)
	if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,summary,payload_json) VALUES(?,?,'pipeline_circuit_reset','owner',?,?)`, id, taskID, opts["reason"], string(metadataJSON)); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE work_items SET status='open',claimed_at='',claimed_by='' WHERE id=? AND status='in_progress' AND NOT EXISTS (SELECT 1 FROM pipeline_runs WHERE task_id=? AND status IN ('claimed','running'))`, taskID, taskID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return outputOne(db, `SELECT * FROM work_item_events WHERE id=?`, id)
}

func workflowPipelineBind(db *sql.DB, args []string) error {
	if len(args) < 3 {
		return errors.New("pipeline-bind requires run id, lease token, and subagent run id")
	}
	opts, err := parseOptions(args[3:])
	if err != nil {
		return err
	}
	childIndex := 0
	if opts["child-index"] != "" {
		childIndex, err = strconv.Atoi(opts["child-index"])
		if err != nil || childIndex < 0 {
			return errors.New("child-index must be a non-negative integer")
		}
	}
	var result sql.Result
	if previous := opts["replace-subagent-id"]; previous != "" {
		result, err = db.Exec(`UPDATE pipeline_runs SET subagent_run_id=?,child_index=?,async_dir=?,updated_at=datetime('now') WHERE id=? AND lease_token=? AND status='running' AND subagent_run_id=? AND datetime(lease_expires_at)>datetime('now')`, args[2], childIndex, opts["async-dir"], args[0], args[1], previous)
	} else {
		result, err = db.Exec(`UPDATE pipeline_runs SET status='running',subagent_run_id=?,child_index=?,async_dir=?,updated_at=datetime('now') WHERE id=? AND lease_token=? AND status='claimed' AND datetime(lease_expires_at)>datetime('now')`, args[2], childIndex, opts["async-dir"], args[0], args[1])
	}
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("pipeline bind rejected: stale or invalid lease")
	}
	return outputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
}

func workflowPipelineRenew(db *sql.DB, args []string) error {
	if len(args) < 2 {
		return errors.New("pipeline-renew requires run id and lease token")
	}
	result, err := db.Exec(`UPDATE pipeline_runs SET lease_expires_at=datetime('now','+4 hours'),updated_at=datetime('now') WHERE id=? AND lease_token=? AND status IN ('claimed','running') AND datetime(lease_expires_at)>datetime('now')`, args[0], args[1])
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("pipeline renewal rejected: stale or invalid lease")
	}
	return outputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
}

func workflowPipelineModel(db *sql.DB, args []string) error {
	if len(args) < 3 {
		return errors.New("pipeline-model requires run id, lease token, and model")
	}
	result, err := db.Exec(`UPDATE pipeline_runs SET agent_model=?,updated_at=datetime('now') WHERE id=? AND lease_token=? AND status IN ('claimed','running')`, args[2], args[0], args[1])
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("pipeline model update rejected: stale or invalid lease")
	}
	return outputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
}

// Escalation persistence (GAP-138): the scheduler saves one structured escalation
// report per completed worker run. The row is bound to the run's active-TIP lineage
// so a resolution can never be replayed against a retired pack, and the run is
// blocked and its claim released in the same transaction.
func workflowEscalationSave(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("usage: pic workflow escalation-save <task-id> --pipeline-run-id <id> --report-json <json>")
	}
	taskID := args[0]
	opts, err := parseOptions(args[1:])
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
	id := "wies-" + shortID()
	if _, err = tx.Exec(`INSERT INTO work_item_escalations(id,work_item_id,pipeline_run_id,instruction_pack_id,instruction_pack_version,instruction_pack_hash,level,status,report_json) VALUES(?,?,?,?,?,?,?,'open',?)`, id, taskID, opts["pipeline-run-id"], packID, packVersion, packHash, level, normalizeJSONText(opts["report-json"])); err != nil {
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
	if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,summary,payload_json) VALUES(?,?,'worker_escalated','worker',?,?)`, "wie-"+shortID(), taskID, "worker escalated "+level+" on TIP "+packID, normalizeJSONText(opts["report-json"])); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return outputOne(db, `SELECT * FROM work_item_escalations WHERE id=?`, id)
}

func workflowEscalationResolve(db *sql.DB, args []string) error {
	if len(args) < 3 {
		return errors.New("usage: pic workflow escalation-resolve <task-id> <escalation-id> <resolution-json> --actor-role contractor")
	}
	taskID, escalationID := args[0], args[1]
	opts, err := parseOptions(args[3:])
	if err != nil || validateWorkflowActor(opts["actor-role"], "contractor") != nil {
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
	result, err := tx.Exec(`UPDATE work_item_escalations SET status='resolved',resolution_json=?,resolved_by=?,resolved_at=datetime('now') WHERE id=? AND status='open'`, normalizeJSONText(args[2]), opts["actor-role"], escalationID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("escalation resolution rejected: already resolved")
	}
	if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,summary,payload_json) VALUES(?,?,'escalation_resolved','contractor',?,?)`, "wie-"+shortID(), taskID, "escalation "+escalationID+" resolved on TIP "+packID, normalizeJSONText(args[2])); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return outputOne(db, `SELECT * FROM work_item_escalations WHERE id=?`, escalationID)
}

func workflowPipelineComplete(db *sql.DB, args []string) error {
	if len(args) < 3 {
		return errors.New("pipeline-complete requires run id, lease token, and status")
	}
	if !contains(pipelineTerminalStatuses, args[2]) {
		return fmt.Errorf("invalid pipeline terminal status: %s", args[2])
	}
	opts, err := parseOptions(args[3:])
	if err != nil {
		return err
	}
	currentStatuses := "'claimed','running'"
	if args[2] == "blocked" {
		// Integration can fail after child completion; only blocked may correct that terminal state.
		currentStatuses += ",'completed'"
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE pipeline_runs SET status=?,result_json=?,error=?,updated_at=datetime('now'),completed_at=datetime('now') WHERE id=? AND lease_token=? AND status IN (`+currentStatuses+`) AND NOT (status='completed' AND stage='review') AND datetime(lease_expires_at)>datetime('now')`, args[2], normalizeJSONText(opts["result-json"]), opts["error"], args[0], args[1])
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("pipeline completion rejected: stale or invalid lease")
	}
	if contains([]string{"failed", "blocked", "cancelled"}, args[2]) {
		if _, err = tx.Exec(`UPDATE work_items SET status='open',claimed_at='',claimed_by='' WHERE id=(SELECT task_id FROM pipeline_runs WHERE id=?) AND status='in_progress' AND NOT EXISTS (SELECT 1 FROM pipeline_runs active WHERE active.task_id=work_items.id AND active.status IN ('claimed','running'))`, args[0]); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return outputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
}

func workflowPipelineCheckpoint(db *sql.DB, args []string) error {
	if len(args) < 3 {
		return errors.New("pipeline-checkpoint requires run id, lease token, and checkpoint")
	}
	column := map[string]string{"integrated": "integrated_at", "artifact_saved": "artifact_saved_at", "advanced": "advanced_at"}[args[2]]
	if column == "" {
		return fmt.Errorf("invalid pipeline checkpoint: %s", args[2])
	}
	opts, err := parseOptions(args[3:])
	if err != nil {
		return err
	}
	if args[2] == "advanced" {
		// Terminal advancement is reconciliation metadata, not an authority-bearing mutation.
		// Allow the durable pending sweep to close a terminal run after its worker lease expires.
		result, updateErr := db.Exec(`UPDATE pipeline_runs SET advanced_at=datetime('now'),updated_at=datetime('now') WHERE id=? AND status IN ('completed','failed','blocked','cancelled','expired') AND advanced_at=''`, args[0])
		if updateErr != nil {
			return updateErr
		}
		if changed, _ := result.RowsAffected(); changed == 1 {
			return outputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
		}
		var terminal int
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pipeline_runs WHERE id=? AND status IN ('completed','failed','blocked','cancelled','expired') AND advanced_at<>'')`, args[0]).Scan(&terminal); err != nil {
			return err
		}
		if terminal != 0 {
			return outputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
		}
	}
	statePredicate := map[string]string{
		"artifact_saved": `stage IN ('worker','autofix') AND status='completed'`,
		"advanced":       `status IN ('completed','failed','blocked','cancelled','expired')`,
		"integrated":     `stage IN ('worker','autofix') AND status='completed' AND EXISTS(SELECT 1 FROM pipeline_runs review WHERE review.task_id=pipeline_runs.task_id AND review.stage='review' AND review.status='completed' AND json_valid(review.result_json) AND json_extract(review.result_json,'$.review_status')='passed' AND json_extract(review.result_json,'$.candidate_run_id')=pipeline_runs.id AND json_extract(review.result_json,'$.candidate_patch_hash')=pipeline_runs.integrated_patch_hash)`,
	}[args[2]]
	if ok, checkErr := rowExists(db, `SELECT 1 FROM pipeline_runs WHERE id=? AND lease_token=? AND `+statePredicate, args[0], args[1]); checkErr != nil {
		return checkErr
	} else if !ok {
		return errors.New("pipeline checkpoint rejected: invalid stage, status, lease, or review authority")
	}
	setClause := column + `=datetime('now'),updated_at=datetime('now')`
	values := []any{}
	if args[2] == "artifact_saved" && opts["patch-file"] != "" {
		patch, readErr := os.ReadFile(opts["patch-file"])
		if readErr != nil {
			return fmt.Errorf("read candidate patch: %w", readErr)
		}
		var databasePath string
		if err := db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&databasePath); err != nil {
			return err
		}
		patchDir := filepath.Join(filepath.Dir(databasePath), "review-patches")
		if err := os.MkdirAll(patchDir, 0o700); err != nil {
			return err
		}
		hash := sha256.Sum256(patch)
		patchPath := filepath.Join(patchDir, args[0]+"-"+fmt.Sprintf("%x", hash)+".patch")
		temporary, err := os.CreateTemp(patchDir, args[0]+"-*.patch.tmp")
		if err != nil {
			return err
		}
		temporaryPath := temporary.Name()
		defer os.Remove(temporaryPath)
		if err := temporary.Chmod(0o600); err != nil {
			temporary.Close()
			return err
		}
		if _, err := temporary.Write(patch); err != nil {
			temporary.Close()
			return err
		}
		if err := temporary.Sync(); err != nil {
			temporary.Close()
			return err
		}
		if err := temporary.Close(); err != nil {
			return err
		}
		if err := os.Rename(temporaryPath, patchPath); err != nil {
			return err
		}
		setClause += `,integrated_patch_path=?,integrated_patch_hash=?`
		values = append(values, patchPath, fmt.Sprintf("%x", hash))
	}
	if args[2] == "integrated" && opts["patch-file"] != "" {

		patch, readErr := os.ReadFile(opts["patch-file"])
		if readErr != nil {
			return fmt.Errorf("read integrated patch: %w", readErr)
		}
		var databasePath string
		if err := db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&databasePath); err != nil {
			return err
		}
		patchDir := filepath.Join(filepath.Dir(databasePath), "review-patches")
		if err := os.MkdirAll(patchDir, 0o700); err != nil {
			return err
		}
		hash := sha256.Sum256(patch)
		patchPath := filepath.Join(patchDir, args[0]+"-"+fmt.Sprintf("%x", hash)+".patch")
		temporary, err := os.CreateTemp(patchDir, args[0]+"-*.patch.tmp")
		if err != nil {
			return err
		}
		temporaryPath := temporary.Name()
		defer os.Remove(temporaryPath)
		if err := temporary.Chmod(0o600); err != nil {
			temporary.Close()
			return err
		}
		if _, err := temporary.Write(patch); err != nil {
			temporary.Close()
			return err
		}
		if err := temporary.Sync(); err != nil {
			temporary.Close()
			return err
		}
		if err := temporary.Close(); err != nil {
			return err
		}
		if err := os.Rename(temporaryPath, patchPath); err != nil {
			return err
		}
		setClause += `,integrated_patch_path=?,integrated_patch_hash=?`
		values = append(values, patchPath, fmt.Sprintf("%x", hash))
	}
	values = append(values, args[0], args[1])
	result, err := db.Exec(`UPDATE pipeline_runs SET `+setClause+` WHERE id=? AND lease_token=? AND `+column+`='' AND `+statePredicate, values...)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("pipeline checkpoint rejected: stale, invalid, or already recorded")
	}
	return outputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
}

func workflowPipelinePending(db *sql.DB, _ []string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE pipeline_runs AS stale SET advanced_at=datetime('now'),updated_at=datetime('now') WHERE stale.status IN ('completed','failed','blocked','expired') AND stale.advanced_at='' AND (
		EXISTS(SELECT 1 FROM work_items wi WHERE wi.id=stale.task_id AND wi.status IN ('done','cancelled')) OR
		NOT EXISTS(SELECT 1 FROM work_item_instruction_packs p WHERE p.work_item_id=stale.task_id AND p.status='active' AND p.id=stale.instruction_pack_id AND p.version=stale.instruction_pack_version AND p.content_hash=stale.instruction_pack_hash)
	)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE pipeline_runs AS older SET advanced_at=datetime('now'),updated_at=datetime('now') WHERE older.status IN ('completed','failed','blocked','expired') AND older.advanced_at='' AND EXISTS(
		SELECT 1 FROM pipeline_runs newer WHERE newer.task_id=older.task_id AND newer.status IN ('completed','failed','blocked','expired') AND newer.advanced_at='' AND newer.rowid>older.rowid
	)`); err != nil {
		return err
	}
	rows, err := queryMaps(tx, `SELECT * FROM pipeline_runs WHERE status IN ('completed','failed','blocked','expired') AND advanced_at='' ORDER BY rowid`)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	writeJSON(os.Stdout, rows)
	return nil
}

func workflowPipelineRuns(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("pipeline-runs requires task id")
	}
	return workflowList(db, args[:1], `SELECT * FROM pipeline_runs WHERE task_id=? ORDER BY rowid DESC`)
}

func workflowPipelineActive(db *sql.DB, _ []string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = expirePipelineLeases(tx, "", ""); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	rows, err := queryMaps(db, `SELECT * FROM pipeline_runs WHERE status IN ('claimed','running') ORDER BY rowid`)
	if err != nil {
		return err
	}
	writeJSON(os.Stdout, rows)
	return nil
}

func expirePipelineLeases(tx *sql.Tx, taskID, stage string) error {
	query := `SELECT DISTINCT task_id FROM pipeline_runs WHERE stage IN ('worker','autofix') AND status IN ('claimed','running') AND datetime(lease_expires_at)<=datetime('now')`
	values := []any{}
	if taskID != "" {
		query += ` AND task_id=?`
		values = append(values, taskID)
	}
	if stage != "" {
		query += ` AND stage=?`
		values = append(values, stage)
	}
	rows, err := tx.Query(query, values...)
	if err != nil {
		return err
	}
	expiredMutationTasks := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		expiredMutationTasks = append(expiredMutationTasks, id)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	update := `UPDATE pipeline_runs SET status='expired',updated_at=datetime('now'),completed_at=datetime('now'),error='lease expired' WHERE status IN ('claimed','running') AND datetime(lease_expires_at)<=datetime('now')`
	values = values[:0]
	if taskID != "" {
		update += ` AND task_id=?`
		values = append(values, taskID)
	}
	if stage != "" {
		update += ` AND stage=?`
		values = append(values, stage)
	}
	if _, err = tx.Exec(update, values...); err != nil {
		return err
	}
	for _, id := range expiredMutationTasks {
		if _, err = tx.Exec(`UPDATE work_items SET status='open',claimed_at='',claimed_by='' WHERE id=? AND status='in_progress' AND NOT EXISTS (SELECT 1 FROM pipeline_runs WHERE task_id=? AND status IN ('claimed','running'))`, id, id); err != nil {
			return err
		}
	}
	return nil
}

func workflowPipelineGroup(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("pipeline-group requires subagent run id")
	}
	rows, err := queryMaps(db, `SELECT * FROM pipeline_runs WHERE subagent_run_id=? ORDER BY child_index`, args[0])
	if err != nil {
		return err
	}
	writeJSON(os.Stdout, rows)
	return nil
}
