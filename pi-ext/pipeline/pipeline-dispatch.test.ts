import assert from "node:assert/strict";
import { existsSync, mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { bindPipelineDispatch, findPipelineDispatch, listPipelineDispatches, writePipelineDispatch, writePipelineOutputLog, writePipelineStatus, type PipelineDispatch } from "./pipeline-dispatch.ts";

function dispatchDir(cwd: string, runId: string): string {
  return join(cwd, ".pi-subagents", "pipeline", runId);
}

function makeDispatch(runId: string, cwd: string): PipelineDispatch {
  return {
    runId,
    leaseToken: `lease-${runId}`,
    agent: "task-worker",
    stage: "worker",
    taskId: "wi-test",
    task: "Do the thing",
    asyncDir: dispatchDir(cwd, runId),
    worktree: "/tmp/pi-agent-dispatch-test",
  };
}

test("write + list roundtrip: pending dispatches are listed, bound ones are not", () => {
  const cwd = mkdtemp("dispatch-list-");
  const first = makeDispatch("pr-1", cwd);
  const second = makeDispatch("pr-2", cwd);
  writePipelineDispatch(first);
  writePipelineDispatch(second);
  assert.deepEqual(listPipelineDispatches(cwd).map((d) => d.runId).sort(), ["pr-1", "pr-2"]);
  bindPipelineDispatch(first, "83c4e9d8-9227");
  assert.deepEqual(listPipelineDispatches(cwd).map((d) => d.runId), ["pr-2"]);
  // find-by-run works regardless of bind state
  assert.equal(findPipelineDispatch(cwd, "pr-1")?.leaseToken, "lease-pr-1");
});

test("bind hard gate: empty agent id is rejected, no bind marker written", () => {
  const cwd = mkdtemp("dispatch-bind-");
  const dispatch = makeDispatch("pr-3", cwd);
  writePipelineDispatch(dispatch);
  assert.throws(() => bindPipelineDispatch(dispatch, ""), /non-empty Agent tool id/);
  assert.throws(() => bindPipelineDispatch(dispatch, "   "), /non-empty Agent tool id/);
  assert.equal(existsSync(join(dispatch.asyncDir, "bind.json")), false);
  // valid id binds once; double bind is rejected
  bindPipelineDispatch(dispatch, "abc-123");
  assert.throws(() => bindPipelineDispatch(dispatch, "abc-123"), /already bound/);
  assert.equal(JSON.parse(readFileSync(join(dispatch.asyncDir, "bind.json"), "utf8")).agentId, "abc-123");
});

test("terminal report writes output log and status shapes the reconcile loop consumes", () => {
  const cwd = mkdtemp("dispatch-report-");
  const dispatch = makeDispatch("pr-4", cwd);
  writePipelineDispatch(dispatch);
  writePipelineOutputLog(dispatch, { completed: true, output: "<task_completion_report>done</task_completion_report>" });
  writePipelineStatus(dispatch, { completed: true, output: "x" });
  const status = JSON.parse(readFileSync(join(dispatch.asyncDir, "status.json"), "utf8"));
  assert.equal(status.state, "completed");
  assert.equal(status.error, "");
  assert.equal(status.failure_code, "");
  assert.equal(status.steps[0].status, "completed");
  assert.match(readFileSync(join(dispatch.asyncDir, "output-0.log"), "utf8"), /task_completion_report/);

  writePipelineStatus(dispatch, { completed: false, output: "", error: "provider timeout", failureCode: "transient_provider" });
  const failed = JSON.parse(readFileSync(join(dispatch.asyncDir, "status.json"), "utf8"));
  assert.equal(failed.state, "failed");
  assert.equal(failed.error, "provider timeout");
  assert.equal(failed.failure_code, "transient_provider");
});

function mkdtemp(prefix: string): string {
  const dir = join("/tmp", `${prefix}${Math.random().toString(36).slice(2)}`);
  mkdirSync(dir, { recursive: true });
  return dir;
}
