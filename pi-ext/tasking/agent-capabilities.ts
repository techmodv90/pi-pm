const childTaskManagerActions: Record<string, ReadonlySet<string>> = {
  "task-reviewer": new Set(["show_work_item", "work_item_workflow_status", "trigger_work_item_review", "search"]),
  "task-scout": new Set(["list_work_items", "show_work_item", "work_item_workflow_status", "search"]),
  "task-planner": new Set(["list_work_items", "show_work_item", "work_item_workflow_status", "load_planning_artifact", "save_blueprint_draft", "save_work_item_artifact", "validate_work_item_graph", "search"]),
  "rri-persona": new Set(["show_work_item", "work_item_workflow_status", "search"]),
  "rri-t-persona": new Set(["show_work_item", "work_item_workflow_status", "search"]),
  // Worker, autofix, and debugger subagents implement within the isolated worktree
  // and never transition workflow lifecycle; persist nothing through task_manager,
  // and may only observe. Least privilege: read-only observation, no owner,
  // contractor, or scheduler lifecycle authority.
  "task-worker": new Set(["show_work_item", "work_item_workflow_status", "search"]),
  "task-debugger": new Set(["show_work_item", "work_item_workflow_status", "search"]),
};

export function assertTaskManagerActionAllowed(agentName: string | undefined, action: string, stage?: string): void {
  if (!agentName) return;
  const allowed = childTaskManagerActions[agentName];
  if (!allowed) throw new Error(`${agentName} is not provisioned for task_manager action ${action}`);
  if (!allowed.has(action)) throw new Error(`${agentName} cannot call task_manager action ${action}`);
  if (action === "save_work_item_artifact") {
    const stages: Record<string, ReadonlySet<string>> = {
      "task-scout": new Set(["scan"]),
      "task-planner": new Set(["task_graph"]),
    };
    if (!stage || !stages[agentName]?.has(stage)) throw new Error(`${agentName} cannot save ${stage || "unspecified"} artifacts`);
  }
}