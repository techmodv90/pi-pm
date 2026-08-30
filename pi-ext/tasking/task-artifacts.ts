import { execPic } from "../core/cli-helpers.ts";

/**
 * Merge task artifact rows while preserving child-specific rows first.
 * Expects optional child and parent artifact arrays and returns a de-duplicated
 * list keyed by id when present, otherwise by object identity order.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
function mergeArtifactRows(childRows: any[] | undefined, parentRows: any[] | undefined): any[] {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const merged: any[] = [];
  const seen = new Set<string>();
  for (const row of [...(childRows || []), ...(parentRows || [])]) {
    if (!row) continue;
    const key = row.id ? String(row.id) : JSON.stringify(row);
    if (seen.has(key)) continue;
    seen.add(key);
    merged.push(row);
  }
  return merged;
}

// Planning context constraint: return only the artifact selected by the latest
// approved checkpoint so fresh agents cannot consume draft or historical rows.
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function currentApprovedPlanningArtifact(data: any, stage: string): any | undefined {
  const checkpoint = (Array.isArray(data?.checkpoints) ? data.checkpoints : [])
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    .filter((entry: any) => entry.stage === stage)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    .sort((a: any, b: any) => Number(b.artifact_revision || 0) - Number(a.artifact_revision || 0))[0];
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  return (Array.isArray(data?.artifacts) ? data.artifacts : []).find((entry: any) => entry.id === checkpoint?.artifact_id);
}


/**
 * Merge parent planning-task artifacts into a phase/feature child task payload.
 * Expects `pic show` style child and parent data; returns child data enriched
 * with parent scan reports, requirements, approved designs, and trace links.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function mergeParentWorkflowArtifacts(childData: any, parentData: any): any {
  if (!childData?.work_item || !parentData?.work_item) return childData;
  return {
    ...childData,
    inherited_parent_work_item: {
      id: parentData.work_item.id,
      title: parentData.work_item.title,
    },
    artifacts: mergeArtifactRows(childData.artifacts, parentData.artifacts),
  };
}

/**
 * Load and merge parent workflow artifacts for a phase child task.
 * Expects `pic show` style task data and cwd; returns unchanged data when there
 * is no parent phase metadata or the parent cannot be loaded.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function withInheritedParentWorkflowArtifacts(taskData: any, cwd: string): any {
  const parentWorkItemId = taskData?.work_item?.parent_id;
  if (parentWorkItemId) {
    const parentData = execPic(["show", parentWorkItemId], cwd);
    if (!parentData?.work_item) return taskData;
    return mergeParentWorkflowArtifacts(taskData, parentData);
  }
  return taskData;
}
