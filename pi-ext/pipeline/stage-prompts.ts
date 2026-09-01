import { randomUUID } from "node:crypto";
import { join } from "node:path";
import { execPic, execPicText } from "../core/cli-helpers.ts";
import { withInheritedParentWorkflowArtifacts } from "../tasking/task-artifacts.ts";
import { buildWorkItemContinuePrompt, buildWorkItemReviewerHandoff, buildWorkItemScanPrompt } from "../tasking/work-item-prompts.ts";
import { finalAssistantText, startSubagent, type SubagentHandle } from "../subagent/runner.ts";
import type { SubagentResult } from "../subagent/types.ts";
import { parsePipelineRuns, type PipelineStage } from "./pipeline-types.ts";
import { renderCanonicalInstructionPackXml } from "./instruction-pack-xml.ts";
import { listSkillFamilies, type SkillFamilyCatalogEntry } from "../subagent/skills.ts";
import { buildAutofixContext, buildEscalationResolutionContext, buildOwnerRejectionContext, buildTargetedReReviewInstructions, buildWorkerCorrectionContext, reviewCycleCount } from "./corrections.ts";
import { currentFailedReview, isMutationStage } from "./report-parsing.ts";
import { buildStagePrimer, buildWorkProgressLedger, type StagePrimerDigest } from "../tasking/work-item-prompts.ts";
import { normalizePipelineData, resolvePlanProfile } from "./stage-resolution.ts";
import { parsePicShow, type PicShowDocument, type PicArtifact, type PicCheckpoint, type PicCompletionReport, type PicInstructionPack, type PicVerificationReport } from "./pic-show.ts";

// Known planning stages the scheduler can route and map to a bounded agent.
// This is a stage/agent registry only; dispatch eligibility is decided by the
// persisted Plan profile, never by this list (see resolvePlanProfile).
export const planningStages: PipelineStage[] = ["rri", "vision", "blueprint", "contracts", "task_graph"];

export function isPlanningStage(stage: PipelineStage): boolean { return planningStages.includes(stage); }

// Planning-stage deadline: planners load up to five approved artifacts and author
// large validated JSON artifacts in one session; the 30-minute managed-worker
// default killed task_graph planners mid-work (three consecutive 30m04s terminations
// on wi-155af9fc/wi-e486a1dd), so planning stages get a doubled deadline.
export const PLANNING_DEADLINE_MS = 60 * 60 * 1000;


export function reviewStagePrompt(taskId: string, cwd: string): string {
  const handoff = buildWorkItemReviewerHandoff(taskId);
  const runs = parsePipelineRuns(execPic(["workflow", "pipeline-runs", taskId], cwd));
  return reviewCycleCount(runs) >= 1 ? handoff + buildTargetedReReviewInstructions() : handoff;
}

// Worker session lineage (GAP-137): key on the instruction pack ID so review-fix
// relaunches resume the same conversation while a retired TIP (execution reset or
// repair mints a new pack) can never inherit the old session.
export function workerSessionPath(cwd: string, packKey: string): string {
  return join(cwd, ".pi", "runtime", "runs", packKey, "session.jsonl");
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function pipelineSpawnParams(stage: PipelineStage, task: any, cwd: string): any {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const spec: any = { agent: task.agent, task: task.task, cwd, stage, taskId: task.taskId, acceptance: stage === "review" ? "attested" : "checked", ...(task.skillFamilies ? { skillFamilies: task.skillFamilies } : {}) };
  if (isPlanningStage(stage)) spec.deadlineMs = PLANNING_DEADLINE_MS;
  if (isMutationStage(stage) || stage === "review") spec.isolation = "worktree";
  return spec;
}

export const FULL_SCAN_SECTIONS = [
  ["Architecture", "Map stack, modules, boundaries, entry points, and data flow. Cite files and lines; do not estimate unrelated metrics."],
  ["Lifecycle", "Trace planning, materialization, authorization, execution, review, verification, acceptance, merge, cancellation, and reset state transitions."],
  ["Authority", "Audit actor-role checks, child-agent capabilities, persistence boundaries, immutability, and security risks. Distinguish implemented guards from gaps."],
  ["Verification", "Inspect manifests, test/build/typecheck commands, test layout, runtime prerequisites, and current blockers. Separate observed runs from historical evidence."],
  ["Reliability", "Inspect the gap ledger, open invariants, operational risks, migrations, generated artifacts, and documentation drift. Report exact statuses only."],
] as const;

export const SCOUT_EVIDENCE_REQUIRED_ELEMENTS = ["scope", "findings", "gaps", "verification", "risks"] as const;

export function validateScoutEvidenceXml(output: string, section: string): void {
  const normalized = normalizeScoutEvidenceXml(output);
  const root = normalized.match(/^<scout_evidence\b([^>]*)>([\s\S]*)<\/scout_evidence>$/);
  const attributes = root?.[1].match(/([a-zA-Z_][\w.-]*)="([^"]*)"/g)?.reduce<Record<string, string>>((values, attribute) => {
    const match = attribute.match(/^([a-zA-Z_][\w.-]*)="([^"]*)"$/);
    if (match) values[match[1]] = match[2];
    return values;
  }, {}) || {};
  if (!root || attributes.section !== section.toLowerCase() || !["high", "medium", "low"].includes(attributes.confidence)) throw new Error(`Scout ${section} output must be one <scout_evidence section="${section.toLowerCase()}" confidence="high|medium|low"> document`);
  for (const element of SCOUT_EVIDENCE_REQUIRED_ELEMENTS) {
    if (!root[2].includes(`<${element}>`) || !root[2].includes(`</${element}>`)) throw new Error(`Scout ${section} evidence missing <${element}>`);
  }
  if (!/<source\s+path="[^"]+"(?:\s+line="[^"]+")?\s*>[\s\S]*<\/source>/.test(root[2])) throw new Error(`Scout ${section} evidence requires at least one source citation`);
}

export function normalizeScoutEvidenceXml(output: string): string {
  const trimmed = output.trim().replace(/^```(?:xml)?\s*([\s\S]*?)\s*```$/, "$1").trim();
  const start = trimmed.indexOf("<scout_evidence");
  const end = trimmed.lastIndexOf("</scout_evidence>");
  if (start >= 0 && end > start) return trimmed.slice(start, end + "</scout_evidence>".length).trim();
  return trimmed;
}

export const SCAN_FANOUT_RETRY_LIMIT = 1;

// Bounded repair constraint: a Scout section whose output fails validation or
// whose process failed is retried exactly once with the concrete validation
// error carried into the retry task; a section that fails again escalates as a
// failed fanout result instead of looping.
export function planScanRetryWave(results: Array<Partial<SubagentResult>>, outputs: string[]): Array<{ index: number; error: string }> {
  const retry: Array<{ index: number; error: string }> = [];
  results.forEach((entry, index) => {
    if (entry.exitCode !== 0) {
      retry.push({ index, error: entry.errorMessage || entry.stderr || "scout process failed" });
      return;
    }
    try {
      validateScoutEvidenceXml(outputs[index] ?? "", FULL_SCAN_SECTIONS[index]![0]);
    } catch (error) {
      retry.push({ index, error: error instanceof Error ? error.message : String(error) });
    }
  });
  return retry;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
function scoutSectionTask(spec: any, section: string, assignment: string, lastError: string): string {
  return `${spec.task}\n\n<section_assignment name="${section.toLowerCase()}">${assignment}</section_assignment>\nThe root must be <scout_evidence section="${section.toLowerCase()}" confidence="high|medium|low">. Return exactly that one XML document. Use exactly one concise finding, at most one gap, and one evidence container with one or two non-empty <source path="relative/file"> citations. Keep the complete document under 2,500 characters, including </scout_evidence>. Do not use Markdown or compose the canonical Scan Report.${lastError ? ` Previous output was invalid: ${lastError}. Correct exactly that defect on this retry.` : ""}`;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function startFullScanFanout(spec: any, agent: any): SubagentHandle {
  const id = randomUUID();
  const startSection = (section: string, assignment: string, lastError: string) => startSubagent({
    ...spec,
    runId: undefined,
    agent,
    task: scoutSectionTask(spec, section, assignment, lastError),
  });
  let handles: Array<{ handle: SubagentHandle }> = [];
  const result = (async (): Promise<SubagentResult> => {
    handles = FULL_SCAN_SECTIONS.map(([section, assignment]) => ({ handle: startSection(section, assignment, "") }));
    const results = await Promise.all(handles.map((entry) => entry.handle.result));
    const outputs = results.map((entry) => finalAssistantText(entry.messages) || entry.errorMessage || entry.stderr || "");
    for (let attempt = 0; attempt < SCAN_FANOUT_RETRY_LIMIT; attempt++) {
      const wave = planScanRetryWave(results, outputs);
      if (!wave.length) break;
      const retried = await Promise.all(wave.map((entry) => startSection(FULL_SCAN_SECTIONS[entry.index]![0], FULL_SCAN_SECTIONS[entry.index]![1], entry.error).result));
      for (const [offset, entry] of wave.entries()) {
        results[entry.index] = retried[offset]!;
        outputs[entry.index] = finalAssistantText(retried[offset]!.messages) || retried[offset]!.errorMessage || retried[offset]!.stderr || "";
      }
    }
    const failed = results.filter((entry) => entry.exitCode !== 0);
    try {
      outputs.forEach((output, index) => validateScoutEvidenceXml(output, FULL_SCAN_SECTIONS[index]![0]));
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    } catch (error: any) {
      const message = error?.message || String(error);
      return {
        runId: id,
        agent: "task-scout-group",
        task: spec.task,
        exitCode: 1,
        messages: [{ role: "assistant", content: [{ type: "text", text: `Scout fanout failed: ${message}` }] }],
        stderr: message,
        usage: results.reduce((total, entry) => ({ input: total.input + entry.usage.input, output: total.output + entry.usage.output, cacheRead: total.cacheRead + entry.usage.cacheRead, cacheWrite: total.cacheWrite + entry.usage.cacheWrite, cost: total.cost + entry.usage.cost, contextTokens: Math.max(total.contextTokens, entry.usage.contextTokens), turns: total.turns + entry.usage.turns }), { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, cost: 0, contextTokens: 0, turns: 0 }),
      };
    }
    const evidence = outputs.map((output, index) => normalizeScoutEvidenceXml(output).replace("<scout_evidence ", `<scout_evidence run_id="${results[index]!.runId}" `)).join("\n");
    return {
      runId: id,
      agent: "task-scout-group",
      task: spec.task,
      exitCode: failed.length ? 1 : 0,
      messages: [{ role: "assistant", content: [{ type: "text", text: `<scan_evidence work_item="${spec.taskId || "unknown"}" scan_level="full">\n${evidence}\n</scan_evidence>\n\nContractor: validate each <scout_evidence> section, resolve contradictions against source, and author one canonical Scan Report. Do not persist any individual Scout output as the Scan artifact.` }] }],
      stderr: failed.map((entry) => entry.errorMessage || entry.stderr).filter(Boolean).join("\n"),
      usage: results.reduce((total, entry) => ({ input: total.input + entry.usage.input, output: total.output + entry.usage.output, cacheRead: total.cacheRead + entry.usage.cacheRead, cacheWrite: total.cacheWrite + entry.usage.cacheWrite, cost: total.cost + entry.usage.cost, contextTokens: Math.max(total.contextTokens, entry.usage.contextTokens), turns: total.turns + entry.usage.turns }), { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, cost: 0, contextTokens: 0, turns: 0 }),
    };
  })();
  return { id, result, stop: () => handles.forEach((entry) => entry.handle.stop()) };
}

export function stageAgent(stage: PipelineStage): string {
  if (stage === "contracts") throw new Error("Contract drafting is Contractor-owned");
  if (stage === "rri") throw new Error("RRI is Contractor-owned");
  return ({ scan: "task-scout", vision: "task-planner", blueprint: "task-planner", task_graph: "task-planner", worker: "task-worker", review: "task-reviewer", autofix: "task-worker" } as const)[stage];
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function planningHandoff(stage: "blueprint" | "task_graph", raw: any, taskId: string, skillFamilyCatalog: SkillFamilyCatalogEntry[] = []): string {
  const requiredStages = stage === "blueprint" ? ["scan", "rri", "vision"] : ["scan", "rri", "vision", "blueprint", "contracts"];
  const checkpoints = (Array.isArray(raw?.checkpoints) ? raw.checkpoints : [])
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    .filter((checkpoint: any) => requiredStages.includes(checkpoint.stage))
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    .reduce((latest: Map<string, any>, checkpoint: any) => {
      const current = latest.get(checkpoint.stage);
      if (!current || Number(checkpoint.artifact_revision || 0) > Number(current.artifact_revision || 0)) latest.set(checkpoint.stage, checkpoint);
      return latest;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    }, new Map<string, any>());
  const payload = {
    work_item: { id: taskId, title: raw?.work_item?.title || "", type: raw?.work_item?.type || "", description: String(raw?.work_item?.description || "").slice(0, 4000) },
    project: { name: raw?.project?.name || "", root_path: raw?.project?.root_path || "." },
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    approved_context: [...checkpoints.values()].map((checkpoint: any) => ({ stage: checkpoint.stage, artifact_id: checkpoint.artifact_id, artifact_revision: checkpoint.artifact_revision, content_hash: checkpoint.content_hash })),
    instructions: "Load each approved context artifact with task_manager action load_planning_artifact before planning. Do not use historical revisions.",
  };
  const encoded = JSON.stringify(payload).replaceAll("]]>", "]] ]>");
  // The catalog travels beside approved_context (not inside it) so the handoff
  // schema stays unchanged: the planner cannot select skill families it cannot
  // see, and additions here are prompt-only, never parsed back.
  const catalogSection = skillFamilyCatalog.length
    ? `<skill_family_catalog><![CDATA[${JSON.stringify(skillFamilyCatalog.map(({ id, description }) => ({ id, description }))).replaceAll("]]>", "]] ]>")}]]></skill_family_catalog>`
    : "";
  return `<${stage}_handoff schema_version="2" work_item_id="${taskId}"><approved_context><![CDATA[${encoded}]]></approved_context>${catalogSection}</${stage}_handoff>`;
}

// Planning profile constraint: a handoff must name the approved checkpoint of
// the immediately precedent stage in the Plan profile so the consumer can tie
// the dispatched stage to its persisted predecessor. The predecessor uses the
// same validity predicate as planPrimerContext, so an orphaned, hash-stale, or
// rejected checkpoint can never be presented as the approved predecessor.
export function predecessorCheckpointFor(doc: CheckpointSource, stage: string, profileStages: string[]): PicCheckpoint | undefined {
  const index = profileStages.indexOf(stage);
  if (index <= 0) return undefined;
  return latestValidatedCheckpoint(doc, profileStages[index - 1])?.checkpoint;
}

export function stagePrompt(stage: PipelineStage, taskId: string, cwd: string): string {
  const raw = execPic(["show", taskId], cwd);
  if (raw.work_item) {
    // Canonical branch: fail-closed typed view of the show document. The legacy
    // artifact-inheritance fallback below keeps the untyped raw document by design.
    const doc = parsePicShow(raw);
    if (stage === "scan") return buildWorkItemScanPrompt(doc.work_item, doc.project);
    if (isPlanningStage(stage)) {
      const profile = resolvePlanProfile(doc);
      const checkpoint = predecessorCheckpointFor(doc, stage, profile.stages);
      const primerContext = planPrimerContext(doc, profile.stages, stage);
      if (primerContext.missing.length) {
        throw new Error(`Work Item ${taskId} planning stage ${stage} is missing approved context for: ${primerContext.missing.join(", ")}. Re-save and re-approve the listed stages before dispatching this one.`);
      }
      const primer = buildStagePrimer({
        work_item_id: taskId,
        stage,
        profile,
        predecessor_checkpoint: checkpoint ? { stage: String(checkpoint.stage), artifact_id: String(checkpoint.artifact_id || ""), artifact_revision: Number(checkpoint.artifact_revision || 1), content_hash: String(checkpoint.content_hash || "") } : undefined,
        approved_digests: primerContext.digests,
      });
      const handoff = stage === "blueprint" || stage === "task_graph" ? planningHandoff(stage, doc, taskId, listSkillFamilies({ cwd })) + "\n" : "";
      return `${primer}\n${handoff}${buildWorkItemContinuePrompt({ work_item_id: taskId, next_stage: stage }, doc.work_item)}`;
    }
    const data = normalizePipelineData(doc);
    const activePack = data.instruction_packs.find((pack: PicInstructionPack) => pack.status === "active");
    if (!activePack) throw new Error(`Work Item ${taskId} requires one active instruction pack`);
    if (stage === "review") return reviewStagePrompt(taskId, cwd);
    if (stage === "autofix") return renderCanonicalInstructionPackXml(data.work_item, activePack) + buildAutofixContext(data);
    const runs = parsePipelineRuns(execPic(["workflow", "pipeline-runs", taskId], cwd));
    const currentReview = currentFailedReview(runs, activePack);
    let verificationCommand = "contractor verification";
    try {
      const gate = ((JSON.parse(activePack.content_json || "{}") as { verification?: Array<{ command?: string }> }).verification || [])[0];
      if (gate?.command) verificationCommand = gate.command;
    } catch {}
    // Attempt numbering comes from the persisted attempt counter on the pack's
    // pipeline runs, not an inferred count, so circuit resets, review-fix
    // epochs, and migrated lineage cannot desynchronize it.
    const packAttempts = runs.filter((run) => run.stage === "worker" && run.instruction_pack_id === activePack.id).map((run) => Number(run.attempt) || 0);
    const ledger = buildWorkProgressLedger({
      activePackId: activePack.id || taskId,
      activePackVersion: activePack.version || 1,
      attempt: (packAttempts.length ? Math.max(...packAttempts) : 0) + 1,
      priorReports: data.completion_reports.filter((report: PicCompletionReport) => !activePack.id || report.instruction_pack_id === activePack.id).slice(0, 5),
      failedVerifications: data.verification_reports.filter((report: PicVerificationReport) => report.status === "failed" || report.status === "partial").slice(0, 3).map((report: PicVerificationReport) => ({ command: verificationCommand, evidence: report.summary || "" })),
      escalationContext: buildEscalationResolutionContext(data, runs),
    });
    return renderCanonicalInstructionPackXml(data.work_item, activePack) + ledger + buildWorkerCorrectionContext({ ...data, current_review: currentReview }) + buildOwnerRejectionContext(data);
  }
  const data = withInheritedParentWorkflowArtifacts(raw, cwd);
  if (stage === "scan") return buildWorkItemScanPrompt(data.work_item, data.project);
  if (stage === "review") return reviewStagePrompt(taskId, cwd);
  if (stage === "autofix") {
    const verificationReports = execPic(["workflow", "verifications", taskId], cwd);
    return execPicText(["workflow", "instruction-pack-render", taskId], cwd) + buildAutofixContext({ ...data, verification_reports: Array.isArray(verificationReports) ? verificationReports : data.verification_reports });
  }
  return execPicText(["workflow", "instruction-pack-render", taskId], cwd) + buildWorkerCorrectionContext(data) + buildEscalationResolutionContext(data, parsePipelineRuns(execPic(["workflow", "pipeline-runs", taskId], cwd)));
}

// Checkpoint validity constraint: only approved/accepted checkpoints whose
// bound artifact revision exists and whose content hash matches that revision
// may supply planning context or predecessor lineage. Rejected, hash-stale, or
// artifact-orphaned checkpoints count as missing/absent, so dispatch fails
// closed instead of planning from tainted history. Tests hand-construct partial
// documents, so only the two collections that feed validity are required.
type CheckpointSource = { checkpoints?: PicCheckpoint[]; artifacts?: PicArtifact[] };

function latestValidatedCheckpoint(doc: CheckpointSource, stage: string): { checkpoint: PicCheckpoint; artifact: PicArtifact } | undefined {
  const checkpoint = (doc.checkpoints || [])
    .filter((entry) => entry.stage === stage && (entry.decision_type === "approved" || entry.decision_type === "accepted"))
    .sort((a, b) => Number(b.artifact_revision || 0) - Number(a.artifact_revision || 0)
      || String(b.created_at || "").localeCompare(String(a.created_at || "")))[0];
  if (!checkpoint) return undefined;
  const artifact = (doc.artifacts || []).find((entry) => entry.id === checkpoint.artifact_id && String(entry.revision) === String(checkpoint.artifact_revision));
  if (!artifact) return undefined;
  if (String(checkpoint.content_hash || "") !== String(artifact.content_hash || "")) return undefined;
  return { checkpoint, artifact };
}

export function planPrimerContext(doc: PicShowDocument, profileStages: string[], stage: string): { digests: StagePrimerDigest[]; missing: string[] } {
  const stageIndex = profileStages.indexOf(stage);
  const predecessors = stageIndex > 0 ? profileStages.slice(0, stageIndex) : [];
  const digests: StagePrimerDigest[] = [];
  const missing: string[] = [];
  for (const prior of predecessors) {
    const validated = latestValidatedCheckpoint(doc, prior);
    if (!validated) {
      missing.push(prior);
      continue;
    }
    digests.push({
      stage: String(validated.checkpoint.stage),
      artifact_id: String(validated.artifact.id),
      artifact_revision: Number(validated.artifact.revision || 1),
      content_hash: String(validated.artifact.content_hash || ""),
      content: String(validated.artifact.content || ""),
    });
  }
  return { digests, missing };
}
