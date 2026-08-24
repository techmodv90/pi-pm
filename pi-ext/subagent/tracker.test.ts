import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { AGENT_STALL_AFTER_MS, AgentRunTracker, agentActivityLabel, formatAgentFooter, renderAgentWidget } from "./tracker.ts";

test("tracker exposes live agent state and persists its prompt and events", () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-agent-tracker-"));
  let stopped = false;
  const tracker = new AgentRunTracker();

  tracker.start({ runId: "run-12345678", agent: "task-worker", task: "Fix the deploy race", cwd, stage: "worker", taskId: "t-42", stop: () => { stopped = true; } });
  tracker.event("run-12345678", "tool", "bash");
  tracker.event("run-12345678", "tool", "read");
  tracker.setUsage("run-12345678", { input: 10_000, output: 2_400, cacheRead: 0, cacheWrite: 0, cost: 0, contextTokens: 0, turns: 1 });

  const run = tracker.get("run-12345678");
  assert.equal(run?.status, "running");
  assert.equal(run?.events.at(-1)?.summary, "read");
  assert.equal(formatAgentFooter(tracker.list(), 120), "1 active · 1 open");
  assert.match(renderAgentWidget(tracker.list(), 80).join("\n"), /Fix the deploy race/);
  assert.match(renderAgentWidget(tracker.list(), 80).join("\n"), /using read/);
  assert.match(renderAgentWidget(tracker.list(), 120).join("\n"), /↻ 1 · 0 tok \(i 10k\/o 2\.4k\) · 2 tools/);
  assert.equal(tracker.stop("run-12345678"), true);
  assert.equal(stopped, true);

  const dir = join(cwd, ".pi-subagents", "runs", "run-12345678");
  assert.equal(readFileSync(join(dir, "prompt.txt"), "utf8"), "Fix the deploy race");
  assert.match(readFileSync(join(dir, "events.jsonl"), "utf8"), /"type":"tool"/);
  assert.equal(statSync(join(dir, "prompt.txt")).mode & 0o777, 0o600);
});

test("tracker retains completed runs but removes them from the active footer", () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-agent-tracker-"));
  const tracker = new AgentRunTracker();
  tracker.start({ runId: "run-1", agent: "task-scout", task: "Inspect", cwd });
  tracker.finish("run-1", "completed");

  assert.equal(tracker.get("run-1")?.status, "completed");
  assert.equal(formatAgentFooter(tracker.list(), 80), "");
});

test("tracker sync marks a running agent failed when its persisted process is gone", () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-agent-tracker-"));
  const tracker = new AgentRunTracker();
  tracker.start({ runId: "orphan", agent: "task-worker", task: "Build", cwd });
  tracker.event("orphan", "tool", "bash");
  tracker.setPid("orphan", 2_147_483_647);

  tracker.sync(cwd);

  assert.equal(tracker.get("orphan")?.status, "failed");
  assert.equal(tracker.get("orphan")?.events.at(-1)?.summary, "agent process exited without a terminal result");
  assert.doesNotMatch(renderAgentWidget(tracker.list(), 80).join("\n"), /using bash/);
});

test("tracker marks a live but silent run stalled without making it terminal", () => {
  const run = {
    runId: "silent",
    agent: "task-worker",
    task: "Build",
    cwd: "/tmp",
    status: "running" as const,
    startedAt: 1,
    heartbeatAt: 1,
    activityState: "using bash",
    events: [],
  };

  assert.match(agentActivityLabel(run, 1 + AGENT_STALL_AFTER_MS), /stalled: no activity/);
  assert.equal(run.status, "running");
});

test("tracker projects managed process and turn lifecycle in the live widget", () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-agent-tracker-"));
  const tracker = new AgentRunTracker();
  tracker.start({ runId: "lifecycle", agent: "task-worker", task: "Build", cwd });

  tracker.observeLifecycle("lifecycle", "active", "using bash");
  assert.equal(agentActivityLabel(tracker.get("lifecycle")!), "active · using bash");
  tracker.observeLifecycle("lifecycle", "blocked");
  assert.equal(agentActivityLabel(tracker.get("lifecycle")!), "blocked");
  tracker.observeLifecycle("lifecycle", "waiting");
  assert.equal(agentActivityLabel(tracker.get("lifecycle")!), "waiting");
  tracker.observeLifecycle("lifecycle", "interrupted");
  assert.equal(agentActivityLabel(tracker.get("lifecycle")!), "interrupted");
  tracker.observeLifecycle("lifecycle", "finalizing");

  const run = tracker.get("lifecycle")!;
  assert.equal(run.status, "running");
  assert.equal(agentActivityLabel(run), "finalizing");
  assert.match(renderAgentWidget([run], 80).join("\n"), /finalizing/);
});

test("tracker syncs nested persona runs under their RRI parent", () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-agent-tracker-"));
  const parent = new AgentRunTracker();
  const child = new AgentRunTracker();
  parent.start({ runId: "rri-parent", agent: "rri-persona", task: "Prepare interview", cwd });
  child.start({ runId: "persona-child", parentRunId: "rri-parent", agent: "rri-persona", task: "End User", cwd });

  parent.sync(cwd);

  assert.equal(parent.get("persona-child")?.parentRunId, "rri-parent");
  const widget = renderAgentWidget(parent.list(), 120).join("\n");
  assert.match(widget, /rri-persona/);
  assert.match(widget, /rri-persona.*End User/);
});test("late events for a cancelled run with deleted worktree do not throw", () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-agent-tracker-"));
  const tracker = new AgentRunTracker();
  tracker.start({ runId: "run-cancelled1", agent: "task-worker", task: "Work", cwd });
  rmSync(cwd, { recursive: true, force: true });
  assert.doesNotThrow(() => {
    tracker.event("run-cancelled1", "message", "thinking");
    tracker.setModel("run-cancelled1", "model-x");
  });
});
