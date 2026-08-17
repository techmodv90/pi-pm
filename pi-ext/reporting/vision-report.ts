export interface VisionReport {
  project_name: string;
  nature: { interface: string; lifecycle: string; scale: string };
  dimensions: { interface: string; data_flow: string; user_model: string; lifecycle: string; scale: string; state: string };
  architecture: { entry_points: string[]; core_modules: string[]; data_layer: string[]; integration_points: string[]; cross_cutting_concerns: string[]; connection_summary: string };
  user_flows: Array<{ user_type: string; entry: string; core_loop: string; edge_cases: string[]; exit: string }>;
  design_direction?: { layout_ascii: string; font_pairing: string; primary_color: string; density: string; motion: string; rationale: string };
  non_ui_direction?: { type: string; decisions: string[] };
  tech_stack: Array<{ layer: string; choice: string; rationale: string; reuse: string }>;
}
const text = (v: unknown, n: string): string => { if (typeof v !== "string" || !v.trim()) throw new Error(`Vision ${n} must be a non-empty string`); return v; };
const list = (v: unknown, n: string): string[] => { if (!Array.isArray(v) || !v.length || v.some((x) => typeof x !== "string" || !x.trim())) throw new Error(`Vision ${n} must be a non-empty string array`); return v; };
export function parseVisionReportJson(content: string): VisionReport {
  const r = JSON.parse(content) as Partial<VisionReport>;
  text(r.project_name, "project_name");
  for (const k of ["interface", "lifecycle", "scale"] as const) text(r.nature?.[k], `nature.${k}`);
  for (const k of ["interface", "data_flow", "user_model", "lifecycle", "scale", "state"] as const) text(r.dimensions?.[k], `dimensions.${k}`);
  for (const k of ["entry_points", "core_modules", "data_layer", "integration_points", "cross_cutting_concerns"] as const) list(r.architecture?.[k], `architecture.${k}`);
  text(r.architecture?.connection_summary, "architecture.connection_summary");
  if (!Array.isArray(r.user_flows) || !r.user_flows.length) throw new Error("Vision user_flows must be a non-empty array");
  for (const f of r.user_flows) { text(f.user_type, "user_flows.user_type"); text(f.entry, "user_flows.entry"); text(f.core_loop, "user_flows.core_loop"); list(f.edge_cases, "user_flows.edge_cases"); text(f.exit, "user_flows.exit"); }
  if (r.design_direction) for (const k of ["layout_ascii", "font_pairing", "primary_color", "density", "motion", "rationale"] as const) text(r.design_direction[k], `design_direction.${k}`);
  if (r.non_ui_direction) { text(r.non_ui_direction.type, "non_ui_direction.type"); list(r.non_ui_direction.decisions, "non_ui_direction.decisions"); }
  if (!r.design_direction && !r.non_ui_direction) throw new Error("Vision requires design_direction or non_ui_direction");
  if (!Array.isArray(r.tech_stack) || !r.tech_stack.length) throw new Error("Vision tech_stack must be a non-empty array");
  for (const s of r.tech_stack) { text(s.layer, "tech_stack.layer"); text(s.choice, "tech_stack.choice"); text(s.rationale, "tech_stack.rationale"); text(s.reuse, "tech_stack.reuse"); }
  return r as VisionReport;
}
const cell = (v: string) => v.replaceAll("|", "\\|").replaceAll("\n", " ");
const bullets = (xs: string[]) => xs.map((x) => `- ${cell(x)}`).join("\n");
export function renderVisionReportMarkdown(r: VisionReport): string {
  const flows = r.user_flows.map((f) => [`### ${cell(f.user_type)}`, `- Entry: ${cell(f.entry)}`, `- Core loop: ${cell(f.core_loop)}`, `- Edge cases: ${f.edge_cases.map(cell).join(", ")}`, `- Exit: ${cell(f.exit)}`].join("\n")).join("\n\n");
  const direction = r.design_direction ? ["## DESIGN DIRECTION", "", "```", r.design_direction.layout_ascii, "```", `- Font pairing: ${cell(r.design_direction.font_pairing)}`, `- Primary color: ${cell(r.design_direction.primary_color)}`, `- Density: ${cell(r.design_direction.density)}`, `- Motion: ${cell(r.design_direction.motion)}`, `- Rationale: ${cell(r.design_direction.rationale)}`].join("\n") : ["## NON-UI DIRECTION", "", `**${cell(r.non_ui_direction!.type)}**`, "", bullets(r.non_ui_direction!.decisions)].join("\n");
  const sections = (["entry_points", "core_modules", "data_layer", "integration_points", "cross_cutting_concerns"] as const).flatMap((k) => [`### ${k.replaceAll("_", " ")}`, bullets(r.architecture[k]), ""]);
  return [`# VISION: ${cell(r.project_name)}`, "", `**Nature:** ${cell(r.nature.interface)} + ${cell(r.nature.lifecycle)} + ${cell(r.nature.scale)}`, "", "## DIMENSIONS", "", ...Object.entries(r.dimensions).map(([k, v]) => `- **${k}:** ${cell(v)}`), "", "## ARCHITECTURE", "", `**How it connects:** ${cell(r.architecture.connection_summary)}`, "", ...sections, "## USER FLOWS", "", flows, "", direction, "", "## TECH STACK", "", "| Layer | Choice | Rationale | Reuse |", "|---|---|---|---|", ...r.tech_stack.map((s) => `| ${cell(s.layer)} | ${cell(s.choice)} | ${cell(s.rationale)} | ${cell(s.reuse)} |`)].join("\n");
}