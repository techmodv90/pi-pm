package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Explicit schema statement classification replaces the retired
// isLegacyBootstrapStatement prefix matcher. Every bootstrap statement is
// classified exactly once into one of two ordered lists: canonical statements
// create the Work Item schema on every database, and legacy statements create
// the retired Epic/Task tables only while a database still carries them.
// Classification was derived mechanically from the previous prefix matcher, with
// one deliberate correction: trg_work_item_pack_immutable guards the canonical
// work_item_instruction_packs table and is now created on fresh databases too.

// WorkItemOwnerDecisionsTableSQL carries the RRI deferral surface (REQ-F1-3):
// decision='deferred' rows persist a deferred P0/P1 question with its
// owner-recorded reason and RRI artifact linkage, so completion_report_id is
// nullable — deferral rows precede any completion report.
// DB is the handle a migration step runs against. Every step now runs on
// one transaction, so both *sql.DB (ad-hoc use outside the runner) and *sql.Tx
// satisfy it.
type DB interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Migration is one ordered schema step. Every step is one-shot and
// transactional: it is skipped once its version is recorded, so an
// already-migrated database performs no DDL or data mutation on later opens,
// and the step's operations plus its version record commit or roll back
// together — a crash leaves the step fully applied or not at all.
type Migration struct {
	Version    int
	Name       string
	LegacyOnly bool
	Apply      func(db DB) error
}

func migrationSteps() []Migration {
	return []Migration{
		{Version: 1, Name: "pre_reconcile_schema", Apply: ReconcileLegacySchema},
		{Version: 2, Name: "artifact_stage_widening", Apply: MigrateArtifactStageSchema},
		{Version: 3, Name: "pipeline_columns_reconcile", Apply: ApplyPipelineColumnMigrations},
		{Version: 4, Name: "canonical_baseline", Apply: func(db DB) error {
			return applyStatements(db, CanonicalSchemaStatements)
		}},
		{Version: 5, Name: "legacy_schema_bootstrap", LegacyOnly: true, Apply: func(db DB) error {
			return applyStatements(db, LegacySchemaStatements)
		}},
		{Version: 6, Name: "canonical_backfills", Apply: ApplyCanonicalBackfills},
		{Version: 7, Name: "legacy_pack_backfills", LegacyOnly: true, Apply: func(db DB) error {
			if err := MigrateLegacyWorkItemInstructionPacks(db); err != nil {
				return err
			}
			return ApplyLegacyPackBackfills(db)
		}},
		{Version: 8, Name: "decomposition_policy_projection", Apply: ApplyDecompositionProjectionColumns},
		{Version: 9, Name: "blueprint_annotation_evidence", Apply: ApplyBlueprintAnnotationEvidenceColumn},
	}
}

// ApplyBlueprintAnnotationEvidenceColumn adds the blueprint disposition
// evidence column (OB-F3-3) to workflow_checkpoints on databases created
// before the annotation review loop. The ALTER is guarded by columnExists so a
// re-run against an already-widened table (the older-binary test path clears
// schema_migrations records) stays idempotent, and by TableExists so partial
// baseline shapes without the table are left to the canonical statement pass.
func ApplyBlueprintAnnotationEvidenceColumn(db DB) error {
	if !TableExists(db, "workflow_checkpoints") {
		return nil
	}
	present, err := columnExists(db, "workflow_checkpoints", "dispositions_json")
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE workflow_checkpoints ADD COLUMN dispositions_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("blueprint annotation evidence migration: %w", err)
	}
	return nil
}

// ApplyDecompositionProjectionColumns adds the decomposition policy v2
// projection surface: edge rationales on the canonical blocking-edge table and
// per-Work-Item decomposition/provenance columns recorded by materialization.
// decomposition_mode defaults to 'vertical' — the policy's absent-mode default —
// so every Work Item row carries an explicit mode. All statements are additive
// with constant defaults, so v1 lineage keeps its exact behavior with empty
// auxiliary values. Each ALTER is guarded by a column-exists check so a re-run
// against an already-widened table (the older-binary simulation path clears
// version records) stays idempotent.
// Note: the rationale lands on work_item_relations — the canonical blocking-edge
// table materialization and `pic show` read — not on the legacy
// work_item_dependencies table retired by the canonical backfills.
func ApplyDecompositionProjectionColumns(db DB) error {
	projections := []struct{ table, column, ddl string }{
		{"work_item_relations", "rationale", `ALTER TABLE work_item_relations ADD COLUMN rationale TEXT NOT NULL DEFAULT ''`},
		{"work_items", "decomposition_mode", `ALTER TABLE work_items ADD COLUMN decomposition_mode TEXT NOT NULL DEFAULT 'vertical'`},
		{"work_items", "decomposition_reason", `ALTER TABLE work_items ADD COLUMN decomposition_reason TEXT NOT NULL DEFAULT ''`},
		{"work_items", "paired_contract_node", `ALTER TABLE work_items ADD COLUMN paired_contract_node TEXT NOT NULL DEFAULT ''`},
		{"work_items", "source_graph_artifact_id", `ALTER TABLE work_items ADD COLUMN source_graph_artifact_id TEXT NOT NULL DEFAULT ''`},
		{"work_items", "source_graph_revision", `ALTER TABLE work_items ADD COLUMN source_graph_revision INTEGER NOT NULL DEFAULT 0`},
		{"work_items", "source_graph_content_hash", `ALTER TABLE work_items ADD COLUMN source_graph_content_hash TEXT NOT NULL DEFAULT ''`},
	}
	for _, projection := range projections {
		present, err := columnExists(db, projection.table, projection.column)
		if err != nil {
			return err
		}
		if present {
			continue
		}
		if _, err := db.Exec(projection.ddl); err != nil {
			return fmt.Errorf("decomposition projection migration: %w", err)
		}
	}
	return nil
}

func columnExists(db DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func ReconcileLegacySchema(db DB) error {
	if err := RemoveLegacyTIPSchema(db); err != nil {
		return fmt.Errorf("remove legacy TIP schema: %w", err)
	}
	if err := MigrateEpicWorkflowSchema(db); err != nil {
		return fmt.Errorf("migrate legacy workflow schema: %w", err)
	}
	if TableExists(db, "work_item_materializations") {
		var tableSQL string
		if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='work_item_materializations'`).Scan(&tableSQL); err != nil {
			return err
		}
		if strings.Contains(tableSQL, "work_item_id TEXT NOT NULL UNIQUE") {
			// The runner holds foreign_keys=OFF on the pinned migration
			// connection, so the copy below never enforces the rebuilt FKs;
			// the pragma itself is a no-op inside a transaction anyway.
			if _, err := db.Exec(`CREATE TABLE work_item_materializations_v2 (root_work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, checkpoint_id TEXT NOT NULL REFERENCES workflow_checkpoints(id), node_key TEXT NOT NULL, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, created_at TEXT DEFAULT (datetime('now')), PRIMARY KEY(root_work_item_id,checkpoint_id,node_key));
				INSERT INTO work_item_materializations_v2 SELECT * FROM work_item_materializations;
				DROP TABLE work_item_materializations;
				ALTER TABLE work_item_materializations_v2 RENAME TO work_item_materializations`); err != nil {
				return fmt.Errorf("migrate work item materializations: %w", err)
			}
		}
	}
	return nil
}

// ApplyMigrations applies the ordered schema steps once per database.
// Legacy steps are skipped (and never recorded) on databases that never carried
// the retired Epic/Task tables.
func ApplyMigrations(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT DEFAULT (datetime('now')))`); err != nil {
		return err
	}
	applied := map[int]bool{}
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return err
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	legacySchema := TableExists(db, "tasks") || TableExists(db, "epics")
	for _, migration := range migrationSteps() {
		if migration.LegacyOnly && !legacySchema {
			continue
		}
		if applied[migration.Version] {
			continue
		}
		if err := ApplyMigration(context.Background(), db, migration); err != nil {
			return fmt.Errorf("schema migration %03d_%s: %w", migration.Version, migration.Name, err)
		}
	}
	// Convergent per-open backfill: retired dependency/gate edge tables keep
	// receiving rows after the version-gated migration applied (post-migration
	// APM imports), and readiness reads only their work_item_relations projection.
	if err := ApplyConvergentDependencyBackfill(db); err != nil {
		return err
	}
	return nil
}

// ApplyMigration runs one step and records its version inside a single
// transaction on one pinned connection. foreign_keys and legacy_alter_table are
// connection-scoped in SQLite and cannot change inside a transaction, and
// database/sql gives no affinity between db.Exec and db.Begin — a pragma sent
// through the pool is not guaranteed to land on the connection the transaction
// ends up on. The pragma setup, the step, the version record, and the pragma
// restore therefore all run on one explicitly pinned *sql.Conn.
func ApplyMigration(ctx context.Context, db *sql.DB, migration Migration) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var foreignKeys, legacyAlterTable int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, `PRAGMA legacy_alter_table`).Scan(&legacyAlterTable); err != nil {
		return err
	}
	restore := func() error {
		if _, err := conn.ExecContext(ctx, pragmaEnabled("legacy_alter_table", legacyAlterTable)); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, pragmaEnabled("foreign_keys", foreignKeys)); err != nil {
			return err
		}
		return nil
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA legacy_alter_table=ON`); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return errors.Join(err, restore())
	}
	finished := false
	defer func() {
		if !finished {
			_ = tx.Rollback()
			_ = restore()
		}
	}()
	if err := migration.Apply(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO schema_migrations(version, name) VALUES(?, ?)`, migration.Version, migration.Name); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	finished = true
	return restore()
}

// applyStatements executes one classified statement batch in order.
func applyStatements(db DB, statements []string) error {
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("initialize schema statement %q: %w", stmt, err)
		}
	}
	return nil
}
