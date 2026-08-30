export type PipelineStage = "scan" | "rri" | "vision" | "blueprint" | "contracts" | "task_graph" | "worker" | "review" | "autofix";
export type PipelineRunStatus = "claimed" | "running" | "completed" | "failed" | "blocked" | "cancelled" | "expired";

export interface PipelineRunRecord {
  id: string;
  task_id: string;
  stage: PipelineStage;
  status: PipelineRunStatus;
  result_json?: string;
  lease_token: string;
  attempt?: number;
  subagent_run_id?: string;
  child_index?: number;
  async_dir?: string;
  integrated_at?: string;
  integrated_patch_path?: string;
  integrated_patch_hash?: string;
  artifact_saved_at?: string;
  candidate_run_id?: string;
  candidate_patch_hash?: string;
  review_fix_cycle?: number;
  advanced_at?: string;
  instruction_pack_id?: string;
  instruction_pack_version?: number;
  instruction_pack_hash?: string;
  effective_contract_snapshot_id?: string;
  effective_contract_snapshot_hash?: string;
  agent_model?: string;
  environment_fingerprint?: string;
  base_commit?: string;
  error?: string;
  created_at?: string;

  skillFamilies?: string[];
  taskPrompt?: string;
}

export function isPipelineRunRecord(value: unknown): value is PipelineRunRecord {
  if (!value || typeof value !== "object") return false;
  const run = value as Record<string, unknown>;
  return typeof run.id === "string"
    && typeof run.task_id === "string"
    && typeof run.stage === "string"
    && ["scan", "rri", "vision", "blueprint", "contracts", "task_graph", "worker", "review", "autofix"].includes(run.stage)
    && typeof run.status === "string"
    && ["claimed", "running", "completed", "failed", "blocked", "cancelled", "expired"].includes(run.status)
    && typeof run.lease_token === "string";
}

export function parsePipelineRuns(value: unknown): PipelineRunRecord[] {
  if (!Array.isArray(value)) throw new Error("pic pipeline-runs returned a non-array result");
  const invalid = value.find((run) => !isPipelineRunRecord(run));
  if (invalid) throw new Error("pic pipeline-runs returned an invalid pipeline run record");
  return value;
}
/** Runtime view of one pipeline run record used across scheduler modules. */
export type PipelineRun = PipelineRunRecord;
