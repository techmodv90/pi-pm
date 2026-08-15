import assert from "node:assert/strict";
import test from "node:test";
import { assertTaskManagerActionAllowed } from "./agent-capabilities.ts";

test("child agent task-manager capabilities exclude lifecycle authority", () => {
  assert.doesNotThrow(() => assertTaskManagerActionAllowed(undefined, "accept_work_item"));
  assert.doesNotThrow(() => assertTaskManagerActionAllowed("task-reviewer", "trigger_work_item_review"));
  assert.throws(() => assertTaskManagerActionAllowed("task-scout", "save_work_item_artifact", "scan"), /cannot call/);
  assert.throws(() => assertTaskManagerActionAllowed("task-reviewer", "accept_work_item"), /cannot call/);
  assert.throws(() => assertTaskManagerActionAllowed("task-scout", "reset_pipeline_circuit"), /cannot call/);
  assert.throws(() => assertTaskManagerActionAllowed("task-scout", "save_work_item_artifact", "blueprint"), /cannot call/);
  assert.throws(() => assertTaskManagerActionAllowed("task-planner", "approve_work_item_artifact"), /cannot call/);
});