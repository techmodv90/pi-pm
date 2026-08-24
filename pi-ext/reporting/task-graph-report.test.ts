import assert from "node:assert/strict";
import test from "node:test";
import { parseTaskGraphReportJson, renderTaskGraphReportMarkdown } from "./task-graph-report.ts";

const graph = { version: 3, execution_policy: "parallel_allowed", nodes: [
  { key: "T01", name: "Core", goal: "Implement core", requirement_keys: ["REQ-1"], depends_on: [], priority: "high", module: "core", estimated_effort_minutes: 30, files: ["src/core.ts"], verification: [{ command: "npm test" }], skillFamilies: [], obligation_keys: ["OBL-1"], provides: ["OBL-1"], consumes: [], evidence_for: [] },
  { key: "VERIFY", type: "gate", name: "Verify", goal: "Verify delivery", requirement_keys: ["REQ-1"], depends_on: ["T01"], priority: "high", module: "integration", estimated_effort_minutes: 10, files: [], verification: [{ command: "npm test" }], skillFamilies: [], obligation_keys: ["OBL-1"], provides: [], consumes: [], evidence_for: ["OBL-1"] },
] };

test("Task Graph validates dependencies and renders owner review content", () => {
  const markdown = renderTaskGraphReportMarkdown(parseTaskGraphReportJson(JSON.stringify(graph)));
  assert.match(markdown, /# TASK GRAPH/);
  assert.match(markdown, /```mermaid[\s\S]*N0 --> N1[\s\S]*```/);
  assert.match(markdown, /PARALLEL EXECUTION WAVES/);
  assert.match(markdown, /Critical path:.*`T01` -> `VERIFY`/);
  assert.match(markdown, /VERIFY.*T01/);
  assert.match(markdown, /src\/core\.ts/);
});

test("Task Graph rejects invalid dependencies", () => {
  const invalid = structuredClone(graph);
  invalid.nodes[1].depends_on = ["MISSING"];
  assert.throws(() => parseTaskGraphReportJson(JSON.stringify(invalid)), /invalid dependency MISSING/);
});

test("Task Graph rejects nodes bound to more than two requirements", () => {
  const invalid = structuredClone(graph);
  invalid.nodes[0].requirement_keys = ["REQ-1", "REQ-2", "REQ-3"];
  assert.throws(() => parseTaskGraphReportJson(JSON.stringify(invalid)), /more than two requirement_keys/);
});

test("Task Graph permits two requirements on one bite-sized node", () => {
  const valid = structuredClone(graph);
  valid.nodes[0].requirement_keys = ["REQ-1", "REQ-2"];
  assert.doesNotThrow(() => parseTaskGraphReportJson(JSON.stringify(valid)));
});