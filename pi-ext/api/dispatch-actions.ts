import type { PipelineScheduler } from "../pipeline/pipeline-scheduler.ts";
import type { TaskManagerParams } from "./task-manager-schema.ts";
import type { ToolOutcome } from "./blueprint-approval.ts";

export function listPipelineDispatchesAction(pipelineScheduler: PipelineScheduler): ToolOutcome {
  const pending = pipelineScheduler.listDispatches();
  return { content: [{ type: "text", text: JSON.stringify(pending, null, 2) }], details: { action: "list_pipeline_dispatches", dispatches: pending } };
}

export function bindPipelineDispatchAction(pipelineScheduler: PipelineScheduler, params: TaskManagerParams): ToolOutcome {
  if (!params.id || !params.agent_id) return { content: [{ type: "text", text: "Error: id (pipeline run id) and agent_id required" }], details: {}, isError: true };
  try {
    const bound = pipelineScheduler.bindDispatch(params.id, params.agent_id);
    return { content: [{ type: "text", text: JSON.stringify(bound, null, 2) }], details: { action: "bind_pipeline_dispatch", runId: params.id, agentId: params.agent_id } };
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return { content: [{ type: "text", text: `Bind blocked: ${message}` }], details: { action: "bind_pipeline_dispatch", error: message }, isError: true };
  }
}

export async function completePipelineDispatchAction(pipelineScheduler: PipelineScheduler, params: TaskManagerParams): Promise<ToolOutcome> {
  if (!params.id || !params.dispatch_status) return { content: [{ type: "text", text: "Error: id (pipeline run id) and dispatch_status (completed|failed) required" }], details: {}, isError: true };
  if (params.dispatch_status === "failed" && !params.output && !params.error) return { content: [{ type: "text", text: "Error: failed dispatch requires output or error" }], details: {}, isError: true };
  try {
    await pipelineScheduler.completeDispatch(params.id, { completed: params.dispatch_status === "completed", output: params.output || "", error: params.error, failureCode: params.failure_code });
    return { content: [{ type: "text", text: `Dispatch ${params.id} reported ${params.dispatch_status}; scheduler reconcile queued.` }], details: { action: "complete_pipeline_dispatch", runId: params.id, dispatchStatus: params.dispatch_status } };
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return { content: [{ type: "text", text: `Report blocked: ${message}` }], details: { action: "complete_pipeline_dispatch", error: message }, isError: true };
  }
}
