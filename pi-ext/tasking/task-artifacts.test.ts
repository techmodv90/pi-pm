import assert from "node:assert/strict";
import test from "node:test";
import { currentApprovedPlanningArtifact, mergeParentWorkflowArtifacts } from "./task-artifacts.ts";

test("currentApprovedPlanningArtifact returns only the checkpoint-bound revision", () => {
  const data = {
    checkpoints: [
      { stage: "vision", artifact_id: "approved", artifact_revision: 2 },
      { stage: "vision", artifact_id: "old", artifact_revision: 1 },
    ],
    artifacts: [
      { id: "old", stage: "vision", revision: 1, content: "old" },
      { id: "approved", stage: "vision", revision: 2, content: "approved" },
      { id: "draft", stage: "vision", revision: 3, content: "unapproved" },
    ],
  };
  assert.equal(currentApprovedPlanningArtifact(data, "vision")?.id, "approved");
  assert.equal(currentApprovedPlanningArtifact(data, "contracts"), undefined);
});


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