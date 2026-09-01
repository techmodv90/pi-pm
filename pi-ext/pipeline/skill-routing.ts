import { execPicAsync, hasDb } from "../core/cli-helpers.ts";
import { evaluateSkillFamilyMatches, type CatalogOptions, type SkillFamilyMatch } from "../subagent/skills.ts";

export interface SkillFamilyRoutingEvaluation {
  selectedFamilies: string[];
  matchedFamilies: SkillFamilyMatch[];
  missingFamilies: string[];
  evidenceSources: string[];
}

interface RoutingPackView {
  id?: string;
  content_json?: string;
  skill_families_json?: string;
}

// Observe-mode constraint: routing evaluation never throws and never blocks a
// launch. A malformed pack content falls back to the persisted
// skill_families_json column; both are best-effort parses.
function selectedSkillFamilies(pack: RoutingPackView): { families: string[]; content: unknown; contentParsed: boolean } {
  let content: unknown;
  let contentParsed = false;
  try {
    content = JSON.parse(pack.content_json || "{}");
    contentParsed = true;
  } catch {}
  const fromContent = (content as { skillFamilies?: unknown } | undefined)?.skillFamilies;
  if (Array.isArray(fromContent) && fromContent.every((value) => typeof value === "string")) {
    return { families: fromContent, content, contentParsed };
  }
  try {
    const parsed = JSON.parse(pack.skill_families_json || "[]");
    if (Array.isArray(parsed)) return { families: parsed.filter((value): value is string => typeof value === "string"), content, contentParsed };
  } catch {}
  return { families: [], content, contentParsed };
}

export function evaluateSkillFamilyRouting(pack: RoutingPackView, scanEvidence: unknown[] = [], catalogOptions: CatalogOptions = {}): SkillFamilyRoutingEvaluation {
  const { families, content, contentParsed } = selectedSkillFamilies(pack);
  const evidence: unknown[] = [];
  const evidenceSources: string[] = [];
  if (contentParsed) {
    evidence.push(content);
    evidenceSources.push("pack_content");
  }
  if (scanEvidence.length) {
    evidence.push(...scanEvidence);
    evidenceSources.push("scan_artifact");
  }
  const matchedFamilies = evidence.length ? evaluateSkillFamilyMatches(evidence, catalogOptions) : [];
  return {
    selectedFamilies: families,
    matchedFamilies,
    missingFamilies: matchedFamilies.map((match) => match.id).filter((id) => !families.includes(id)),
    evidenceSources,
  };
}

export function skillRoutingEventPayload(stage: string, packId: string, evaluation: SkillFamilyRoutingEvaluation): Record<string, unknown> {
  return {
    stage,
    pack_id: packId,
    selected_families: evaluation.selectedFamilies,
    matched_families: evaluation.matchedFamilies.map((match) => ({ id: match.id, matched_by: match.matchedBy })),
    missing_families: evaluation.missingFamilies,
    evidence_sources: evaluation.evidenceSources,
  };
}

// Fire-and-forget wide event (activity-tracker pattern): telemetry failure must
// never block a launch, and execPicAsync already resolves failures into the
// result object instead of rejecting.
export function recordSkillRoutingEvent(cwd: string, taskId: string, stage: string, packId: string, evaluation: SkillFamilyRoutingEvaluation): void {
  if (!hasDb(cwd)) return;
  const payload = skillRoutingEventPayload(stage, packId, evaluation);
  const summary = `Skill family routing (${stage}): matched ${evaluation.matchedFamilies.length}, missing ${evaluation.missingFamilies.length}`;
  void execPicAsync(["workflow", "event-add", taskId, "skill_family_routing", "--actor-role", "scheduler", "--summary", summary, "--payload-json", JSON.stringify(payload)], cwd);
}
