import type { PipelineRun, PipelineStage } from "./pipeline-types.ts";
import { normalizePlanningDepth, planStagesForProfile, type PlanningDepth } from "../tasking/workflow-modes.ts";
import { getBlockingTaskDependencies } from "../tasking/workflow-gates.ts";
import { validateSkillFamilies } from "../subagent/skills.ts";
import { activePackDoneReports, isMutationStage, latestVerificationAfter, reviewRequiresOwner, reviewStatusForCandidate } from "./report-parsing.ts";
export interface PlanningProfileState {
  depth: PlanningDepth;
  version: number;
  contentHash: string;
  stages: string[];
  resolved: boolean;
}

// Stage-taxonomy parity constraint: mirrors the Go workItemStages taxonomy in
// go-pic/cmd/pic/work_items.go stage for stage, including the supplementary
// rri_t_scenarios entry, so persisted profiles are never silently stripped of it.
export const PLANNING_STAGE_ORDER: string[] = ["scan", "rri", "rri_t_scenarios", "vision", "blueprint", "contracts", "task_graph"];

// Supplementary planning stages mirror the Go visibility-only carve-out in
// work_items.go ("rri_t_scenarios is a supplementary retained scenario
// artifact: it is reported for owner visibility but never gates the planning
// workflow"): they stay in the taxonomy and persisted profiles but never
// produce an approval checkpoint, so predecessor lookups must skip them.
export const SUPPLEMENTARY_PLANNING_STAGES: string[] = ["rri_t_scenarios"];

// Persisted-profile constraint (fail-closed): a persisted stage array is
// accepted only when every entry is a supported stage and the entries follow
// the canonical taxonomy order. Unknown, duplicated, or reordered entries
// reject the whole profile (returning []) instead of being silently filtered,
// so a malformed persisted order can never bypass predecessor sequencing or
// diverge from the Go-computed stage taxonomy.
function canonicalPersistedStages(parsed: unknown): string[] {
  if (!Array.isArray(parsed)) return [];
  let cursor = -1;
  const stages: string[] = [];
  for (const entry of parsed) {
    const index = PLANNING_STAGE_ORDER.indexOf(String(entry));
    // An unknown stage (-1), a duplicate (equal index), or a reordered entry
    // (smaller index) all fail the strictly-advancing cursor check at once.
    if (index <= cursor) return [];
    cursor = index;
    stages.push(String(entry));
  }
  return stages;
}

export function persistedProfileDepth(value: unknown): PlanningDepth {
  return normalizePlanningDepth(value);
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function resolvePlanProfile(data: any): PlanningProfileState {
  const item = data?.work_item || {};
  const rawProfiles = Array.isArray(data?.profiles) ? data.profiles : [];
  const plan = rawProfiles
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    .filter((entry: any) => entry.profile_name === "plan")
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    .sort((a: any, b: any) => Number(b.profile_version || 0) - Number(a.profile_version || 0))[0];
  if (plan) {
    let stages: string[] = [];
    try {
      stages = canonicalPersistedStages(JSON.parse(plan.stages_json || "[]"));
    } catch {}
    if (stages.length) {
      return {
        depth: persistedProfileDepth(plan.planning_depth),
        version: Number(plan.profile_version || 0),
        contentHash: plan.content_hash || "",
        stages,
        resolved: true,
      };
    }
  }
  const depth = persistedProfileDepth(item.planning_depth);
  return {
    depth,
    version: 0,
    contentHash: "",
    stages: planStagesForProfile(item.type || "", item.parent_id || "", depth),
    resolved: false,
  };
}



// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function normalizePipelineData(data: any): any {
  if (!data?.work_item) return data;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const approvedArtifactIds = new Set((data.checkpoints || []).map((checkpoint: any) => checkpoint.artifact_id));
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const approvedArtifacts = (data.artifacts || []).filter((artifact: any) => approvedArtifactIds.has(artifact.id)).map((artifact: any) => {
    try { return { ...artifact, ...JSON.parse(artifact.content || "{}"), status: "approved" }; }
    catch { return { ...artifact, status: "approved" }; }
  });
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const packs = (data.instruction_packs || []).map((pack: any) => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
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
    plan_profile: resolvePlanProfile(data),
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    dependencies: (data.dependencies || []).map((dependency: any) => ({
      ...dependency,
      depends_on_task_id: dependency.depends_on_task_id || dependency.depends_on_work_item_id,
    })),
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    scan_reports: approvedArtifacts.filter((artifact: any) => artifact.stage === "scan"),
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    rri_sessions: approvedArtifacts.filter((artifact: any) => artifact.stage === "rri"),
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    designs: approvedArtifacts.filter((artifact: any) => artifact.stage === "blueprint"),
    instruction_packs: packs,
  };
}

// Resumable execution states constraint: an interrupted pipeline (expired/stale
// review run, scheduler death mid-autofix) leaves next_stage parked at a runnable
// stage with no active run. Selection must pick these up; nextPipelineStage stays
// safe — under-selection deadlocks the item forever.

export const RESUMABLE_NEXT_STAGES = ["implement", "review", "autofix"];

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function isResumableExecutionState(state: any): boolean {
  return RESUMABLE_NEXT_STAGES.includes(state?.next_stage);
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function canonicalReadyLeafIds(root: any, load: (id: string) => any): string[] {
  const ready: string[] = [];
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const visit = (data: any): void => {
    const item = data?.work_item;
    if (!item || item.status === "cancelled") return;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    const activeChildren = (data.children || []).filter((child: any) => child.status !== "cancelled");
    if (activeChildren.length) {
      for (const child of activeChildren) visit(load(child.id));
      return;
    }
    if (["task", "bug", "chore"].includes(item.type)) {
      if (data.ready || isResumableExecutionState(data.execution_state)) ready.push(item.id);
    }
  };
  visit(root);
  return ready;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function buildPipelineDryRun(root: any, load: (id: string) => any): any {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const leaves: any[] = [];
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
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

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function pipelineWorkerBlockReason(data: any): string | null {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const activePacks = (data.instruction_packs || []).filter((pack: any) => pack.status === "active");
  const awaitingFirstClaimTIP = data.canonical && data.ready && activePacks.length === 0;
  if (activePacks.length !== 1 && !awaitingFirstClaimTIP) return `Work Item "${data.work_item?.title || data.work_item?.id || "unknown"}" requires exactly one active Task Instruction Pack before work.`;
  const blockers = getBlockingTaskDependencies(data.dependencies || [], data.phase_metadata || null);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  if (blockers.length) return `Work Item "${data.work_item?.title || data.work_item?.id || "unknown"}" is blocked by incomplete dependencies: ${blockers.map((dependency: any) => dependency.depends_on_task_id).join(", ")}`;
  const activePack = activePacks[0];
  if (!activePack) return null;
  if (!data.canonical && (Number(activePack.content_schema_version || 1) < 3 || !activePack.effective_contract_snapshot_id || !activePack.effective_contract_snapshot_hash)) return `Work Item "${data.work_item?.title || "unknown"}" requires a schema-v3 Task Instruction Pack with an effective contract snapshot; revise and activate the TIP before launch.`;
  try {
    const families = JSON.parse(activePack.skill_families_json || "[]");
    validateSkillFamilies(families, { cwd: process.cwd() });
  } catch (error) {
    return error instanceof Error ? error.message : String(error);
  }
  return null;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function assertRunContractCurrent(data: any, run: Pick<PipelineRun, "instruction_pack_id" | "instruction_pack_hash" | "effective_contract_snapshot_id" | "effective_contract_snapshot_hash">): void {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
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

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function nextPipelineStage(data: any, runs: any[] = []): PipelineStage | null {
  if (data.canonical && data.execution_state) {
    if (data.execution_state.pipeline_stage) return data.execution_state.pipeline_stage;
    return data.ready && data.execution_state.next_stage === "instruction_pack" ? "worker" : null;
  }
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
  if (!activePack && (data.scan_reports || [])[0]?.status !== "completed") return "scan";
  if (!activePack) return null;
  const doneReports = activePackDoneReports(data, activePack);
  const latest = doneReports[0];
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  if ((data.owner_decisions || []).some((decision: any) => decision.decision === "rejected" && decision.completion_report_id
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    && (data.completion_reports || []).some((report: any) => report.id === decision.completion_report_id && report.instruction_pack_id === activePack.id))) return "worker";
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const candidate = runs.find((run: any) => isMutationStage(run.stage) && run.status === "completed"
    && run.instruction_pack_hash === activePack.content_hash && run.artifact_saved_at && run.integrated_patch_hash);
  if (!latest && candidate) {
    const review = reviewStatusForCandidate(runs, candidate);
    return review === "failed" && !reviewRequiresOwner(runs, candidate) ? "worker" : review === "failed" ? null : "review";
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const done = Boolean(latest && runs.some((run: any) =>
    run.id === latest.pipeline_run_id && isMutationStage(run.stage) && run.status === "completed" && (run.artifact_saved_at || run.integrated_at) && run.integrated_patch_hash,
  ));
  if (doneReports.length && !done) return null;
  if (!done) return "worker";
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const completionCandidate = runs.find((run: any) => run.id === latest?.pipeline_run_id);
  const durableReviewStatus = reviewStatusForCandidate(runs, completionCandidate);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const reviewStatus = data.canonical || runs.some((run: any) => run.stage === "review") ? durableReviewStatus : data.work_item?.review_status;
  if (reviewStatus === "failed") return reviewRequiresOwner(runs, completionCandidate) ? null : "worker";
  const verification = latestVerificationAfter(data, latest);
  if (reviewStatus === "passed" && verification && (verification.status === "failed" || verification.status === "partial")) return "autofix";
  if (reviewStatus !== "passed") return "review";
  return null;
}

export function workerIntegrationCandidate(runs: PipelineRun[]): PipelineRun | undefined {
  return runs.find((run) => isMutationStage(run.stage) && run.artifact_saved_at && !run.integrated_at && !run.advanced_at);
}
