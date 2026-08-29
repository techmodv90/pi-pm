package main

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

var canonicalSchemaStatements = []string{
		workItemsTableSQL,
		`CREATE TABLE IF NOT EXISTS work_item_labels (work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, label TEXT NOT NULL, PRIMARY KEY(work_item_id,label))`,
		`CREATE TABLE IF NOT EXISTS work_item_dependencies (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, depends_on_work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, created_at TEXT DEFAULT (datetime('now')), UNIQUE(work_item_id,depends_on_work_item_id), CHECK(work_item_id!=depends_on_work_item_id))`,
		`CREATE TABLE IF NOT EXISTS work_item_gates (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, gate_work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, created_at TEXT DEFAULT (datetime('now')), UNIQUE(work_item_id,gate_work_item_id), CHECK(work_item_id!=gate_work_item_id))`,
		`CREATE TABLE IF NOT EXISTS work_item_relations (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, relation_type TEXT NOT NULL CHECK(relation_type IN ('blocks','gates','related')), related_work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, created_at TEXT DEFAULT (datetime('now')), UNIQUE(work_item_id,relation_type,related_work_item_id), CHECK(work_item_id!=related_work_item_id))`,
		workItemArtifactsTableSQL,
		workflowCheckpointsTableSQL,
		`CREATE TABLE IF NOT EXISTS implementation_authorizations (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, task_graph_checkpoint_id TEXT NOT NULL REFERENCES workflow_checkpoints(id), authorized_by TEXT NOT NULL, revoked_at TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')))`,
		`CREATE TABLE IF NOT EXISTS work_item_materializations (root_work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, checkpoint_id TEXT NOT NULL REFERENCES workflow_checkpoints(id), node_key TEXT NOT NULL, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, created_at TEXT DEFAULT (datetime('now')), PRIMARY KEY(root_work_item_id,checkpoint_id,node_key))`,
		`CREATE TABLE IF NOT EXISTS work_item_instruction_packs (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, checkpoint_id TEXT NOT NULL REFERENCES workflow_checkpoints(id), version INTEGER NOT NULL CHECK(version>0), status TEXT NOT NULL DEFAULT 'inactive' CHECK(status IN ('inactive','active','stale')), content_json TEXT NOT NULL, content_hash TEXT NOT NULL, activated_at TEXT DEFAULT '', stale_at TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')), UNIQUE(work_item_id,version))`,
		`CREATE TABLE IF NOT EXISTS work_item_verification_reports (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, checkpoint_id TEXT DEFAULT '', completion_report_id TEXT REFERENCES work_item_completion_reports(id), status TEXT NOT NULL CHECK(status IN ('passed','failed','partial','blocked')), summary TEXT DEFAULT '', verified_by_role TEXT NOT NULL DEFAULT '', pipeline_high_water_rowid INTEGER NOT NULL DEFAULT 0, rri_t_json TEXT NOT NULL DEFAULT '', created_at TEXT DEFAULT (datetime('now')))`,
		`CREATE TABLE IF NOT EXISTS work_item_corrective_bugs (verification_report_id TEXT PRIMARY KEY REFERENCES work_item_verification_reports(id) ON DELETE CASCADE, bug_work_item_id TEXT NOT NULL UNIQUE REFERENCES work_items(id) ON DELETE CASCADE, owner_approval_required INTEGER NOT NULL DEFAULT 0, created_at TEXT DEFAULT (datetime('now')))`,
		workItemCompletionReportsTableSQL,
		workItemEscalationsTableSQL,
		`CREATE TABLE IF NOT EXISTS work_item_owner_decisions (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, completion_report_id TEXT NOT NULL REFERENCES work_item_completion_reports(id), decision TEXT NOT NULL CHECK(decision IN ('accepted','rejected')), notes TEXT DEFAULT '', decided_by_role TEXT NOT NULL DEFAULT '', created_at TEXT DEFAULT (datetime('now')))`,
		`CREATE TABLE IF NOT EXISTS work_item_delivery_states (work_item_id TEXT PRIMARY KEY REFERENCES work_items(id) ON DELETE CASCADE, integration_mode TEXT NOT NULL CHECK(integration_mode IN ('branch','coordination')), branch_name TEXT DEFAULT '', base_branch TEXT DEFAULT 'develop', base_commit TEXT DEFAULT '', verified_head TEXT DEFAULT '', verification_report_id TEXT DEFAULT '', merge_status TEXT NOT NULL DEFAULT '' CHECK(merge_status IN ('','merge_pending','merged','blocked')), merged_commit TEXT DEFAULT '', merge_error TEXT DEFAULT '', updated_at TEXT DEFAULT (datetime('now')))`,
		`CREATE TABLE IF NOT EXISTS work_item_aggregate_owner_decisions (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, verification_report_id TEXT NOT NULL REFERENCES work_item_verification_reports(id), decision TEXT NOT NULL CHECK(decision IN ('accepted','rejected')), notes TEXT DEFAULT '', decided_by_role TEXT NOT NULL DEFAULT '', created_at TEXT DEFAULT (datetime('now')))`,
		`CREATE TABLE IF NOT EXISTS work_item_events (id TEXT PRIMARY KEY, work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE, event_type TEXT NOT NULL, actor_role TEXT DEFAULT '', actor_model TEXT DEFAULT '', summary TEXT DEFAULT '', payload_json TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')))`,
		ownedWorkflowTableSQL["requirements"],
		ownedWorkflowTableSQL["owner_decisions"],
		`CREATE TABLE IF NOT EXISTS session_activity (session_id TEXT PRIMARY KEY, task_id TEXT DEFAULT '', status TEXT DEFAULT 'idle' CHECK(status IN ('active','idle')), current_step_label TEXT DEFAULT '', last_skill TEXT DEFAULT '', updated_at TEXT DEFAULT (datetime('now')))`,
		pipelineRunsTableSQL,
		workItemProfilesTableSQL,
		`UPDATE work_items SET review_status='passed' WHERE status='done' AND type IN ('task','bug','chore') AND EXISTS (
			SELECT 1 FROM work_item_owner_decisions decision
			JOIN work_item_completion_reports completion ON completion.id=decision.completion_report_id AND completion.work_item_id=decision.work_item_id AND completion.status='done'
			JOIN work_item_instruction_packs pack ON pack.id=completion.instruction_pack_id AND pack.work_item_id=completion.work_item_id AND pack.version=completion.instruction_pack_version AND pack.content_hash=completion.instruction_pack_hash AND pack.status='active'
			JOIN work_item_verification_reports verification ON verification.work_item_id=completion.work_item_id AND verification.completion_report_id=completion.id AND verification.status='passed'
			WHERE decision.work_item_id=work_items.id AND decision.decision='accepted'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_work_items_parent ON work_items(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_work_items_status_claim ON work_items(type,status,deferred,claimed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_labels_label ON work_item_labels(label)`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_dependencies_item ON work_item_dependencies(work_item_id)`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_dependencies_blocker ON work_item_dependencies(depends_on_work_item_id)`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_gates_item ON work_item_gates(work_item_id)`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_relations_item ON work_item_relations(work_item_id,relation_type)`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_artifacts_item_stage ON work_item_artifacts(work_item_id,stage,revision DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_checkpoints_item_stage ON workflow_checkpoints(work_item_id,stage,artifact_revision DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_implementation_authorizations_item ON implementation_authorizations(work_item_id,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_materializations_root ON work_item_materializations(root_work_item_id,checkpoint_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_work_item_instruction_packs_active ON work_item_instruction_packs(work_item_id) WHERE status='active'`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_instruction_packs_checkpoint ON work_item_instruction_packs(checkpoint_id,status)`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_verification_reports_item ON work_item_verification_reports(work_item_id,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_completion_reports_item ON work_item_completion_reports(work_item_id,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_owner_decisions_item ON work_item_owner_decisions(work_item_id,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_delivery_states_status ON work_item_delivery_states(merge_status,integration_mode)`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_aggregate_owner_decisions_item ON work_item_aggregate_owner_decisions(work_item_id,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_events_item ON work_item_events(work_item_id,created_at DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_requirements_task_key ON requirements(task_id,requirement_key) WHERE task_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_requirements_epic_key ON requirements(epic_id,requirement_key) WHERE epic_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_requirements_task ON requirements(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_requirements_contract_key ON requirements(contract_key) WHERE contract_key!=''`,
		`CREATE INDEX IF NOT EXISTS idx_pipeline_runs_task ON pipeline_runs(task_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_work_item_profiles_item ON work_item_profiles(work_item_id,profile_name)`,
		`CREATE INDEX IF NOT EXISTS idx_pipeline_runs_pending ON pipeline_runs(advanced_at,status,task_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_pipeline_runs_active_stage ON pipeline_runs(task_id, stage) WHERE status IN ('claimed','running')`,
		`CREATE TRIGGER IF NOT EXISTS trg_work_item_artifact_immutable BEFORE UPDATE ON work_item_artifacts BEGIN SELECT RAISE(ABORT,'work item artifacts are immutable'); END`,
		`DROP TRIGGER IF EXISTS trg_work_item_artifact_delete_immutable`,
		`CREATE TRIGGER trg_work_item_artifact_delete_immutable BEFORE DELETE ON work_item_artifacts WHEN EXISTS(SELECT 1 FROM workflow_checkpoints WHERE artifact_id=OLD.id) BEGIN SELECT RAISE(ABORT,'approved work item artifacts are immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_work_item_pack_immutable BEFORE UPDATE OF work_item_id,checkpoint_id,version,content_json,content_hash ON work_item_instruction_packs BEGIN SELECT RAISE(ABORT,'work item instruction packs are immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_keyed_requirement_content_immutable BEFORE UPDATE OF requirement_key,contract_key,inherit_to_descendants,title,description,acceptance_criteria ON requirements WHEN OLD.contract_key!='' BEGIN SELECT RAISE(ABORT,'keyed requirement content is immutable; create a replacement requirement'); END`,
}

var legacySchemaStatements = []string{
		`CREATE TABLE IF NOT EXISTS epics (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			workflow_mode TEXT DEFAULT 'full' CHECK(workflow_mode IN ('quick','standard','designed','full')),
			design_status TEXT DEFAULT '' CHECK(design_status IN ('','pending','approved','rejected')),
			owner_status TEXT DEFAULT '' CHECK(owner_status IN ('','pending','accepted','rejected')),
			status TEXT DEFAULT 'open' CHECK(status IN ('open','in_progress','done','cancelled')),
			created_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS epic_events (id TEXT PRIMARY KEY, epic_id TEXT NOT NULL REFERENCES epics(id) ON DELETE CASCADE, event_type TEXT NOT NULL, actor_role TEXT DEFAULT '', actor_model TEXT DEFAULT '', summary TEXT DEFAULT '', payload_json TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')))`,
		tasksTableSQL,
		`CREATE TABLE IF NOT EXISTS task_events (id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE, event_type TEXT NOT NULL, actor_role TEXT DEFAULT '', actor_model TEXT DEFAULT '', summary TEXT DEFAULT '', payload_json TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')))`,
		ownedWorkflowTableSQL["scan_reports"],
		ownedWorkflowTableSQL["rri_sessions"],
		ownedWorkflowTableSQL["designs"],
		`CREATE TABLE IF NOT EXISTS completion_reports (id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE, instruction_pack_id TEXT DEFAULT '', instruction_pack_version INTEGER DEFAULT 0, instruction_pack_hash TEXT DEFAULT '', effective_contract_snapshot_id TEXT DEFAULT '', effective_contract_snapshot_hash TEXT DEFAULT '', pipeline_run_id TEXT DEFAULT '', status TEXT NOT NULL CHECK(status IN ('done','partial','blocked','failed')), summary TEXT DEFAULT '', report_markdown TEXT DEFAULT '', files_changed_json TEXT DEFAULT '', tests_run_json TEXT DEFAULT '', acceptance_results_json TEXT DEFAULT '', issues_json TEXT DEFAULT '', deviations_json TEXT DEFAULT '', suggestions_json TEXT DEFAULT '', created_by_model TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')))`,
		`CREATE TABLE IF NOT EXISTS task_materializations (task_id TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE, epic_id TEXT NOT NULL REFERENCES epics(id) ON DELETE CASCADE, plan_node_key TEXT NOT NULL, design_id TEXT NOT NULL REFERENCES designs(id), design_version INTEGER NOT NULL, execution_policy TEXT DEFAULT 'strict_sequential' CHECK(execution_policy IN ('strict_sequential','partially_parallel','parallel_allowed','deferred_optional')), ordinal INTEGER NOT NULL, created_at TEXT DEFAULT (datetime('now')), UNIQUE(epic_id,plan_node_key,design_id))`,
		`CREATE TABLE IF NOT EXISTS task_instruction_packs (id TEXT PRIMARY KEY, display_key TEXT NOT NULL UNIQUE, task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE, version INTEGER NOT NULL, status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','active','stale','superseded')), source_type TEXT NOT NULL CHECK(source_type IN ('epic_task_plan','standalone_task','standalone_design')), source_task_revision INTEGER NOT NULL, source_design_id TEXT DEFAULT '', source_design_version INTEGER DEFAULT 0, revision_kind TEXT NOT NULL DEFAULT 'initial' CHECK(revision_kind IN ('initial','scope','verification','contract','execution')), goal TEXT NOT NULL, module TEXT DEFAULT '', estimated_effort_minutes INTEGER DEFAULT 0, files_json TEXT NOT NULL, patterns_json TEXT NOT NULL, business_rules_json TEXT NOT NULL, validation_rules_json TEXT NOT NULL, error_handling_json TEXT NOT NULL, state_transitions_json TEXT NOT NULL, contract_obligations_json TEXT NOT NULL, constraints_json TEXT NOT NULL, verification_json TEXT NOT NULL, requirement_snapshots_json TEXT NOT NULL, content_schema_version INTEGER NOT NULL DEFAULT 1, skill_families_json TEXT NOT NULL DEFAULT '[]', effective_contract_snapshot_id TEXT DEFAULT '', effective_contract_snapshot_hash TEXT DEFAULT '', content_hash TEXT NOT NULL, activated_at TEXT DEFAULT '', stale_at TEXT DEFAULT '', superseded_at TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')), UNIQUE(task_id,version))`,
		`CREATE TABLE IF NOT EXISTS instruction_pack_requirement_links (instruction_pack_id TEXT NOT NULL REFERENCES task_instruction_packs(id) ON DELETE CASCADE, requirement_id TEXT NOT NULL REFERENCES requirements(id), created_at TEXT DEFAULT (datetime('now')), PRIMARY KEY(instruction_pack_id,requirement_id))`,
		ownedWorkflowTableSQL["verification_reports"],
		`CREATE TABLE IF NOT EXISTS verification_items (id TEXT PRIMARY KEY, verification_report_id TEXT NOT NULL REFERENCES verification_reports(id) ON DELETE CASCADE, requirement_id TEXT REFERENCES requirements(id) ON DELETE SET NULL, status TEXT NOT NULL CHECK(status IN ('pass','fail','partial','deferred','not_applicable')), evidence TEXT DEFAULT '', notes TEXT DEFAULT '', "commit" TEXT DEFAULT '')`,
		ownedWorkflowTableSQL["escalations"],
		`CREATE TABLE IF NOT EXISTS contract_operations (id TEXT PRIMARY KEY, task_id TEXT REFERENCES tasks(id) ON DELETE CASCADE, epic_id TEXT REFERENCES epics(id) ON DELETE CASCADE, operation_type TEXT NOT NULL CHECK(operation_type IN ('replace','withdraw','defer')), status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','approved','rejected')), inherit_to_descendants INTEGER NOT NULL DEFAULT 0 CHECK(inherit_to_descendants IN (0,1)), replacement_requirement_id TEXT REFERENCES requirements(id), resume_condition TEXT DEFAULT '' CHECK(resume_condition IN ('','subject_completed','owner_reactivation')), completed_task_impact TEXT NOT NULL DEFAULT 'none' CHECK(completed_task_impact IN ('none','review')), owner_decision_id TEXT REFERENCES owner_decisions(id), created_at TEXT DEFAULT (datetime('now')), approved_at TEXT DEFAULT '', reactivated_at TEXT DEFAULT '', CHECK((task_id IS NOT NULL) != (epic_id IS NOT NULL)), CHECK((operation_type='replace')=(replacement_requirement_id IS NOT NULL)))`,
		`CREATE TABLE IF NOT EXISTS contract_operation_targets (operation_id TEXT NOT NULL REFERENCES contract_operations(id) ON DELETE CASCADE, requirement_id TEXT NOT NULL REFERENCES requirements(id), PRIMARY KEY(operation_id,requirement_id))`,
		`CREATE TABLE IF NOT EXISTS effective_contract_snapshots (id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE, content_hash TEXT NOT NULL, created_at TEXT DEFAULT (datetime('now')))`,
		`CREATE TABLE IF NOT EXISTS effective_contract_entries (snapshot_id TEXT NOT NULL REFERENCES effective_contract_snapshots(id) ON DELETE CASCADE, requirement_id TEXT NOT NULL REFERENCES requirements(id), contract_key TEXT DEFAULT '', requirement_hash TEXT NOT NULL, status TEXT NOT NULL CHECK(status IN ('effective','excluded')), operation_id TEXT REFERENCES contract_operations(id), provenance TEXT NOT NULL, PRIMARY KEY(snapshot_id,requirement_id))`,
		`CREATE TABLE IF NOT EXISTS contract_task_impacts (operation_id TEXT NOT NULL REFERENCES contract_operations(id), task_id TEXT NOT NULL REFERENCES tasks(id), status TEXT NOT NULL DEFAULT 'contract_outdated' CHECK(status='contract_outdated'), created_at TEXT DEFAULT (datetime('now')), PRIMARY KEY(operation_id,task_id))`,
		`CREATE TABLE IF NOT EXISTS task_dependencies (id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE, depends_on_task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE, dependency_type TEXT DEFAULT 'blocks' CHECK(dependency_type IN ('blocks','phase','related')), notes TEXT DEFAULT '', created_at TEXT DEFAULT (datetime('now')), UNIQUE(task_id, depends_on_task_id))`,
		`CREATE TABLE IF NOT EXISTS task_phase_metadata (task_id TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE, parent_task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL, phase_number INTEGER NOT NULL, phase_title TEXT DEFAULT '', execution_policy TEXT DEFAULT 'strict_sequential' CHECK(execution_policy IN ('strict_sequential','partially_parallel','parallel_allowed','deferred_optional')), is_deferrable INTEGER DEFAULT 0, can_start_before_previous INTEGER DEFAULT 0, created_at TEXT DEFAULT (datetime('now')))`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_epic ON tasks(epic_id)`,
		`CREATE INDEX IF NOT EXISTS idx_epic_events_epic ON epic_events(epic_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_designs_task_version ON designs(task_id,version) WHERE task_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_designs_epic_version ON designs(epic_id,version) WHERE epic_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_instruction_packs_active_task ON task_instruction_packs(task_id) WHERE status='active'`,
		`CREATE INDEX IF NOT EXISTS idx_instruction_packs_task ON task_instruction_packs(task_id,version DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_instruction_pack_requirements ON instruction_pack_requirement_links(requirement_id)`,
		`CREATE INDEX IF NOT EXISTS idx_task_events_task ON task_events(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_scan_reports_task ON scan_reports(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_rri_sessions_task ON rri_sessions(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_designs_task ON designs(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_completion_reports_task ON completion_reports(task_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_completion_reports_pipeline_run ON completion_reports(pipeline_run_id) WHERE pipeline_run_id!=''`,
		`CREATE INDEX IF NOT EXISTS idx_verification_reports_task ON verification_reports(task_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_verification_reports_pipeline_run ON verification_reports(pipeline_run_id) WHERE pipeline_run_id!=''`,
		`CREATE INDEX IF NOT EXISTS idx_verification_items_report ON verification_items(verification_report_id)`,
		`CREATE INDEX IF NOT EXISTS idx_escalations_task ON escalations(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_owner_decisions_task ON owner_decisions(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_contract_operations_task ON contract_operations(task_id,status)`,
		`CREATE INDEX IF NOT EXISTS idx_contract_operations_epic ON contract_operations(epic_id,status)`,
		`CREATE INDEX IF NOT EXISTS idx_contract_targets_requirement ON contract_operation_targets(requirement_id)`,
		`CREATE INDEX IF NOT EXISTS idx_contract_snapshots_task ON effective_contract_snapshots(task_id,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_task_dependencies_task ON task_dependencies(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_task_dependencies_depends_on ON task_dependencies(depends_on_task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_task_phase_metadata_parent ON task_phase_metadata(parent_task_id)`,
		`DROP TRIGGER IF EXISTS trg_instruction_pack_content_immutable`,
		`CREATE TRIGGER IF NOT EXISTS trg_instruction_pack_content_immutable BEFORE UPDATE OF task_id,version,source_type,source_task_revision,source_design_id,source_design_version,goal,module,estimated_effort_minutes,files_json,patterns_json,business_rules_json,validation_rules_json,error_handling_json,state_transitions_json,contract_obligations_json,constraints_json,verification_json,requirement_snapshots_json,content_schema_version,skill_families_json,effective_contract_snapshot_id,effective_contract_snapshot_hash,content_hash ON task_instruction_packs WHEN OLD.status!='draft' BEGIN SELECT RAISE(ABORT,'activated instruction pack content is immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_instruction_pack_delete_immutable BEFORE DELETE ON task_instruction_packs WHEN OLD.status!='draft' BEGIN SELECT RAISE(ABORT,'activated instruction pack cannot be deleted'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_instruction_pack_links_insert_immutable BEFORE INSERT ON instruction_pack_requirement_links WHEN (SELECT status FROM task_instruction_packs WHERE id=NEW.instruction_pack_id)!='draft' BEGIN SELECT RAISE(ABORT,'activated instruction pack links are immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_instruction_pack_links_update_immutable BEFORE UPDATE ON instruction_pack_requirement_links WHEN (SELECT status FROM task_instruction_packs WHERE id=OLD.instruction_pack_id)!='draft' BEGIN SELECT RAISE(ABORT,'activated instruction pack links are immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_instruction_pack_links_delete_immutable BEFORE DELETE ON instruction_pack_requirement_links WHEN (SELECT status FROM task_instruction_packs WHERE id=OLD.instruction_pack_id)!='draft' BEGIN SELECT RAISE(ABORT,'activated instruction pack links are immutable'); END`,
		`DROP TRIGGER IF EXISTS trg_requirement_content_stales_packs`,
		`CREATE TRIGGER trg_requirement_content_stales_packs AFTER UPDATE OF requirement_key,title,description,acceptance_criteria ON requirements BEGIN UPDATE task_instruction_packs SET status='stale',stale_at=datetime('now') WHERE status='active' AND id IN (SELECT instruction_pack_id FROM instruction_pack_requirement_links WHERE requirement_id=NEW.id); UPDATE tasks SET review_status='pending',reviewed_instruction_pack_id='',owner_status='pending' WHERE id IN (SELECT task_id FROM task_instruction_packs WHERE status='stale' AND id IN (SELECT instruction_pack_id FROM instruction_pack_requirement_links WHERE requirement_id=NEW.id)); UPDATE verification_reports SET superseded_at=datetime('now') WHERE task_id IN (SELECT task_id FROM task_instruction_packs WHERE status='stale' AND id IN (SELECT instruction_pack_id FROM instruction_pack_requirement_links WHERE requirement_id=NEW.id)) AND superseded_at=''; UPDATE epics SET owner_status='pending' WHERE id IN (SELECT epic_id FROM tasks WHERE id IN (SELECT task_id FROM task_instruction_packs WHERE status='stale' AND id IN (SELECT instruction_pack_id FROM instruction_pack_requirement_links WHERE requirement_id=NEW.id))); END`,
}


// schemaDB is the handle a migration step runs against. Every step now runs on
// one transaction, so both *sql.DB (ad-hoc use outside the runner) and *sql.Tx
// satisfy it.
type schemaDB interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// schemaMigration is one ordered schema step. Every step is one-shot and
// transactional: it is skipped once its version is recorded, so an
// already-migrated database performs no DDL or data mutation on later opens,
// and the step's operations plus its version record commit or roll back
// together — a crash leaves the step fully applied or not at all.
type schemaMigration struct {
	version    int
	name       string
	legacyOnly bool
	apply      func(db schemaDB) error
}

func schemaMigrationSteps() []schemaMigration {
	return []schemaMigration{
		{version: 1, name: "pre_reconcile_schema", apply: reconcileLegacySchema},
		{version: 2, name: "artifact_stage_widening", apply: migrateArtifactStageSchema},
		{version: 3, name: "pipeline_columns_reconcile", apply: applyPipelineColumnMigrations},
		{version: 4, name: "canonical_baseline", apply: func(db schemaDB) error {
			return applySchemaStatements(db, canonicalSchemaStatements)
		}},
		{version: 5, name: "legacy_schema_bootstrap", legacyOnly: true, apply: func(db schemaDB) error {
			return applySchemaStatements(db, legacySchemaStatements)
		}},
		{version: 6, name: "canonical_backfills", apply: applyCanonicalBackfills},
		{version: 7, name: "legacy_pack_backfills", legacyOnly: true, apply: func(db schemaDB) error {
			if err := migrateLegacyWorkItemInstructionPacks(db); err != nil {
				return err
			}
			return applyLegacyPackBackfills(db)
		}},
	}
}

func reconcileLegacySchema(db schemaDB) error {
	if err := removeLegacyTIPSchema(db); err != nil {
		return fmt.Errorf("remove legacy TIP schema: %w", err)
	}
	if err := migrateEpicWorkflowSchema(db); err != nil {
		return fmt.Errorf("migrate legacy workflow schema: %w", err)
	}
	if tableExists(db, "work_item_materializations") {
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

// applySchemaMigrations applies the ordered schema steps once per database.
// Legacy steps are skipped (and never recorded) on databases that never carried
// the retired Epic/Task tables.
func applySchemaMigrations(db *sql.DB) error {
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
	legacySchema := tableExists(db, "tasks") || tableExists(db, "epics")
	for _, migration := range schemaMigrationSteps() {
		if migration.legacyOnly && !legacySchema {
			continue
		}
		if applied[migration.version] {
			continue
		}
		if err := applySchemaMigration(context.Background(), db, migration); err != nil {
			return fmt.Errorf("schema migration %03d_%s: %w", migration.version, migration.name, err)
		}
	}
	return nil
}

// applySchemaMigration runs one step and records its version inside a single
// transaction on one pinned connection. foreign_keys and legacy_alter_table are
// connection-scoped in SQLite and cannot change inside a transaction, and
// database/sql gives no affinity between db.Exec and db.Begin — a pragma sent
// through the pool is not guaranteed to land on the connection the transaction
// ends up on. The pragma setup, the step, the version record, and the pragma
// restore therefore all run on one explicitly pinned *sql.Conn.
func applySchemaMigration(ctx context.Context, db *sql.DB, migration schemaMigration) error {
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
	if err := migration.apply(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO schema_migrations(version, name) VALUES(?, ?)`, migration.version, migration.name); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	finished = true
	return restore()
}

// applySchemaStatements executes one classified statement batch in order.
func applySchemaStatements(db schemaDB, statements []string) error {
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("initialize schema statement %q: %w", stmt, err)
		}
	}
	return nil
}

// applyPipelineColumnMigrations adds columns that predate a table's current
// definition and rebuilds tables whose shape or foreign keys drifted.
func applyPipelineColumnMigrations(db schemaDB) error {
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
		if tableExists(db, migration.table) && !hasColumn(db, migration.table, migration.column) {
			if _, err := db.Exec(`ALTER TABLE ` + migration.table + ` ADD COLUMN ` + migration.column + ` ` + migration.definition); err != nil && !hasColumn(db, migration.table, migration.column) {
				return err
			}
		}
	}
	if hasColumn(db, "pipeline_runs", "integrated_patch") {
		if _, err := db.Exec(`ALTER TABLE pipeline_runs DROP COLUMN integrated_patch`); err != nil && hasColumn(db, "pipeline_runs", "integrated_patch") {
			return err
		}
	}
	var pipelineSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='pipeline_runs'`).Scan(&pipelineSQL); err == nil && (!strings.Contains(pipelineSQL, "'rri'") || strings.Contains(pipelineSQL, "REFERENCES tasks(id)")) {
		if err := rebuildSchemaTable(db, "pipeline_runs", pipelineRunsTableSQL); err != nil {
			return err
		}
	}
	var completionRunTarget string
	if err := db.QueryRow(`SELECT "table" FROM pragma_foreign_key_list('work_item_completion_reports') WHERE "from"='pipeline_run_id'`).Scan(&completionRunTarget); err != nil && err != sql.ErrNoRows {
		return err
	}
	if completionRunTarget == "pipeline_runs__workflow_migration" {
		if err := rebuildSchemaTable(db, "work_item_completion_reports", workItemCompletionReportsTableSQL); err != nil {
			return err
		}
	}
	return nil
}


// applyCanonicalBackfills reconciles canonical evidence that may have completed
// after a Work Item row was last written. The UPDATEs are convergent (guarded
// by WHERE clauses), so they re-run on every open exactly as before.
func applyCanonicalBackfills(db schemaDB) error {
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
	if _, err := db.Exec(`INSERT OR IGNORE INTO work_item_relations(id,work_item_id,relation_type,related_work_item_id,created_at)
		SELECT 'wir-migrated-'||id,work_item_id,'blocks',depends_on_work_item_id,created_at FROM work_item_dependencies
		UNION ALL
		SELECT 'wir-migrated-'||id,work_item_id,'gates',gate_work_item_id,created_at FROM work_item_gates`); err != nil {
		return err
	}
	return nil
}

// applyLegacyPackBackfills recomputes legacy pack revision kinds and supersedes
// legacy verification reports after migration. Both are convergent.
func applyLegacyPackBackfills(db schemaDB) error {
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
