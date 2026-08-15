import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { assertIndexMatchesReviewedPatch, assertReviewBaseCurrent, assertReviewFixChangedPatch, assertRunContractCurrent, buildAutofixContext, buildOwnerRejectionContext, buildPipelineDryRun, buildWorkerCorrectionContext, canonicalReadyLeafIds, filterGeneratedFiles, finalizeReviewedIntegration, formatPipelineStatus, mergeAggregateBranch, normalizePipelineData, nextPipelineStage, parseApplyNumstatPaths, parsePorcelainPaths, parseReviewReport, parseTaskCompletionReport, pipelineFailureResult, PipelineScheduler, pipelineIntegrationBlockReason, pipelineSpawnParams, pipelineVerificationBlockReason, pipelineWorkerBlockReason, recoverReviewedPatch, renderCanonicalInstructionPackMarkdown, validateWorkerChangedFiles, validateWorkerOutput, validateWorkerPatchArtifact, workerIntegrationCandidate } from "./pipeline-scheduler.ts";
import { parsePipelineRuns } from "./pipeline-types.ts";

test("normalizePipelineData adapts canonical Work Items without snapshot authority", () => {
  const data = normalizePipelineData({
    work_item: { id: "wi-1", type: "task", title: "Leaf", status: "open" },
    ready: true,
    artifacts: [
      { id: "scan-1", stage: "scan", revision: 1, content: '{"summary":"Repository scan"}' },
      { id: "blueprint-1", stage: "blueprint", revision: 1, content: '{"summary":"Approved design"}' },
      { id: "vision-draft", stage: "vision", revision: 2, content: '{"summary":"Unapproved"}' },
    ],
    checkpoints: [
      { artifact_id: "scan-1", stage: "scan", artifact_revision: 1 },
      { artifact_id: "blueprint-1", stage: "blueprint", artifact_revision: 1 },
    ],
    instruction_packs: [{ id: "wip-1", version: 2, status: "active", content_hash: "hash-2", content_json: '{"constraints":{"scope_roots":["src"]},"skillFamilies":["languages/typescript"]}' }],
    dependencies: [],
  });
  assert.equal(data.work_item.id, "wi-1");
  assert.equal(data.task, undefined);
  assert.equal(data.canonical, true);
  assert.deepEqual(data.instruction_packs[0].constraints_json, '{"scope_roots":["src"]}');
  assert.deepEqual(data.scan_reports.map((artifact: any) => artifact.id), ["scan-1"]);
  assert.deepEqual(data.designs.map((artifact: any) => artifact.id), ["blueprint-1"]);
  assert.equal(pipelineWorkerBlockReason(data), null);
  assert.doesNotThrow(() => assertRunContractCurrent(data, { instruction_pack_id: "wip-1", instruction_pack_hash: "hash-2" }));
});

test("canonical worker handoff renders structured TIP content as readable Markdown", () => {
  const markdown = renderCanonicalInstructionPackMarkdown(
    { id: "wi-1", title: "Migrate guests", type: "task", priority: "high" },
    { id: "pack-1", version: 1, status: "active", content_hash: "hash-1", content_json: JSON.stringify({ content: { goal: "Move guest mutations", files: ["src/guests.ts"], patterns: [{ file: "src/mutations.ts", symbol: "createMutation", reason: "Reuse mutation behavior" }], business_rules: ["Preserve validation"], validation_rules: [{ input: "guest forms", rule: "Preserve validation", failure: "Keep input visible" }], constraints: { scope_roots: ["src"], must_not_change: ["docs"] }, verification: [{ command: "npm test", expected_writes: ["coverage/**"], required: true }], skillFamilies: ["languages/typescript"] }, requirements: [{ requirement_key: "REQ-1", title: "Guest mutations", acceptance_criteria: "Mutations use the API" }] }) },
  );
  assert.match(markdown, /^# WORK ITEM INSTRUCTION PACK: Migrate guests/m);
  assert.match(markdown, /scheduler has already claimed and launched this Work Item/);
  assert.match(markdown, /Do not call `work_on_work_item`, `pipeline-claim`, `reset_pipeline_circuit`/);
  assert.match(markdown, /- Skill families: languages\/typescript/);
  assert.match(markdown, /- Key files:\n  - src\/guests\.ts/);
  assert.match(markdown, /- Patterns:\n  - \*\*File:\*\* src\/mutations\.ts\n    - \*\*Symbol:\*\* createMutation\n    - \*\*Reason:\*\* Reuse mutation behavior/);
  assert.match(markdown, /### Validation\n- \*\*Input:\*\* guest forms\n  - \*\*Rule:\*\* Preserve validation\n  - \*\*Failure:\*\* Keep input visible/);
  assert.match(markdown, /## CONSTRAINTS\n- \*\*Scope roots:\*\* src\n- \*\*Must not change:\*\* docs/);
  assert.match(markdown, /## VERIFICATION\n- \*\*Command:\*\* npm test\n  - \*\*Expected writes:\*\* coverage\/\*\*\n  - \*\*Required:\*\* true/);
  assert.match(markdown, /## TASK\nMove guest mutations/);
  assert.match(markdown, /## ACCEPTANCE CRITERIA[\s\S]*REQ-1/);
  assert.match(markdown, /## VERIFICATION[\s\S]*npm test/);
  assert.doesNotMatch(markdown, /\{"(?:file|input|scope_roots|command)"/);
  assert.doesNotMatch(markdown, /```json/);
});

test("pipeline circuit reset tool forwards typed runner evidence", () => {
  const source = readFileSync(new URL("../api/tool.ts", import.meta.url), "utf8");
  assert.match(source, /change_type: Type\.Optional\(StringEnum\(\["contract", "environment", "runner", "artifact"\]/);
  assert.match(source, /"--change-type", params\.change_type, "--evidence-json", params\.evidence_json/);
});

test("owner-only graph actions never synthesize owner authorization", () => {
  const source = readFileSync(new URL("../api/tool.ts", import.meta.url), "utf8");
  assert.doesNotMatch(source, /params\.actor_role \|\| "owner"/);
  assert.match(source, /actor_role must be owner after explicit owner approval/);
});

test("child task-manager capabilities are restricted by the launched agent role", () => {
  const tool = readFileSync(new URL("../api/tool.ts", import.meta.url), "utf8");
  const runner = readFileSync(new URL("../subagent/runner.ts", import.meta.url), "utf8");
  assert.match(tool, /assertTaskManagerActionAllowed\(process\.env\.PI_TASK_AGENT_NAME/);
  assert.match(runner, /PI_TASK_AGENT_NAME: spec\.agent\.name/);
});

test("canonicalReadyLeafIds returns only authorized dependency-ready executable descendants", () => {
  const details: Record<string, any> = {
    root: { work_item: { id: "root", type: "epic" }, children: [{ id: "feature", type: "feature" }, { id: "blocked", type: "task" }] },
    feature: { work_item: { id: "feature", type: "feature" }, children: [{ id: "ready", type: "task" }, { id: "gate", type: "gate" }] },
    ready: { work_item: { id: "ready", type: "task" }, ready: true },
    blocked: { work_item: { id: "blocked", type: "task" }, ready: false },
    gate: { work_item: { id: "gate", type: "gate" }, ready: false },
  };
  assert.deepEqual(canonicalReadyLeafIds(details.root, (id) => details[id]), ["ready"]);
});

test("canonicalReadyLeafIds traverses materialized children of executable parents", () => {
  const details: Record<string, any> = {
    child: { work_item: { id: "child", type: "task" }, ready: true, children: [] },
  };
  const root = { work_item: { id: "parent", type: "task" }, ready: false, children: [{ id: "child" }] };
  assert.deepEqual(canonicalReadyLeafIds(root, (id) => details[id]), ["child"]);
});

test("canonicalReadyLeafIds treats cancelled children as absent", () => {
  const details: Record<string, any> = {
    cancelled: { work_item: { id: "cancelled", type: "task", status: "cancelled" }, ready: false, children: [] },
  };
  const root = { work_item: { id: "root", type: "task" }, ready: true, children: [{ id: "cancelled", status: "cancelled" }] };
  assert.deepEqual(canonicalReadyLeafIds(root, (id) => details[id]), ["root"]);
});

test("pipeline dry-run reports planned leaf stages and blockers without mutations", () => {
  const details: Record<string, any> = {
    ready: { work_item: { id: "ready", type: "task" }, ready: true, children: [], instruction_packs: [{ status: "active" }] },
    blocked: { work_item: { id: "blocked", type: "task" }, ready: false, children: [], instruction_packs: [{ status: "active" }], dependencies: [{ depends_on_work_item_id: "ready", status: "open" }] },
    cancelled: { work_item: { id: "cancelled", type: "task", status: "cancelled" }, ready: false, children: [], instruction_packs: [] },
  };
  const root = { work_item: { id: "root", type: "task" }, children: [{ id: "ready" }, { id: "blocked" }, { id: "cancelled" }] };
  assert.deepEqual(buildPipelineDryRun(root, (id) => details[id]), {
    rootTaskId: "root",
    leaves: [
      { taskId: "ready", ready: true, stage: "worker", blocker: null },
      { taskId: "blocked", ready: false, stage: null, blocker: 'Work Item "blocked" is blocked by incomplete dependencies: ready' },
    ],
  });
});

test("canonical aggregate scheduling requires owner resolution after Scan rejection", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  assert.match(source, /next_stage[\s\S]{0,500}scan-rejection[\s\S]{0,500}launchGroup\("scan"/);
  assert.match(source, /Owner decision required:[\s\S]{0,220}reset_work_item_planning/);
});

test("full aggregate Scan fans out bounded evidence sections for contractor synthesis", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  for (const section of ["Architecture", "Lifecycle", "Authority", "Verification", "Reliability"]) assert.match(source, new RegExp(`\\["${section}"`));
  assert.match(source, /startFullScanFanout/);
  assert.match(source, /Do not compose the canonical Scan Report/);
  assert.match(source, /Contractor: validate these section reports[\s\S]+author one canonical Scan Report/);
});

test("successful Scan Scout handoff completes without reporting a blocked attempt", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const scanStart = source.indexOf('if (run.stage === "scan")');
  const scanFinish = source.slice(scanStart, source.indexOf("this.pi.sendUserMessage", scanStart));
  assert.match(scanFinish, /pipeline-complete[\s\S]+"completed"/);
  assert.doesNotMatch(scanFinish, /pipeline-complete[\s\S]+"blocked"/);
});

test("canonical aggregate creation starts orchestration at Scan", () => {
  const source = readFileSync(new URL("../api/tool.ts", import.meta.url), "utf8");
  assert.match(source, /create_work_item[\s\S]+pipelineScheduler\.start\(/);
});

test("canonical failed-review worker prompts include persisted correction findings", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const stagePromptBody = source.slice(source.indexOf("function stagePrompt"), source.indexOf("function rejectedCandidatePatch"));
  assert.match(stagePromptBody, /currentFailedReview[\s\S]+buildWorkerCorrectionContext\(\{ \.\.\.data, current_review: currentReview \}\)/);
});

test("pipeline run parsing rejects malformed pic records instead of silently dropping them", () => {
  assert.throws(() => parsePipelineRuns([{ id: "run-1", task_id: "task-1" }]), /invalid pipeline run record/);
  assert.deepEqual(parsePipelineRuns([{ id: "run-1", task_id: "task-1", stage: "worker", status: "completed", lease_token: "lease-1" }]), [
    { id: "run-1", task_id: "task-1", stage: "worker", status: "completed", lease_token: "lease-1" },
  ]);
});

test("review output is a single structured scheduler-owned verdict", () => {
  assert.deepEqual(parseReviewReport('```review-report\n{"status":"passed","notes":"Reviewed integrated patch","findings":[]}\n```'), {
    status: "passed",
    notes: "Reviewed integrated patch",
    findings: [],
  });
  assert.throws(() => parseReviewReport("Approved"), /exactly one review-report/);
  assert.throws(() => parseReviewReport('```review-report\n{"status":"passed","notes":"ok","findings":"none"}\n```'), /findings must be strings/);
});

test("nextPipelineStage stops after task review", () => {
  assert.equal(nextPipelineStage({}), "scan");
  assert.equal(nextPipelineStage({ scan_reports: [{ status: "partial" }] }), "scan");
  assert.equal(nextPipelineStage({ scan_reports: [{ status: "partial" }, { status: "completed" }] }), "scan");
  assert.equal(nextPipelineStage({ scan_reports: [{ status: "completed" }] }), null);
  const ready = { instruction_packs: [{ status: "active" }] };
  assert.equal(nextPipelineStage(ready), "worker");
  const completion = { id: "cr-worker", status: "done", pipeline_run_id: "pr-worker" };
  const integratedWorker = { id: "pr-worker", stage: "worker", status: "completed", integrated_at: "2026-01-01", integrated_patch_hash: "abc" };
  const candidateWorker = { id: "pr-candidate", stage: "worker", status: "completed", artifact_saved_at: "2026-01-01", integrated_patch_path: "/tmp/candidate.patch", integrated_patch_hash: "def" };
  assert.equal(nextPipelineStage({ ...ready, completion_reports: [], work_item: { review_status: "pending" } }, [candidateWorker]), "review");
  assert.equal(nextPipelineStage({ ...ready, completion_reports: [completion], work_item: { review_status: "pending" } }, [integratedWorker]), "review");
  assert.equal(nextPipelineStage({ ...ready, completion_reports: [completion], work_item: { review_status: "failed" } }, [integratedWorker]), "worker");
  assert.equal(nextPipelineStage({ ...ready, completion_reports: [completion], work_item: { review_status: "passed" } }, [integratedWorker]), null);
  assert.equal(nextPipelineStage({ ...ready, completion_reports: [completion], work_item: { review_status: "passed" }, verification_reports: [{ status: "failed" }] }, [integratedWorker]), null);
});

test("nextPipelineStage binds review authority to the latest candidate", () => {
  const pack = { status: "active", id: "tip-1", version: 1, content_hash: "pack-hash" };
  const oldCandidate = { id: "pr-old", stage: "worker", status: "completed", instruction_pack_hash: "pack-hash", artifact_saved_at: "2026-01-01", integrated_patch_hash: "old-hash", advanced_at: "2026-01-01" };
  const oldReview = { id: "pr-old-review", stage: "review", status: "completed", candidate_run_id: "pr-old", candidate_patch_hash: "old-hash", result_json: '{"review_status":"passed","candidate_run_id":"pr-old","candidate_patch_hash":"old-hash"}' };
  const currentCandidate = { id: "pr-current", stage: "worker", status: "completed", instruction_pack_hash: "pack-hash", artifact_saved_at: "2026-01-02", integrated_patch_hash: "current-hash", advanced_at: "" };
  assert.equal(nextPipelineStage({ instruction_packs: [pack], completion_reports: [], work_item: { review_status: "passed" } }, [currentCandidate, oldReview, oldCandidate]), "review");
  assert.equal(nextPipelineStage({ canonical: true, execution_state: { pipeline_stage: "review" }, work_item: { review_status: "passed" } }, []), "review");
});

test("canonical owner rejection routes the active TIP to fresh implementation", () => {
  const pack = { status: "active", id: "tip-1", version: 1, content_hash: "pack-hash" };
  const completion = { id: "cr-1", status: "done", pipeline_run_id: "pr-1", instruction_pack_id: "tip-1", instruction_pack_version: 1, instruction_pack_hash: "pack-hash" };
  const candidate = { id: "pr-1", stage: "worker", status: "completed", artifact_saved_at: "2026-01-01", integrated_at: "2026-01-01", integrated_patch_hash: "hash-1" };
  const review = { id: "review-1", stage: "review", status: "completed", candidate_run_id: "pr-1", candidate_patch_hash: "hash-1", result_json: '{"review_status":"passed","candidate_run_id":"pr-1","candidate_patch_hash":"hash-1"}' };
  const data = { instruction_packs: [pack], completion_reports: [completion], verification_reports: [{ completion_report_id: "cr-1", status: "passed" }], owner_decisions: [{ completion_report_id: "cr-1", decision: "rejected" }], work_item: { review_status: "pending" } };
  assert.equal(nextPipelineStage(data, [review, candidate]), "worker");
  assert.match(buildOwnerRejectionContext({ owner_decisions: [{ decision: "rejected", completion_report_id: "cr-1", notes: "Fix the export" }] }), /cr-1[\s\S]*Fix the export/);
});

test("completed scan pauses for TIP activation instead of completing the task", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const advanceBody = source.slice(source.indexOf("private async advance"), source.indexOf("private async resumePending"));
  assert.match(advanceBody, /const activePack = [^;]+status === "active"[\s\S]+if \(!activePack\) return;/);
});

test("nextPipelineStage routes a failed contractor verification to targeted autofix", () => {
  const pack = { status: "active", id: "tip-1", version: 1, content_hash: "pack-hash" };
  const completion = { id: "cr-worker", status: "done", pipeline_run_id: "pr-worker", instruction_pack_id: "tip-1", instruction_pack_version: 1, instruction_pack_hash: "pack-hash", created_at: "2026-01-01 00:00:00" };
  const integratedWorker = { id: "pr-worker", stage: "worker", status: "completed", integrated_at: "2026-01-01", integrated_patch_hash: "abc" };
  const failedVerification = { id: "vr-1", completion_report_id: "cr-worker", status: "failed", created_at: "2026-01-02 00:00:00" };
  const data = { instruction_packs: [pack], completion_reports: [completion], verification_reports: [failedVerification], work_item: { review_status: "passed" } };

  assert.equal(nextPipelineStage(data, [integratedWorker]), "autofix");

  const autofixCompletion = { ...completion, pipeline_run_id: "pr-autofix", created_at: "2026-01-03 00:00:00" };
  const integratedAutofix = { id: "pr-autofix", stage: "autofix", status: "completed", integrated_at: "2026-01-03", integrated_patch_hash: "def" };
  assert.equal(nextPipelineStage({ ...data, completion_reports: [autofixCompletion, completion], work_item: { review_status: "pending" } }, [integratedAutofix, integratedWorker]), "review");
});

test("nextPipelineStage binds same-second verification to its completion report", () => {
  const pack = { status: "active", id: "tip-1", version: 1, content_hash: "pack-hash" };
  const completion = { id: "cr-current", sequence: 20, status: "done", pipeline_run_id: "pr-worker", instruction_pack_id: "tip-1", instruction_pack_version: 1, instruction_pack_hash: "pack-hash", created_at: "2026-01-01 00:00:00" };
  const olderCompletion = { ...completion, id: "cr-older", pipeline_run_id: "pr-older" };
  const verification = { sequence: 1, completion_report_id: "cr-current", status: "failed", created_at: "2026-01-01 00:00:00" };
  const unrelated = { sequence: 99, completion_report_id: "cr-older", status: "passed", created_at: "2026-01-01 00:00:00" };
  const worker = { id: "pr-worker", stage: "worker", status: "completed", integrated_at: "2026-01-01", integrated_patch_hash: "abc" };

  assert.equal(nextPipelineStage({ instruction_packs: [pack], completion_reports: [completion, olderCompletion], verification_reports: [unrelated, verification], work_item: { review_status: "passed" } }, [worker]), "autofix");
});

test("non-passed contractor verification cannot fall through to task completion", () => {
  const pack = { status: "active", id: "tip-1", version: 1, content_hash: "pack-hash" };
  const completion = { id: "cr-worker", status: "done", pipeline_run_id: "pr-worker", instruction_pack_id: "tip-1", instruction_pack_version: 1, instruction_pack_hash: "pack-hash", created_at: "2026-01-01 00:00:00" };
  const integratedWorker = { id: "pr-worker", stage: "worker", status: "completed", integrated_at: "2026-01-01", integrated_patch_hash: "abc" };
  const base = { instruction_packs: [pack], completion_reports: [completion], work_item: { title: "Example", review_status: "passed" } };

  assert.equal(nextPipelineStage({ ...base, verification_reports: [{ completion_report_id: "cr-worker", status: "partial", created_at: "2026-01-02 00:00:00" }] }, [integratedWorker]), "autofix");
  const blocked = { ...base, verification_reports: [{ completion_report_id: "cr-worker", status: "blocked", summary: "PostgreSQL unavailable", created_at: "2026-01-02 00:00:00" }] };
  assert.equal(nextPipelineStage(blocked, [integratedWorker]), null);
  assert.match(pipelineVerificationBlockReason(blocked), /verification is blocked.*PostgreSQL unavailable/i);
});

test("autofix context carries exact failed verification evidence and forbids contract weakening", () => {
  const prompt = buildAutofixContext({ verification_reports: [{ status: "failed", summary: "API contract failed", items: [{ requirement_id: "REQ-7", status: "fail", evidence: "expected 409, got 500" }] }] });
  assert.match(prompt, /REQ-7: fail - expected 409, got 500/);
  assert.match(prompt, /not a fresh implementation or retry/i);
  assert.match(prompt, /Do not weaken tests, verification commands, acceptance criteria, or scope/);
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const stagePromptBody = source.slice(source.indexOf("function stagePrompt"), source.indexOf("function rejectedCandidatePatch"));
  assert.match(stagePromptBody, /stage === "autofix"[\s\S]+buildAutofixContext\(data\)/);
});

test("nextPipelineStage rejects completion reports without integrated worker evidence", () => {
  const ready = { instruction_packs: [{ status: "active" }], completion_reports: [{ id: "cr-stale", status: "done", pipeline_run_id: "pr-stale", created_at: "2026-01-01 00:00:00" }], work_item: { review_status: "pending" } };
  assert.equal(nextPipelineStage(ready, []), null);
  assert.equal(nextPipelineStage(ready, [{ id: "pr-stale", stage: "worker", status: "completed", integrated_at: "", integrated_patch_hash: "" }]), null);
  assert.match(pipelineIntegrationBlockReason(ready, []), /lacks integrated worker patch evidence/);
  assert.equal(nextPipelineStage({ ...ready, owner_decisions: [{ decision_type: "request_changes", related_type: "completion_report", related_id: "cr-stale" }] }, []), "worker");
  assert.equal(nextPipelineStage({ ...ready, owner_decisions: [{ decision_type: "request_changes", related_type: "", created_at: "2026-01-02 00:00:00" }] }, []), "worker");
});

test("pipelineWorkerBlockReason enforces worker prerequisites", () => {
  assert.match(pipelineWorkerBlockReason({ work_item: { title: "Missing pack" } }) || "", /exactly one active Task Instruction Pack/);
  assert.match(pipelineWorkerBlockReason({ work_item: { title: "Legacy" }, instruction_packs: [{ status: "active", content_schema_version: 2 }], dependencies: [] }) || "", /schema-v3.*effective contract/);
  assert.equal(pipelineWorkerBlockReason({ work_item: { title: "Ready" }, instruction_packs: [{ status: "active", content_schema_version: 3, skill_families_json: "[]", effective_contract_snapshot_id: "ecs-1", effective_contract_snapshot_hash: "hash-1" }], dependencies: [] }), null);
});

test("worker output is rejected when its effective contract is stale", () => {
  const run = { effective_contract_snapshot_id: "ecs-1", effective_contract_snapshot_hash: "hash-1" };
  assert.doesNotThrow(() => assertRunContractCurrent({ instruction_packs: [{ status: "active", effective_contract_snapshot_id: "ecs-1", effective_contract_snapshot_hash: "hash-1" }] }, run));
  assert.throws(() => assertRunContractCurrent({ instruction_packs: [{ status: "stale", effective_contract_snapshot_id: "ecs-1", effective_contract_snapshot_hash: "hash-1" }] }, run), /effective contract changed/);
});

test("pipelineSpawnParams maps one persisted claim to one owned subagent", () => {
  const task = { agent: "task-reviewer", task: "review", taskId: "t-1", skillFamilies: ["languages/golang"] };
  assert.deepEqual(pipelineSpawnParams("review", task, "/repo/project"), {
    agent: "task-reviewer",
    task: "review",
    cwd: "/repo/project",
    stage: "review",
    taskId: "t-1",
    skillFamilies: ["languages/golang"],
    acceptance: "attested",
    isolation: "worktree",
  });
  assert.deepEqual(
    { isolation: pipelineSpawnParams("worker", task, "/repo/project").isolation, acceptance: pipelineSpawnParams("worker", task, "/repo/project").acceptance },
    { isolation: "worktree", acceptance: "checked" },
  );
  assert.deepEqual(
    { isolation: pipelineSpawnParams("autofix", task, "/repo/project").isolation, acceptance: pipelineSpawnParams("autofix", task, "/repo/project").acceptance },
    { isolation: "worktree", acceptance: "checked" },
  );
});

test("formatPipelineStatus keeps task pipeline output compact", () => {
  const text = formatPipelineStatus({ task_id: "t-42", runs: [{ stage: "worker", status: "running", attempt: 2, subagent_run_id: "12345678-abcd", agent_model: "deepseek-v4-flash[1m]" }] });
  assert.equal(text, "Pipeline t-42\n- worker running attempt=2 run=12345678 model=deepseek-v4-flash[1m]");
  assert.doesNotMatch(text, /lease_token|async_dir|instruction_pack_hash/);
});

test("operator start reconciles bounded durable state without granting retry authority", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  assert.match(source, /if \(process\.env\.PI_TASK_PARENT_RUN_ID\) return scheduler/);
  const startBody = source.slice(source.indexOf("async start("), source.indexOf("status(taskId"));
  assert.match(startBody, /setImmediate\([\s\S]+await this\.reconcile\(\)[\s\S]+await this\.scheduleReady\(rootTaskId\)/);
  assert.match(startBody, /return \{ rootTaskId, status: "accepted" \}/);
  assert.doesNotMatch(startBody, /scheduleReady\(rootTaskId, true\)/);
  const statusBody = source.slice(source.indexOf("status(taskId"), source.indexOf("async stop("));
  assert.match(statusBody, /lastError/);
  assert.match(source, /stage === "worker" && explicitRetry[^\n]+--explicit-retry/);

  assert.match(source, /activeTaskIds[\s\S]+launchTaskIds = taskIds\.filter/);
  assert.match(source, /reconcileSafely/);
  assert.match(source, /deterministic contract failure requires TIP revision[\s\S]+sendUserMessage/);
});

test("automatic retry exhaustion is surfaced to the operator", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const reportError = source.slice(source.indexOf("private reportError("), source.indexOf("private reportProgress("));
  assert.doesNotMatch(reportError, /automatic worker retry limit reached[^\n]+return/);
  assert.match(reportError, /sendUserMessage/);
});

test("canonical worker circuit guidance keeps repair out of the application session", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const reportError = source.slice(source.indexOf("private reportError("), source.indexOf("private reportProgress("));
  assert.doesNotMatch(reportError, /worker circuit breaker[\s\S]+Effective Contract/);
  assert.match(reportError, /Do not modify the task-system extension from this application session/);
});

test("detached worker progress cannot throw through a stale session context", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const progressBody = source.slice(source.indexOf("private reportProgress("), source.indexOf("private notifyBlockedAttempt("));
  assert.match(progressBody, /try \{ this\.pi\.events\.emit/);
  assert.match(progressBody, /catch \{\}/);
  assert.match(progressBody, /try \{ ctx\.ui\.setStatus/);
});

test("scheduler worktree provisioning uses the asynchronous launch boundary", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  assert.match(source, /await prepareSubagentWorktree\(spec\.cwd, spec\.initialPatchPath, claim\.id\)/);
  assert.match(source, /spec\.preparedWorktree = prepared\.cwd/);
});

test("scheduler launches each ready Work Item at its persisted next stage", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const body = source.slice(source.indexOf("private async scheduleReady"), source.indexOf("private async launchGroup"));
  assert.match(body, /nextPipelineStage/);
  assert.doesNotMatch(body, /launchGroup\("worker", taskIds/);
});

test("parallel sibling reviews are invalidated when the integration base changes", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  assert.match(source, /"--base-commit", repositoryHead\(this\.cwd\)/);
  assert.match(source, /assertReviewBaseCurrent\(run, this\.cwd\)/);
  const repo = mkdtempSync(join(tmpdir(), "task-system-review-base-"));
  execFileSync("git", ["init", "-q"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "file.txt"), "base\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  const base = execFileSync("git", ["rev-parse", "HEAD"], { cwd: repo, encoding: "utf8" }).trim();
  assert.doesNotThrow(() => assertReviewBaseCurrent({ base_commit: base } as any, repo));
  writeFileSync(join(repo, "file.txt"), "changed\n");
  execFileSync("git", ["commit", "-am", "changed", "-q"], { cwd: repo });
  assert.throws(() => assertReviewBaseCurrent({ base_commit: base } as any, repo), /review base changed/);
  assert.match(source, /candidate patch no longer applies to the current integration base[\s\S]+checkpoint\(candidate, "advanced"/);
});

test("worker integration selects an artifact-saved candidate despite a blocked handoff", () => {
  const runs = [
    { id: "attempt-22", stage: "worker", status: "blocked", artifact_saved_at: "2026-01-01", advanced_at: "", integrated_at: "" },
    { id: "attempt-21", stage: "worker", status: "blocked", advanced_at: "", integrated_at: "" },
    { id: "attempt-20", stage: "worker", status: "completed", artifact_saved_at: "2026-01-01", advanced_at: "", integrated_at: "" },
    { id: "attempt-19", stage: "worker", status: "completed", artifact_saved_at: "2026-01-01", advanced_at: "", integrated_at: "" },
  ] as any;
  assert.equal(workerIntegrationCandidate(runs)?.id, "attempt-22");
});

test("review-fix workers must change the rejected candidate patch", () => {
  const patch = Buffer.from("reviewed candidate");
  const hash = createHash("sha256").update(patch).digest("hex");
  assert.throws(
    () => assertReviewFixChangedPatch({ review_fix_cycle: 1, candidate_patch_hash: hash } as any, patch),
    /review-fix produced the unchanged rejected candidate patch/,
  );
  assert.doesNotThrow(() => assertReviewFixChangedPatch({ review_fix_cycle: 1, candidate_patch_hash: "different" } as any, patch));
  assert.doesNotThrow(() => assertReviewFixChangedPatch({ review_fix_cycle: 0, candidate_patch_hash: hash } as any, patch));
});

test("failed review correction prompt requires a changed patch", () => {
  const prompt = buildWorkerCorrectionContext({ work_item: { review_status: "failed", review_notes: "Fix the import preview error" } });
  assert.match(prompt, /review-fix run/);
  assert.match(prompt, /non-empty patch whose SHA-256 differs from the rejected candidate/);
});
test("completed worker candidates transition directly to review and are never downgraded", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const finishBody = source.slice(source.indexOf("private async finish"), source.indexOf("private async continueWorkerGroup"));
  const groupBody = source.slice(source.indexOf("private async continueWorkerGroup"), source.indexOf("private integrateReviewedCandidate"));
  assert.match(groupBody, /await this\.launchGroup\("review", \[entry\.task_id\]\)/);
  assert.doesNotMatch(groupBody, /await this\.advance\(run\.task_id\)/);
  assert.match(finishBody, /if \(persisted\?\.status === "completed" && persisted\.artifact_saved_at\)/);
});

test("Git recovery path parsing normalizes rename destinations", () => {
  assert.deepEqual([...parseApplyNumstatPaths("0\t0\tnew.ts\0")], ["new.ts"]);
  assert.deepEqual([...parsePorcelainPaths("R  new.ts\0old.ts\0")], ["new.ts"]);
});

test("reviewed integration rejects extra edits in a candidate file", () => {
  const repo = mkdtempSync(join(tmpdir(), "task-system-reviewed-index-"));
  try {
    execFileSync("git", ["init", "-q"], { cwd: repo });
    execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
    execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
    writeFileSync(join(repo, "file.txt"), "base\n");
    execFileSync("git", ["add", "file.txt"], { cwd: repo });
    execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
    writeFileSync(join(repo, "file.txt"), "reviewed\n");
    const patch = join(repo, "reviewed.patch");
    writeFileSync(patch, execFileSync("git", ["diff", "--binary", "HEAD", "--", "."], { cwd: repo }));
    execFileSync("git", ["reset", "--hard", "-q", "HEAD"], { cwd: repo });
    execFileSync("git", ["apply", "--index", patch], { cwd: repo });
    writeFileSync(join(repo, "file.txt"), "reviewed\nunreviewed\n");
    execFileSync("git", ["add", "file.txt"], { cwd: repo });
    assert.throws(() => assertIndexMatchesReviewedPatch(patch, repo), /differs from reviewed candidate/);
  } finally {
    rmSync(repo, { recursive: true, force: true });
  }
});

test("restart recognizes an already committed reviewed patch", () => {
  const repo = mkdtempSync(join(tmpdir(), "task-system-review-restart-"));
  try {
    execFileSync("git", ["init", "-q"], { cwd: repo });
    execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
    execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
    writeFileSync(join(repo, "file.txt"), "base\n");
    execFileSync("git", ["add", "file.txt"], { cwd: repo });
    execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
    writeFileSync(join(repo, "file.txt"), "reviewed\n");
    const patch = join(repo, "reviewed.patch");
    writeFileSync(join(repo, ".git", "info", "exclude"), "reviewed.patch\n");
    writeFileSync(patch, execFileSync("git", ["diff", "--binary", "HEAD", "--", "."], { cwd: repo }));
    execFileSync("git", ["reset", "--hard", "-q", "HEAD"], { cwd: repo });
    execFileSync("git", ["apply", "--index", patch], { cwd: repo });
    execFileSync("git", ["commit", "-qm", "reviewed"], { cwd: repo });
    const before = execFileSync("git", ["rev-list", "--count", "HEAD"], { cwd: repo, encoding: "utf8" }).trim();
    assert.equal(recoverReviewedPatch(patch, repo, "reviewed"), true);
    assert.equal(recoverReviewedPatch(patch, repo, "reviewed"), true);
    assert.equal(execFileSync("git", ["rev-list", "--count", "HEAD"], { cwd: repo, encoding: "utf8" }).trim(), before);
  } finally {
    rmSync(repo, { recursive: true, force: true });
  }
});

test("clean recovery rejects a coincidental commit", () => {
  const repo = mkdtempSync(join(tmpdir(), "task-system-review-provenance-"));
  try {
    execFileSync("git", ["init", "-q"], { cwd: repo });
    execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
    execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
    writeFileSync(join(repo, "file.txt"), "base\n");
    execFileSync("git", ["add", "file.txt"], { cwd: repo });
    execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
    writeFileSync(join(repo, "file.txt"), "reviewed\n");
    const patch = join(repo, "reviewed.patch");
    writeFileSync(join(repo, ".git", "info", "exclude"), "reviewed.patch\n");
    writeFileSync(patch, execFileSync("git", ["diff", "--binary", "HEAD", "--", "."], { cwd: repo }));
    execFileSync("git", ["add", "file.txt"], { cwd: repo });
    execFileSync("git", ["commit", "-qm", "manual commit"], { cwd: repo });
    assert.throws(() => recoverReviewedPatch(patch, repo, "task-system: integrate reviewed worker run-1"), /reviewed integration commit/);
  } finally {
    rmSync(repo, { recursive: true, force: true });
  }
});

test("scheduler integration finalizer checkpoints once across post-commit restart", () => {
  const repo = mkdtempSync(join(tmpdir(), "task-system-review-finalizer-"));
  try {
    execFileSync("git", ["init", "-q"], { cwd: repo });
    execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
    execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
    writeFileSync(join(repo, "file.txt"), "base\n");
    execFileSync("git", ["add", "file.txt"], { cwd: repo });
    execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
    writeFileSync(join(repo, "file.txt"), "reviewed\n");
    const patch = join(repo, "reviewed.patch");
    const commitMessage = "task-system: integrate reviewed worker run-1";
    writeFileSync(join(repo, ".git", "info", "exclude"), "reviewed.patch\n");
    writeFileSync(patch, execFileSync("git", ["diff", "--binary", "HEAD", "--", "."], { cwd: repo }));
    execFileSync("git", ["add", "file.txt"], { cwd: repo });
    execFileSync("git", ["commit", "-qm", commitMessage], { cwd: repo });
    let integrated = false;
    let checkpoints = 0;
    const checkpoint = () => { checkpoints++; integrated = true; };
    finalizeReviewedIntegration({ patch, cwd: repo, commitMessage, integrated, checkpoint });
    finalizeReviewedIntegration({ patch, cwd: repo, commitMessage, integrated, checkpoint });
    assert.equal(checkpoints, 1);
  } finally {
    rmSync(repo, { recursive: true, force: true });
  }
});

test("integration publication does not run mutating pre-commit hooks", () => {
  const repo = mkdtempSync(join(tmpdir(), "task-system-review-hook-"));
  try {
    execFileSync("git", ["init", "-q"], { cwd: repo });
    execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
    execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
    writeFileSync(join(repo, "file.txt"), "base\n");
    execFileSync("git", ["add", "file.txt"], { cwd: repo });
    execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
    writeFileSync(join(repo, "file.txt"), "reviewed\n");
    const patch = join(repo, "reviewed.patch");
    const commitMessage = "task-system: integrate reviewed worker run-1";
    writeFileSync(join(repo, ".git", "info", "exclude"), "reviewed.patch\n.pi/tasks.db*\n.pi-subagents/\n");
    writeFileSync(patch, execFileSync("git", ["diff", "--binary", "HEAD", "--", "."], { cwd: repo }));
    execFileSync("git", ["reset", "--hard", "-q", "HEAD"], { cwd: repo });
    writeFileSync(join(repo, ".git", "hooks", "pre-commit"), "#!/bin/sh\nprintf 'hooked\\n' >> file.txt\ngit add file.txt\n", { mode: 0o755 });
    const originalHead = execFileSync("git", ["rev-parse", "HEAD"], { cwd: repo, encoding: "utf8" }).trim();
    let checkpoints = 0;
    finalizeReviewedIntegration({ patch, cwd: repo, commitMessage, integrated: false, checkpoint: () => { checkpoints++; } });
    assert.equal(checkpoints, 1);
    assert.notEqual(execFileSync("git", ["rev-parse", "HEAD"], { cwd: repo, encoding: "utf8" }).trim(), originalHead);
    assert.equal(readFileSync(join(repo, "file.txt"), "utf8"), "reviewed\n");
    assert.equal(execFileSync("git", ["status", "--porcelain"], { cwd: repo, encoding: "utf8" }).trim(), "");
  } finally {
    rmSync(repo, { recursive: true, force: true });
  }
});

test("integration publication does not run commit-spawning post-commit hooks", () => {
  const repo = mkdtempSync(join(tmpdir(), "task-system-review-post-hook-"));
  try {
    execFileSync("git", ["init", "-q"], { cwd: repo });
    execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
    execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
    writeFileSync(join(repo, "file.txt"), "base\n");
    execFileSync("git", ["add", "file.txt"], { cwd: repo });
    execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
    const originalHead = execFileSync("git", ["rev-parse", "HEAD"], { cwd: repo, encoding: "utf8" }).trim();
    writeFileSync(join(repo, "file.txt"), "reviewed\n");
    const patch = join(repo, "reviewed.patch");
    const commitMessage = "task-system: integrate reviewed worker run-1";
    writeFileSync(join(repo, ".git", "info", "exclude"), "reviewed.patch\n.pi/tasks.db*\n.pi-subagents/\n");
    writeFileSync(patch, execFileSync("git", ["diff", "--binary", "HEAD", "--", "."], { cwd: repo }));
    execFileSync("git", ["reset", "--hard", "-q", "HEAD"], { cwd: repo });
    writeFileSync(join(repo, ".git", "hooks", "post-commit"), "#!/bin/sh\nrm .git/hooks/post-commit\nprintf 'extra\\n' > extra.txt\ngit add extra.txt\ngit commit -qm 'hook commit'\n", { mode: 0o755 });
    let checkpoints = 0;
    finalizeReviewedIntegration({ patch, cwd: repo, commitMessage, integrated: false, checkpoint: () => { checkpoints++; } });
    assert.equal(checkpoints, 1);
    assert.equal(execFileSync("git", ["log", "-1", "--format=%s"], { cwd: repo, encoding: "utf8" }).trim(), commitMessage);
    assert.equal(execFileSync("git", ["rev-list", "--count", `${originalHead}..HEAD`], { cwd: repo, encoding: "utf8" }).trim(), "1");
    assert.equal(execFileSync("git", ["status", "--porcelain"], { cwd: repo, encoding: "utf8" }).trim(), "");
  } finally {
    rmSync(repo, { recursive: true, force: true });
  }
});

test("integration publication does not run dirtying post-commit hooks", () => {
  const repo = mkdtempSync(join(tmpdir(), "task-system-review-dirty-hook-"));
  try {
    execFileSync("git", ["init", "-q"], { cwd: repo });
    execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
    execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
    writeFileSync(join(repo, "file.txt"), "base\n");
    execFileSync("git", ["add", "file.txt"], { cwd: repo });
    execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
    const originalHead = execFileSync("git", ["rev-parse", "HEAD"], { cwd: repo, encoding: "utf8" }).trim();
    writeFileSync(join(repo, "file.txt"), "reviewed\n");
    const patch = join(repo, "reviewed.patch");
    const commitMessage = "task-system: integrate reviewed worker run-1";
    writeFileSync(join(repo, ".git", "info", "exclude"), "reviewed.patch\n.pi/tasks.db*\n.pi-subagents/\n");
    writeFileSync(patch, execFileSync("git", ["diff", "--binary", "HEAD", "--", "."], { cwd: repo }));
    execFileSync("git", ["reset", "--hard", "-q", "HEAD"], { cwd: repo });
    writeFileSync(join(repo, ".git", "hooks", "post-commit"), "#!/bin/sh\nprintf 'side effect\\n' > hook-output.txt\n", { mode: 0o755 });
    let checkpoints = 0;
    finalizeReviewedIntegration({ patch, cwd: repo, commitMessage, integrated: false, checkpoint: () => { checkpoints++; } });
    assert.equal(checkpoints, 1);
    assert.notEqual(execFileSync("git", ["rev-parse", "HEAD"], { cwd: repo, encoding: "utf8" }).trim(), originalHead);
    assert.equal(execFileSync("git", ["status", "--porcelain"], { cwd: repo, encoding: "utf8" }).trim(), "");
  } finally {
    rmSync(repo, { recursive: true, force: true });
  }
});

test("worker scope reports root drift and approval-required files", () => {
  assert.deepEqual(validateWorkerChangedFiles(["src/a.ts", "docs/note.md", "package.json"], {
    scope_roots: ["src"],
    approval_required: ["package.json"],
  }), { unexpected: ["docs/note.md", "package.json"] });
});
test("worker integration excludes completed sibling phase tasks", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  assert.match(source, /taskData\?\.work_item\?\.status === "done"\) return \[\]/);
});

test("passed top-level review stops at executable verification gate", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  assert.doesNotMatch(source, /review_status === "passed" && !data\.work_item\?\.parent_id[\s\S]*scheduleReady\(taskId/);
});

test("passed review delivers the executable contractor verification handoff", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const advance = source.slice(source.indexOf("private async advance("), source.indexOf("private async resumePending("));
  assert.match(advance, /buildTaskVerifyPrompt/);
  assert.match(advance, /sendUserMessage[\s\S]+deliverAs: "followUp"/);
});

test("aggregate delivery merges the verified head to develop exactly once", () => {
  const root = mkdtempSync(join(tmpdir(), "task-aggregate-merge-"));
  const repo = join(root, "repo");
  const remote = join(root, "remote.git");
  mkdirSync(repo);
  execFileSync("git", ["init", "-q", "-b", "develop"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "base.txt"), "base\n");
  writeFileSync(join(repo, ".gitignore"), ".pi/tasks.db*\n.pi-subagents/\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  const base = execFileSync("git", ["rev-parse", "HEAD"], { cwd: repo, encoding: "utf8" }).trim();
  execFileSync("git", ["init", "-q", "--bare", remote]);
  execFileSync("git", ["remote", "add", "origin", remote], { cwd: repo });
  execFileSync("git", ["push", "-qu", "origin", "develop"], { cwd: repo });
  execFileSync("git", ["switch", "-qc", "feature/delivery"], { cwd: repo });
  writeFileSync(join(repo, "feature.txt"), "feature\n");
  execFileSync("git", ["add", "."], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "feature"], { cwd: repo });
  const head = execFileSync("git", ["rev-parse", "HEAD"], { cwd: repo, encoding: "utf8" }).trim();
  const state = { work_item_id: "wi-feature", branch_name: "feature/delivery", base_branch: "develop", base_commit: base, verified_head: head };

  const merged = mergeAggregateBranch(repo, state);
  assert.equal(execFileSync("git", ["rev-list", "--count", `${base}..${merged}`], { cwd: repo, encoding: "utf8" }).trim(), "2");
  assert.equal(mergeAggregateBranch(repo, state), merged);
});

test("hybrid scheduler records use canonical Work Item lifecycle mutations", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");

  assert.doesNotMatch(source, /execPic\(\["task", (?:"update"|"status")/);
  assert.equal(source.match(/execPic\(\["work-item", "review"/g)?.length, 2);
  assert.equal(source.match(/execPic\(\["work-item", "status"/g)?.length, 2);
  assert.match(source, /withInheritedParentWorkflowArtifacts/);
});

test("worker claims launch implementation directly", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  assert.doesNotMatch(source, /task-worker-preflight|parsePreflightReport|persistPreflightResult/);
  assert.match(source, /agent: stageAgent\(stage\)/);
  assert.match(source, /pipelineRunIds: claims\.map/);
  assert.match(source, /subagentRunIds/);
  assert.doesNotMatch(source, /recoverOrphanedRuns/);
});

test("operator stop persists cancellation before stopping runtime and terminal failures do not respawn", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const stopBody = source.slice(source.indexOf("async stop("), source.indexOf("private async scheduleReady"));

  assert.ok(stopBody.indexOf('"pipeline-complete"') < stopBody.indexOf("agentHandles.get"));
  assert.doesNotMatch(source, /status !== "completed"\) \{[\s\S]{0,300}scheduleReady/);
  assert.doesNotMatch(source, /childStatus !== "complete"[\s\S]{0,500}continueWorkerGroup/);
});

test("failed review advances directly into the correction worker loop", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const resumeBody = source.slice(source.indexOf("private async resumePending"), source.indexOf("private pipelineRuns"));
  assert.ok(resumeBody.lastIndexOf('checkpoint(run, "advanced"') < resumeBody.lastIndexOf("await this.advance"));
  const advanceBody = source.slice(source.indexOf("private async advance"), source.indexOf("private async resumePending"));
  assert.match(advanceBody, /nextPipelineStage\(data, this\.pipelineRuns\(taskId\)\)/);
  assert.doesNotMatch(advanceBody, /next === "worker" && data\.task\?\.review_status === "failed"\) return/);
});

test("pipeline checkpoints use process status instead of a run payload error field", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const body = source.slice(source.indexOf("function checkpoint("), source.indexOf("function saveWorkerReport("));
  assert.match(body, /execPicText\(args, cwd\)/);
  assert.doesNotMatch(body, /result\.error/);
});

test("Git index writes retry transient lock contention and pauses are delivered", () => {
  const helpers = readFileSync(new URL("../core/cli-helpers.ts", import.meta.url), "utf8");
  const scheduler = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  assert.match(helpers, /attempt >= 12 \|\| !message\.includes\("index\.lock"\)/);
  assert.match(helpers, /Atomics\.wait\([^;]+250\)/);
  assert.match(scheduler, /sendUserMessage\(`Async pipeline paused:/);
});

test("DONE worker reports promote only after reviewed integration", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const groupBody = source.slice(source.indexOf("private async continueWorkerGroup"), source.indexOf("private integrateReviewedCandidate"));
  assert.doesNotMatch(groupBody, /saveWorkerReport\(/);
  assert.doesNotMatch(groupBody, /checkpoint\(entry, "integrated"/);
  const integrationBody = source.slice(source.indexOf("private integrateReviewedCandidate"), source.indexOf("private async advance"));
  assert.match(integrationBody, /checkpoint\(workerRun, "integrated"/);
  assert.ok(integrationBody.indexOf("integrateReviewedCandidate") < integrationBody.indexOf("promoteReviewedCandidate"));
  assert.match(integrationBody, /saveWorkerReport\(run/);
  assert.doesNotMatch(groupBody, /--3way/);
});

test("Reviewer context reads durable candidate artifacts without a Completion Report", () => {
  const source = readFileSync(new URL("../tasking/settings.ts", import.meta.url), "utf8");
  const body = source.slice(source.indexOf("export function buildReviewContext"), source.indexOf("// ── Shared interfaces"));
  assert.doesNotMatch(body, /completion_reports/);
  assert.match(body, /candidate\.async_dir.*output-/s);
  assert.match(body, /candidate\.integrated_patch_path/);
  assert.match(body, /Candidate patch evidence hash mismatch/);
  assert.match(body, /patch\.length === 0/);
});

test("review verdict is durable before restart-safe candidate integration", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const finishBody = source.slice(source.indexOf("private async finish"), source.indexOf("private async continueWorkerGroup"));
  assert.ok(finishBody.indexOf('"pipeline-complete"') < finishBody.indexOf("integrateReviewedCandidate"));
  const pendingBody = source.slice(source.indexOf("private async resumePending"), source.indexOf("private pipelineRuns"));
  assert.match(pendingBody, /persistedReviewOutcome/);
  assert.match(pendingBody, /integrateReviewedCandidate/);
});

test("integration stages only reviewed patch changes and orphan recovery retires unbound claims", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const integrationBody = source.slice(source.indexOf("private integrateReviewedCandidate"), source.indexOf("private async advance"));
  assert.match(integrationBody, /finalizeReviewedIntegration\(/);
  const finalizerBody = source.slice(source.indexOf("export function finalizeReviewedIntegration"), source.indexOf("function assertCleanGit"));
  assert.match(finalizerBody, /git", \["apply", "--index"/);
  assert.doesNotMatch(integrationBody, /execGitIndexWrite\(\["add", "-A"\]/);
  const recoveryBody = source.slice(source.indexOf("private recoverOrphanedRuns"), source.indexOf("stopSession"));
  assert.doesNotMatch(recoveryBody, /if \(!pid && run\.status !== "running"\) continue/);
});

test("session startup performs no pipeline I/O", async () => {
  const pi = { events: { on: () => () => {} } } as any;
  const scheduler = new PipelineScheduler(pi) as any;
  let recovered = 0;
  let reconciled = 0;
  scheduler.recoverOrphanedRuns = () => { recovered++; };
  scheduler.reconcileSafely = async () => { reconciled++; };
  scheduler.startSession({ cwd: "/repo" } as any);
  assert.equal(scheduler.cwd, "/repo");
  assert.equal(recovered, 0);
  assert.equal(reconciled, 0);
  await new Promise<void>((resolve) => setImmediate(resolve));
  assert.equal(recovered, 0);
  assert.equal(reconciled, 0);
});

test("worker completion yields before synchronous artifact reconciliation", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  const body = source.slice(source.indexOf("private async persistAgentResult"), source.indexOf("private async reconcileSafely"));
  assert.match(body, /await new Promise<void>\(\(resolve\) => setImmediate\(resolve\)\)/);
  assert.match(body, /this\.queueReconcile\(\)/);
  assert.match(body, /setImmediate\(\(\) => \{ void this\.reconcileSafely\(\); \}\)/);
});

test("owned runner completion persists output and Task-specific worktree patch evidence", async () => {
  const repo = mkdtempSync(join(tmpdir(), "task-system-owned-"));
  execFileSync("git", ["init", "-q", "-b", "master"], { cwd: repo });
  execFileSync("git", ["config", "user.email", "test@example.com"], { cwd: repo });
  execFileSync("git", ["config", "user.name", "Test"], { cwd: repo });
  writeFileSync(join(repo, "file.txt"), "base\n");
  execFileSync("git", ["add", "file.txt"], { cwd: repo });
  execFileSync("git", ["commit", "-qm", "base"], { cwd: repo });
  const runId = `agent-${Date.now()}`;
  const worktree = join(tmpdir(), `pi-task-worktree-${runId}`);
  execFileSync("git", ["worktree", "add", "-qb", `pi-agent-${runId}`, worktree], { cwd: repo });
  writeFileSync(join(worktree, "file.txt"), "changed\n");
  writeFileSync(join(worktree, "new.txt"), "new\n");
  mkdirSync(join(worktree, "test-results"));
  writeFileSync(join(worktree, "test-results", ".last-run.json"), "{}\n");

  const pi = { events: { on: () => () => {}, emit: () => {} } } as any;
  const scheduler = new PipelineScheduler(pi) as any;
  const artifactDir = join(repo, ".pi-subagents", "pipeline", "pr-1");
  mkdirSync(artifactDir, { recursive: true });
  scheduler.cwd = repo;
  scheduler.agentRuns.set(runId, { id: "pr-1", task_id: "t-1", stage: "worker", lease_token: "lease", async_dir: artifactDir, child_index: 0 });
  await scheduler.persistAgentResult({
    runId,
    agent: "task-worker",
    task: "work",
    exitCode: 0,
    messages: [{ role: "assistant", content: [{ type: "text", text: "## COMPLETION REPORT\n\n**STATUS:** DONE\n\n**FILES CHANGED:**\n- file.txt\n**TEST RESULTS:**\n- PASS\n**ISSUES DISCOVERED:**\n- None\n**DEVIATIONS FROM SPEC:**\n- None\n**SUGGESTIONS FOR CHỦ THẦU:**\n- None" }] }],
    stderr: "",
    usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, cost: 0, contextTokens: 0, turns: 1 },
    workspace: {
      assignedWorktree: worktree, childProcessCwd: worktree, bashCwd: worktree,
      readToolRoot: worktree, editToolRoot: worktree, writeToolRoot: worktree, applyPatchRoot: worktree,
      gitToplevel: worktree, head: execFileSync("git", ["rev-parse", "HEAD"], { cwd: worktree, encoding: "utf8" }).trim(),
      statusBefore: "", statusAfter: " M file.txt\n?? new.txt", diffStatAfter: "file.txt | 2 +-",
    },
  });

  assert.match(readFileSync(join(artifactDir, "output-0.log"), "utf8"), /COMPLETION REPORT/);
  const patch = readFileSync(join(artifactDir, "worktree-diffs", "task-0-task-worker.patch"), "utf8");
  assert.match(patch, /changed/);
  assert.match(patch, /new\.txt/);
  assert.doesNotMatch(patch, /test-results/);
  assert.equal(JSON.parse(readFileSync(join(artifactDir, "status.json"), "utf8")).state, "completed");
});

test("worker scope only blocks protected task-system paths", () => {
  assert.throws(() => validateWorkerChangedFiles([".pi/tasks.db"], { scope_roots: ["."] }), /protected path/);
  assert.deepEqual(validateWorkerChangedFiles(["package-lock.json", "docs/note.md"], {}), { unexpected: [] });
});

test("generated verification artifacts are excluded from patches and scope checks", () => {
  assert.deepEqual(filterGeneratedFiles([
    "src/main.ts",
    "test-results/.last-run.json",
    "playwright-report/index.html",
    "custom-output/result.json",
  ], { generated_files: ["custom-output/**"] }), {
    changedFiles: ["src/main.ts"],
    generatedFiles: ["test-results/.last-run.json", "playwright-report/index.html", "custom-output/result.json"],
  });
});

test("blocked worker attempts proactively queue their terminal outcome", () => {
  const messages: Array<{ text: string; options: any }> = [];
  const scheduler = new PipelineScheduler({
    sendUserMessage: (text: string, options: any) => messages.push({ text, options }),
    events: { on: () => () => {} },
  } as any) as any;

  scheduler.notifyBlockedAttempt({ task_id: "T01", stage: "worker", attempt: 3 }, "required verification did not pass: npm test");

  assert.equal(messages.length, 1);
  assert.match(messages[0]!.text, /T01 worker attempt 3 is blocked/);
  assert.match(messages[0]!.text, /required verification/);
  assert.match(messages[0]!.text, /No patch was integrated/);
  assert.match(messages[0]!.text, /worker or runner issue/);
  assert.deepEqual(messages[0]!.options, { deliverAs: "followUp" });
});

test("pipeline failures preserve actionable non-scope classifications", () => {
  assert.deepEqual(pipelineFailureResult("pipeline bind rejected: stale or invalid lease"), { failure_code: "runner_protocol_invalid" });

  assert.deepEqual(pipelineFailureResult("required verification did not pass: docker exec qgis-postgres-1 psql"), { failure_code: "environment_blocked" });
  assert.deepEqual(pipelineFailureResult("required verification did not pass: npm test"), { failure_code: "worker_output_invalid" });
  assert.deepEqual(pipelineFailureResult("worker artifact invalid: empty patch"), { failure_code: "worker_artifact_invalid" });
  assert.deepEqual(pipelineFailureResult("autofix made no repository changes"), { failure_code: "no_progress_autofix" });
  assert.deepEqual(pipelineFailureResult("tests failed"), {});
});

test("worker artifact validation rejects an empty patch that claims changed files", () => {
  const dir = mkdtempSync(join(tmpdir(), "task-worker-artifact-"));
  const patch = join(dir, "worker.patch");
  const output = join(dir, "output.log");
  writeFileSync(patch, "");
  writeFileSync(output, "completion report");

  assert.throws(
    () => validateWorkerPatchArtifact(patch, output, { changedFiles: ["main.go"] }),
    (error: Error) => error.message.includes("artifact invalid") && error.message.includes(output) && error.message.includes(patch),
  );
  assert.doesNotThrow(() => validateWorkerPatchArtifact(patch, output, { changedFiles: [] }));
});

test("worker provenance is bound by the pipeline claim instead of report hash repetition", () => {
  const source = readFileSync(new URL("./pipeline-scheduler.ts", import.meta.url), "utf8");
  assert.doesNotMatch(source, new RegExp(["subagents", "rpc"].join(":")));
  assert.doesNotMatch(source, new RegExp(["pi", "subagents", "manager"].join("-")));
  assert.match(source, /completion-save.*--pipeline-run-id/s);
});

test("parseTaskCompletionReport distinguishes Task outcome from runtime acceptance", () => {
  const report = parseTaskCompletionReport(`## COMPLETION REPORT — TIP-001 v1\n\n**STATUS:** PARTIAL\n\n**FILES CHANGED:**\n- None\n**TEST RESULTS:**\n- FAIL\n**ISSUES DISCOVERED:**\n- blocker\n**DEVIATIONS FROM SPEC:**\n- None\n**SUGGESTIONS FOR CHỦ THẦU:**\n- decide`);
  assert.equal(report.status, "partial");
  assert.throws(() => parseTaskCompletionReport("**STATUS:** DONE"), /FILES CHANGED/);
});

test("DONE worker output requires passing verification and canonicalizes changed files", () => {
  assert.deepEqual(validateWorkerOutput("done", ["src/main.ts"], {}), { reported: ["src/main.ts"], actual: ["src/main.ts"], mismatch: false });
});

test("failed review corrections are handed to worker", () => {
  assert.equal(buildWorkerCorrectionContext({ work_item: { review_status: "pending" } }), "");
  const context = buildWorkerCorrectionContext({ work_item: { review_status: "failed", review_notes: "Wire expectedVersion" } });
  assert.match(context, /REVIEW CORRECTIONS/);
  assert.match(context, /Wire expectedVersion/);
  assert.match(context, /already applied to the assigned worktree/);
});
