// Planning handoff constraint: every planning stage handoff must carry and
// validate Work Item identity, the dispatched stage, the predecessor checkpoint,
// and the persisted Plan profile version/hash so a consumer can reject a stale
// or mismatched stage binding.
export interface PlanningHandoffAttributes {
  work_item_id: string;
  stage: string;
  predecessor_checkpoint: string;
  profile_version: string;
  profile_hash: string;
}

export const PLANNING_HANDOFF_STAGES = ["rri", "vision", "blueprint", "task_graph"] as const;

export function normalizePlanningHandoffAttributes(attrs: Record<string, unknown>): PlanningHandoffAttributes {
  return {
    work_item_id: String(attrs.work_item_id || ""),
    stage: String(attrs.stage || ""),
    predecessor_checkpoint: String(attrs.predecessor_checkpoint || ""),
    profile_version: String(attrs.profile_version ?? ""),
    profile_hash: String(attrs.profile_hash || ""),
  };
}

function xmlEscapeAttribute(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&apos;");
}

/**
 * Validate planning handoff attributes against the bounded planning stages.
 * Throws when identity, stage, or the required profile version/hash binding is
 * malformed or references an unsupported stage.
 */
export function assertPlanningHandoffAttributes(attrs: PlanningHandoffAttributes): void {
  if (!attrs.work_item_id) throw new Error("planning handoff missing Work Item identity");
  if (!PLANNING_HANDOFF_STAGES.includes(attrs.stage as (typeof PLANNING_HANDOFF_STAGES)[number])) {
    throw new Error(`planning handoff has unsupported stage ${attrs.stage || "none"}`);
  }
  if (!attrs.profile_version || !attrs.profile_hash) {
    throw new Error(`planning ${attrs.stage} handoff requires a persisted profile version and hash`);
  }
}

/**
 * Build a validated planning handoff envelope that binds Work Item identity,
 * stage, predecessor checkpoint, and profile version/hash to the stage payload.
 */
export function buildPlanningHandoffXml(attrs: PlanningHandoffAttributes, body: string): string {
  assertPlanningHandoffAttributes(attrs);
  const escaped = {
    work_item_id: xmlEscapeAttribute(attrs.work_item_id),
    stage: xmlEscapeAttribute(attrs.stage),
    predecessor_checkpoint: xmlEscapeAttribute(attrs.predecessor_checkpoint),
    profile_version: xmlEscapeAttribute(attrs.profile_version),
    profile_hash: xmlEscapeAttribute(attrs.profile_hash),
  };
  // Planning handoff constraint: wrap the stage payload in CDATA so raw output
  // containing XML metacharacters or a literal </planning_handoff> cannot corrupt
  // the envelope, and split any embedded terminator so the block stays well-formed.
  const cdataBody = `<![CDATA[${body.replace(/]]>/g, "]]]]><![CDATA[>")}]]>`;
  return `<planning_handoff schema_version="1" work_item_id="${escaped.work_item_id}" stage="${escaped.stage}" predecessor_checkpoint="${escaped.predecessor_checkpoint}" profile_version="${escaped.profile_version}" profile_hash="${escaped.profile_hash}">\n<body>\n${cdataBody}\n</body>\n</planning_handoff>`;
}

/**
 * Extract and validate the binding attributes of a planning handoff envelope.
 */
export function parsePlanningHandoffAttributes(xml: string): PlanningHandoffAttributes {
  const match = xml.match(/^<planning_handoff\s([^>]+)>[\s\S]*?<\/planning_handoff>$/);
  if (!match) throw new Error("planning handoff must be one <planning_handoff> document");
  const parsed = match[1]!.match(/([a-zA-Z_][\w.-]*)="([^"]*)"/g)?.reduce<Record<string, string>>((values, attribute) => {
    const found = attribute.match(/^([a-zA-Z_][\w.-]*)="([^"]*)"$/);
    if (found) values[found[1]!] = found[2]!;
    return values;
  }, {}) || {};
  const attrs = normalizePlanningHandoffAttributes(parsed);
  assertPlanningHandoffAttributes(attrs);
  return attrs;
}
