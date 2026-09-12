import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { execPic, withGitWriteLock } from "../core/cli-helpers.ts";
import { loadLatestBlueprintDraft } from "../core/blueprint-drafts.ts";
import { buildPlanningHandoffXml } from "../tasking/work-item-prompts.ts";
import type { PipelineRun } from "./pipeline-types.ts";
import { isMutationStage, parseReviewReport, parseTaskCompletionReport, persistedReviewOutcome } from "./report-parsing.ts";
import { assertReviewBaseCurrent, finalizeReviewedIntegration } from "./integration.ts";
import { filterGeneratedFiles, pipelineFailureResult, validateWorkerOutput, validateWorkerPatchArtifact, workerPatch } from "./worker-validation.ts";
import { assertReviewFixChangedPatch } from "./corrections.ts";
import { isPlanningStage, predecessorCheckpointFor } from "./stage-prompts.ts";
import { assertRunContractCurrent, normalizePipelineData, resolvePlanProfile, workerIntegrationCandidate } from "./stage-resolution.ts";
import { checkpoint, outputFor, saveWorkerReport } from "./run-helpers.ts";
import { advance } from "./advance.ts";
import { launchGroup } from "./launch.ts";
import type { SchedulerDeps } from "./scheduler-context.ts";

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export async function finish(deps: SchedulerDeps, run: PipelineRun, status: any): Promise<void> {
  const { cwd } = deps;
  let reviewCompleted = false;
  try {
    const child = status.steps?.[run.child_index || 0] || {};
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    const resolvedModel = child.model || child.resolvedModel || child.modelAttempts?.findLast?.((attempt: any) => attempt.success)?.model || "";
    if (resolvedModel) execPic(["workflow", "pipeline-model", run.id, run.lease_token, resolvedModel], cwd);
    if (isMutationStage(run.stage)) {
      const output = outputFor(run);
      const taskReport = parseTaskCompletionReport(output);
      if (!run.artifact_saved_at) {
        // Provenance comes from the persisted claim; Workers need not echo hashes in prose.
        if (taskReport.status === "done") {
          const workspacePath = join(run.async_dir || "", "workspace.json");
          if (!existsSync(workspacePath)) throw new Error(`worker workspace diagnostics missing: ${workspacePath}`);
          const workspace = JSON.parse(readFileSync(workspacePath, "utf8"));
          const data = normalizePipelineData(deps.showItem(run.task_id));
          assertRunContractCurrent(data, run);
          // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
          const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
          const constraints = JSON.parse(activePack?.constraints_json || "{}");
          const actualChangedFiles = filterGeneratedFiles(workspace.changedFiles || [], constraints).changedFiles;
          const normalizedReport = { changedFiles: actualChangedFiles };
          validateWorkerOutput(taskReport.status, actualChangedFiles, constraints);
          const patch = workerPatch(run);
          const outputPath = join(run.async_dir || "", `output-${run.child_index || 0}.log`);
          validateWorkerPatchArtifact(patch, outputPath, normalizedReport);
          assertReviewFixChangedPatch(run, readFileSync(patch), taskReport.no_change_justification);
          if (run.stage === "autofix" && statSync(patch).size === 0) throw new Error("autofix made no repository changes");
          if (statSync(patch).size > 0) execFileSync("git", ["apply", "--check", patch], { cwd, stdio: "pipe" });
        }
      }
      if (taskReport.status === "escalated") {
        // Fail-closed escalation (GAP-138): persist the structured report bound to the
        // run's TIP lineage, block the run, release the claim, and stop — never retry
        // or continue downstream while the escalation is open.
        const saved = execPic(["workflow", "escalation-save", run.task_id, "--pipeline-run-id", run.id, "--report-json", JSON.stringify(taskReport.escalation)], cwd);
        if (saved.error) {
          // GAP-141: never lose the escalation intent to a tooling mismatch (e.g., a
          // stale installed pic predating escalation-save). Persist the run blocked
          // with the full structured payload and completion report so the owner sees
          // the actual question instead of only the subcommand error.
          const reason = `escalation persistence failed (${saved.error}); worker escalation payload preserved below`;
          const result = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "blocked", "--error", reason, "--result-json", JSON.stringify({ ...pipelineFailureResult(reason), blocker: taskReport.escalation?.summary || reason, completion_report: taskReport.markdown, escalation: taskReport.escalation })], cwd);
          if (result.error) throw new Error(result.error);
          checkpoint(run, "advanced", cwd);
          deps.notifyBlockedAttempt(run, `${reason}\n\n${taskReport.markdown}`);
          return;
        }
        checkpoint(run, "advanced", cwd);
        deps.notifyBlockedAttempt(run, `worker escalated ${taskReport.escalation.level}: ${taskReport.escalation.summary || "decision required before progress can resume"}`);
        return;
      }
      if (taskReport.status !== "done") {
        const reason = taskReport.blocker || `worker reported ${taskReport.status}`;
        execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "blocked", "--error", reason, "--result-json", JSON.stringify({ ...pipelineFailureResult(reason), blocker: reason, completion_report: taskReport.markdown, ...(taskReport.failure_metadata ? { failure_metadata: taskReport.failure_metadata } : {}) })], cwd);
        checkpoint(run, "advanced", cwd);
        deps.notifyBlockedAttempt(run, reason);
        return;
      }
    }
    if (run.stage === "review") {
      assertReviewBaseCurrent(run, cwd);
      const review = parseReviewReport(outputFor(run));
      const reviewNotes = review.findings.length ? `${review.notes}\n\n${review.findings.map((finding) => `- ${finding}`).join("\n")}` : review.notes;
      const result = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "completed", "--result-json", JSON.stringify({ subagent_state: status.state, review_status: review.status, notes: review.notes, findings: review.findings, owner_approval_required: review.ownerApprovalRequired, candidate_run_id: run.candidate_run_id, candidate_patch_hash: run.candidate_patch_hash })], cwd);
      if (result.error) throw new Error(result.error);
      reviewCompleted = true;
      // Integration-before-advance constraint (RLB-GAP-007): `work-item review`
      // advances next_stage to contractor_verification, so the candidate must
      // integrate FIRST — a failed integration then leaves the run completed
      // but not advanced, which pipeline-pending → resumePending converges on,
      // instead of a wedged verification stage with no delivered commit.
      if (review.status === "passed") {
        const workerRun = integrateReviewedCandidate(deps, run.task_id, run);
        promoteReviewedCandidate(deps, workerRun);
      }
      const update = execPic(["work-item", "review", run.task_id, review.status, "--notes", reviewNotes, "--pipeline-run-id", run.id], cwd);
      if (update.error) throw new Error(update.error);
      checkpoint(run, "advanced", cwd);
      await advance(deps, run.task_id);
      return;
    }
    if (isPlanningStage(run.stage)) {
      const result = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "completed", "--result-json", JSON.stringify({ subagent_state: status.state })], cwd);
      if (result.error) throw new Error(result.error);
      publishPlanningHandoff(deps, run, outputFor(run));
      checkpoint(run, "advanced", cwd);
      return;
    }
    const result = execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "completed", "--result-json", JSON.stringify({ subagent_state: status.state })], cwd);
    if (result.error) throw new Error(result.error);
    const data = deps.showItem(run.task_id);
    const parentId = data.work_item?.parent_id;
    if (parentId) deps.addRoot(parentId);
    if (isMutationStage(run.stage)) {
      await continueWorkerGroup(deps, run);
      return;
    }
    checkpoint(run, "advanced", cwd);
    await advance(deps, run.task_id, parentId);
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    if (reviewCompleted) {
      deps.notifyBlockedAttempt(run, reason);
      return;
    }
    const persisted = isMutationStage(run.stage) ? deps.pipelineRuns(run.task_id).find((entry) => entry.id === run.id) : undefined;
    if (persisted?.status === "completed" && persisted.artifact_saved_at) {
      deps.notifyBlockedAttempt(persisted, reason);
      return;
    }
    execPic(["workflow", "pipeline-complete", run.id, run.lease_token, "blocked", "--error", reason, "--result-json", JSON.stringify(pipelineFailureResult(reason))], cwd);
    if (isMutationStage(run.stage)) await continueWorkerGroup(deps, run);
    else deps.notifyBlockedAttempt(run, reason);
  }
}

export async function continueWorkerGroup(deps: SchedulerDeps, run: PipelineRun): Promise<void> {
  const { cwd } = deps;
  const task = deps.showItem(run.task_id);
  const parentId = task.work_item?.parent_id;
  const parent = parentId ? deps.showItem(parentId) : null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const taskIds = parentId ? (parent?.children || []).map((child: any) => child.id) : [run.task_id];
  const taskRuns = new Map<string, PipelineRun[]>();
  const group = taskIds.flatMap((taskId: string) => {
    const taskData = normalizePipelineData(deps.showItem(taskId));
    if (taskData?.work_item?.status === "done") return [];
    const runs = execPic(["workflow", "pipeline-runs", taskId], cwd);
    if (!Array.isArray(runs)) return [];
    taskRuns.set(taskId, runs);
    const latest = workerIntegrationCandidate(runs) || runs.find((entry: PipelineRun) => isMutationStage(entry.stage) && !entry.advanced_at);
    return latest ? [latest] : [];
  });
  if (group.some((entry: PipelineRun) => entry.status === "claimed" || entry.status === "running")) return;

  if (group.some((entry: PipelineRun) => entry.status !== "completed")) {
    for (const entry of group.filter((entry: PipelineRun) => entry.status !== "completed")) {
      checkpoint(entry, "advanced", cwd);
      deps.notifyBlockedAttempt(entry, entry.error || `worker pipeline ended with status ${entry.status || "unknown"}`);
    }
    return;
  }

  for (const entry of group) {
    const report = parseTaskCompletionReport(outputFor(entry));
    if (report.status === "escalated") throw new Error("escalated run cannot be integrated");
    const data = normalizePipelineData(deps.showItem(entry.task_id));
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
    const constraints = JSON.parse(activePack?.constraints_json || "{}");
    const workspace = JSON.parse(readFileSync(join(entry.async_dir || "", "workspace.json"), "utf8"));
    const actualChangedFiles = filterGeneratedFiles(workspace.changedFiles || [], constraints).changedFiles;
    validateWorkerOutput(report.status, actualChangedFiles, constraints);
    const patch = workerPatch(entry);
    if (!entry.artifact_saved_at) {
      if (!existsSync(patch)) throw new Error(`worker patch missing: ${patch}`);
      checkpoint(entry, "artifact_saved", cwd, patch);
    }

  }

  for (const entry of group) {
    for (const sibling of taskRuns.get(entry.task_id) || []) {
      if (isMutationStage(sibling.stage) && sibling.id !== entry.id && !sibling.advanced_at) checkpoint(sibling, "advanced", cwd);
    }
  }

  for (const entry of group) await launchGroup(deps, "review", [entry.task_id]);
  for (const entry of group) checkpoint(entry, "advanced", cwd);
}

export function integrateReviewedCandidate(deps: SchedulerDeps, taskId: string, reviewRun: PipelineRun): PipelineRun {
  const { cwd } = deps;
  const data = normalizePipelineData(deps.showItem(taskId));
  assertRunContractCurrent(data, reviewRun);
  const workerRun = deps.pipelineRuns(taskId).find((candidate: PipelineRun) => candidate.id === reviewRun.candidate_run_id && isMutationStage(candidate.stage));
  const patchPath = workerRun?.integrated_patch_path;
  const patchHash = workerRun?.integrated_patch_hash;
  if (!workerRun?.artifact_saved_at || !patchPath || !patchHash) throw new Error("review passed without validated candidate patch evidence");
  if (reviewRun.candidate_patch_hash !== patchHash) throw new Error("review passed for a different candidate patch");
  if (!workerRun.integrated_at) {
    withGitWriteLock(cwd, () => {
      if (!existsSync(patchPath)) throw new Error(`candidate patch missing: ${patchPath}`);
      const actualHash = createHash("sha256").update(readFileSync(patchPath)).digest("hex");
      if (actualHash !== patchHash) throw new Error("candidate patch changed after review");
      const commitMessage = `task-system: integrate reviewed worker ${workerRun.subagent_run_id || workerRun.id}`;
      finalizeReviewedIntegration({
        patch: patchPath,
        cwd,
        commitMessage,
        integrated: false,
        checkpoint: () => checkpoint(workerRun, "integrated", cwd),
      });
    });
  }
  return workerRun;
}

export function promoteReviewedCandidate(deps: SchedulerDeps, run: PipelineRun): void {
  const { cwd } = deps;
  const raw = deps.showItem(run.task_id);
  const data = normalizePipelineData(raw);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  if ((data.completion_reports || []).some((report: any) => report.status === "done" && report.pipeline_run_id === run.id)) return;
  const report = parseTaskCompletionReport(outputFor(run));
  if (report.status === "escalated") throw new Error("escalated run cannot be integrated");
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
  const constraints = JSON.parse(activePack?.constraints_json || "{}");
  const workspace = JSON.parse(readFileSync(join(run.async_dir || "", "workspace.json"), "utf8"));
  const changedFiles = filterGeneratedFiles(workspace.changedFiles || [], constraints).changedFiles;
  if (raw.work_item) {
    const saved = execPic(["work-item", "completion-save", run.task_id, "done", "--pipeline-run-id", run.id, "--summary", "Reviewed implementation completed", "--report-markdown", report.markdown], cwd);
    if (saved.error) throw new Error(saved.error);
  } else {
    // The escalated guard above makes this narrowing safe: escalated runs never integrate.
    const integrationStatus = report.status as "done" | "partial" | "blocked";
    saveWorkerReport(run, cwd, { status: integrationStatus, markdown: report.markdown }, { changedFiles, diffSummary: "Reviewed implementation completed" });
  }
}

export function publishPlanningHandoff(deps: SchedulerDeps, run: PipelineRun, output: string): void {
  // Blueprint draft constraint: planner output is not canonical until the
  // Contractor checkpoint and owner promotion complete.
  const payload = run.stage === "blueprint"
    ? JSON.stringify(loadLatestBlueprintDraft(deps.cwd, run.task_id))
    : output;
  const data = deps.showItem(run.task_id);
  const profile = resolvePlanProfile(data);
  const predecessor = predecessorCheckpointFor(data, run.stage, profile.stages);
  const envelope = buildPlanningHandoffXml({
    work_item_id: run.task_id,
    stage: run.stage,
    predecessor_checkpoint: predecessor?.artifact_id ? String(predecessor.artifact_id) : "",
    profile_version: String(profile.version),
    profile_hash: profile.contentHash,
  }, payload);
  const handoffId = deps.handoffs.put(run.stage, run.task_id, envelope);
  const action = run.stage === "rri"
    ? "conduct the owner interview, persist confirmed requirements and decisions, then save the owner-confirmed RRI artifact"
    : run.stage === "blueprint"
      ? "load the temporary draft with load_blueprint_draft, validate its JSON content, and use its draft_id for review_blueprint_checkpoint; revise through save_blueprint_draft if needed, then present the checked draft for owner approval; do not call save_work_item_artifact"
      : `validate the result, save the ${run.stage} artifact, and present it for owner approval`;
  deps.sendUserMessage(`${run.stage.toUpperCase()} analysis ready for ${run.task_id}. Load ephemeral handoff ${handoffId}, ${action}. The handoff expires five minutes after first load and is never persisted.`);
}

export async function resumePending(deps: SchedulerDeps, run: PipelineRun): Promise<void> {
  if (run.advanced_at) return;
  if (isMutationStage(run.stage)) {
    await continueWorkerGroup(deps, run);
    return;
  }
  const data = deps.showItem(run.task_id);
  if (run.status !== "completed") {
    checkpoint(run, "advanced", deps.cwd);
    return;
  }
  if (run.stage === "review") {
    const outcome = persistedReviewOutcome(run);
    if (!outcome) throw new Error("completed review is missing its durable verdict");
    const candidate = deps.pipelineRuns(run.task_id).find((entry: PipelineRun) => entry.id === outcome.candidateRunId && isMutationStage(entry.stage));
    if (!candidate || candidate.status !== "completed" || !candidate.artifact_saved_at || !candidate.integrated_patch_path || candidate.integrated_patch_hash !== outcome.candidatePatchHash) {
      throw new Error("completed review references invalid candidate lineage");
    }
    const reviewData = deps.showItem(run.task_id);
    // Same integration-before-advance ordering as finish()'s review path (RLB-GAP-007):
    // integrate the passed candidate before recording the review verdict.
    if (outcome.status === "passed") {
      const workerRun = integrateReviewedCandidate(deps, run.task_id, run);
      promoteReviewedCandidate(deps, workerRun);
    }
    if (reviewData.work_item?.review_status !== outcome.status) {
      const notes = outcome.findings.length ? `${outcome.notes}\n\n${outcome.findings.map((finding) => `- ${finding}`).join("\n")}` : outcome.notes;
      const update = execPic(["work-item", "review", run.task_id, outcome.status, "--notes", notes, "--pipeline-run-id", run.id], deps.cwd);
      if (update.error) throw new Error(update.error);
    }
  }
  if (isPlanningStage(run.stage)) {
    publishPlanningHandoff(deps, run, outputFor(run));
    checkpoint(run, "advanced", deps.cwd);
    return;
  }
  const parentId = data.work_item?.parent_id;
  checkpoint(run, "advanced", deps.cwd);
  await advance(deps, run.task_id, parentId);
}
