export interface BlueprintSeam {
  id: string;
  surface: string;
  isolates: string;
  prior_art?: string;
}
export interface BlueprintReport {
  project_info: { project: string; nature: string; date: string };
  goals: { primary_goal: string; target_audience: string; key_message: string };
  architecture: { building_blocks: string[]; connection_summary: string; data_flow: string };
  design_system?: { colors: { primary: string; secondary: string; accent: string }; typography: { headings: string; body: string } };
  tech_stack: Array<{ layer: string; choice: string; rationale: string; reuse: string }>;
  file_structure: Array<{ path: string; purpose: string }>;
  rri_requirements_matrix: Array<{ blueprint_section: string; requirements: string[]; source_questions: string[] }>;
  // Policy v1 (absent marker or version 1) requires task_decomposition_preview.
  // Policy v2 is the solution spec: preview retired, owner-approved
  // verification_seams required.
  task_decomposition_preview?: { estimated_tasks: number; tasks: Array<{ tip_id: string; title: string; goal: string }>; estimated_effort_minutes: number };
  decomposition_policy_version?: number;
  verification_seams?: BlueprintSeam[];
  // Additive schema_version 2.1 marker: only policy v2 carrying the numeric
  // marker 2.1 validates and renders the v2.1-only sections, and the marker
  // commits the artifact to carrying every v2.1 section; approved artifacts
  // without it keep validating under legacy rules forever. Destination stays
  // in Work Item goals, so the scope context carries no destination field.
  schema_version?: number;
  implementation_decisions?: Array<{ decision: string; rationale: string; alternatives_considered: string[] }>;
  adr_candidates?: Array<{ context: string; choice: string; reason: string }>;
  excluded_keys?: string[];
  // RRI-owned scope context rows mirrored from the approved RRI report shapes.
  deferrals?: Array<{ question: string; resolution: string }>;
  not_yet_specified?: Array<{ uncertainty: string; graduation_path: string }>;
  out_of_scope?: Array<{ exclusion: string; reason: string }>;
}
const required = (value: unknown, name: string): string => { if (typeof value !== "string" || !value.trim()) throw new Error(`Blueprint ${name} must be a non-empty string`); return value; };
// Marker-gated v2.1 section shape: arrays stay arrays and every object field
// must be a non-empty string, so malformed content fails before rendering.
const v21StringRows = (rows: unknown, name: string, fields: string[]): void => {
  if (rows === undefined) return;
  if (!Array.isArray(rows)) throw new Error(`Blueprint policy v2.1 ${name} must be an array`);
  for (const row of rows) {
    if (!row || typeof row !== "object" || Array.isArray(row)) throw new Error(`Blueprint policy v2.1 ${name} rows must be objects`);
    for (const field of fields) required((row as Record<string, unknown>)[field], `policy v2.1 ${name}.${field}`);
  }
};
const isV21 = (r: Partial<BlueprintReport>): boolean => r.decomposition_policy_version === 2 && r.schema_version === 2.1;
export function parseBlueprintReportJson(content: string): BlueprintReport {
  const r = JSON.parse(content) as Partial<BlueprintReport>;
  for (const key of ["project", "nature", "date"] as const) required(r.project_info?.[key], `project_info.${key}`);
  for (const key of ["primary_goal", "target_audience", "key_message"] as const) required(r.goals?.[key], `goals.${key}`);
  if (!r.architecture?.building_blocks?.length) throw new Error("Blueprint architecture.building_blocks is required");
  required(r.architecture.connection_summary, "architecture.connection_summary"); required(r.architecture.data_flow, "architecture.data_flow");
  if (!r.tech_stack?.length || !r.file_structure?.length || !r.rri_requirements_matrix?.length) throw new Error("Blueprint requires tech_stack, file_structure, and rri_requirements_matrix");
  for (const row of r.rri_requirements_matrix) if (!row.blueprint_section || !row.requirements?.length || !row.source_questions?.length) throw new Error("Blueprint RRI matrix rows are incomplete");
  if ((r.decomposition_policy_version ?? 1) > 2) throw new Error(`Blueprint decomposition_policy_version ${r.decomposition_policy_version} is unsupported`);
  if (r.decomposition_policy_version === 2) {
    // The 2.1 marker is a shape commitment: anything that is not the numeric
    // marker 2.1 (e.g. the string "2.1") is rejected instead of silently
    // degrading the artifact to legacy v2 and dropping its v2.1 sections.
    if (r.schema_version !== undefined && r.schema_version !== 2.1) throw new Error(`Blueprint policy v2.1 schema_version must be the numeric marker 2.1, got ${JSON.stringify(r.schema_version)}`);
    if (isV21(r)) {
      // Required v2.1 section presence: a 2.1-marked artifact must carry every
      // v2.1 section in shape, with implementation_decisions non-empty.
      if (!Array.isArray(r.implementation_decisions) || !r.implementation_decisions.length) throw new Error("Blueprint policy v2.1 implementation_decisions must be a non-empty array");
      for (const row of r.implementation_decisions) {
        if (!row || typeof row !== "object" || Array.isArray(row)) throw new Error("Blueprint policy v2.1 implementation_decisions rows must be objects");
        required(row.decision, "policy v2.1 implementation_decisions.decision");
        required(row.rationale, "policy v2.1 implementation_decisions.rationale");
        const alternatives = row.alternatives_considered;
        if (!Array.isArray(alternatives) || !alternatives.length || alternatives.some((a) => typeof a !== "string" || !a.trim())) throw new Error("Blueprint policy v2.1 implementation_decisions.alternatives_considered must be a non-empty array of non-empty strings");
      }
      for (const name of ["adr_candidates", "excluded_keys", "deferrals", "not_yet_specified", "out_of_scope"] as const) {
        if (!Array.isArray(r[name])) throw new Error(`Blueprint policy v2.1 ${name} must be an array`);
      }
      for (const key of r.excluded_keys!) if (typeof key !== "string" || !key.trim()) throw new Error("Blueprint policy v2.1 excluded_keys require non-empty keys");
      v21StringRows(r.adr_candidates, "adr_candidates", ["context", "choice", "reason"]);
      v21StringRows(r.deferrals, "deferrals", ["question", "resolution"]);
      v21StringRows(r.not_yet_specified, "not_yet_specified", ["uncertainty", "graduation_path"]);
      v21StringRows(r.out_of_scope, "out_of_scope", ["exclusion", "reason"]);
    }
    const seams = r.verification_seams || [];
    if (!seams.length) throw new Error("Blueprint policy v2 requires at least one verification seam");
    const seen = new Set<string>();
    for (const seam of seams) {
      if (!seam.id || seen.has(seam.id)) throw new Error("Blueprint verification seams require unique non-empty ids");
      if (!String(seam.surface || "").trim() || !String(seam.isolates || "").trim()) throw new Error(`Blueprint verification seam ${seam.id} requires surface and isolates`);
      seen.add(seam.id);
    }
    return r as BlueprintReport;
  }
  if (!r.task_decomposition_preview?.tasks?.length || r.task_decomposition_preview.estimated_tasks !== r.task_decomposition_preview.tasks.length || r.task_decomposition_preview.estimated_effort_minutes < 1) throw new Error("Blueprint task decomposition preview is invalid");
  return r as BlueprintReport;
}
const cell = (value: string) => value.replaceAll("|", "\\|").replaceAll("\n", " ");
export function renderBlueprintReportMarkdown(r: BlueprintReport): string {
  const design = r.design_system ? ["### DESIGN SYSTEM", "#### Colors", `Primary: ${r.design_system.colors.primary} | Secondary: ${r.design_system.colors.secondary} | Accent: ${r.design_system.colors.accent}`, "#### Typography", `Headings: ${r.design_system.typography.headings} | Body: ${r.design_system.typography.body}`, ""] : [];
  const decomposition = r.decomposition_policy_version === 2
    ? ["### VERIFICATION SEAMS", "| Seam | Surface | Isolates | Prior Art |", "|------|---------|----------|-----------|", ...r.verification_seams!.map((x) => `| ${cell(x.id)} | ${cell(x.surface)} | ${cell(x.isolates)} | ${x.prior_art ? cell(x.prior_art) : "—"} |`)]
    : ["### TASK DECOMPOSITION PREVIEW", `Estimated Tasks: ${r.task_decomposition_preview!.estimated_tasks}`, ...r.task_decomposition_preview!.tasks.map((x, i) => `${i === r.task_decomposition_preview!.tasks.length - 1 ? "└──" : "├──"} ${cell(x.tip_id)}: ${cell(x.title)} — ${cell(x.goal)}`), `Estimated Effort: ${r.task_decomposition_preview!.estimated_effort_minutes} min Claude Code time`];
  const seamCheckpoint = r.decomposition_policy_version === 2 ? "- [ ] Verification seams isolate every requirement" : "- [ ] Task decomposition is reasonable";
  // v2.1-only projections: rendered exclusively under the explicit 2.1 marker,
  // and only for sections the artifact actually carries, so legacy policy-v2
  // rendering stays byte-identical. No destination section is projected.
  const v21 = isV21(r);
  const decisionSections = v21 && r.implementation_decisions?.length ? ["### IMPLEMENTATION DECISIONS", "| Decision | Rationale | Alternatives Considered |", "|----------|-----------|--------------------------|", ...r.implementation_decisions.map((x) => `| ${cell(x.decision)} | ${cell(x.rationale)} | ${x.alternatives_considered.map(cell).join(", ")} |`), ""] : [];
  const adrSections = v21 && r.adr_candidates?.length ? ["### ADR CANDIDATES", "| Context | Choice | Reason |", "|---------|--------|--------|", ...r.adr_candidates.map((x) => `| ${cell(x.context)} | ${cell(x.choice)} | ${cell(x.reason)} |`), ""] : [];
  const scopeSections = v21 && (r.deferrals?.length || r.not_yet_specified?.length || r.out_of_scope?.length)
    ? ["### SCOPE CONTEXT",
      ...(r.deferrals?.length ? ["**Deferrals:**", ...r.deferrals.map((x) => `- **${cell(x.question)}:** ${cell(x.resolution)}`), ""] : []),
      ...(r.not_yet_specified?.length ? ["**Not Yet Specified:**", ...r.not_yet_specified.map((x) => `- **${cell(x.uncertainty)}:** graduation path -> ${cell(x.graduation_path)}`), ""] : []),
      ...(r.out_of_scope?.length ? ["**Out of Scope:**", ...r.out_of_scope.map((x) => `- **${cell(x.exclusion)}:** ${cell(x.reason)}`), ""] : [])]
    : [];
  return [`# BLUEPRINT: ${cell(r.project_info.project)}`, "", "### PROJECT INFO", "| Field | Value |", "|-------|-------|", `| Project | ${cell(r.project_info.project)} |`, `| Nature | ${cell(r.project_info.nature)} |`, `| Date | ${cell(r.project_info.date)} |`, "", "### GOALS", `**Primary Goal:** ${cell(r.goals.primary_goal)}`, `**Target Audience:** ${cell(r.goals.target_audience)}`, `**Key Message:** ${cell(r.goals.key_message)}`, "", "### ARCHITECTURE", r.architecture.building_blocks.map((x) => `- ${cell(x)}`).join("\n"), "", `**Connections:** ${cell(r.architecture.connection_summary)}`, `**Data flow:** ${cell(r.architecture.data_flow)}`, "", ...design, "### TECH STACK", ...r.tech_stack.map((x) => `- **${cell(x.layer)}:** ${cell(x.choice)} — ${cell(x.rationale)}; reuse: ${cell(x.reuse)}`), "", "### FILE STRUCTURE", "```text", ...r.file_structure.map((x) => `${x.path} — ${x.purpose}`), "```", "", "### RRI REQUIREMENTS MATRIX", "| Blueprint Section | Requirements | Source (RRI Q#) |", "|-------------------|-------------|-----------------|", ...r.rri_requirements_matrix.map((x) => `| ${cell(x.blueprint_section)} | ${x.requirements.map(cell).join(", ")} | ${x.source_questions.map(cell).join(", ")} |`), "", ...decisionSections, ...adrSections, ...scopeSections, ...decomposition, "", "### CHECKPOINT", "- [ ] Architecture matches expectations", "- [ ] Design is appropriate (if UI)", "- [ ] Requirements are complete (from RRI)", seamCheckpoint, "- [ ] Nothing important is missing"].join("\n");
}
