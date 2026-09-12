import { execPic } from "../core/cli-helpers.ts";
import type { ExtensionContext } from "@mariozechner/pi-coding-agent";
import type { PipelineScheduler } from "../pipeline/pipeline-scheduler.ts";
import type { TaskManagerParams } from "./task-manager-schema.ts";
import { aggregateGitEvidence, compileRriTSubmission } from "./task-manager-helpers.ts";
import type { ToolOutcome } from "./blueprint-approval.ts";

export function verifyAggregateWorkItem(ctx: { cwd: string }, params: TaskManagerParams): ToolOutcome | { args: string[] } {
  if (!params.id || !params.verification_status || params.actor_role !== "contractor") return { content: [{ type: "text", text: "Error: id, verification_status, and actor_role=contractor required" }], details: {}, isError: true };
  // RRI-T scenario ownership (OB-5): compile the grading submission from
  // the aggregate's own persisted scenarios only; parent-inherited rows
  // are never a valid scenario source for a feature aggregate, so the
  // aggregate is loaded directly instead of merged with parent artifacts.
  const aggregateData = execPic(["show", params.id], ctx.cwd);
  if (!aggregateData.work_item) return { content: [{ type: "text", text: `Error: ${aggregateData.error || "Work Item not found"}` }], details: {}, isError: true };
  let rriTJson = "";
  try {
    // RRI-T grading submission (OB-6): compile the graded scenario
    // evidence from the persisted rri_t_scenarios artifact only; the
    // contractor grades in the main session, so the submission never
    // re-runs persona subagents and fails closed without a saved list.
    rriTJson = compileRriTSubmission(aggregateData, params.rri_t_evidence_json || "");
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return { content: [{ type: "text", text: `RRI-T verification blocked: ${message}` }], details: { error: message }, isError: true };
  }
  const args = ["work-item", "aggregate-verify", params.id, params.verification_status, params.summary || params.notes || "", "--actor-role", params.actor_role, "--rri-t-json", rriTJson];
  const git = aggregateGitEvidence(ctx.cwd);
  args.push("--branch-name", git.branch, "--head-commit", git.head, "--base-commit", git.baseCommit);
  return { args };
}

export function acceptAggregateWorkItem(ctx: { cwd: string }, params: TaskManagerParams): ToolOutcome | { args: string[] } {
  if (!params.id || !params.verification_report_id || !params.decision || params.actor_role !== "owner") return { content: [{ type: "text", text: "Error: id, verification_report_id, decision, and actor_role=owner required" }], details: {}, isError: true };
  const git = aggregateGitEvidence(ctx.cwd);
  return { args: ["work-item", "aggregate-accept", params.id, params.verification_report_id, params.decision, params.notes || "", "--actor-role", params.actor_role, "--head-commit", git.head, "--base-commit", git.baseCommit] };
}

export async function mergeAggregateWorkItem(pipelineScheduler: PipelineScheduler, ctx: ExtensionContext, params: TaskManagerParams): Promise<ToolOutcome> {
  if (!params.id) return { content: [{ type: "text", text: "Error: id required" }], details: {}, isError: true };
  try {
    const result = await pipelineScheduler.mergeAggregate(params.id, ctx);
    return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return { content: [{ type: "text", text: `Aggregate merge blocked: ${message}` }], details: { error: message }, isError: true };
  }
}
