import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { execPic, execPicText } from "../core/cli-helpers";
import { buildReviewInstructions, formatWorkItemChecklist } from "./work-item-prompts";
import { normalizeWorkflowMode, workflowModeBadge } from "./workflow-modes.ts";

// ── Model metadata helpers ─────────────────────────────────────

/**
 * Return a stable model identifier for reporting only.
 * Expects a pi model object and returns provider/id without changing models.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function modelKey(model: any): string | undefined {
  if (!model) return undefined;
  return `${model.provider || "unknown"}/${model.id}`;
}

/**
 * Extract a task id from a task work prompt.
 * Expects generated task handoff markdown and returns the first task id marker.
 */
export function extractTaskIdFromWorkPrompt(prompt: string): string | null {
  const idMatch = prompt.match(/\*\*Task ID:\*\*\s+(\S+)/);
  if (idMatch) return idMatch[1];
  return prompt.match(/^work on task\s+(\S+)/)?.[1] || null;
}

// ── Review context ─────────────────────────────────────────────

/**
 * Build a review request from persisted task context and repository diff.
 * Expects a task id and cwd; returns review markdown plus task/diff metadata.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function buildReviewContext(taskId: string, cwd: string): { text?: string; task?: any; gitDiff?: string; changedFiles?: string[]; error?: string } {
  const data = execPic(["show", taskId], cwd);
  if (!data.work_item) return { error: data.error || "Work Item not found" };

  const task = data.work_item;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const activePacks = (data.instruction_packs || []).filter((pack: any) => pack.status === "active");
  if (activePacks.length !== 1) return { error: "Review requires exactly one active Task Instruction Pack" };
  const pack = activePacks[0];
  const runs = execPic(["workflow", "pipeline-runs", taskId], cwd);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const activeReview = Array.isArray(runs) ? runs.find((run: any) => run.stage === "review" && ["claimed", "running"].includes(run.status)) : null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const candidate = Array.isArray(runs) ? runs.find((run: any) => run.id === activeReview?.candidate_run_id && ["worker", "autofix"].includes(run.stage)) : null;
  if (!activeReview || !candidate) return { error: "Review requires a bound Worker candidate pipeline run" };
  if (candidate.instruction_pack_id !== pack.id || Number(candidate.instruction_pack_version) !== Number(pack.version) || candidate.instruction_pack_hash !== pack.content_hash) {
    return { error: "Review requires Worker pipeline evidence bound to the active Task Instruction Pack ID, version, and hash" };
  }
  if (!candidate.artifact_saved_at || !candidate.integrated_patch_path || !candidate.integrated_patch_hash) return { error: "Review requires persisted candidate patch evidence" };
  let candidateReport = "";
  try { candidateReport = readFileSync(`${candidate.async_dir}/output-${candidate.child_index || 0}.log`, "utf8"); } catch { return { error: "Persisted candidate Worker output is unavailable" }; }
  const renderedPack = execPicText(["workflow", "instruction-pack-render", taskId], cwd);
  const items = data.items || [];
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const doneItems = items.filter((i: any) => i.done);
  const allDone = doneItems.length === items.length && items.length > 0;

  let changedFiles: string[] = [];
  let gitDiff = "";
  {
    let patch: Buffer;
    try { patch = readFileSync(candidate.integrated_patch_path); } catch { return { error: "Persisted candidate patch evidence is unavailable" }; }
    const actualHash = createHash("sha256").update(patch).digest("hex");
    if (actualHash !== candidate.integrated_patch_hash) return { error: "Candidate patch evidence hash mismatch" };
    if (activeReview.candidate_patch_hash !== actualHash) return { error: "Review claim candidate patch hash mismatch" };
    gitDiff = patch.toString("utf8");
    if (patch.length === 0) {
      changedFiles = [];
    } else try {
      changedFiles = execFileSync("git", ["apply", "--numstat", candidate.integrated_patch_path], { cwd, encoding: "utf8" })
        .trim().split("\n").filter(Boolean).map((line) => line.split("\t").at(-1) || "").filter(Boolean);
    } catch { return { error: "Candidate patch changed-file evidence is invalid" }; }
  }

  let text = `# Review Request: ${task.title}\n\n`;
  text += `**Task ID:** ${task.id}\n`;
  text += `**TIP:** ${pack.id} v${pack.version}\n`;
  text += `**TIP Hash:** ${pack.content_hash}\n`;
  text += `**Candidate Run:** ${candidate.id}\n`;
  text += `**Completed by:** ${candidate.agent_model || task.completed_by_model || "unknown"}\n`;
  text += `**Status:** ${task.status}\n\n`;
  text += `## Authoritative Task Instruction Pack\n\n${renderedPack}\n`;
  text += `\n## Bound Candidate Worker Report\n\n${candidateReport}\n\n`;
  if (task.notes) {
    text += `## Notes\n${task.notes}\n\n`;
  }
  text += `## Completed Items\n${formatWorkItemChecklist(doneItems, true)}`;
  if (!allDone && items.length > 0) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    text += `\n## Remaining Items\n${formatWorkItemChecklist(items.filter((i: any) => !i.done), false)}`;
  }
  text += `\n## Changed Files\n`;
  if (changedFiles.length > 0) {
    for (const file of changedFiles) text += `- ${file}\n`;
  } else {
    text += `No changed files detected (or not a git repository).\n`;
  }

  text += `\n## Candidate Task Diff\n`;
  text += gitDiff || "No changes detected.";
  text += `\n\n${buildReviewInstructions(task.id)}`;
  text += `\n## Output Requirement\nReturn exactly one canonical review-report block. The scheduler persists the verdict; do not mutate task state.\n`;

  return { text, task, gitDiff, changedFiles };
}

// ── Shared interfaces ──────────────────────────────────────────

export interface TaskItemInfo {
  id: string;
  content: string;
  done: boolean;
}

export interface TaskInfo {
  id: string;
  title: string;
  status: string;
  priority?: string;
  description?: string;
  notes?: string;
  completedByModel?: string;
  reviewStatus?: string;
  reviewNotes?: string;
  workflowMode?: string;
  workflowConfidence?: number;
  workflowReason?: string;
  designStatus?: string;
  ownerStatus?: string;
  epicTitle?: string;
  hasDesign?: boolean;
  items: TaskItemInfo[];
}

export interface TaskListEntry {
  id: string;
  title: string;
  status: string;
  priority?: string;
  epic?: string;
  type: "task";
  itemsDone?: number;
  itemsTotal?: number;
  workflowMode?: string;
  phaseParentTaskId?: string | null;
}

// ── Icons ──────────────────────────────────────────────────────

/**
 * Return the display icon for a task status.
 * Expects a task status string and returns a unicode status marker.
 */
export function statusIcon(status: string): string {
  switch (status) {
    case "done": return "✅";
    case "in_progress": return "🔵";
    case "cancelled": return "❌";
    default: return "⬜";
  }
}

/**
 * Return the workflow label shown in task lists.
 * Expects an optional workflow mode and returns a badge plus normalized label.
 */
export function workflowLabel(mode?: string): string {
  const normalizedMode = normalizeWorkflowMode(mode);
  return `[${workflowModeBadge(normalizedMode)}] ${normalizedMode}`;
}

/**
 * Return the display label for a task priority.
 * Expects a priority value and returns a color-coded text label.
 */
export function priorityLabel(p: string): string {
  switch (p) {
    case "high": return "🔴 High";
    case "medium": return "🟡 Medium";
    case "low": return "🟢 Low";
    default: return "";
  }
}
