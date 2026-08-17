import assert from "node:assert/strict";
import test from "node:test";
import { parseBlueprintReportJson, renderBlueprintReportMarkdown } from "./blueprint-report.ts";

const report = { project_info: { project: "Task System", nature: "CLI + pipeline + team", date: "2026-08-17" }, goals: { primary_goal: "Reliable workflow", target_audience: "Owner and agents", key_message: "Every transition is durable" }, architecture: { building_blocks: ["CLI", "Scheduler", "SQLite"], connection_summary: "CLI drives scheduler state", data_flow: "Inputs -> CLI -> SQLite" }, tech_stack: [{ layer: "Backend", choice: "Go", rationale: "Existing", reuse: "go-pic" }], file_structure: [{ path: "go-pic/cmd/pic", purpose: "Workflow backend" }], rri_requirements_matrix: [{ blueprint_section: "Lifecycle", requirements: ["REQ-001"], source_questions: ["Q1"] }], task_decomposition_preview: { estimated_tasks: 1, tasks: [{ tip_id: "TIP-001", title: "Lifecycle", goal: "Enforce transitions" }], estimated_effort_minutes: 30 } };

test("Blueprint JSON renders the owner checkpoint", () => {
  const markdown = renderBlueprintReportMarkdown(parseBlueprintReportJson(JSON.stringify(report)));
  for (const heading of ["PROJECT INFO", "GOALS", "ARCHITECTURE", "TECH STACK", "FILE STRUCTURE", "RRI REQUIREMENTS MATRIX", "TASK DECOMPOSITION PREVIEW", "CHECKPOINT"]) assert.match(markdown, new RegExp(heading));
  assert.match(markdown, /- \[ \] Architecture matches expectations/);
});

test("Blueprint rejects task count drift", () => {
  assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...report, task_decomposition_preview: { ...report.task_decomposition_preview, estimated_tasks: 2 } })), /task decomposition preview/);
});
