import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";

const source = readFileSync(new URL("../agents/task-reviewer.md", import.meta.url), "utf8");

test("child Reviewer is Code Review, not aggregate QA", () => {
  assert.match(source, /child Code Review, not aggregate QA/);
  assert.match(source, /Standards \(repository conventions/);
  assert.match(source, /Spec \(the Task contract/);
  assert.match(source, /Aggregate Verification later evaluates/);
  assert.match(source, /bound Requirements/);
});
