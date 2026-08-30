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
  const executionState = {
    active_instruction_pack_id: "",
    candidate_run_id: "",
    review_status: "",
    owner_approval_required: false,
    completion_report_id: "",
    verification_status: "",
    owner_decision: "",
    next_stage: "instruction_pack",
    pipeline_stage: "",
  };
  const parsed = parsePicShow({
    work_item: { ...workItem, review_status: "pending" },
    artifacts: [artifact],
    ready: true,
    canonical: true,
    execution_state: executionState,
  });
  assert.deepEqual(parsed.artifacts, [artifact]);
  assert.equal(parsed.ready, true);
  assert.equal(parsed.canonical, true);
  assert.deepEqual(parsed.execution_state, executionState);
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
  assert.throws(() => parsePicShow({ work_item: workItem, execution_state: "instruction_pack" }), /execution_state object/);
});

test("parsePicShow rejects malformed collection entries naming the index", () => {
	const workItem = { id: "wi-1", type: "task", title: "Leaf" };
	assert.throws(() => parsePicShow({ work_item: workItem, artifacts: [null] }), /pic show artifacts\[0\] must be an object/);
	assert.throws(() => parsePicShow({ work_item: workItem, dependencies: ["bad"] }), /pic show dependencies\[0\] must be an object/);
	assert.throws(() => parsePicShow({ work_item: workItem, checkpoints: [7] }), /pic show checkpoints\[0\] must be an object/);
	assert.throws(() => parsePicShow({ work_item: workItem, children: [[]] }), /pic show children\[0\] must be an object/);
	// Objects with arbitrary shapes still pass; element validation is structural.
	const parsed = parsePicShow({ work_item: workItem, artifacts: [{ stage: "scan" }] });
	assert.deepEqual(parsed.artifacts, [{ stage: "scan" }]);
});
