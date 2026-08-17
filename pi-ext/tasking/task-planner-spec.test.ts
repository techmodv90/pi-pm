import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";

const source = readFileSync(new URL("../agents/task-planner.md", import.meta.url), "utf8");
test("task-planner Blueprint contract is artifact-first", () => {
  assert.match(source, /<blueprint_handoff>/);
  assert.match(source, /save_work_item_artifact.*stage="blueprint"/s);
  assert.match(source, /save_work_item_artifact.*exactly once/s);
  assert.match(source, /Do not call `save_design`/);
  assert.match(source, /Never return a Markdown planning report instead of saving the JSON artifact/);
  assert.doesNotMatch(source, /\*\*Design saved:\*\*/);
  assert.doesNotMatch(source, /\*\*Task Plan nodes:\*\*/);
});

test("task-planner Task Graph contract uses XML input and JSON persistence", () => {
  assert.match(source, /<task_graph_handoff>/);
  assert.match(source, /stage="task_graph"/);
  assert.match(source, /schema version 3/);
});
