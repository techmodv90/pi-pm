import assert from "node:assert/strict";
import test from "node:test";
import { parsePicShow } from "./pic-show.ts";

const workItem = { id: "wi-1", type: "task", title: "Leaf task" };

test("parsePicShow normalizes a canonical show document and defaults absent collections", () => {
  const parsed = parsePicShow({ work_item: workItem, project: { name: "demo", root_path: "/tmp/demo" } });
  assert.deepEqual(parsed.work_item, workItem);
  assert.equal(parsed.project?.name, "demo");
  assert.deepEqual(parsed.artifacts, []);
  assert.deepEqual(parsed.checkpoints, []);
  assert.deepEqual(parsed.instruction_packs, []);
  assert.deepEqual(parsed.completion_reports, []);
  assert.deepEqual(parsed.verification_reports, []);
  assert.deepEqual(parsed.owner_decisions, []);
  assert.deepEqual(parsed.escalations, []);
  assert.deepEqual(parsed.children, []);
  assert.deepEqual(parsed.dependencies, []);
  assert.deepEqual(parsed.requirements, []);
});

test("parsePicShow preserves present collections and scalar flags", () => {
  const artifact = { id: "war-1", stage: "scan", revision: 1, content_hash: "sha256:x" };
  const parsed = parsePicShow({
    work_item: { ...workItem, review_status: "pending" },
    artifacts: [artifact],
    ready: true,
    canonical: true,
    execution_state: "instruction_pack",
  });
  assert.deepEqual(parsed.artifacts, [artifact]);
  assert.equal(parsed.ready, true);
  assert.equal(parsed.canonical, true);
  assert.equal(parsed.execution_state, "instruction_pack");
});

test("parsePicShow fails closed naming the malformed field", () => {
  assert.throws(() => parsePicShow(null), /pic show returned a non-object document/);
  assert.throws(() => parsePicShow("nope"), /pic show returned a non-object document/);
  assert.throws(() => parsePicShow({}), /missing work_item/);
  assert.throws(() => parsePicShow({ work_item: { type: "task", title: "x" } }), /work_item\.id/);
  assert.throws(() => parsePicShow({ work_item: { id: "wi-1", title: "x" } }), /work_item\.type/);
  assert.throws(() => parsePicShow({ work_item: { id: "wi-1", type: "task", title: "x" }, artifacts: "nope" }), /artifacts must be an array/);
  assert.throws(() => parsePicShow({ work_item: workItem, checkpoints: 7 }), /checkpoints must be an array/);
  assert.throws(() => parsePicShow({ work_item: workItem, ready: "yes" }), /ready must be a boolean/);
});
