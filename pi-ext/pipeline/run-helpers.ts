import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { execPic, execPicText } from "../core/cli-helpers.ts";
import type { PipelineRun } from "./pipeline-types.ts";

export function checkpoint(run: PipelineRun, name: "integrated" | "artifact_saved" | "advanced", cwd: string, patchFile = ""): void {
  const args = ["workflow", "pipeline-checkpoint", run.id, run.lease_token, name];
  if (patchFile) args.push("--patch-file", patchFile);
  try {
    execPicText(args, cwd);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  } catch (error: any) {
    const message = error?.stderr?.toString().trim() || error?.message || String(error);
    if (message.includes("already recorded")) return;
    // Terminal runs with an expired lease are reconciled by the durable pending-run sweep.
    // Do not turn that cleanup race into a new worker blocker.
    if (name === "advanced" && (message.includes("invalid stage, status, lease") || message.includes("stale, invalid, or already recorded"))) return;
    throw new Error(message);
  }
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function saveWorkerReport(run: PipelineRun, cwd: string, taskReport: { status: "done" | "partial" | "blocked"; markdown: string }, report: any = { changedFiles: [], commandsRun: [], criteriaSatisfied: [], diffSummary: `Async worker ${taskReport.status}`, reviewFindings: [], residualRisks: [] }): void {
  const result = execPic([
    "workflow", "completion-save", run.task_id, taskReport.status,
    "--pipeline-run-id", run.id,
    "--summary", report.diffSummary || `Async worker ${taskReport.status}`,
    "--report-markdown", taskReport.markdown,
    "--files-changed-json", JSON.stringify(report.changedFiles || []),
    "--tests-run-json", JSON.stringify(report.commandsRun || []),
    "--acceptance-results-json", JSON.stringify(report.criteriaSatisfied || []),
    "--issues-json", JSON.stringify(report.reviewFindings || []),
    "--deviations-json", "[]",
    "--suggestions-json", JSON.stringify(report.residualRisks || []),
  ], cwd);
  if (result.error) throw new Error(result.error);
}

export function outputFor(run: PipelineRun): string {
  const path = join(run.async_dir || "", `output-${run.child_index || 0}.log`);
  if (!existsSync(path)) throw new Error(`subagent output missing: ${path}`);
  return readFileSync(path, "utf8");
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function statusFor(run: PipelineRun): any {
  const path = join(run.async_dir || "", "status.json");
  if (!existsSync(path)) return null;
  const status = JSON.parse(readFileSync(path, "utf8"));
  if (status.state === "running" && Number.isInteger(status.pid)) {
    try {
      process.kill(status.pid, 0);
    } catch {
      return { ...status, state: "failed", error: "subagent process is no longer running" };
    }
  }
  return status;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function formatPipelineStatus(result: any): string {
  const runs = Array.isArray(result?.runs) ? result.runs : [];
  if (!runs.length) return `Pipeline ${result?.task_id || "unknown"}: no runs`;
  const lines = [`Pipeline ${result.task_id || "unknown"}`];
  for (const run of runs) {
    const runId = run.subagent_run_id ? ` run=${String(run.subagent_run_id).slice(0, 8)}` : "";
    const model = run.agent_model ? ` model=${run.agent_model}` : "";
    const error = run.error ? ` error=${String(run.error).replace(/\s+/g, " ").slice(0, 120)}` : "";
    lines.push(`- ${run.stage || "unknown"} ${run.status || "unknown"} attempt=${run.attempt || 1}${runId}${model}${error}`);
  }
  return lines.join("\n");
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function formatPipelineStop(result: any): string {
  const cancelled = Array.isArray(result?.cancelled_runs) ? result.cancelled_runs.length : 0;
  return `Pipeline ${result?.task_id || "unknown"}: cancelled ${cancelled} run${cancelled === 1 ? "" : "s"}`;
}
