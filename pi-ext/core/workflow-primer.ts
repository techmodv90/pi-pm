import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

export const WORKFLOW_PRIMER = `
## Work Item Workflow

Use the task_manager tool as the canonical interface for tracked work.
- Discover unblocked parallel work with action "ready_work_items".
- Inspect with \`show_work_item\` and \`work_item_workflow_status\`. Follow the persisted \`next_stage\`; do not skip gates.
- Planning order is scan -> rri -> vision -> blueprint -> contracts -> task_graph -> materialize -> authorize -> implement. Task Planner owns Blueprint and Task Graph only; the Contractor owns Vision and Contract drafting. Save each planning result with \`save_work_item_artifact\`. Call \`approve_work_item_artifact\` only after explicit owner approval.

Requirements and TIPs:
- Requirements returned by \`show_work_item\` are authoritative. Task-graph \`requirement_keys\` must reference and cover them; never invent requirement IDs.
- Every acceptance criterion must contain separate \`Given\`, \`When\`, and \`Then\` steps before task-graph approval or TIP creation.
- There is no \`task_manager\` action for direct requirement mutation. If a requirement is missing or malformed, stop before task_graph, report the blocker, and do not edit the database or invoke \`pic\` directly.
- During RRI, checkpoint disposable interview state with \`checkpoint_rri_interview\`, then call \`save_rri_interview\` once to atomically persist confirmed requirements, owner decisions, and the final RRI artifact.
- \`materialize_work_item\` creates or reuses only the approved Work Item DAG, metadata, and dependency relations. It does not generate TIPs. Do not create, edit, activate, or render TIPs through direct CLI commands.

Execution order:
- After explicit owner approval of the task graph, call \`materialize_work_item\`.
- Ask for explicit owner authorization, then call \`authorize_work_item_implementation\` with \`actor_role=owner\`; the scheduler generates and freezes each ready executable's TIP transactionally immediately before its first worker claim.
- Launch only dependency-ready executable Work Items with \`work_on_work_item\`; the persisted scheduler owns worker and review execution.
- Follow \`work_item_workflow_status\` for contractor verification and aggregate-only owner acceptance/merge actions. Passed executable children close automatically; never request owner acceptance for a child Task, Bug, or Chore.
- Workflow debugging rule: never work around a blocker by filtering runtime state, relabeling a failure, or bypassing a gate. Trace the persisted state transition first; model valid handoffs explicitly and add regression evidence before retrying.
- Scan handoff rule: focused scans use one read-only Scout and full scans fan out bounded section assignments. Scouts return immutable evidence only; the contractor validates it, resolves conflicts, authors one canonical Scan Report, and saves that artifact exactly once. Rejected evidence pauses for an explicit owner rescan decision.
- A Feature owns a delivery branch by default; an Epic coordinates by default unless explicitly marked branch-owning. Only one aggregate on a containment path may own a branch.
- After aggregate verification and explicit owner acceptance, the scheduler merges the bound branch to \`develop\`; merge failure leaves \`merge_pending\` and must not rerun completed children.
- Legacy pic task commands and task-runner dispatch were removed. Never invoke "pic task" or launch task workers directly.
`;

export function registerWorkflowPrimer(pi: ExtensionAPI) {
  pi.on("before_agent_start", (event) => ({
    systemPrompt: `${event.systemPrompt}\n${WORKFLOW_PRIMER}`,
  }));
}