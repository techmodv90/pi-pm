import { execFile, execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, realpathSync, statSync, writeFileSync } from "node:fs";
import { promisify } from "node:util";
import { join, basename } from "node:path";
import type { ExtensionAPI, ExtensionContext } from "@mariozechner/pi-coding-agent";
import { Type } from "typebox";
import { execPic, execPicText, withGitWriteLock } from "../core/cli-helpers.ts";
import { EphemeralHandoffStore } from "../core/ephemeral-handoffs.ts";
import { loadLatestBlueprintDraft } from "../core/blueprint-drafts.ts";


import { withInheritedParentWorkflowArtifacts } from "../tasking/task-artifacts.ts";
import { buildTaskVerifyPrompt, buildWorkItemContinuePrompt, buildPlanningHandoffXml, CANONICAL_SCAN_REPORT_XML_FORMAT } from "../tasking/work-item-prompts.ts";
import { discoverAgents } from "../subagent/agents.ts";
import { cleanupOrphanedSubagentWorktrees, finalAssistantText, prepareSubagentWorktree, removeSubagentWorktree, retainWorktreeForResume, startSubagentResilient, type SubagentHandle } from "../subagent/runner.ts";

import type { SubagentResult } from "../subagent/types.ts";
import { parsePicShow, type PicShowDocument } from "./pic-show.ts";
import { parsePipelineRuns, type PipelineRun, type PipelineStage } from "./pipeline-types.ts";
import { mergeRriTAuthoringResults, parseRriTPersonaResult, RRI_T_PERSONAS } from "./rri-t.ts";
import { activePackDoneReports, currentFailedReview, isMutationStage, latestVerificationAfter, parseReviewReport, parseTaskCompletionReport, persistedReviewOutcome, pipelineVerificationBlockReason } from "./report-parsing.ts";
import { assertCleanGit, assertReviewBaseCurrent, finalizeReviewedIntegration, mergeAggregateBranch, rejectedCandidatePatch, repositoryHead, verificationEnvironmentFingerprint, type AggregateDeliveryState } from "./integration.ts";
import { DEFAULT_GENERATED_FILES, filterGeneratedFiles, pipelineFailureResult, validateWorkerOutput, validateWorkerPatchArtifact, workerPatch } from "./worker-validation.ts";
import { REVIEW_FIX_ROUND_LIMIT, assertReviewFixChangedPatch, buildReviewFixCapBlock, reviewCycleCount } from "./corrections.ts";
import { isPlanningStage, pipelineSpawnParams, planningStages, stageAgent, stagePrompt, predecessorCheckpointFor, startFullScanFanout, workerSessionPath } from "./stage-prompts.ts";
import { assertRunContractCurrent, buildPipelineDryRun, canonicalReadyLeafIds, isResumableExecutionState, nextPipelineStage, normalizePipelineData, pipelineWorkerBlockReason, resolvePlanProfile, workerIntegrationCandidate, type PlanningProfileState } from "./stage-resolution.ts";
import { evaluateSkillFamilyRouting, recordSkillRoutingEvent } from "./skill-routing.ts";

export * from "./rri-t.ts";
export * from "./report-parsing.ts";
export * from "./integration.ts";
export * from "./worker-validation.ts";
export * from "./corrections.ts";
export * from "./stage-prompts.ts";
export * from "./instruction-pack-xml.ts";
export * from "./stage-resolution.ts";

const execFileAsync = promisify(execFile);









function checkpoint(run: PipelineRun, name: "integrated" | "artifact_saved" | "advanced", cwd: string, patchFile = ""): void {
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
function saveWorkerReport(run: PipelineRun, cwd: string, taskReport: { status: "done" | "partial" | "blocked"; markdown: string }, report: any = { changedFiles: [], commandsRun: [], criteriaSatisfied: [], diffSummary: `Async worker ${taskReport.status}`, reviewFindings: [], residualRisks: [] }): void {
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

function outputFor(run: PipelineRun): string {
  const path = join(run.async_dir || "", `output-${run.child_index || 0}.log`);
  if (!existsSync(path)) throw new Error(`subagent output missing: ${path}`);
  return readFileSync(path, "utf8");
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
function statusFor(run: PipelineRun): any {
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











export class PipelineScheduler {
  readonly handoffs = new EphemeralHandoffStore();
  private cwd = "";

  /** Fail-closed typed view of one `pic show` document. */
  showItem(id: string): PicShowDocument {
    return parsePicShow(execPic(["show", id], this.cwd));
  }

  /** Ready executable descendant ids under an aggregate root. */
  readyLeafIds(root: PicShowDocument): string[] {
    return canonicalReadyLeafIds(root, (id) => this.showItem(id));
  }

  private integrating = Promise.resolve();
  private reconciling = false;
  private context?: ExtensionContext;
  private lastError = "";

  private roots = new Set<string>();
  private readonly pi: ExtensionAPI;
  private agentRuns = new Map<string, PipelineRun>();
  private agentHandles = new Map<string, SubagentHandle>();
  // Durable worker worktree constraint (RLB-GAP-001): failure modes of retained
  // pack worktrees, keyed by worktree key (instruction pack id), consumed by the
  // next launch of the same pack as the resume preamble source.
  private retainedFailures = new Map<string, string>();

  constructor(pi: ExtensionAPI) { this.pi = pi; }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  async runRriT(data: any): Promise<string> {
    const item = data?.work_item || {};
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    const scope = JSON.stringify({ work_item: item, requirements: data?.requirements || [], artifacts: (data?.artifacts || []).filter((artifact: any) => ["scan", "vision", "blueprint", "contracts", "task_graph"].includes(artifact.stage)), children: data?.children || [] });
    const text = `${item.title || item.id || "Aggregate"} ${item.description || ""} ${scope}`;
    const personas: string[] = ["QA / Tester"];
    if ((data?.requirements || []).length || /rule|workflow|policy|report|requirement|business/i.test(text)) personas.push("Business Analyst");
    if (/ui|ux|user|screen|page|form|mobile|accessib/i.test(text)) personas.push("End User");
    if (/api|database|integration|code|module|dependency|performance|backend|existing/i.test(text) || !personas.includes("Developer")) personas.push("Developer");
    if (/production|deploy|operation|observability|backup|recovery|scale|uptime|monitor/i.test(text)) personas.push("Operator");
    const uniquePersonas = [...new Set(personas)].filter((persona): persona is (typeof RRI_T_PERSONAS)[number] => RRI_T_PERSONAS.includes(persona as (typeof RRI_T_PERSONAS)[number]));
    const personaAgent = discoverAgents(this.cwd, "project").find((candidate) => candidate.name === "rri-t-persona");
    if (!personaAgent) throw new Error("Task-system agent definition not found: rri-t-persona");
    // RRI-T authoring-only fanout: personas run with only the repository reading
    // tools declared by the rri-t-persona definition (read, grep, find, ls), no
    // worktree isolation, and exactly two validation attempts that carry the named
    // parser error into the retry; personas never execute procedures or self-grade,
    // so a persona run ends only in validated scenarios or a bounded failure.
    const results = await Promise.all(uniquePersonas.map(async (persona) => {
      let lastError = "";
      for (let attempt = 0; attempt < 2; attempt++) {
        const handle = startSubagentResilient({
          agent: personaAgent,
          task: `# RRI-T aggregate scenario authoring\nWork Item: ${item.id || "unknown"}\nAssigned perspective: ${persona}\n\nRepository context:\n${scope}\n\nSelect only risk-relevant scenarios for this perspective and author them; do not execute any procedure, collect evidence, or grade results. Return exactly one <rri_t_persona> XML document.${lastError ? ` Previous output was invalid: ${lastError}. Correct it on this retry.` : ""}`,
          cwd: this.cwd,
          stage: "aggregate_verification",
          taskId: item.id,
          acceptance: "checked",
        });
        const result = await handle.result;
        try {
          if (result.exitCode !== 0) throw new Error(result.errorMessage || result.stderr || "persona process failed");
          return parseRriTPersonaResult(finalAssistantText(result.messages), persona);
        } catch (error) {
          lastError = error instanceof Error ? error.message : String(error);
        }
      }
      throw new Error(`RRI-T persona ${persona} failed validation: ${lastError}`);
    }));
    return JSON.stringify(mergeRriTAuthoringResults(results, uniquePersonas));
  }

  private async persistAgentResult(result: SubagentResult): Promise<void> {
    const run = this.agentRuns.get(result.runId);
    if (!run?.async_dir) return;

    try {
      // Completion handoff must yield after HerdR closes; patch inspection and pic reconciliation are synchronous.
      await new Promise<void>((resolve) => setImmediate(resolve));
      const completed = result.exitCode === 0 && result.stopReason !== "aborted";
      const status = completed ? "completed" : "failed";
      const output = finalAssistantText(result.messages) || result.stderr || result.errorMessage || "";
      writeFileSync(join(run.async_dir, `output-${run.child_index || 0}.log`), output, { mode: 0o600 });
      if (result.workspace) writeFileSync(join(run.async_dir, "workspace.json"), JSON.stringify(result.workspace, null, 2), { mode: 0o600 });
      if (completed && isMutationStage(run.stage)) await this.writeWorkerPatch(run, result);
      // Transient-fault classification persistence constraint: carry the runner's
      // in-claim transient provider classification into the status artifact so the
      // reconciliation completion (pipeline-complete --result-json) can surface
      // durable failure_code=transient_provider instead of a generic failure.
      writeFileSync(join(run.async_dir, "status.json"), JSON.stringify({ state: completed ? "completed" : "failed", error: result.errorMessage || result.stderr || "", failure_code: completed ? "" : result.failureCode || "", steps: [{ status, model: result.model || "" }] }), { mode: 0o600 });
      // Durable worker worktree constraint (RLB-GAP-001): a mutation-stage child
      // that died before emitting its completion report retains its pack-keyed
      // worktree for resume; deterministic outcomes (report emitted, success,
      // cancellation, parse-invalid output with exit 0) clean up exactly as
      // GAP-091/096 required. Worktree ownership is keyed by the branch key,
      // which equals the worktree directory name (claim id or instruction pack).
      const assignedWorktree = result.workspace?.assignedWorktree;
      if (assignedWorktree) {
        const worktreeKey = basename(assignedWorktree);
        if (retainWorktreeForResume(run.stage, result)) {
          this.retainedFailures.set(worktreeKey, result.failureCode || result.stopReason || "prior attempt died before emitting its completion report");
        } else {
          this.retainedFailures.delete(worktreeKey);
          removeSubagentWorktree(this.cwd, assignedWorktree, worktreeKey);
        }
      }
      this.agentHandles.delete(result.runId);
      this.agentRuns.delete(result.runId);
      this.queueReconcile();
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (run.async_dir) writeFileSync(join(run.async_dir, "status.json"), JSON.stringify({ state: "failed", error: message, failure_code: result.failureCode || "", steps: [{ status: "failed", error: message }] }), { mode: 0o600 });
      if (result.workspace?.assignedWorktree) try { removeSubagentWorktree(this.cwd, result.workspace.assignedWorktree, basename(result.workspace.assignedWorktree)); } catch {}
      this.agentHandles.delete(result.runId);
      this.agentRuns.delete(result.runId);
      if (this.context) this.reportError(error, this.context);
      this.queueReconcile();
    }
  }

  private queueReconcile(): void {
    setImmediate(() => { void this.reconcileSafely(); });
  }


  private async reconcileSafely(): Promise<void> {
    try {
      await this.reconcile();
    } catch (error) {
      if (this.context) this.reportError(error, this.context);
    }
  }

  private async writeWorkerPatch(run: PipelineRun, result: SubagentResult): Promise<void> {
    if (!run.async_dir) return;
    const worktree = result.workspace?.assignedWorktree;
    if (!worktree) throw new Error("worker result missing assigned worktree");
    const gitToplevel = (await execFileAsync("git", ["-C", worktree, "rev-parse", "--show-toplevel"], { encoding: "utf8" })).stdout.trim();
    if (realpathSync(gitToplevel) !== realpathSync(worktree)) throw new Error(`worker worktree invariant failed after exit: assigned=${worktree} git_toplevel=${gitToplevel}`);
    // Pre-existing tolerance: an unreadable show document (e.g. no project DB in a
    // probe repo) falls back to default constraints instead of losing the patch.
    let constraints: Record<string, unknown> = {};
    try {
      const data = this.showItem(run.task_id);
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

  startSession(ctx: ExtensionContext): void {
    this.cwd = ctx.cwd;
    this.context = ctx;
  }


  stopSession(): void {
    this.handoffs.clear();
    this.context = undefined;
  }

  finalizeHandoffs(workItemId: string, workflow: string): void {
    this.handoffs.deleteForWorkItem(workItemId, workflow);
  }

  private reportError(error: unknown, ctx: ExtensionContext): void {
    const message = error instanceof Error ? error.message : String(error);
    ctx.ui.setStatus("task-pipeline", undefined);
    if (message === this.lastError) return;
    this.lastError = message;
    if (message.includes("autofix cycle limit reached")) {
      this.pi.sendUserMessage(
        "Targeted autofix stopped after three completed fix cycles for the unchanged active TIP. Review the persisted verification evidence and choose one owner action: revise the TIP, accept the remaining failure as explicit debt, or stop the task.",
        { deliverAs: "followUp" },
      );
      return;
    }
    if (message.includes("worker circuit breaker open")) {
      this.pi.sendUserMessage(
        "The worker circuit breaker stopped this task after a deterministic failure for the unchanged active TIP. Do not modify the task-system extension from this application session. After the runner or report protocol is repaired separately, record an owner circuit reset with evidence before retrying.",
        { deliverAs: "followUp" },
      );
      return;
    }
    if (message.includes("deterministic contract failure requires TIP revision")) {
      this.pi.sendUserMessage(
        "The worker retry was not launched because the active TIP has a deterministic failure. Revise and activate a new TIP, then explicitly retry; the unchanged pack cannot continue.",
        { deliverAs: "followUp" },
      );
      return;
    }
    this.stopSession();
    this.pi.sendUserMessage(`Async pipeline paused: ${message}`, { deliverAs: "followUp" });
    ctx.ui.notify(`Async pipeline paused: ${message}`, "warning");
  }

  private reportProgress(runId: string, taskId: string, stage: PipelineStage, event: string, text: string): void {
    try { this.pi.events.emit("task-pipeline:progress", { runId, taskId, stage, event, text }); } catch {}
    const ctx = this.context;
    if (ctx) try { ctx.ui.setStatus("task-pipeline", `${stage} ${taskId}: ${event}`); } catch {}
  }

  private notifyBlockedAttempt(run: PipelineRun, reason: string): void {
    const attempt = run.attempt || 1;
    const integrated = this.cwd && this.pipelineRuns(run.task_id).some((candidate) => candidate.id === run.id && candidate.integrated_at);
    const patchState = integrated ? "The patch was integrated before the pipeline paused." : "No patch was integrated; the repository was not changed by this attempt.";
    const nextAction = "Review the blocker, correct the worker or runner issue, then explicitly retry.";
    this.pi.sendUserMessage(
      `${run.task_id} ${run.stage} attempt ${attempt} is blocked.\n\nReason: ${reason}\n\n${patchState}\n\n${nextAction}`,
      { deliverAs: "followUp" },
    );
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  async start(rootTaskId: string, ctx: ExtensionContext): Promise<any> {
    this.cwd = ctx.cwd;
    this.context = ctx;
    this.lastError = "";
    this.roots.add(rootTaskId);
    const workflow = execPic(["work-item", "workflow-status", rootTaskId], ctx.cwd);
    if (workflow.next_stage === "scan") {
      const rejection = execPic(["work-item", "scan-rejection", rootTaskId], ctx.cwd);
      if (rejection.rejected) {
        throw new Error(`Scan report was rejected by the contractor: ${rejection.reason}. Owner decision required: call reset_work_item_planning with actor_role=owner to rescan, or leave the Work Item at Scan and do not retry.`);
      }
      return await this.launchGroup("scan", [rootTaskId]);
    }
    if (workflow.next_stage === "rri") {
      const data = execPic(["show", rootTaskId], ctx.cwd);
      return { stage: "rri", taskIds: [rootTaskId], contractor: true, prompt: buildWorkItemContinuePrompt(workflow, data.work_item) };
    }
    if (planningStages.includes(workflow.next_stage)) {
      if (workflow.next_stage === "contracts") throw new Error("Contract drafting is Contractor-owned; use work_on_work_item to return the Contract prompt to the main session");
      assertCleanGit(ctx.cwd);
      return await this.launchGroup(workflow.next_stage, [rootTaskId]);
    }
    assertCleanGit(ctx.cwd);
    await this.reconcile();
    return await this.scheduleReady(rootTaskId);
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  async startReadyBatch(ctx: ExtensionContext): Promise<any> {
    this.cwd = ctx.cwd;
    this.context = ctx;
    this.lastError = "";
    assertCleanGit(ctx.cwd);
    await this.reconcile();
    const ready = execPic(["work-item", "ready"], ctx.cwd);
    const listed = execPic(["work-item", "list"], ctx.cwd);
    const taskIds = [...new Set([
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
      ...(Array.isArray(ready) ? ready.map((item: any) => item.id) : []),
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
      ...(Array.isArray(listed) ? listed.filter((item: any) => ["task", "bug", "chore"].includes(item.type) && item.status === "in_progress").map((item: any) => item.id).filter((id: any) => {
        // Auto-batch must not touch items with a live claim; explicit retries are
        // guarded by the one-active-run-per-(task,stage) unique index instead.
        const runs = this.pipelineRuns(id);
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
      const stage = nextPipelineStage(data, this.pipelineRuns(taskId));
      if (stage) stages.set(stage, [...(stages.get(stage) || []), taskId]);
    }
    const launches = [];
    for (const [stage, ids] of stages) launches.push(await this.launchGroup(stage, ids));
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
  dryRun(rootTaskId: string, ctx: ExtensionContext): any {
    const root = execPic(["show", rootTaskId], ctx.cwd);
    if (!root.work_item) return { rootTaskId, leaves: [], blocker: "Work Item not found" };
    return buildPipelineDryRun(root, (id) => execPic(["show", id], ctx.cwd));
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  status(taskId: string, ctx: ExtensionContext): any {
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
    return { task_id: taskId, runs, error: runs.length ? "" : this.lastError };
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  async stop(taskId: string, ctx: ExtensionContext): Promise<any> {
    const status = this.status(taskId, ctx);
    const active = (status.runs || []).filter((run: PipelineRun & { status: string }) => run.status === "claimed" || run.status === "running");
    const runIds = [...new Set(active.map((run: PipelineRun) => run.subagent_run_id).filter(Boolean))];
    for (const run of active) {
      const cancelled = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "cancelled", "--error", "cancelled by operator"], ctx.cwd);
      if (cancelled.error) throw new Error(cancelled.error);
    }
    for (const runId of runIds) this.agentHandles.get(String(runId))?.stop();
    return { task_id: taskId, cancelled_runs: active.map((run: PipelineRun) => run.id) };
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  async mergeAggregate(workItemId: string, ctx: ExtensionContext): Promise<any> {
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

  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  private async scheduleReady(rootTaskId: string, explicitRetry = false): Promise<any> {
    const root = this.showItem(rootTaskId);
    if (root.work_item) {
      const taskIds = this.readyLeafIds(root);
      if (!taskIds.length) return { rootTaskId, launches: [], blocked: "No authorized dependency-ready executable Work Items" };
      await new Promise<void>((resolve) => setImmediate(resolve));
      const stages = new Map<PipelineStage, string[]>();
      for (const taskId of taskIds) {
        const data = normalizePipelineData(this.showItem(taskId));
        const stage = nextPipelineStage(data, this.pipelineRuns(taskId));
        if (stage) stages.set(stage, [...(stages.get(stage) || []), taskId]);
      }
      const launches = [];
      for (const [stage, ids] of stages) launches.push(await this.launchGroup(stage, ids, explicitRetry));
      return { rootTaskId, launches };
    }
    return { rootTaskId, launches: [], blocked: "Work Item not found" };
  }


  // Planning profile constraint: refuse to dispatch a planning stage that the
  // persisted Plan profile (or the kind/depth contract before it is persisted)
  // does not include, and bind the claim to the persisted profile version/hash
  // so a stale Go/TypeScript profile view cannot dispatch an unapproved stage.
  // A stage must not dispatch before the Plan profile is persisted: the handoff
  // envelope requires a profile version/hash, so a resolved:false profile would
  // publish an invalid envelope with no recovery.
  private planEligibility(taskId: string, stage: PipelineStage): { profile: PlanningProfileState } {
    const profile = resolvePlanProfile(normalizePipelineData(this.showItem(taskId)));
    if (!profile.resolved || !profile.contentHash) {
      throw new Error(`planning stage ${stage} cannot dispatch for ${taskId} before the Plan profile is persisted; persist the approved profile before dispatch`);
    }
    if (!profile.stages.includes(stage)) {
      throw new Error(`planning stage ${stage} is not in the persisted plan profile for ${taskId} (depth ${profile.depth}); revise the profile before dispatch`);
    }
    return { profile };
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  private async launchGroup(stage: PipelineStage, taskIds: string[], explicitRetry = false): Promise<any> {
    const active = execPic(["workflow", "pipeline-active"], this.cwd);
    const activeRuns = Array.isArray(active) ? active.filter((run: PipelineRun) => run.stage === stage && taskIds.includes(run.task_id)) : [];
    const activeTaskIds = new Set(activeRuns.map((run: PipelineRun) => run.task_id));
    const launchTaskIds = taskIds.filter((taskId) => !activeTaskIds.has(taskId));
    if (launchTaskIds.length === 0) return { stage, taskIds, pipelineRunIds: [], activePipelineRunIds: activeRuns.map((run: PipelineRun) => run.id), subagentRunIds: [] };
    const workerPrompts = new Map<string, string>();
    const initialPatchPaths = new Map<string, string>();
    const reviewFixTaskIds = new Set<string>();
    if (isMutationStage(stage)) {
      assertCleanGit(this.cwd);
      for (const taskId of launchTaskIds) {
        const raw = this.showItem(taskId);
        const data = raw.work_item ? normalizePipelineData(raw) : withInheritedParentWorkflowArtifacts(raw, this.cwd);
        if (!data.work_item) throw new Error(data.error || `Task ${taskId} not found`);
        const blockReason = pipelineWorkerBlockReason(data);
        if (blockReason) throw new Error(blockReason);
        const runs = this.pipelineRuns(taskId);
        // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
        const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
        // Observe-mode routing telemetry (skill-family-routing plan): record the
        // routing evaluation for every worker/autofix launch without ever
        // blocking it — enforcement is a follow-up gated on this data.
        if (stage === "worker" || stage === "autofix") {
          const routingPack = (data.instruction_packs || []).find((pack: { status?: string }) => pack.status === "active");
          const scanEvidence = Array.isArray(data.scan_reports) && data.scan_reports.length ? [data.scan_reports[0]] : [];
          recordSkillRoutingEvent(this.cwd, taskId, stage, routingPack?.id || "", evaluateSkillFamilyRouting(routingPack || {}, scanEvidence, { cwd: this.cwd }));
        }
        if (stage === "worker" && currentFailedReview(runs, activePack)) {
          const cycle = reviewCycleCount(runs);
          if (cycle >= REVIEW_FIX_ROUND_LIMIT) {
            // Round-cap persistence constraint: persist the owner-action block
            // durably BEFORE refusing the launch, so the failed review is elevated
            // to owner-approval-required and nextPipelineStage/claim gates stop
            // relaunching the fix worker across reconciliation (a transient throw
            // alone would leave the failed review eligible for a repeated launch).
            const failedReview = currentFailedReview(runs, activePack);
            const capBlock = buildReviewFixCapBlock(taskId, failedReview?.findings || []);
            try {
              const blocked = execPic(["workflow", "review-fix-block", taskId, "--summary", capBlock], this.cwd);
              if (blocked.error) throw new Error(blocked.error);
            } catch (error) {
              const message = error instanceof Error ? error.message : String(error);
              throw new Error(`round-cap block persisted with error (${message}); owner action still required:\n\n${capBlock}`);
            }
            throw new Error(capBlock);
          }
          reviewFixTaskIds.add(taskId);
          const rejectedPatch = rejectedCandidatePatch(data, runs, this.cwd);
          if (rejectedPatch) initialPatchPaths.set(taskId, rejectedPatch);
        }
      }
    }
    const claims: PipelineRun[] = [];
    try {
      for (const taskId of launchTaskIds) {
        const data = this.showItem(taskId);
        // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
        const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
        const claimArgs = ["workflow", "pipeline-claim", taskId, stage, "--lease-seconds", "14400", "--environment-fingerprint", verificationEnvironmentFingerprint(this.cwd), "--base-commit", repositoryHead(this.cwd)];
        if (isPlanningStage(stage)) {
          const { profile } = this.planEligibility(taskId, stage);
          if (profile.resolved && profile.version > 0) claimArgs.push("--profile-version", String(profile.version), "--profile-hash", profile.contentHash);
        }
        if (stage === "worker" && reviewFixTaskIds.has(taskId)) claimArgs.push("--review-fix", "1");
        if (stage === "worker" && explicitRetry) claimArgs.push("--explicit-retry", "1");
        if (activePack && (isMutationStage(stage) || stage === "review")) claimArgs.push("--instruction-pack-id", activePack.id ?? "", "--instruction-pack-hash", activePack.content_hash ?? "");
        const claim = execPic(claimArgs, this.cwd);
        if (claim.error) throw new Error(claim.error);
        claims.push(claim);
      }
      if (isMutationStage(stage)) {
        await new Promise<void>((resolve) => setImmediate(resolve));
        for (const taskId of launchTaskIds) {
          const raw = this.showItem(taskId);
          const reset = execPic(["work-item", "status", taskId, "in_progress"], this.cwd);
          if (reset.error) throw new Error(reset.error);
          if (!raw.work_item) {
            const event = execPic(["workflow", "event-add", taskId, "implementation_started", "--actor-role", "orchestrator", "--summary", stage === "autofix" ? "Targeted autofix started" : "Persisted Worker stage started"], this.cwd);
            if (event.error) throw new Error(event.error);
          }
        }
      }
      const subagentRunIds: string[] = [];
      await new Promise<void>((resolve) => setImmediate(resolve));
      for (let index = 0; index < claims.length; index++) {
        const claim = claims[index]!;
        const taskId = launchTaskIds[index]!;
        const data = normalizePipelineData(this.showItem(taskId));
        // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
        const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
        let skillFamilies: string[] = [];
        if (activePack?.skill_families_json) {
          const parsed = JSON.parse(activePack.skill_families_json);
          if (!Array.isArray(parsed) || !parsed.every((family) => typeof family === "string")) throw new Error(`Task ${taskId} has invalid persisted skill families`);
          skillFamilies = parsed;
        }
        let taskPrompt = workerPrompts.get(taskId) || stagePrompt(stage, taskId, this.cwd);
        if (stage === "rri") taskPrompt += `\n\nComplete RRI source context:\n${JSON.stringify({ work_item: data.work_item, scan_reports: data.scan_reports, requirements: data.requirements || [], owner_decisions: data.owner_decisions || [] })}`;
        const task = { agent: stageAgent(stage), task: taskPrompt, taskId, ...(isMutationStage(stage) || stage === "review" ? { skillFamilies } : {}) };
        const spec = pipelineSpawnParams(stage, task, this.cwd);
        if (stage === "worker") {
          spec.initialPatchPath = initialPatchPaths.get(taskId);
          spec.sessionPath = workerSessionPath(this.cwd, activePack?.id || claim.instruction_pack_id || taskId);
          // Durable worker worktree constraint (RLB-GAP-001): worker-stage spawns
          // (including review-fix relaunches) key their worktree by instruction
          // pack so a transient failure retains the partial work for the retry;
          // review/scan stages stay run-keyed and clean up per GAP-091/096.
          const packKey = activePack?.id || claim.instruction_pack_id || taskId;
          spec.durableWorktreeKey = packKey;
          const retainedMode = this.retainedFailures.get(packKey);
          if (retainedMode) spec.resumeFailureMode = retainedMode;
        }
        if (stage === "review") {
          const candidate = this.pipelineRuns(taskId).find((entry) => entry.id === claim.candidate_run_id);
          if (!candidate?.integrated_patch_path || candidate.integrated_patch_hash !== claim.candidate_patch_hash || !existsSync(candidate.integrated_patch_path)) {
            throw new Error("review candidate patch attestation failed");
          }
          spec.initialPatchPath = candidate.integrated_patch_path;
        }
        const agent = discoverAgents(this.cwd, "project").find((candidate) => candidate.name === spec.agent);
        if (!agent) throw new Error(`Task-system agent definition not found: ${spec.agent}`);
        if (spec.isolation === "worktree") {
          let prepared;
          try {
            prepared = await prepareSubagentWorktree(spec.cwd, spec.initialPatchPath, claim.id, spec.durableWorktreeKey || claim.id);
          } catch (error) {
            if (stage === "review") {
              const candidate = this.pipelineRuns(taskId).find((entry) => entry.id === claim.candidate_run_id);
              if (candidate) {
                execPic(["workflow", "pipeline-complete", candidate.id, candidate.lease_token, "blocked", "--error", "candidate patch no longer applies to the current integration base"], this.cwd);
                checkpoint(candidate, "advanced", this.cwd);
              }
            }
            throw error;
          }
          spec.runId = prepared.runId;
          spec.preparedWorktree = prepared.cwd;
          spec.reusedRetainedWorktree = prepared.reused;
          // Fresh creation after a deterministic terminal: no retained worktree
          // exists for this pack anymore, so drop the stale failure-mode note.
          if (!prepared.reused && spec.durableWorktreeKey) this.retainedFailures.delete(spec.durableWorktreeKey);
        }
        let runId = "";
        let handle: SubagentHandle;
        try {
          handle = stage === "scan" && ["epic", "feature"].includes(data.work_item?.type)
            ? startFullScanFanout(spec, agent)
            : startSubagentResilient({ ...spec, agent }, (update) => {
                this.reportProgress(runId, taskId, stage, update.event, finalAssistantText(update.result.messages));
              });
        } catch (error) {
          // Durable worker worktree constraint (RLB-GAP-001): a reused retained
          // worktree keeps its partial work on spawn failure; only fresh
          // creations are cleaned up.
          if (spec.preparedWorktree && spec.runId && !spec.reusedRetainedWorktree) removeSubagentWorktree(this.cwd, spec.preparedWorktree, basename(spec.preparedWorktree));
          throw error;
        }
        runId = handle.id;
        const artifactDir = join(this.cwd, ".pi-subagents", "pipeline", claim.id);
        mkdirSync(artifactDir, { recursive: true, mode: 0o700 });
        writeFileSync(join(artifactDir, "status.json"), JSON.stringify({ state: "running", pid: handle.pid, steps: [{ status: "running" }] }), { mode: 0o600 });
        this.agentRuns.set(runId, { ...claim, skillFamilies, taskPrompt, subagent_run_id: runId, async_dir: artifactDir, child_index: 0 });
        this.agentHandles.set(runId, handle);
        void handle.result.then((result) => this.persistAgentResult(result));
        const bound = execPic(["workflow", "pipeline-bind", claim.id, claim.lease_token, runId, "--async-dir", artifactDir, "--child-index", "0"], this.cwd);
        if (bound.error) {
          handle.stop();
          throw new Error(bound.error);
        }
        subagentRunIds.push(runId);
      }
      return {
        stage,
        taskIds: launchTaskIds,
        pipelineRunIds: claims.map((claim) => claim.id),
        activePipelineRunIds: [...activeRuns.map((run: PipelineRun) => run.id), ...claims.map((claim) => claim.id)],
        subagentRunIds,
      };
    } catch (error) {
      for (const claim of claims) execPic(["workflow", "pipeline-complete", claim.id, claim.lease_token, "failed", "--error", error instanceof Error ? error.message : String(error)], this.cwd);
      throw error;
    }
  }

  private async reconcile(): Promise<void> {
    if (!this.cwd || this.reconciling) return;
    this.reconciling = true;
    try {
      const active = execPic(["workflow", "pipeline-active"], this.cwd);
      if (!Array.isArray(active)) return;
      cleanupOrphanedSubagentWorktrees(this.cwd, new Set(active.flatMap((run: PipelineRun) => [run.id, run.subagent_run_id || ""]).filter(Boolean)));
      for (const run of active as PipelineRun[]) {
        const renewed = execPic(["workflow", "pipeline-renew", run.id, run.lease_token], this.cwd);
        if (renewed.error) continue;
        const status = statusFor(run);
        if (!status || status.state === "running" || status.state === "queued") continue;
        const childStatus = status.steps?.[run.child_index || 0]?.status || status.state;
        if (childStatus === "running" || childStatus === "queued") continue;
        if (childStatus !== "complete" && childStatus !== "completed") {
          const childError = status.steps?.[run.child_index || 0]?.error;
          const reason = childError || status.error || `subagent child ${childStatus}`;
          // Transient-fault classification persistence constraint: the runner's in-claim
          // transient provider classification (surfaced via status.failure_code from
          // status.json) must reach the durable pipeline run result. It takes precedence
          // over the reason-string mapping so exhaustion always lands as failure_code=
          // transient_provider on the blocked stage, feeding the existing block event path.
          const failureCode = typeof status.failure_code === "string" && status.failure_code ? status.failure_code : pipelineFailureResult(reason).failure_code;
          const completeArgs = ["workflow", "pipeline-complete", run.id, run.lease_token, "failed", "--error", reason];
          if (failureCode) completeArgs.push("--result-json", JSON.stringify({ failure_code: failureCode }));
          execPic(completeArgs, this.cwd);
          checkpoint(run, "advanced", this.cwd);
          this.notifyBlockedAttempt(run, reason);
          continue;
        }
        this.integrating = this.integrating.then(() => this.finish(run, status)).catch(() => undefined);
        await this.integrating;
      }
      const pending = execPic(["workflow", "pipeline-pending"], this.cwd);
      if (Array.isArray(pending)) {
        for (const run of pending as PipelineRun[]) await this.resumePending(run);
      }
    } finally {
      this.reconciling = false;
    }
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  private async finish(run: PipelineRun, status: any): Promise<void> {
    let reviewCompleted = false;
    try {
      const child = status.steps?.[run.child_index || 0] || {};
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
      const resolvedModel = child.model || child.resolvedModel || child.modelAttempts?.findLast?.((attempt: any) => attempt.success)?.model || "";
      if (resolvedModel) execPic(["workflow", "pipeline-model", run.id, run.lease_token, resolvedModel], this.cwd);
      if (isMutationStage(run.stage)) {
        const output = outputFor(run);
        const taskReport = parseTaskCompletionReport(output);
        if (!run.artifact_saved_at) {
          // Provenance comes from the persisted claim; Workers need not echo hashes in prose.
          if (taskReport.status === "done") {
            const workspacePath = join(run.async_dir || "", "workspace.json");
            if (!existsSync(workspacePath)) throw new Error(`worker workspace diagnostics missing: ${workspacePath}`);
            const workspace = JSON.parse(readFileSync(workspacePath, "utf8"));
            const data = normalizePipelineData(this.showItem(run.task_id));
            assertRunContractCurrent(data, run);
            // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
            const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
            const constraints = JSON.parse(activePack?.constraints_json || "{}");
            const actualChangedFiles = filterGeneratedFiles(workspace.changedFiles || [], constraints).changedFiles;
            const normalizedReport = { changedFiles: actualChangedFiles };
            validateWorkerOutput(taskReport.status, actualChangedFiles, constraints);
            const patch = workerPatch(run);
            const outputPath = join(run.async_dir || "", `output-${run.child_index || 0}.log`);
            validateWorkerPatchArtifact(patch, outputPath, normalizedReport);
            assertReviewFixChangedPatch(run, readFileSync(patch), taskReport.no_change_justification);
            if (run.stage === "autofix" && statSync(patch).size === 0) throw new Error("autofix made no repository changes");
            if (statSync(patch).size > 0) execFileSync("git", ["apply", "--check", patch], { cwd: this.cwd, stdio: "pipe" });
          }
        }
        if (taskReport.status === "escalated") {
          // Fail-closed escalation (GAP-138): persist the structured report bound to the
          // run's TIP lineage, block the run, release the claim, and stop — never retry
          // or continue downstream while the escalation is open.
          const saved = execPic(["workflow", "escalation-save", run.task_id, "--pipeline-run-id", run.id, "--report-json", JSON.stringify(taskReport.escalation)], this.cwd);
          if (saved.error) {
            // GAP-141: never lose the escalation intent to a tooling mismatch (e.g., a
            // stale installed pic predating escalation-save). Persist the run blocked
            // with the full structured payload and completion report so the owner sees
            // the actual question instead of only the subcommand error.
            const reason = `escalation persistence failed (${saved.error}); worker escalation payload preserved below`;
            const result = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "blocked", "--error", reason, "--result-json", JSON.stringify({ ...pipelineFailureResult(reason), blocker: taskReport.escalation?.summary || reason, completion_report: taskReport.markdown, escalation: taskReport.escalation })], this.cwd);
            if (result.error) throw new Error(result.error);
            checkpoint(run, "advanced", this.cwd);
            this.notifyBlockedAttempt(run, `${reason}\n\n${taskReport.markdown}`);
            return;
          }
          checkpoint(run, "advanced", this.cwd);
          this.notifyBlockedAttempt(run, `worker escalated ${taskReport.escalation.level}: ${taskReport.escalation.summary || "decision required before progress can resume"}`);
          return;
        }
        if (taskReport.status !== "done") {
          const reason = taskReport.blocker || `worker reported ${taskReport.status}`;
          execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "blocked", "--error", reason, "--result-json", JSON.stringify({ ...pipelineFailureResult(reason), blocker: reason, completion_report: taskReport.markdown, ...(taskReport.failure_metadata ? { failure_metadata: taskReport.failure_metadata } : {}) })], this.cwd);
          checkpoint(run, "advanced", this.cwd);
          this.notifyBlockedAttempt(run, reason);
          return;
        }
      }
      if (run.stage === "review") {
        assertReviewBaseCurrent(run, this.cwd);
        const review = parseReviewReport(outputFor(run));
        const reviewNotes = review.findings.length ? `${review.notes}\n\n${review.findings.map((finding) => `- ${finding}`).join("\n")}` : review.notes;
        const result = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "completed", "--result-json", JSON.stringify({ subagent_state: status.state, review_status: review.status, notes: review.notes, findings: review.findings, owner_approval_required: review.ownerApprovalRequired, candidate_run_id: run.candidate_run_id, candidate_patch_hash: run.candidate_patch_hash })], this.cwd);
        if (result.error) throw new Error(result.error);
        reviewCompleted = true;
        const update = execPic(["work-item", "review", run.task_id, review.status, "--notes", reviewNotes, "--pipeline-run-id", run.id], this.cwd);
        if (update.error) throw new Error(update.error);
        if (review.status === "passed") {
          const workerRun = this.integrateReviewedCandidate(run.task_id, run);
          this.promoteReviewedCandidate(workerRun);
        }
        checkpoint(run, "advanced", this.cwd);
        await this.advance(run.task_id);
        return;
      }
      if (run.stage === "scan") {
        const output = outputFor(run);
        const result = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "completed", "--result-json", JSON.stringify({ subagent_state: status.state, scan_report: output })], this.cwd);
        if (result.error) throw new Error(result.error);
        const handoffId = this.handoffs.put("scan", run.task_id, output);
        this.pi.sendUserMessage(`Scan evidence ready for contractor synthesis for ${run.task_id}. Load ephemeral handoff ${handoffId}, validate every section against source, resolve contradictions, and save one canonical Scan Report as structured XML matching this schema:\n\n${CANONICAL_SCAN_REPORT_XML_FORMAT}\n\nDo not format owner-facing Markdown; the task_manager tool renders the saved XML deterministically. Otherwise reject the scan. The handoff expires five minutes after first load and is never persisted.`, { deliverAs: "followUp" });
        checkpoint(run, "advanced", this.cwd);
        return;
      }
      if (isPlanningStage(run.stage)) {
        const result = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "completed", "--result-json", JSON.stringify({ subagent_state: status.state })], this.cwd);
        if (result.error) throw new Error(result.error);
        this.publishPlanningHandoff(run, outputFor(run));
        checkpoint(run, "advanced", this.cwd);
        return;
      }
      const result = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "completed", "--result-json", JSON.stringify({ subagent_state: status.state })], this.cwd);
      if (result.error) throw new Error(result.error);
      const data = this.showItem(run.task_id);
      const parentId = data.work_item?.parent_id;
      if (parentId) this.roots.add(parentId);
      if (isMutationStage(run.stage)) {
        await this.continueWorkerGroup(run);
        return;
      }
      checkpoint(run, "advanced", this.cwd);
      await this.advance(run.task_id, parentId);
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      if (reviewCompleted) {
        this.notifyBlockedAttempt(run, reason);
        return;
      }
      const persisted = isMutationStage(run.stage) ? this.pipelineRuns(run.task_id).find((entry) => entry.id === run.id) : undefined;
      if (persisted?.status === "completed" && persisted.artifact_saved_at) {
        this.notifyBlockedAttempt(persisted, reason);
        return;
      }
      execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "blocked", "--error", reason, "--result-json", JSON.stringify(pipelineFailureResult(reason))], this.cwd);
      if (isMutationStage(run.stage)) await this.continueWorkerGroup(run);
      else this.notifyBlockedAttempt(run, reason);
    }
  }

  private async continueWorkerGroup(run: PipelineRun): Promise<void> {
    const task = this.showItem(run.task_id);
    const parentId = task.work_item?.parent_id;
    const parent = parentId ? this.showItem(parentId) : null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    const taskIds = parentId ? (parent?.children || []).map((child: any) => child.id) : [run.task_id];
    const taskRuns = new Map<string, PipelineRun[]>();
    const group = taskIds.flatMap((taskId: string) => {
      const taskData = normalizePipelineData(this.showItem(taskId));
      if (taskData?.work_item?.status === "done") return [];
      const runs = execPic(["workflow", "pipeline-runs", taskId], this.cwd);
      if (!Array.isArray(runs)) return [];
      taskRuns.set(taskId, runs);
      const latest = workerIntegrationCandidate(runs) || runs.find((entry: PipelineRun) => isMutationStage(entry.stage) && !entry.advanced_at);
      return latest ? [latest] : [];
    });
    if (group.some((entry: PipelineRun) => entry.status === "claimed" || entry.status === "running")) return;

    if (group.some((entry: PipelineRun) => entry.status !== "completed")) {
      for (const entry of group.filter((entry: PipelineRun) => entry.status !== "completed")) {
        checkpoint(entry, "advanced", this.cwd);
        this.notifyBlockedAttempt(entry, entry.error || `worker pipeline ended with status ${entry.status || "unknown"}`);
      }
      return;
    }

    for (const entry of group) {
      const report = parseTaskCompletionReport(outputFor(entry));
      if (report.status === "escalated") throw new Error("escalated run cannot be integrated");
      const data = normalizePipelineData(this.showItem(entry.task_id));
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
      const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
      const constraints = JSON.parse(activePack?.constraints_json || "{}");
      const workspace = JSON.parse(readFileSync(join(entry.async_dir || "", "workspace.json"), "utf8"));
      const actualChangedFiles = filterGeneratedFiles(workspace.changedFiles || [], constraints).changedFiles;
      validateWorkerOutput(report.status, actualChangedFiles, constraints);
      const patch = workerPatch(entry);
      if (!entry.artifact_saved_at) {
        if (!existsSync(patch)) throw new Error(`worker patch missing: ${patch}`);
        checkpoint(entry, "artifact_saved", this.cwd, patch);
      }

    }

    for (const entry of group) {
      for (const sibling of taskRuns.get(entry.task_id) || []) {
        if (isMutationStage(sibling.stage) && sibling.id !== entry.id && !sibling.advanced_at) checkpoint(sibling, "advanced", this.cwd);
      }
    }

    for (const entry of group) await this.launchGroup("review", [entry.task_id]);
    for (const entry of group) checkpoint(entry, "advanced", this.cwd);
  }

  private integrateReviewedCandidate(taskId: string, reviewRun: PipelineRun): PipelineRun {
    const data = normalizePipelineData(this.showItem(taskId));
    assertRunContractCurrent(data, reviewRun);
    const workerRun = this.pipelineRuns(taskId).find((candidate: PipelineRun) => candidate.id === reviewRun.candidate_run_id && isMutationStage(candidate.stage));
    if (!workerRun?.artifact_saved_at || !workerRun.integrated_patch_path || !workerRun.integrated_patch_hash) throw new Error("review passed without validated candidate patch evidence");
    if (reviewRun.candidate_patch_hash !== workerRun.integrated_patch_hash) throw new Error("review passed for a different candidate patch");
    if (!workerRun.integrated_at) {
      withGitWriteLock(this.cwd, () => {
        const patch = workerRun.integrated_patch_path;
        if (!existsSync(patch)) throw new Error(`candidate patch missing: ${patch}`);
        const actualHash = createHash("sha256").update(readFileSync(patch)).digest("hex");
        if (actualHash !== workerRun.integrated_patch_hash) throw new Error("candidate patch changed after review");
        const commitMessage = `task-system: integrate reviewed worker ${workerRun.subagent_run_id || workerRun.id}`;
        finalizeReviewedIntegration({
          patch,
          cwd: this.cwd,
          commitMessage,
          integrated: false,
          checkpoint: () => checkpoint(workerRun, "integrated", this.cwd),
        });
      });
    }
    return workerRun;
  }

  private promoteReviewedCandidate(run: PipelineRun): void {
    const raw = this.showItem(run.task_id);
    const data = normalizePipelineData(raw);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    if ((data.completion_reports || []).some((report: any) => report.status === "done" && report.pipeline_run_id === run.id)) return;
    const report = parseTaskCompletionReport(outputFor(run));
    if (report.status === "escalated") throw new Error("escalated run cannot be integrated");
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
    const constraints = JSON.parse(activePack?.constraints_json || "{}");
    const workspace = JSON.parse(readFileSync(join(run.async_dir || "", "workspace.json"), "utf8"));
    const changedFiles = filterGeneratedFiles(workspace.changedFiles || [], constraints).changedFiles;
    if (raw.work_item) {
      const saved = execPic(["work-item", "completion-save", run.task_id, "done", "--pipeline-run-id", run.id, "--summary", "Reviewed implementation completed", "--report-markdown", report.markdown], this.cwd);
      if (saved.error) throw new Error(saved.error);
    } else {
      // The escalated guard above makes this narrowing safe: escalated runs never integrate.
      const integrationStatus = report.status as "done" | "partial" | "blocked";
      saveWorkerReport(run, this.cwd, { status: integrationStatus, markdown: report.markdown }, { changedFiles, diffSummary: "Reviewed implementation completed" });
    }
  }

  private async advance(taskId: string, parentId?: string): Promise<void> {
    const raw = this.showItem(taskId);
    const data = raw.work_item ? normalizePipelineData(raw) : withInheritedParentWorkflowArtifacts(raw, this.cwd);
    const next = nextPipelineStage(data, this.pipelineRuns(taskId));
    if (next) {
      if (isMutationStage(next)) assertCleanGit(this.cwd);
      await this.launchGroup(next, [taskId]);
      return;
    }
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
    if (!activePack) return;
    const verificationBlock = pipelineVerificationBlockReason(data);
    if (verificationBlock) throw new Error(verificationBlock);
    const doneReports = activePackDoneReports(data, activePack);
    if (doneReports.length) {
      if (!latestVerificationAfter(data, doneReports[0])) this.pi.sendUserMessage(buildTaskVerifyPrompt(data), { deliverAs: "followUp" });
      return;
    }
    const done = execPic(["work-item", "status", taskId, "done"], this.cwd);
    if (done.error) throw new Error(done.error);
    // Close-out transition: point the contractor at the next dependency-ready
    // work so leaf completion flows straight into the next increment.
    if (parentId && !this.parentHasActiveRuns(parentId)) {
      const parent = this.showItem(parentId);
      const readyIds = this.readyLeafIds(parent).filter((id) => id !== taskId);
      const nextUp = readyIds.length
        ? `${readyIds.length} dependency-ready leaf(es) will launch next: ${readyIds.slice(0, 5).join(", ")}${readyIds.length > 5 ? ", …" : ""}`
        : "No dependency-ready leaves remain; check `work-item workflow-status` for the aggregate's verification stage.";
      this.pi.sendUserMessage(`${taskId} is done. ${nextUp}`, { deliverAs: "followUp" });
    }

    if (!parentId) return;
    if (this.parentHasActiveRuns(parentId)) return;
    await this.scheduleReady(parentId);
  }

  private async resumePending(run: PipelineRun): Promise<void> {
    if (run.advanced_at) return;
    if (isMutationStage(run.stage)) {
      await this.continueWorkerGroup(run);
      return;
    }
    const data = this.showItem(run.task_id);
    if (run.status !== "completed") {
      checkpoint(run, "advanced", this.cwd);
      return;
    }
    if (run.stage === "review") {
      const outcome = persistedReviewOutcome(run);
      if (!outcome) throw new Error("completed review is missing its durable verdict");
      const candidate = this.pipelineRuns(run.task_id).find((entry: PipelineRun) => entry.id === outcome.candidateRunId && isMutationStage(entry.stage));
      if (!candidate || candidate.status !== "completed" || !candidate.artifact_saved_at || !candidate.integrated_patch_path || candidate.integrated_patch_hash !== outcome.candidatePatchHash) {
        throw new Error("completed review references invalid candidate lineage");
      }
      const reviewData = this.showItem(run.task_id);
      if (reviewData.work_item?.review_status !== outcome.status) {
        const notes = outcome.findings.length ? `${outcome.notes}\n\n${outcome.findings.map((finding) => `- ${finding}`).join("\n")}` : outcome.notes;
        const update = execPic(["work-item", "review", run.task_id, outcome.status, "--notes", notes, "--pipeline-run-id", run.id], this.cwd);
        if (update.error) throw new Error(update.error);
      }
      if (outcome.status === "passed") {
        const workerRun = this.integrateReviewedCandidate(run.task_id, run);
        this.promoteReviewedCandidate(workerRun);
      }
    }
    if (isPlanningStage(run.stage)) {
      this.publishPlanningHandoff(run, outputFor(run));
      checkpoint(run, "advanced", this.cwd);
      return;
    }
    const parentId = data.work_item?.parent_id;
    checkpoint(run, "advanced", this.cwd);
    await this.advance(run.task_id, parentId);
  }

  private publishPlanningHandoff(run: PipelineRun, output: string): void {
    // Blueprint draft constraint: planner output is not canonical until the
    // Contractor checkpoint and owner promotion complete.
    const payload = run.stage === "blueprint"
      ? JSON.stringify(loadLatestBlueprintDraft(this.cwd, run.task_id))
      : output;
    const data = this.showItem(run.task_id);
    const profile = resolvePlanProfile(data);
    const predecessor = predecessorCheckpointFor(data, run.stage, profile.stages);
    const envelope = buildPlanningHandoffXml({
      work_item_id: run.task_id,
      stage: run.stage,
      predecessor_checkpoint: predecessor?.artifact_id ? String(predecessor.artifact_id) : "",
      profile_version: String(profile.version),
      profile_hash: profile.contentHash,
    }, payload);
    const handoffId = this.handoffs.put(run.stage, run.task_id, envelope);
    const action = run.stage === "rri"
      ? "conduct the owner interview, persist confirmed requirements and decisions, then save the owner-confirmed RRI artifact"
      : run.stage === "blueprint"
        ? "load the temporary draft with load_blueprint_draft, validate its JSON content, and use its draft_id for review_blueprint_checkpoint; revise through save_blueprint_draft if needed, then present the checked draft for owner approval; do not call save_work_item_artifact"
        : `validate the result, save the ${run.stage} artifact, and present it for owner approval`;
    this.pi.sendUserMessage(`${run.stage.toUpperCase()} analysis ready for ${run.task_id}. Load ephemeral handoff ${handoffId}, ${action}. The handoff expires five minutes after first load and is never persisted.`, { deliverAs: "followUp" });
  }


  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  private pipelineRuns(taskId: string): any[] {
    const runs = execPic(["workflow", "pipeline-runs", taskId], this.cwd);
    return parsePipelineRuns(runs);
  }

  private parentHasActiveRuns(parentId: string): boolean {
    const parent = this.showItem(parentId);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    const childIds = (parent.children || []).map((child: any) => child.id);
    const ids = new Set([parentId, ...childIds]);
    const active = execPic(["workflow", "pipeline-active"], this.cwd);
    return Array.isArray(active) && active.some((run: PipelineRun) => ids.has(run.task_id));
  }
}

export function registerPipelineScheduler(pi: ExtensionAPI): PipelineScheduler {
  const scheduler = new PipelineScheduler(pi);
  if (process.env.PI_TASK_PARENT_RUN_ID) return scheduler;
  pi.on("session_start", (_event, ctx) => scheduler.startSession(ctx));
  pi.on("session_shutdown", () => scheduler.stopSession());
  pi.registerCommand("task-pipeline", {
    description: "Start, inspect, or stop an asynchronous task DAG pipeline",
    async handler(args, ctx) {
      const [action = "status", taskId] = args.trim().split(/\s+/);
      if (!taskId || !["status", "stop"].includes(action)) {
        ctx.ui.notify("Usage: /task-pipeline status|stop <task-id>", "warning");
        return;
      }
      try {
        const result = action === "stop"
          ? await scheduler.stop(taskId, ctx)
          : scheduler.status(taskId, ctx);
        ctx.ui.notify(JSON.stringify(result), "info");
      } catch (error) {
        ctx.ui.notify(error instanceof Error ? error.message : String(error), "error");
      }
    },
  });
  pi.registerTool({
    name: "ephemeral_handoff",
    label: "Ephemeral Handoff",
    description: "Read temporary Scout, RRI, or planning evidence from memory. Evidence is never persisted.",
    parameters: Type.Object({ id: Type.String(), work_item_id: Type.String() }),
    async execute(_id, params) {
      const entry = scheduler.handoffs.get(params.id, params.work_item_id);
      if (!entry) return { content: [{ type: "text", text: "Error: ephemeral handoff missing or expired; rerun the producing stage" }], details: { id: params.id, workflow: "", work_item_id: params.work_item_id, expires_at: 0 }, isError: true };
      return { content: [{ type: "text", text: entry.payload }], details: { id: entry.id, workflow: entry.workflow, work_item_id: entry.workItemId, expires_at: entry.expiresAt } };
    },
  });
  pi.registerTool({
    name: "task_pipeline",
    label: "Task Pipeline",
    description: "Inspect or stop durable asynchronous Work Item pipelines. Start work through task_manager work_on_work_item or /task work.",
    parameters: Type.Object({ action: Type.Union([Type.Literal("status"), Type.Literal("stop")]), task_id: Type.String() }),
    async execute(_id, params, _signal, _update, ctx) {
      try {
        const result = params.action === "stop"
          ? await scheduler.stop(params.task_id, ctx)
          : scheduler.status(params.task_id, ctx);
        const text = params.action === "stop" ? formatPipelineStop(result) : formatPipelineStatus(result);
        return { content: [{ type: "text", text }], details: result };
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        return { content: [{ type: "text", text: `Error: ${message}` }], details: { error: message }, isError: true };
      }
    },
  });
  return scheduler;
}
