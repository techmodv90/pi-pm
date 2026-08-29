// Typed boundary for `pic show` output. The scheduler previously consumed this
// JSON as untyped `any`, so a Go-side shape drift surfaced as silent undefined
// deep inside stage logic. parsePicShow fails closed on structural mismatch,
// naming the offending field, and normalizes absent collections to empty arrays
// because `pic show` output varies by Work Item type and pipeline state.

export interface PicWorkItem {
  id: string;
  type: string;
  title: string;
  description?: string;
  status?: string;
  priority?: string;
  parent_id?: string;
  review_status?: string;
  review_notes?: string;
  planning_depth?: string;
  deferred?: number;
  claimed_at?: string;
  claimed_by?: string;
  created_at?: string;
  workflow_mode?: string;
  design_status?: string;
  owner_status?: string;
}

export interface PicProject {
  name?: string;
  root_path?: string;
}

export interface PicArtifact {
  id?: string;
  work_item_id?: string;
  stage?: string;
  revision?: number;
  content?: string;
  content_hash?: string;
  summary?: string;
  created_at?: string;
}

export interface PicCheckpoint {
  id?: string;
  stage?: string;
  artifact_id?: string;
  artifact_revision?: number;
  content_hash?: string;
  decision_type?: string;
  created_at?: string;
}

export interface PicInstructionPack {
  id?: string;
  work_item_id?: string;
  version?: number;
  status?: string;
  content_json?: string;
  content_hash?: string;
  instruction_pack_id?: string;
  instruction_pack_version?: number;
  effective_contract_snapshot_id?: string;
  effective_contract_snapshot_hash?: string;
  display_key?: string;
  constraints_json?: string;
  files_json?: string;
  verification_json?: string;
  requirement_snapshots_json?: string;
  goal?: string;
  module?: string;
  estimated_effort_minutes?: number;
}

export interface PicCompletionReport {
  id?: string;
  status?: string;
  instruction_pack_id?: string;
  instruction_pack_version?: number;
  instruction_pack_hash?: string;
  pipeline_run_id?: string;
  summary?: string;
  created_at?: string;
}

export interface PicVerificationReport {
  id?: string;
  status?: string;
  summary?: string;
  pipeline_run_id?: string;
  created_at?: string;
  items?: PicVerificationItem[];
}

export interface PicVerificationItem {
  requirement_id?: string;
  status?: string;
  evidence?: string;
  notes?: string;
}

export interface PicOwnerDecision {
  id?: string;
  decision?: string;
  decision_type?: string;
  notes?: string;
  completion_report_id?: string;
  created_at?: string;
}

export interface PicEscalation {
  id?: string;
  level?: string | number;
  status?: string;
  title?: string;
  resolution_json?: string;
  instruction_pack_id?: string;
  resolved_at?: string;
}

export interface PicChild {
  id?: string;
  type?: string;
  title?: string;
  status?: string;
}

export interface PicDependency {
  depends_on_task_id?: string;
  dependency_type?: string;
  status?: string;
  review_status?: string;
  title?: string;
}

export interface PicProfile {
  profile_name?: string;
  profile_version?: number | string;
  planning_depth?: unknown;
  stages_json?: string;
  content_hash?: string;
}

export interface PicShowDocument {
  work_item: PicWorkItem;
  project?: PicProject;
  artifacts: PicArtifact[];
  checkpoints: PicCheckpoint[];
  instruction_packs: PicInstructionPack[];
  completion_reports: PicCompletionReport[];
  verification_reports: PicVerificationReport[];
  scan_reports: Record<string, unknown>[];
  designs: Record<string, unknown>[];
  requirements: Record<string, unknown>[];
  children: PicChild[];
  dependencies: PicDependency[];
  owner_decisions: PicOwnerDecision[];
  escalations: PicEscalation[];
  profiles: PicProfile[];
  ready?: boolean;
  canonical?: boolean;
  execution_state?: string;
  current_review?: Record<string, unknown>;
  error?: string;
}

type CollectionKey = "artifacts" | "checkpoints" | "instruction_packs" | "completion_reports" | "verification_reports" | "scan_reports" | "designs" | "requirements" | "children" | "dependencies" | "owner_decisions" | "escalations" | "profiles";

const COLLECTION_KEYS: CollectionKey[] = [
  "artifacts", "checkpoints", "instruction_packs", "completion_reports", "verification_reports",
  "scan_reports", "designs", "requirements", "children", "dependencies", "owner_decisions",
  "escalations", "profiles",
];

function requireObject(value: unknown, what: string): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`pic show returned a non-object document: expected ${what}`);
  return value as Record<string, unknown>;
}

function optionalCollection(value: unknown, key: CollectionKey): Record<string, unknown>[] {
  if (value === undefined) return [];
  if (!Array.isArray(value)) throw new Error(`pic show ${key} must be an array`);
  return value.map((entry, index) => {
    // Fail closed on element shapes: a null or scalar entry would surface as an
    // opaque undefined-field failure deep in scheduler logic instead of here.
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) throw new Error(`pic show ${key}[${index}] must be an object`);
    return entry as Record<string, unknown>;
  });
}

function requiredString(item: Record<string, unknown>, field: string, what: string): string {
  const value = item[field];
  if (typeof value !== "string" || !value.trim()) throw new Error(`pic show ${what}.${field} must be a non-empty string`);
  return value;
}

function optionalFlag(value: unknown, key: string): boolean | undefined {
  if (value === undefined) return undefined;
  if (typeof value !== "boolean") throw new Error(`pic show ${key} must be a boolean`);
  return value;
}

/**
 * Validate and normalize one `pic show` document. Throws an Error naming the
 * first malformed field; absent optional collections normalize to empty arrays.
 */
export function parsePicShow(raw: unknown): PicShowDocument {
  const doc = requireObject(raw, "a work item document");
  const workItemRaw = doc.work_item;
  if (workItemRaw === undefined) throw new Error("pic show document is missing work_item");
  const workItem = requireObject(workItemRaw, "work_item object");
  const parsedWorkItem: PicWorkItem = {
    id: requiredString(workItem, "id", "work_item"),
    type: requiredString(workItem, "type", "work_item"),
    title: requiredString(workItem, "title", "work_item"),
  };
  for (const [key, value] of Object.entries(workItem)) {
    if (key === "id" || key === "type" || key === "title") continue;
    (parsedWorkItem as unknown as Record<string, unknown>)[key] = value;
  }
  const parsed = { work_item: parsedWorkItem } as unknown as PicShowDocument;
  for (const key of COLLECTION_KEYS) {
    (parsed as unknown as Record<string, unknown>)[key] = optionalCollection(doc[key], key);
  }
  if (doc.project !== undefined) {
    parsed.project = requireObject(doc.project, "project object") as PicProject;
  }
  if (doc.ready !== undefined) parsed.ready = optionalFlag(doc.ready, "ready");
  if (doc.canonical !== undefined) parsed.canonical = optionalFlag(doc.canonical, "canonical");
  if (doc.execution_state !== undefined) {
    if (typeof doc.execution_state !== "string") throw new Error("pic show execution_state must be a string");
    parsed.execution_state = doc.execution_state;
  }
  if (doc.current_review !== undefined) {
    parsed.current_review = requireObject(doc.current_review, "current_review object");
  }
  if (doc.error !== undefined) {
    if (typeof doc.error !== "string") throw new Error("pic show error must be a string");
    parsed.error = doc.error;
  }
  return parsed;
}
