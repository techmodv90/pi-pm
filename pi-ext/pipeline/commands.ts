import { execPic } from "../core/cli-helpers.ts";
import { cleanupOrphanedSubagentWorktrees } from "../subagent/runner.ts";
import type { ExtensionContext } from "@mariozechner/pi-coding-agent";
import type { AggregateDeliveryState } from "./integration.ts";
import { isResumableExecutionState, normalizePipelineData, nextPipelineStage, buildPipelineDryRun } from "./stage-resolution.ts";
import { mergeAggregateBranch } from "./integration.ts";
import type { PipelineRun, PipelineStage } from "./pipeline-types.ts";
import { launchGroup } from "./launch.ts";
import type { SchedulerDeps } from "./scheduler-context.ts";

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export async function startReadyBatch(deps: SchedulerDeps, ctx: ExtensionContext): Promise<any> {
  const active = execPic(["work-item", "ready"], ctx.cwd);
  const listed = execPic(["work-item", "list"], ctx.cwd);
  const taskIds = [...new Set([
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    ...(Array.isArray(active) ? active.map((item: any) => item.id) : []),
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    ...(Array.isArray(listed) ? listed.filter((item: any) => ["task", "bug", "chore"].includes(item.type) && item.status === "in_progress").map((item: any) => item.id).filter((id: any) => {
      // Auto-batch must not touch items with a live claim; explicit retries are
      // guarded by the one-active-run-per-(task,stage) unique index instead.
      const runs = deps.pipelineRuns(id);
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
      if (runs.some((run: any) => run.status === "claimed" || run.status === "running")) return false;
      const state = execPic(["work-item", "workflow-status", id], ctx.cwd);
      return isResumableExecutionState(state);
    }) : []),
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  ])].filter((id: any): id is string => typeof id === "string");
  if (!taskIds.length) return { launches: [], blocked: "No authorized dependency-ready executable Work Items" };
  const stages = new Map<PipelineStage, string[]>();
  for (const taskId of taskIds) {
    const data = normalizePipelineData(execPic(["show", taskId], ctx.cwd));
    const stage = nextPipelineStage(data, deps.pipelineRuns(taskId));
    if (stage) stages.set(stage, [...(stages.get(stage) || []), taskId]);
  }
  const launches = [];
  for (const [stage, ids] of stages) launches.push(await launchGroup(deps, stage, ids));
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const pipelineRunIds = launches.flatMap((launch: any) => launch.pipelineRunIds || []);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const subagentRunIds = launches.flatMap((launch: any) => launch.subagentRunIds || []);
  if (!pipelineRunIds.length || !subagentRunIds.length) {
    return { taskIds, launches, blocked: "Ready Work Items were found, but no persisted pipeline or subagent runs were created." };
  }
  return { taskIds, launches, pipelineRunIds, subagentRunIds };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function status(taskId: string, ctx: ExtensionContext, lastError: string): any {
  const active = execPic(["workflow", "pipeline-active"], ctx.cwd);
  if (Array.isArray(active)) cleanupOrphanedSubagentWorktrees(ctx.cwd, new Set(active.flatMap((run: PipelineRun) => [run.id, run.subagent_run_id || ""]).filter(Boolean)));
  const activeRun = Array.isArray(active)
    ? active.find((run: PipelineRun) => run.id === taskId || run.subagent_run_id === taskId)
    : undefined;
  if (activeRun) return { task_id: activeRun.task_id, pipeline_run_id: activeRun.id, subagent_run_id: activeRun.subagent_run_id, runs: [activeRun] };
  const root = execPic(["show", taskId], ctx.cwd);
  const taskIds = root.work_item
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    ? [taskId, ...(root.children || []).map((child: any) => child.id)]
    : [taskId];
  const runs = taskIds.flatMap((id: string) => {
    const runs = execPic(["workflow", "pipeline-runs", id], ctx.cwd);
    return Array.isArray(runs) ? runs : [];
  });
  return { task_id: taskId, runs, error: runs.length ? "" : lastError };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export async function stop(taskId: string, ctx: ExtensionContext): Promise<any> {
  const currentStatus = status(taskId, ctx, "");
  const active = (currentStatus.runs || []).filter((run: PipelineRun & { status: string }) => run.status === "claimed" || run.status === "running");
  for (const run of active) {
    const cancelled = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "cancelled", "--error", "cancelled by operator"], ctx.cwd);
    if (cancelled.error) throw new Error(cancelled.error);
  }
  return { task_id: taskId, cancelled_runs: active.map((run: PipelineRun) => run.id) };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function dryRun(rootTaskId: string, ctx: ExtensionContext): any {
  const root = execPic(["show", rootTaskId], ctx.cwd);
  if (!root.work_item) return { rootTaskId, leaves: [], blocker: "Work Item not found" };
  return buildPipelineDryRun(root, (id) => execPic(["show", id], ctx.cwd));
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export async function mergeAggregate(workItemId: string, ctx: ExtensionContext): Promise<any> {
  const state = execPic(["work-item", "workflow-status", workItemId], ctx.cwd) as AggregateDeliveryState & { next_stage?: string; integration_mode?: string };
  if (state.integration_mode === "coordination" && state.next_stage === "done") return state;
  if (state.next_stage !== "merge_pending" || state.integration_mode !== "branch") throw new Error(`Work Item ${workItemId} is not awaiting a branch merge`);
  try {
    const mergeCommit = mergeAggregateBranch(ctx.cwd, state);
    const result = execPic(["work-item", "aggregate-merge-result", workItemId, state.verified_head, "merged", mergeCommit], ctx.cwd);
    if (result.error) throw new Error(result.error);
    return result;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    const blocked = execPic(["work-item", "aggregate-merge-result", workItemId, state.verified_head, "blocked", message], ctx.cwd);
    if (blocked.error) throw new Error(`${message}; failed to persist merge blocker: ${blocked.error}`);
    throw new Error(message);
  }
}
