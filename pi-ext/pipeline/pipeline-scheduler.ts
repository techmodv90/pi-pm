import { execFile, execFileSync } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { promisify } from "node:util";
import { join, matchesGlob } from "node:path";
import type { ExtensionAPI, ExtensionContext } from "@mariozechner/pi-coding-agent";
import { Type } from "typebox";
import { execGitIndexWrite, execPic, execPicText, withGitWriteLock } from "../core/cli-helpers.ts";

import { validateSkillFamilies } from "../subagent/skills.ts";
import { withInheritedParentWorkflowArtifacts } from "../tasking/task-artifacts.ts";
import { buildTaskVerifyPrompt, buildWorkItemReviewerHandoff, buildWorkItemScanPrompt } from "../tasking/work-item-prompts.ts";
import { getBlockingTaskDependencies } from "../tasking/workflow-gates.ts";
import { discoverAgents } from "../subagent/agents.ts";
import { cleanupOrphanedSubagentWorktrees, finalAssistantText, prepareSubagentWorktree, removeSubagentWorktree, startSubagent, type SubagentHandle } from "../subagent/runner.ts";

import type { SubagentResult } from "../subagent/types.ts";
import { parsePipelineRuns, type PipelineRunRecord, type PipelineStage } from "./pipeline-types.ts";

const execFileAsync = promisify(execFile);
const DEFAULT_GENERATED_FILES = ["test-results/**", "playwright-report/**", "coverage/**", ".nyc_output/**"];

function verificationEnvironmentFingerprint(cwd: string): string {
  const values = ["NODE_ENV", "CI", "DATABASE_URL", "TEST_DATABASE_URL", "PGHOST", "PGPORT", "PGDATABASE", "PGUSER"]
    .map((name) => `${name}=${process.env[name] || ""}`);
  try { values.push(`HEAD=${execFileSync("git", ["rev-parse", "HEAD"], { cwd, encoding: "utf8" }).trim()}`); } catch { values.push("HEAD=unknown"); }
  try { values.push(`DOCKER=${execFileSync("docker", ["ps", "--format", "{{.Names}}={{.Image}}={{.Status}}"], { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim()}`); } catch { values.push("DOCKER=unavailable"); }
  return createHash("sha256").update(values.join("\n")).digest("hex");
}

function repositoryHead(cwd: string): string {
  return execFileSync("git", ["rev-parse", "HEAD"], { cwd, encoding: "utf8" }).trim();
}

export interface AggregateDeliveryState {
  work_item_id: string;
  branch_name: string;
  base_branch: string;
  base_commit: string;
  verified_head: string;
}

export function mergeAggregateBranch(cwd: string, state: AggregateDeliveryState): string {
  return withGitWriteLock(cwd, () => {
    assertCleanGit(cwd);
    const branch = execFileSync("git", ["branch", "--show-current"], { cwd, encoding: "utf8" }).trim();
    if (branch !== state.branch_name || repositoryHead(cwd) !== state.verified_head) throw new Error("delivery branch changed after aggregate verification");
    execFileSync("git", ["fetch", "origin", state.base_branch], { cwd, stdio: "pipe" });
    const remoteBase = execFileSync("git", ["rev-parse", `refs/remotes/origin/${state.base_branch}`], { cwd, encoding: "utf8" }).trim();
    try {
      execFileSync("git", ["merge-base", "--is-ancestor", state.verified_head, remoteBase], { cwd, stdio: "pipe" });
      return remoteBase;
    } catch {}
    if (remoteBase !== state.base_commit) throw new Error(`${state.base_branch} changed after aggregate verification`);

    const tempRoot = mkdtempSync(join(tmpdir(), "pi-aggregate-merge-"));
    const worktree = join(tempRoot, "worktree");
    try {
      execFileSync("git", ["worktree", "add", "--detach", worktree, remoteBase], { cwd, stdio: "pipe" });
      execFileSync("git", ["merge", "--no-ff", state.verified_head, "-m", `merge(${state.work_item_id}): verified aggregate delivery`], { cwd: worktree, stdio: "pipe" });
      const mergeCommit = repositoryHead(worktree);
      execFileSync("git", ["push", "origin", `HEAD:${state.base_branch}`], { cwd: worktree, stdio: "pipe" });
      const remote = execFileSync("git", ["ls-remote", "origin", `refs/heads/${state.base_branch}`], { cwd, encoding: "utf8" }).trim().split(/\s+/)[0];
      if (remote !== mergeCommit) throw new Error(`remote ${state.base_branch} did not confirm merge ${mergeCommit}`);
      return mergeCommit;
    } finally {
      try { execFileSync("git", ["worktree", "remove", "--force", worktree], { cwd, stdio: "pipe" }); } catch {}
      rmSync(tempRoot, { recursive: true, force: true });
    }
  });
}

export function assertReviewBaseCurrent(run: PipelineRun, cwd: string): void {
  if (run.base_commit && run.base_commit !== repositoryHead(cwd)) throw new Error("review base changed before integration; a fresh review is required");
}

function isMutationStage(stage: string): boolean {
  return stage === "worker" || stage === "autofix";
}

type PipelineRun = PipelineRunRecord;

export function normalizePipelineData(data: any): any {
  if (!data?.work_item) return data;
  const approvedArtifactIds = new Set((data.checkpoints || []).map((checkpoint: any) => checkpoint.artifact_id));
  const approvedArtifacts = (data.artifacts || []).filter((artifact: any) => approvedArtifactIds.has(artifact.id)).map((artifact: any) => {
    try { return { ...artifact, ...JSON.parse(artifact.content || "{}"), status: "approved" }; }
    catch { return { ...artifact, status: "approved" }; }
  });
  const packs = (data.instruction_packs || []).map((pack: any) => {
    let content: any = {};
    try { content = JSON.parse(pack.content_json || "{}"); } catch {}
    return {
      ...pack,
      constraints_json: JSON.stringify(content.constraints || {}),
      skill_families_json: JSON.stringify(content.skillFamilies || []),
    };
  });
  return {
    ...data,
    canonical: true,
    dependencies: (data.dependencies || []).map((dependency: any) => ({
      ...dependency,
      depends_on_task_id: dependency.depends_on_task_id || dependency.depends_on_work_item_id,
    })),
    scan_reports: approvedArtifacts.filter((artifact: any) => artifact.stage === "scan"),
    rri_sessions: approvedArtifacts.filter((artifact: any) => artifact.stage === "rri"),
    designs: approvedArtifacts.filter((artifact: any) => artifact.stage === "blueprint"),
    instruction_packs: packs,
  };
}

function markdownLabel(key: string): string {
  const label = key.replace(/([a-z0-9])([A-Z])/g, "$1 $2").replace(/[_-]+/g, " ").toLowerCase();
  return label.charAt(0).toUpperCase() + label.slice(1);
}

function markdownObject(value: Record<string, any>, indent: string, nested: boolean): string {
  return Object.entries(value).map(([key, field], index) => {
    const fieldIndent = nested && index > 0 ? `${indent}  ` : indent;
    if (field && typeof field === "object" && !Array.isArray(field)) {
      return `${fieldIndent}- **${markdownLabel(key)}:**\n${markdownObject(field, `${fieldIndent}  `, false)}`;
    }
    if (Array.isArray(field) && field.some((item) => item && typeof item === "object")) {
      return `${fieldIndent}- **${markdownLabel(key)}:**\n${markdownItems(field, `${fieldIndent}  `)}`;
    }
    const text = Array.isArray(field) ? field.join(", ") || "None" : field == null || field === "" ? "None" : String(field);
    return `${fieldIndent}- **${markdownLabel(key)}:** ${text}`;
  }).join("\n") || `${indent}- None`;
}

function markdownItems(values: any, indent = ""): string {
  if (values && typeof values === "object" && !Array.isArray(values)) return markdownObject(values, indent, false);
  return (Array.isArray(values) ? values : values == null ? [] : [values])
    .map((value) => value && typeof value === "object" ? markdownObject(value, indent, true) : `${indent}- ${value}`)
    .join("\n") || `${indent}- None`;
}

function xmlEscape(value: unknown): string {
  return String(value ?? "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&apos;");
}

function xmlValue(tag: string, value: unknown): string {
  return `<${tag}>${xmlEscape(value)}</${tag}>`;
}

function xmlFields(value: unknown): string {
  if (!value || typeof value !== "object" || Array.isArray(value)) return xmlValue("value", value);
  return Object.entries(value).map(([key, field]) => {
    const tag = key.replace(/[^a-zA-Z0-9_.-]/g, "_");
    if (Array.isArray(field)) return `<${tag}>${field.map((entry) => xmlFields(entry)).join("")}</${tag}>`;
    if (field && typeof field === "object") return `<${tag}>${xmlFields(field)}</${tag}>`;
    return xmlValue(tag, field);
  }).join("");
}

function xmlCollection(tag: string, itemTag: string, value: unknown): string {
  const values = Array.isArray(value) ? value : value == null ? [] : [value];
  return `<${tag}>${values.map((entry) => `<${itemTag}>${entry && typeof entry === "object" ? xmlFields(entry) : xmlEscape(entry)}</${itemTag}>`).join("")}</${tag}>`;
}

export function validateInstructionPackXml(output: string): void {
  const root = output.trim().match(/^<instruction_pack\s+schema_version="1"\s+display_key="[^"]+"\s+id="[^"]+"\s+version="[^"]+"\s+content_hash="[^"]+">([\s\S]*)<\/instruction_pack>$/);
  if (!root) throw new Error("Worker handoff must be one versioned <instruction_pack> XML document");
  for (const element of ["pipeline_ownership", "handoff_validation", "header", "context", "task", "specifications", "requirements", "constraints", "verification", "report_format"]) {
    if (!root[1].includes(`<${element}>`) || !root[1].includes(`</${element}>`)) throw new Error(`Instruction Pack XML missing <${element}>`);
  }
}

export function renderCanonicalInstructionPackXml(item: any, pack: any): string {
  const envelope = JSON.parse(pack.content_json || "{}");
  const content = envelope.content || envelope;
  const requirements = Array.isArray(envelope.requirements)
    ? envelope.requirements
    : Array.isArray(content.requirement_snapshots) ? content.requirement_snapshots : [];
  const files = (content.files || []).map((file: unknown) => xmlValue("file", file)).join("");
  const patterns = (content.patterns || []).map((pattern: unknown) => `<pattern>${xmlFields(pattern)}</pattern>`).join("");
  const requirementXml = requirements.map((requirement: any) => `<requirement key="${xmlEscape(requirement.requirement_key || requirement.requirement_id || "Requirement")}">${xmlValue("title", requirement.title || "Acceptance")}${xmlValue("acceptance_criteria", requirement.acceptance_criteria || "")}</requirement>`).join("");
  const output = `<instruction_pack schema_version="1" display_key="${xmlEscape(pack.display_key || `TIP-${pack.version}`)}" id="${xmlEscape(pack.id)}" version="${xmlEscape(pack.version)}" content_hash="${xmlEscape(pack.content_hash)}">
  <pipeline_ownership>
    <instruction>The scheduler has already claimed and launched this Work Item. Implement the bounded scope directly.</instruction>
    <instruction>Do not call work_on_work_item, pipeline-claim, reset_pipeline_circuit, or other lifecycle-control actions from this worker.</instruction>
  </pipeline_ownership>
  <handoff_validation>${xmlValue("status", "READY")}${xmlValue("pack_status", pack.status)}${xmlValue("content_hash", pack.content_hash)}</handoff_validation>
  <header>${xmlValue("work_item", item.id)}${xmlValue("work_item_type", item.type)}${xmlValue("priority", item.priority || "medium")}${xmlCollection("skill_families", "skill_family", content.skill_families || content.skillFamilies)}</header>
  <context><working_directory>current process CWD is authoritative</working_directory><files>${files}</files><patterns>${patterns}</patterns></context>
  ${xmlValue("task", content.goal || item.description || item.title)}
  <specifications>${xmlCollection("business_rules", "rule", content.business_rules)}${xmlCollection("validation_rules", "rule", content.validation_rules)}${xmlCollection("error_handling", "rule", content.error_handling)}${xmlCollection("state_transitions", "transition", content.state_transitions)}${xmlCollection("contract_obligations", "obligation", content.contract_obligations)}</specifications>
  <requirements>${requirementXml}</requirements>
  <constraints>${xmlFields(content.constraints || {})}</constraints>
  ${xmlCollection("verification", "check", content.verification)}
  <report_format>Return the canonical Completion or Issue Report for pack ${xmlEscape(pack.id)} version ${xmlEscape(pack.version)} and content hash ${xmlEscape(pack.content_hash)}.</report_format>
</instruction_pack>`;
  validateInstructionPackXml(output);
  return output;
}

export function canonicalReadyLeafIds(root: any, load: (id: string) => any): string[] {
  const ready: string[] = [];
  const visit = (data: any): void => {
    const item = data?.work_item;
    if (!item || item.status === "cancelled") return;
    const activeChildren = (data.children || []).filter((child: any) => child.status !== "cancelled");
    if (activeChildren.length) {
      for (const child of activeChildren) visit(load(child.id));
      return;
    }
    if (["task", "bug", "chore"].includes(item.type)) {
      if (data.ready) ready.push(item.id);
    }
  };
  visit(root);
  return ready;
}

export function buildPipelineDryRun(root: any, load: (id: string) => any): any {
  const leaves: any[] = [];
  const visit = (data: any): void => {
    const item = data?.work_item;
    if (!item || item.status === "cancelled") return;
    if ((data.children || []).length) {
      for (const child of data.children) visit(load(child.id));
      return;
    }
    if (!["task", "bug", "chore"].includes(item.type)) return;
    leaves.push({ taskId: item.id, ready: Boolean(data.ready), stage: data.ready ? nextPipelineStage(normalizePipelineData(data)) : null, blocker: data.ready ? null : pipelineWorkerBlockReason(normalizePipelineData(data)) });
  };
  visit(root);
  return { rootTaskId: root?.work_item?.id, leaves };
}

export function pipelineWorkerBlockReason(data: any): string | null {
  const activePacks = (data.instruction_packs || []).filter((pack: any) => pack.status === "active");
  if (activePacks.length !== 1) return `Work Item "${data.work_item?.title || data.work_item?.id || "unknown"}" requires exactly one active Task Instruction Pack before work.`;
  const blockers = getBlockingTaskDependencies(data.dependencies || [], data.phase_metadata || null);
  if (blockers.length) return `Work Item "${data.work_item?.title || data.work_item?.id || "unknown"}" is blocked by incomplete dependencies: ${blockers.map((dependency: any) => dependency.depends_on_task_id).join(", ")}`;
  const activePack = activePacks[0];
  if (!data.canonical && (Number(activePack.content_schema_version || 1) < 3 || !activePack.effective_contract_snapshot_id || !activePack.effective_contract_snapshot_hash)) return `Work Item "${data.work_item?.title || "unknown"}" requires a schema-v3 Task Instruction Pack with an effective contract snapshot; revise and activate the TIP before launch.`;
  try {
    const families = JSON.parse(activePack.skill_families_json || "[]");
    validateSkillFamilies(families, { cwd: process.cwd() });
  } catch (error) {
    return error instanceof Error ? error.message : String(error);
  }
  return null;
}

export function assertRunContractCurrent(data: any, run: Pick<PipelineRun, "instruction_pack_id" | "instruction_pack_hash" | "effective_contract_snapshot_id" | "effective_contract_snapshot_hash">): void {
  const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
  if (data.canonical) {
    if (!activePack || activePack.id !== run.instruction_pack_id || activePack.content_hash !== run.instruction_pack_hash) {
      throw new Error("worker instruction pack changed; output quarantined until a revised TIP is activated");
    }
  } else if (!activePack
    || activePack.effective_contract_snapshot_id !== run.effective_contract_snapshot_id
    || activePack.effective_contract_snapshot_hash !== run.effective_contract_snapshot_hash) {
    throw new Error("worker effective contract changed; output quarantined until a revised TIP is activated");
  }
}

export function nextPipelineStage(data: any, runs: any[] = []): PipelineStage | null {
  if (data.canonical && data.execution_state) return data.execution_state.pipeline_stage || null;
  const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
  if (!activePack && (data.scan_reports || [])[0]?.status !== "completed") return "scan";
  if (!activePack) return null;
  const doneReports = activePackDoneReports(data, activePack);
  const latest = doneReports[0];
  if ((data.owner_decisions || []).some((decision: any) => decision.decision === "rejected" && decision.completion_report_id
    && (data.completion_reports || []).some((report: any) => report.id === decision.completion_report_id && report.instruction_pack_id === activePack.id))) return "worker";
  const candidate = runs.find((run: any) => isMutationStage(run.stage) && run.status === "completed"
    && run.instruction_pack_hash === activePack.content_hash && run.artifact_saved_at && run.integrated_patch_hash);
  if (!latest && candidate) {
    return reviewStatusForCandidate(runs, candidate) === "failed" ? "worker" : "review";
  }

  const done = Boolean(latest && runs.some((run: any) =>
    run.id === latest.pipeline_run_id && isMutationStage(run.stage) && run.status === "completed" && (run.artifact_saved_at || run.integrated_at) && run.integrated_patch_hash,
  ));
  if (doneReports.length && !done) return null;
  if (!done) return "worker";
  const completionCandidate = runs.find((run: any) => run.id === latest?.pipeline_run_id);
  const durableReviewStatus = reviewStatusForCandidate(runs, completionCandidate);
  const reviewStatus = data.canonical || runs.some((run: any) => run.stage === "review") ? durableReviewStatus : data.work_item?.review_status;
  if (reviewStatus === "failed") return "worker";
  const verification = latestVerificationAfter(data, latest);
  if (reviewStatus === "passed" && verification && (verification.status === "failed" || verification.status === "partial")) return "autofix";
  if (reviewStatus !== "passed") return "review";
  return null;
}

function reviewStatusForCandidate(runs: any[], candidate: any): "passed" | "failed" | "" {
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

function currentFailedReview(runs: any[], activePack: any): any | undefined {
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

function latestVerificationAfter(data: any, completion: any): any | undefined {
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

function activePackDoneReports(data: any, activePack: any): any[] {
  return (data.completion_reports || []).filter((report: any) => report.status === "done"
    && (!activePack.id || report.instruction_pack_id === activePack.id)
    && (!activePack.version || report.instruction_pack_version === activePack.version)
    && (!activePack.content_hash || report.instruction_pack_hash === activePack.content_hash)
    && !(data.owner_decisions || []).some((decision: any) => (decision.decision === "rejected" && decision.completion_report_id === report.id)
      || (decision.decision_type === "request_changes" && ((decision.related_type === "completion_report" && decision.related_id === report.id)
        || (!decision.related_type && decision.created_at >= report.created_at)))));
}

export function parseTaskCompletionReport(output: string): { status: "done" | "partial" | "blocked"; markdown: string } {
  const statusMatch = output.match(/\*\*STATUS:\*\*\s*(DONE|PARTIAL|BLOCKED)\b/i);
  if (!statusMatch) throw new Error("worker output missing canonical Completion/Issue Report status");
  for (const section of ["FILES CHANGED", "TEST RESULTS", "ISSUES DISCOVERED", "DEVIATIONS FROM SPEC", "SUGGESTIONS FOR CHỦ THẦU"]) {
    if (!output.includes(`**${section}:**`)) throw new Error(`worker output missing ${section}`);
  }
  return { status: statusMatch[1]!.toLowerCase() as "done" | "partial" | "blocked", markdown: output };
}


export function parseReviewReport(output: string): { status: "passed" | "failed"; notes: string; findings: string[] } {
  const matches = [...output.matchAll(/```review-report\s*\n([\s\S]*?)\n```/g)];
  if (matches.length !== 1) throw new Error("expected exactly one review-report block");
  const report = JSON.parse(matches[0]![1]!);
  if (report.status !== "passed" && report.status !== "failed") throw new Error("invalid review status");
  if (typeof report.notes !== "string" || !report.notes.trim()) throw new Error("review report notes are required");
  if (!Array.isArray(report.findings) || !report.findings.every((finding: unknown) => typeof finding === "string")) throw new Error("review report findings must be strings");
  if (report.status === "passed" && report.findings.length) throw new Error("passed review report cannot contain findings");
  return { status: report.status, notes: report.notes, findings: report.findings };
}


function persistedReviewOutcome(run: PipelineRun): { status: "passed" | "failed"; notes: string; findings: string[]; candidateRunId: string; candidatePatchHash: string } | undefined {
  try {
    const result = JSON.parse(run.result_json || "{}");
    if ((result.review_status !== "passed" && result.review_status !== "failed") || !Array.isArray(result.findings) || typeof result.candidate_run_id !== "string" || !result.candidate_run_id || typeof result.candidate_patch_hash !== "string" || !result.candidate_patch_hash) return undefined;
    return { status: result.review_status, notes: String(result.notes || ""), findings: result.findings.filter((finding: unknown): finding is string => typeof finding === "string"), candidateRunId: result.candidate_run_id, candidatePatchHash: result.candidate_patch_hash };
  } catch {
    return undefined;
  }
}

export function parseApplyNumstatPaths(output: Buffer | string): Set<string> {
  const fields = output.toString().split("\0");
  const paths = new Set<string>();
  for (let index = 0; index < fields.length && fields[index]; index++) {
    const parts = fields[index]!.split("\t");
    if (parts.length < 3) continue;
    if (parts[2]) paths.add(parts.slice(2).join("\t"));
    else {
      if (fields[index + 1]) paths.add(fields[++index]!);
      if (fields[index + 1]) paths.add(fields[++index]!);
    }
  }
  return paths;
}

export function parsePorcelainPaths(output: Buffer | string): Set<string> {
  const fields = output.toString().split("\0");
  const paths = new Set<string>();
  for (let index = 0; index < fields.length && fields[index]; index++) {
    const entry = fields[index]!;
    const status = entry.slice(0, 2);
    paths.add(entry.slice(3));
    if ((status.includes("R") || status.includes("C")) && fields[index + 1]) index++;
  }
  return paths;
}

export function assertIndexMatchesReviewedPatch(patch: string, cwd: string): void {
  const reviewed = readFileSync(patch);
  const staged = execFileSync("git", ["diff", "--cached", "--binary", "HEAD", "--", "."], { cwd });
  if (!reviewed.equals(staged)) throw new Error("staged integration differs from reviewed candidate patch");
}

function assertCommitMatchesReviewedPatch(patch: string, cwd: string, commitMessage: string, parent: string, commit: string): void {
  const actualMessage = execFileSync("git", ["log", "-1", "--format=%s", commit], { cwd, encoding: "utf8" }).trim();
  const committed = execFileSync("git", ["diff", "--binary", parent, commit, "--", "."], { cwd });
  if (actualMessage !== commitMessage || !readFileSync(patch).equals(committed)) {
    throw new Error("existing HEAD is not the reviewed integration commit");
  }
}

export function recoverReviewedPatch(patch: string, cwd: string, commitMessage: string): boolean {
  execFileSync("git", ["apply", "--reverse", "--check", patch], { cwd, stdio: "pipe" });
  const candidateFiles = parseApplyNumstatPaths(execFileSync("git", ["apply", "--numstat", "-z", patch], { cwd }));
  const dirtyFiles = parsePorcelainPaths(execFileSync("git", ["status", "--porcelain=v1", "-z"], { cwd }));
  const unrelated = [...dirtyFiles].filter((file) => !candidateFiles.has(file));
  if (unrelated.length) throw new Error(`integration recovery found unrelated repository changes: ${unrelated.join(", ")}`);
  if (!dirtyFiles.size) {
    assertCommitMatchesReviewedPatch(patch, cwd, commitMessage, "HEAD^", "HEAD");
    return true;
  }
  execGitIndexWrite(["add", "-A", "--", ...candidateFiles], cwd);
  assertIndexMatchesReviewedPatch(patch, cwd);
  return false;
}

export function finalizeReviewedIntegration(options: { patch: string; cwd: string; commitMessage: string; integrated: boolean; checkpoint: () => void }): void {
  if (options.integrated) return;
  const originalHead = execFileSync("git", ["rev-parse", "HEAD"], { cwd: options.cwd, encoding: "utf8" }).trim();
  let recovering = false;
  if (statSync(options.patch).size > 0) {
    try {
      assertCleanGit(options.cwd);
      execFileSync("git", ["apply", "--index", "--check", options.patch], { cwd: options.cwd, stdio: "pipe" });
      execFileSync("git", ["apply", "--index", options.patch], { cwd: options.cwd, stdio: "pipe" });
    } catch (applyError) {
      try {
        execFileSync("git", ["apply", "--reverse", "--check", options.patch], { cwd: options.cwd, stdio: "pipe" });
        recovering = true;
      } catch {
        throw applyError;
      }
    }
  }
  if (recovering && recoverReviewedPatch(options.patch, options.cwd, options.commitMessage)) {
    options.checkpoint();
    return;
  }
  assertIndexMatchesReviewedPatch(options.patch, options.cwd);
  try {
    execFileSync("git", ["diff", "--cached", "--quiet"], { cwd: options.cwd, stdio: "pipe" });
  } catch {
    const tree = execFileSync("git", ["write-tree"], { cwd: options.cwd, encoding: "utf8" }).trim();
    const commit = execFileSync("git", ["commit-tree", tree, "-p", originalHead, "-m", options.commitMessage], { cwd: options.cwd, encoding: "utf8" }).trim();
    assertCommitMatchesReviewedPatch(options.patch, options.cwd, options.commitMessage, originalHead, commit);
    execGitIndexWrite(["update-ref", "HEAD", commit, originalHead], options.cwd);
    assertCleanGit(options.cwd);
  }
  options.checkpoint();
}

function assertCleanGit(cwd: string): void {
  const root = execFileSync("git", ["rev-parse", "--show-toplevel"], { cwd, encoding: "utf8" }).trim();
  const status = execFileSync("git", ["status", "--porcelain"], { cwd: root, encoding: "utf8" });
  if (status.trim()) throw new Error("Async pipeline requires a clean Git working tree. Commit or stash changes first.");
  const trackedState = execFileSync("git", ["ls-files", "--", ".pi/tasks.db", ".pi/tasks.db-shm", ".pi/tasks.db-wal", ".pi-subagents"], { cwd: root, encoding: "utf8" }).trim();
  if (trackedState) throw new Error("Async pipeline requires task DB and subagent artifacts to be untracked. Remove them from the Git index first.");
  const ignoredPaths = [".pi-subagents/probe"];
  if (existsSync(join(root, ".pi", "tasks.db"))) ignoredPaths.push(".pi/tasks.db");
  for (const path of ignoredPaths) {
    try {
      execFileSync("git", ["check-ignore", "-q", path], { cwd: root, stdio: "pipe" });
    } catch {
      throw new Error("Async pipeline requires .pi/tasks.db* and .pi-subagents/ to be ignored by Git.");
    }
  }
}

export function pipelineSpawnParams(stage: PipelineStage, task: any, cwd: string): any {
  const spec: any = { agent: task.agent, task: task.task, cwd, stage, taskId: task.taskId, acceptance: stage === "review" ? "attested" : "checked", ...(task.skillFamilies ? { skillFamilies: task.skillFamilies } : {}) };
  if (isMutationStage(stage) || stage === "review") spec.isolation = "worktree";
  return spec;
}

const FULL_SCAN_SECTIONS = [
  ["Architecture", "Map stack, modules, boundaries, entry points, and data flow. Cite files and lines; do not estimate unrelated metrics."],
  ["Lifecycle", "Trace planning, materialization, authorization, execution, review, verification, acceptance, merge, cancellation, and reset state transitions."],
  ["Authority", "Audit actor-role checks, child-agent capabilities, persistence boundaries, immutability, and security risks. Distinguish implemented guards from gaps."],
  ["Verification", "Inspect manifests, test/build/typecheck commands, test layout, runtime prerequisites, and current blockers. Separate observed runs from historical evidence."],
  ["Reliability", "Inspect the gap ledger, open invariants, operational risks, migrations, generated artifacts, and documentation drift. Report exact statuses only."],
] as const;

const SCOUT_EVIDENCE_REQUIRED_ELEMENTS = ["scope", "findings", "gaps", "verification", "risks", "handoff_questions", "recommended_actions"] as const;

export function validateScoutEvidenceXml(output: string, section: string): void {
  const root = output.trim().match(/^<scout_evidence\s+section="([^"]+)"\s+confidence="(high|medium|low)">([\s\S]*)<\/scout_evidence>$/);
  if (!root || root[1] !== section.toLowerCase()) throw new Error(`Scout ${section} output must be one <scout_evidence section="${section.toLowerCase()}" confidence="high|medium|low"> document`);
  for (const element of SCOUT_EVIDENCE_REQUIRED_ELEMENTS) {
    if (!root[3].includes(`<${element}>`) || !root[3].includes(`</${element}>`)) throw new Error(`Scout ${section} evidence missing <${element}>`);
  }
  if (!/<source\s+path="[^"]+"(?:\s+line="[^"]+")?\s*>[\s\S]*<\/source>/.test(root[3])) throw new Error(`Scout ${section} evidence requires at least one source citation`);
}

function startFullScanFanout(spec: any, agent: any): SubagentHandle {
  const id = randomUUID();
  const handles = FULL_SCAN_SECTIONS.map(([section, assignment]) => startSubagent({
    ...spec,
    runId: undefined,
    agent,
    task: `${spec.task}\n\n<section_assignment name="${section.toLowerCase()}">${assignment}</section_assignment>\nReturn exactly one XML document matching the schema in your system prompt. Do not use Markdown outside XML and do not compose the canonical Scan Report.`,
  }));
  const result = Promise.all(handles.map((handle) => handle.result)).then((results): SubagentResult => {
    const failed = results.filter((entry) => entry.exitCode !== 0);
    const outputs = results.map((entry) => finalAssistantText(entry.messages) || entry.errorMessage || entry.stderr || "");
    outputs.forEach((output, index) => validateScoutEvidenceXml(output, FULL_SCAN_SECTIONS[index]![0]));
    const evidence = outputs.map((output, index) => output.replace("<scout_evidence ", `<scout_evidence run_id="${results[index]!.runId}" `)).join("\n");
    return {
      runId: id,
      agent: "task-scout-group",
      task: spec.task,
      exitCode: failed.length ? 1 : 0,
      messages: [{ role: "assistant", content: [{ type: "text", text: `<scan_evidence work_item="${spec.taskId || "unknown"}" scan_level="full">\n${evidence}\n</scan_evidence>\n\nContractor: validate each <scout_evidence> section, resolve contradictions against source, and author one canonical Scan Report. Do not persist any individual Scout output as the Scan artifact.` }] }],
      stderr: failed.map((entry) => entry.errorMessage || entry.stderr).filter(Boolean).join("\n"),
      usage: results.reduce((total, entry) => ({ input: total.input + entry.usage.input, output: total.output + entry.usage.output, cacheRead: total.cacheRead + entry.usage.cacheRead, cacheWrite: total.cacheWrite + entry.usage.cacheWrite, cost: total.cost + entry.usage.cost, contextTokens: Math.max(total.contextTokens, entry.usage.contextTokens), turns: total.turns + entry.usage.turns }), { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, cost: 0, contextTokens: 0, turns: 0 }),
    };
  });
  return { id, result, stop: () => handles.forEach((handle) => handle.stop()) };
}

function outputFor(run: PipelineRun): string {
  const path = join(run.async_dir || "", `output-${run.child_index || 0}.log`);
  if (!existsSync(path)) throw new Error(`subagent output missing: ${path}`);
  return readFileSync(path, "utf8");
}

function statusFor(run: PipelineRun): any {
  const path = join(run.async_dir || "", "status.json");
  if (!existsSync(path)) return null;
  const status = JSON.parse(readFileSync(path, "utf8"));
  if (status.state === "running" && Number.isInteger(status.pid)) {
    try {
      process.kill(status.pid, 0);
    } catch {
      return { ...status, state: "failed", error: "subagent process is no longer running" };
    }
  }
  return status;
}

export function formatPipelineStatus(result: any): string {
  const runs = Array.isArray(result?.runs) ? result.runs : [];
  if (!runs.length) return `Pipeline ${result?.task_id || "unknown"}: no runs`;
  const lines = [`Pipeline ${result.task_id || "unknown"}`];
  for (const run of runs) {
    const runId = run.subagent_run_id ? ` run=${String(run.subagent_run_id).slice(0, 8)}` : "";
    const model = run.agent_model ? ` model=${run.agent_model}` : "";
    const error = run.error ? ` error=${String(run.error).replace(/\s+/g, " ").slice(0, 120)}` : "";
    lines.push(`- ${run.stage || "unknown"} ${run.status || "unknown"} attempt=${run.attempt || 1}${runId}${model}${error}`);
  }
  return lines.join("\n");
}

export function formatPipelineStop(result: any): string {
  const cancelled = Array.isArray(result?.cancelled_runs) ? result.cancelled_runs.length : 0;
  return `Pipeline ${result?.task_id || "unknown"}: cancelled ${cancelled} run${cancelled === 1 ? "" : "s"}`;
}

function workerPatch(run: PipelineRun): string {
  return join(run.async_dir || "", "worktree-diffs", `task-${run.child_index || 0}-task-worker.patch`);
}

export function validateWorkerPatchArtifact(patchPath: string, outputPath: string, report: any): void {
  if (!existsSync(patchPath)) throw new Error(`worker patch missing: ${patchPath}; output: ${outputPath}`);
  if (statSync(patchPath).size === 0 && Array.isArray(report.changedFiles) && report.changedFiles.length > 0) {
    throw new Error(`worker artifact invalid: completion report claims changed files but patch is empty; output: ${outputPath}; patch: ${patchPath}`);
  }
}

function scopeCovers(path: string, patterns: string[]): boolean {
  return patterns.some((pattern) => pattern === "." || path === pattern || path.startsWith(`${pattern.replace(/\/+$/, "")}/`) || matchesGlob(path, pattern));
}

export function validateWorkerChangedFiles(changedFiles: string[], constraints: any): { unexpected: string[] } {
  const protectedPaths = [".git/**", ".pi/**", ".pi-subagents/**", ...(Array.isArray(constraints?.protected_paths) ? constraints.protected_paths : [])];
  const protectedChanges = changedFiles.filter((path) => scopeCovers(path, protectedPaths));
  if (protectedChanges.length) throw new Error(`worker changed protected path: ${protectedChanges.join(", ")}`);
  const roots = Array.isArray(constraints?.scope_roots) ? constraints.scope_roots : [];
  const approvalRequired = Array.isArray(constraints?.approval_required) ? constraints.approval_required : [];
  return { unexpected: [...new Set(changedFiles.filter((path) => (roots.length > 0 && !scopeCovers(path, roots)) || scopeCovers(path, approvalRequired)))] };
}

export function validateWorkerOutput(status: "done" | "partial" | "blocked", actualChangedFiles: string[], constraints: any): { reported: string[]; actual: string[]; mismatch: boolean; unexpected?: string[] } {
  if (status !== "done") return { reported: [], actual: [], mismatch: false };
  const actual = [...new Set<string>(actualChangedFiles)].sort();
  const scope = validateWorkerChangedFiles(actual, constraints);
  const result: { reported: string[]; actual: string[]; mismatch: boolean; unexpected?: string[] } = { reported: actual, actual, mismatch: false };
  if (scope.unexpected.length) result.unexpected = scope.unexpected;
  return result;
}

export function filterGeneratedFiles(paths: string[], constraints: any): { changedFiles: string[]; generatedFiles: string[] } {
  const patterns = [...DEFAULT_GENERATED_FILES, ...(Array.isArray(constraints?.generated_files) ? constraints.generated_files : [])];
  const generatedFiles = paths.filter((path) => patterns.some((pattern) => {
    const recursiveRoot = pattern.endsWith("/**") ? pattern.slice(0, -3).replace(/\/+$/, "") : "";
    return (recursiveRoot && (path === recursiveRoot || path.startsWith(`${recursiveRoot}/`))) || matchesGlob(path, pattern);
  }));
  return { changedFiles: paths.filter((path) => !generatedFiles.includes(path)), generatedFiles };
}

export function pipelineFailureResult(reason: string): Record<string, string> {
  if (reason.includes("worker artifact invalid") || reason.includes("worker patch missing")) return { failure_code: "worker_artifact_invalid" };
  if (reason.includes("autofix made no repository changes")) return { failure_code: "no_progress_autofix" };
  if (reason.includes("Agent is already processing") || reason.includes("stale or invalid lease") || reason.includes("lease expired") || reason.includes("pipeline bind rejected")) return { failure_code: "runner_protocol_invalid" };
  if (reason.includes("completion report missing")) return { failure_code: "runner_protocol_invalid" };
  if (reason.includes("required verification did not pass") && /docker|postgres|TEST_DATABASE_URL|database|connection refused|connection reset/i.test(reason)) return { failure_code: "environment_blocked" };
  if (reason.includes("worker output") || reason.includes("required verification did not pass") || reason.includes("unsatisfied criteria") || reason.includes("changedFiles do not match")) return { failure_code: "worker_output_invalid" };
  return {};
}

function checkpoint(run: PipelineRun, name: "integrated" | "artifact_saved" | "advanced", cwd: string, patchFile = ""): void {
  const args = ["workflow", "pipeline-checkpoint", run.id, run.lease_token, name];
  if (patchFile) args.push("--patch-file", patchFile);
  try {
    execPicText(args, cwd);
  } catch (error: any) {
    const message = error?.stderr?.toString().trim() || error?.message || String(error);
    if (!message.includes("already recorded")) throw new Error(message);
  }
}

function saveWorkerReport(run: PipelineRun, cwd: string, taskReport: { status: "done" | "partial" | "blocked"; markdown: string }, report: any = { changedFiles: [], commandsRun: [], criteriaSatisfied: [], diffSummary: `Async worker ${taskReport.status}`, reviewFindings: [], residualRisks: [] }): void {
  const result = execPic([
    "workflow", "completion-save", run.task_id, taskReport.status,
    "--pipeline-run-id", run.id,
    "--summary", report.diffSummary || `Async worker ${taskReport.status}`,
    "--report-markdown", taskReport.markdown,
    "--files-changed-json", JSON.stringify(report.changedFiles || []),
    "--tests-run-json", JSON.stringify(report.commandsRun || []),
    "--acceptance-results-json", JSON.stringify(report.criteriaSatisfied || []),
    "--issues-json", JSON.stringify(report.reviewFindings || []),
    "--deviations-json", "[]",
    "--suggestions-json", JSON.stringify(report.residualRisks || []),
  ], cwd);
  if (result.error) throw new Error(result.error);
}

function stageAgent(stage: PipelineStage): string {
  return ({ scan: "task-scout", worker: "task-worker", review: "task-reviewer", autofix: "task-worker" } as const)[stage];
}

export function buildWorkerCorrectionContext(data: any): string {
  if (!data?.current_review && (data.canonical || data?.work_item?.review_status !== "failed")) return "";
  const findings = data.current_review
    ? [data.current_review.notes, ...(data.current_review.findings || []).map((finding: string) => `- ${finding}`)].filter(Boolean).join("\n\n")
    : String(data.work_item.review_notes || "").trim();
  if (!findings) throw new Error("failed review is missing persisted correction findings");
  return `\n\n## REVIEW CORRECTIONS\nThe rejected candidate is already applied to the assigned worktree. This is a review-fix run, not a verification-only run: make the required edits for every finding below and return a non-empty patch whose SHA-256 differs from the rejected candidate. Do not report DONE or claim the fix is complete without changing the worktree. Git-derived changed files will be assessed by Reviewer.\n\n${findings}\n`;
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

export function workerIntegrationCandidate(runs: PipelineRun[]): PipelineRun | undefined {
  return runs.find((run) => isMutationStage(run.stage) && run.artifact_saved_at && !run.integrated_at && !run.advanced_at);
}

export function assertReviewFixChangedPatch(run: PipelineRun, patch: Buffer): void {
  if ((run.review_fix_cycle || 0) < 1 || !run.candidate_patch_hash) return;
  const patchHash = createHash("sha256").update(patch).digest("hex");
  if (patchHash === run.candidate_patch_hash) throw new Error("review-fix produced the unchanged rejected candidate patch");
}

function stagePrompt(stage: PipelineStage, taskId: string, cwd: string): string {
  const raw = execPic(["show", taskId], cwd);
  if (raw.work_item) {
    if (stage === "scan") return buildWorkItemScanPrompt(raw.work_item, raw.project);
    const data = normalizePipelineData(raw);
    const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
    if (!activePack) throw new Error(`Work Item ${taskId} requires one active instruction pack`);
    if (stage === "review") return buildWorkItemReviewerHandoff(taskId);
    if (stage === "autofix") return renderCanonicalInstructionPackXml(data.work_item, activePack) + buildAutofixContext(data);
    const currentReview = currentFailedReview(parsePipelineRuns(execPic(["workflow", "pipeline-runs", taskId], cwd)), activePack);
    return renderCanonicalInstructionPackXml(data.work_item, activePack) + buildWorkerCorrectionContext({ ...data, current_review: currentReview }) + buildOwnerRejectionContext(data);
  }
  const data = withInheritedParentWorkflowArtifacts(raw, cwd);
  if (stage === "scan") return buildWorkItemScanPrompt(data.work_item, data.project);
  if (stage === "review") return buildWorkItemReviewerHandoff(taskId);
  if (stage === "autofix") {
    const verificationReports = execPic(["workflow", "verifications", taskId], cwd);
    return execPicText(["workflow", "instruction-pack-render", taskId], cwd) + buildAutofixContext({ ...data, verification_reports: Array.isArray(verificationReports) ? verificationReports : data.verification_reports });
  }
  return execPicText(["workflow", "instruction-pack-render", taskId], cwd) + buildWorkerCorrectionContext(data);
}

function rejectedCandidatePatch(data: any, runs: PipelineRun[]): string | undefined {
  const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
  const failedReview = currentFailedReview(runs, activePack);
  const candidate = failedReview?.candidate;
  if (!failedReview || !candidate || candidate.integrated_at) return undefined;
  if (!candidate?.integrated_patch_path || !existsSync(candidate.integrated_patch_path)) throw new Error("failed review candidate patch is unavailable");
  return candidate.integrated_patch_path;
}

export class PipelineScheduler {
  private cwd = "";
  private integrating = Promise.resolve();
  private reconciling = false;
  private context?: ExtensionContext;
  private lastError = "";

  private roots = new Set<string>();
  private readonly pi: ExtensionAPI;
  private agentRuns = new Map<string, PipelineRun>();
  private agentHandles = new Map<string, SubagentHandle>();

  constructor(pi: ExtensionAPI) { this.pi = pi; }

  private async persistAgentResult(result: SubagentResult): Promise<void> {
    const run = this.agentRuns.get(result.runId);
    if (!run?.async_dir) return;

    try {
      // Completion handoff must yield after HerdR closes; patch inspection and pic reconciliation are synchronous.
      await new Promise<void>((resolve) => setImmediate(resolve));
      const completed = result.exitCode === 0 && result.stopReason !== "aborted";
      const status = completed ? "completed" : "failed";
      const output = finalAssistantText(result.messages) || result.stderr || result.errorMessage || "";
      writeFileSync(join(run.async_dir, `output-${run.child_index || 0}.log`), output, { mode: 0o600 });
      if (result.workspace) writeFileSync(join(run.async_dir, "workspace.json"), JSON.stringify(result.workspace, null, 2), { mode: 0o600 });
      if (completed && isMutationStage(run.stage)) await this.writeWorkerPatch(run, result);
      writeFileSync(join(run.async_dir, "status.json"), JSON.stringify({ state: completed ? "completed" : "failed", error: result.errorMessage || result.stderr || "", steps: [{ status, model: result.model || "" }] }), { mode: 0o600 });
      if (result.workspace?.assignedWorktree) removeSubagentWorktree(this.cwd, result.workspace.assignedWorktree, result.runId);
      this.agentHandles.delete(result.runId);
      this.agentRuns.delete(result.runId);
      this.queueReconcile();
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (run.async_dir) writeFileSync(join(run.async_dir, "status.json"), JSON.stringify({ state: "failed", error: message, steps: [{ status: "failed", error: message }] }), { mode: 0o600 });
      if (result.workspace?.assignedWorktree) try { removeSubagentWorktree(this.cwd, result.workspace.assignedWorktree, result.runId); } catch {}
      this.agentHandles.delete(result.runId);
      this.agentRuns.delete(result.runId);
      if (this.context) this.reportError(error, this.context);
      this.queueReconcile();
    }
  }

  private queueReconcile(): void {
    setImmediate(() => { void this.reconcileSafely(); });
  }


  private async reconcileSafely(): Promise<void> {
    try {
      await this.reconcile();
    } catch (error) {
      if (this.context) this.reportError(error, this.context);
    }
  }

  private async writeWorkerPatch(run: PipelineRun, result: SubagentResult): Promise<void> {
    if (!run.async_dir) return;
    const worktree = result.workspace?.assignedWorktree;
    if (!worktree) throw new Error("worker result missing assigned worktree");
    const gitToplevel = (await execFileAsync("git", ["-C", worktree, "rev-parse", "--show-toplevel"], { encoding: "utf8" })).stdout.trim();
    if (realpathSync(gitToplevel) !== realpathSync(worktree)) throw new Error(`worker worktree invariant failed after exit: assigned=${worktree} git_toplevel=${gitToplevel}`);
    const data = execPic(["show", run.task_id], this.cwd);
    const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
    const constraints = JSON.parse(activePack?.constraints_json || "{}");
    await execFileAsync("git", ["-C", worktree, "add", "-N", "--", "."], { encoding: "utf8" });
    const changedResult = await execFileAsync("git", ["-C", worktree, "diff", "--name-only", "HEAD"], { encoding: "utf8" });
    const filtered = filterGeneratedFiles(changedResult.stdout.trim().split("\n").filter(Boolean), constraints);
    const changedFiles = filtered.changedFiles;
    if (result.workspace) result.workspace.changedFiles = changedFiles;
    if (result.workspace) result.workspace.generatedFiles = filtered.generatedFiles;
    const excluded = [...DEFAULT_GENERATED_FILES, ...(Array.isArray(constraints.generated_files) ? constraints.generated_files : [])].map((pattern) => `:(exclude,glob)${pattern}`);
    const patchResult = await execFileAsync("git", ["-C", worktree, "diff", "--binary", "HEAD", "--", ".", ...excluded], { encoding: "utf8", maxBuffer: 100 * 1024 * 1024 });
    const dir = join(run.async_dir, "worktree-diffs");
    mkdirSync(dir, { recursive: true, mode: 0o700 });
    const patch = workerPatch(run);
    writeFileSync(patch, patchResult.stdout, { mode: 0o600 });
    validateWorkerPatchArtifact(patch, join(run.async_dir, `output-${run.child_index || 0}.log`), { changedFiles: changedFiles });
    writeFileSync(join(run.async_dir, "workspace.json"), JSON.stringify(result.workspace, null, 2), { mode: 0o600 });
  }

  startSession(ctx: ExtensionContext): void {
    this.cwd = ctx.cwd;
    this.context = ctx;
  }


  stopSession(): void {
    this.context = undefined;
  }

  private reportError(error: unknown, ctx: ExtensionContext): void {
    const message = error instanceof Error ? error.message : String(error);
    ctx.ui.setStatus("task-pipeline", undefined);
    if (message === this.lastError) return;
    this.lastError = message;
    if (message.includes("autofix cycle limit reached")) {
      this.pi.sendUserMessage(
        "Targeted autofix stopped after three completed fix cycles for the unchanged active TIP. Review the persisted verification evidence and choose one owner action: revise the TIP, accept the remaining failure as explicit debt, or stop the task.",
        { deliverAs: "followUp" },
      );
      return;
    }
    if (message.includes("worker circuit breaker open")) {
      this.pi.sendUserMessage(
        "The worker circuit breaker stopped this task after a deterministic failure for the unchanged active TIP. Do not modify the task-system extension from this application session. After the runner or report protocol is repaired separately, record an owner circuit reset with evidence before retrying.",
        { deliverAs: "followUp" },
      );
      return;
    }
    if (message.includes("deterministic contract failure requires TIP revision")) {
      this.pi.sendUserMessage(
        "The worker retry was not launched because the active TIP has a deterministic failure. Revise and activate a new TIP, then explicitly retry; the unchanged pack cannot continue.",
        { deliverAs: "followUp" },
      );
      return;
    }
    this.stopSession();
    this.pi.sendUserMessage(`Async pipeline paused: ${message}`, { deliverAs: "followUp" });
    ctx.ui.notify(`Async pipeline paused: ${message}`, "warning");
  }

  private reportProgress(runId: string, taskId: string, stage: PipelineStage, event: string, text: string): void {
    try { this.pi.events.emit("task-pipeline:progress", { runId, taskId, stage, event, text }); } catch {}
    const ctx = this.context;
    if (ctx) try { ctx.ui.setStatus("task-pipeline", `${stage} ${taskId}: ${event}`); } catch {}
  }

  private notifyBlockedAttempt(run: PipelineRun, reason: string): void {
    const attempt = run.attempt || 1;
    const integrated = this.cwd && this.pipelineRuns(run.task_id).some((candidate) => candidate.id === run.id && candidate.integrated_at);
    const patchState = integrated ? "The patch was integrated before the pipeline paused." : "No patch was integrated; the repository was not changed by this attempt.";
    const nextAction = "Review the blocker, correct the worker or runner issue, then explicitly retry.";
    this.pi.sendUserMessage(
      `${run.task_id} ${run.stage} attempt ${attempt} is blocked.\n\nReason: ${reason}\n\n${patchState}\n\n${nextAction}`,
      { deliverAs: "followUp" },
    );
  }

  async start(rootTaskId: string, ctx: ExtensionContext): Promise<any> {
    this.cwd = ctx.cwd;
    this.context = ctx;
    this.lastError = "";
    this.roots.add(rootTaskId);
    setImmediate(() => {
      void (async () => {
        try {
          const workflow = execPic(["work-item", "workflow-status", rootTaskId], ctx.cwd);
          if (workflow.next_stage === "scan") {
            const rejection = execPic(["work-item", "scan-rejection", rootTaskId], ctx.cwd);
            if (rejection.rejected) {
              throw new Error(`Scan report was rejected by the contractor: ${rejection.reason}. Owner decision required: call reset_work_item_planning with actor_role=owner to rescan, or leave the Work Item at Scan and do not retry.`);
            }
            await this.launchGroup("scan", [rootTaskId]);
            return;
          }
          assertCleanGit(ctx.cwd);
          await this.reconcile();
          await this.scheduleReady(rootTaskId);
        } catch (error) {
          this.reportError(error, ctx);
        }
      })();
    });
    return { rootTaskId, status: "accepted" };
  }

  dryRun(rootTaskId: string, ctx: ExtensionContext): any {
    const root = execPic(["show", rootTaskId], ctx.cwd);
    if (!root.work_item) return { rootTaskId, leaves: [], blocker: "Work Item not found" };
    return buildPipelineDryRun(root, (id) => execPic(["show", id], ctx.cwd));
  }

  status(taskId: string, ctx: ExtensionContext): any {
    const active = execPic(["workflow", "pipeline-active"], ctx.cwd);
    if (Array.isArray(active)) cleanupOrphanedSubagentWorktrees(ctx.cwd, new Set(active.flatMap((run: PipelineRun) => [run.id, run.subagent_run_id || ""]).filter(Boolean)));
    const activeRun = Array.isArray(active)
      ? active.find((run: PipelineRun) => run.id === taskId || run.subagent_run_id === taskId)
      : undefined;
    if (activeRun) return { task_id: activeRun.task_id, pipeline_run_id: activeRun.id, subagent_run_id: activeRun.subagent_run_id, runs: [activeRun] };
    const root = execPic(["show", taskId], ctx.cwd);
    const taskIds = root.work_item
      ? [taskId, ...(root.children || []).map((child: any) => child.id)]
      : [taskId];
    const runs = taskIds.flatMap((id: string) => {
      const runs = execPic(["workflow", "pipeline-runs", id], ctx.cwd);
      return Array.isArray(runs) ? runs : [];
    });
    return { task_id: taskId, runs, error: runs.length ? "" : this.lastError };
  }

  async stop(taskId: string, ctx: ExtensionContext): Promise<any> {
    const status = this.status(taskId, ctx);
    const active = (status.runs || []).filter((run: PipelineRun & { status: string }) => run.status === "claimed" || run.status === "running");
    const runIds = [...new Set(active.map((run: PipelineRun) => run.subagent_run_id).filter(Boolean))];
    for (const run of active) {
      const cancelled = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "cancelled", "--error", "cancelled by operator"], ctx.cwd);
      if (cancelled.error) throw new Error(cancelled.error);
    }
    for (const runId of runIds) this.agentHandles.get(String(runId))?.stop();
    return { task_id: taskId, cancelled_runs: active.map((run: PipelineRun) => run.id) };
  }

  async mergeAggregate(workItemId: string, ctx: ExtensionContext): Promise<any> {
    const state = execPic(["work-item", "workflow-status", workItemId], ctx.cwd) as AggregateDeliveryState & { next_stage?: string; integration_mode?: string };
    if (state.integration_mode === "coordination" && state.next_stage === "done") return state;
    if (state.next_stage !== "merge_pending" || state.integration_mode !== "branch") throw new Error(`Work Item ${workItemId} is not awaiting a branch merge`);
    try {
      const mergeCommit = mergeAggregateBranch(ctx.cwd, state);
      const result = execPic(["work-item", "aggregate-merge-result", workItemId, state.verified_head, "merged", mergeCommit], ctx.cwd);
      if (result.error) throw new Error(result.error);
      return result;
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      const blocked = execPic(["work-item", "aggregate-merge-result", workItemId, state.verified_head, "blocked", message], ctx.cwd);
      if (blocked.error) throw new Error(`${message}; failed to persist merge blocker: ${blocked.error}`);
      throw new Error(message);
    }
  }

  private async scheduleReady(rootTaskId: string, explicitRetry = false): Promise<any> {
    const root = execPic(["show", rootTaskId], this.cwd);
    if (root.work_item) {
      const taskIds = canonicalReadyLeafIds(root, (id) => execPic(["show", id], this.cwd));
      if (!taskIds.length) return { rootTaskId, launches: [], blocked: "No authorized dependency-ready executable Work Items" };
      await new Promise<void>((resolve) => setImmediate(resolve));
      const stages = new Map<PipelineStage, string[]>();
      for (const taskId of taskIds) {
        const data = normalizePipelineData(execPic(["show", taskId], this.cwd));
        const stage = nextPipelineStage(data, this.pipelineRuns(taskId));
        if (stage) stages.set(stage, [...(stages.get(stage) || []), taskId]);
      }
      const launches = [];
      for (const [stage, ids] of stages) launches.push(await this.launchGroup(stage, ids, explicitRetry));
      return { rootTaskId, launches };
    }
    return { rootTaskId, launches: [], blocked: "Work Item not found" };
  }


  private async launchGroup(stage: PipelineStage, taskIds: string[], explicitRetry = false): Promise<any> {
    const active = execPic(["workflow", "pipeline-active"], this.cwd);
    const activeRuns = Array.isArray(active) ? active.filter((run: PipelineRun) => run.stage === stage && taskIds.includes(run.task_id)) : [];
    const activeTaskIds = new Set(activeRuns.map((run: PipelineRun) => run.task_id));
    const launchTaskIds = taskIds.filter((taskId) => !activeTaskIds.has(taskId));
    if (launchTaskIds.length === 0) return { stage, taskIds, pipelineRunIds: [], activePipelineRunIds: activeRuns.map((run: PipelineRun) => run.id), subagentRunIds: [] };
    const workerPrompts = new Map<string, string>();
    const initialPatchPaths = new Map<string, string>();
    const reviewFixTaskIds = new Set<string>();
    if (isMutationStage(stage)) {
      assertCleanGit(this.cwd);
      for (const taskId of launchTaskIds) {
        const raw = execPic(["show", taskId], this.cwd);
        const data = raw.work_item ? normalizePipelineData(raw) : withInheritedParentWorkflowArtifacts(raw, this.cwd);
        if (!data.work_item) throw new Error(data.error || `Task ${taskId} not found`);
        const blockReason = pipelineWorkerBlockReason(data);
        if (blockReason) throw new Error(blockReason);
        workerPrompts.set(taskId, stagePrompt(stage, taskId, this.cwd));
        const runs = this.pipelineRuns(taskId);
        const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
        if (stage === "worker" && currentFailedReview(runs, activePack)) {
          reviewFixTaskIds.add(taskId);
          initialPatchPaths.set(taskId, rejectedCandidatePatch(data, runs)!);
        }
      }
    }
    const claims: PipelineRun[] = [];
    try {
      for (const taskId of launchTaskIds) {
        const data = execPic(["show", taskId], this.cwd);
        const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
        const claimArgs = ["workflow", "pipeline-claim", taskId, stage, "--lease-seconds", "14400", "--environment-fingerprint", verificationEnvironmentFingerprint(this.cwd), "--base-commit", repositoryHead(this.cwd)];
        if (stage === "worker" && reviewFixTaskIds.has(taskId)) claimArgs.push("--review-fix", "1");
        if (stage === "worker" && explicitRetry) claimArgs.push("--explicit-retry", "1");
        if (activePack && (isMutationStage(stage) || stage === "review")) claimArgs.push("--instruction-pack-id", activePack.id, "--instruction-pack-hash", activePack.content_hash);
        const claim = execPic(claimArgs, this.cwd);
        if (claim.error) throw new Error(claim.error);
        claims.push(claim);
      }
      if (isMutationStage(stage)) {
        await new Promise<void>((resolve) => setImmediate(resolve));
        for (const taskId of launchTaskIds) {
          const raw = execPic(["show", taskId], this.cwd);
          const reset = execPic(["work-item", "status", taskId, "in_progress"], this.cwd);
          if (reset.error) throw new Error(reset.error);
          if (!raw.work_item) {
            const event = execPic(["workflow", "event-add", taskId, "implementation_started", "--actor-role", "orchestrator", "--summary", stage === "autofix" ? "Targeted autofix started" : "Persisted Worker stage started"], this.cwd);
            if (event.error) throw new Error(event.error);
          }
        }
      }
      const subagentRunIds: string[] = [];
      await new Promise<void>((resolve) => setImmediate(resolve));
      for (let index = 0; index < claims.length; index++) {
        const claim = claims[index]!;
        const taskId = launchTaskIds[index]!;
        const data = normalizePipelineData(execPic(["show", taskId], this.cwd));
        const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
        let skillFamilies: string[] = [];
        if (activePack?.skill_families_json) {
          const parsed = JSON.parse(activePack.skill_families_json);
          if (!Array.isArray(parsed) || !parsed.every((family) => typeof family === "string")) throw new Error(`Task ${taskId} has invalid persisted skill families`);
          skillFamilies = parsed;
        }
        const taskPrompt = workerPrompts.get(taskId) || stagePrompt(stage, taskId, this.cwd);
        const task = { agent: stageAgent(stage), task: taskPrompt, taskId, ...(isMutationStage(stage) || stage === "review" ? { skillFamilies } : {}) };
        const spec = pipelineSpawnParams(stage, task, this.cwd);
        if (stage === "worker") spec.initialPatchPath = initialPatchPaths.get(taskId);
        if (stage === "review") {
          const candidate = this.pipelineRuns(taskId).find((entry) => entry.id === claim.candidate_run_id);
          if (!candidate?.integrated_patch_path || candidate.integrated_patch_hash !== claim.candidate_patch_hash || !existsSync(candidate.integrated_patch_path)) {
            throw new Error("review candidate patch attestation failed");
          }
          spec.initialPatchPath = candidate.integrated_patch_path;
        }
        const agent = discoverAgents(this.cwd, "project").find((candidate) => candidate.name === spec.agent);
        if (!agent) throw new Error(`Task-system agent definition not found: ${spec.agent}`);
        if (spec.isolation === "worktree") {
          let prepared;
          try {
            prepared = await prepareSubagentWorktree(spec.cwd, spec.initialPatchPath, claim.id);
          } catch (error) {
            if (stage === "review") {
              const candidate = this.pipelineRuns(taskId).find((entry) => entry.id === claim.candidate_run_id);
              if (candidate) {
                execPic(["workflow", "pipeline-complete", candidate.id, candidate.lease_token, "blocked", "--error", "candidate patch no longer applies to the current integration base"], this.cwd);
                checkpoint(candidate, "advanced", this.cwd);
              }
            }
            throw error;
          }
          spec.runId = prepared.runId;
          spec.preparedWorktree = prepared.cwd;
        }
        let runId = "";
        let handle: SubagentHandle;
        try {
          handle = stage === "scan" && ["epic", "feature"].includes(data.work_item?.type)
            ? startFullScanFanout(spec, agent)
            : startSubagent({ ...spec, agent }, (update) => {
                this.reportProgress(runId, taskId, stage, update.event, finalAssistantText(update.result.messages));
              });
        } catch (error) {
          if (spec.preparedWorktree && spec.runId) removeSubagentWorktree(this.cwd, spec.preparedWorktree, spec.runId);
          throw error;
        }
        runId = handle.id;
        const artifactDir = join(this.cwd, ".pi-subagents", "pipeline", claim.id);
        mkdirSync(artifactDir, { recursive: true, mode: 0o700 });
        writeFileSync(join(artifactDir, "status.json"), JSON.stringify({ state: "running", pid: handle.pid, steps: [{ status: "running" }] }), { mode: 0o600 });
        this.agentRuns.set(runId, { ...claim, skillFamilies, taskPrompt, subagent_run_id: runId, async_dir: artifactDir, child_index: 0 });
        this.agentHandles.set(runId, handle);
        void handle.result.then((result) => this.persistAgentResult(result));
        const bound = execPic(["workflow", "pipeline-bind", claim.id, claim.lease_token, runId, "--async-dir", artifactDir, "--child-index", "0"], this.cwd);
        if (bound.error) {
          handle.stop();
          throw new Error(bound.error);
        }
        subagentRunIds.push(runId);
      }
      return {
        stage,
        taskIds: launchTaskIds,
        pipelineRunIds: claims.map((claim) => claim.id),
        activePipelineRunIds: [...activeRuns.map((run: PipelineRun) => run.id), ...claims.map((claim) => claim.id)],
        subagentRunIds,
      };
    } catch (error) {
      for (const claim of claims) execPic(["workflow", "pipeline-complete", claim.id, claim.lease_token, "failed", "--error", error instanceof Error ? error.message : String(error)], this.cwd);
      throw error;
    }
  }

  private async reconcile(): Promise<void> {
    if (!this.cwd || this.reconciling) return;
    this.reconciling = true;
    try {
      const active = execPic(["workflow", "pipeline-active"], this.cwd);
      if (!Array.isArray(active)) return;
      cleanupOrphanedSubagentWorktrees(this.cwd, new Set(active.flatMap((run: PipelineRun) => [run.id, run.subagent_run_id || ""]).filter(Boolean)));
      for (const run of active as PipelineRun[]) {
        const renewed = execPic(["workflow", "pipeline-renew", run.id, run.lease_token], this.cwd);
        if (renewed.error) continue;
        const status = statusFor(run);
        if (!status || status.state === "running" || status.state === "queued") continue;
        const childStatus = status.steps?.[run.child_index || 0]?.status || status.state;
        if (childStatus === "running" || childStatus === "queued") continue;
        if (childStatus !== "complete" && childStatus !== "completed") {
          const childError = status.steps?.[run.child_index || 0]?.error;
          const reason = childError || status.error || `subagent child ${childStatus}`;
          execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "failed", "--error", reason], this.cwd);
          checkpoint(run, "advanced", this.cwd);
          this.notifyBlockedAttempt(run, reason);
          continue;
        }
        this.integrating = this.integrating.then(() => this.finish(run, status)).catch(() => undefined);
        await this.integrating;
      }
      const pending = execPic(["workflow", "pipeline-pending"], this.cwd);
      if (Array.isArray(pending)) {
        for (const run of pending as PipelineRun[]) await this.resumePending(run);
      }
    } finally {
      this.reconciling = false;
    }
  }

  private async finish(run: PipelineRun, status: any): Promise<void> {
    let reviewCompleted = false;
    try {
      const child = status.steps?.[run.child_index || 0] || {};
      const resolvedModel = child.model || child.resolvedModel || child.modelAttempts?.findLast?.((attempt: any) => attempt.success)?.model || "";
      if (resolvedModel) execPic(["workflow", "pipeline-model", run.id, run.lease_token, resolvedModel], this.cwd);
      if (isMutationStage(run.stage)) {
        const output = outputFor(run);
        const taskReport = parseTaskCompletionReport(output);
        if (!run.artifact_saved_at) {
          // Provenance comes from the persisted claim; Workers need not echo hashes in prose.
          if (taskReport.status === "done") {
            const workspacePath = join(run.async_dir || "", "workspace.json");
            if (!existsSync(workspacePath)) throw new Error(`worker workspace diagnostics missing: ${workspacePath}`);
            const workspace = JSON.parse(readFileSync(workspacePath, "utf8"));
            const data = normalizePipelineData(execPic(["show", run.task_id], this.cwd));
            assertRunContractCurrent(data, run);
            const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
            const constraints = JSON.parse(activePack?.constraints_json || "{}");
            const actualChangedFiles = filterGeneratedFiles(workspace.changedFiles || [], constraints).changedFiles;
            const normalizedReport = { changedFiles: actualChangedFiles };
            validateWorkerOutput(taskReport.status, actualChangedFiles, constraints);
            const patch = workerPatch(run);
            const outputPath = join(run.async_dir || "", `output-${run.child_index || 0}.log`);
            validateWorkerPatchArtifact(patch, outputPath, normalizedReport);
            assertReviewFixChangedPatch(run, readFileSync(patch));
            if (run.stage === "autofix" && statSync(patch).size === 0) throw new Error("autofix made no repository changes");
            if (statSync(patch).size > 0) execFileSync("git", ["apply", "--check", patch], { cwd: this.cwd, stdio: "pipe" });
          }
        }
        if (taskReport.status !== "done") {
          const reason = `worker reported ${taskReport.status}`;
          if (!run.artifact_saved_at) checkpoint(run, "artifact_saved", this.cwd);
          execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "blocked", "--error", reason, "--result-json", JSON.stringify(pipelineFailureResult(reason))], this.cwd);
          checkpoint(run, "advanced", this.cwd);
          this.notifyBlockedAttempt(run, reason);
          return;
        }
      }
      if (run.stage === "review") {
        assertReviewBaseCurrent(run, this.cwd);
        const review = parseReviewReport(outputFor(run));
        const reviewNotes = review.findings.length ? `${review.notes}\n\n${review.findings.map((finding) => `- ${finding}`).join("\n")}` : review.notes;
        const result = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "completed", "--result-json", JSON.stringify({ subagent_state: status.state, review_status: review.status, notes: review.notes, findings: review.findings, candidate_run_id: run.candidate_run_id, candidate_patch_hash: run.candidate_patch_hash })], this.cwd);
        if (result.error) throw new Error(result.error);
        reviewCompleted = true;
        const update = execPic(["work-item", "review", run.task_id, review.status, "--notes", reviewNotes, "--pipeline-run-id", run.id], this.cwd);
        if (update.error) throw new Error(update.error);
        if (review.status === "passed") {
          const workerRun = this.integrateReviewedCandidate(run.task_id, run);
          this.promoteReviewedCandidate(workerRun);
        }
        checkpoint(run, "advanced", this.cwd);
        await this.advance(run.task_id);
        return;
      }
      if (run.stage === "scan") {
        const output = outputFor(run);
        const result = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "completed", "--result-json", JSON.stringify({ subagent_state: status.state, scan_report: output })], this.cwd);
        if (result.error) throw new Error(result.error);
        this.pi.sendUserMessage(`Scan evidence ready for contractor synthesis for ${run.task_id}. Inspect existing Scan drafts and validate every section against source. Resolve contradictions, author one canonical Scan Report, and save that contractor-authored report exactly once with save_work_item_artifact. If evidence is insufficient, call reject_work_item_scan with actor_role=contractor and explain why; the owner decides whether to dispatch targeted follow-up Scouts.\n\n${output}`, { deliverAs: "followUp" });
        checkpoint(run, "advanced", this.cwd);
        return;
      }
      const result = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "completed", "--result-json", JSON.stringify({ subagent_state: status.state })], this.cwd);
      if (result.error) throw new Error(result.error);
      const data = execPic(["show", run.task_id], this.cwd);
      const parentId = data.work_item?.parent_id;
      if (parentId) this.roots.add(parentId);
      if (isMutationStage(run.stage)) {
        await this.continueWorkerGroup(run);
        return;
      }
      checkpoint(run, "advanced", this.cwd);
      await this.advance(run.task_id, parentId);
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      if (reviewCompleted) {
        this.notifyBlockedAttempt(run, reason);
        return;
      }
      const persisted = isMutationStage(run.stage) ? this.pipelineRuns(run.task_id).find((entry) => entry.id === run.id) : undefined;
      if (persisted?.status === "completed" && persisted.artifact_saved_at) {
        this.notifyBlockedAttempt(persisted, reason);
        return;
      }
      execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "blocked", "--error", reason, "--result-json", JSON.stringify(pipelineFailureResult(reason))], this.cwd);
      if (isMutationStage(run.stage)) await this.continueWorkerGroup(run);
      else this.notifyBlockedAttempt(run, reason);
    }
  }

  private async continueWorkerGroup(run: PipelineRun): Promise<void> {
    const task = execPic(["show", run.task_id], this.cwd);
    const parentId = task.work_item?.parent_id;
    const parent = parentId ? execPic(["show", parentId], this.cwd) : null;
    const taskIds = parentId ? (parent?.children || []).map((child: any) => child.id) : [run.task_id];
    const taskRuns = new Map<string, PipelineRun[]>();
    const group = taskIds.flatMap((taskId: string) => {
      const taskData = normalizePipelineData(execPic(["show", taskId], this.cwd));
      if (taskData?.work_item?.status === "done") return [];
      const runs = execPic(["workflow", "pipeline-runs", taskId], this.cwd);
      if (!Array.isArray(runs)) return [];
      taskRuns.set(taskId, runs);
      const latest = workerIntegrationCandidate(runs) || runs.find((entry: PipelineRun) => isMutationStage(entry.stage) && !entry.advanced_at);
      return latest ? [latest] : [];
    });
    if (group.some((entry: PipelineRun) => entry.status === "claimed" || entry.status === "running")) return;

    if (group.some((entry: PipelineRun) => entry.status !== "completed")) {
      for (const entry of group.filter((entry: PipelineRun) => entry.status !== "completed")) {
        checkpoint(entry, "advanced", this.cwd);
        this.notifyBlockedAttempt(entry, entry.error || `worker pipeline ended with status ${entry.status || "unknown"}`);
      }
      return;
    }

    for (const entry of group) {
      const report = parseTaskCompletionReport(outputFor(entry));
      const data = normalizePipelineData(execPic(["show", entry.task_id], this.cwd));
      const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
      const constraints = JSON.parse(activePack?.constraints_json || "{}");
      const workspace = JSON.parse(readFileSync(join(entry.async_dir || "", "workspace.json"), "utf8"));
      const actualChangedFiles = filterGeneratedFiles(workspace.changedFiles || [], constraints).changedFiles;
      validateWorkerOutput(report.status, actualChangedFiles, constraints);
      const patch = workerPatch(entry);
      if (!entry.artifact_saved_at) {
        if (!existsSync(patch)) throw new Error(`worker patch missing: ${patch}`);
        checkpoint(entry, "artifact_saved", this.cwd, patch);
      }

    }

    for (const entry of group) {
      for (const sibling of taskRuns.get(entry.task_id) || []) {
        if (isMutationStage(sibling.stage) && sibling.id !== entry.id && !sibling.advanced_at) checkpoint(sibling, "advanced", this.cwd);
      }
    }

    for (const entry of group) await this.launchGroup("review", [entry.task_id]);
    for (const entry of group) checkpoint(entry, "advanced", this.cwd);
  }

  private integrateReviewedCandidate(taskId: string, reviewRun: PipelineRun): PipelineRun {
    const data = normalizePipelineData(execPic(["show", taskId], this.cwd));
    assertRunContractCurrent(data, reviewRun);
    const workerRun = this.pipelineRuns(taskId).find((candidate: PipelineRun) => candidate.id === reviewRun.candidate_run_id && isMutationStage(candidate.stage));
    if (!workerRun?.artifact_saved_at || !workerRun.integrated_patch_path || !workerRun.integrated_patch_hash) throw new Error("review passed without validated candidate patch evidence");
    if (reviewRun.candidate_patch_hash !== workerRun.integrated_patch_hash) throw new Error("review passed for a different candidate patch");
    if (!workerRun.integrated_at) {
      withGitWriteLock(this.cwd, () => {
        const patch = workerRun.integrated_patch_path;
        if (!existsSync(patch)) throw new Error(`candidate patch missing: ${patch}`);
        const actualHash = createHash("sha256").update(readFileSync(patch)).digest("hex");
        if (actualHash !== workerRun.integrated_patch_hash) throw new Error("candidate patch changed after review");
        const commitMessage = `task-system: integrate reviewed worker ${workerRun.subagent_run_id || workerRun.id}`;
        finalizeReviewedIntegration({
          patch,
          cwd: this.cwd,
          commitMessage,
          integrated: false,
          checkpoint: () => checkpoint(workerRun, "integrated", this.cwd),
        });
      });
    }
    return workerRun;
  }

  private promoteReviewedCandidate(run: PipelineRun): void {
    const raw = execPic(["show", run.task_id], this.cwd);
    const data = normalizePipelineData(raw);
    if ((data.completion_reports || []).some((report: any) => report.status === "done" && report.pipeline_run_id === run.id)) return;
    const report = parseTaskCompletionReport(outputFor(run));
    const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
    const constraints = JSON.parse(activePack?.constraints_json || "{}");
    const workspace = JSON.parse(readFileSync(join(run.async_dir || "", "workspace.json"), "utf8"));
    const changedFiles = filterGeneratedFiles(workspace.changedFiles || [], constraints).changedFiles;
    if (raw.work_item) {
      const saved = execPic(["work-item", "completion-save", run.task_id, "done", "--pipeline-run-id", run.id, "--summary", "Reviewed implementation completed", "--report-markdown", report.markdown], this.cwd);
      if (saved.error) throw new Error(saved.error);
    } else {
      saveWorkerReport(run, this.cwd, report, { changedFiles, diffSummary: "Reviewed implementation completed" });
    }
  }

  private async advance(taskId: string, parentId?: string): Promise<void> {
    const raw = execPic(["show", taskId], this.cwd);
    const data = raw.work_item ? normalizePipelineData(raw) : withInheritedParentWorkflowArtifacts(raw, this.cwd);
    const next = nextPipelineStage(data, this.pipelineRuns(taskId));
    if (next) {
      if (isMutationStage(next)) assertCleanGit(this.cwd);
      await this.launchGroup(next, [taskId]);
      return;
    }
    const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
    if (!activePack) return;
    const verificationBlock = pipelineVerificationBlockReason(data);
    if (verificationBlock) throw new Error(verificationBlock);
    const doneReports = activePackDoneReports(data, activePack);
    if (doneReports.length) {
      if (!latestVerificationAfter(data, doneReports[0])) this.pi.sendUserMessage(buildTaskVerifyPrompt(data), { deliverAs: "followUp" });
      return;
    }
    const done = execPic(["work-item", "status", taskId, "done"], this.cwd);
    if (done.error) throw new Error(done.error);

    if (!parentId) return;
    if (this.parentHasActiveRuns(parentId)) return;
    await this.scheduleReady(parentId);
  }

  private async resumePending(run: PipelineRun): Promise<void> {
    if (run.advanced_at) return;
    if (isMutationStage(run.stage)) {
      await this.continueWorkerGroup(run);
      return;
    }
    const data = execPic(["show", run.task_id], this.cwd);
    if (run.status !== "completed") {
      checkpoint(run, "advanced", this.cwd);
      return;
    }
    if (run.stage === "review") {
      const outcome = persistedReviewOutcome(run);
      if (!outcome) throw new Error("completed review is missing its durable verdict");
      const candidate = this.pipelineRuns(run.task_id).find((entry: PipelineRun) => entry.id === outcome.candidateRunId && isMutationStage(entry.stage));
      if (!candidate || candidate.status !== "completed" || !candidate.artifact_saved_at || !candidate.integrated_patch_path || candidate.integrated_patch_hash !== outcome.candidatePatchHash) {
        throw new Error("completed review references invalid candidate lineage");
      }
      const reviewData = execPic(["show", run.task_id], this.cwd);
      if (reviewData.work_item?.review_status !== outcome.status) {
        const notes = outcome.findings.length ? `${outcome.notes}\n\n${outcome.findings.map((finding) => `- ${finding}`).join("\n")}` : outcome.notes;
        const update = execPic(["work-item", "review", run.task_id, outcome.status, "--notes", notes, "--pipeline-run-id", run.id], this.cwd);
        if (update.error) throw new Error(update.error);
      }
      if (outcome.status === "passed") {
        const workerRun = this.integrateReviewedCandidate(run.task_id, run);
        this.promoteReviewedCandidate(workerRun);
      }
    }
    const parentId = data.work_item?.parent_id;
    checkpoint(run, "advanced", this.cwd);
    await this.advance(run.task_id, parentId);
  }

  private pipelineRuns(taskId: string): any[] {
    const runs = execPic(["workflow", "pipeline-runs", taskId], this.cwd);
    return parsePipelineRuns(runs);
  }

  private parentHasActiveRuns(parentId: string): boolean {
    const parent = execPic(["show", parentId], this.cwd);
    const childIds = (parent.children || []).map((child: any) => child.id);
    const ids = new Set([parentId, ...childIds]);
    const active = execPic(["workflow", "pipeline-active"], this.cwd);
    return Array.isArray(active) && active.some((run: PipelineRun) => ids.has(run.task_id));
  }
}

export function registerPipelineScheduler(pi: ExtensionAPI): PipelineScheduler {
  const scheduler = new PipelineScheduler(pi);
  if (process.env.PI_TASK_PARENT_RUN_ID) return scheduler;
  pi.on("session_start", (_event, ctx) => scheduler.startSession(ctx));
  pi.on("session_shutdown", () => scheduler.stopSession());
  pi.registerCommand("task-pipeline", {
    description: "Start, inspect, or stop an asynchronous task DAG pipeline",
    async handler(args, ctx) {
      const [action = "status", taskId] = args.trim().split(/\s+/);
      if (!taskId || !["status", "stop"].includes(action)) {
        ctx.ui.notify("Usage: /task-pipeline status|stop <task-id>", "warning");
        return;
      }
      try {
        const result = action === "stop"
          ? await scheduler.stop(taskId, ctx)
          : scheduler.status(taskId, ctx);
        ctx.ui.notify(JSON.stringify(result), "info");
      } catch (error) {
        ctx.ui.notify(error instanceof Error ? error.message : String(error), "error");
      }
    },
  });
  pi.registerTool({
    name: "task_pipeline",
    label: "Task Pipeline",
    description: "Inspect or stop durable asynchronous Work Item pipelines. Start work through task_manager work_on_work_item or /task work.",
    parameters: Type.Object({ action: Type.Union([Type.Literal("status"), Type.Literal("stop")]), task_id: Type.String() }),
    async execute(_id, params, _signal, _update, ctx) {
      try {
        const result = params.action === "stop"
          ? await scheduler.stop(params.task_id, ctx)
          : scheduler.status(params.task_id, ctx);
        const text = params.action === "stop" ? formatPipelineStop(result) : formatPipelineStatus(result);
        return { content: [{ type: "text", text }], details: result };
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        return { content: [{ type: "text", text: `Error: ${message}` }], details: { error: message }, isError: true };
      }
    },
  });
  return scheduler;
}
