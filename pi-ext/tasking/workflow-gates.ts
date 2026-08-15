import { requiresDesignApproval } from "./workflow-modes.ts";

export interface WorkflowGateTask {
  workflow_mode?: string;
  design_status?: string;
  title?: string;
}

export interface WorkflowGateScanReport {
  id?: string;
  summary?: string;
  status?: string;
}

export interface WorkflowGateRriSession {
  id?: string;
  status?: string;
}

export interface WorkflowGateDependency {
  depends_on_task_id?: string;
  dependency_type?: string;
  status?: string;
  review_status?: string;
  title?: string;
}

export interface WorkflowGatePhaseMetadata {
  execution_policy?: string;
  can_start_before_previous?: number | boolean;
}

/**
 * Return whether implementation is blocked by an unapproved design gate.
 * Expects task workflow metadata and returns true for designed/full tasks whose
 * design_status is not approved.
 */
export function isTaskDesignBlocked(task: WorkflowGateTask): boolean {
  return requiresDesignApproval(task.workflow_mode) && task.design_status !== "approved";
}

/**
 * Return whether implementation is blocked by missing scan context.
 * Expects task workflow metadata and optional scan reports; returns true when
 * the orchestrator has no persisted mode-appropriate scan evidence to hand off.
 */
export function isTaskScanBlocked(_task: WorkflowGateTask, scanReports: WorkflowGateScanReport[] | undefined): boolean {
  return !Array.isArray(scanReports) || scanReports[0]?.status !== "completed";
}

export function isTaskRriBlocked(rriSessions: WorkflowGateRriSession[] | undefined): boolean {
  return !Array.isArray(rriSessions) || rriSessions[0]?.status !== "completed";
}

/**
 * Return dependency rows that block phase execution.
 * Expects dependency rows enriched with dependency task status/review state and
 * phase metadata; returns only unsatisfied phase/blocking dependencies.
 */
export function getBlockingTaskDependencies(
  dependencies: WorkflowGateDependency[] | undefined,
  phaseMetadata?: WorkflowGatePhaseMetadata | null,
): WorkflowGateDependency[] {
  const dependencyList = Array.isArray(dependencies) ? dependencies : [];
  void phaseMetadata;

  return dependencyList.filter((dependency) => {
    const type = dependency.dependency_type || "blocks";
    if (type === "related") return false;
    const statusSatisfied = dependency.status === "done";
    const reviewSatisfied = type === "phase" ? dependency.review_status === "passed" : !dependency.review_status || dependency.review_status === "passed";
    return !(statusSatisfied && reviewSatisfied);
  });
}

/**
 * Return the first blocking reason for starting implementation work.
 * Expects a task and optional dependency/phase metadata; returns a user-facing
 * message when scan, design, or phase dependency gates are not satisfied.
 */
export function getTaskWorkBlockReason(
  task: WorkflowGateTask,
  dependencies?: WorkflowGateDependency[],
  phaseMetadata?: WorkflowGatePhaseMetadata | null,
  scanReports?: WorkflowGateScanReport[],
  rriSessions?: WorkflowGateRriSession[],
  designs?: Array<{ id?: string; status?: string }>,
): string | null {
  const taskTitle = task.title || "this task";
  if (scanReports !== undefined && isTaskScanBlocked(task, scanReports)) {
    return `Work Item "${taskTitle}" requires persisted scan evidence before work. Publish a scan artifact with save_work_item_artifact and approve it with approve_work_item_artifact first.`;
  }

  if (rriSessions !== undefined && isTaskRriBlocked(rriSessions)) {
    return `Task "${taskTitle}" requires a completed persisted RRI session before work.`;
  }

  if (designs !== undefined && designs[0]?.status !== "approved") {
    return `Task "${taskTitle}" requires a persisted approved design before work.`;
  }

  if (isTaskDesignBlocked(task)) {
    return `Task "${taskTitle}" requires approved design before work.`;
  }

  const blockingDependencies = getBlockingTaskDependencies(dependencies, phaseMetadata);
  if (blockingDependencies.length > 0) {
    const dependencyList = blockingDependencies.map((dependency) => dependency.title || dependency.depends_on_task_id || "previous phase").join(", ");
    return `Task "${taskTitle}" is blocked by incomplete phase dependency: ${dependencyList}.`;
  }

  return null;
}
