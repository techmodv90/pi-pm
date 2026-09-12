import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

export const WORKFLOW_PRIMER = `
## Work Item Workflow

Use the task_manager tool as the canonical interface for tracked **execution** work.
- Planning happens through the /apm commands (lean flow, see docs/lean-flow.md):
  /apm start routes complexity, /apm spec/tech-plan/breakdown author .apm artifacts,
  and pic workflow import-apm creates Work Items from them. Do not start new work
  through task_manager planning stages — scan/rri/vision/blueprint/contracts/task_graph
  are retired (docs/plans/deletion-ledger.json).
- Inspect with \`show_work_item\` and \`work_item_workflow_status\`. Follow the persisted
  state; do not skip gates.

Requirements and TIPs:
- Requirements returned by \`show_work_item\` are authoritative; never invent requirement IDs.
- TIPs are immutable and frozen at first worker claim. Do not create, edit, activate, or render TIPs through direct CLI commands.

Execution order:
- Ask for explicit owner authorization, then call \`authorize_work_item_implementation\` with \`actor_role=owner\`; the scheduler generates and freezes each ready executable's TIP transactionally immediately before its first worker claim.
- Launch only dependency-ready executable Work Items with \`work_on_work_item\`; the persisted scheduler owns worker and review execution.
- Follow \`work_item_workflow_status\` for contractor verification and aggregate-only owner acceptance/merge actions. Passed executable children close automatically; never request owner acceptance for a child Task, Bug, or Chore.
- Workflow debugging rule: never work around a blocker by filtering runtime state, relabeling a failure, or bypassing a gate. Trace the persisted state transition first; model valid handoffs explicitly and add regression evidence before retrying.
- A Feature owns a delivery branch by default; an Epic coordinates by default unless explicitly marked branch-owning. Only one aggregate on a containment path may own a branch.
- After aggregate verification and explicit owner acceptance, the scheduler merges the bound branch to \`develop\`; merge failure leaves \`merge_pending\` and must not rerun completed children.
- Legacy pic task commands and task-runner dispatch were removed. Never invoke "pic task" or launch task workers directly.
`;

export function registerWorkflowPrimer(pi: ExtensionAPI) {
  pi.on("before_agent_start", (event) => ({
    systemPrompt: `${event.systemPrompt}\n${WORKFLOW_PRIMER}`,
  }));
}