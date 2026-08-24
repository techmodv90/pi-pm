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
  assert.throws(() => assertTaskManagerActionAllowed("task-planner", "save_work_item_artifact", "blueprint"), /cannot save blueprint/);
  assert.doesNotThrow(() => assertTaskManagerActionAllowed("task-planner", "save_work_item_artifact", "task_graph"));
  assert.throws(() => assertTaskManagerActionAllowed("task-planner", "save_work_item_artifact", "contracts"), /cannot save contracts/);
  // task-worker and task-debugger are least-privilege observers; they may view
  // read-only state but never mutate workflow lifecycle.
  assert.doesNotThrow(() => assertTaskManagerActionAllowed("task-worker", "show_work_item"));
  assert.doesNotThrow(() => assertTaskManagerActionAllowed("task-debugger", "work_item_workflow_status"));
});

test("unknown child agents are denied by default (defense in depth)", () => {
  assert.throws(() => assertTaskManagerActionAllowed("task-cop", "show_work_item", "scan"), /is not provisioned/);
  assert.throws(() => assertTaskManagerActionAllowed("task-cop", "accept_work_item"), /is not provisioned/);
});

test("task-worker and task-debugger cannot invoke owner, contractor, or scheduler lifecycle actions", () => {
  const lifecycleActions = [
    "accept_work_item", "approve_work_item_artifact", "reset_pipeline_circuit",
    "claim_work_item", "merge_aggregate_work_item", "update_work_item_status",
    "close_aggregate_work_item",
  ];
  for (const child of ["task-worker", "task-debugger"]) {
    for (const action of lifecycleActions) {
      assert.throws(() => assertTaskManagerActionAllowed(child, action), /cannot call/, `${child} must be blocked from ${action}`);
    }
  }
});