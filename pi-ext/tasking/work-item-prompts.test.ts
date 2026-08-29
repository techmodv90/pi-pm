import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import {
  buildStagePrimer,
  buildWorkProgressLedger,
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
  latestRriTScenarios,
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

test("aggregate verification handoff loads persisted scenarios before contractor grading", () => {
  const artifact = JSON.stringify({
    methodology: "rri-t",
    personas: ["QA / Tester"],
    scenarios: [{ id: "RRI-T-1", persona: "QA / Tester", dimension: "D3", stress_axis: "ERROR", requirement_id: "REQ-1", procedure: "submit the empty form → inline error is shown", remediation_hint: "Assert the error text" }],
    not_applicable: [],
    open_blockers: [],
  });
  const prompt = buildAggregateVerifyPrompt({
    work_item: { id: "wi-epic", title: "Release" },
    children: [{ id: "wi-child", title: "Child", status: "done" }],
    artifacts: [{ id: "wia-1", work_item_id: "wi-epic", stage: "rri_t_scenarios", revision: 1, content_hash: "hash-1", content: artifact }],
  });
  // persisted artifact, not in-memory persona output
  assert.match(prompt, /Persisted RRI-T Scenarios/);
  assert.match(prompt, /Loaded from artifact wia-1 \(revision 1, content hash hash-1\) — never from in-memory persona output/);
  assert.match(prompt, /RRI-T-1/);
  assert.match(prompt, /Do not\s+re-run persona subagents/i);
  // soft owner gate: explicit trim/defer honored, no response proceeds without stalling
  assert.match(prompt, /Owner Scenario Gate \(soft\)/);
  assert.match(prompt, /trim or defer/i);
  assert.match(prompt, /without stalling/);
  // contractor-only execution and grading with not_applicable reasons
  assert.match(prompt, /contractor only/);
  assert.match(prompt, /no subagent executes procedures or produces grades/);
  assert.match(prompt, /not_applicable with a concrete reason/);
  assert.match(prompt, /instead of failing verification/);
  assert.match(prompt, /FAIL blocks aggregate verification/);
  // submission paths to the canonical aggregate verification action
  assert.match(prompt, /verify_aggregate_work_item/);
  assert.match(prompt, /rri_t_evidence_json/);
  assert.match(prompt, /actor_role=contractor/);
});

test("aggregate verification blocks when no scenario artifact was persisted", () => {
  const prompt = buildAggregateVerifyPrompt({ work_item: { id: "wi-epic", title: "Release" }, children: [] });
  assert.match(prompt, /No persisted rri_t_scenarios artifact was found/);
  assert.match(prompt, /blocked until the authored scenarios are saved before execution/);
  assert.match(prompt, /verify_aggregate_work_item/);
});

test("aggregate scenario selection stays on the aggregate's own artifact, never a parent's higher revision", () => {
  const parentArtifact = JSON.stringify({
    methodology: "rri-t",
    personas: ["QA / Tester"],
    scenarios: [{ id: "PARENT-REV3", persona: "QA / Tester", dimension: "D3", stress_axis: "ERROR", requirement_id: "REQ-P", procedure: "parent scenario must not leak", remediation_hint: "" }],
    not_applicable: [],
    open_blockers: [],
  });
  const ownArtifact = JSON.stringify({
    methodology: "rri-t",
    personas: ["QA / Tester"],
    scenarios: [{ id: "FEATURE-REV1", persona: "QA / Tester", dimension: "D3", stress_axis: "ERROR", requirement_id: "REQ-1", procedure: "submit the empty form → inline error is shown", remediation_hint: "Assert the error text" }],
    not_applicable: [],
    open_blockers: [],
  });
  // Merged-data shape produced by withInheritedParentWorkflowArtifacts: the
  // parent's higher-revision scenarios appear next to the aggregate's own rows.
  const merged = {
    work_item: { id: "wi-feature", title: "Feature" },
    inherited_parent_work_item: { id: "wi-epic", title: "Epic" },
    children: [{ id: "wi-child", title: "Child", status: "done" }],
    artifacts: [
      { id: "wia-own", work_item_id: "wi-feature", stage: "rri_t_scenarios", revision: 1, content_hash: "hash-own", content: ownArtifact },
      { id: "wia-parent", work_item_id: "wi-epic", stage: "rri_t_scenarios", revision: 3, content_hash: "hash-parent", content: parentArtifact },
    ],
  };
  const selected = latestRriTScenarios(merged);
  assert.equal(selected?.artifact.id, "wia-own");
  assert.equal(selected?.artifact.revision, 1);
  const prompt = buildAggregateVerifyPrompt(merged);
  assert.match(prompt, /Loaded from artifact wia-own \(revision 1/);
  assert.match(prompt, /FEATURE-REV1/);
  assert.doesNotMatch(prompt, /PARENT-REV3/);
});

test("tool persists the scenario artifact before execution and never re-authors at grading submission", () => {
  const tool = readFileSync(new URL("../api/tool.ts", import.meta.url), "utf8");
  // save-before-execution: persona authoring output is persisted as the rri_t_scenarios artifact
  assert.match(tool, /artifact-save[\s\S]{0,80}rri_t_scenarios/);
  assert.match(tool, /await scheduler\.runRriT\(data\)/);
  // the verify action compiles graded evidence from the persisted artifact via the canonical --rri-t-json path
  const verifyCase = tool.slice(tool.indexOf('case "verify_aggregate_work_item"'), tool.indexOf('case "accept_aggregate_work_item"'));
  assert.doesNotMatch(verifyCase, /runRriT/);
  assert.match(verifyCase, /compileRriTSubmission/);
  assert.match(verifyCase, /--rri-t-json/);
  // grading happens before submission, so a missing persisted artifact blocks instead of executing an unpersisted list
  assert.ok(verifyCase.indexOf("compileRriTSubmission") < verifyCase.indexOf('"aggregate-verify"'));
  // the aggregate's own persisted scenarios are graded, never parent-inherited rows
  assert.doesNotMatch(verifyCase, /withInheritedParentWorkflowArtifacts/);
});

test("RRI-T grading compiler dedupes on the id-based identity and rejects duplicate deferred dispositions", () => {
  const tool = readFileSync(new URL("../api/tool.ts", import.meta.url), "utf8");
  const compile = tool.slice(tool.indexOf("function rriTScenarioIdentity"), tool.indexOf("export function registerTaskManagerTool"));
  // The identity contract is id-based (dimension|stress_axis|requirement_id|id) —
  // never the persona — so scenarios sharing persona, dimension, stress axis, and
  // requirement stay distinct by id, and the compiled outcome carries the
  // persisted scenario id for the canonical Go validator.
  assert.match(compile, /\$\{scenario\.dimension\}\|\$\{scenario\.stress_axis\}\|\$\{scenario\.requirement_id\}\|\$\{scenario\.id\}/);
  assert.doesNotMatch(compile, /\$\{scenario\.persona\}\|\$\{scenario\.dimension\}/);
  assert.match(compile, /scenarios\.push\(\{ id: match\.id,/);
  // One persisted scenario receives exactly one outcome: the shared outcome set
  // rejects a duplicate deferred disposition (the same scenario deferred twice via
  // not_applicable) as well as a scenario graded and deferred at once.
  assert.match(compile, /outcomes\.has\(key\)/);
  assert.match(compile, /received more than one outcome/);
});

test("graded submission requires persisted identities, executed evidence, and not_applicable reasons", () => {
  const tool = readFileSync(new URL("../api/tool.ts", import.meta.url), "utf8");
  const compile = tool.slice(tool.indexOf("function compileRriTSubmission"), tool.indexOf("export function registerTaskManagerTool"));
  assert.match(compile, /persisted rri_t_scenarios artifact is missing/);
  assert.match(compile, /not in the persisted rri_t_scenarios artifact/);
  assert.match(compile, /requires executed evidence/);
  assert.match(compile, /result must be PASS, ACCEPTABLE, PAINFUL, or FAIL/);
  assert.match(compile, /not_applicable scenario .* requires a concrete reason/);
  assert.match(compile, /received more than one outcome/);
  assert.match(compile, /must reuse the persisted procedure verbatim/);
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
test("task-worker escalation ladder is prompt-encoded and fail-closed", () => {
  const source = readFileSync(new URL("../agents/task-worker.md", import.meta.url), "utf8");
  assert.match(source, /escalation_ladder/);
  assert.match(source, /Mechanical L3 floor/);
  assert.match(source, /Artifact contradiction/);
  assert.match(source, /Under-determination/);
  assert.match(source, /When unsure between levels, escalate upward/);
  assert.match(source, /status=\\"escalated\\"|status="escalated"/);
  assert.match(source, /checked_sources/);
  assert.match(source, /Never finish with a prose question inside a success summary/);
});

test("stage primer carries lineage, bounded approved digests, repo conventions, and definition of done", () => {
  const longContent = "R".repeat(4000);
  const primer = buildStagePrimer({
    work_item_id: "wi-1",
    stage: "blueprint",
    profile: { version: 3, contentHash: "sha256:prof", stages: ["scan", "rri", "vision", "blueprint", "contracts", "task_graph"] },
    predecessor_checkpoint: { stage: "vision", artifact_id: "war-9", artifact_revision: 2, content_hash: "sha256:vis" },
    approved_digests: [
      { stage: "scan", artifact_id: "war-1", artifact_revision: 1, content_hash: "sha256:scan", content: longContent },
      { stage: "rri", artifact_id: "war-2", artifact_revision: 1, content_hash: "sha256:rri", content: "requirements digest" },
    ],
  });
  assert.match(primer, /Work Item: wi-1/);
  assert.match(primer, /Stage: blueprint/);
  assert.match(primer, /profile v3 \(sha256:prof\)/);
  assert.match(primer, /Predecessor: checkpoint vision@2 \(sha256:vis\)/);
  assert.match(primer, /load_planning_artifact/);
  assert.match(primer, /scan @1 \(sha256:scan\)/);
  assert.match(primer, /requirements digest/);
  // Digest budget: an oversized artifact is truncated with an ellipsis marker.
  assert.ok(primer.length < 4000, `primer too long: ${primer.length}`);
  assert.match(primer, /R{1000}…/s);
  assert.match(primer, /DEFINITION OF DONE/);
  assert.match(primer, /Do not save or approve owner decisions yourself/);
});

test("progress ledger heads a relaunch with attempt identity and trimmed prior evidence", () => {
  const ledger = buildWorkProgressLedger({
    activePackId: "wip-9",
    activePackVersion: 4,
    attempt: 3,
    priorReports: [
      { id: "cr-1", status: "partial", summary: "Implemented the parser but verification failed", created_at: "2026-01-01 00:00:00" },
    ],
    failedVerifications: [
      { command: "go test ./...", evidence: "FAIL TestX\n    ".concat("x".repeat(2000)) },
    ],
    escalationContext: "\n\n## ESCALATION RESOLUTIONS\n- Escalation esc-1: use sqlite\n",
  });
  assert.match(ledger, /This is attempt 3 of TIP-004 \(pack wip-9 v4\) — continue, do not re-plan from scratch/);
  assert.match(ledger, /Attempt evidence ledger/);
  assert.match(ledger, /cr-1 .*partial.*Implemented the parser/);
  assert.match(ledger, /go test \.\/\.\.\./);
  // Failed verification output is trimmed to a bounded budget.
  assert.ok(ledger.length < 3000, `ledger too long: ${ledger.length}`);
  assert.match(ledger, /ESCALATION RESOLUTIONS/);
});

test("scheduler stage prompts dispatch the primer and the ledger at the right stages", () => {
  const source = readFileSync(new URL("../pipeline/stage-prompts.ts", import.meta.url), "utf8");
  assert.match(source, /isPlanningStage\(stage\)\)[\s\S]{0,400}buildStagePrimer/);
  assert.match(source, /buildWorkProgressLedger\(/);
});
