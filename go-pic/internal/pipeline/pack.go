package pipeline

// Pipeline run lifecycle. pipeline_runs rows are the scheduler's durable
// state machine: a claim creates the row with a lease, checkpoint/complete
// transitions record progress, and terminal states carry the artifacts the
// review and integration paths consume. All SQL lives here; the pi-ext
// scheduler drives it through the pic CLI.

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/earendil-works/task-system/go-pic/internal/store"
	"github.com/earendil-works/task-system/go-pic/internal/tip"
	workitem "github.com/earendil-works/task-system/go-pic/internal/work-item"
)

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
		return tip.ActivateInstructionPack(tx, existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var content string
	if err = tx.QueryRow(`SELECT a.content FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.id=?`, checkpointID).Scan(&content); err != nil {
		return err
	}
	plan, err := tip.ParseTaskPlanJSON("```task-plan-json\n" + content + "\n```")
	if err != nil {
		return err
	}
	var node *tip.TaskPlanDocumentNode
	for index := range plan.Nodes {
		if plan.Nodes[index].Key == nodeKey {
			node = &plan.Nodes[index]
			break
		}
	}
	if node == nil {
		return fmt.Errorf("materialized node %s is missing from the approved task graph", nodeKey)
	}
	requirements, err := workitem.ValidateTaskGraphRequirementCoverage(tx, rootID, plan)
	if err != nil {
		return err
	}
	packContent, contentHash, err := tip.MaterializedInstructionPack(*node, plan.Version, requirements)
	if err != nil {
		return err
	}
	var version int
	if err = tx.QueryRow(`SELECT COALESCE(MAX(version),0)+1 FROM work_item_instruction_packs WHERE work_item_id=?`, taskID).Scan(&version); err != nil {
		return err
	}
	packID := "wip-" + store.ShortID()
	if _, err = tx.Exec(`INSERT INTO work_item_instruction_packs(id,work_item_id,checkpoint_id,version,status,content_json,content_hash) VALUES(?,?,?,?,'inactive',?,?)`, packID, taskID, checkpointID, version, string(packContent), contentHash); err != nil {
		return err
	}
	return tip.ActivateInstructionPack(tx, packID)
}

func expireLeases(tx *sql.Tx, taskID, stage string) error {
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
