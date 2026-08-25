import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { EventEmitter } from "node:events";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import { assertManagedAcceptance, buildPiInvocation, finalAssistantText, parseJsonEvent, prepareSubagentWorktree, removeSubagentWorktree, startSubagent } from "./runner.ts";
import { AgentRunTracker } from "./tracker.ts";

process.env.PI_TASK_HERDR_PANEL = "0";

test("managed pipeline stages require their acceptance contract", () => {
  const base = { agent: { name: "task-worker" }, task: "work", cwd: "/repo" } as any;
  assert.doesNotThrow(() => assertManagedAcceptance({ ...base, stage: "worker", acceptance: "checked" }));
  assert.doesNotThrow(() => assertManagedAcceptance({ ...base, stage: "review", acceptance: "attested" }));
  assert.throws(() => assertManagedAcceptance({ ...base, stage: "worker", acceptance: "attested" }), /requires acceptance checked/);
  assert.throws(() => assertManagedAcceptance({ ...base, stage: "review" }), /requires acceptance attested/);
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
  assert.throws(() => readFileSync(join(tmpdir(), "pi-task-worktree-invalid-test", "work.go"), "utf8"));
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
