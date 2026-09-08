package workitem

// Transition oracle constraint: workflow-status reports these hints as
// structured next_actions, and gate rejections cite the same oracle so every
// blocked transition names its valid next steps from one authority. Actions
// carry stable IDs and argument templates so extension consumers can dispatch
// them without parsing display text.

// NextAction is one structured, dispatchable transition step for a stage.
type NextAction struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`            // "tool" or "cli"
	Action string `json:"action"`          // task_manager action name or CLI command
	Args   string `json:"args,omitempty"`  // argument template with placeholders
	Label  string `json:"label"`           // human-readable description
	Actor  string `json:"actor,omitempty"` // "owner" or "contractor" when role-bound
}

func toolAction(id, action, args, label, actor string) NextAction {
	return NextAction{ID: id, Kind: "tool", Action: action, Args: args, Label: label, Actor: actor}
}

// NextActionHints returns the exact actions valid at a workflow stage. The done
// stage yields no hints on purpose: there is nothing left to run.
func NextActionHints(stage string) []NextAction {
	switch stage {
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

// WithNextActions attaches the stage's structured oracle actions to a
// workflow-status map. The Task Graph approval checkpoint additionally carries
// the five granularity questions the owner reviews before approving.
func WithNextActions(status map[string]any) map[string]any {
	if next, ok := status["next_stage"].(string); ok {
		if hints := NextActionHints(next); len(hints) > 0 {
			status["next_actions"] = hints
		}
	}
	return status
}
