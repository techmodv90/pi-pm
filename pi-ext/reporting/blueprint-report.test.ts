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

// v2.1 is an additive marker on policy v2, never a new decomposition policy
// version: decision sections and RRI scope context validate and render only
// under the explicit marker, and approved legacy v2 artifacts stay untouched.
const v21Report = { ...v2Report, schema_version: 2.1, implementation_decisions: [{ decision: "Embed the dashboard in the Go binary", rationale: "One static artifact", alternatives_considered: ["Serve from a separate dev server", "Ship a Node sidecar"] }], adr_candidates: [{ context: "Dashboard delivery", choice: "Embedded static build", reason: "No runtime dependency on Node" }], excluded_keys: ["REQ-OUT-1"], deferrals: [{ question: "Export formats", resolution: "Owner deferred formats to contracts" }], not_yet_specified: [{ uncertainty: "Report layout", graduation_path: "Decide at contracts" }], out_of_scope: [{ exclusion: "Team collaboration", reason: "Out of this delivery" }] };

test("Blueprint policy v2.1 parses and renders decision sections and scope context", () => {
  const markdown = renderBlueprintReportMarkdown(parseBlueprintReportJson(JSON.stringify(v21Report)));
  for (const heading of ["VERIFICATION SEAMS", "IMPLEMENTATION DECISIONS", "ADR CANDIDATES", "SCOPE CONTEXT", "CHECKPOINT"]) assert.match(markdown, new RegExp(heading));
  assert.match(markdown, /Embed the dashboard in the Go binary/);
  assert.match(markdown, /Embedded static build/);
  assert.match(markdown, /Deferrals:[\s\S]*Owner deferred formats to contracts/);
  assert.match(markdown, /Not Yet Specified:[\s\S]*graduation path -> Decide at contracts/);
  assert.match(markdown, /Out of Scope:[\s\S]*Out of this delivery/);
  assert.doesNotMatch(markdown, /DESTINATION/);
  assert.doesNotMatch(markdown, /TASK DECOMPOSITION PREVIEW/);
});

test("Blueprint policy v2.1 rejects malformed content with field-specific errors", () => {
  assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...v21Report, implementation_decisions: [{ rationale: "x", alternatives_considered: ["a"] }] })), /implementation_decisions\.decision must be a non-empty string/);
  assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...v21Report, implementation_decisions: [{ decision: "d", rationale: "r", alternatives_considered: [] }] })), /alternatives_considered must be a non-empty array of non-empty strings/);
  assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...v21Report, adr_candidates: [{ context: "c", choice: "ch", reason: "" }] })), /adr_candidates\.reason must be a non-empty string/);
  assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...v21Report, excluded_keys: [""] })), /excluded_keys require non-empty keys/);
  assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...v21Report, not_yet_specified: [{ uncertainty: "u" }] })), /not_yet_specified\.graduation_path must be a non-empty string/);
  assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...v21Report, out_of_scope: "nope" })), /out_of_scope must be an array/);
});

test("Blueprint policy v2.1 marker requires the v2.1 sections", () => {
  // A 2.1-marked artifact must carry every v2.1 section; missing sections are
  // rejected instead of silently rendering no v2.1 projection.
  for (const missing of ["implementation_decisions", "adr_candidates", "excluded_keys", "deferrals", "not_yet_specified", "out_of_scope"] as const) {
    const partial = { ...v21Report };
    delete partial[missing];
    assert.throws(() => parseBlueprintReportJson(JSON.stringify(partial)), new RegExp(`v2\\.1 ${missing} must be an?`));
  }
  assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...v21Report, implementation_decisions: [] })), /implementation_decisions must be a non-empty array/);
});

test("Blueprint policy v2.1 marker must be the numeric value 2.1", () => {
  // A non-numeric or wrong-value marker is rejected outright; it must never
  // degrade the artifact to legacy v2 and silently drop its v2.1 sections.
  for (const marker of ["2.1", 2, 3, "v2.1", null]) {
    assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...v21Report, schema_version: marker })), /schema_version must be the numeric marker 2.1/);
  }
});

test("Blueprint legacy v2 stays valid forever and never renders v2.1-only sections", () => {
  const legacy = renderBlueprintReportMarkdown(parseBlueprintReportJson(JSON.stringify(v2Report)));
  assert.doesNotMatch(legacy, /IMPLEMENTATION DECISIONS|ADR CANDIDATES|SCOPE CONTEXT/);
  assert.match(legacy, /VERIFICATION SEAMS/);
});

test("Blueprint policy v2 carries no user stories and no second testing section", () => {
  for (const extra of [{ user_stories: [{ story: "As an owner" }] }, { testing: [{ case: "t" }] }]) {
    assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...v21Report, ...extra })), /no user stories and no second testing section/);
    assert.throws(() => parseBlueprintReportJson(JSON.stringify({ ...v2Report, ...extra })), /no user stories and no second testing section/);
  }
});
