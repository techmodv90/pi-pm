package schema

import (
	"database/sql"
	"strings"
)

// ApplyPipelineColumnMigrations adds columns that predate a table's current
// definition and rebuilds tables whose shape or foreign keys drifted.
func ApplyPipelineColumnMigrations(db DB) error {
	for _, migration := range []struct{ table, column, definition string }{
		{"epics", "workflow_mode", "TEXT DEFAULT 'full'"},
		{"epics", "design_status", "TEXT DEFAULT ''"},
		{"epics", "owner_status", "TEXT DEFAULT ''"},
		{"completion_reports", "pipeline_run_id", "TEXT DEFAULT ''"},
		{"verification_reports", "pipeline_run_id", "TEXT DEFAULT ''"},
		{"verification_reports", "effective_contract_snapshot_id", "TEXT DEFAULT ''"},
		{"verification_reports", "effective_contract_snapshot_hash", "TEXT DEFAULT ''"},
		{"verification_reports", "completion_report_id", "TEXT DEFAULT ''"},
		{"work_item_verification_reports", "completion_report_id", "TEXT REFERENCES work_item_completion_reports(id)"},
		{"work_item_verification_reports", "verified_by_role", "TEXT NOT NULL DEFAULT ''"},
		{"work_item_verification_reports", "pipeline_high_water_rowid", "INTEGER NOT NULL DEFAULT 0"},
		{"work_item_verification_reports", "rri_t_json", "TEXT NOT NULL DEFAULT ''"},
		{"work_item_corrective_bugs", "owner_approval_required", "INTEGER NOT NULL DEFAULT 0"},
		{"work_item_owner_decisions", "decided_by_role", "TEXT NOT NULL DEFAULT ''"},
		{"verification_reports", "superseded_at", "TEXT DEFAULT ''"},
		{"verification_reports", "superseded_by_report_id", "TEXT DEFAULT ''"},
		{"pipeline_runs", "environment_fingerprint", "TEXT DEFAULT ''"},
		{"pipeline_runs", "base_commit", "TEXT DEFAULT ''"},
		{"tasks", "origin", "TEXT DEFAULT 'manual'"},
		{"tasks", "revision", "INTEGER DEFAULT 1"},
		{"tasks", "reviewed_instruction_pack_id", "TEXT DEFAULT ''"},
		{"task_instruction_packs", "content_schema_version", "INTEGER NOT NULL DEFAULT 1"},
		{"task_instruction_packs", "revision_kind", "TEXT NOT NULL DEFAULT 'initial'"},
		{"task_instruction_packs", "skill_families_json", "TEXT NOT NULL DEFAULT '[]'"},
		{"task_instruction_packs", "effective_contract_snapshot_id", "TEXT DEFAULT ''"},
		{"task_instruction_packs", "effective_contract_snapshot_hash", "TEXT DEFAULT ''"},
		{"requirements", "contract_key", "TEXT DEFAULT ''"},
		{"requirements", "inherit_to_descendants", "INTEGER NOT NULL DEFAULT 0"},
		{"contract_operations", "reactivated_at", "TEXT DEFAULT ''"},
		{"completion_reports", "instruction_pack_id", "TEXT DEFAULT ''"},
		{"completion_reports", "instruction_pack_version", "INTEGER DEFAULT 0"},
		{"completion_reports", "instruction_pack_hash", "TEXT DEFAULT ''"},
		{"completion_reports", "report_markdown", "TEXT DEFAULT ''"},
		{"completion_reports", "effective_contract_snapshot_id", "TEXT DEFAULT ''"},
		{"completion_reports", "effective_contract_snapshot_hash", "TEXT DEFAULT ''"},
		{"pipeline_runs", "instruction_pack_id", "TEXT DEFAULT ''"},
		{"pipeline_runs", "instruction_pack_version", "INTEGER DEFAULT 0"},
		{"pipeline_runs", "instruction_pack_hash", "TEXT DEFAULT ''"},
		{"pipeline_runs", "effective_contract_snapshot_id", "TEXT DEFAULT ''"},
		{"pipeline_runs", "effective_contract_snapshot_hash", "TEXT DEFAULT ''"},
		{"pipeline_runs", "agent_model", "TEXT DEFAULT ''"},
		{"pipeline_runs", "child_index", "INTEGER DEFAULT 0"},
		{"pipeline_runs", "integrated_patch_path", "TEXT DEFAULT ''"},
		{"pipeline_runs", "integrated_patch_hash", "TEXT DEFAULT ''"},
		{"pipeline_runs", "integrated_at", "TEXT DEFAULT ''"},
		{"pipeline_runs", "artifact_saved_at", "TEXT DEFAULT ''"},
		{"pipeline_runs", "candidate_run_id", "TEXT DEFAULT ''"},
		{"pipeline_runs", "candidate_patch_hash", "TEXT DEFAULT ''"},
		{"pipeline_runs", "review_fix_cycle", "INTEGER DEFAULT 0"},
		{"pipeline_runs", "advanced_at", "TEXT DEFAULT ''"},
		{"pipeline_runs", "migration_status", "TEXT DEFAULT 'legacy'"},
		{"work_items", "review_status", "TEXT DEFAULT 'pending'"},
		{"work_items", "review_notes", "TEXT DEFAULT ''"},
		{"work_items", "planning_depth", "TEXT DEFAULT 'full'"},
		{"pipeline_runs", "profile_version", "INTEGER DEFAULT 0"},
		{"pipeline_runs", "profile_hash", "TEXT DEFAULT ''"},
	} {
		if TableExists(db, migration.table) && !HasColumn(db, migration.table, migration.column) {
			if _, err := db.Exec(`ALTER TABLE ` + migration.table + ` ADD COLUMN ` + migration.column + ` ` + migration.definition); err != nil && !HasColumn(db, migration.table, migration.column) {
				return err
			}
		}
	}
	if HasColumn(db, "pipeline_runs", "integrated_patch") {
		if _, err := db.Exec(`ALTER TABLE pipeline_runs DROP COLUMN integrated_patch`); err != nil && HasColumn(db, "pipeline_runs", "integrated_patch") {
			return err
		}
	}
	var pipelineSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='pipeline_runs'`).Scan(&pipelineSQL); err == nil && (!strings.Contains(pipelineSQL, "'rri'") || strings.Contains(pipelineSQL, "REFERENCES tasks(id)")) {
		// Retired legacy stage vocabulary (scan,worker,review,qa,verify) violates
		// the canonical stage CHECK, so translate it during the rebuild copy.
		// Distinct targets (qa→autofix, verify→review) preserve
		// UNIQUE(task_id, stage, attempt) for databases that carried both.
		pipelineStageExprs := map[string]string{
			"stage": `CASE stage WHEN 'qa' THEN 'autofix' WHEN 'verify' THEN 'review' ELSE stage END`,
		}
		if err := rebuildSchemaTable(db, "pipeline_runs", PipelineRunsTableSQL, pipelineStageExprs); err != nil {
			return err
		}
	}
	var completionRunTarget string
	if err := db.QueryRow(`SELECT "table" FROM pragma_foreign_key_list('work_item_completion_reports') WHERE "from"='pipeline_run_id'`).Scan(&completionRunTarget); err != nil && err != sql.ErrNoRows {
		return err
	}
	if completionRunTarget == "pipeline_runs__workflow_migration" {
		if err := rebuildSchemaTable(db, "work_item_completion_reports", WorkItemCompletionReportsTableSQL); err != nil {
			return err
		}
	}
	var ownerDecisionSQL string
	ownerDecisionErr := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='work_item_owner_decisions'`).Scan(&ownerDecisionSQL)
	if ownerDecisionErr != nil && ownerDecisionErr != sql.ErrNoRows {
		return ownerDecisionErr
	}
	if ownerDecisionErr == nil && !strings.Contains(ownerDecisionSQL, "question_id") {
		// The legacy shape (decision IN ('accepted','rejected'),
		// completion_report_id NOT NULL) cannot hold RRI deferral rows, so
		// rebuild; the canonical statement batch re-runs after this step and
		// recreates the item index on the rebuilt table.
		if err := rebuildSchemaTable(db, "work_item_owner_decisions", WorkItemOwnerDecisionsTableSQL); err != nil {
			return err
		}
	}
	return nil
}

// ApplyCanonicalBackfills reconciles canonical evidence that may have completed
// after a Work Item row was last written. The UPDATEs are convergent (guarded
// by WHERE clauses), so they re-run on every open exactly as before.
func ApplyCanonicalBackfills(db DB) error {
	if _, err := db.Exec(`UPDATE work_items SET review_status='passed' WHERE status='done' AND type IN ('task','bug','chore') AND EXISTS (
		SELECT 1 FROM work_item_owner_decisions decision
		JOIN work_item_completion_reports completion ON completion.id=decision.completion_report_id AND completion.work_item_id=decision.work_item_id AND completion.status='done'
		JOIN work_item_instruction_packs pack ON pack.id=completion.instruction_pack_id AND pack.work_item_id=completion.work_item_id AND pack.version=completion.instruction_pack_version AND pack.content_hash=completion.instruction_pack_hash AND pack.status='active'
		JOIN work_item_verification_reports verification ON verification.work_item_id=completion.work_item_id AND verification.completion_report_id=completion.id AND verification.status='passed'
		WHERE decision.work_item_id=work_items.id AND decision.decision='accepted'
	)`); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE work_items SET status='done',claimed_at='',claimed_by='',review_status='passed' WHERE type IN ('task','bug','chore') AND EXISTS (
		SELECT 1 FROM work_item_verification_reports verification
		JOIN work_item_completion_reports completion ON completion.id=verification.completion_report_id AND completion.work_item_id=verification.work_item_id AND completion.status='done'
		JOIN work_item_instruction_packs pack ON pack.id=completion.instruction_pack_id AND pack.work_item_id=completion.work_item_id AND pack.version=completion.instruction_pack_version AND pack.content_hash=completion.instruction_pack_hash AND pack.status='active'
		JOIN pipeline_runs candidate ON candidate.id=completion.pipeline_run_id AND candidate.task_id=completion.work_item_id AND candidate.integrated_at<>'' AND candidate.integrated_patch_hash<>''
		JOIN pipeline_runs review ON review.task_id=completion.work_item_id AND review.stage='review' AND review.status='completed' AND review.candidate_run_id=candidate.id AND json_valid(review.result_json) AND json_extract(review.result_json,'$.review_status')='passed' AND json_extract(review.result_json,'$.candidate_patch_hash')=candidate.integrated_patch_hash
		WHERE verification.work_item_id=work_items.id AND verification.status='passed' AND (
			verification.pipeline_high_water_rowid=0 AND NOT EXISTS (SELECT 1 FROM pipeline_runs later WHERE later.task_id=verification.work_item_id AND datetime(later.created_at)>datetime(verification.created_at))
			OR verification.pipeline_high_water_rowid>0 AND NOT EXISTS (SELECT 1 FROM pipeline_runs later WHERE later.task_id=verification.work_item_id AND later.rowid>verification.pipeline_high_water_rowid)
		) AND NOT EXISTS (SELECT 1 FROM work_item_owner_decisions decision WHERE decision.work_item_id=verification.work_item_id AND decision.completion_report_id=verification.completion_report_id AND decision.decision='rejected')
	)`); err != nil {
		return err
	}
	if err := ApplyConvergentDependencyBackfill(db); err != nil {
		return err
	}
	return nil
}

// ApplyConvergentDependencyBackfill projects retired dependency and gate edge
// tables onto work_item_relations blocks/gates rows. The migration runner
// applies version 6 exactly once, but edges keep arriving after that (the APM
// import writes work_item_dependencies rows post-migration), and the readiness
// SQL (workitem.ReadySQL) reads only work_item_relations — so this backfill must
// converge on every open, not just at migration time. INSERT OR IGNORE keeps it
// idempotent under the wir-migrated- id scheme.
func ApplyConvergentDependencyBackfill(db DB) error {
	// Minimal schemas (hand-crafted fixtures recording migration versions
	// without the tables those versions created) have nothing to project;
	// skip instead of failing initDB on tables every migrated real database
	// already has.
	var tableCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('work_item_dependencies','work_item_gates','work_item_relations')`).Scan(&tableCount); err != nil {
		return err
	}
	if tableCount < 3 {
		return nil
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO work_item_relations(id,work_item_id,relation_type,related_work_item_id,created_at)
		SELECT 'wir-migrated-'||id,work_item_id,'blocks',depends_on_work_item_id,created_at FROM work_item_dependencies
		UNION ALL
		SELECT 'wir-migrated-'||id,work_item_id,'gates',gate_work_item_id,created_at FROM work_item_gates`); err != nil {
		return err
	}
	return nil
}

// ApplyLegacyPackBackfills recomputes legacy pack revision kinds and supersedes
// legacy verification reports after migration. Both are convergent.
func ApplyLegacyPackBackfills(db DB) error {
	if _, err := db.Exec(`UPDATE task_instruction_packs AS current SET revision_kind=CASE
	WHEN NOT EXISTS(SELECT 1 FROM task_instruction_packs previous WHERE previous.task_id=current.task_id AND previous.version<current.version) THEN 'initial'
	WHEN COALESCE(current.effective_contract_snapshot_hash,'')!=COALESCE((SELECT previous.effective_contract_snapshot_hash FROM task_instruction_packs previous WHERE previous.task_id=current.task_id AND previous.version<current.version ORDER BY previous.version DESC LIMIT 1),'') THEN 'contract'
	WHEN current.files_json!=(SELECT previous.files_json FROM task_instruction_packs previous WHERE previous.task_id=current.task_id AND previous.version<current.version ORDER BY previous.version DESC LIMIT 1) OR current.constraints_json!=(SELECT previous.constraints_json FROM task_instruction_packs previous WHERE previous.task_id=current.task_id AND previous.version<current.version ORDER BY previous.version DESC LIMIT 1) THEN 'scope'
	WHEN current.verification_json!=(SELECT previous.verification_json FROM task_instruction_packs previous WHERE previous.task_id=current.task_id AND previous.version<current.version ORDER BY previous.version DESC LIMIT 1) THEN 'verification'
	ELSE 'execution' END`); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE verification_reports AS old SET superseded_at=COALESCE((SELECT newer.created_at FROM verification_reports newer WHERE ((newer.task_id=old.task_id AND old.task_id IS NOT NULL) OR (newer.epic_id=old.epic_id AND old.epic_id IS NOT NULL)) AND newer.rowid>old.rowid ORDER BY newer.rowid DESC LIMIT 1),''), superseded_by_report_id=COALESCE((SELECT newer.id FROM verification_reports newer WHERE ((newer.task_id=old.task_id AND old.task_id IS NOT NULL) OR (newer.epic_id=old.epic_id AND old.epic_id IS NOT NULL)) AND newer.rowid>old.rowid ORDER BY newer.rowid DESC LIMIT 1),'') WHERE superseded_at='' AND EXISTS(SELECT 1 FROM verification_reports newer WHERE ((newer.task_id=old.task_id AND old.task_id IS NOT NULL) OR (newer.epic_id=old.epic_id AND old.epic_id IS NOT NULL)) AND newer.rowid>old.rowid)`); err != nil {
		return err
	}
	return nil
}
