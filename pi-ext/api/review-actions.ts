import { execPic } from "../core/cli-helpers.ts";
import { buildReviewContext } from "../tasking/settings.ts";
import { buildWorkItemContinuePrompt, buildWorkItemDebugPrompt } from "../tasking/work-item-prompts.ts";
import { withInheritedParentWorkflowArtifacts } from "../tasking/task-artifacts.ts";
import type { ExtensionContext } from "@mariozechner/pi-coding-agent";
import type { PipelineScheduler } from "../pipeline/pipeline-scheduler.ts";
import type { TaskManagerParams } from "./task-manager-schema.ts";
import type { ToolOutcome } from "./blueprint-approval.ts";

function errorOutcome(message: string, details: unknown = {}): ToolOutcome {
  return { content: [{ type: "text", text: `Error: ${message}` }], details, isError: true };
}

export async function workOnWorkItem(pipelineScheduler: PipelineScheduler, ctx: ExtensionContext, params: TaskManagerParams): Promise<ToolOutcome> {
  if (!params.id) {
    try {
      const result = await pipelineScheduler.startReadyBatch(ctx);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: { action: "work_on_work_item", pipeline: result } };
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return { content: [{ type: "text", text: `Work batch blocked: ${message}` }], details: { action: "work_on_work_item", error: message }, isError: true };
    }
  }
  const data = execPic(["show", params.id], ctx.cwd);
  if (!data.work_item) return { content: [{ type: "text", text: `Error: ${data.error || "Work Item not found"}` }], details: {}, isError: true };
  const status = execPic(["work-item", "workflow-status", params.id], ctx.cwd);
  if (!status.error && (status.next_stage === "rri" || status.next_stage === "vision" || status.next_stage === "contracts")) {
    const prompt = buildWorkItemContinuePrompt(status, data.work_item);
    return { content: [{ type: "text", text: prompt }], details: { action: "work_on_work_item", workItem: data.work_item, next_stage: status.next_stage, contractor: true } };
  }
  try {
    const result = await pipelineScheduler.start(params.id, ctx);
    return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: { action: "work_on_work_item", workItem: data.work_item, pipeline: result } };
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return { content: [{ type: "text", text: `Work pipeline blocked: ${message}` }], details: { action: "work_on_work_item", workItem: data.work_item, error: message }, isError: true };
  }
}

export function debugWorkItem(ctx: { cwd: string }, params: TaskManagerParams): ToolOutcome {
  if (!params.id) return errorOutcome("id required for debug_work_item");
  const data = execPic(["show", params.id], ctx.cwd);
  if (!data.work_item) return { content: [{ type: "text", text: `Error: ${data.error || "Work Item not found"}` }], details: {}, isError: true };
  const inheritedData = withInheritedParentWorkflowArtifacts(data, ctx.cwd);
  const text = buildWorkItemDebugPrompt(inheritedData.work_item, {
    scanReports: (inheritedData.artifacts || []).filter((artifact: { stage?: string }) => artifact.stage === "scan"),
    trigger: params.event_type || "manual",
    evidence: params.notes || params.description || "",
  });
  return { content: [{ type: "text", text }], details: { action: "debug_work_item", workItem: inheritedData.work_item, trigger: params.event_type || "manual" } };
}

export function triggerWorkItemReview(ctx: { cwd: string }, params: TaskManagerParams): ToolOutcome {
  if (!params.id) return errorOutcome("id required");
  const data = execPic(["show", params.id], ctx.cwd);
  if (!data.work_item || !["task", "bug", "chore"].includes(data.work_item.type)) {
    return { content: [{ type: "text", text: `Error: ${data.error || "Executable Work Item not found"}` }], details: {}, isError: true };
  }
  const review = buildReviewContext(params.id, ctx.cwd);
  if (review.error || !review.text) {
    return { content: [{ type: "text", text: `Error: ${review.error || "Failed to build review context"}` }], details: {}, isError: true };
  }
  const text = [
    `# Review Context for Work Item ${params.id}`,
    "",
    "This is the complete pack-bound review context. Review it directly; do not launch another reviewer.",
    "",
    review.text,
  ].join("\n");
  return {
    content: [{ type: "text", text }],
    details: { action: "trigger_work_item_review", readyForSubagent: true, workItem: data.work_item, gitDiff: review.gitDiff, reviewContext: review.text },
  };
}
