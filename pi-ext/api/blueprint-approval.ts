import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { execPic } from "../core/cli-helpers.ts";
import {
  annotationDispositionGate,
  deleteBlueprintDraft,
  deleteBlueprintDispositions,
  deleteBlueprintPlan,
  deletePlanReviewState,
  loadBlueprintDispositions,
  loadBlueprintDraft,
  planApprovalGate,
  planReviewAnnotations,
  planReviewCliAvailable,
  recoverPlanReviewResult,
  requestPlanReview,
  saveBlueprintDispositions,
  saveBlueprintDraft,
  validateBlueprintDispositions,
  writeBlueprintPlan,
} from "../core/blueprint-drafts.ts";
import { planAdrFiles, writeAdrFiles, type AdrCandidate } from "../core/blueprint-adr.ts";
import { parseBlueprintReportJson, renderBlueprintReportMarkdown } from "../reporting/blueprint-report.ts";
import type { TaskManagerParams } from "./task-manager-schema.ts";

export interface ToolOutcome {
  content: { type: "text"; text: string }[];
  details: unknown;
  isError?: boolean;
}

function errorOutcome(message: string, details: unknown = {}): ToolOutcome {
  return { content: [{ type: "text", text: `Error: ${message}` }], details, isError: true };
}

export async function reviewBlueprintCheckpoint(pi: ExtensionAPI, ctx: { cwd: string }, params: TaskManagerParams): Promise<ToolOutcome> {
  if (!params.id || !params.artifact_id || !params.content || params.actor_role !== "contractor") return errorOutcome("id, artifact_id (the draft ID), content, and actor_role=contractor are required");
  const draft = loadBlueprintDraft(ctx.cwd, params.id, params.artifact_id);
  let checkpoint: Record<string, unknown>;
  try { checkpoint = JSON.parse(params.content) as Record<string, unknown>; }
  catch { return errorOutcome("content must be a JSON object with the five checkpoint booleans: architecture, design, requirements, task_decomposition (verification_seams for decomposition policy v2 drafts), nothing_missing"); }
  // The fifth check is policy-dependent: decomposition policy v2 drafts
  // are reviewed for verification seams, v1 drafts for task decomposition.
  const policyVersion = (JSON.parse(draft.content) as { decomposition_policy_version?: number }).decomposition_policy_version ?? 1;
  const checks = policyVersion === 2
    ? ["architecture", "design", "requirements", "verification_seams", "nothing_missing"]
    : ["architecture", "design", "requirements", "task_decomposition", "nothing_missing"];
  if (!checks.every((key) => checkpoint[key] === true)) return { content: [{ type: "text", text: `Error: all five Blueprint checks must pass; set each to true: ${checks.join(", ")}` }], details: {}, isError: true };
  // OB-F3-1 stable review projection: persist the reviewed render at
  // the stable plan path before owner review. Rendering failures
  // surface as the checkpoint error and a failed persistence write
  // never removes the prior plan file (atomic rename inside
  // writeBlueprintPlan).
  let planMarkdown: string;
  try { planMarkdown = renderBlueprintReportMarkdown(parseBlueprintReportJson(draft.content)); }
  catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return errorOutcome(message);
  }
  try { writeBlueprintPlan(ctx.cwd, params.id, planMarkdown); }
  catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return { content: [{ type: "text", text: `Error: ${message}` }], details: { error: message }, isError: true };
  }
  const reviewed = saveBlueprintDraft(ctx.cwd, params.id, draft.content, checkpoint);
  const blueprintPresentation = planMarkdown.replaceAll("- [ ]", "- [x]");
  // OB-F3-4 review entry: hand the reviewed render to the Plannotator
  // Pi extension through the asynchronous plannotator:request
  // plan-review event. An unavailable extension is a guarded fallback
  // that never fabricates an annotation outcome; zero annotations
  // approve with no dispositions recorded, and the standalone CLI is
  // informational only.
  const planReview = await requestPlanReview(pi.events, ctx.cwd, params.id, planMarkdown);
  const reviewNote = planReview.status === "pending"
    ? `Plan review requested through the Plannotator Pi extension (review ${planReview.reviewId}); annotations are optional — zero annotations approve with no dispositions recorded.`
    : `Plan review entry unavailable (${planReview.error || "extension did not respond"}); proceeding without annotations. The standalone plannotator CLI is not required (CLI on PATH: ${planReviewCliAvailable() ? "yes" : "no"}).`;
  return { content: [{ type: "text", text: `${blueprintPresentation}\n\nContractor checkpoint passed. Draft ${reviewed.draftId} is ready for owner approval.\nPlan review: ${planReview.planPath}\n${reviewNote}` }], details: { draft_id: reviewed.draftId, reviewed: true, plan_path: planReview.planPath, plan_review: planReview } };
}

export async function approveBlueprintDraft(pi: ExtensionAPI, ctx: { cwd: string }, params: TaskManagerParams): Promise<ToolOutcome> {
  if (!params.id || !params.artifact_id || params.actor_role !== "owner") return errorOutcome("id, draft_id, and actor_role=owner are required");
  const draft = loadBlueprintDraft(ctx.cwd, params.id, params.artifact_id);
  if (!draft.reviewed) return errorOutcome("Contractor review is required before owner approval");
  // OB-F3-4 hard gate: recover the persisted Plannotator review
  // result before approval. A pending review or a rejected review
  // with entered annotations blocks approval until the revision loop
  // resolves it; an approved review, a guarded unavailable runtime,
  // or a never-requested review passes with zero dispositions.
  const planReview = await recoverPlanReviewResult(pi.events, ctx.cwd, params.id);
  const gate = planApprovalGate(planReview);
  // OB-F3-2/OB-F3-3 persisted-state hard gate: a still-pending review
  // blocks approval outright (no annotations exist to resolve); a
  // rejected review falls through to the disposition gate, where the
  // annotations derived from the persisted feedback are recorded into
  // the persisted disposition store and the unresolved count is
  // recomputed from persistence before any canonical save. A pending
  // annotation returns a gate error with no save and no cleanup, so
  // the draft, plan file, and recorded dispositions all stay
  // recoverable for the next attempt.
  const annotations = planReviewAnnotations(planReview);
  let dispositions = loadBlueprintDispositions(ctx.cwd, params.id);
  if (!gate.ok && annotations.length === 0) return { content: [{ type: "text", text: `Error: ${gate.reason}` }], details: { plan_review: planReview }, isError: true };
  if (params.dispositions !== undefined) {
    try {
      const validated = validateBlueprintDispositions(params.dispositions);
      for (const entry of validated) {
        if (!annotations.includes(entry.annotation)) {
          throw new Error(`Disposition annotation "${entry.annotation}" does not match a recorded plan-review annotation`);
        }
      }
      dispositions = saveBlueprintDispositions(ctx.cwd, params.id, validated);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return { content: [{ type: "text", text: `Error: ${message}` }], details: { annotations, dispositions }, isError: true };
    }
  }
  const dispositionGate = annotationDispositionGate(annotations, dispositions);
  if (!dispositionGate.ok) return { content: [{ type: "text", text: `Error: ${dispositionGate.reason}` }], details: { annotations, dispositions }, isError: true };
  // OB-F2-3: full adr_candidates validation (shape, string fields,
  // safe slugs) and target-conflict preflight run BEFORE the Go
  // artifact-save/approve, so malformed or conflicting input can
  // never leave the canonical Blueprint approved while the tool
  // returns an error. writeAdrFiles runs only after both Go
  // operations succeed.
  let adrPayload: { adr_candidates?: unknown };
  try { adrPayload = JSON.parse(draft.content); }
  catch {
    return errorOutcome("Blueprint draft content must be valid JSON");
  }
  const parsedAdrCandidates = adrPayload.adr_candidates;
  if (parsedAdrCandidates !== undefined && !Array.isArray(parsedAdrCandidates)) return errorOutcome("adr_candidates must be an array of {context, choice, reason} objects");
  const adrCandidates = (parsedAdrCandidates ?? []) as AdrCandidate[];
  try { planAdrFiles(ctx.cwd, adrCandidates); }
  catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return errorOutcome(message);
  }
  const saved = execPic(["work-item", "artifact-save", params.id, "blueprint", draft.content], ctx.cwd);
  if (saved.error) return { content: [{ type: "text", text: `Error: ${saved.error}` }], details: saved, isError: true };
  // Durable evidence ordering (OB-F3-3): the Go approval commits the
  // terminal dispositions onto the Blueprint checkpoint in the same
  // transaction, before any temporary file is deleted; a failed save
  // or approval leaves zero evidence and preserves temporary files.
  const approveArgs = ["work-item", "artifact-approve", params.id, "blueprint", saved.id, "approved"];
  if (dispositionGate.resolved.length > 0) approveArgs.push("--dispositions-json", JSON.stringify(dispositionGate.resolved));
  const approved = execPic(approveArgs, ctx.cwd);
  if (approved.error) return { content: [{ type: "text", text: `Error: ${approved.error}` }], details: { saved, approved }, isError: true };
  let adrFiles: string[] = [];
  // Validation already passed before the Go calls; this write only
  // fails on a mid-approval filesystem change, which must surface as
  // a concrete approval-side error, not a thrown exception.
  if (adrCandidates.length > 0) {
    try { adrFiles = writeAdrFiles(ctx.cwd, adrCandidates); }
    catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return { content: [{ type: "text", text: `Error: ${message}` }], details: { saved, approved }, isError: true };
    }
  }
  deleteBlueprintDraft(ctx.cwd, params.id);
  deletePlanReviewState(ctx.cwd, params.id);
  deleteBlueprintDispositions(ctx.cwd, params.id);
  deleteBlueprintPlan(ctx.cwd, params.id);
  return { content: [{ type: "text", text: JSON.stringify({ saved, approved, adr_files: adrFiles }, null, 2) }], details: { saved, approved, adr_files: adrFiles } };
}
