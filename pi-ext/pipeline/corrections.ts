import { createHash } from "node:crypto";
import type { PipelineRun, PipelineRunRecord } from "./pipeline-types.ts";

export function buildTargetedReReviewInstructions(): string {
  return [
    "\n## TARGETED RE-REVIEW",
    "This is a follow-up review after a review-fix round. Answer exactly these three targeted questions and fold your answers into the canonical review_report notes:",
    "1. RESOLVED - was each P0/P1 correction finding from the prior failed review resolved by this fix?",
    "2. NEW DEFECT - did the fix introduce a new defect within the blast radius of the changed files?",
    "3. PRIOR NOTES STANDING - do the prior P1 and P2 notes from the last review still stand unchanged, including any deferred P2 items?",
    "Return status=failed only when a P0/P1 finding remains unresolved or the fix introduced a defect in its blast radius.",
  ].join("\n") + "\n";
}

export function reviewCycleCount(runs: PipelineRunRecord[]): number {
  // Round-cap constraint: only COMPLETED review-fix worker runs advance the cap,
  // counted as DISTINCT cycle numbers. go-pic assigns review_fix_cycle as MAX+1
  // per claim, so a failed or transiently exhausted fix claim consumes a cycle
  // number without completing; max() would overcount completed rounds and trip
  // the cap before three fix rounds actually finished.
  return new Set(runs.filter((run) => run.stage === "worker" && run.status === "completed" && (run.review_fix_cycle || 0) >= 1)
    .map((run) => run.review_fix_cycle)).size;
}

export function buildReviewFixCapBlock(taskId: string, findings: string[]): string {
  const synthesis = synthesizeReviewFindings(findings);
  return [
    `review-fix round cap (${REVIEW_FIX_ROUND_LIMIT}) reached for ${taskId}: the unchanged active instruction pack cannot relaunch the fix worker.`,
    `Owner action required. Synthesized unresolved findings:`,
    renderSynthesizedFindings(synthesis),
    `Resolve the remaining P0/P1 findings in a fresh instruction pack, accept a deferral for the deferred items, or otherwise direct the next fix; the scheduler will not relaunch automatically.`,
  ].filter(Boolean).join("\n");

}
export const REVIEW_FIX_ROUND_LIMIT = 3;

// Finding synthesis constraint: a failed review's findings are classified into
// P0/P1/P2 classes plus an explicit defer list before reaching the fix worker.
// The fix worker receives only that synthesis, never the raw corrections dump.
// Classification follows the reviewer severity model (Critical/Important/Minor
// from task-reviewer.md): Critical -> P0, Important -> P1, Minor -> P2, and
// explicitly non-blocking items land in the defer list.
export interface SynthesizedFindings {
  p0: string[];
  p1: string[];
  p2: string[];
  deferred: string[];
}

export function synthesizeReviewFindings(findings: string[]): SynthesizedFindings {
  const buckets: SynthesizedFindings = { p0: [], p1: [], p2: [], deferred: [] };
  for (const raw of findings) {
    const finding = String(raw || "").trim();
    if (!finding) continue;
    const lower = finding.toLowerCase();
    if (/defer|non-blocking|hold\s+off|optional|timebox/i.test(lower)) buckets.deferred.push(finding);
    else if (/critical|must|secure|vulnerab|crash|data loss|corrupt|unsafe|deadlock|acceptance criterion|denial of service|failing (?:ci|test|build|verification)/i.test(lower)) buckets.p0.push(finding);
    else if (/minor|style|naming|refactor|suggest|cosmetic|nitpick|typo/i.test(lower)) buckets.p2.push(finding);
    else buckets.p1.push(finding);
  }
  return buckets;
}

export function renderSynthesizedFindings(synthesis: SynthesizedFindings, notes = ""): string {
  const sections: string[] = [];
  if (synthesis.p0.length) sections.push(`P0 (critical, must fix):\n${synthesis.p0.map((finding) => `- ${finding}`).join("\n")}`);
  if (synthesis.p1.length) sections.push(`P1 (important, fix now):\n${synthesis.p1.map((finding) => `- ${finding}`).join("\n")}`);
  if (synthesis.p2.length) sections.push(`P2 (minor, address if touching):\n${synthesis.p2.map((finding) => `- ${finding}`).join("\n")}`);
  if (synthesis.deferred.length) sections.push(`Deferred (non-blocking, explicitly set aside):\n${synthesis.deferred.map((finding) => `- ${finding}`).join("\n")}`);
  const body = sections.join("\n\n");
  return notes ? `${notes}\n\n${body}` : body;
}

export function buildWorkerCorrectionContext(data: any): string {
  if (!data?.current_review && (data.canonical || data?.work_item?.review_status !== "failed")) return "";
  // Synthesis gate constraint: the fix worker receives only classified findings.
  // A structured failed review contributes its <finding> entries (never the raw
  // notes dump); a legacy review_notes fallback is treated as one finding.
  const rawFindings = data.current_review
    ? (data.current_review.findings || []).filter((value: unknown) => value && String(value).trim())
    : [String(data.work_item.review_notes || "").trim()].filter(Boolean);
  if (!rawFindings.length) throw new Error("failed review is missing persisted correction findings");
  const synthesis = synthesizeReviewFindings(rawFindings);
  return `\n\n## REVIEW CORRECTIONS (synthesized findings only)\nThe rejected candidate is already applied to the assigned worktree. This is a review-fix run, not a verification-only run: make the required edits for every P0 and P1 finding below and return a non-empty patch whose SHA-256 differs from the rejected candidate. Do not report DONE or claim the fix is complete without changing the worktree. Git-derived changed files will be assessed by Reviewer.\nException: if EVERY P0 and P1 finding is demonstrably already satisfied by the current worktree or cannot be addressed by a code change, you may return DONE without edits, but your completion_report must then include a <no_change_justification> section addressing each P0 and P1 finding point by point with evidence. A missing or thin justification will be rejected; Reviewer independently re-judges the candidate either way.\n\n${renderSynthesizedFindings(synthesis)}\n`;
}

export function buildOwnerRejectionContext(data: any): string {
  const rejection = (data.owner_decisions || []).find((decision: any) => decision.decision === "rejected");
  if (!rejection) return "";
  return `\n\n## OWNER-REQUESTED CORRECTION\nThe owner rejected Completion Report ${rejection.completion_report_id}. Produce a fresh candidate that addresses this decision; do not reuse the rejected completion as authority.\n\n${rejection.notes || "Owner requested changes."}\n`;
}

export function buildAutofixContext(data: any): string {
  const verification = (data.verification_reports || []).find((report: any) => report.status === "failed" || report.status === "partial");
  if (!verification) throw new Error("autofix requires a failed or partial contractor verification report");
  const items = Array.isArray(verification.items) ? verification.items : [];
  return `\n\n## TARGETED AUTOFIX\nThis is not a fresh implementation or retry. Preserve the integrated work and close only the concrete verification gaps below under the unchanged active TIP. Do not weaken tests, verification commands, acceptance criteria, or scope. Return scope expansion instead of editing outside the TIP.\n\nVerification summary: ${verification.summary || "failed contractor verification"}\n${items.map((item: any) => `- ${item.requirement_id || "unlinked"}: ${item.status || "failed"} - ${item.evidence || item.notes || "no evidence supplied"}`).join("\n")}\n`;
}

export function assertReviewFixChangedPatch(run: PipelineRun, patch: Buffer, noChangeJustification?: string): void {
  if ((run.review_fix_cycle || 0) < 1 || !run.candidate_patch_hash) return;
  const patchHash = createHash("sha256").update(patch).digest("hex");
  if (patchHash !== run.candidate_patch_hash) return;
  // Justified no-op escape hatch: a worker that proves every P0/P1 finding is already
  // satisfied or not code-actionable may resubmit the unchanged candidate; the fresh
  // reviewer arbitrates on merit and the distinct-cycle round cap bounds abuse.
  if ((noChangeJustification || "").trim().length >= 40) return;
  throw new Error("review-fix produced the unchanged rejected candidate patch");
}

/**
 * Escalation resolution injection (GAP-138): resolutions recorded after every existing
 * pipeline run have not been seen by any worker yet; inject them into the next relaunch.
 */
export function buildEscalationResolutionContext(data: any, runs: PipelineRun[]): string {
  const escalations = Array.isArray(data?.escalations) ? data.escalations : [];
  const lastRunStart = runs.map((run) => String(run.created_at || "")).sort().at(-1) || "";
  const pending = escalations.filter((entry: any) => entry.status === "resolved" && String(entry.resolved_at || "") > lastRunStart);
  if (pending.length === 0) return "";
  const lines = pending.map((entry: any) => {
    let resolution: any = {};
    try { resolution = JSON.parse(entry.resolution_json || "{}"); } catch {}
    return `- Escalation ${entry.id} (${entry.level}, TIP ${entry.instruction_pack_id}): ${JSON.stringify(resolution)}`;
  });
  return `\n\n## ESCALATION RESOLUTIONS\nThe following escalations raised by a previous attempt on this TIP have been resolved. These decisions are authoritative contractor/owner answers: apply them exactly, do not re-litigate them, and do not re-escalate the same question without new evidence.\n\n${lines.join("\n")}\n`;
}
