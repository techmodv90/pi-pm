import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { AgentRunTracker } from "./tracker.ts";

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
  assert.equal(tracker.list().length, 1);
  assert.equal(tracker.stop("run-12345678"), true);
  assert.equal(stopped, true);

  const dir = join(cwd, ".pi-subagents", "runs", "run-12345678");
  assert.equal(readFileSync(join(dir, "prompt.txt"), "utf8"), "Fix the deploy race");
  assert.match(readFileSync(join(dir, "events.jsonl"), "utf8"), /"type":"tool"/);
  assert.equal(statSync(join(dir, "prompt.txt")).mode & 0o777, 0o600);
});

test("tracker retains completed runs after finish", () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-agent-tracker-"));
  const tracker = new AgentRunTracker();
  tracker.start({ runId: "run-1", agent: "task-scout", task: "Inspect", cwd });
  tracker.finish("run-1", "completed");

  assert.equal(tracker.get("run-1")?.status, "completed");
});

test("tracker sync salvages a done completion report from a dead process instead of failing the run", () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-agent-tracker-"));
  const tracker = new AgentRunTracker();
  tracker.start({ runId: "salvage", agent: "task-worker", task: "Build", cwd });
  tracker.event("salvage", "message", 'All verification complete. <completion_report tip_id="wip-1" version="1" status="done">...</completion_report>');
  tracker.setPid("salvage", 2_147_483_647);

  tracker.sync(cwd);

  assert.equal(tracker.get("salvage")?.status, "completed");
  assert.match(tracker.get("salvage")?.terminalReason || "", /salvaged/);
});

test("tracker sync salvages a terminal review verdict from a dead process instead of failing the run", () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-agent-tracker-"));
  const tracker = new AgentRunTracker();
  tracker.start({ runId: "salvage-review", agent: "task-reviewer", task: "Review", cwd });
  tracker.event("salvage-review", "message", '<review_report status="passed"><notes>ok</notes></review_report>');
  tracker.setPid("salvage-review", 2_147_483_647);

  tracker.sync(cwd);

  assert.equal(tracker.get("salvage-review")?.status, "completed");
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
});

test("tracker touch refreshes the heartbeat without making a streaming run terminal", () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-agent-tracker-touch-"));
  const tracker = new AgentRunTracker();
  tracker.start({ runId: "streaming", agent: "task-worker", task: "Build", cwd });
  const startedAt = tracker.get("streaming")!.startedAt;

  tracker.touch("streaming");

  const run = tracker.get("streaming")!;
  assert.ok(run.heartbeatAt! >= startedAt);
  assert.equal(run.status, "running");

  tracker.finish("streaming", "completed");
  tracker.touch("streaming");
  assert.notEqual(tracker.get("streaming")?.status, "running");
});

test("tracker records observed lifecycle states on the run", () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-agent-tracker-"));
  const tracker = new AgentRunTracker();
  tracker.start({ runId: "lifecycle", agent: "task-worker", task: "Build", cwd });

  tracker.observeLifecycle("lifecycle", "active", "using bash");
  const run = tracker.get("lifecycle")!;
  assert.equal(run.lifecycleState, "active");
  assert.equal(run.lifecycleDetail, "using bash");

  tracker.observeLifecycle("lifecycle", "finalizing");
  assert.equal(tracker.get("lifecycle")?.lifecycleState, "finalizing");
  assert.equal(tracker.get("lifecycle")?.status, "running");
});

test("tracker syncs nested persona runs under their RRI parent", () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-agent-tracker-"));
  const parent = new AgentRunTracker();
  const child = new AgentRunTracker();
  parent.start({ runId: "rri-parent", agent: "rri-persona", task: "Prepare interview", cwd });
  child.start({ runId: "persona-child", parentRunId: "rri-parent", agent: "rri-persona", task: "End User", cwd });

  parent.sync(cwd);

  assert.equal(parent.get("persona-child")?.parentRunId, "rri-parent");
});

test("late events for a cancelled run with deleted worktree do not throw", () => {
  const cwd = mkdtempSync(join(tmpdir(), "task-agent-tracker-"));
  const tracker = new AgentRunTracker();
  tracker.start({ runId: "run-cancelled1", agent: "task-worker", task: "Work", cwd });
  rmSync(cwd, { recursive: true, force: true });
  assert.doesNotThrow(() => {
    tracker.event("run-cancelled1", "message", "thinking");
    tracker.setModel("run-cancelled1", "model-x");
  });
});
