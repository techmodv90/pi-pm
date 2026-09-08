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
	"strconv"

	"github.com/earendil-works/task-system/go-pic/internal/store"
	workitem "github.com/earendil-works/task-system/go-pic/internal/work-item"
)

func CircuitReset(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("pipeline-circuit-reset requires task id")
	}
	taskID := args[0]
	opts, err := store.ParseOptions(args[1:])
	if err != nil {
		return err
	}
	if workitem.ValidateWorkflowActor(opts["actor-role"], "owner") != nil {
		return errors.New("pipeline circuit reset requires actor_role=owner")
	}
	if opts["reason"] == "" {
		return errors.New("pipeline-circuit-reset requires --reason")
	}
	if !store.Contains([]string{"contract", "environment", "runner", "artifact"}, opts["change-type"]) {
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
	// Circuit-reset pack invariant: owner reset identifies the execution input
	// being changed, so it needs a pack snapshot — but not necessarily an active
	// one. A failed claim rolls back its freshly generated TIP, so a deadlock can
	// persist with zero active packs (limiter blocks the claim that would create
	// one); fall back to the latest inactive pack to keep reset reachable.
	pack, err := store.QueryOne(tx, `SELECT * FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, taskID)
	if err != nil {
		pack, err = store.QueryOne(tx, `SELECT * FROM work_item_instruction_packs WHERE work_item_id=? ORDER BY version DESC LIMIT 1`, taskID)
		if err != nil {
			return errors.New("pipeline circuit reset requires an existing instruction pack")
		}
	}
	snapshotHash := store.PersistedText(pack["content_hash"])
	if snapshotHash == "" {
		return errors.New("pipeline circuit reset requires an instruction pack hash")
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
	id := "wie-" + store.ShortID()
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
	return store.OutputOne(db, `SELECT * FROM work_item_events WHERE id=?`, id)
}

func Bind(db *sql.DB, args []string) error {
	if len(args) < 3 {
		return errors.New("pipeline-bind requires run id, lease token, and subagent run id")
	}
	opts, err := store.ParseOptions(args[3:])
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
	return store.OutputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
}

func Renew(db *sql.DB, args []string) error {
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
	return store.OutputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
}

func Model(db *sql.DB, args []string) error {
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
	return store.OutputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
}
