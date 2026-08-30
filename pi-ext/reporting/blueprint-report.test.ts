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

const v2Report = { decomposition_policy_version: 2, project_info: { project: "Task System", nature: "CLI + pipeline + team", date: "2026-08-29" }, goals: { primary_goal: "Reliable workflow", target_audience: "Owner and agents", key_message: "Every transition is durable" }, architecture: { building_blocks: ["CLI", "Scheduler", "SQLite"], connection_summary: "CLI drives scheduler state", data_flow: "Inputs -> CLI -> SQLite" }, tech_stack: [{ layer: "Backend", choice: "Go", rationale: "Existing", reuse: "go-pic" }], file_structure: [{ path: "go-pic/cmd/pic", purpose: "Workflow backend" }], rri_requirements_matrix: [{ blueprint_section: "Lifecycle", requirements: ["REQ-001"], source_questions: ["Q1"] }], verification_seams: [{ id: "cli-materialize", surface: "pic materialize against a temporary SQLite database", isolates: "materialization atomicity", prior_art: "TestWorkItemGraphMaterialization" }, { id: "go-tests", surface: "go test ./...", isolates: "package-level behavior" }] };

test("Blueprint policy v2 renders verification seams and the seam checkpoint", () => {
  const markdown = renderBlueprintReportMarkdown(parseBlueprintReportJson(JSON.stringify(v2Report)));
  for (const heading of ["VERIFICATION SEAMS", "CHECKPOINT"]) assert.match(markdown, new RegExp(heading));
  assert.match(markdown, /cli-materialize/);
  assert.match(markdown, /- \[ \] Verification seams isolate every requirement/);
  assert.doesNotMatch(markdown, /TASK DECOMPOSITION PREVIEW/);
});

test("Blueprint policy v2 validation fails closed on seams", () => {
  assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...v2Report, verification_seams: [] })), /at least one verification seam/);
  assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...v2Report, verification_seams: [{ ...v2Report.verification_seams[0], id: "dup" }, { ...v2Report.verification_seams[1], id: "dup" }] })), /unique non-empty ids/);
  assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...v2Report, verification_seams: [{ id: "s", surface: "", isolates: "x" }] })), /surface and isolates/);
  assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...v2Report, decomposition_policy_version: 3 })), /unsupported/);
});
