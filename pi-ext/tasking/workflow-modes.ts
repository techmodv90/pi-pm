export type WorkflowMode = "quick" | "standard" | "designed" | "full";
export type DesignStatus = "" | "pending" | "approved" | "rejected";
export type OwnerStatus = "" | "pending" | "accepted" | "rejected";

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
