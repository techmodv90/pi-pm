import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import {
  buildAggregateVerifyPrompt,
  buildReviewInstructions,
  buildTaskVerifyPrompt,
  buildWorkItemContinuePrompt,
  buildWorkItemDebugPrompt,
  buildWorkItemReviewerHandoff,
  buildWorkItemScanPrompt,
  formatWorkItemChecklist,
} from "./work-item-prompts.ts";

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
  assert.match(prompts, /Return the complete Scan Report to the contractor/);
  assert.match(prompts, /trigger_work_item_review/);
  assert.doesNotMatch(prompts, /scan_task|rri_task|design_task|complete_task_item|save_verification_report|save_owner_decision/);
  assert.doesNotMatch(prompts, /\bEpic\b|\bTask Item\b|\bchild Tasks\b/);
});

test("Scan Scout returns the report without Work Item persistence", () => {
  const prompt = buildWorkItemScanPrompt({ id: "wi-1", title: "Canonical item" });
  assert.match(prompt, /read-only task-scout/i);
  assert.match(prompt, /Return the complete Scan Report to the contractor/);
  assert.doesNotMatch(prompt, /save_work_item_artifact|approve_work_item_artifact/);
});

test("formatWorkItemChecklist preserves archived checklist evidence", () => {
  assert.equal(formatWorkItemChecklist([{ id: "i-1", content: "Done" }], true), "- [x] (id: i-1) Done\n");
});

test("executable continuation follows TIP execution gates", () => {
  assert.match(buildWorkItemContinuePrompt({ work_item_id: "wi-1", next_stage: "implement" }, { title: "Leaf", type: "task" }), /work_on_work_item/);
  assert.match(buildWorkItemContinuePrompt({ work_item_id: "wi-1", next_stage: "contractor_verification" }, { title: "Leaf", type: "task" }), /verify_work_item/);
  assert.match(buildWorkItemContinuePrompt({ work_item_id: "wi-1", next_stage: "owner_acceptance" }, { title: "Feature", type: "feature" }), /accept_aggregate_work_item/);
  assert.match(buildWorkItemContinuePrompt({ work_item_id: "wi-1", next_stage: "merge_pending" }, { title: "Feature", type: "feature" }), /merge_aggregate_work_item/);
});