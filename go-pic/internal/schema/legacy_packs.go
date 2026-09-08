package schema

func MigrateLegacyWorkItemInstructionPacks(db DB) error {
	if _, err := db.Exec(`INSERT OR IGNORE INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash,created_at)
		SELECT 'wia-migrated-tip-'||p.task_id,p.task_id,'task_graph',COALESCE((SELECT MAX(a.revision)+1 FROM work_item_artifacts a WHERE a.work_item_id=p.task_id AND a.stage='task_graph'),1),'{}',p.content_hash,p.created_at
		FROM task_instruction_packs p JOIN work_items wi ON wi.id=p.task_id
		WHERE p.version=(SELECT MAX(latest.version) FROM task_instruction_packs latest WHERE latest.task_id=p.task_id)
		AND NOT EXISTS(SELECT 1 FROM work_item_instruction_packs canonical WHERE canonical.work_item_id=p.task_id)`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type,created_at)
		SELECT 'wic-migrated-tip-'||a.work_item_id,a.work_item_id,'task_graph',a.id,a.revision,a.content_hash,'migrated_legacy_tip',a.created_at
		FROM work_item_artifacts a WHERE a.id='wia-migrated-tip-'||a.work_item_id`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO work_item_instruction_packs(id,work_item_id,checkpoint_id,version,status,content_json,content_hash,activated_at,stale_at,created_at)
		SELECT p.id,p.task_id,'wic-migrated-tip-'||p.task_id,p.version,
			CASE p.status WHEN 'active' THEN 'active' WHEN 'draft' THEN 'inactive' ELSE 'stale' END,
			json_object('goal',p.goal,'module',p.module,'estimated_effort_minutes',p.estimated_effort_minutes,'files',json(p.files_json),'patterns',json(COALESCE(p.patterns_json,'[]')),'business_rules',json(p.business_rules_json),'validation_rules',json(p.validation_rules_json),'error_handling',json(p.error_handling_json),'state_transitions',json(p.state_transitions_json),'contract_obligations',json(p.contract_obligations_json),'constraints',json(p.constraints_json),'verification',json(p.verification_json),'requirement_snapshots',json(p.requirement_snapshots_json),'content_schema_version',p.content_schema_version,'skill_families',json(p.skill_families_json)),
			p.content_hash,p.activated_at,COALESCE(NULLIF(p.stale_at,''),p.superseded_at),p.created_at
		FROM task_instruction_packs p JOIN work_items wi ON wi.id=p.task_id
		WHERE EXISTS(SELECT 1 FROM workflow_checkpoints checkpoint WHERE checkpoint.id='wic-migrated-tip-'||p.task_id)
		AND p.version=(SELECT MAX(latest.version) FROM task_instruction_packs latest WHERE latest.task_id=p.task_id)
		AND NOT EXISTS(SELECT 1 FROM work_item_instruction_packs canonical WHERE canonical.work_item_id=p.task_id)`); err != nil {
		return err
	}
	return nil
}
