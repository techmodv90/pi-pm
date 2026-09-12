import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { appendFileSync, closeSync, existsSync, mkdirSync, mkdtempSync, openSync, readdirSync, readFileSync, readSync, renameSync, rmSync, statSync, writeFileSync } from "node:fs";
import { randomUUID } from "node:crypto";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import type { SubagentHandle, SubagentResult, SubagentSpec, SubagentUpdate, SubagentUsage } from "./types.ts";
import { agentRunTracker } from "./tracker.ts";
import { createHerdrPanel, type HerdrPanelHandle } from "./herdr-panel.ts";
import { resolveSkillDirectories, resolvedSkillNames } from "./skills.ts";
import { buildPiInvocation, getAppendSystemPromptPaths, parseJsonEvent } from "./spawn-utils.ts";
import { gitStatusOrUndefined, gitText, updateUsage, workspaceDiagnostics } from "./workspace.ts";
import { classifyRunnerTransientFault, RUNNER_TRANSIENT_RETRIES, runnerTransientBackoffMs } from "./transient.ts";
import { resetSubagentWorktreeToCandidate } from "./worktree.ts";

export type SpawnFunction = typeof spawn;
export const MANAGED_WORKER_DEADLINE_MS = 30 * 60 * 1000;
export const REVIEW_DEADLINE_MS = 20 * 60 * 1000;
// Review-fix constraint: allow complex rejected-candidate repairs more wall time than first-pass workers.
export const REVIEW_FIX_DEADLINE_MS = 45 * 60 * 1000;
// Watchdog constraint: a managed child silent this long is wedged (provider hang),
// not thinking — pi JSON mode emits stdout events continuously during live turns.
export const RUNNER_INACTIVITY_KILL_MS = 15 * 60 * 1000;
// Grace between "exit" and drain-or-finalize; long enough for normal stdio
// flush, short enough to beat the tracker's 1s dead-pid failure sync.
export const EXIT_FINALIZE_GRACE_MS = 500;
const WORKER_WRAP_UP_MS = 5_000;
const defaultHerdrPanel = createHerdrPanel();
const methodologiesDirectory = fileURLToPath(new URL("../methodologies", import.meta.url));

const emptyUsage = (): SubagentUsage => ({ input: 0, output: 0, cacheRead: 0, cacheWrite: 0, cost: 0, contextTokens: 0, turns: 0 });
const READ_ONLY_AGENTS = new Set(["task-scout", "task-reviewer", "rri-persona"]);

// In-claim transient retry constraint: retry at most RUNNER_TRANSIENT_RETRIES
// times inside the same claim (same runId, no numbered pipeline attempt
// consumed), resetting the isolated worktree between attempts. On exhaustion the
// handled result is tagged failure_code=transient_provider.

export { buildPiInvocation, getAppendSystemPromptPaths, parseJsonEvent, finalAssistantText } from "./spawn-utils.ts";
export type { SubagentSpec, SubagentHandle, SubagentResult, SubagentUpdate, SubagentUsage, SubagentWorkspace } from "./types.ts";
export {
  cleanupOrphanedSubagentWorktrees,
  ORPHANED_WORKTREE_MAX_AGE_MS,
  prepareSubagentWorktree,
  removeSubagentWorktree,
  resetSubagentWorktreeToCandidate,
  retainWorktreeForResume,
} from "./worktree.ts";
export {
  classifyRunnerTransientFault,
  type RunnerTransientFault,
  RUNNER_TRANSIENT_RETRIES,
  RUNNER_TRANSIENT_BACKOFF_MS,
  runnerTransientBackoffMs,
} from "./transient.ts";

export function assertManagedAcceptance(spec: SubagentSpec): void {
  if (!spec.stage) return;
  const required = spec.stage === "review" ? "attested" : "checked";
  if (spec.acceptance !== required) throw new Error(`${spec.stage} subagent requires acceptance ${required}`);
}

export function startSubagent(spec: SubagentSpec, onUpdate?: (update: SubagentUpdate) => void, spawnProcess: SpawnFunction = spawn): SubagentHandle {
  assertManagedAcceptance(spec);
  const id = spec.runId || randomUUID();
  const tracker = spec.tracker || agentRunTracker;
  const result: SubagentResult = { runId: id, agent: spec.agent.name, task: spec.task, exitCode: -1, messages: [], stderr: "", usage: emptyUsage() };
  const promptDir = mkdtempSync(join(tmpdir(), "pi-task-subagent-"));
  let runCwd = spec.cwd;
  const readOnlyStatusBefore = spec.isolation !== "worktree" && READ_ONLY_AGENTS.has(spec.agent.name)
    ? gitStatusOrUndefined(spec.cwd)
    : undefined;
  if (spec.isolation === "worktree") {
    runCwd = spec.preparedWorktree || join(spec.cwd, ".pi", "worktrees", id);
    if (!spec.preparedWorktree) {
      mkdirSync(join(spec.cwd, ".pi", "worktrees"), { recursive: true, mode: 0o700 });
      execFileSync("git", ["worktree", "add", "-b", `pi-agent-${id}`, runCwd, "HEAD"], { cwd: spec.cwd, stdio: "pipe" });
    }
    if (!spec.preparedWorktree && spec.initialPatchPath && statSync(spec.initialPatchPath).size > 0) {
      execFileSync("git", ["apply", "--check", spec.initialPatchPath], { cwd: runCwd, stdio: "pipe" });
      execFileSync("git", ["apply", spec.initialPatchPath], { cwd: runCwd, stdio: "pipe" });
    }
    result.workspace = workspaceDiagnostics(runCwd);
    runCwd = result.workspace.assignedWorktree;
  }
  const promptPath = join(promptDir, "system-prompt.md");
  writeFileSync(promptPath, spec.agent.systemPrompt, { encoding: "utf8", mode: 0o600 });
  const skillDirectories = spec.skillDirectories ?? resolveSkillDirectories({ baselineSkills: spec.agent.skills || [], skillFamilies: spec.skillFamilies || [], cwd: spec.cwd });
  const args = ["--mode", "json", "-p", "-ne", "--extension", fileURLToPath(new URL("../index.ts", import.meta.url))];
  if (spec.sessionPath) {
    // Worker session binding (GAP-137): pin the conversation host-side so a review-fix
    // relaunch continues it. Lineage is the path key (instruction pack ID), so a retired
    // TIP can never resume; a corrupt file falls back to fresh creation.
    const sessionDir = dirname(spec.sessionPath);
    mkdirSync(sessionDir, { recursive: true, mode: 0o700 });
    try {
      const runsRoot = dirname(sessionDir);
      for (const entry of readdirSync(runsRoot)) {
        const sibling = join(runsRoot, entry);
        if (sibling === sessionDir) continue;
        if (Date.now() - statSync(sibling).mtimeMs > 24 * 60 * 60 * 1000) rmSync(sibling, { recursive: true, force: true });
      }
    } catch {}
    let resumable = false;
    try {
      const fd = openSync(spec.sessionPath, "r");
      const head = Buffer.alloc(1);
      readSync(fd, head, 0, 1, 0);
      closeSync(fd);
      resumable = head[0] === 0x7b; // JSONL session files start with '{'
      if (resumable) {
        // Session-resume cwd guard (GAP-140): pi validates the resumed session
        // against its recorded working directory; a GC'd temp worktree would kill
        // the worker at load on every retry. Set the stale file aside instead.
        const firstLineEnd = (() => {
          const buf = Buffer.alloc(4096);
          const fd2 = openSync(spec.sessionPath, "r");
          const n = readSync(fd2, buf, 0, 4096, 0);
          closeSync(fd2);
          return buf.subarray(0, n).indexOf(0x0a);
        })();
        if (firstLineEnd > 0) {
          const header = JSON.parse(readFileSync(spec.sessionPath, { encoding: "utf8", flag: "r" }).slice(0, firstLineEnd));
          if (typeof header?.cwd === "string" && !existsSync(header.cwd)) {
            renameSync(spec.sessionPath, `${spec.sessionPath}.stale-${Date.now()}`);
            resumable = false;
          }
        }
      }
    } catch {}
    if (!resumable && existsSync(spec.sessionPath)) rmSync(spec.sessionPath, { force: true });
    args.push("--session", spec.sessionPath);
  } else args.push("--no-session");
  if (spec.agent.model) args.push("--model", spec.agent.model);
  if (spec.agent.thinking) args.push("--thinking", spec.agent.thinking);
  if (spec.agent.tools?.length) args.push("--tools", spec.agent.tools.join(","));
  for (const directory of skillDirectories) args.push("--skill", directory);
  for (const path of getAppendSystemPromptPaths(spec.agent.name, spec.agent.systemPrompt, promptPath)) args.push("--append-system-prompt", path);
  // Durable worker worktree constraint (RLB-GAP-001): a run resuming a retained
  // pack worktree gets a mandatory preamble describing the prior failure mode
  // and the worktree's current state, so the child orients before continuing.
  let resumePreamble = "";
  if (spec.durableWorktreeKey) {
    const runDir = spec.preparedWorktree || runCwd;
    const isWorktreeCwd = runDir !== spec.cwd && runDir.startsWith(spec.cwd);
    let statusSample = "(empty)";
    try {
      statusSample = execFileSync("git", ["status", "--short"], { cwd: runDir, encoding: "utf8" }).trim().split("\n").slice(0, 30).join("\n") || "(clean)";
    } catch {}
    const retained = Boolean(spec.resumeFailureMode);
    resumePreamble = retained
      ? `RESUME: A prior attempt of this task died in the retained worktree (${spec.resumeFailureMode}). The worktree still holds that attempt's uncommitted partial work. Re-orient with the git status sample below, verify the partial work against the task, and continue from there — do not start over from zero.\nCurrent worktree git status:\n${statusSample}\nYou have a fresh deadline budget for this attempt.`
      : "FRESH START: No prior attempt survived; the worktree is newly created. Implement the task from the current state.\nCurrent worktree git status:\n" + statusSample;
    if (isWorktreeCwd) resumePreamble += "\nWORKTREE DISCIPLINE: git refs/stash is shared across linked worktrees — NEVER run git stash or git stash pop here; your uncommitted work is your durable state. Do not create commits. Do not touch paths outside this worktree.";
  }
  const baseTask = resumePreamble ? `${resumePreamble}\n\n${spec.task}` : spec.task;
  const patchedTask = spec.initialPatchPath
    ? spec.stage === "review"
      ? `${baseTask}\n\nCANDIDATE ATTESTATION: The bound candidate patch has been applied to this isolated worktree. Review this worktree, not the parent checkout.`
      : `${baseTask}\n\nREVIEW-FIX RUN: The rejected candidate patch has been applied to this worktree. You must modify the worktree to address the review findings and produce a non-empty patch different from the rejected candidate. A no-op completion is invalid.`
    : baseTask;
  args.push(`Task: ${patchedTask}`);
  const invocation = buildPiInvocation(args);
  let child: ChildProcess | undefined;
  const herdrPanel = spec.herdrPanel ?? defaultHerdrPanel;
  const herdrLogPath = join(runCwd, ".pi-subagents", "runs", id, "herdr.log");
  let herdrHandle: HerdrPanelHandle | undefined;
  let settled = false;
  let stopChild = () => {};
  // Test/ops override hooks: per-spec caps so unit tests need not wait real minutes.
  const stageDeadlineMs = (spec as any).deadlineMs ?? (spec.stage === "review" ? REVIEW_DEADLINE_MS : spec.initialPatchPath ? REVIEW_FIX_DEADLINE_MS : MANAGED_WORKER_DEADLINE_MS);
  const inactivityKillMs = (spec as any).inactivityKillMs ?? RUNNER_INACTIVITY_KILL_MS;
  let deadlineTimer: NodeJS.Timeout | undefined;
  let activityTimer: NodeJS.Timeout | undefined;
  let exitGraceTimer: NodeJS.Timeout | undefined;
  let abortListener: (() => void) | undefined;
  // Tracker state must outlive worktree cleanup: persist under the host repo,
  // not the worktree cwd that gets removed when the run completes.
  tracker.start({ runId: id, agent: spec.agent.name, task: spec.task, cwd: spec.cwd, stage: spec.stage, taskId: spec.taskId, stop: () => stopChild() });
  const appendHerdrLog = (message: string) => {
    if (!herdrHandle) return;
    try { appendFileSync(herdrLogPath, `${message.replace(/\s+/g, " ").trim().slice(0, 4000)}\n`, { encoding: "utf8", mode: 0o600 }); } catch {}
  };
  const resultPromise = new Promise<SubagentResult>((resolve) => {
    const finish = (exitCode: number, stopReason?: string) => {
      if (settled) return;
      settled = true;
      tracker.observeLifecycle(id, "finalizing");
      result.exitCode = exitCode;
      result.stopReason = stopReason || result.stopReason || (exitCode === 0 ? "end" : "error");
      if (abortListener) spec.signal?.removeEventListener("abort", abortListener);
      if (deadlineTimer) clearTimeout(deadlineTimer);
      if (activityTimer) clearTimeout(activityTimer);
      if (exitGraceTimer) clearTimeout(exitGraceTimer);
      if (result.workspace) {
        try {
          result.workspace.statusAfter = gitText(result.workspace.assignedWorktree, ["status", "--short"]);
          result.workspace.diffStatAfter = gitText(result.workspace.assignedWorktree, ["diff", "--stat", "HEAD"]);
        } catch (error) {
          result.exitCode = 1;
          result.stopReason = "error";
          result.errorMessage = `worker post-exit diagnostics failed: ${error instanceof Error ? error.message : String(error)}`;
        }
      }
      if (readOnlyStatusBefore !== undefined) {
        try {
          const statusAfter = gitStatusOrUndefined(spec.cwd);
          if (statusAfter !== readOnlyStatusBefore) {
            result.exitCode = 1;
            result.stopReason = "error";
            result.errorMessage = "read_only_repository_mutation: read-only agent changed repository state; verdict invalidated";
          }
        } catch (error) {
          result.exitCode = 1;
          result.stopReason = "error";
          result.errorMessage = `read-only post-exit diagnostics failed: ${error instanceof Error ? error.message : String(error)}`;
        }
      }
      appendHerdrLog(`[${result.stopReason}]`);
      if (herdrHandle) {
        try { herdrPanel.close(herdrHandle); } catch {}
        herdrHandle = undefined;
      }
      tracker.setModel(id, result.model);
      tracker.finish(id, result.stopReason === "aborted" ? "aborted" : result.exitCode === 0 ? "completed" : "failed", result.stopReason);
      try { rmSync(promptDir, { recursive: true, force: true }); } catch {}
      resolve(result);
    };
    try {
      child = spawnProcess(invocation.command, invocation.args, {
        cwd: runCwd,
        env: { ...process.env, PI_TASK_PARENT_RUN_ID: id, PI_TASK_AGENT_NAME: spec.agent.name, PI_TASK_METHODOLOGIES_DIR: methodologiesDirectory, ...(result.workspace ? { PI_TASK_WORKTREE: result.workspace.assignedWorktree } : {}) },
        shell: false,
        stdio: ["ignore", "pipe", "pipe"],
        detached: true,
      });
      tracker.setPid(id, child.pid);
      try {
        if (herdrPanel.available()) {
          const skillNames = resolvedSkillNames(skillDirectories);
          writeFileSync(herdrLogPath, `${spec.agent.name} | ${spec.taskId || id}\nskills | ${skillNames.join(", ") || "none"}\n${spec.task}\n\n`, { encoding: "utf8", mode: 0o600 });
          herdrHandle = herdrPanel.open({ cwd: runCwd, label: `${spec.agent.name}-${(spec.taskId || id).slice(0, 16)}`, logPath: herdrLogPath });
        }
      } catch {
        herdrHandle = undefined;
      }
      let buffer = "";
      const processLine = (line: string) => {
        const event = parseJsonEvent(line);
        if (!event) return;
        if (event.type !== "message_end" && event.type !== "tool_result_end") return;
        if (!event.message) return;
        result.messages.push(event.message);
        updateUsage(result, event.message);
        tracker.setModel(id, result.model);
        tracker.setUsage(id, result.usage);
        const content = Array.isArray(event.message.content) ? event.message.content : [];
        const tools = content.filter((part: any) => part?.type === "toolCall").map((part: any) => part.name).filter(Boolean);
        const text = content.find((part: any) => part?.type === "text")?.text || content.find((part: any) => part?.type === "thinking")?.thinking || "message";
        appendHerdrLog(tools.length ? `using ${tools.join(", ")}` : String(text));
        tracker.event(id, tools.length ? "tool" : event.type === "tool_result_end" ? "tool_result" : "message", tools.length ? tools.join(", ") : String(text).trim());
        onUpdate?.({ result, event: event.type === "message_end" ? "message" : "tool_result" });
      };
      const bumpActivity = () => {
        if (activityTimer) clearTimeout(activityTimer);
        activityTimer = setTimeout(() => {
          if (settled) return;
          result.stopReason = "stalled";
          tracker.event(id, "stderr", `inactivity watchdog: no child output for ${Math.round(inactivityKillMs / 60000)}m; terminating process group`);
          stopChild();
        }, inactivityKillMs);
        activityTimer.unref();
      };
      child.stdout?.on("data", (data: Buffer | string) => {
        // Any stdout bytes prove the child is alive even between message_end
        // events, so long silent model turns must not trip the stall detector.
        tracker.touch(id);
        bumpActivity();
        buffer += data.toString();
        const lines = buffer.split("\n");
        buffer = lines.pop() || "";
        for (const line of lines) processLine(line);
      });
      child.stderr?.on("data", (data: Buffer | string) => {
        bumpActivity();
        const text = data.toString();
        result.stderr += text;
        appendHerdrLog(text);
        if (text.trim()) tracker.event(id, "stderr", text.trim());
      });
      child.on("error", (error) => { result.errorMessage = error.message; finish(1, "error"); });
      // Terminal-result constraint: finalize on "exit", not "close". Detached
      // grandchildren can hold the pipe write-ends and defer "close"
      // indefinitely, so finish() would never run and the final message (which
      // carries the done completion or review report) would be lost. "close"
      // remains the drain-fast-path; the exit grace flushes whatever the child
      // wrote but that never got a trailing newline.
      child.on("close", (code) => { if (buffer.trim()) processLine(buffer); buffer = ""; finish(code ?? 1); });
      child.on("exit", (code) => {
        if (settled) return;
        exitGraceTimer = setTimeout(() => {
          if (settled) return;
          if (buffer.trim()) processLine(buffer);
          buffer = "";
          finish(code ?? 1);
        }, EXIT_FINALIZE_GRACE_MS);
        exitGraceTimer.unref();
      });
      stopChild = () => {
        if (settled) return;
        if (result.stopReason !== "timed_out" && result.stopReason !== "stalled") result.stopReason = "aborted";
        tracker.observeLifecycle(id, "interrupted");
        const pid = child?.pid;
        if (pid) {
          try { process.kill(-pid, "SIGTERM"); } catch { child?.kill("SIGTERM"); }
          setTimeout(() => {
            if (settled) return;
            try { process.kill(-pid, "SIGKILL"); } catch { child?.kill("SIGKILL"); }
          }, WORKER_WRAP_UP_MS).unref();
        }
      };
      // Deadline constraint: EVERY managed child gets a hard wall-clock cap —
      // reviewers previously had none, so a hung review blocked the item forever.
      deadlineTimer = setTimeout(() => {
        if (settled) return;
        result.stopReason = "timed_out";
        tracker.event(id, "stderr", `${spec.stage || "subagent"} deadline reached; terminating process group with ${WORKER_WRAP_UP_MS}ms kill grace`);
        stopChild();
      }, stageDeadlineMs);
      deadlineTimer.unref();
      bumpActivity();
      abortListener = stopChild;
      if (spec.signal?.aborted) stopChild();
      else spec.signal?.addEventListener("abort", abortListener, { once: true });
    } catch (error) {
      result.errorMessage = error instanceof Error ? error.message : String(error);
      finish(1, "error");
    }
  });
  return {
    id,
    pid: child?.pid,
    result: resultPromise,
    stop: () => stopChild(),
  };
}

export function startSubagentResilient(spec: SubagentSpec, onUpdate?: (update: SubagentUpdate) => void, spawnProcess: SpawnFunction = spawn): SubagentHandle {
  const runId = spec.runId || randomUUID();
  const boundSpec: SubagentSpec = { ...spec, runId };
  let handle = startSubagent(boundSpec, onUpdate, spawnProcess);
  let currentStop = () => handle.stop();
  const result = (async (): Promise<SubagentResult> => {
    let retry = 0;
    for (;;) {
      const attempt = await handle.result;
      const fault = classifyRunnerTransientFault(attempt);
      if (fault === "none" || retry >= RUNNER_TRANSIENT_RETRIES) {
        if (fault !== "none") {
          // Mark the run failed (not completed) so the scheduler blocks the stage
          // via the transient_provider classification instead of treating the
          // empty output as a completed worker that then fails XML validation.
          attempt.exitCode = 1;
          attempt.failureCode = "transient_provider";
          attempt.errorMessage = `transient provider fault (${fault}) after ${retry} in-claim ${retry === 1 ? "retry" : "retries"}; exhausted without consuming a numbered attempt`;
        }
        return attempt;
      }
      retry++;
      if (attempt.workspace?.assignedWorktree && !boundSpec.durableWorktreeKey) {
        // Durable worker worktree constraint (RLB-GAP-001): a pack-keyed retained
        // worktree skips the destructive reset — the uncommitted diff IS the
        // resume state between in-claim retries.
        try { resetSubagentWorktreeToCandidate(spec, attempt.workspace.assignedWorktree, spec.cwd); }
        catch {}
      }
      await new Promise((resolve) => setTimeout(resolve, runnerTransientBackoffMs(retry, spec)));
      handle = startSubagent(boundSpec, onUpdate, spawnProcess);
      currentStop = () => handle.stop();
    }
  })();
  return { id: runId, result, stop: () => currentStop() };
}
