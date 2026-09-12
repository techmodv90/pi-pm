// Stage transition priming constraint: every dispatched planning stage prompt
// carries the persisted lineage (profile version/hash, predecessor checkpoint),
// a bounded digest of each approved predecessor artifact, the repo context from
// the accepted Scan, and a per-stage definition of done — so a contractor can
// transit phases without reassembling context from raw artifact IDs.

export interface StagePrimerDigest {
  stage: string;
  artifact_id: string;
  artifact_revision: number | string;
  content_hash: string;
  content: string;
}

export interface StagePrimerProfile {
  version: number | string;
  contentHash: string;
  stages: string[];
}

export interface StagePrimerContext {
  work_item_id: string;
  stage: string;
  profile?: StagePrimerProfile;
  predecessor_checkpoint?: { stage: string; artifact_id: string; artifact_revision: number | string; content_hash: string };
  approved_digests?: StagePrimerDigest[];
}

const PRIMER_DIGEST_CHARS = 1000;

const STAGE_DEFINITIONS_OF_DONE: Record<string, string[]> = {
  scan: [
    "One canonical Scan Report saved as structured XML with source-backed evidence.",
    "Owner acceptance requested; the report is not self-approved.",
  ],
  rri: [
    "Owner-impacting P0/P1 questions resolved or explicitly open.",
    "Requirements are testable with Given/When/Then acceptance criteria.",
    "The interview is published exactly once via save_rri_interview.",
  ],
  vision: [
    "One Vision JSON artifact with every field present and arrays nonempty.",
    "Rendered Markdown presented to the owner for approval.",
  ],
  blueprint: [
    "Blueprint JSON is the solution spec: project_info, goals, architecture, tech_stack, file_structure, rri_requirements_matrix, implementation_decisions, adr_candidates, excluded_keys, and owner-approved verification_seams (decomposition_policy_version 2, schema_version 2.1, no task_decomposition_preview).",
    "ADR candidates carry eligibility criteria and docs/adr/ files are written only after owner approval; draft state writes nothing.",
    "The reviewed draft passed all five checkpoint checks, including verification seams.",
    "The owner approved the draft via approve_blueprint_draft.",
  ],
  contracts: [
    "Every deliverable maps to at least one RRI requirement key.",
    "Every obligation has atomic behavior with literal Given/When/Then acceptance steps, a primary decomposition class, and a seam the approved Blueprint declares.",
    "The Contract binds the exact approved Blueprint artifact id, revision, and content hash.",
    "The owner replied CONFIRM before the Contract was saved and approved.",
  ],
  task_graph: [
    "The graph covers every non-deferred requirement with vertical tracer-bullet slices by default and justified, reasoned exceptions.",
    "Every blocking edge carries a rationale, every executable node an effective acceptance, and every verification entry a Blueprint-declared seam.",
    "validate_work_item_graph passes.",
    "The owner answered the five granularity questions and approved the graph revision.",
  ],
};

function boundedDigest(content: string, budget = PRIMER_DIGEST_CHARS): string {
  const flat = String(content || "").trim().replace(/\s+/g, " ");
  return flat.length > budget ? `${flat.slice(0, budget)}…` : flat;
}

export function buildStagePrimer(context: StagePrimerContext): string {
  const lines: string[] = [
    `# STAGE PRIMER: ${context.stage}`,
    `Work Item: ${context.work_item_id}`,
    `Stage: ${context.stage}`,
  ];
  if (context.profile) {
    lines.push(`Lineage: profile v${context.profile.version} (${context.profile.contentHash || "unhashed"})`);
  }
  if (context.predecessor_checkpoint) {
    const checkpoint = context.predecessor_checkpoint;
    lines.push(`Predecessor: checkpoint ${checkpoint.stage}@${checkpoint.artifact_revision} (${checkpoint.content_hash || "unhashed"})`);
  }
  const digests = context.approved_digests || [];
  if (digests.length) {
    lines.push("", "## APPROVED CONTEXT DIGESTS");
    for (const digest of digests) {
      lines.push(`- ${digest.stage} @${digest.artifact_revision} (${digest.content_hash || "unhashed"}): ${boundedDigest(digest.content)}`);
    }
    lines.push("Load each full artifact with `load_planning_artifact` before authoring. Do not use historical revisions.");
  }
  lines.push("", "## DEFINITION OF DONE");
  for (const item of STAGE_DEFINITIONS_OF_DONE[context.stage] || ["Follow the persisted stage instructions exactly."]) {
    lines.push(`- ${item}`);
  }
  lines.push("- Do not save or approve owner decisions yourself; request explicit owner approval.");
  return lines.join("\n") + "\n";
}
