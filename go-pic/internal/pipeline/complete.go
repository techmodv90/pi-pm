package pipeline

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/earendil-works/task-system/go-pic/internal/store"
)

// TerminalStatuses enumerates the terminal pipeline_runs states.
var TerminalStatuses = []string{"completed", "failed", "blocked", "cancelled"}

func Complete(db *sql.DB, args []string) error {
	if len(args) < 3 {
		return errors.New("pipeline-complete requires run id, lease token, and status")
	}
	if !store.Contains(TerminalStatuses, args[2]) {
		return fmt.Errorf("invalid pipeline terminal status: %s", args[2])
	}
	opts, err := store.ParseOptions(args[3:])
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
	result, err := tx.Exec(`UPDATE pipeline_runs SET status=?,result_json=?,error=?,updated_at=datetime('now'),completed_at=datetime('now') WHERE id=? AND lease_token=? AND status IN (`+currentStatuses+`) AND NOT (status='completed' AND stage='review') AND datetime(lease_expires_at)>datetime('now')`, args[2], store.NormalizeJSONText(opts["result-json"]), opts["error"], args[0], args[1])
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("pipeline completion rejected: stale or invalid lease")
	}
	if store.Contains([]string{"failed", "blocked", "cancelled"}, args[2]) {
		if _, err = tx.Exec(`UPDATE work_items SET status='open',claimed_at='',claimed_by='' WHERE id=(SELECT task_id FROM pipeline_runs WHERE id=?) AND status='in_progress' AND NOT EXISTS (SELECT 1 FROM pipeline_runs active WHERE active.task_id=work_items.id AND active.status IN ('claimed','running'))`, args[0]); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return store.OutputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
}

func Checkpoint(db *sql.DB, args []string) error {
	if len(args) < 3 {
		return errors.New("pipeline-checkpoint requires run id, lease token, and checkpoint")
	}
	column := map[string]string{"integrated": "integrated_at", "artifact_saved": "artifact_saved_at", "advanced": "advanced_at"}[args[2]]
	if column == "" {
		return fmt.Errorf("invalid pipeline checkpoint: %s", args[2])
	}
	opts, err := store.ParseOptions(args[3:])
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
			return store.OutputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
		}
		var terminal int
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pipeline_runs WHERE id=? AND status IN ('completed','failed','blocked','cancelled','expired') AND advanced_at<>'')`, args[0]).Scan(&terminal); err != nil {
			return err
		}
		if terminal != 0 {
			return store.OutputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
		}
	}
	statePredicate := map[string]string{
		"artifact_saved": `stage IN ('worker','autofix') AND status='completed'`,
		"advanced":       `status IN ('completed','failed','blocked','cancelled','expired')`,
		"integrated":     `stage IN ('worker','autofix') AND status='completed' AND EXISTS(SELECT 1 FROM pipeline_runs review WHERE review.task_id=pipeline_runs.task_id AND review.stage='review' AND review.status='completed' AND json_valid(review.result_json) AND json_extract(review.result_json,'$.review_status')='passed' AND json_extract(review.result_json,'$.candidate_run_id')=pipeline_runs.id AND json_extract(review.result_json,'$.candidate_patch_hash')=pipeline_runs.integrated_patch_hash)`,
	}[args[2]]
	if ok, checkErr := store.RowExists(db, `SELECT 1 FROM pipeline_runs WHERE id=? AND lease_token=? AND `+statePredicate, args[0], args[1]); checkErr != nil {
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
	return store.OutputOne(db, `SELECT * FROM pipeline_runs WHERE id=?`, args[0])
}
