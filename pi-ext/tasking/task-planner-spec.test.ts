import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";

const source = readFileSync(new URL("../agents/task-planner.md", import.meta.url), "utf8");

test("Blueprint continuation requires Contractor checkpoint before owner approval", () => {
  const source = readFileSync(new URL("./work-item-prompts.ts", import.meta.url), "utf8");
  assert.match(source, /load_blueprint_draft/);
  assert.match(source, /draft_id/);
});

test("task-planner Blueprint handoff requires Contractor checkpoint before owner approval", () => {
  assert.match(source, /save_blueprint_draft/);
  assert.match(source, /review_blueprint_checkpoint/);
  assert.match(source, /approve_blueprint_draft/);
});

test("task-planner Blueprint contract is artifact-first", () => {
  assert.match(source, /<blueprint_handoff>/);
  assert.match(source, /schema version 2/);
  assert.match(source, /load_planning_artifact/);
  assert.match(source, /save_blueprint_draft.*stage="blueprint"/s);
  assert.match(source, /temporary/);
  assert.match(source, /Do not call `save_design`/);
  assert.match(source, /Never return a Markdown planning report instead of saving the temporary draft/);
  assert.doesNotMatch(source, /\*\*Design saved:\*\*/);
  assert.doesNotMatch(source, /\*\*Task Plan nodes:\*\*/);
});

test("task-planner Task Graph contract uses XML input and JSON persistence", () => {
  assert.match(source, /<task_graph_handoff>/);
  assert.match(source, /stage="task_graph"/);
  assert.match(source, /schema version 3/);
});

test("task-planner models aggregate decomposition as vertical-slice groups and bite-sized requirement increments", () => {
  assert.match(source, /read the repository root `CONTEXT\.md`/);
  assert.match(source, /Feature normally represents one complete, demonstrable vertical slice/);
  assert.match(source, /An Epic may either contain multiple Features or represent one coherent vertical slice/);
  assert.match(source, /bite-sized increments/);
  assert.match(source, /at most two authoritative Requirements/);
  assert.match(source, /Do not target or cap the number of nodes/);
  assert.match(source, /authoritative Requirements with Given\/When\/Then/);
  assert.match(source, /vague horizontal buckets/);
  assert.match(source, /confirm granularity/);
});

test("task-planner loads codebase-design only for consequential module seams", () => {
  assert.match(source, /skills: write-plan, shape-spec, codebase-design/);
  assert.match(source, /Apply the loaded `codebase-design` skill/);
  assert.match(source, /Module Interface, Seam, Adapter/);
  assert.match(source, /Carry the chosen Seam and its invariants into the Task Graph/);
  assert.match(source, /Do not invoke its Design It Twice process unless/);
});
