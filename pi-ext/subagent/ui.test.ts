import assert from "node:assert/strict";
import test from "node:test";

import { renderAgentView } from "./ui.ts";
import type { AgentRun } from "./tracker.ts";

const run: AgentRun = {
  runId: "run-12345678",
  agent: "task-worker",
  task: "Fix deployment race without dropping queued work",
  cwd: "/repo",
  stage: "worker",
  taskId: "t-42",
  model: "gpt-test",
  status: "running",
  startedAt: 1_000,
  events: [{ at: 2_000, type: "tool", summary: "bash" }],
};

test("agent view renders run selection, activity, prompt, and details within width", () => {
  const activity = renderAgentView([run], 0, "activity", 72, 20, 61_000);
  assert.match(activity.join("\n"), /task-worker.*t-42.*01:00/);
  assert.match(activity.join("\n"), /tool.*bash/);
  assert.ok(activity.every((line) => line.length <= 72));

  assert.match(renderAgentView([run], 0, "prompt", 72, 20).join("\n"), /Fix deployment race/);
  assert.match(renderAgentView([run], 0, "details", 72, 20).join("\n"), /gpt-test/);
});

test("agent view scrolls the run list with the selected run", () => {
  const runs = Array.from({ length: 8 }, (_, index) => ({ ...run, runId: `run-${index}`, taskId: `t-${index}` }));
  const view = renderAgentView(runs, 7, "activity", 72, 20, 61_000).join("\n");
  assert.match(view, /> starting\s+task-worker t-7/);
  assert.doesNotMatch(view, /task-worker t-0/);
});