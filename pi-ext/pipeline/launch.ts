import { existsSync } from "node:fs";
import { join } from "node:path";
import { execPic } from "../core/cli-helpers.ts";
import { withInheritedParentWorkflowArtifacts } from "../tasking/task-artifacts.ts";
import { discoverAgents } from "../subagent/agents.ts";
import { prepareSubagentWorktree } from "../subagent/runner.ts";
import type { PipelineDispatch } from "./pipeline-dispatch.ts";
import { writePipelineDispatch } from "./pipeline-dispatch.ts";
import type { PipelineRun, PipelineStage } from "./pipeline-types.ts";
import { currentFailedReview, isMutationStage } from "./report-parsing.ts";
import { assertCleanGit, rejectedCandidatePatch, repositoryHead, verificationEnvironmentFingerprint } from "./integration.ts";
import { REVIEW_FIX_ROUND_LIMIT, buildReviewFixCapBlock, reviewCycleCount } from "./corrections.ts";
import { isPlanningStage, pipelineSpawnParams, stageAgent, stagePrompt, workerSessionPath } from "./stage-prompts.ts";
import { normalizePipelineData, pipelineWorkerBlockReason, resolvePlanProfile, type PlanningProfileState } from "./stage-resolution.ts";
import { evaluateSkillFamilyRouting, recordSkillRoutingEvent } from "./skill-routing.ts";
import { checkpoint } from "./run-helpers.ts";
import type { SchedulerDeps } from "./scheduler-context.ts";

// Planning profile constraint: refuse to dispatch a planning stage that the
// persisted Plan profile (or the kind/depth contract before it is persisted)
// does not include, and bind the claim to the persisted profile version/hash
// so a stale Go/TypeScript profile view cannot dispatch an unapproved stage.
// A stage must not dispatch before the Plan profile is persisted: the handoff
// envelope requires a profile version/hash, so a resolved:false profile would
// publish an invalid envelope with no recovery.
function planEligibility(deps: SchedulerDeps, taskId: string, stage: PipelineStage): { profile: PlanningProfileState } {
  const profile = resolvePlanProfile(normalizePipelineData(deps.showItem(taskId)));
  if (!profile.resolved || !profile.contentHash) {
    throw new Error(`planning stage ${stage} cannot dispatch for ${taskId} before the Plan profile is persisted; persist the approved profile before dispatch`);
  }
  if (!profile.stages.includes(stage)) {
    throw new Error(`planning stage ${stage} is not in the persisted plan profile for ${taskId} (depth ${profile.depth}); revise the profile before dispatch`);
  }
  return { profile };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export async function launchGroup(deps: SchedulerDeps, stage: PipelineStage, taskIds: string[], explicitRetry = false): Promise<any> {
  const { cwd } = deps;
  const active = execPic(["workflow", "pipeline-active"], cwd);
  const activeRuns = Array.isArray(active) ? active.filter((run: PipelineRun) => run.stage === stage && taskIds.includes(run.task_id)) : [];
  const activeTaskIds = new Set(activeRuns.map((run: PipelineRun) => run.task_id));
  const launchTaskIds = taskIds.filter((taskId) => !activeTaskIds.has(taskId));
  if (launchTaskIds.length === 0) return { stage, taskIds, pipelineRunIds: [], activePipelineRunIds: activeRuns.map((run: PipelineRun) => run.id), subagentRunIds: [] };
  const initialPatchPaths = new Map<string, string>();
  const reviewFixTaskIds = new Set<string>();
  if (isMutationStage(stage)) {
    assertCleanGit(cwd);
    for (const taskId of launchTaskIds) {
      const raw = deps.showItem(taskId);
      const data = raw.work_item ? normalizePipelineData(raw) : withInheritedParentWorkflowArtifacts(raw, cwd);
      if (!data.work_item) throw new Error(data.error || `Task ${taskId} not found`);
      const blockReason = pipelineWorkerBlockReason(data);
      if (blockReason) throw new Error(blockReason);
      const runs = deps.pipelineRuns(taskId);
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
      const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
      // Observe-mode routing telemetry (skill-family-routing plan): record the
      // routing evaluation for every worker/autofix launch without ever
      // blocking it — enforcement is a follow-up gated on this data.
      if (stage === "worker" || stage === "autofix") {
        const routingPack = (data.instruction_packs || []).find((pack: { status?: string }) => pack.status === "active");
        const scanEvidence = Array.isArray(data.scan_reports) && data.scan_reports.length ? [data.scan_reports[0]] : [];
        recordSkillRoutingEvent(cwd, taskId, stage, routingPack?.id || "", evaluateSkillFamilyRouting(routingPack || {}, scanEvidence, { cwd }));
      }
      if (stage === "worker" && currentFailedReview(runs, activePack)) {
        const cycle = reviewCycleCount(runs);
        if (cycle >= REVIEW_FIX_ROUND_LIMIT) {
          // Round-cap persistence constraint: persist the owner-action block
          // durably BEFORE refusing the launch, so the failed review is elevated
          // to owner-approval-required and nextPipelineStage/claim gates stop
          // relaunching the fix worker across reconciliation (a transient throw
          // alone would leave the failed review eligible for a repeated launch).
          const failedReview = currentFailedReview(runs, activePack);
          const capBlock = buildReviewFixCapBlock(taskId, failedReview?.findings || []);
          try {
            const blocked = execPic(["workflow", "review-fix-block", taskId, "--summary", capBlock], cwd);
            if (blocked.error) throw new Error(blocked.error);
          } catch (error) {
            const message = error instanceof Error ? error.message : String(error);
            throw new Error(`round-cap block persisted with error (${message}); owner action still required:\n\n${capBlock}`);
          }
          throw new Error(capBlock);
        }
        reviewFixTaskIds.add(taskId);
        const rejectedPatch = rejectedCandidatePatch(data, runs, cwd);
        if (rejectedPatch) initialPatchPaths.set(taskId, rejectedPatch);
      }
    }
  }
  const claims: PipelineRun[] = [];
  try {
    for (const taskId of launchTaskIds) {
      const data = deps.showItem(taskId);
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
      const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
      const claimArgs = ["workflow", "pipeline-claim", taskId, stage, "--lease-seconds", "14400", "--environment-fingerprint", verificationEnvironmentFingerprint(cwd), "--base-commit", repositoryHead(cwd)];
      if (isPlanningStage(stage)) {
        const { profile } = planEligibility(deps, taskId, stage);
        if (profile.resolved && profile.version > 0) claimArgs.push("--profile-version", String(profile.version), "--profile-hash", profile.contentHash);
      }
      if (stage === "worker" && reviewFixTaskIds.has(taskId)) claimArgs.push("--review-fix", "1");
      if (stage === "worker" && explicitRetry) claimArgs.push("--explicit-retry", "1");
      if (activePack && (isMutationStage(stage) || stage === "review")) claimArgs.push("--instruction-pack-id", activePack.id ?? "", "--instruction-pack-hash", activePack.content_hash ?? "");
      const claim = execPic(claimArgs, cwd);
      if (claim.error) throw new Error(claim.error);
      claims.push(claim);
    }
    if (isMutationStage(stage)) {
      await new Promise<void>((resolve) => setImmediate(resolve));
      for (const taskId of launchTaskIds) {
        const raw = deps.showItem(taskId);
        const reset = execPic(["work-item", "status", taskId, "in_progress"], cwd);
        if (reset.error) throw new Error(reset.error);
        if (!raw.work_item) {
          const event = execPic(["workflow", "event-add", taskId, "implementation_started", "--actor-role", "orchestrator", "--summary", stage === "autofix" ? "Targeted autofix started" : "Persisted Worker stage started"], cwd);
          if (event.error) throw new Error(event.error);
        }
      }
    }
    const subagentRunIds: string[] = [];
    const dispatches: PipelineDispatch[] = [];
    await new Promise<void>((resolve) => setImmediate(resolve));
    for (let index = 0; index < claims.length; index++) {
      const claim = claims[index]!;
      const taskId = launchTaskIds[index]!;
      const data = normalizePipelineData(deps.showItem(taskId));
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
      const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
      let skillFamilies: string[] = [];
      if (activePack?.skill_families_json) {
        const parsed = JSON.parse(activePack.skill_families_json);
        if (!Array.isArray(parsed) || !parsed.every((family) => typeof family === "string")) throw new Error(`Task ${taskId} has invalid persisted skill families`);
        skillFamilies = parsed;
      }
      let taskPrompt = stagePrompt(stage, taskId, cwd);
      // Fix-round findings relay (T004 smoke, 2026-09-07): dispatch payloads
      // carry only the original task description, so review findings never
      // reach the fix-round worker unless attached here — the contractor
      // relay proved unreliable when done by hand.
      if (stage === "worker" && claim.candidate_run_id) {
        const failedReview = deps.pipelineRuns(taskId).filter((entry) => entry.stage === "review" && entry.status === "completed" && entry.candidate_run_id === claim.candidate_run_id)
          .filter((entry) => { try { return JSON.parse(entry.result_json || "{}")?.review_status === "failed"; } catch { return false; } })
          .pop();
        const findingsRaw: unknown = (failedReview as { findings?: unknown } | undefined)?.findings;
        const findings = Array.isArray(findingsRaw) ? findingsRaw.filter((finding: unknown) => typeof finding === "string" && (finding as string).trim()) as string[] : [];
        if (findings.length) {
          taskPrompt += `\n\nReview findings to address (from the failed review of candidate ${claim.candidate_run_id}):\n${findings.map((finding: string) => `- ${finding}`).join("\n")}`;
        }
      }
      if (stage === "rri") taskPrompt += `\n\nComplete RRI source context:\n${JSON.stringify({ work_item: data.work_item, scan_reports: data.scan_reports, requirements: data.requirements || [], owner_decisions: data.owner_decisions || [] })}`;
      const task = { agent: stageAgent(stage), task: taskPrompt, taskId, ...(isMutationStage(stage) || stage === "review" ? { skillFamilies } : {}) };
      const spec = pipelineSpawnParams(stage, task, cwd);
      if (stage === "worker") {
        spec.initialPatchPath = initialPatchPaths.get(taskId);
        spec.sessionPath = workerSessionPath(cwd, activePack?.id || claim.instruction_pack_id || taskId);
        // Durable worker worktree constraint (RLB-GAP-001): worker-stage spawns
        // (including review-fix relaunches) key their worktree by instruction
        // pack so a transient failure retains the partial work for the retry;
        // review/scan stages stay run-keyed and clean up per GAP-091/096.
        const packKey = activePack?.id || claim.instruction_pack_id || taskId;
        spec.durableWorktreeKey = packKey;
        const retainedMode = deps.retainedFailures.get(packKey);
        if (retainedMode) spec.resumeFailureMode = retainedMode;
      }
      if (stage === "review") {
        const candidate = deps.pipelineRuns(taskId).find((entry) => entry.id === claim.candidate_run_id);
        if (!candidate?.integrated_patch_path || candidate.integrated_patch_hash !== claim.candidate_patch_hash || !existsSync(candidate.integrated_patch_path)) {
          throw new Error("review candidate patch attestation failed");
        }
        spec.initialPatchPath = candidate.integrated_patch_path;
      }
      const agent = discoverAgents(cwd, "project").find((candidate) => candidate.name === spec.agent);
      if (!agent) throw new Error(`Task-system agent definition not found: ${spec.agent}`);
      if (spec.isolation === "worktree") {
        let prepared;
        try {
          // RLB-GAP-003: pass the claim's stamped base_commit so retained
          // worktrees align to the exact commit the candidate patch must
          // later apply against.
          prepared = await prepareSubagentWorktree(spec.cwd, spec.initialPatchPath, claim.id, spec.durableWorktreeKey || claim.id, claim.base_commit || undefined);
        } catch (error) {
          if (stage === "review") {
            const candidate = deps.pipelineRuns(taskId).find((entry) => entry.id === claim.candidate_run_id);
            if (candidate) {
              execPic(["workflow", "pipeline-complete", candidate.id, candidate.lease_token, "blocked", "--error", "candidate patch no longer applies to the current integration base"], cwd);
              checkpoint(candidate, "advanced", cwd);
            }
          }
          throw error;
        }
        spec.runId = prepared.runId;
        spec.preparedWorktree = prepared.cwd;
        spec.reusedRetainedWorktree = prepared.reused;
        // Fresh creation after a deterministic terminal: no retained worktree
        // exists for this pack anymore, so drop the stale failure-mode note.
        if (!prepared.reused && spec.durableWorktreeKey) deps.retainedFailures.delete(spec.durableWorktreeKey);
      }
      // Agent-tool dispatch: persist the dispatch record with the prepared
      // worktree; the contractor binds the Agent tool id (hard gate: empty
      // id rejected) and reports terminal output. The run row stays
      // `claimed` with no async_dir until bind, so reconcile skips it.
      const artifactDir = join(cwd, ".pi-subagents", "pipeline", claim.id);
      const dispatch: PipelineDispatch = {
        runId: claim.id,
        leaseToken: claim.lease_token,
        agent: spec.agent,
        stage,
        taskId,
        task: taskPrompt,
        asyncDir: artifactDir,
        worktree: spec.preparedWorktree || "",
        initialPatchPath: spec.initialPatchPath,
        skillFamilies,
      };
      writePipelineDispatch(dispatch);
      dispatches.push(dispatch);
    }
    return {
      stage,
      taskIds: launchTaskIds,
      pipelineRunIds: claims.map((claim) => claim.id),
      activePipelineRunIds: [...activeRuns.map((run: PipelineRun) => run.id), ...claims.map((claim) => claim.id)],
      subagentRunIds,
      dispatches,
    };
  } catch (error) {
    for (const claim of claims) execPic(["workflow", "pipeline-complete", claim.id, claim.lease_token, "failed", "--error", error instanceof Error ? error.message : String(error)], cwd);
    throw error;
  }
}
