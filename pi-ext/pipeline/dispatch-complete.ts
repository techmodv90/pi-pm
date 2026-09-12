import { execFile } from "node:child_process";
import { mkdirSync, realpathSync, statSync, writeFileSync } from "node:fs";
import { promisify } from "node:util";
import { join } from "node:path";
import type { SubagentResult } from "../subagent/types.ts";
import { isMutationStage, parseTaskCompletionReport } from "./report-parsing.ts";
import type { PipelineRun, PipelineStage } from "./pipeline-types.ts";
import { DEFAULT_GENERATED_FILES, filterGeneratedFiles, validateWorkerPatchArtifact, workerPatch } from "./worker-validation.ts";
import { findPipelineDispatch, writePipelineOutputLog, writePipelineStatus, type PipelineDispatchReport } from "./pipeline-dispatch.ts";
import type { SchedulerDeps } from "./scheduler-context.ts";

const execFileAsync = promisify(execFile);

export async function writeWorkerPatch(deps: Pick<SchedulerDeps, "cwd" | "showItem">, run: PipelineRun, result: SubagentResult): Promise<void> {
  if (!run.async_dir) return;
  const worktree = result.workspace?.assignedWorktree;
  if (!worktree) throw new Error("worker result missing assigned worktree");
  const gitToplevel = (await execFileAsync("git", ["-C", worktree, "rev-parse", "--show-toplevel"], { encoding: "utf8" })).stdout.trim();
  if (realpathSync(gitToplevel) !== realpathSync(worktree)) throw new Error(`worker worktree invariant failed after exit: assigned=${worktree} git_toplevel=${gitToplevel}`);
  // Pre-existing tolerance: an unreadable show document (e.g. no project DB in a
  // probe repo) falls back to default constraints instead of losing the patch.
  let constraints: Record<string, unknown> = {};
  try {
    const data = deps.showItem(run.task_id);
    const activePack = data.instruction_packs.find((pack) => pack.status === "active");
    constraints = JSON.parse(activePack?.constraints_json || "{}");
  } catch {}
  await execFileAsync("git", ["-C", worktree, "add", "-N", "--", "."], { encoding: "utf8" });
  const changedResult = await execFileAsync("git", ["-C", worktree, "diff", "--name-only", "HEAD"], { encoding: "utf8" });
  const filtered = filterGeneratedFiles(changedResult.stdout.trim().split("\n").filter(Boolean), constraints);
  const changedFiles = filtered.changedFiles;
  if (result.workspace) result.workspace.changedFiles = changedFiles;
  if (result.workspace) result.workspace.generatedFiles = filtered.generatedFiles;
  const excluded = [...DEFAULT_GENERATED_FILES, ...(Array.isArray(constraints.generated_files) ? constraints.generated_files : [])].map((pattern) => `:(exclude,glob)${pattern}`);
  const patchResult = await execFileAsync("git", ["-C", worktree, "diff", "--binary", "HEAD", "--", ".", ...excluded], { encoding: "utf8", maxBuffer: 100 * 1024 * 1024 });
  const dir = join(run.async_dir, "worktree-diffs");
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  const patch = workerPatch(run);
  writeFileSync(patch, patchResult.stdout, { mode: 0o600 });
  validateWorkerPatchArtifact(patch, join(run.async_dir, `output-${run.child_index || 0}.log`), { changedFiles: changedFiles });
  writeFileSync(join(run.async_dir, "workspace.json"), JSON.stringify(result.workspace, null, 2), { mode: 0o600 });
}

/**
 * Contractor-reported terminal output for a dispatched run. Completed
 * mutation stages get their worktree patch captured through the same
 * writeWorkerPatch path the process runner used; the reconcile loop then
 * advances the state machine exactly as before. Failed reports leave
 * completed_at/output null and persist a failed status for retry routing.
 */
export async function completeDispatch(deps: Pick<SchedulerDeps, "cwd" | "showItem">, queueReconcile: () => void, runId: string, report: PipelineDispatchReport): Promise<void> {
  const { cwd } = deps;
  const dispatch = findPipelineDispatch(cwd, runId);
  if (!dispatch) throw new Error(`no pipeline dispatch for run ${runId}`);
  // Dispatch-status semantics guard (T004 smoke, 2026-09-07): a returned
  // review verdict is a COMPLETED review stage whose failed status lives in
  // the report; dispatch_status=failed means "the stage never ran" and, if
  // misused, strands the state machine (reviewStatusForCandidate only admits
  // completed review runs, and pipeline-complete cannot correct a failed run).
  if (!report.completed && dispatch.stage === "review" && /<review_report\b/.test(report.output || "")) {
    throw new Error("review dispatch reported failed but its output carries a review report; a returned review verdict is a completed review stage — report it with dispatch_status=completed so the verdict routes the fix round (dispatch_status=failed means the stage never ran)");
  }
  writePipelineOutputLog(dispatch, report);
  if (report.completed && isMutationStage(dispatch.stage as PipelineStage)) {
    // Fail-fast report validation (T004 smoke, 2026-09-07): run the same
    // parseTaskCompletionReport validation finish() applies BEFORE any
    // terminal transition, so a malformed relay leaves the run `running` and
    // retryable instead of terminally blocked (blocked is uncorrectable).
    const taskReport = parseTaskCompletionReport(report.output || "");
    if (!dispatch.worktree) throw new Error(`dispatch ${runId} completed without a prepared worktree`);
    // Synthesize the minimal SubagentResult shape writeWorkerPatch consumes.
    await writeWorkerPatch(
      deps,
      { id: dispatch.runId, task_id: dispatch.taskId, stage: dispatch.stage, child_index: 0, async_dir: dispatch.asyncDir } as unknown as PipelineRun,
      { exitCode: 0, stopReason: "completed", messages: [], stderr: "", errorMessage: "", workspace: { assignedWorktree: dispatch.worktree } } as unknown as SubagentResult,
    );
    // Fail-fast empty-patch guard (T004 smoke, 2026-09-07): done workers with
    // no changes and no justification almost always committed their work
    // inside the task worktree, which breaks the scheduler's uncommitted-diff
    // patch capture. Surface that at capture time, not two stages later.
    const patch = workerPatch({ id: dispatch.runId, child_index: 0, async_dir: dispatch.asyncDir } as unknown as PipelineRun);
    if (taskReport.status === "done" && statSync(patch).size === 0 && !taskReport.no_change_justification) {
      throw new Error(`worker patch capture is empty (0 bytes) for run ${runId}; if the worker committed its changes inside the worktree, uncommit them (git reset --mixed <base>) and re-emit the completion report — task worktrees must never contain the work as commits; the scheduler captures the uncommitted diff`);
    }
  } else if (dispatch.worktree) {
    writeFileSync(join(dispatch.asyncDir, "workspace.json"), JSON.stringify({ assignedWorktree: dispatch.worktree }, null, 2), { mode: 0o600 });
  }
  writePipelineStatus(dispatch, report);
  queueReconcile();
}
