package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Transition oracle constraint: workflow-status reports these hints as
// structured next_actions, and gate rejections cite the same oracle so every
// blocked transition names its valid next steps from one authority. Actions
// carry stable IDs and argument templates so extension consumers can dispatch
// them without parsing display text.

// NextAction is one structured, dispatchable transition step for a stage.
type NextAction struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`           // "tool" or "cli"
	Action string `json:"action"`         // task_manager action name or CLI command
	Args   string `json:"args,omitempty"`  // argument template with placeholders
	Label  string `json:"label"`           // human-readable description
	Actor  string `json:"actor,omitempty"` // "owner" or "contractor" when role-bound
}

func toolAction(id, action, args, label, actor string) NextAction {
	return NextAction{ID: id, Kind: "tool", Action: action, Args: args, Label: label, Actor: actor}
}

// nextActionHints returns the exact actions valid at a workflow stage. The done
// stage yields no hints on purpose: there is nothing left to run.
func nextActionHints(stage string) []NextAction {
	switch stage {
	case "scan":
		return []NextAction{
			toolAction("save_scan_artifact", "save_work_item_artifact", "<id> scan <xml>", "Save the canonical Scan Report as structured XML", "contractor"),
			toolAction("approve_scan_artifact", "approve_work_item_artifact", "<id> scan current accepted", "Accept the current Scan revision", "owner"),
		}
	case "rri":
		return []NextAction{
			toolAction("save_rri_interview", "save_rri_interview", "<id>", "Publish the RRI interview once", "contractor"),
			toolAction("approve_rri_artifact", "approve_work_item_artifact", "<id> rri current approved", "Approve the current RRI revision", "owner"),
		}
	case "vision":
		return []NextAction{
			toolAction("save_vision_artifact", "save_work_item_artifact", "<id> vision <json>", "Save the Vision artifact", "contractor"),
			toolAction("approve_vision_artifact", "approve_work_item_artifact", "<id> vision current approved", "Approve the current Vision revision", "owner"),
		}
	case "blueprint":
		return []NextAction{
			toolAction("save_blueprint_draft", "save_blueprint_draft", "<id> <json>", "Save the Blueprint draft", "contractor"),
			toolAction("review_blueprint_checkpoint", "review_blueprint_checkpoint", "<id> <draft-id>", "Run the five Blueprint checkpoint checks", "contractor"),
			toolAction("approve_blueprint_draft", "approve_blueprint_draft", "<id> <draft-id>", "Approve the reviewed Blueprint draft", "owner"),
		}
	case "contracts":
		return []NextAction{
			toolAction("preview_contract", "preview_artifact", "<id> contracts <json>", "Preview the Contract and present rendered Markdown", "contractor"),
			toolAction("save_contracts_artifact", "save_work_item_artifact", "<id> contracts <json>", "Save the Contract artifact after owner CONFIRM", "contractor"),
			toolAction("approve_contracts_artifact", "approve_work_item_artifact", "<id> contracts current approved", "Approve the current Contract revision", "owner"),
		}
	case "task_graph":
		return []NextAction{
			toolAction("save_task_graph_artifact", "save_work_item_artifact", "<id> task_graph <json>", "Save the task graph", "contractor"),
			toolAction("validate_task_graph", "validate_work_item_graph", "<id>", "Validate the task graph", "contractor"),
			toolAction("approve_task_graph_artifact", "approve_work_item_artifact", "<id> task_graph current approved", "Approve the current task-graph revision", "owner"),
		}
	case "materialize":
		return []NextAction{
			toolAction("materialize_graph", "materialize_work_item", "<id>", "Materialize the approved task graph", "contractor"),
		}
	case "authorize":
		return []NextAction{
			toolAction("authorize_implementation", "authorize_work_item_implementation", "<id>", "Authorize implementation of the approved graph", "owner"),
		}
	case "implement":
		return []NextAction{
			toolAction("work_ready_leaves", "work_on_work_item", "<leaf-id>", "Launch a dependency-ready executable leaf", "contractor"),
		}
	case "contractor_verification":
		return []NextAction{
			toolAction("verify_work_item", "verify_work_item", "<id> <completion-report-id>", "Verify the integrated completion evidence", "contractor"),
		}
	case "aggregate_verification":
		return []NextAction{
			toolAction("verify_aggregate", "verify_aggregate_work_item", "<id>", "Run aggregate verification and grade RRI-T scenarios", "contractor"),
		}
	case "owner_acceptance":
		return []NextAction{
			toolAction("accept_aggregate", "accept_aggregate_work_item", "<id>", "Record the final aggregate owner decision", "owner"),
		}
	case "merge_pending":
		return []NextAction{
			toolAction("merge_aggregate", "merge_aggregate_work_item", "<id>", "Retry the bound delivery merge", "contractor"),
		}
	default:
		return nil
	}
}

// taskGraphApprovalQuestions are the five structured granularity questions the
// owner answers at the existing Task Graph approval checkpoint, before
// materialization. They ride the approval gate — no new approval state is
// introduced — and are rendered wherever the Task Graph approval checkpoint is
// presented (workflow-status next actions and gate hints).
var taskGraphApprovalQuestions = []string{
	"Are any slices too coarse or too fine?",
	"Does each slice have independently meaningful verification?",
	"Does each blocker genuinely gate execution?",
	"Are any horizontal exceptions justified?",
	"Should any node merge or split?",
}

// withNextActions attaches the stage's structured oracle actions to a
// workflow-status map. The Task Graph approval checkpoint additionally carries
// the five granularity questions the owner reviews before approving.
func withNextActions(status map[string]any) map[string]any {
	if next, ok := status["next_stage"].(string); ok {
		if hints := nextActionHints(next); len(hints) > 0 {
			status["next_actions"] = hints
		}
		if next == "task_graph" {
			status["checkpoint_questions"] = taskGraphApprovalQuestions
		}
	}
	return status
}

// nextActionHint renders the first action for a stage as gate-rejection text.
func nextActionHint(stage string) string {
	hints := nextActionHints(stage)
	if len(hints) == 0 {
		return ""
	}
	first := hints[0]
	hint := fmt.Sprintf("%s %s", first.Action, first.Args)
	if first.Actor != "" {
		hint += fmt.Sprintf(" (actor_role=%s)", first.Actor)
	}
	return fmt.Sprintf("Next: %s", hint)
}

// workItemCheckpointDecide records several owner stage decisions in one
// command. Each decision runs through the same per-stage approval core inside
// ONE transaction, processed in planning-profile order so predecessor checks
// resolve within the batch. No stage semantics are combined: every decision
// still binds its own stage, latest artifact revision, and content hash.
func workItemCheckpointDecide(db *sql.DB, args []string) (err error) {
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
	// Glossary constraint (REQ-F1-6): a batched rri approval is still an owner
	// approval, so it applies the same CONTEXT.md glossary update as
	// artifact-approve; any later batch failure or commit failure restores the
	// pre-write content because the whole transaction rolls back.
	committed := false
	var restoreGlossary func() error
	defer func() {
		if restoreGlossary != nil && !committed {
			if restoreErr := restoreGlossary(); restoreErr != nil {
				// A compensation failure leaves CONTEXT.md out of sync with the
				// rolled-back lifecycle state, so it must reach the owner.
				if err == nil {
					err = fmt.Errorf("approval rolled back, but restoring the pre-write CONTEXT.md failed: %w", restoreErr)
				} else {
					err = fmt.Errorf("%w (restoring the pre-write CONTEXT.md also failed: %v)", err, restoreErr)
				}
			}
		}
	}()
	for _, stage := range ordered {
		artifactID, revision, contentHash, err := approveWorkItemArtifactTx(tx, workItemID, stage, "current", selected[stage])
		if err != nil {
			return err
		}
		if _, restore, applyErr := applyRriGlossaryApproval(tx, workItemID, stage, artifactID); applyErr != nil {
			return applyErr
		} else if restore != nil {
			restoreGlossary = restore
		}
		decisions = append(decisions, map[string]any{"stage": stage, "decision": selected[stage], "artifact_id": artifactID, "revision": revision, "content_hash": contentHash})
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true
	writeJSON(os.Stdout, map[string]any{"work_item_id": workItemID, "decisions": decisions})
	return nil
}
