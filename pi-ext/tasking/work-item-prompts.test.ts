import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import {
  assertPlanningHandoffAttributes,
  buildAggregateVerifyPrompt,
  buildPlanningHandoffXml,
  buildReviewInstructions,
  buildTaskVerifyPrompt,
  buildWorkItemContinuePrompt,
  buildWorkItemDebugPrompt,
  buildWorkItemReviewerHandoff,
  buildWorkItemScanPrompt,
  formatWorkItemChecklist,
  normalizePlanningHandoffAttributes,
  parsePlanningHandoffAttributes,
} from "./work-item-prompts.ts";

test("planning handoff binds identity, stage, predecessor checkpoint, and profile version/hash", () => {
  const xml = buildPlanningHandoffXml({ work_item_id: "wi-f", stage: "rri", predecessor_checkpoint: "cp-scan-1", profile_version: "2", profile_hash: "hash-2" }, "owner interview evidence");
  assert.match(xml, /^<planning_handoff schema_version="1" work_item_id="wi-f" stage="rri" predecessor_checkpoint="cp-scan-1" profile_version="2" profile_hash="hash-2">/);
  assert.match(xml, /owner interview evidence/);
  assert.deepEqual(parsePlanningHandoffAttributes(xml), { work_item_id: "wi-f", stage: "rri", predecessor_checkpoint: "cp-scan-1", profile_version: "2", profile_hash: "hash-2" });
});

test("planning handoff validation rejects malformed bindings and unsupported stages", () => {
  assert.throws(() => buildPlanningHandoffXml({ work_item_id: "", stage: "rri", predecessor_checkpoint: "", profile_version: "1", profile_hash: "h" }, "body"), /missing Work Item identity/);
  assert.throws(() => buildPlanningHandoffXml({ work_item_id: "wi-f", stage: "contracts", predecessor_checkpoint: "", profile_version: "1", profile_hash: "h" }, "body"), /unsupported stage contracts/);
  assert.throws(() => buildPlanningHandoffXml({ work_item_id: "wi-f", stage: "rri", predecessor_checkpoint: "", profile_version: "", profile_hash: "" }, "body"), /persisted profile version and hash/);
  assert.throws(() => parsePlanningHandoffAttributes("not xml"), /one <planning_handoff>/);
  assert.throws(() => assertPlanningHandoffAttributes(normalizePlanningHandoffAttributes({ work_item_id: "wi-f", stage: "blueprint" })), /persisted profile version and hash/);
});

test("planning handoff rejects dispatch before the Plan profile is persisted (resolved:false)", () => {
  // A resolved:false profile yields an empty profile_hash; building the handoff
  // must throw so no pre-persistence dispatch can publish an invalid envelope.
  const resolvedFalseProfile = { version: 0, contentHash: "" };
  assert.throws(
    () => buildPlanningHandoffXml({ work_item_id: "wi-g", stage: "task_graph", predecessor_checkpoint: "cp-rri", profile_version: String(resolvedFalseProfile.version), profile_hash: resolvedFalseProfile.contentHash }, "<raw/>"),
    /persisted profile version and hash/,
  );
});

test("planning handoff CDATA-wraps raw payload so XML metacharacters cannot corrupt the envelope", () => {
  const xml = buildPlanningHandoffXml({ work_item_id: "wi-f", stage: "blueprint", predecessor_checkpoint: "cp-vision", profile_version: "2", profile_hash: "hash-2" }, "a < b & \"quoted\" </planning_handoff>]]> tail");
  // The raw body is CDATA-wrapped, its embedded CDATA terminator is split, and
  // the envelope still parses to the declared attributes despite metacharacters.
  assert.ok(xml.includes("<![CDATA[a < b & \"quoted\" </planning_handoff>]]]]><![CDATA[> tail]]>"));
  assert.ok(xml.includes("a < b & \"quoted\" </planning_handoff>"));
  assert.equal(parsePlanningHandoffAttributes(xml).work_item_id, "wi-f");
  assert.equal(parsePlanningHandoffAttributes(xml).stage, "blueprint");
});

test("contractor verification handoff executes the active TIP protocol", () => {
  const prompt = buildTaskVerifyPrompt({
    work_item: { id: "wi-1", title: "Verify me" },
    instruction_packs: [{ id: "wip-1", status: "active", content_json: JSON.stringify({ verification: [
      { command: "npm test", required: true },
      { command: "npm run check", required: true, setup_commands: ["npm ci"] },
    ] }) }],
    completion_reports: [{ id: "wicr-1", instruction_pack_id: "wip-1", status: "done" }],
  });

  assert.match(prompt, /execute this verification now/i);
  assert.match(prompt, /npm test/);
  assert.match(prompt, /npm ci/);
  assert.match(prompt, /npm run check/);
  assert.match(prompt, /verify_work_item/);
  assert.match(prompt, /wicr-1/);
  assert.match(prompt, /actor_role=contractor/);
  assert.match(prompt, /do not delegate/i);
  assert.match(prompt, /child closes automatically/i);

  const tool = readFileSync(new URL("../api/tool.ts", import.meta.url), "utf8");
  const commands = readFileSync(new URL("../api/commands.ts", import.meta.url), "utf8");
  assert.match(tool, /work_item_workflow_status[\s\S]+aggregate_verification[\s\S]+buildAggregateVerifyPrompt/);
  assert.match(commands, /next_stage === "contractor_verification"[\s\S]+buildTaskVerifyPrompt/);
});

test("aggregate verification handoff precedes the single owner decision", () => {
  const prompt = buildAggregateVerifyPrompt({ work_item: { id: "wi-epic", title: "Release" }, children: [{ id: "wi-child", title: "Child", status: "done" }] });
  assert.match(prompt, /final aggregate verification/i);
  assert.match(prompt, /wi-child/);
  assert.match(prompt, /verify_aggregate_work_item/);
  assert.match(prompt, /Do not call owner acceptance/);
});

test("Work Item prompts use only canonical lifecycle actions", () => {
  const prompts = [
    buildWorkItemContinuePrompt({ work_item_id: "wi-1", next_stage: "scan" }, { title: "Canonical item" }),
    buildWorkItemScanPrompt({ id: "wi-1", title: "Canonical item" }, { name: "Project", root_path: "/repo" }),
    buildWorkItemDebugPrompt({ id: "wi-1", title: "Canonical item" }, { trigger: "failure" }),
    buildWorkItemReviewerHandoff("wi-1"),
    buildReviewInstructions("wi-1"),
  ].join("\n");

  assert.match(prompts, /save_work_item_artifact/);
  assert.match(prompts, /Return evidence for your assigned Scan section only/);
  assert.match(prompts, /trigger_work_item_review/);
  assert.doesNotMatch(prompts, /scan_task|rri_task|design_task|complete_task_item|save_verification_report|save_owner_decision/);
  assert.doesNotMatch(prompts, /\bEpic\b|\bTask Item\b|\bchild Tasks\b/);
});

test("Scan Scout returns section evidence for contractor synthesis", () => {
  const prompt = buildWorkItemScanPrompt({ id: "wi-1", title: "Canonical item" });
  assert.match(prompt, /read-only task-scout/i);
  assert.match(prompt, /assigned Scan section only/);
  assert.doesNotMatch(prompt, /<scan_report>|reject_work_item_scan/);
  assert.doesNotMatch(prompt, /approve_work_item_artifact/);
});

test("Scan contractor handoff requests canonical XML without presentation formatting", () => {
  const prompt = buildWorkItemContinuePrompt({ work_item_id: "wi-1", next_stage: "scan" }, { title: "Canonical item" });
  assert.match(prompt, /canonical Scan Report[\s\S]+as structured XML/);
  assert.doesNotMatch(prompt, /Markdown report format|TECH_STACK/);
});

test("formatWorkItemChecklist preserves archived checklist evidence", () => {
  assert.equal(formatWorkItemChecklist([{ id: "i-1", content: "Done" }], true), "- [x] (id: i-1) Done\n");
});

test("executable continuation follows TIP execution gates", () => {
  assert.match(buildWorkItemContinuePrompt({ work_item_id: "wi-1", next_stage: "implement" }, { title: "Leaf", type: "task" }), /work_on_work_item/);
  assert.match(buildWorkItemContinuePrompt({ work_item_id: "wi-1", next_stage: "task_graph" }, { title: "Leaf", type: "task" }), /exactly one.*existing Work Item/i);
  assert.match(buildWorkItemContinuePrompt({ work_item_id: "wi-1", next_stage: "contractor_verification" }, { title: "Leaf", type: "task" }), /verify_work_item/);
  assert.match(buildWorkItemContinuePrompt({ work_item_id: "wi-1", next_stage: "owner_acceptance" }, { title: "Feature", type: "feature" }), /accept_aggregate_work_item/);
  assert.match(buildWorkItemContinuePrompt({ work_item_id: "wi-1", next_stage: "merge_pending" }, { title: "Feature", type: "feature" }), /merge_aggregate_work_item/);
});