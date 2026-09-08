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
	"os"

	"github.com/earendil-works/task-system/go-pic/internal/store"
)

func Pending(db *sql.DB, _ []string) error {
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
	rows, err := store.QueryMaps(tx, `SELECT * FROM pipeline_runs WHERE status IN ('completed','failed','blocked','expired') AND advanced_at='' ORDER BY rowid`)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	store.WriteJSON(os.Stdout, rows)
	return nil
}

func Runs(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("pipeline-runs requires task id")
	}
	return listJSON(db, args[:1], `SELECT * FROM pipeline_runs WHERE task_id=? ORDER BY rowid DESC`)
}

func Active(db *sql.DB, _ []string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = expireLeases(tx, "", ""); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	rows, err := store.QueryMaps(db, `SELECT * FROM pipeline_runs WHERE status IN ('claimed','running') ORDER BY rowid`)
	if err != nil {
		return err
	}
	store.WriteJSON(os.Stdout, rows)
	return nil
}

func Group(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("pipeline-group requires subagent run id")
	}
	rows, err := store.QueryMaps(db, `SELECT * FROM pipeline_runs WHERE subagent_run_id=? ORDER BY child_index`, args[0])
	if err != nil {
		return err
	}
	store.WriteJSON(os.Stdout, rows)
	return nil
}

// listJSON prints the rows produced by query with the Work Item id from args.
func listJSON(db *sql.DB, args []string, query string) error {
	if len(args) < 1 {
		return errors.New("Work Item id required")
	}
	rows, err := store.QueryMaps(db, query, args[0])
	if err != nil {
		return err
	}
	store.WriteJSON(os.Stdout, rows)
	return nil
}

// workflowPipelineShow returns the full pipeline_runs row for one run id.
func Show(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("pipeline-show requires pipeline run id")
	}
	// Terminal-run diagnostics surface (RLB-GAP-005): pipeline-active only
	// returns claimed/running runs, so blocked/failed/completed run error
	// reasons had no sanctioned read path.
	rows, err := store.QueryMaps(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no pipeline run found: %s", args[0])
	}
	store.WriteJSON(os.Stdout, rows[0])
	return nil
}
