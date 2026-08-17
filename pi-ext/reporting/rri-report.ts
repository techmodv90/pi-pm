export interface RriReport {
  project_name: string;
  generated: string;
  requirements_matrix: Array<{ req_id: string; requirement: string; source: string; priority: string; persona: string }>;
  auto_answered: Array<{ topic: string; details: string; resolution: string }>;
  decisions_log: Array<{ decision: string; options_considered: string; chosen: string; rationale: string }>;
  open_questions: Array<{ id: string; question: string }>;
}

function cell(value: string): string { return value.replaceAll("|", "\\|").replaceAll("\n", " "); }

export function parseRriReportJson(content: string): RriReport {
  const report = JSON.parse(content) as Partial<RriReport>;
  if (!report.project_name || !report.generated || !Array.isArray(report.requirements_matrix) || !Array.isArray(report.auto_answered) || !Array.isArray(report.decisions_log) || !Array.isArray(report.open_questions)) {
    throw new Error("RRI report JSON is missing one of the required sections");
  }
  return report as RriReport;
}

export function renderRriReportMarkdown(report: RriReport): string {
  const requirements = report.requirements_matrix.length === 0
    ? "| | | | | |\n|-|-|-|-|-|"
    : report.requirements_matrix.map((row) => `| ${cell(row.req_id)} | ${cell(row.requirement)} | ${cell(row.source)} | ${cell(row.priority)} | ${cell(row.persona)} |`).join("\n");
  const autoAnswered = report.auto_answered.length === 0 ? "- None" : report.auto_answered.map((row) => `- **${cell(row.topic)}:** ${cell(row.details)} -> ${cell(row.resolution)}`).join("\n");
  const decisions = report.decisions_log.length === 0 ? "| | | | |\n|-|-|-|-|" : report.decisions_log.map((row) => `| ${cell(row.decision)} | ${cell(row.options_considered)} | ${cell(row.chosen)} | ${cell(row.rationale)} |`).join("\n");
  const questions = report.open_questions.length === 0 ? "- None" : report.open_questions.map((row) => `- **${cell(row.id)}:** ${cell(row.question)}`).join("\n");
  return [
    `# RRI REPORT: ${report.project_name}`, `Generated: ${report.generated}`, "", "## REQUIREMENTS MATRIX", "",
    "| REQ-ID | Requirement | Source | Priority | Persona |", "|--------|-------------|--------|----------|---------|", requirements,
    "", "## AUTO-ANSWERED (from Scan)", autoAnswered, "", "## DECISIONS LOG", "",
    "| Decision | Options Considered | Chosen | Rationale |", "|----------|--------------------|--------|-----------|", decisions,
    "", "## OPEN QUESTIONS", questions,
  ].join("\n");
}