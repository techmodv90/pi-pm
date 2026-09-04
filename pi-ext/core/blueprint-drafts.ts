import { randomUUID } from "node:crypto";
import { accessSync, chmodSync, constants as fsConstants, mkdirSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { deleteRuntimeDraft, loadLatestRuntimeDraft, loadRuntimeDraft, saveRuntimeDraft } from "./runtime-drafts.ts";

export interface BlueprintDraft {
  workItemId: string;
  draftId: string;
  content: string;
  reviewed: boolean;
  checkpoint?: unknown;
  updatedAt: string;
}

interface BlueprintDraftState {
  content: string;
  reviewed: boolean;
  checkpoint?: unknown;
}

export function saveBlueprintDraft(root: string, workItemId: string, content: string, checkpoint?: unknown): BlueprintDraft {
  const draft = saveRuntimeDraft<BlueprintDraftState>(root, "blueprint", workItemId, {
    content,
    reviewed: checkpoint !== undefined,
    ...(checkpoint === undefined ? {} : { checkpoint }),
  });
  return { workItemId, draftId: draft.draftId, ...draft.state, updatedAt: draft.updatedAt };
}

export function loadBlueprintDraft(root: string, workItemId: string, draftId: string): BlueprintDraft {
  const draft = loadRuntimeDraft<BlueprintDraftState>(root, "blueprint", workItemId, draftId);
  return { workItemId, draftId: draft.draftId, ...draft.state, updatedAt: draft.updatedAt };
}

export function loadLatestBlueprintDraft(root: string, workItemId: string): BlueprintDraft {
  const draft = loadLatestRuntimeDraft<BlueprintDraftState>(root, "blueprint", workItemId);
  return { workItemId, draftId: draft.draftId, ...draft.state, updatedAt: draft.updatedAt };
}

export function deleteBlueprintDraft(root: string, workItemId: string): void {
  deleteRuntimeDraft(root, "blueprint", workItemId);
}

// Plan projection constraint (OB-F3-1): the reviewed Blueprint render is
// persisted at one stable per-Work-Item path under .pi/artifacts/plans before
// owner review, and every rejection-loop revision rewrites the same path so
// prior content survives for diff history. Unsafe Work Item IDs are rejected
// before path construction.
export function blueprintPlanPath(root: string, workItemId: string): string {
  if (!/^wi-[a-z0-9]+$/.test(workItemId)) throw new Error("Invalid Work Item ID for plan projection");
  return join(root, ".pi", "artifacts", "plans", `${workItemId}.md`);
}

export function writeBlueprintPlan(root: string, workItemId: string, markdown: string): string {
  const path = blueprintPlanPath(root, workItemId);
  const temporary = `${path}.${process.pid}-${randomUUID()}.tmp`;
  mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
  writeFileSync(temporary, markdown, { encoding: "utf8", mode: 0o600 });
  chmodSync(temporary, 0o600);
  // Terminal commit: rename is the last fallible operation, so every
  // preparation failure above leaves the previous plan bytes intact.
  renameSync(temporary, path);
  return path;
}

// Plan-review runtime constraint (OB-F3-4): @plannotator/pi-extension v0.27.9
// is the primary review runtime, reached through the asynchronous
// plannotator:request plan-review event (plannotator_submit_plan itself rejects
// calls outside Plannotator planning mode); the standalone plannotator CLI is
// only an informational alternative, and its absence is a guarded no-op — never
// an annotation success. Zero owner annotations approve the plan with no
// disposition ceremony; entered annotations become the pending state the hard
// gate enforces (the plan stays unapproved until the review loop resolves
// them).
export const PLANNOTATOR_REQUEST_CHANNEL = "plannotator:request";
export const PLAN_REVIEW_DRAFT_STAGE = "plan-review";
export const PLAN_REVIEW_REQUEST_TIMEOUT_MS = 5_000;

// Structural EventBus surface (matches the Pi extension runtime EventBus), so
// tests can inject a fake bus without importing the host runtime.
export interface PlannotatorEvents {
  emit(channel: string, data: unknown): unknown;
}

// Response shapes of the Plannotator extension's plannotator:request handler.
type PlannotatorReply<T> = { status: "handled"; result: T } | { status: "unavailable" | "error"; error?: string };
type PlannotatorPlanReviewStart = { status: "pending"; reviewId: string };
type PlannotatorReviewStatus = { status: "pending" | "completed" | "missing"; approved?: boolean; feedback?: string };

export type PlanReviewStatus = "pending" | "unavailable" | "approved" | "rejected";

export interface PlanReviewState {
  workItemId: string;
  planPath: string;
  status: PlanReviewStatus;
  reviewId?: string;
  feedback?: string;
  error?: string;
  updatedAt: string;
}

function planReviewTimeoutMs(): number {
  const override = Number(process.env.PI_PLAN_REVIEW_TIMEOUT_MS);
  return Number.isFinite(override) && override > 0 ? override : PLAN_REVIEW_REQUEST_TIMEOUT_MS;
}

// Emit a plannotator:request and await the extension's respond callback; a
// silent channel (extension not loaded) resolves as a timeout instead of
// hanging the tool call, so every outcome is guarded.
function plannotatorRequest<T>(events: PlannotatorEvents, action: "plan-review" | "review-status", payload: unknown, timeoutMs: number): Promise<PlannotatorReply<T> | { status: "timeout" }> {
  return new Promise((resolve) => {
    let timer: NodeJS.Timeout;
    let settled = false;
    const finish = (response: PlannotatorReply<T> | { status: "timeout" }) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve(response);
    };
    timer = setTimeout(() => finish({ status: "timeout" }), timeoutMs);
    try {
      events.emit(PLANNOTATOR_REQUEST_CHANNEL, { requestId: randomUUID(), action, payload, respond: finish });
    } catch (error) {
      finish({ status: "unavailable", error: error instanceof Error ? error.message : String(error) });
    }
  });
}

function persistPlanReviewState(root: string, state: Omit<PlanReviewState, "updatedAt">): PlanReviewState {
  const draft = saveRuntimeDraft<Omit<PlanReviewState, "updatedAt">>(root, PLAN_REVIEW_DRAFT_STAGE, state.workItemId, state);
  return { ...draft.state, updatedAt: draft.updatedAt };
}

export function loadPlanReviewState(root: string, workItemId: string): PlanReviewState | undefined {
  try { return loadLatestRuntimeDraft<PlanReviewState>(root, PLAN_REVIEW_DRAFT_STAGE, workItemId).state; }
  catch { return undefined; }
}

export function deletePlanReviewState(root: string, workItemId: string): void {
  deleteRuntimeDraft(root, PLAN_REVIEW_DRAFT_STAGE, workItemId);
}

// Request a Plannotator plan review for the persisted plan render. An unsafe
// Work Item ID throws before any request is emitted; every extension outcome
// (handled, unavailable, error, timeout) resolves without throwing, and an
// unavailable runtime is recorded as a guarded fallback — never as annotations.
export async function requestPlanReview(events: PlannotatorEvents, root: string, workItemId: string, markdown: string, options: { timeoutMs?: number } = {}): Promise<PlanReviewState> {
  const planPath = blueprintPlanPath(root, workItemId);
  const reply = await plannotatorRequest<PlannotatorPlanReviewStart>(events, "plan-review", { planFilePath: planPath, planContent: markdown, origin: "pi-task-system" }, options.timeoutMs ?? planReviewTimeoutMs());
  const base = { workItemId, planPath };
  if (reply.status === "handled" && typeof reply.result?.reviewId === "string" && reply.result.reviewId.trim()) {
    return persistPlanReviewState(root, { ...base, status: "pending", reviewId: reply.result.reviewId });
  }
  const error = reply.status === "handled"
    ? "Plannotator plan-review response carried no review identity"
    : reply.status === "timeout"
      ? "Plannotator extension did not respond"
      : reply.error || "Plannotator extension is unavailable";
  return persistPlanReviewState(root, { ...base, status: "unavailable", error });
}

// Recover the review outcome for a pending review through the extension's
// review-status request: completed results flip the state to approved
// (approve, no feedback) or rejected (rejected or any entered annotation —
// annotations become the pending state the hard gate enforces). Guarded: a
// silent channel, a still-pending review, or a missing stored result leaves
// the persisted state unchanged.
export async function recoverPlanReviewResult(events: PlannotatorEvents, root: string, workItemId: string, options: { timeoutMs?: number } = {}): Promise<PlanReviewState | undefined> {
  const state = loadPlanReviewState(root, workItemId);
  if (!state || state.status !== "pending" || !state.reviewId) return state;
  const reply = await plannotatorRequest<PlannotatorReviewStatus>(events, "review-status", { reviewId: state.reviewId }, options.timeoutMs ?? planReviewTimeoutMs());
  if (reply.status !== "handled" || !reply.result || reply.result.status !== "completed") return state;
  const feedback = typeof reply.result.feedback === "string" && reply.result.feedback.trim() ? reply.result.feedback : undefined;
  if (reply.result.approved === true && !feedback) {
    return persistPlanReviewState(root, { ...state, status: "approved" });
  }
  return persistPlanReviewState(root, { ...state, status: "rejected", feedback: feedback || "Plan review was rejected without recorded feedback" });
}

// Hard gate for approve_blueprint_draft: a pending review or a rejected review
// with pending annotations blocks approval; an approved review, a guarded
// unavailable runtime, or a never-requested review passes with zero
// dispositions recorded.
export function planApprovalGate(state: PlanReviewState | undefined): { ok: true; dispositions: 0 } | { ok: false; reason: string } {
  if (!state || state.status === "unavailable") return { ok: true, dispositions: 0 };
  if (state.status === "pending") {
    return { ok: false, reason: `Plan review ${state.reviewId} is still pending; the owner must complete the Plannotator review before approval` };
  }
  if (state.status === "rejected") {
    return { ok: false, reason: `Plan review was rejected with pending annotations: ${state.feedback || "no feedback recorded"}; revise the Blueprint and rerun review_blueprint_checkpoint` };
  }
  return { ok: true, dispositions: 0 };
}

// Guarded standalone-CLI discovery, informational only: a missing binary is a
// plain false, never an error or a fabricated annotation outcome.
export function planReviewCliAvailable(): boolean {
  for (const dir of (process.env.PATH || "").split(":")) {
    if (!dir) continue;
    try { accessSync(join(dir, "plannotator"), fsConstants.X_OK); return true; } catch { /* not in this directory */ }
  }
  return false;
}

// ── Annotation dispositions (OB-F3-2 / OB-F3-3) ─────────────────────────

export type AnnotationResolution = "addressed" | "waived";

export interface BlueprintAnnotationDisposition {
  annotation: string;
  resolution: AnnotationResolution;
  evidence: string;
}

const BLUEPRINT_DISPOSITIONS_DRAFT_STAGE = "blueprint-dispositions";

// Disposition evidence surface constraint: only terminal resolutions
// {annotation, resolution: addressed|waived, evidence} are accepted, with
// nonempty annotation and evidence, deduplicated by annotation identity, so
// incomplete evidence can never reach canonical save.
export function validateBlueprintDispositions(input: unknown): BlueprintAnnotationDisposition[] {
  if (!Array.isArray(input) || input.length === 0) throw new Error("Blueprint annotation dispositions must be a nonempty array of {annotation, resolution, evidence}");
  const seen = new Set<string>();
  return input.map((entry) => {
    const disposition = entry as Partial<BlueprintAnnotationDisposition> | null;
    const annotation = typeof disposition?.annotation === "string" ? disposition.annotation.trim() : "";
    const evidence = typeof disposition?.evidence === "string" ? disposition.evidence.trim() : "";
    if (!annotation || !evidence || (disposition?.resolution !== "addressed" && disposition?.resolution !== "waived")) {
      throw new Error("Each annotation disposition requires {annotation, resolution: addressed|waived, evidence}");
    }
    if (seen.has(annotation)) throw new Error(`Duplicate disposition for annotation: ${annotation}`);
    seen.add(annotation);
    return { annotation, resolution: disposition.resolution, evidence };
  });
}

// Disposition store: the persisted review record the approval gate recomputes
// from on every attempt. Merging is idempotent by annotation identity (the
// newest terminal resolution replaces an older one); the durable immutable
// evidence copy is written to the approval checkpoint by the Go CLI.
export function saveBlueprintDispositions(root: string, workItemId: string, dispositions: BlueprintAnnotationDisposition[]): BlueprintAnnotationDisposition[] {
  const merged = new Map<string, BlueprintAnnotationDisposition>();
  for (const entry of loadBlueprintDispositions(root, workItemId)) merged.set(entry.annotation, entry);
  for (const entry of validateBlueprintDispositions(dispositions)) merged.set(entry.annotation, entry);
  const state = [...merged.values()];
  saveRuntimeDraft(root, BLUEPRINT_DISPOSITIONS_DRAFT_STAGE, workItemId, state);
  return state;
}

export function loadBlueprintDispositions(root: string, workItemId: string): BlueprintAnnotationDisposition[] {
  try {
    const draft = loadLatestRuntimeDraft<BlueprintAnnotationDisposition[]>(root, BLUEPRINT_DISPOSITIONS_DRAFT_STAGE, workItemId);
    // Persisted-state integrity (REQ-8): only terminal dispositions with
    // nonempty evidence may leave the store. A malformed persisted entry is
    // dropped here so it can never be treated as resolved annotation
    // evidence; the approval gate then fails closed on the unresolved
    // annotation instead of trusting corrupted state.
    return validateBlueprintDispositions(draft.state);
  } catch {
    return [];
  }
}

export function deleteBlueprintDispositions(root: string, workItemId: string): void {
  deleteRuntimeDraft(root, BLUEPRINT_DISPOSITIONS_DRAFT_STAGE, workItemId);
}

// Annotation identity constraint: annotations are derived from the persisted
// review feedback, never from approval-call memory — each nonempty feedback
// line is one annotation identity; unsplit feedback is one annotation.
export function planReviewAnnotations(state: PlanReviewState | undefined): string[] {
  if (!state || state.status !== "rejected" || !state.feedback?.trim()) return [];
  const lines = state.feedback.split("\n").map((line) => line.trim()).filter(Boolean);
  return lines.length > 0 ? lines : [state.feedback.trim()];
}

// Persisted-state hard gate (OB-F3-2): recomputed on every approval attempt
// from the persisted review annotations plus the persisted disposition store —
// never from memory or actor_role. Every annotation must carry a terminal
// disposition before canonical save; the resolved set passed to the Go evidence
// write is the intersection with the current annotations so stale entries from
// a superseded review are never recorded as evidence.
export function annotationDispositionGate(annotations: string[], dispositions: BlueprintAnnotationDisposition[]): { ok: true; resolved: BlueprintAnnotationDisposition[] } | { ok: false; reason: string } {
  // Fail-closed revalidation (REQ-8): every loaded disposition is revalidated
  // before gate evaluation, so a malformed or evidence-free entry — including
  // one passed straight from in-memory callers — fails the gate instead of
  // satisfying an annotation. An empty store is legitimate (zero-disposition
  // approval) and skips validation.
  if (dispositions.length > 0) {
    try { validateBlueprintDispositions(dispositions); } catch (error) {
      return { ok: false, reason: error instanceof Error ? error.message : String(error) };
    }
  }
  const resolvedByAnnotation = new Map(dispositions.map((entry) => [entry.annotation, entry]));
  const pending = annotations.filter((annotation) => !resolvedByAnnotation.has(annotation));
  if (pending.length > 0) {
    return { ok: false, reason: `${pending.length} unresolved annotation${pending.length === 1 ? "" : "s"} pending disposition: ${pending.join("; ")}; record an addressed or waived disposition with evidence for each annotation` };
  }
  return { ok: true, resolved: dispositions.filter((entry) => annotations.includes(entry.annotation)) };
}

// Temp plan cleanup constraint (REQ-F3-3): the review markdown is temporary
// and is deleted after successful approval; the durable disposition evidence
// lives on the approval checkpoint, so cleanup never loses the review record.
export function deleteBlueprintPlan(root: string, workItemId: string): void {
  rmSync(blueprintPlanPath(root, workItemId), { force: true });
}
