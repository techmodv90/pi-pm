import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// Agent-tool dispatch seam: pipeline stages are no longer executed by spawned
// `pi` processes; the scheduler persists a dispatch record, the contractor
// spawns the stage agent through the Agent tool (run_in_background, by hard
// gate), binds the returned agent id, and reports terminal output back. The
// reconcile machinery (status.json/output log/workspace.json) consumes the
// same artifact shapes the process runner used to produce.

export interface PipelineDispatch {
  runId: string;
  leaseToken: string;
  /** Agent definition name that must be passed to the Agent tool `agent` param. */
  agent: string;
  stage: string;
  taskId: string;
  task: string;
  asyncDir: string;
  /** Prepared isolated worktree; empty string means run in the project cwd. */
  worktree: string;
  initialPatchPath?: string;
  skillFamilies?: string[];
}

export function writePipelineDispatch(dispatch: PipelineDispatch): void {
  mkdirSync(dispatch.asyncDir, { recursive: true, mode: 0o700 });
  writeFileSync(join(dispatch.asyncDir, "dispatch.json"), JSON.stringify(dispatch, null, 2), { mode: 0o600 });
}

function readPipelineDispatch(asyncDir: string): PipelineDispatch | null {
  const path = join(asyncDir, "dispatch.json");
  if (!existsSync(path)) return null;
  return JSON.parse(readFileSync(path, "utf8")) as PipelineDispatch;
}

function dispatchDirs(cwd: string): string[] {
  const root = join(cwd, ".pi-subagents", "pipeline");
  if (!existsSync(root)) return [];
  return readdirSync(root, { withFileTypes: true }).filter((entry) => entry.isDirectory()).map((entry) => join(root, entry.name));
}

/** Pending dispatches: written, not yet bound to an Agent tool id. */
export function listPipelineDispatches(cwd: string): PipelineDispatch[] {
  return dispatchDirs(cwd)
    .filter((dir) => existsSync(join(dir, "dispatch.json")) && !existsSync(join(dir, "bind.json")))
    .map((dir) => readPipelineDispatch(dir))
    .filter((dispatch): dispatch is PipelineDispatch => dispatch !== null);
}

/** Any dispatch by run id regardless of bind state; completion reporting needs it. */
export function findPipelineDispatch(cwd: string, runId: string): PipelineDispatch | null {
  for (const dir of dispatchDirs(cwd)) {
    const dispatch = readPipelineDispatch(dir);
    if (dispatch?.runId === runId) return dispatch;
  }
  return null;
}

/**
 * Hard gate: a pipeline dispatch claim requires a non-empty Agent tool id.
 * Foreground spawns return text instead of an id, so an empty id means the
 * spawn bypassed the background gate — reject before the lease is bound.
 */
export function bindPipelineDispatch(dispatch: PipelineDispatch, agentId: string): void {
  if (!agentId || !agentId.trim()) throw new Error("pipeline dispatch bind requires a non-empty Agent tool id");
  if (existsSync(join(dispatch.asyncDir, "bind.json"))) throw new Error(`dispatch ${dispatch.runId} is already bound`);
  writeFileSync(join(dispatch.asyncDir, "bind.json"), JSON.stringify({ agentId: agentId.trim(), boundAt: new Date().toISOString() }, null, 2), { mode: 0o600 });
}

export interface PipelineDispatchReport {
  completed: boolean;
  output: string;
  error?: string;
  failureCode?: string;
}

/** Terminal artifacts mirroring the process runner's persistAgentResult shapes. */
export function writePipelineOutputLog(dispatch: PipelineDispatch, report: PipelineDispatchReport): void {
  writeFileSync(join(dispatch.asyncDir, "output-0.log"), report.output, { mode: 0o600 });
}

export function writePipelineStatus(dispatch: PipelineDispatch, report: PipelineDispatchReport): void {
  writeFileSync(
    join(dispatch.asyncDir, "status.json"),
    JSON.stringify({
      state: report.completed ? "completed" : "failed",
      error: report.completed ? "" : (report.error || report.output || "agent failed"),
      failure_code: report.completed ? "" : (report.failureCode || ""),
      steps: [{ status: report.completed ? "completed" : "failed" }],
    }, null, 2),
    { mode: 0o600 },
  );
}
