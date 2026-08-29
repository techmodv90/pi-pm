package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Transition oracle constraint: workflow-status reports these hints as
// next_actions, and gate rejections cite the same oracle so every blocked
// transition names its valid next steps from one authority.

// nextActionHints returns the exact tool/CLI actions valid at a workflow stage.
// The done stage yields no hints on purpose: there is nothing left to run.
func nextActionHints(stage string) []string {
	switch stage {
	case "scan":
		return []string{
			"save_work_item_artifact (stage=scan, structured XML)",
			"approve_work_item_artifact <id> scan current accepted (actor_role=owner)",
		}
	case "rri":
		return []string{
			"save_rri_interview (requirements, decisions, report)",
			"approve_work_item_artifact <id> rri current approved (actor_role=owner)",
		}
	case "vision":
		return []string{
			"save_work_item_artifact (stage=vision JSON)",
			"approve_work_item_artifact <id> vision current approved (actor_role=owner)",
		}
	case "blueprint":
		return []string{
			"save_blueprint_draft (blueprint JSON)",
			"review_blueprint_checkpoint (all five checks, actor_role=contractor)",
			"approve_blueprint_draft (actor_role=owner)",
		}
	case "contracts":
		return []string{
			"preview_artifact (stage=contracts) and present rendered Markdown to the owner",
			"save_work_item_artifact (stage=contracts JSON)",
			"approve_work_item_artifact <id> contracts current approved (actor_role=owner)",
		}
	case "task_graph":
		return []string{
			"save_work_item_artifact (stage=task_graph JSON)",
			"validate_work_item_graph <id>",
			"approve_work_item_artifact <id> task_graph current approved (actor_role=owner)",
		}
	case "materialize":
		return []string{
			"materialize_work_item (tool) / pic work-item materialize <id>",
		}
	case "authorize":
		return []string{
			"authorize_work_item_implementation (tool) / pic work-item authorize <id> owner (actor_role=owner)",
		}
	case "implement":
		return []string{
			"work_on_work_item for each dependency-ready executable leaf",
		}
	case "contractor_verification":
		return []string{
			"verify_work_item (actor_role=contractor)",
		}
	case "aggregate_verification":
		return []string{
			"verify_aggregate_work_item (actor_role=contractor)",
		}
	case "owner_acceptance":
		return []string{
			"accept_aggregate_work_item (actor_role=owner)",
		}
	case "merge_pending":
		return []string{
			"merge_aggregate_work_item",
		}
	default:
		return nil
	}
}

// withNextActions attaches the stage's oracle hints to a workflow-status map.
func withNextActions(status map[string]any) map[string]any {
	if next, ok := status["next_stage"].(string); ok {
		if hints := nextActionHints(next); len(hints) > 0 {
			status["next_actions"] = hints
		}
	}
	return status
}

// nextActionHint returns the first hint for a stage, formatted for rejection
// messages, or an empty string when the stage has no hint.
func nextActionHint(stage string) string {
	hints := nextActionHints(stage)
	if len(hints) == 0 {
		return ""
	}
	return fmt.Sprintf("Next: %s", hints[0])
}

// workItemCheckpointDecide records several owner stage decisions in one
// command. Each decision runs through the same per-stage approval core inside
// ONE transaction, processed in planning-profile order so predecessor checks
// resolve within the batch. No stage semantics are combined: every decision
// still binds its own stage, latest artifact revision, and content hash.
func workItemCheckpointDecide(db *sql.DB, args []string) error {
	if len(args) != 3 || args[1] != "--decisions" {
		return errors.New("usage: pic work-item checkpoint-decide <id> --decisions <stage>:<accepted|approved>[,...]")
	}
	workItemID := args[0]
	stages, err := planningStagesForWorkItem(db, workItemID)
	if err != nil {
		return err
	}
	selected := map[string]string{}
	for _, token := range strings.Split(args[2], ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		parts := strings.SplitN(token, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("invalid decision %q; expected <stage>:<accepted|approved>", token)
		}
		stage, decision := parts[0], parts[1]
		if !contains(workItemStages, stage) || stage == "rri_t_scenarios" {
			return fmt.Errorf("unsupported checkpoint stage %s", stage)
		}
		expected := "approved"
		if stage == "scan" {
			expected = "accepted"
		}
		if decision != expected {
			return fmt.Errorf("%s requires decision %s", stage, expected)
		}
		if _, duplicate := selected[stage]; duplicate {
			return fmt.Errorf("duplicate decision for stage %s", stage)
		}
		selected[stage] = decision
	}
	if len(selected) == 0 {
		return errors.New("no decisions supplied")
	}
	var ordered []string
	for _, stage := range stages {
		if _, ok := selected[stage]; ok {
			ordered = append(ordered, stage)
		}
	}
	for _, stage := range workItemStages {
		if _, ok := selected[stage]; ok && !contains(stages, stage) {
			return fmt.Errorf("stage %s is not part of this Work Item planning profile", stage)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	decisions := []map[string]any{}
	for _, stage := range ordered {
		artifactID, revision, contentHash, err := approveWorkItemArtifactTx(tx, workItemID, stage, "current", selected[stage])
		if err != nil {
			return err
		}
		decisions = append(decisions, map[string]any{"stage": stage, "decision": selected[stage], "artifact_id": artifactID, "revision": revision, "content_hash": contentHash})
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	writeJSON(os.Stdout, map[string]any{"work_item_id": workItemID, "decisions": decisions})
	return nil
}
