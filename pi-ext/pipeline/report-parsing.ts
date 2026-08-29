import { XMLParser, XMLValidator } from "fast-xml-parser";
import type { PipelineRun } from "./pipeline-types.ts";

export function isMutationStage(stage: string): boolean { return stage === "worker" || stage === "autofix"; }

export function reviewRequiresOwner(runs: any[], candidate: any): boolean {
  const review = runs.find((run: any) => run.stage === "review" && run.status === "completed"
    && run.candidate_run_id === candidate?.id && run.candidate_patch_hash === candidate?.integrated_patch_hash);
  try {
    // Numeric-1 constraint: workflowReviewFixBlock historically persisted the
    // durable owner block as JSON number 1; accept both boolean and numeric
    // forms so legacy rows still stop the fix-worker relaunch loop.
    const flag = JSON.parse(review?.result_json || "{}").owner_approval_required;
    return flag === true || flag === 1 || flag === "true";
  } catch { return false; }
}

export function reviewStatusForCandidate(runs: any[], candidate: any): "passed" | "failed" | "" {
  if (!candidate?.id || !candidate.integrated_patch_hash) return "";
  for (const review of runs) {
    if (review.stage !== "review" || review.status !== "completed" || review.candidate_run_id !== candidate.id || review.candidate_patch_hash !== candidate.integrated_patch_hash) continue;
    try {
      const result = JSON.parse(review.result_json || "{}");
      if (result.review_status === "passed" || result.review_status === "failed") return result.review_status;
    } catch {}
  }
  return "";
}

export function currentFailedReview(runs: any[], activePack: any): any | undefined {
  const candidate = runs.find((run: any) => isMutationStage(run.stage) && run.status === "completed" && run.artifact_saved_at
    && !run.integrated_at && run.instruction_pack_id === activePack?.id && Number(run.instruction_pack_version) === Number(activePack?.version)
    && run.instruction_pack_hash === activePack?.content_hash);
  if (!candidate) return undefined;
  const review = runs.find((run: any) => run.stage === "review" && run.status === "completed" && run.candidate_run_id === candidate.id
    && run.candidate_patch_hash === candidate.integrated_patch_hash);
  if (!review) return undefined;
  try {
    const result = JSON.parse(review.result_json || "{}");
    return result.review_status === "failed" ? { ...result, candidate } : undefined;
  } catch { return undefined; }
}

export function latestVerificationAfter(data: any, completion: any): any | undefined {
  return (data.verification_reports || [])
    .filter((report: any) => report.completion_report_id === completion?.id)
    .sort((left: any, right: any) => Number(right.sequence || 0) - Number(left.sequence || 0)
      || String(right.created_at || "").localeCompare(String(left.created_at || "")))[0];
}

export function pipelineVerificationBlockReason(data: any): string | null {
  const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
  const completion = activePackDoneReports(data, activePack)[0];
  const verification = latestVerificationAfter(data, completion);
  if (verification?.status !== "blocked") return null;
  return `Work Item "${data.work_item?.title || "unknown"}" verification is blocked${verification.summary ? `: ${verification.summary}` : ". Resolve the verification prerequisite and run verification again."}`;
}

export function pipelineIntegrationBlockReason(data: any, runs: any[]): string | null {
  const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
  const doneReports = activePack ? activePackDoneReports(data, activePack) : [];
  if (!doneReports.length) return null;
  const latest = doneReports[0]!;
  const integrated = runs.some((run: any) =>
    run.id === latest.pipeline_run_id && isMutationStage(run.stage) && run.status === "completed" && run.integrated_at && run.integrated_patch_hash,
  );
  return integrated ? null : `Work Item "${data.work_item?.title || "unknown"}" is blocked because its DONE Completion Report lacks integrated worker patch evidence.`;
}

export function activePackDoneReports(data: any, activePack: any): any[] {
  return (data.completion_reports || []).filter((report: any) => report.status === "done"
    && (!activePack.id || report.instruction_pack_id === activePack.id)
    && (!activePack.version || report.instruction_pack_version === activePack.version)
    && (!activePack.content_hash || report.instruction_pack_hash === activePack.content_hash)
    && !(data.owner_decisions || []).some((decision: any) => (decision.decision === "rejected" && decision.completion_report_id === report.id)
      || (decision.decision_type === "request_changes" && ((decision.related_type === "completion_report" && decision.related_id === report.id)
        || (!decision.related_type && decision.created_at >= report.created_at)))));
}

export function parseTaskCompletionReport(output: string): { status: "done" | "partial" | "blocked" | "escalated"; markdown: string; blocker: string; escalation?: any; failure_metadata?: Record<string, string>; no_change_justification?: string } {
  const documents = [...output.matchAll(/<completion_report\b[\s\S]*?<\/completion_report>/g)].map((match) => match[0]);
  if (documents.length !== 1) throw new Error("worker output must contain one completion_report XML document");
  const document = documents[0]!.replace(/<([^<>\s]+)&gt;/g, "&lt;$1&gt;");
  if (!XMLValidator.validate(document)) throw new Error("worker output contains invalid XML");
  let parsed: any;
  try {
    parsed = new XMLParser({ ignoreAttributes: false }).parse(document)?.completion_report;
  } catch (error) {
    throw new Error(`worker output contains invalid XML: ${error instanceof Error ? error.message : String(error)}`);
  }
  const status = String(parsed?.["@_status"] || "").toLowerCase();
  if (!["done", "partial", "blocked", "escalated"].includes(status)) throw new Error("worker output has invalid status");
  const sections = Object.fromEntries(["files_changed", "test_results", "issues_discovered", "deviations", "suggestions"].map((section) => {
    const text = completionSectionText(parsed?.[section]);
    if (!text) throw new Error(`worker output missing ${section}`);
    return [section, text];
  }));
  let escalation: any;
  if (status === "escalated") {
    const raw = typeof parsed?.escalation === "string" ? parsed.escalation.trim() : "";
    if (!raw) throw new Error("escalated worker output requires one escalation section containing the structured report JSON");
    try {
      escalation = JSON.parse(raw);
    } catch (error) {
      throw new Error(`escalation section must be valid JSON: ${error instanceof Error ? error.message : String(error)}`);
    }
    if (!(["L2", "L3"].includes(escalation.level))) throw new Error("escalation report requires level L2 or L3");
    // Presence-only audit floor (GAP-138): makes the artifact-contradiction test auditable.
    if (!Array.isArray(escalation.checked_sources) || escalation.checked_sources.length === 0) throw new Error("escalation report requires a nonempty checked_sources list");
  }
  const blocker = [sections.issues_discovered, sections.deviations]
    .filter((value) => value && value.toLowerCase() !== "none")
    .join(" ");
  // Advisory worker-supplied triage hints (failure_metadata): malformed or hostile
  // values are dropped, never trusted for control flow — the owner still decides.
  let failureMetadata: Record<string, string> | undefined;
  const metaRaw = typeof parsed?.failure_metadata === "string" ? parsed.failure_metadata.trim() : "";
  if (metaRaw) {
    try {
      const candidate = JSON.parse(metaRaw);
      if (candidate && typeof candidate === "object" && !Array.isArray(candidate)) {
        failureMetadata = Object.fromEntries(
          Object.entries(candidate)
            .filter(([, value]) => typeof value === "string" && (value as string).length <= 500)
            .slice(0, 8),
        ) as Record<string, string>;
        if (!Object.keys(failureMetadata).length) failureMetadata = undefined;
      }
    } catch {
      // advisory only: unparseable metadata is omitted, never fatal
    }
  }
  // Justified no-op path (review-fix): worker claims every P0/P1 finding is already
  // satisfied or not code-actionable; reviewer re-arbitrates the unchanged candidate.
  const noChangeJustification = typeof parsed?.no_change_justification === "string" ? parsed.no_change_justification.trim().slice(0, 4000) : undefined;
  return { status: status as "done" | "partial" | "blocked" | "escalated", markdown: document, blocker, ...(escalation ? { escalation } : {}), ...(failureMetadata ? { failure_metadata: failureMetadata } : {}), ...(noChangeJustification ? { no_change_justification: noChangeJustification } : {}) };
}

export function completionSectionText(value: unknown): string {
  if (typeof value === "string" || typeof value === "number") return String(value).trim();
  if (Array.isArray(value)) return value.map(completionSectionText).filter(Boolean).join("\n");
  if (value && typeof value === "object") return Object.entries(value)
    .filter(([key]) => !key.startsWith("@_"))
    .map(([, child]) => completionSectionText(child))
    .filter(Boolean)
    .join("\n");
  return "";
}


export function parseReviewReport(output: string): { status: "passed" | "failed"; notes: string; findings: string[]; ownerApprovalRequired: boolean } {
  const outputText = output.trim();
  const start = outputText.indexOf("<review_report");
  const end = outputText.lastIndexOf("</review_report>");
  if (start < 0 || end < start || outputText.indexOf("<review_report", start + 1) >= 0) throw new Error("expected one review_report XML document");
  const document = outputText.slice(start, end + "</review_report>".length);
  if (!XMLValidator.validate(document)) throw new Error("review report contains invalid XML");
  const report = new XMLParser({ ignoreAttributes: false }).parse(document)?.review_report;
  const reviewStatus = report?.["@_status"];
  if (reviewStatus !== "passed" && reviewStatus !== "failed") throw new Error("invalid review status");
  if (typeof report?.notes !== "string" || !report.notes.trim()) throw new Error("review report notes are required");
  const findings = report.findings?.finding ? (Array.isArray(report.findings.finding) ? report.findings.finding : [report.findings.finding]) : [];
  if (!findings.every((finding: unknown) => typeof finding === "string" && finding.trim())) throw new Error("review report findings must be strings");
  const normalizedStatus = reviewStatus === "passed" && findings.length ? "failed" : reviewStatus;
  const ownerApprovalRequired = String(report.owner_approval_required || "").toLowerCase() === "true";
  if (normalizedStatus === "passed" && ownerApprovalRequired) throw new Error("passed review report cannot require owner approval");
  return { status: normalizedStatus, notes: report.notes, findings, ownerApprovalRequired };
}


export function persistedReviewOutcome(run: PipelineRun): { status: "passed" | "failed"; notes: string; findings: string[]; candidateRunId: string; candidatePatchHash: string } | undefined {
  try {
    const result = JSON.parse(run.result_json || "{}");
    if ((result.review_status !== "passed" && result.review_status !== "failed") || !Array.isArray(result.findings) || typeof result.candidate_run_id !== "string" || !result.candidate_run_id || typeof result.candidate_patch_hash !== "string" || !result.candidate_patch_hash) return undefined;
    return { status: result.review_status, notes: String(result.notes || ""), findings: result.findings.filter((finding: unknown): finding is string => typeof finding === "string"), candidateRunId: result.candidate_run_id, candidatePatchHash: result.candidate_patch_hash };
  } catch {
    return undefined;
  }
}
