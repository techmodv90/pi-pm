export type WorkflowMode = "quick" | "standard" | "designed" | "full";
export type DesignStatus = "" | "pending" | "approved" | "rejected";
export type OwnerStatus = "" | "pending" | "accepted" | "rejected";

// Planning profile constraint: the durable planning stages the scheduler may
// dispatch for a Work Item, derived in the same order as the persisted Plan
// profile. Contracts stays a contractor main-session gate and never launches a
// bounded child agent, so it is listed only as a known planning stage.
export const PLANNING_STAGES = ["scan", "rri", "vision", "blueprint", "contracts", "task_graph"] as const;
export type PlanningStage = typeof PLANNING_STAGES[number];

export const PLANNING_DEPTHS = ["quick", "standard", "designed", "full"] as const;
export type PlanningDepth = typeof PLANNING_DEPTHS[number];

export const WORKFLOW_MODES: WorkflowMode[] = ["quick", "standard", "designed", "full"];
export const DESIGN_STATUSES: DesignStatus[] = ["", "pending", "approved", "rejected"];
export const OWNER_STATUSES: OwnerStatus[] = ["", "pending", "accepted", "rejected"];

const MODE_RANK: Record<WorkflowMode, number> = {
  quick: 0,
  standard: 1,
  designed: 2,
  full: 3,
};

const DESIGNED_KEYWORDS = [
  "feature",
  "pivot",
  "database",
  "schema",
  "migration",
  "auth",
  "permission",
  "security",
  "payment",
  "delete",
  "destructive",
  "architecture",
  "refactor",
  "workflow",
  "lifecycle",
  "model handoff",
  "agent orchestration",
  "new command",
  "new subsystem",
];

const FULL_KEYWORDS = [
  "redesign",
  "rewrite",
  "large feature",
  "multi-agent",
  "multi-session",
  "end-to-end system",
];

/**
 * Normalize an arbitrary workflow mode value to a supported mode.
 * Expects an unknown user/database value and returns a safe default when the
 * value is missing or unsupported.
 */
export function normalizeWorkflowMode(mode: unknown): WorkflowMode {
  return typeof mode === "string" && WORKFLOW_MODES.includes(mode as WorkflowMode)
    ? mode as WorkflowMode
    : "standard";
}

/**
 * Normalize a persisted planning depth to a supported depth.
 * Planning depth defaults to 'full' to match the persisted schema, distinct
 * from the 'standard' default used for interactive workflow-mode gating.
 */
export function normalizePlanningDepth(depth: unknown): PlanningDepth {
  return typeof depth === "string" && PLANNING_DEPTHS.includes(depth as PlanningDepth)
    ? depth as PlanningDepth
    : "full";
}

/**
 * Select the durable planning stages for a Work Item from its kind, parent, and
 * planning depth. Mirrors the persisted Plan profile contract: RRI and Task
 * Graph are always selected; Vision, Blueprint, and Contracts are depth-gated;
 * a standalone leaf (task/bug/chore with no parent) never selects aggregate-only
 * design stages. This is a fallback for the scheduler before a Plan profile is
 * persisted; once persisted, the profile is the sole authority.
 */
export function planStagesForProfile(kind: unknown, parentId: unknown, depth: unknown): PlanningStage[] {
  const normalizedKind = String(kind || "");
  const normalizedParent = String(parentId || "");
  if (["task", "bug", "chore"].includes(normalizedKind) && !normalizedParent) {
    return ["scan", "rri", "task_graph"];
  }
  switch (normalizePlanningDepth(depth)) {
    case "full":
      return ["scan", "rri", "vision", "blueprint", "contracts", "task_graph"];
    case "designed":
      return ["scan", "rri", "blueprint", "task_graph"];
    default: // quick, standard
      return ["scan", "rri", "task_graph"];
  }
}

/**
 * Return true when a planning stage is known to the scheduler plan registry.
 */
export function isKnownPlanningStage(stage: unknown): stage is PlanningStage {
  return typeof stage === "string" && PLANNING_STAGES.includes(stage as PlanningStage);
}

/**
 * Normalize a workflow confidence score into the 0..1 range.
 * Expects an unknown numeric value and returns 0 when invalid.
 */
export function normalizeWorkflowConfidence(confidence: unknown): number {
  if (typeof confidence !== "number" || Number.isNaN(confidence)) return 0;
  return Math.min(1, Math.max(0, confidence));
}

/**
 * Normalize a design status value for workflow gates.
 * Expects an unknown value and returns an empty status for unsupported input.
 */
export function normalizeDesignStatus(status: unknown): DesignStatus {
  return typeof status === "string" && DESIGN_STATUSES.includes(status as DesignStatus)
    ? status as DesignStatus
    : "";
}

/**
 * Normalize an owner status value for workflow gates.
 * Expects an unknown value and returns an empty status for unsupported input.
 */
export function normalizeOwnerStatus(status: unknown): OwnerStatus {
  return typeof status === "string" && OWNER_STATUSES.includes(status as OwnerStatus)
    ? status as OwnerStatus
    : "";
}

/**
 * Return true when a workflow mode requires an approved design before work.
 * Expects a workflow mode value and returns whether implementation should be
 * blocked until design_status is approved.
 */
export function requiresDesignApproval(mode: unknown): boolean {
  const normalized = normalizeWorkflowMode(mode);
  return normalized === "designed" || normalized === "full";
}

/**
 * Return the heavier of two workflow modes.
 * Expects two workflow modes and returns the one with the higher process level.
 */
export function maxWorkflowMode(a: WorkflowMode, b: WorkflowMode): WorkflowMode {
  return MODE_RANK[a] >= MODE_RANK[b] ? a : b;
}

/**
 * Suggest a minimum workflow mode from deterministic risk keywords.
 * Expects task title and optional description; returns a conservative floor that
 * LLM classification should not go below for risky tasks.
 */
export function suggestMinimumWorkflowMode(title: string, description = ""): WorkflowMode {
  const text = `${title}\n${description}`.toLowerCase();
  if (FULL_KEYWORDS.some((keyword) => text.includes(keyword))) return "full";
  if (DESIGNED_KEYWORDS.some((keyword) => text.includes(keyword))) return "designed";
  return "quick";
}

/**
 * Format a compact badge for workflow mode display in task lists.
 * Expects a workflow mode value and returns a one-letter uppercase badge.
 */
export function workflowModeBadge(mode: unknown): string {
  return normalizeWorkflowMode(mode).slice(0, 1).toUpperCase();
}
