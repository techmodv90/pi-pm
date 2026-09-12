import { execPic } from "../core/cli-helpers.ts";
import { withInheritedParentWorkflowArtifacts } from "../tasking/task-artifacts.ts";
import { buildTaskVerifyPrompt } from "../tasking/work-item-prompts.ts";
import type { PicShowDocument } from "./pic-show.ts";
import type { PipelineRun, PipelineStage } from "./pipeline-types.ts";
import { activePackDoneReports, isMutationStage, latestVerificationAfter, pipelineVerificationBlockReason } from "./report-parsing.ts";
import { assertCleanGit } from "./integration.ts";
import { canonicalReadyLeafIds, normalizePipelineData, nextPipelineStage } from "./stage-resolution.ts";
import { launchGroup } from "./launch.ts";
import type { SchedulerDeps } from "./scheduler-context.ts";

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export async function advance(deps: SchedulerDeps, taskId: string, parentId?: string): Promise<void> {
  const { cwd } = deps;
  const raw = deps.showItem(taskId);
  const data = raw.work_item ? normalizePipelineData(raw) : withInheritedParentWorkflowArtifacts(raw, cwd);
  const next = nextPipelineStage(data, deps.pipelineRuns(taskId));
  if (next) {
    if (isMutationStage(next)) assertCleanGit(cwd);
    await launchGroup(deps, next, [taskId]);
    return;
  }
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
  if (!activePack) return;
  const verificationBlock = pipelineVerificationBlockReason(data);
  if (verificationBlock) throw new Error(verificationBlock);
  const doneReports = activePackDoneReports(data, activePack);
  if (doneReports.length) {
    if (!latestVerificationAfter(data, doneReports[0])) deps.sendUserMessage(buildTaskVerifyPrompt(data));
    return;
  }
  const done = execPic(["work-item", "status", taskId, "done"], cwd);
  if (done.error) throw new Error(done.error);
  // Close-out transition: point the contractor at the next dependency-ready
  // work so leaf completion flows straight into the next increment.
  if (parentId && !parentHasActiveRuns(deps, parentId)) {
    const parent = deps.showItem(parentId);
    const readyIds = readyLeafIds(deps, parent).filter((id) => id !== taskId);
    const nextUp = readyIds.length
      ? `${readyIds.length} dependency-ready leaf(es) will launch next: ${readyIds.slice(0, 5).join(", ")}${readyIds.length > 5 ? ", …" : ""}`
      : "No dependency-ready leaves remain; check `work-item workflow-status` for the aggregate's verification stage.";
    deps.sendUserMessage(`${taskId} is done. ${nextUp}`);
  }

  if (!parentId) return;
  if (parentHasActiveRuns(deps, parentId)) return;
  await scheduleReady(deps, parentId);
}

export function parentHasActiveRuns(deps: SchedulerDeps, parentId: string): boolean {
  const parent = deps.showItem(parentId);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const childIds = (parent.children || []).map((child: any) => child.id);
  const ids = new Set([parentId, ...childIds]);
  const active = execPic(["workflow", "pipeline-active"], deps.cwd);
  return Array.isArray(active) && active.some((run: PipelineRun) => ids.has(run.task_id));
}

/** Ready executable descendant ids under an aggregate root. */
export function readyLeafIds(deps: SchedulerDeps, root: PicShowDocument): string[] {
  return canonicalReadyLeafIds(root, (id) => deps.showItem(id));
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export async function scheduleReady(deps: SchedulerDeps, rootTaskId: string, explicitRetry = false): Promise<any> {
  const root = deps.showItem(rootTaskId);
  if (root.work_item) {
    const taskIds = canonicalReadyLeafIds(root, (id) => deps.showItem(id));
    if (!taskIds.length) return { rootTaskId, launches: [], blocked: "No authorized dependency-ready executable Work Items" };
    await new Promise<void>((resolve) => setImmediate(resolve));
    const stages = new Map<PipelineStage, string[]>();
    for (const taskId of taskIds) {
      const data = normalizePipelineData(deps.showItem(taskId));
      const stage = nextPipelineStage(data, deps.pipelineRuns(taskId));
      if (stage) stages.set(stage, [...(stages.get(stage) || []), taskId]);
    }
    const launches = [];
    for (const [stage, ids] of stages) launches.push(await launchGroup(deps, stage, ids, explicitRetry));
    return { rootTaskId, launches };
  }
  return { rootTaskId, launches: [], blocked: "Work Item not found" };
}
