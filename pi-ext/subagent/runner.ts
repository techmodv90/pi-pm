import { execFile, execFileSync, spawn, type ChildProcess } from "node:child_process";
import { appendFileSync, closeSync, existsSync, mkdirSync, mkdtempSync, openSync, readdirSync, readFileSync, readSync, realpathSync, renameSync, rmSync, statSync, writeFileSync } from "node:fs";
import { randomUUID } from "node:crypto";
import { tmpdir } from "node:os";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import type { AgentConfig } from "./types.ts";
import type { SubagentResult, SubagentUpdate, SubagentUsage, SubagentWorkspace } from "./types.ts";
import { agentRunTracker, type AgentRunTracker } from "./tracker.ts";
import { createHerdrPanel, type HerdrPanel, type HerdrPanelHandle } from "./herdr-panel.ts";
import { resolveSkillDirectories, resolvedSkillNames } from "./skills.ts";

export type SpawnFunction = typeof spawn;
export const MANAGED_WORKER_DEADLINE_MS = 30 * 60 * 1000;
const WORKER_WRAP_UP_MS = 5_000;
const execFileAsync = promisify(execFile);
const defaultHerdrPanel = createHerdrPanel();
const methodologiesDirectory = fileURLToPath(new URL("../methodologies", import.meta.url));

const emptyUsage = (): SubagentUsage => ({ input: 0, output: 0, cacheRead: 0, cacheWrite: 0, cost: 0, contextTokens: 0, turns: 0 });
const READ_ONLY_AGENTS = new Set(["task-scout", "task-reviewer", "rri-persona", "rri-t-persona"]);

export function buildPiInvocation(args: string[], script = process.argv[1]): { command: string; args: string[] } {
  const virtualScript = script?.startsWith("/$bunfs/root/");
  if (script && !virtualScript && existsSync(script)) return { command: process.execPath, args: [script, ...args] };
  const runtime = process.execPath.toLowerCase().split(/[\\/]/).pop() || "";
  if (!/^(node|bun)(\.exe)?$/.test(runtime)) return { command: process.execPath, args };
  return { command: "pi", args };
}

export function parseJsonEvent(line: string): any | null {
  if (!line.trim()) return null;
  try {
    return JSON.parse(line);
  } catch {
    return null;
  }
}

export function finalAssistantText(messages: any[]): string {
  for (let index = messages.length - 1; index >= 0; index--) {
    const message = messages[index];
    if (message?.role !== "assistant") continue;
    const text = message.content?.find?.((part: any) => part.type === "text")?.text;
    if (text) return text;
  }
  return "";
}

export interface SubagentSpec {
  agent: AgentConfig;
  task: string;
  cwd: string;
  acceptance?: "checked" | "attested";
  isolation?: "worktree";
  signal?: AbortSignal;
  runId?: string;
  stage?: string;
  taskId?: string;
  tracker?: AgentRunTracker;
  skillDirectories?: string[];
  skillFamilies?: string[];
  herdrPanel?: HerdrPanel;
  initialPatchPath?: string;
  preparedWorktree?: string;
  /** Host-side pi session file (create-or-continue) pinned outside the worktree; enables review-fix resume. */
  sessionPath?: string;
  /** Backoff base (ms) between in-claim transient-provider retries; defaults to RUNNER_TRANSIENT_BACKOFF_MS. */
  transientBackoffMs?: number;
}

export interface SubagentHandle {
  id: string;
  pid?: number;
  result: Promise<SubagentResult>;
  stop(): void;
}

export function assertManagedAcceptance(spec: SubagentSpec): void {
  if (!spec.stage) return;
  const required = spec.stage === "review" ? "attested" : "checked";
  if (spec.acceptance !== required) throw new Error(`${spec.stage} subagent requires acceptance ${required}`);
}

export async function prepareSubagentWorktree(cwd: string, initialPatchPath?: string, runId: string = randomUUID()): Promise<{ runId: string; cwd: string }> {
  const worktreeRoot = join(cwd, ".pi", "worktrees");
  mkdirSync(worktreeRoot, { recursive: true, mode: 0o700 });
  const worktree = join(worktreeRoot, runId);
  await execFileAsync("git", ["worktree", "add", "-b", `pi-agent-${runId}`, worktree, "HEAD"], { cwd });
  try {
    if (initialPatchPath && statSync(initialPatchPath).size > 0) {
      try {
        await execFileAsync("git", ["apply", "--check", initialPatchPath], { cwd: worktree });
        await execFileAsync("git", ["apply", initialPatchPath], { cwd: worktree });
      } catch (error) {
        // Review recovery: an integrated candidate patch is already in the base; review that base directly.
        try {
          await execFileAsync("git", ["apply", "--reverse", "--check", initialPatchPath], { cwd: worktree });
        } catch {
          throw error;
        }
      }
    }
  } catch (error) {
    removeSubagentWorktree(cwd, worktree, runId);
    throw error;
  }
  return { runId, cwd: worktree };
}

export function removeSubagentWorktree(cwd: string, worktree: string, runId: string): void {
  const branch = `pi-agent-${runId}`;
  if (basename(worktree) !== runId) throw new Error(`refusing to remove unowned worktree: ${worktree}`);
  const canonicalWorktree = existsSync(worktree) ? realpathSync(worktree) : worktree;
  const registration = execFileSync("git", ["worktree", "list", "--porcelain"], { cwd, encoding: "utf8" })
    .trim().split(/\n\n+/).find((block) => {
      const registeredPath = block.match(/^worktree (.+)$/m)?.[1];
      return registeredPath && (existsSync(registeredPath) ? realpathSync(registeredPath) : registeredPath) === canonicalWorktree;
    });
  if (registration) {
    const registeredBranch = registration.match(/^branch refs\/heads\/(.+)$/m)?.[1];
    if (registeredBranch !== branch) throw new Error(`refusing to remove unowned worktree branch: ${registeredBranch || "detached"}`);
    execFileSync("git", ["worktree", "remove", "--force", worktree], { cwd, stdio: "pipe" });
  } else if (existsSync(worktree)) {
    throw new Error(`refusing to remove unregistered worktree: ${worktree}`);
  }
  try { execFileSync("git", ["show-ref", "--verify", `refs/heads/${branch}`], { cwd, stdio: "pipe" }); }
  catch { return; }
  execFileSync("git", ["branch", "-D", branch], { cwd, stdio: "pipe" });
}

export function cleanupOrphanedSubagentWorktrees(cwd: string, activeRunIds: ReadonlySet<string>): void {
  const blocks = execFileSync("git", ["worktree", "list", "--porcelain"], { cwd, encoding: "utf8" }).trim().split(/\n\n+/);
  for (const block of blocks) {
    const path = block.match(/^worktree (.+)$/m)?.[1];
    const branch = block.match(/^branch refs\/heads\/pi-agent-(.+)$/m)?.[1];
    if (!path || !branch || activeRunIds.has(branch) || basename(path) !== branch) continue;
    removeSubagentWorktree(cwd, path, branch);
  }
}

function gitText(cwd: string, args: string[]): string {
  return execFileSync("git", args, { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
}

function gitStatusOrUndefined(cwd: string): string | undefined {
  try { return gitText(cwd, ["status", "--porcelain=v1", "--untracked-files=all"]); } catch { return undefined; }
}

function workspaceDiagnostics(cwd: string): SubagentWorkspace {
  const canonical = realpathSync(cwd);
  const gitToplevel = realpathSync(gitText(canonical, ["rev-parse", "--show-toplevel"]));
  if (gitToplevel !== canonical) throw new Error(`worker worktree invariant failed: child_process_cwd=${canonical} git_toplevel=${gitToplevel}`);
  return {
    assignedWorktree: canonical,
    childProcessCwd: canonical,
    bashCwd: canonical,
    readToolRoot: canonical,
    editToolRoot: canonical,
    writeToolRoot: canonical,
    applyPatchRoot: canonical,
    gitToplevel,
    head: gitText(canonical, ["rev-parse", "HEAD"]),
    statusBefore: gitText(canonical, ["status", "--short"]),
    statusAfter: "",
    diffStatAfter: "",
  };
}

function updateUsage(result: SubagentResult, message: any): void {
  if (message.role !== "assistant") return;
  result.usage.turns++;
  const usage = message.usage;
  if (!usage) return;
  result.usage.input += usage.input || 0;
  result.usage.output += usage.output || 0;
  result.usage.cacheRead += usage.cacheRead || 0;
  result.usage.cacheWrite += usage.cacheWrite || 0;
  result.usage.cost += usage.cost?.total || 0;
  result.usage.contextTokens = usage.totalTokens || 0;
  if (!result.model && message.model) result.model = message.model;
  result.stopReason = message.stopReason || result.stopReason;
  result.errorMessage = message.errorMessage || result.errorMessage;
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
  if (spec.agent.systemPrompt.trim()) args.push("--append-system-prompt", promptPath);
  const patchedTask = spec.initialPatchPath
    ? spec.stage === "review"
      ? `${spec.task}\n\nCANDIDATE ATTESTATION: The bound candidate patch has been applied to this isolated worktree. Review this worktree, not the parent checkout.`
      : `${spec.task}\n\nREVIEW-FIX RUN: The rejected candidate patch has been applied to this worktree. You must modify the worktree to address the review findings and produce a non-empty patch different from the rejected candidate. A no-op completion is invalid.`
    : spec.task;
  args.push(`Task: ${patchedTask}`);
  const invocation = buildPiInvocation(args);
  let child: ChildProcess | undefined;
  const herdrPanel = spec.herdrPanel ?? defaultHerdrPanel;
  const herdrLogPath = join(runCwd, ".pi-subagents", "runs", id, "herdr.log");
  let herdrHandle: HerdrPanelHandle | undefined;
  let settled = false;
  let stopChild = () => {};
  let deadlineTimer: NodeJS.Timeout | undefined;
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
      child.stdout?.on("data", (data: Buffer | string) => {
        // Any stdout bytes prove the child is alive even between message_end
        // events, so long silent model turns must not trip the stall detector.
        tracker.touch(id);
        buffer += data.toString();
        const lines = buffer.split("\n");
        buffer = lines.pop() || "";
        for (const line of lines) processLine(line);
      });
      child.stderr?.on("data", (data: Buffer | string) => {
        const text = data.toString();
        result.stderr += text;
        appendHerdrLog(text);
        if (text.trim()) tracker.event(id, "stderr", text.trim());
      });
      child.on("error", (error) => { result.errorMessage = error.message; finish(1, "error"); });
      child.on("close", (code) => { if (buffer.trim()) processLine(buffer); finish(code ?? 1); });
      stopChild = () => {
        if (settled) return;
        if (result.stopReason !== "timed_out") result.stopReason = "aborted";
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
      if (spec.stage === "worker" || spec.stage === "autofix") {
        deadlineTimer = setTimeout(() => {
          if (settled) return;
          result.stopReason = "timed_out";
          tracker.event(id, "stderr", `worker deadline reached; terminating process group with ${WORKER_WRAP_UP_MS}ms kill grace`);
          stopChild();
        }, MANAGED_WORKER_DEADLINE_MS);
        deadlineTimer.unref();
      }
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

// Transient provider fault classification constraint: the runner classifies
// output BEFORE any downstream XML validation so empty-output and inference-
// abort faults are retried inside the same claim instead of being surfaced as
// malformed-verdict failures. A completed agent that returned assistant text is
// never transient.
export type RunnerTransientFault = "empty_output" | "inference_abort" | "none";

export const RUNNER_TRANSIENT_RETRIES = 2;
export const RUNNER_TRANSIENT_BACKOFF_MS = 500;

export function classifyRunnerTransientFault(result: SubagentResult): RunnerTransientFault {
  if (result.stopReason === "aborted") return "none";
  const diagnostic = `${result.errorMessage || ""}\n${result.stderr || ""}`;
  if (/inference[\s_-]?abort|inference\s+error|empty[\s_-]?(?:model|provider)[\s_-]?(?:output|response)|provider[\s_-]?error/i.test(diagnostic)) return "inference_abort";
  // Empty assistant output is transient independently of exit code: a provider
  // that returns no text (even with a nonzero child exit and no matching
  // diagnostic) must still be retried in-claim rather than misclassified as a
  // normal failure. Explicit user aborts are excluded above.
  if (!finalAssistantText(result.messages || []).trim()) return "empty_output";
  return "none";
}

export function runnerTransientBackoffMs(retryIndex: number, spec: SubagentSpec): number {
  const base = spec.transientBackoffMs !== undefined ? spec.transientBackoffMs : RUNNER_TRANSIENT_BACKOFF_MS;
  return base * Math.max(1, retryIndex);
}

// In-claim transient retry constraint: retry at most RUNNER_TRANSIENT_RETRIES
// times inside the same claim (same runId, no numbered pipeline attempt
// consumed), resetting the isolated worktree between attempts. On exhaustion the
// handled result is tagged failure_code=transient_provider.

// Candidate-state restore constraint: a retry must run against the same attested
// candidate as the original launch. git reset --hard HEAD alone discards the
// uncommitted initial patch that prepareSubagentWorktree applied, so after the
// reset we reapply and revalidate spec.initialPatchPath (mirroring the
// reverse-check integration fallback used at preparation time).
export function resetSubagentWorktreeToCandidate(spec: SubagentSpec, assignedWorktree: string, cwd: string): void {
  execFileSync("git", ["-C", assignedWorktree, "reset", "--hard", "HEAD"], { cwd, stdio: "pipe" });
  // Retry-state cleanliness constraint: git reset --hard leaves untracked files
  // from the failed attempt in place, so a retry could inherit generated or newly
  // created artifacts and diverge from the attested candidate. Drop untracked
  // paths before reapplying the patch; no -x, ignored build output stays.
  execFileSync("git", ["-C", assignedWorktree, "clean", "-fd"], { cwd, stdio: "pipe" });
  if (!spec.initialPatchPath || statSync(spec.initialPatchPath).size === 0) return;
  try {
    execFileSync("git", ["apply", "--check", spec.initialPatchPath], { cwd: assignedWorktree, stdio: "pipe" });
    execFileSync("git", ["apply", spec.initialPatchPath], { cwd: assignedWorktree, stdio: "pipe" });
  } catch (error) {
    // Review recovery: mirror preparation; the candidate patch may already be
    // integrated in the base commit, in which case applying is a no-op.
    try { execFileSync("git", ["apply", "--reverse", "--check", spec.initialPatchPath], { cwd: assignedWorktree, stdio: "pipe" }); }
    catch { throw error; }
  }
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
      if (attempt.workspace?.assignedWorktree) {
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