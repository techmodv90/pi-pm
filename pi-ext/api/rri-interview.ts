import { execPic } from "../core/cli-helpers.ts";
import { loadRriDraft, saveRriDraft } from "../core/rri-drafts.ts";
import { parseRriReportJson, renderRriReportMarkdown } from "../reporting/rri-report.ts";
import type { PipelineScheduler } from "../pipeline/pipeline-scheduler.ts";
import type { TaskManagerParams } from "./task-manager-schema.ts";
import { approvedScanLineage, rriDraftRoot } from "./task-manager-helpers.ts";
import type { ToolOutcome } from "./blueprint-approval.ts";

function errorOutcome(message: string, details: unknown = {}): ToolOutcome {
  return { content: [{ type: "text", text: `Error: ${message}` }], details, isError: true };
}

export function checkpointRriInterview(ctx: { cwd: string }, params: TaskManagerParams): ToolOutcome {
  if (!params.id || !params.content) return errorOutcome("id and JSON content required");
  let state: unknown;
  try { state = JSON.parse(params.content); }
  catch { return errorOutcome("RRI interview content must be valid JSON"); }
  if (!state || typeof state !== "object" || Array.isArray(state)) return errorOutcome("RRI interview content must be one JSON object");
  try {
    const path = saveRriDraft(rriDraftRoot(ctx.cwd), params.id, approvedScanLineage(ctx.cwd, params.id), state);
    const result = { work_item_id: params.id, checkpointed: true, path };
    return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return errorOutcome(message);
  }
}

export function loadRriInterview(ctx: { cwd: string }, params: TaskManagerParams): ToolOutcome {
  if (!params.id) return errorOutcome("id required");
  try {
    const result = loadRriDraft(rriDraftRoot(ctx.cwd), params.id, approvedScanLineage(ctx.cwd, params.id));
    return { content: [{ type: "text", text: JSON.stringify(result.state, null, 2) }], details: result };
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return errorOutcome(message);
  }
}

export function saveRriInterview(pipelineScheduler: PipelineScheduler, ctx: { cwd: string }, params: TaskManagerParams): ToolOutcome {
  if (!params.id || !params.content || params.actor_role !== "contractor") return errorOutcome("id, content (final RRI JSON), and actor_role=contractor are required");
  let rriPresentation = "";
  try {
    const payload = JSON.parse(params.content) as { report?: unknown };
    if (!payload.report) throw new Error("RRI finalization requires a structured report object");
    rriPresentation = renderRriReportMarkdown(parseRriReportJson(JSON.stringify(payload.report)));
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return { content: [{ type: "text", text: `Error: ${message}` }], details: {}, isError: true };
  }
  const result = execPic(["work-item", "rri-finalize", params.id, params.content, "--actor-role", params.actor_role], ctx.cwd);
  if (result.error) return { content: [{ type: "text", text: `Error: ${result.error}` }], details: result, isError: true };
  pipelineScheduler.finalizeHandoffs(params.id, "rri");
  return { content: [{ type: "text", text: rriPresentation }], details: { ...result, rriPresentation } };
}
