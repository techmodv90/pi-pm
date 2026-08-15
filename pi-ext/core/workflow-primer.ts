import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

export const WORKFLOW_PRIMER = `
## Work Item Workflow

Use the task_manager tool as the canonical interface for tracked work.
- Discover unblocked parallel work with action "ready_work_items".
- Inspect with \`show_work_item\` and \`work_item_workflow_status\`. Follow the persisted \`next_stage\`; do not skip gates.
- Planning order is scan -> rri -> vision -> blueprint -> contracts -> task_graph -> materialize -> authorize -> implement. Save each planning result with \`save_work_item_artifact\`. Call \`approve_work_item_artifact\` only after explicit owner approval.

Requirements and TIPs:
- Requirements returned by \`show_work_item\` are authoritative. Task-graph \`requirement_keys\` must reference and cover them; never invent requirement IDs.
- Every acceptance criterion must contain separate \`Given\`, \`When\`, and \`Then\` steps before task-graph approval or TIP creation.
- There is no \`task_manager\` action for direct requirement mutation. If a requirement is missing or malformed, stop before task_graph, report the blocker, and do not edit the database or invoke \`pic\` directly.
- \`materialize_work_item\` generates inactive TIPs from the approved task graph. Do not create, edit, activate, or render TIPs through direct CLI commands.

Execution order:
- After explicit owner approval of the task graph, call \`materialize_work_item\`.
- Ask for explicit owner authorization, then call \`authorize_work_item_implementation\` with \`actor_role=owner\`; this activates the generated TIPs.
- Launch only dependency-ready executable Work Items with \`work_on_work_item\`; the persisted scheduler owns worker and review execution.
- Follow \`work_item_workflow_status\` for contractor verification and aggregate-only owner acceptance/merge actions. Passed executable children close automatically; never request owner acceptance for a child Task, Bug, or Chore.
- A Feature owns a delivery branch by default; an Epic coordinates by default unless explicitly marked branch-owning. Only one aggregate on a containment path may own a branch.
- After aggregate verification and explicit owner acceptance, the scheduler merges the bound branch to \`develop\`; merge failure leaves \`merge_pending\` and must not rerun completed children.
- Legacy pic task commands and task-runner dispatch were removed. Never invoke "pic task" or launch task workers directly.
`;

export function registerWorkflowPrimer(pi: ExtensionAPI) {
  pi.on("before_agent_start", (event) => ({
    systemPrompt: `${event.systemPrompt}\n${WORKFLOW_PRIMER}`,
  }));
}