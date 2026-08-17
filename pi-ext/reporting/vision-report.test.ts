import assert from "node:assert/strict";
import test from "node:test";
import { parseVisionReportJson, renderVisionReportMarkdown } from "./vision-report.ts";

const report = {
  project_name: "Task System",
  nature: { interface: "Web UI + CLI", lifecycle: "Pipeline", scale: "Team" },
  dimensions: { interface: "Web UI", data_flow: "SQLite and files", user_model: "Owner and agents", lifecycle: "Pipeline", scale: "Team", state: "Persistent DB" },
  architecture: { entry_points: ["CLI"], core_modules: ["Scheduler"], data_layer: ["SQLite"], integration_points: ["Pi"], cross_cutting_concerns: ["Audit"], connection_summary: "CLI enters scheduler, which persists workflow state." },
  user_flows: [{ user_type: "Owner", entry: "Open Work Item", core_loop: "Review stage output", edge_cases: ["Rejected artifact"], exit: "Approve or reset" }],
  design_direction: { layout_ascii: "[nav][content]", font_pairing: "System sans", primary_color: "#2563EB", density: "Compact", motion: "Subtle", rationale: "Operational scanning" },
  tech_stack: [{ layer: "Runtime", choice: "Go + TypeScript", rationale: "Existing stack", reuse: "Reuse current implementation" }],
};

test("Vision JSON renders the required owner-facing sections", () => {
  const markdown = renderVisionReportMarkdown(parseVisionReportJson(JSON.stringify(report)));
  assert.match(markdown, /# VISION: Task System/);
  assert.match(markdown, /## DIMENSIONS/);
  assert.match(markdown, /## ARCHITECTURE/);
  assert.match(markdown, /## USER FLOWS/);
  assert.match(markdown, /## DESIGN DIRECTION/);
  assert.match(markdown, /## TECH STACK/);
});

test("Vision validation rejects reports without project dimensions", () => {
  const invalid = structuredClone(report) as typeof report;
  delete (invalid.dimensions as Partial<typeof report.dimensions>).state;
  assert.throws(() => parseVisionReportJson(JSON.stringify(invalid)), /dimensions.state/);
});
