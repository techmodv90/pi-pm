import { maxWorkflowMode, type WorkflowMode } from "../tasking/workflow-modes.ts";

export type PhaseExecutionPolicy = "strict_sequential" | "partially_parallel" | "parallel_allowed" | "deferred_optional";
export type PhaseRecommendation = "single_task" | "phased_milestone";

export interface PhaseAssessmentSignal {
  label: string;
  pattern: RegExp;
  weight: number;
  workflowFloor: WorkflowMode;
}

export interface PhaseAssessmentResult {
  recommendation: PhaseRecommendation;
  score: number;
  riskLevel: "low" | "medium" | "high";
  workflowModeFloor: WorkflowMode;
  reasons: string[];
  suggestedQuestion: string;
}

const PHASE_SIGNALS: PhaseAssessmentSignal[] = [
  { label: "Database schema or migration change", pattern: /\b(database|schema|migration|migrate|prisma|sql|table|column|backfill)\b/i, weight: 2, workflowFloor: "designed" },
  { label: "Public API or contract change", pattern: /\b(api|endpoint|route|graphql|rest|contract|webhook|sdk)\b/i, weight: 2, workflowFloor: "designed" },
  { label: "Security, authorization, or permission impact", pattern: /\b(auth|authorization|permission|security|role|rbac|token|secret)\b/i, weight: 2, workflowFloor: "designed" },
  { label: "External integration or payment dependency", pattern: /\b(integration|stripe|payment|third[- ]party|provider|external|oauth)\b/i, weight: 2, workflowFloor: "designed" },
  { label: "Background processing or async workflow", pattern: /\b(queue|worker|job|cron|async|event|message|notification)\b/i, weight: 1, workflowFloor: "designed" },
  { label: "Production rollout or operational readiness concern", pattern: /\b(rollout|monitoring|metrics|logging|observability|rollback|deploy|production|backup)\b/i, weight: 1, workflowFloor: "designed" },
  { label: "Large or multi-session feature", pattern: /\b(large|complex|multi[- ]session|milestone|phase|subsystem|end-to-end|e2e|rewrite|redesign)\b/i, weight: 2, workflowFloor: "full" },
  { label: "Multiple modules or layered backend work", pattern: /\b(service|repository|controller|handler|module|model|domain|admin|reporting)\b/i, weight: 1, workflowFloor: "standard" },
];

/**
 * Convert a phase assessment score to an owner-visible risk label.
 * Expects a numeric score and returns low, medium, or high for RRI output.
 */
function phaseRiskLevel(score: number): PhaseAssessmentResult["riskLevel"] {
  if (score >= 6) return "high";
  if (score >= 3) return "medium";
  return "low";
}

/**
 * Assess whether a task should become a phased milestone.
 * Expects a task title and optional description and returns deterministic phase
 * signals that RRI/design prompts can combine with human answers.
 */
export function assessPhaseNeed(title: string, description = ""): PhaseAssessmentResult {
  const text = `${title}\n${description}`;
  let score = 0;
  let workflowModeFloor: WorkflowMode = "quick";
  const reasons: string[] = [];

  for (const signal of PHASE_SIGNALS) {
    if (!signal.pattern.test(text)) continue;
    score += signal.weight;
    workflowModeFloor = maxWorkflowMode(workflowModeFloor, signal.workflowFloor);
    reasons.push(signal.label);
  }

  const recommendation: PhaseRecommendation = score >= 3 ? "phased_milestone" : "single_task";
  return {
    recommendation,
    score,
    riskLevel: phaseRiskLevel(score),
    workflowModeFloor,
    reasons,
    suggestedQuestion: "Should this feature be delivered as one task or split into phased milestones with explicit ordering/dependencies?",
  };
}

/**
 * Format phase assessment as markdown for RRI prompts.
 * Expects an assessment result and returns owner-facing guidance with the phase
 * decision question and signals that triggered the recommendation.
 */
export function buildPhaseAssessmentMarkdown(assessment: PhaseAssessmentResult): string {
  const lines: string[] = [];
  lines.push("## Phase Assessment");
  lines.push(`Recommendation: ${assessment.recommendation === "phased_milestone" ? "phased milestone" : "single task"}`);
  lines.push(`Score: ${assessment.score}`);
  lines.push(`Risk Level: ${assessment.riskLevel}`);
  lines.push(`Workflow Floor: ${assessment.workflowModeFloor}`);
  lines.push("Reasons:");
  if (assessment.reasons.length === 0) {
    lines.push("- No strong phase triggers detected from the current title/description.");
  } else {
    for (const reason of assessment.reasons) lines.push(`- ${reason}`);
  }
  lines.push("Decision question:");
  lines.push(`- ${assessment.suggestedQuestion}`);
  return lines.join("\n") + "\n";
}

/**
 * Extract a markdown section by heading name for phase-plan parsing.
 * Expects markdown text and a level-two heading name and returns that section
 * body until the next level-two heading, or null when absent.
 */
function extractPhaseMarkdownSection(description: string, sectionName: string): string | null {
  const escapedSection = sectionName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = description.match(new RegExp(`## ${escapedSection}\\n([\\s\\S]*?)(?=\\n## |$)`));
  return match ? match[1].trim() : null;
}

/**
 * Extract the Phase Plan section from a structured task description.
 * Expects markdown task description text and returns the phase plan body when
 * present, otherwise null for non-phased tasks.
 */
export function extractPhasePlan(description = ""): string | null {
  return extractPhaseMarkdownSection(description, "Phase Plan");
}

/**
 * Return true when a task description includes a non-empty Phase Plan section.
 * Expects markdown task description text and returns whether phase metadata is
 * available for work/design prompts.
 */
export function hasPhasePlan(description = ""): boolean {
  return !!extractPhasePlan(description);
}

/**
 * Build the phase-plan template required during RRI/design.
 * Expects no input and returns a concise markdown schema for phase boundaries,
 * ordering policy, dependencies, and deferrable phases.
 */
export function buildTaskPlanTemplate(): string {
  return `## Task Plan

The approved Blueprint must contain exactly one fenced \`task-plan-json\` block. Use schema version 3 for new implementation plans. Every node must declare \`skillFamilies\` chosen from the available skill family catalog (the \`<skill_family_catalog>\` section of the dispatch handoff when planning through the pipeline; \`pic\`'s packaged catalog otherwise), using [] when no family applies — the catalog id list is authoritative, and unknown ids are rejected. Assign each node only the requirement keys it implements. Contract changes must use stable contract keys and approved structured replace, withdraw, or defer operations before materialization; prose precedence and free-form deviations do not resolve conflicts. Populate every field with approved Task-scoped content; use \`Not applicable: <specific reason>\` only when a section genuinely does not apply.

\`\`\`task-plan-json
{
  "version": 3,
  "execution_policy": "strict_sequential",
  "nodes": [
    {
      "key": "T01",
      "name": "<one observable outcome>",
      "goal": "<single independently reviewable outcome>",
      "requirement_keys": ["REQ-001"],
      "depends_on": [],
      "priority": "P1",
      "module": "<bounded module>",
      "skillFamilies": [],
      "estimated_effort_minutes": 60,
      "files": ["<repo-relative path>"],
      "patterns": [{"file": "<path>", "symbol": "<symbol>", "reason": "<verified reason>"}],
      "business_rules": ["<detailed scoped rule>"],
      "validation_rules": [{"input": "<input>", "rule": "<rule>", "failure": "<required behavior>"}],
      "error_handling": [{"condition": "<failure>", "behavior": "<required behavior>", "recovery": "<recovery/propagation>"}],
      "state_transitions": ["<from -> to, or approved Not applicable reason>"],
      "contract_obligations": ["<scoped obligation with contract ID/section>"],
      "constraints": {
        "scope_roots": ["<bounded module directory>"],
        "protected_paths": [".git/**", ".pi/**", ".pi-subagents/**"],
        "generated_files": ["test-results/**", "playwright-report/**", "coverage/**"],
        "must_not_change": ["<boundary>"],
        "required_reuse": ["<existing helper/pattern>"],
        "compatibility": ["<constraint or approved Not applicable reason>"],
        "approved_deviation_ids": []
      },
      "verification": [{"command": "<exact focused command>", "required": true, "requires": [], "setup_commands": [], "expected_writes": ["test-results/**"]}]
    }
  ]
}
\`\`\`
`;
}

export function buildPhasePlanTemplate(): string {
  return [
    "## Phase Plan",
    "Recommendation: single task | phased milestone",
    "Decision: approved | rejected | deferred",
    "Execution Policy: strict_sequential | partially_parallel | parallel_allowed | deferred_optional",
    "Ordering Rationale: <why phases must run in order or can be parallel/out-of-order>",
    "Phases:",
    "- [Phase 1] <title>",
    "  Goal: <phase outcome>",
    "  Depends On: <none | Phase N | Phase N, Phase M>",
    "  Can Start Before Previous Phase Is Verified: yes | no",
    "  Deferrable: yes | no",
    "  Exit Criteria: <verification gate>",
  ].join("\n") + "\n";
}

/**
 * Build child Work Item instructions for phased milestones.
 */
export function buildPhaseTaskItemInstructions(): string {
  return [
    "When a phased milestone is approved:",
    "- Create child Work Items named `[Phase N] <feature> — <phase title>` when phases may be worked, verified, deferred, or resumed independently.",
    "- Persist every declared `Depends On` edge with task_manager `relate_work_items` using relation_type `blocks`; do not infer a universal previous-phase chain when the approved plan defines a DAG.",
    "- Prefer contract-first sibling lanes when valid: backend, UI-with-fixtures, and independent secondary features may run together after their shared contract; integration depends on every lane it consumes.",
    "- Each phase Work Item needs a clear outcome and verification evidence.",
    "- If phases are strict, later phase work must not start until prior phase verification passes or owner explicitly overrides it.",
  ].join("\n") + "\n";
}
