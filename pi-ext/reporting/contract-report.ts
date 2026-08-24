export interface ContractReport {
  project_name: string;
  deliverables: Array<{ item: string; details: string; requirements: string[] }>;
  tech_stack: Array<{ layer: string; choice: string; rationale: string }>;
  task_graph_summary: { tip_count: number; estimated_minutes: number };
  not_included: string[];
  obligations: Array<{ id: string; requirement_keys: string[]; behavior: string; acceptance: string }>;
  obligation_schema_version: 2;
}
const required = (v: unknown, name: string) => { if (typeof v !== "string" || !v.trim()) throw new Error(`Contract ${name} is required`); return v; };
export function parseContractReportJson(content: string): ContractReport {
  const r = JSON.parse(content) as Partial<ContractReport>;
  required(r.project_name, "project_name");
  if (!r.deliverables?.length || r.deliverables.some((x) => !x.item || !x.details || !x.requirements?.length)) throw new Error("Contract deliverables are incomplete");
  if (!r.tech_stack?.length || r.tech_stack.some((x) => !x.layer || !x.choice || !x.rationale)) throw new Error("Contract tech_stack is incomplete");
  if (!r.task_graph_summary || r.task_graph_summary.tip_count < 1 || r.task_graph_summary.estimated_minutes < 1) throw new Error("Contract task_graph_summary is invalid");
  if (!r.not_included?.length) throw new Error("Contract not_included is required");
  if (r.obligation_schema_version !== 2) throw new Error("Contract obligation_schema_version must be 2");
  if (!r.obligations?.length || r.obligations.some((x) => !x.id || !x.requirement_keys?.length || !x.behavior || !x.acceptance)) throw new Error("Contract obligations are incomplete");
  return r as ContractReport;
}
const cell = (v: string) => v.replaceAll("|", "\\|").replaceAll("\n", " ");
export function renderContractReportMarkdown(r: ContractReport): string {
  return [`# CONTRACT: ${cell(r.project_name)}`, "", "## DELIVERABLES", "| # | Item | Details | Requirements |", "|---|------|---------|--------------|", ...r.deliverables.map((x, i) => `| ${i + 1} | ${cell(x.item)} | ${cell(x.details)} | ${x.requirements.map(cell).join(", ")} |`), "", "## OBLIGATIONS", "| ID | Requirements | Behavior | Acceptance |", "|----|--------------|----------|------------|", ...r.obligations.map((x) => `| ${cell(x.id)} | ${x.requirement_keys.map(cell).join(", ")} | ${cell(x.behavior)} | ${cell(x.acceptance)} |`), "", "## TECH STACK", ...r.tech_stack.map((x) => `- **${cell(x.layer)}:** ${cell(x.choice)} — ${cell(x.rationale)}`), "", "## TASK GRAPH SUMMARY", `${r.task_graph_summary.tip_count} TIPs, estimated ${r.task_graph_summary.estimated_minutes} minutes`, "", "## NOT INCLUDED", ...r.not_included.map((x) => `- ${cell(x)}`), "", "## CONFIRM", "Reply `CONFIRM` to receive Task Graph."].join("\n");
}
