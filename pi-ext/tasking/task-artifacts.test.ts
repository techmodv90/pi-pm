import assert from "node:assert/strict";
import test from "node:test";
import { mergeParentWorkflowArtifacts } from "./task-artifacts.ts";

test("mergeParentWorkflowArtifacts inherits canonical Work Item artifacts by parent_id", () => {
  const merged = mergeParentWorkflowArtifacts(
    {
      work_item: { id: "child", parent_id: "parent", title: "Child", type: "task" },
      artifacts: [{ id: "child-scan", stage: "scan" }],
    },
    {
      work_item: { id: "parent", title: "Parent", type: "feature" },
      artifacts: [{ id: "parent-scan", stage: "scan" }, { id: "parent-design", stage: "vision" }],
    },
  );

  assert.deepEqual(merged.artifacts.map((artifact: any) => artifact.id), ["child-scan", "parent-scan", "parent-design"]);
  assert.equal(merged.inherited_parent_work_item.id, "parent");
});