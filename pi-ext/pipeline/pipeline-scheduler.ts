import type { ExtensionAPI, ExtensionContext } from "@mariozechner/pi-coding-agent";
import { Type } from "typebox";
import { execPic } from "../core/cli-helpers.ts";
import { EphemeralHandoffStore } from "../core/ephemeral-handoffs.ts";
import { cleanupOrphanedSubagentWorktrees } from "../subagent/runner.ts";
import { parsePicShow, type PicShowDocument } from "./pic-show.ts";
import { parsePipelineRuns, type PipelineRun } from "./pipeline-types.ts";
import { pipelineFailureResult } from "./worker-validation.ts";
import { canonicalReadyLeafIds } from "./stage-resolution.ts";
import { assertCleanGit } from "./integration.ts";
import { checkpoint, statusFor, formatPipelineStatus, formatPipelineStop } from "./run-helpers.ts";
import { scheduleReady } from "./advance.ts";
import { finish, resumePending } from "./finish.ts";
import { startReadyBatch as startReadyBatchOf, status as statusOf, stop as stopOf, dryRun as dryRunOf, mergeAggregate as mergeAggregateOf } from "./commands.ts";
import { completeDispatch as completeDispatchOf } from "./dispatch-complete.ts";
import { bindPipelineDispatch, findPipelineDispatch as findPipelineDispatchOf, listPipelineDispatches as listPipelineDispatchesOf, type PipelineDispatchReport } from "./pipeline-dispatch.ts";
import type { SchedulerDeps } from "./scheduler-context.ts";

export * from "./rri-t.ts";
export * from "./report-parsing.ts";
export * from "./integration.ts";
export * from "./worker-validation.ts";
export * from "./corrections.ts";
export * from "./stage-prompts.ts";
export * from "./instruction-pack-xml.ts";
export * from "./stage-resolution.ts";
export { formatPipelineStatus, formatPipelineStop } from "./run-helpers.ts";

export class PipelineScheduler {
  readonly handoffs = new EphemeralHandoffStore();

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
  // Durable worker worktree constraint (RLB-GAP-001): failure modes of retained
  // pack worktrees, keyed by worktree key (instruction pack id), consumed by the
  // next launch of the same pack as the resume preamble source.
  private retainedFailures = new Map<string, string>();

  constructor(pi: ExtensionAPI) { this.pi = pi; }

  private cwd = "";

  /** Facade consumed by the split scheduler operation modules. */
  private get deps(): SchedulerDeps {
    return {
      cwd: this.cwd,
      showItem: (id) => this.showItem(id),
      pipelineRuns: (taskId) => this.pipelineRuns(taskId),
      sendUserMessage: (text) => this.pi.sendUserMessage(text, { deliverAs: "followUp" }),
      notifyBlockedAttempt: (run, reason) => this.notifyBlockedAttempt(run, reason),
      addRoot: (id) => this.roots.add(id),
      retainedFailures: this.retainedFailures,
      handoffs: this.handoffs,
    };
  }

  private queueReconcile(): void {
    setImmediate(() => { void this.reconcileSafely(); });
  }

  /** Pending Agent-tool dispatches awaiting a contractor spawn + bind, enriched
   *  with the run's live lifecycle state (status, error, integration fields) —
   *  the zero-sqlite contractor surface (RLB-GAP-002). */
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  listDispatches(): any[] {
    return listPipelineDispatchesOf(this.cwd).map((dispatch) => {
      let run: PipelineRun | undefined;
      try {
        run = this.pipelineRuns(dispatch.taskId).find((entry) => entry.id === dispatch.runId);
      } catch {
        // Work item no longer readable: launch-time dispatch facts remain.
      }
      return { ...dispatch, run: run ?? null };
    });
  }

  /** Bind an Agent tool id to a dispatched run — the hard gate: empty ids are rejected. */
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  bindDispatch(runId: string, agentId: string): any {
    const dispatch = findPipelineDispatchOf(this.cwd, runId);
    if (!dispatch) throw new Error(`no pending pipeline dispatch for run ${runId}`);
    bindPipelineDispatch(dispatch, agentId);
    const bound = execPic(["workflow", "pipeline-bind", dispatch.runId, dispatch.leaseToken, agentId.trim(), "--async-dir", dispatch.asyncDir, "--child-index", "0"], this.cwd);
    if (bound.error) throw new Error(bound.error);
    return bound;
  }

  async completeDispatch(runId: string, report: PipelineDispatchReport): Promise<void> {
    await completeDispatchOf(this.deps, () => this.queueReconcile(), runId, report);
  }

  private async reconcileSafely(): Promise<void> {
    try {
      await this.reconcile();
    } catch (error) {
      if (this.context) this.reportError(error, this.context);
    }
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
    // legacy planning stages are disabled (owner decision 2026-09-07): the
    // scheduler never launches scan/rri/vision/blueprint/contracts/task_graph
    // because nextPipelineStage can no longer return one. A stale planning
    // next_stage on an imported parent (pre-lean era) is therefore inert:
    // scheduling proceeds to the ready lean leaves instead of hard-failing
    // child close-out (T004 smoke, 2026-09-07). Contractor-owned planning
    // (rri/contracts/vision prompts) is reached via work_on_work_item in
    // api/tool.ts, never through the spawn scheduler.
    assertCleanGit(ctx.cwd);
    await this.reconcile();
    return await scheduleReady(this.deps, rootTaskId);
  }

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  async startReadyBatch(ctx: ExtensionContext): Promise<any> {
    this.cwd = ctx.cwd;
    this.context = ctx;
    this.lastError = "";
    assertCleanGit(ctx.cwd);
    await this.reconcile();
    return startReadyBatchOf(this.deps, ctx);
  }

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  dryRun(rootTaskId: string, ctx: ExtensionContext): any {
    return dryRunOf(rootTaskId, ctx);
  }

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  status(taskId: string, ctx: ExtensionContext): any {
    return statusOf(taskId, ctx, this.lastError);
  }

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  async stop(taskId: string, ctx: ExtensionContext): Promise<any> {
    return stopOf(taskId, ctx);
  }

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  async mergeAggregate(workItemId: string, ctx: ExtensionContext): Promise<any> {
    return mergeAggregateOf(workItemId, ctx);
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
        this.integrating = this.integrating.then(() => finish(this.deps, run, status)).catch(() => undefined);
        await this.integrating;
      }
      const pending = execPic(["workflow", "pipeline-pending"], this.cwd);
      if (Array.isArray(pending)) {
        for (const run of pending as PipelineRun[]) await resumePending(this.deps, run);
      }
    } finally {
      this.reconciling = false;
    }
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  private pipelineRuns(taskId: string): any[] {
    const runs = execPic(["workflow", "pipeline-runs", taskId], this.cwd);
    return parsePipelineRuns(runs);
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
