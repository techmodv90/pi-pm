import test from "node:test";
import assert from "node:assert/strict";
import { getBlockingTaskDependencies, getTaskWorkBlockReason, isTaskRriBlocked, isTaskScanBlocked } from "./workflow-gates.ts";

test("getBlockingTaskDependencies blocks incomplete strict phase dependencies", () => {
  assert.deepEqual(getBlockingTaskDependencies([
    { title: "Phase 1", dependency_type: "phase", status: "open" },
  ], { execution_policy: "strict_sequential" }).map((dependency) => dependency.title), ["Phase 1"]);

  assert.deepEqual(getBlockingTaskDependencies([
    { title: "Phase 1", dependency_type: "phase", status: "done", review_status: "" },
  ], { execution_policy: "strict_sequential" }).map((dependency) => dependency.title), ["Phase 1"]);

  assert.deepEqual(getBlockingTaskDependencies([
    { title: "Phase 1", dependency_type: "phase", status: "done", review_status: "passed" },
  ], { execution_policy: "strict_sequential" }), []);

  assert.deepEqual(getBlockingTaskDependencies([
    { title: "Phase 1", dependency_type: "phase", status: "open" },
  ], { execution_policy: "parallel_allowed" }).map((dependency) => dependency.title), ["Phase 1"]);
});

test("isTaskScanBlocked requires persisted scan for every mode", () => {
  assert.equal(isTaskScanBlocked({ workflow_mode: "quick" }, []), true);
  assert.equal(isTaskScanBlocked({ workflow_mode: "standard" }, []), true);
  assert.equal(isTaskScanBlocked({ workflow_mode: "designed" }, undefined), true);
  assert.equal(isTaskScanBlocked({ workflow_mode: "standard" }, [{ id: "sr-1", status: "partial" }]), true);
  assert.equal(isTaskScanBlocked({ workflow_mode: "standard" }, [{ id: "sr-1", status: "failed" }]), true);
  assert.equal(isTaskScanBlocked({ workflow_mode: "standard" }, [{ id: "sr-1", status: "completed" }]), false);
  assert.equal(isTaskScanBlocked({ workflow_mode: "standard" }, [{ id: "sr-2", status: "partial" }, { id: "sr-1", status: "completed" }]), true);
});

test("isTaskRriBlocked requires a completed persisted RRI session", () => {
  assert.equal(isTaskRriBlocked([]), true);
  assert.equal(isTaskRriBlocked([{ status: "interviewing" }]), true);
  assert.equal(isTaskRriBlocked([{ status: "completed" }]), false);
  assert.equal(isTaskRriBlocked([{ status: "interviewing" }, { status: "completed" }]), true);
});

test("getTaskWorkBlockReason prioritizes scan, design, and dependency gates", () => {
  assert.match(
    getTaskWorkBlockReason({ workflow_mode: "standard", title: "Standard" }, [], null, []) || "",
    /save_work_item_artifact.*approve_work_item_artifact/,
  );

  assert.match(
    getTaskWorkBlockReason({ workflow_mode: "quick", title: "Quick" }, [], null, []) || "",
    /scan evidence/,
  );

  assert.match(
    getTaskWorkBlockReason({ workflow_mode: "designed", design_status: "pending", title: "A" }, [], null, [{ id: "sr-1", status: "completed" }], [{ status: "completed" }], [{ id: "des-1", status: "pending" }]) || "",
    /approved design/,
  );

  assert.match(
    getTaskWorkBlockReason({ workflow_mode: "designed", design_status: "approved", title: "B" }, [{ title: "Phase 1", dependency_type: "phase", status: "open" }], { execution_policy: "strict_sequential" }, [{ id: "sr-1", status: "completed" }], [{ status: "completed" }], [{ id: "des-1", status: "approved" }]) || "",
    /incomplete phase dependency: Phase 1/,
  );

  assert.equal(
    getTaskWorkBlockReason({ workflow_mode: "designed", design_status: "approved", title: "C" }, [], null, [{ id: "sr-1", status: "completed" }], [{ status: "completed" }], [{ id: "des-1", status: "approved" }]),
    null,
  );
});
