export interface RriReport {
  project_name: string;
  generated: string;
  /** Marker-gated frontier schema: reports carrying rri_policy_version 2 require the frontier open_questions fields, pre-marker reports stay legacy-valid. */
  rri_policy_version?: number;
  requirements_matrix: Array<{ req_id: string; requirement: string; source: string; priority: string; persona: string }>;
  auto_answered: Array<{ topic: string; details: string; resolution: string }>;
  decisions_log: Array<{ decision: string; options_considered: string; chosen: string; rationale: string }>;
  open_questions: Array<{
    id: string;
    question: string;
    status?: string;
    priority?: string;
    mode?: string;
    blocks?: boolean;
    resolution?: { answer: string; source: string };
  }>;
  /** Marked scope sections: required on rri_policy_version 2 reports, absent on legacy reports. */
  not_yet_specified?: Array<{ uncertainty: string; graduation_path: string }>;
  out_of_scope?: Array<{ exclusion: string; reason: string }>;
}

const RRI_QUESTION_STATUSES = ["open", "resolved", "deferred"];
const RRI_QUESTION_PRIORITIES = ["P0", "P1", "P2", "P3"];
const RRI_QUESTION_MODES = ["afk", "hitl"];

function isMarkedRriReport(report: Partial<RriReport>): boolean {
  const marker = report.rri_policy_version;
  // Mirrors Go int unmarshalling: null is a no-op that stays legacy, while any
  // non-number or fractional marker fails validation, never coercing into the
  // marker-gated or legacy branch.
  if (marker !== undefined && marker !== null) {
    if (typeof marker !== "number" || !Number.isInteger(marker)) {
      throw new Error(`RRI rri_policy_version must be an integer`);
    }
  }
  return (marker ?? 1) >= 2;
}

function cell(value: string): string { return value.replaceAll("|", "\\|").replaceAll("\n", " "); }

// Enforces the same marker-gated open_questions contract as the Go validator:
// marked rows require status, priority, mode, and blocks, and a resolution
// answer plus source when resolved or deferred; legacy rows stay unchanged.
function validateRriOpenQuestions(report: Partial<RriReport>): void {
  const marked = isMarkedRriReport(report);
  for (const row of report.open_questions ?? []) {
    if (!row || !row.id || !row.question) throw new Error("RRI report contains an incomplete row");
    if (!marked) continue;
    if (!row.status) throw new Error(`RRI open_questions row ${row.id} requires status`);
    if (!RRI_QUESTION_STATUSES.includes(row.status)) throw new Error(`RRI open_questions row ${row.id} has invalid status ${row.status}`);
    if (!row.priority) throw new Error(`RRI open_questions row ${row.id} requires priority`);
    if (!RRI_QUESTION_PRIORITIES.includes(row.priority)) throw new Error(`RRI open_questions row ${row.id} has invalid priority ${row.priority}`);
    if (!row.mode) throw new Error(`RRI open_questions row ${row.id} requires mode`);
    if (!RRI_QUESTION_MODES.includes(row.mode)) throw new Error(`RRI open_questions row ${row.id} has invalid mode ${row.mode}`);
    if (typeof row.blocks !== "boolean") throw new Error(`RRI open_questions row ${row.id} requires blocks`);
    // Any non-null resolution value must be a well-typed object regardless of
    // status: Go rejects falsy scalars like "", 0, and false at unmarshal time
    // since they cannot decode into rriResolution, and the renderer would crash
    // on non-string answer or source fields.
    if (row.resolution !== undefined && row.resolution !== null) {
      if (typeof row.resolution !== "object" || typeof row.resolution.answer !== "string" || !row.resolution.answer || typeof row.resolution.source !== "string" || !row.resolution.source) {
        throw new Error(`RRI open_questions row ${row.id} requires resolution answer and source to be non-empty strings`);
      }
    }
    // Mirrors Go unmarshalling: resolved and deferred rows must carry a resolution.
    if (row.status !== "open" && (row.resolution === undefined || row.resolution === null)) {
      throw new Error(`RRI open_questions row ${row.id} requires resolution with answer and source when status is resolved or deferred`);
    }
  }
}

// Marker-gated scope sections mirror the Go validator: marked reports require
// both sections, legacy reports stay valid without them. null behaves like Go
// pointer/slice unmarshalling and counts as a missing section.
function validateRriScopeSections(report: Partial<RriReport>): void {
  if (!isMarkedRriReport(report)) return;
  if (!Array.isArray(report.not_yet_specified)) throw new Error("Marked RRI report requires the not_yet_specified section");
  if (!Array.isArray(report.out_of_scope)) throw new Error("Marked RRI report requires the out_of_scope section");
  for (const row of report.not_yet_specified) {
    if (!row || typeof row.uncertainty !== "string" || !row.uncertainty || typeof row.graduation_path !== "string" || !row.graduation_path) {
      throw new Error("RRI not_yet_specified rows require uncertainty and graduation_path to be non-empty strings");
    }
  }
  for (const row of report.out_of_scope) {
    if (!row || typeof row.exclusion !== "string" || !row.exclusion || typeof row.reason !== "string" || !row.reason) {
      throw new Error("RRI out_of_scope rows require exclusion and reason to be non-empty strings");
    }
  }
}

// Marker-gated required-array parity with the Go validator (OB-F1-7): marked
// reports must carry every core array, and the failure names the missing
// section exactly like the Go validator; legacy reports keep their pre-marker
// tolerance, with absent arrays defaulting to empty like Go nil slices.
const RRI_CORE_SECTIONS = ["requirements_matrix", "auto_answered", "decisions_log", "open_questions"] as const;

function validateRriCoreSections(report: Partial<RriReport>, marked: boolean): void {
  const record = report as Record<string, unknown>;
  for (const section of RRI_CORE_SECTIONS) {
    const value = record[section];
    if (value === undefined || value === null) {
      if (marked) throw new Error(`marked RRI report is missing the ${section} section`);
      record[section] = [];
      continue;
    }
    if (!Array.isArray(value)) throw new Error("RRI report is missing one of the required sections");
  }
}

export function parseRriReportJson(content: string): RriReport {
  const report = JSON.parse(content) as Partial<RriReport>;
  const marked = isMarkedRriReport(report);
  if ((report.rri_policy_version ?? 1) > 2) throw new Error(`RRI rri_policy_version ${report.rri_policy_version} is unsupported`);
  if (!report.project_name || !report.generated) {
    throw new Error("RRI report is missing one of the required sections");
  }
  validateRriCoreSections(report, marked);
  const required = (report.requirements_matrix as RriReport["requirements_matrix"]).every((row) => row && row.req_id && row.requirement && row.source && row.priority && row.persona);
  const autoAnswered = (report.auto_answered as RriReport["auto_answered"]).every((row) => row && row.topic && row.details && row.resolution);
  const decisions = (report.decisions_log as RriReport["decisions_log"]).every((row) => row && row.decision && row.options_considered && row.chosen && row.rationale);
  if (!required || !autoAnswered || !decisions) throw new Error("RRI report contains an incomplete row");
  validateRriScopeSections(report);
  validateRriOpenQuestions(report);
  return report as RriReport;
}

export function renderRriReportMarkdown(report: RriReport): string {
  const requirements = report.requirements_matrix.length === 0
    ? "| | | | | |\n|-|-|-|-|-|"
    : report.requirements_matrix.map((row) => `| ${cell(row.req_id)} | ${cell(row.requirement)} | ${cell(row.source)} | ${cell(row.priority)} | ${cell(row.persona)} |`).join("\n");
  const autoAnswered = report.auto_answered.length === 0 ? "- None" : report.auto_answered.map((row) => `- **${cell(row.topic)}:** ${cell(row.details)} -> ${cell(row.resolution)}`).join("\n");
  const decisions = report.decisions_log.length === 0 ? "| | | | |\n|-|-|-|-|" : report.decisions_log.map((row) => `| ${cell(row.decision)} | ${cell(row.options_considered)} | ${cell(row.chosen)} | ${cell(row.rationale)} |`).join("\n");
  const marked = isMarkedRriReport(report);
  const questions = report.open_questions.length === 0 ? "- None" : report.open_questions.map((row) => {
    const base = `- **${cell(row.id)}:** ${cell(row.question)}`;
    if (!marked) return base;
    const parts = [`status: ${row.status}`, `priority: ${row.priority}`, `mode: ${row.mode}`, `blocks: ${row.blocks}`];
    if (row.resolution) parts.push(`resolution: ${cell(row.resolution.answer)} (source: ${cell(row.resolution.source)})`);
    return `${base} (${parts.join(", ")})`;
  }).join("\n");
  const notYetSpecified = report.not_yet_specified?.length
    ? report.not_yet_specified.map((row) => `- **${cell(row.uncertainty)}:** graduation path -> ${cell(row.graduation_path)}`).join("\n")
    : "- None";
  const outOfScope = report.out_of_scope?.length
    ? report.out_of_scope.map((row) => `- **${cell(row.exclusion)}:** ${cell(row.reason)}`).join("\n")
    : "- None";
  return [
    `# RRI REPORT: ${report.project_name}`, `Generated: ${report.generated}`, "", "## REQUIREMENTS MATRIX", "",
    "| REQ-ID | Requirement | Source | Priority | Persona |", "|--------|-------------|--------|----------|---------|", requirements,
    "", "## AUTO-ANSWERED (from Scan)", autoAnswered, "", "## DECISIONS LOG", "",
    "| Decision | Options Considered | Chosen | Rationale |", "|----------|--------------------|--------|-----------|", decisions,
    "", "## OPEN QUESTIONS", questions,
    "", "## NOT YET SPECIFIED", notYetSpecified,
    "", "## OUT OF SCOPE", outOfScope,
  ].join("\n");
}
