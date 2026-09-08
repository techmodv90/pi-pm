package main

// Pipeline run diagnostics and integration-proof surfaces extracted from the
// oversized pipeline.go / work_items.go monoliths (RLB-GAP-005/006 fixes).

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/earendil-works/task-system/go-pic/internal/store"
	"os"
)

// workflowPipelineShow returns the full pipeline_runs row for one run id.
func workflowPipelineShow(db *sql.DB, args []string) error {
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
