import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { EventEmitter } from "node:events";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, utimesSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import { assertManagedAcceptance, buildPiInvocation, classifyRunnerTransientFault, finalAssistantText, getAppendSystemPromptPaths, parseJsonEvent, prepareSubagentWorktree, removeSubagentWorktree, retainWorktreeForResume, RUNNER_TRANSIENT_RETRIES, startSubagent, startSubagentResilient } from "./runner.ts";
import { AgentRunTracker } from "./tracker.ts";

process.env.PI_TASK_HERDR_PANEL = "0";

test("managed pipeline stages require their acceptance contract", () => {
  const base = { agent: { name: "task-worker" }, task: "work", cwd: "/repo" } as any;
  assert.doesNotThrow(() => assertManagedAcceptance({ ...base, stage: "worker", acceptance: "checked" }));
  assert.doesNotThrow(() => assertManagedAcceptance({ ...base, stage: "review", acceptance: "attested" }));
  assert.throws(() => assertManagedAcceptance({ ...base, stage: "worker", acceptance: "attested" }), /requires acceptance checked/);
  assert.throws(() => assertManagedAcceptance({ ...base, stage: "review" }), /requires acceptance attested/);
});

const silentChild = () => {
  // Emulates SIGTERM semantics: process-group kill fails (ESRCH), fallback
  // child.kill terminates and the closed event carries the signal exit code.
  const child: any = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), pid: 424242 });
  child.kill = () => { setImmediate(() => child.emit("close", 137)); return true; };
  return child;
};

function spawnNeverExiting(child: any) {
  return (() => child) as any;
}

// Watchdog/deadline timers are unref'd; a ref'd interval holds the event loop
// open until the awaited result settles (cleared in each watchdog test).
const keepEventLoopAlive = () => setInterval(() => {}, 50);

test("deadline kills a hung review-stage child", async () => {
  const tracker = new AgentRunTracker();
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-review-deadline-"));
  const handle = startSubagent({
    agent: { name: "task-reviewer", description: "", systemPrompt: "", source: "packaged", filePath: "task-reviewer.md" },
    task: "review",
    cwd,
    stage: "review",
    acceptance: "attested",
    tracker,
    deadlineMs: 30,
    herdrPanel: { available: () => false, open: () => ({ paneId: "" }), close: () => {} },
  }, undefined, spawnNeverExiting(silentChild()));
  const keepAlive = keepEventLoopAlive();
  try {
    const result = await handle.result;
    assert.equal(result.stopReason, "timed_out");
    assert.notEqual(result.exitCode, 0);
  } finally { clearInterval(keepAlive); }
});

test("inactivity watchdog kills a silent child and classifies provider_stall", async () => {
  const tracker = new AgentRunTracker();
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-stall-watchdog-"));
  const handle = startSubagentResilient({
    agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "task-worker.md" },
    task: "work",
    cwd,
    stage: "worker",
    acceptance: "checked",
    tracker,
    inactivityKillMs: 30,
    deadlineMs: 60_000,
    herdrPanel: { available: () => false, open: () => ({ paneId: "" }), close: () => {} },
  } as any, undefined, spawnNeverExiting(silentChild()));
  const keepAlive = keepEventLoopAlive();
  try {
    const result = await handle.result;
    assert.equal(result.stopReason, "stalled");
    assert.match(result.errorMessage || "", /provider_stall/);
  } finally { clearInterval(keepAlive); }
});

test("classifyRunnerTransientFault maps stalled to provider_stall", () => {
  assert.equal(classifyRunnerTransientFault({ stopReason: "stalled" } as any), "provider_stall");
  assert.equal(classifyRunnerTransientFault({ stopReason: "aborted" } as any), "none");
  // Durable worker worktree constraint (RLB-GAP-001): deadline kills are
  // throughput faults, transient so the pack worktree is retained and the
  // in-claim retry resumes with a fresh budget (SQ-1).
  assert.equal(classifyRunnerTransientFault({ stopReason: "timed_out" } as any), "provider_stall");
});

test("retainWorktreeForResume retains report-less worker deaths and cleans deterministic terminals", () => {
  const partialMessages = [{ role: "assistant", content: [{ type: "text", text: "partial work, no report" }] }];
  const reportMessages = [{ role: "assistant", content: [{ type: "text", text: "done <completion_report status=\"done\"/>" }] }];
  // Provider stream cut / watchdog / deadline death with no report: retain.
  assert.equal(retainWorktreeForResume("worker", { exitCode: 1, stopReason: "stalled", messages: partialMessages } as any), true);
  assert.equal(retainWorktreeForResume("worker", { exitCode: 1, stopReason: "timed_out", messages: partialMessages } as any), true);
  assert.equal(retainWorktreeForResume("worker", { exitCode: 1, stopReason: "end", messages: partialMessages } as any), true);
  // Exit-0 stream cut: provider ended the stream without the report.
  assert.equal(retainWorktreeForResume("worker", { exitCode: 0, messages: partialMessages } as any), true);
  // Deterministic terminals: report emitted, success, cancellation, non-mutation stage.
  assert.equal(retainWorktreeForResume("worker", { exitCode: 0, stopReason: "end", messages: reportMessages } as any), false);
  assert.equal(retainWorktreeForResume("worker", { exitCode: 1, stopReason: "aborted", messages: partialMessages } as any), false);
  assert.equal(retainWorktreeForResume("review", { exitCode: 1, stopReason: "stalled", messages: partialMessages } as any), false);
  assert.equal(retainWorktreeForResume(undefined, { exitCode: 1, stopReason: "stalled", messages: partialMessages } as any), false);
});

test("pack-keyed worktrees are reused without reset and refuse foreign registrations", async () => {
  const repo = mkdtempSync(join(tmpdir(), "task-subagent-packwt-"));
  execFileSync("git", ["init", "-q"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "work.go"), "package work\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  const first = await prepareSubagentWorktree(repo, undefined, "run-1", "wip-pack-1");
  assert.equal(first.reused, false);
  writeFileSync(join(first.cwd, "work.go"), "package work\n// partial attempt work\n");
  const second = await prepareSubagentWorktree(repo, undefined, "run-2", "wip-pack-1");
  assert.equal(second.reused, true);
  assert.equal(second.cwd, first.cwd);
  assert.equal(readFileSync(join(second.cwd, "work.go"), "utf8"), "package work\n// partial attempt work\n");
  // Unregistered directory with the expected branch name fails closed.
  const rogue = join(repo, ".pi", "worktrees", "wip-pack-2");
  mkdirSync(rogue, { recursive: true });
  await assert.rejects(() => prepareSubagentWorktree(repo, undefined, "run-3", "wip-pack-2"), /non-worktree path|unregistered/);
  rmSync(rogue, { recursive: true, force: true });
  // A worktree registered on a foreign branch fails closed.
  execFileSync("git", ["worktree", "add", "-b", "someone-elses-branch", rogue, "HEAD"], { cwd: repo });
  await assert.rejects(() => prepareSubagentWorktree(repo, undefined, "run-4", "wip-pack-2"), /foreign-branch/);
  removeSubagentWorktree(repo, first.cwd, "wip-pack-1");
  execFileSync("git", ["worktree", "remove", "--force", rogue], { cwd: repo });
  execFileSync("git", ["branch", "-D", "someone-elses-branch"], { cwd: repo });
});

test("orphan sweep keeps fresh retained worktrees and prunes only aged ones", async () => {
  const runner = await import("./runner.ts") as any;
  const repo = mkdtempSync(join(tmpdir(), "task-subagent-ttl-"));
  execFileSync("git", ["init", "-q"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "work.go"), "package work\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  const fresh = await prepareSubagentWorktree(repo, undefined, "run-fresh", "wip-pack-fresh");
  const aged = await prepareSubagentWorktree(repo, undefined, "run-aged", "wip-pack-aged");
  const stale = new Date(Date.now() - 25 * 60 * 60 * 1000);
  utimesSync(aged.cwd, stale, stale);
  runner.cleanupOrphanedSubagentWorktrees(repo, new Set(), runner.ORPHANED_WORKTREE_MAX_AGE_MS);
  assert.equal(readFileSync(join(fresh.cwd, "work.go"), "utf8"), "package work\n");
  assert.throws(() => readFileSync(join(aged.cwd, "work.go"), "utf8"));
  removeSubagentWorktree(repo, fresh.cwd, "wip-pack-fresh");
});

test("startSubagent publishes finalizing before terminal completion", async () => {
  const child = Object.assign(new EventEmitter(), {
    stdout: new EventEmitter(),
    stderr: new EventEmitter(),
    kill: () => true,
  });
  const tracker = new AgentRunTracker();
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-finalizing-"));
  const states: string[] = [];
  tracker.subscribe(() => {
    const run = tracker.list()[0];
    if (run) states.push(`${run.status}:${run.lifecycleState || "none"}`);
  });
  const handle = startSubagent({
    agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "task-worker.md" },
    task: "work",
    cwd,
    stage: "worker",
    acceptance: "checked",
    tracker,
    herdrPanel: { available: () => false, open: () => ({ paneId: "" }), close: () => {} },
  }, undefined, (() => {
    setImmediate(() => child.emit("close", 0));
    return child;
  }) as any);

  await handle.result;
  assert.ok(states.indexOf("running:finalizing") >= 0);
  assert.ok(states.indexOf("running:finalizing") < states.indexOf("completed:completed"));
});

test("buildPiInvocation runs the installed pi command outside a node script", () => {
  assert.deepEqual(buildPiInvocation(["--mode", "json"], "/$bunfs/root/pi"), { command: "pi", args: ["--mode", "json"] });
});

test("task-worker appends global APPEND_SYSTEM.md before its role prompt", () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-worker-append-system-"));
  const globalPrompt = join(cwd, "APPEND_SYSTEM.md");
  const rolePrompt = join(cwd, "task-worker-system-prompt.md");
  writeFileSync(globalPrompt, "global rules");
  assert.deepEqual(getAppendSystemPromptPaths("task-worker", "role rules", rolePrompt, globalPrompt), [globalPrompt, rolePrompt]);
  assert.deepEqual(getAppendSystemPromptPaths("task-reviewer", "review rules", rolePrompt, globalPrompt), [rolePrompt]);
});

test("startSubagent forwards the agent thinking level to pi", async () => {
  const child = Object.assign(new EventEmitter(), {
    stdout: new EventEmitter(),
    stderr: new EventEmitter(),
    kill: () => true,
  });
  let args: string[] = [];
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-thinking-"));
  const handle = startSubagent({
    agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "task-worker.md", thinking: "high" },
    task: "verify thinking",
    cwd,
    stage: "worker",
    acceptance: "checked",
    herdrPanel: { available: () => false, open: () => ({ paneId: "" }), close: () => {} },
  }, undefined, ((command, childArgs) => {
    assert.equal(command, process.execPath);
    args = childArgs;
    setImmediate(() => child.emit("close", 0));
    return child as any;
  }) as any);
  await handle.result;
  const thinkingIndex = args.indexOf("--thinking");
  assert.ok(thinkingIndex >= 0);
  assert.equal(args[thinkingIndex + 1], "high");
});

test("startSubagent supplies extension-owned methodologies", async () => {
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  let methodologyDir = "";
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-methodologies-"));
  const handle = startSubagent({
    agent: { name: "rri-persona", description: "", systemPrompt: "", source: "packaged", filePath: "rri-persona.md" },
    task: "verify methodology path",
    cwd,
    stage: "rri",
    acceptance: "checked",
    herdrPanel: { available: () => false, open: () => ({ paneId: "" }), close: () => {} },
  }, undefined, ((command, args, options) => {
    methodologyDir = (options.env as Record<string, string>).PI_TASK_METHODOLOGIES_DIR;
    setImmediate(() => child.emit("close", 0));
    return child as any;
  }) as any);
  await handle.result;
  assert.match(methodologyDir, /pi-ext[\\/]methodologies$/);
});

test("parseJsonEvent accepts assistant and tool result JSONL events", () => {
  assert.deepEqual(parseJsonEvent('{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}'), {
    type: "message_end",
    message: { role: "assistant", content: [{ type: "text", text: "done" }] },
  });
  assert.equal(parseJsonEvent("not json"), null);
});

test("finalAssistantText returns the last assistant text", () => {
  assert.equal(finalAssistantText([
    { role: "assistant", content: [{ type: "text", text: "first" }] },
    { role: "toolResult", content: [] },
    { role: "assistant", content: [{ type: "text", text: "last" }] },
  ]), "last");
});

test("review-fix runner task includes a no-op prohibition", () => {
  const source = readFileSync(new URL("./runner.ts", import.meta.url), "utf8");
  assert.match(source, /REVIEW-FIX RUN/);
  assert.match(source, /A no-op completion is invalid/);
  assert.match(source, /CANDIDATE ATTESTATION/);
  assert.match(source, /Review this worktree, not the parent checkout/);
});

test("startSubagent mirrors live activity to an automatically managed HerdR panel", async () => {
  const child = Object.assign(new EventEmitter(), {
    stdout: new EventEmitter(),
    stderr: new EventEmitter(),
    kill: () => true,
  });
  let logPath = "";
  let openedCwd = "";
  let closedPane = "";
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-herdr-"));
  const baselineSkill = join(cwd, "skills", "test-first");
  const family = join(cwd, "skills", "languages", "typescript");
  const familySkill = join(family, "typescript-strict");
  mkdirSync(baselineSkill, { recursive: true });
  mkdirSync(familySkill, { recursive: true });
  writeFileSync(join(baselineSkill, "SKILL.md"), "---\nname: test-first\n---\n");
  writeFileSync(join(familySkill, "SKILL.md"), "---\nname: typescript-strict\n---\n");
  const handle = startSubagent({
    agent: { name: "task-worker", description: "", skills: ["test-first"], systemPrompt: "", source: "packaged", filePath: "task-worker.md" },
    task: "implement T06",
    cwd,
    stage: "worker",
    acceptance: "checked",
    taskId: "t-42",
    skillFamilies: ["languages/typescript"],
    skillDirectories: [baselineSkill, family],
    herdrPanel: {
      available: () => true,
      open: (input) => {
        logPath = input.logPath;
        openedCwd = input.cwd;
        return { paneId: "w1:p2" };
      },
      close: (panelHandle) => { closedPane = panelHandle.paneId; },
    },
  }, undefined, (() => {
    setImmediate(() => {
      child.stdout.emit("data", '{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolCall","name":"bash"}]}}\n');
      child.stderr.emit("data", "provider retry\n");
      child.stdout.emit("data", '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}\n');
      child.emit("close", 0);
    });
    return child;
  }) as any);

  const result = await handle.result;
  assert.equal(openedCwd, cwd);
  assert.equal(closedPane, "w1:p2");
  assert.match(readFileSync(logPath, "utf8"), /task-worker \| t-42/);
  assert.match(readFileSync(logPath, "utf8"), /skills \| test-first, typescript-strict/);
  assert.match(readFileSync(logPath, "utf8"), /using bash/);
  assert.match(readFileSync(logPath, "utf8"), /provider retry/);
  assert.match(readFileSync(logPath, "utf8"), /done/);
  assert.equal(result.exitCode, 0);
});

test("startSubagent streams message events before the child exits", async () => {
  const child = Object.assign(new EventEmitter(), {
    stdout: new EventEmitter(),
    stderr: new EventEmitter(),
    kill: () => true,
  });
  const updates: string[] = [];
  const tracker = new AgentRunTracker();
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-runner-"));

  const handle = startSubagent({
    agent: { name: "worker", description: "", systemPrompt: "", source: "packaged", filePath: "worker.md" },
    task: "work",
    cwd,
    stage: "worker",
    acceptance: "checked",
    taskId: "t-42",
    tracker,
  }, (update) => updates.push(update.event), (() => {
    setImmediate(() => {
      child.stdout.emit("data", '{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolCall","name":"read","arguments":{"path":"secret.txt"}}]}}\n');
      child.stderr.emit("data", "provider retry\n");
      child.stdout.emit("data", '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}\n');
      child.emit("close", 0);
    });
    return child;
  }) as any);

  const result = await handle.result;
  assert.deepEqual(updates, ["message", "message"]);
  assert.equal(finalAssistantText(result.messages), "done");
  assert.equal(result.exitCode, 0);
  assert.equal(tracker.get(handle.id)?.taskId, "t-42");
  assert.deepEqual(tracker.get(handle.id)?.events.map((event) => [event.type, event.summary]), [
    ["started", "worker"],
    ["tool", "read"],
    ["stderr", "provider retry"],
    ["message", "done"],
    ["completed", "end"],
  ]);
});

test("runner finalizes on child exit even when close is deferred by detached grandchildren", async () => {
  // Regression: detached bash grandchildren hold pipe write-ends, so "close"
  // never fires and the terminal result is lost; finalization must ride "exit".
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  const tracker = new AgentRunTracker();
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-exit-finalize-"));

  const handle = startSubagent({
    agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "worker.md" },
    task: "work",
    cwd,
    stage: "worker",
    acceptance: "checked",
    tracker,
  }, undefined, (() => {
    setImmediate(() => {
      child.stdout.emit("data", VALID_OUTPUT);
      child.emit("exit", 0);
      // No "close": a detached grandchild still holds the pipe write-end.
    });
    return child;
  }) as any);

  const result = await handle.result;
  assert.equal(result.exitCode, 0);
  assert.equal(finalAssistantText(result.messages), "done");
  assert.equal(tracker.get(handle.id)?.status, "completed");
});

test("runner exit finalization flushes a buffered final line that never got a newline", async () => {
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  const tracker = new AgentRunTracker();
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-exit-flush-"));

  const handle = startSubagent({
    agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "worker.md" },
    task: "work",
    cwd,
    stage: "worker",
    acceptance: "checked",
    tracker,
  }, undefined, (() => {
    setImmediate(() => {
      child.stdout.emit("data", VALID_OUTPUT);
      child.emit("exit", 0);
    });
    return child;
  }) as any);

  const result = await handle.result;
  assert.equal(result.exitCode, 0);
  assert.equal(finalAssistantText(result.messages), "done");
});

test("isolated worker binds the child process and tool root to one canonical worktree", async () => {
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  const repo = mkdtempSync(join(tmpdir(), "task-subagent-worktree-"));
  execFileSync("git", ["init", "-q"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "work.go"), "package work\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  let spawnedCwd = "";
  const handle = startSubagent({
    agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "worker.md" },
    task: "change work.go",
    cwd: repo,
    isolation: "worktree",
    skillDirectories: [],
  }, undefined, ((_command: any, _args: any, options: any) => {
    spawnedCwd = options.cwd;
    writeFileSync(join(spawnedCwd, "work.go"), "package changed\n");
    setImmediate(() => child.emit("close", 0));
    return child;
  }) as any);

  const result = await handle.result;
  assert.equal(spawnedCwd, result.workspace?.assignedWorktree);
  assert.equal(result.workspace?.gitToplevel, result.workspace?.assignedWorktree);
  assert.equal(result.workspace?.readToolRoot, spawnedCwd);
  assert.match(result.workspace?.statusAfter || "", /work\.go/);
  // Tracker state must persist in the host repo so worktree cleanup cannot
  // destroy the run's metrics evidence.
  assert.equal(existsSync(join(repo, ".pi-subagents", "runs", result.runId, "state.json")), true);
});

test("isolated correction worker starts with the reviewed candidate patch applied", async () => {
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  const repo = mkdtempSync(join(tmpdir(), "task-subagent-correction-"));
  execFileSync("git", ["init", "-q"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "work.go"), "package work\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  writeFileSync(join(repo, "work.go"), "package candidate\n");
  const patch = join(repo, "candidate.patch");
  writeFileSync(patch, execFileSync("git", ["diff", "HEAD", "--", "work.go"], { cwd: repo }));
  execFileSync("git", ["checkout", "--", "work.go"], { cwd: repo });

  const handle = startSubagent({
    agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "worker.md" },
    task: "correct work.go",
    cwd: repo,
    isolation: "worktree",
    initialPatchPath: patch,
    skillDirectories: [],
  }, undefined, ((_command: any, _args: any, options: any) => {
    assert.equal(readFileSync(join(options.cwd, "work.go"), "utf8"), "package candidate\n");
    setImmediate(() => child.emit("close", 0));
    return child;
  }) as any);

  await handle.result;
});

test("isolated reviewer accepts an empty no-op candidate patch", async () => {
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  const repo = mkdtempSync(join(tmpdir(), "task-subagent-empty-patch-"));
  execFileSync("git", ["init", "-q"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "work.go"), "package work\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  const patch = join(repo, "candidate.patch");
  writeFileSync(patch, "");

  const handle = startSubagent({
    agent: { name: "task-reviewer", description: "", systemPrompt: "", source: "packaged", filePath: "reviewer.md" },
    task: "review work.go",
    cwd: repo,
    isolation: "worktree",
    initialPatchPath: patch,
    skillDirectories: [],
  }, undefined, ((_command: any, _args: any, options: any) => {
    assert.equal(readFileSync(join(options.cwd, "work.go"), "utf8"), "package work\n");
    setImmediate(() => child.emit("close", 0));
    return child;
  }) as any);

  await handle.result;
});

test("asynchronous worktree preparation accepts an empty no-op candidate patch", async () => {
  const repo = mkdtempSync(join(tmpdir(), "task-subagent-prepare-empty-"));
  execFileSync("git", ["init", "-q"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "work.go"), "package work\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  const patch = join(repo, "candidate.patch");
  writeFileSync(patch, "");

  const prepared = await prepareSubagentWorktree(repo, patch);
  assert.equal(readFileSync(join(prepared.cwd, "work.go"), "utf8"), "package work\n");
  removeSubagentWorktree(repo, prepared.cwd, prepared.runId);
});

test("failed candidate preparation removes its owned worktree and branch", async () => {
  const repo = mkdtempSync(join(tmpdir(), "task-subagent-prepare-invalid-"));
  execFileSync("git", ["init", "-q"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "work.go"), "package work\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  const patch = join(repo, "candidate.patch");
  writeFileSync(patch, "not a patch\n");
  await assert.rejects(() => prepareSubagentWorktree(repo, patch, "invalid-test"));
  assert.throws(() => execFileSync("git", ["show-ref", "--verify", "refs/heads/pi-agent-invalid-test"], { cwd: repo, stdio: "pipe" }));
  assert.throws(() => readFileSync(join(repo, ".pi", "worktrees", "invalid-test", "work.go"), "utf8"));
});

test("owned worktree cleanup removes only the matching task-system branch", async () => {
  const runner = await import("./runner.ts") as any;
  assert.equal(typeof runner.removeSubagentWorktree, "function");
  const repo = mkdtempSync(join(tmpdir(), "task-subagent-cleanup-"));
  execFileSync("git", ["init", "-q"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "work.go"), "package work\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  const prepared = await prepareSubagentWorktree(repo, undefined, "cleanup-test");

  runner.removeSubagentWorktree(repo, prepared.cwd, "cleanup-test");
  assert.throws(() => execFileSync("git", ["show-ref", "--verify", "refs/heads/pi-agent-cleanup-test"], { cwd: repo, stdio: "pipe" }));
  assert.throws(() => runner.removeSubagentWorktree(repo, repo, "cleanup-test"), /refusing to remove unowned worktree/);
});

test("restart cleanup preserves active task-system worktrees and removes orphans", async () => {
  const runner = await import("./runner.ts") as any;
  assert.equal(typeof runner.cleanupOrphanedSubagentWorktrees, "function");
  const repo = mkdtempSync(join(tmpdir(), "task-subagent-orphans-"));
  execFileSync("git", ["init", "-q"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "work.go"), "package work\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  const active = await prepareSubagentWorktree(repo, undefined, "active-test");
  const orphan = await prepareSubagentWorktree(repo, undefined, "orphan-test");

  runner.cleanupOrphanedSubagentWorktrees(repo, new Set(["active-test"]));
  assert.equal(readFileSync(join(active.cwd, "work.go"), "utf8"), "package work\n");
  assert.throws(() => readFileSync(join(orphan.cwd, "work.go"), "utf8"));
  runner.removeSubagentWorktree(repo, active.cwd, "active-test");
});

test("restart cleanup removes prunable registrations before owned branches", async () => {
  const runner = await import("./runner.ts") as any;
  const repo = mkdtempSync(join(tmpdir(), "task-subagent-prunable-"));
  execFileSync("git", ["init", "-q"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "work.go"), "package work\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  const orphan = await prepareSubagentWorktree(repo, undefined, "prunable-test");
  rmSync(orphan.cwd, { recursive: true, force: true });

  runner.cleanupOrphanedSubagentWorktrees(repo, new Set());
  assert.doesNotMatch(execFileSync("git", ["worktree", "list", "--porcelain"], { cwd: repo, encoding: "utf8" }), /prunable-test/);
  assert.throws(() => execFileSync("git", ["show-ref", "--verify", "refs/heads/pi-agent-prunable-test"], { cwd: repo, stdio: "pipe" }));
});

test("child agents disable inherited extensions and load the owned task-system extension", async () => {
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  const tracker = new AgentRunTracker();
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-runner-"));
  let invocation: any;
  const handle = startSubagent({
    agent: { name: "rri-persona", description: "", tools: ["read", "subagent"], systemPrompt: "", source: "packaged", filePath: "rri-persona.md" },
    task: "prepare interview",
    cwd,
    tracker,
  }, undefined, ((command: any, args: any, options: any) => {
    invocation = { command, args, options };
    setImmediate(() => child.emit("close", 0));
    return child;
  }) as any);
  await handle.result;
  assert.ok(invocation.args.includes("-ne"));
  const extensionIndex = invocation.args.indexOf("--extension");
  assert.match(invocation.args[extensionIndex + 1], /index\.ts$/);
  assert.equal(invocation.options.env.PI_TASK_PARENT_RUN_ID, handle.id);
});

test("startSubagent passes every resolved skill directory to Pi", async () => {
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-runner-"));
  let args: string[] = [];
  const handle = startSubagent({
    agent: { name: "worker", description: "", skills: ["baseline"], systemPrompt: "", source: "packaged", filePath: "worker.md" },
    task: "work",
    cwd,
    skillDirectories: ["/skills/baseline", "/skills/languages/golang"],
  }, undefined, ((_command: any, invocationArgs: string[]) => {
    args = invocationArgs;
    setImmediate(() => child.emit("close", 0));
    return child;
  }) as any);
  await handle.result;
  assert.deepEqual(args.flatMap((arg, index) => arg === "--skill" ? [args[index + 1]] : []), ["/skills/baseline", "/skills/languages/golang"]);
});

test("startSubagent prepends the resume preamble and no-stash discipline for durable worktrees", async () => {
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-resume-"));
  execFileSync("git", ["init", "-q"], { cwd });
  const worktree = join(cwd, ".pi", "worktrees", "wip-preamble-pack");
  mkdirSync(worktree, { recursive: true });
  execFileSync("git", ["init", "-q"], { cwd: worktree });
  writeFileSync(join(worktree, "partial.txt"), "partial\n");
  let taskArg = "";
  const handle = startSubagent({
    agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "worker.md" },
    task: "implement the thing",
    cwd,
    stage: "worker",
    acceptance: "checked",
    durableWorktreeKey: "wip-preamble-pack",
    resumeFailureMode: "prior attempt died before emitting its completion report",
    preparedWorktree: worktree,
  }, undefined, ((_command: any, invocationArgs: string[]) => {
    taskArg = invocationArgs.find((arg) => arg.startsWith("Task: ")) || "";
    setImmediate(() => child.emit("close", 0));
    return child;
  }) as any);
  await handle.result;
  assert.match(taskArg, /^Task: RESUME: A prior attempt of this task died in the retained worktree \(prior attempt died before emitting its completion report\)/);
  assert.match(taskArg, /partial\.txt/); // git status sample from the retained worktree
  assert.match(taskArg, /fresh deadline budget/);
  assert.match(taskArg, /NEVER run git stash or git stash pop/);
  assert.match(taskArg, /implement the thing/);
});

test("read-only agent verdict is invalidated when it mutates repository state", async () => {
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-readonly-"));
  execFileSync("git", ["init", "-q"], { cwd });
  const handle = startSubagent({
    agent: { name: "task-reviewer", description: "", tools: ["read", "bash"], systemPrompt: "", source: "packaged", filePath: "task-reviewer.md" },
    task: "review",
    cwd,
  }, undefined, ((_command: any, _args: any, options: any) => {
    setImmediate(() => {
      writeFileSync(join(options.cwd, "unauthorized.txt"), "changed\n");
      child.emit("close", 0);
    });
    return child;
  }) as any);
  const result = await handle.result;
  assert.equal(result.exitCode, 1);
  assert.match(result.errorMessage || "", /read_only_repository_mutation/);
});

test("read-only agent ignores task database runtime changes", async () => {
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-readonly-db-"));
  execFileSync("git", ["init", "-q"], { cwd });
  writeFileSync(join(cwd, ".gitignore"), ".pi/\n.pi-subagents/\ntasks.db*\n");
  mkdirSync(join(cwd, ".pi"), { recursive: true });
  const handle = startSubagent({
    agent: { name: "task-scout", description: "", tools: ["read", "bash"], systemPrompt: "", source: "packaged", filePath: "task-scout.md" },
    task: "scan",
    cwd,
  }, undefined, ((_command: any, _args: any, options: any) => {
    setImmediate(() => {
      writeFileSync(join(options.cwd, ".pi", "tasks.db-wal"), "runtime\n", { flag: "a" });
      child.emit("close", 0);
    });
    return child;
  }) as any);
  const result = await handle.result;
  assert.notEqual(result.errorMessage?.includes("read_only_repository_mutation"), true);
});
test("sessionPath replaces --no-session with pi create-or-continue session binding", async () => {
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-runner-"));
  const sessionPath = join(cwd, ".pi", "runtime", "runs", "wip-sessionpack", "session.jsonl");
  let args: string[] = [];
  const handle = startSubagent({
    agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "task-worker.md" },
    task: "work",
    cwd,
    sessionPath,
  }, undefined, ((_command: any, invocationArgs: string[]) => {
    args = invocationArgs;
    setImmediate(() => child.emit("close", 0));
    return child;
  }) as any);
  await handle.result;
  assert.ok(!args.includes("--no-session"), "session-bound workers must not run with --no-session");
  const index = args.indexOf("--session");
  assert.equal(args[index + 1], sessionPath);
});

test("corrupt worker session file is removed so the relaunch falls back to fresh", async () => {
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-runner-"));
  const sessionPath = join(cwd, ".pi", "runtime", "runs", "wip-corrupt", "session.jsonl");
  mkdirSync(dirname(sessionPath), { recursive: true });
  writeFileSync(sessionPath, "this is not a jsonl session file");
  let args: string[] = [];
  const handle = startSubagent({
    agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "task-worker.md" },
    task: "work",
    cwd,
    sessionPath,
  }, undefined, ((_command: any, invocationArgs: string[]) => {
    args = invocationArgs;
    setImmediate(() => child.emit("close", 0));
    return child;
  }) as any);
  await handle.result;
  const index = args.indexOf("--session");
  assert.equal(args[index + 1], sessionPath);
  assert.ok(!existsSync(sessionPath), "corrupt session file must be deleted before pi recreates it fresh");
});

test("a valid existing worker session file is preserved for continuation", async () => {
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-runner-"));
  const sessionPath = join(cwd, ".pi", "runtime", "runs", "wip-resume", "session.jsonl");
  mkdirSync(dirname(sessionPath), { recursive: true });
  writeFileSync(sessionPath, '{"type":"session"}\n');
  let args: string[] = [];
  const handle = startSubagent({
    agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "task-worker.md" },
    task: "work",
    cwd,
    sessionPath,
  }, undefined, ((_command: any, invocationArgs: string[]) => {
    args = invocationArgs;
    setImmediate(() => child.emit("close", 0));
    return child;
  }) as any);
  await handle.result;
  assert.equal(readFileSync(sessionPath, "utf8"), '{"type":"session"}\n');
});

test("session bound to a vanished recorded working directory is set aside and relaunch falls back to fresh", async () => {
  const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-runner-"));
  const sessionPath = join(cwd, ".pi", "runtime", "runs", "wip-gonecwd", "session.jsonl");
  mkdirSync(dirname(sessionPath), { recursive: true });
  const vanished = join(cwd, "gone-worktree");
  writeFileSync(sessionPath, JSON.stringify({ type: "session", version: 3, id: "x", cwd: vanished }) + "\n");
  let args: string[] = [];
  const handle = startSubagent({
    agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "task-worker.md" },
    task: "work",
    cwd,
    sessionPath,
  }, undefined, ((_command: any, invocationArgs: string[]) => {
    args = invocationArgs;
    setImmediate(() => child.emit("close", 0));
    return child;
  }) as any);
  await handle.result;
  const index = args.indexOf("--session");
  assert.equal(args[index + 1], sessionPath);
  assert.ok(!existsSync(sessionPath), "session with a dead recorded cwd must not be resumed in place");
  const stale = readdirSync(dirname(sessionPath)).find((name) => name.startsWith("session.jsonl.stale-"));
  assert.ok(stale, "the stale session must be preserved aside, not deleted");
});

function spawnWithOutcomes(outcomes: Array<string | null>, exitCode = 0) {
  let index = 0;
  return (_command: any, _args: any) => {
    const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
    const output = outcomes[Math.min(index++, outcomes.length - 1)];
    setImmediate(() => {
      if (output) child.stdout.emit("data", `${output}\n`);
      child.emit("close", exitCode);
    });
    return child;
  };
}

const VALID_OUTPUT = '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}';

test("runner classifies transient provider faults before XML validation", () => {
  // Empty output: a completed child with no assistant text.
  const base = { runId: "r", agent: { name: "task-worker" }, task: "work", cwd: "/repo", exitCode: 0, messages: [], stderr: "", usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, cost: 0, contextTokens: 0, turns: 0 } };
  assert.equal(classifyRunnerTransientFault({ ...base, messages: [] }), "empty_output");
  // Empty output with a nonzero exit and no matching diagnostic is still transient:
  // empty assistant output is classified independently of exit code.
  assert.equal(classifyRunnerTransientFault({ ...base, exitCode: 1, messages: [], stderr: "runtime: goroutine stack exceeds", errorMessage: "child exited with code 1" }), "empty_output");
  // Valid XML output path: assistant text present => never transient.
  assert.equal(classifyRunnerTransientFault({ ...base, messages: [{ role: "assistant", content: [{ type: "text", text: "<review_report status='passed'/>" }] }] }), "none");
  // Inference-abort provider error.
  assert.equal(classifyRunnerTransientFault({ ...base, exitCode: 1, errorMessage: "inference-abort: upstream provider closed the stream", stderr: "" }), "inference_abort");
  assert.equal(classifyRunnerTransientFault({ ...base, exitCode: 1, stderr: "provider error: empty model response" }), "inference_abort");
  // User aborts are never transient, even with empty output.
  assert.equal(classifyRunnerTransientFault({ ...base, stopReason: "aborted", exitCode: 1 }), "none");
});

test("runner resilient retry keeps a valid first output without consuming retries", async () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-resilient-valid-"));
  const handle = startSubagentResilient({ agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "worker.md" }, task: "work", cwd, transientBackoffMs: 1 }, undefined, spawnWithOutcomes([VALID_OUTPUT]));
  const result = await handle.result;
  assert.equal(result.exitCode, 0);
  assert.equal(result.failureCode, undefined);
  assert.equal(finalAssistantText(result.messages), "done");
});

test("runner resilient retry recovers from empty output inside the same claim", async () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-resilient-recover-"));
  // First two attempts produce empty output; the third succeeds => exactly 2 in-claim retries.
  const handle = startSubagentResilient({ agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "worker.md" }, task: "work", cwd, transientBackoffMs: 1 }, undefined, spawnWithOutcomes([null, null, VALID_OUTPUT]));
  const result = await handle.result;
  assert.equal(result.exitCode, 0);
  assert.equal(result.failureCode, undefined);
  assert.equal(finalAssistantText(result.messages), "done");
});

test("runner resilient retry exhausts at the boundary and tags transient_provider", async () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-subagent-resilient-exhaust-"));
  // All attempts empty => RUNNER_TRANSIENT_RETRIES + 1 calls, tagging on exhaustion.
  assert.equal(RUNNER_TRANSIENT_RETRIES, 2);
  let calls = 0;
  const handle = startSubagentResilient({ agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "worker.md" }, task: "work", cwd, transientBackoffMs: 1 }, undefined, ((_command: any, _args: any, _options: any) => {
    calls++;
    const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
    setImmediate(() => child.emit("close", 0));
    return child;
  }) as any);
  const result = await handle.result;
  assert.equal(calls, RUNNER_TRANSIENT_RETRIES + 1);
  assert.equal(result.failureCode, "transient_provider");
  assert.match(result.errorMessage || "", /without consuming a numbered attempt/);
});

test("worktrees are created under repo/.pi/worktrees/<runId> and removed through ownership checks", async () => {
  const repo = mkdtempSync(join(tmpdir(), "task-subagent-worktree-location-"));
  execFileSync("git", ["init", "-q"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "work.go"), "package work\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  const prepared = await prepareSubagentWorktree(repo, undefined, "loc-test");
  assert.equal(prepared.cwd, join(repo, ".pi", "worktrees", "loc-test"));
  assert.ok(existsSync(prepared.cwd));
  try {
    removeSubagentWorktree(repo, prepared.cwd, "loc-test");
    assert.ok(!existsSync(prepared.cwd));
  } finally {
    rmSync(join(repo, ".pi", "worktrees", "loc-test"), { recursive: true, force: true });
  }
});

test("an in-claim retry restores the rejected candidate patch to the isolated worktree", async () => {
  const repo = mkdtempSync(join(tmpdir(), "task-subagent-resilient-candidate-"));
  execFileSync("git", ["init", "-q"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "work.go"), "package work\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  // Attest the rejected-candidate patch against the base without committing it.
  writeFileSync(join(repo, "work.go"), "package work\n\nconst CANDIDATE = 1\n");
  execFileSync("git", ["add", "work.go"], { cwd: repo });
  const patch = join(repo, "candidate.patch");
  writeFileSync(patch, execFileSync("git", ["diff", "--cached"], { cwd: repo, encoding: "utf8" }));
  execFileSync("git", ["reset", "-q"], { cwd: repo });
  writeFileSync(join(repo, "work.go"), "package work\n");
  const prepared = await prepareSubagentWorktree(repo, patch, "retry-cand");
  try {
    let calls = 0;
    let candidateRestored = false;
    let untrackedRemoved = false;
    const handle = startSubagentResilient(
      { agent: { name: "task-worker", description: "", systemPrompt: "", source: "packaged", filePath: "worker.md" }, task: "work", cwd: repo, isolation: "worktree", preparedWorktree: prepared.cwd, initialPatchPath: patch, transientBackoffMs: 1 },
      undefined,
      ((_command: any, _args: any, options: any) => {
        calls++;
        const child = Object.assign(new EventEmitter(), { stdout: new EventEmitter(), stderr: new EventEmitter(), kill: () => true });
        setImmediate(() => {
          if (calls === 1) {
            // A failed attempt left an untracked artifact behind: the retry reset
            // must remove it so the retry starts from pure candidate state.
            writeFileSync(join(options.cwd as string, "retry-artifact.txt"), "stale");
            // Empty output: a transient fault that triggers the reset+reapply retry.
            child.emit("close", 0);
          } else {
            untrackedRemoved = !existsSync(join(options.cwd as string, "retry-artifact.txt"));
            candidateRestored = readFileSync(join(options.cwd as string, "work.go"), "utf8").includes("CANDIDATE");
            child.stdout.emit("data", `${VALID_OUTPUT}\n`);
            child.emit("close", 0);
          }
        });
        return child;
      }) as any,
    );
    const result = await handle.result;
    assert.equal(calls, 2);
    assert.equal(candidateRestored, true, "candidate patch must be reapplied after the retry reset");
    assert.equal(untrackedRemoved, true, "untracked artifacts from the failed attempt must not survive the retry reset");
    assert.equal(result.failureCode, undefined);
    assert.equal(finalAssistantText(result.messages), "done");
  } finally {
    removeSubagentWorktree(repo, prepared.cwd, "retry-cand");
    rmSync(join(repo, ".pi", "worktrees", "retry-cand"), { recursive: true, force: true });
  }
});
