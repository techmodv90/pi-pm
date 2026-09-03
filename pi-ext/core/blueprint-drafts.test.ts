import assert from "node:assert/strict";
import { chmodSync, existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import { blueprintPlanPath, deleteBlueprintDraft, deletePlanReviewState, loadBlueprintDraft, loadLatestBlueprintDraft, loadPlanReviewState, planApprovalGate, PLANNOTATOR_REQUEST_CHANNEL, recoverPlanReviewResult, requestPlanReview, saveBlueprintDraft, writeBlueprintPlan, type PlannotatorEvents } from "./blueprint-drafts.ts";
import { parseBlueprintReportJson, renderBlueprintReportMarkdown } from "../reporting/blueprint-report.ts";
import { registerTaskManagerTool } from "../api/tool.ts";

function tempRoot(prefix = "pic-blueprint-"): string {
  return mkdtempSync(join(tmpdir(), prefix));
}

test("Blueprint drafts persist under the project root and can be replaced and deleted", () => {
  const root = tempRoot();
  try {
    const first = saveBlueprintDraft(root, "wi-abc123", '{"version":1}');
    const path = join(root, ".pi", "runtime", "blueprint", "wi-abc123.json");
    assert.equal(JSON.parse(readFileSync(path, "utf8")).state.content, '{"version":1}');
    assert.equal(statSync(path).mode & 0o777, 0o600);
    assert.equal(loadBlueprintDraft(root, "wi-abc123", first.draftId).reviewed, false);
    assert.equal(loadLatestBlueprintDraft(root, "wi-abc123").draftId, first.draftId);

    const reviewed = saveBlueprintDraft(root, "wi-abc123", '{"version":2}', { architecture: true });
    assert.notEqual(reviewed.draftId, first.draftId);
    assert.equal(loadBlueprintDraft(root, "wi-abc123", reviewed.draftId).reviewed, true);
    assert.throws(() => loadBlueprintDraft(root, "wi-abc123", first.draftId), /stale/);

    deleteBlueprintDraft(root, "wi-abc123");
    assert.throws(() => loadBlueprintDraft(root, "wi-abc123", reviewed.draftId));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("Plan projection rejects unsafe Work Item IDs before path construction and persists at the stable plan path", () => {
  const root = tempRoot("pic-blueprint-plan-");
  try {
    for (const unsafe of ["wi-../escape", "WI-ABC", "wi-abc/def", "", "other-id"]) {
      assert.throws(() => blueprintPlanPath(root, unsafe), /Invalid Work Item ID for plan projection/);
    }
    const path = writeBlueprintPlan(root, "wi-abc123", "# BLUEPRINT: rendered");
    assert.equal(path, join(root, ".pi", "artifacts", "plans", "wi-abc123.md"));
    assert.equal(readFileSync(path, "utf8"), "# BLUEPRINT: rendered");
    assert.equal(statSync(path).mode & 0o777, 0o600);
    assert.equal(statSync(dirname(path)).mode & 0o777, 0o700);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("Plan projection rewrites the same path on every rejection-loop revision", () => {
  const root = tempRoot("pic-blueprint-plan-rev-");
  try {
    const first = writeBlueprintPlan(root, "wi-abc123", "# PLAN v1");
    const second = writeBlueprintPlan(root, "wi-abc123", "# PLAN v2");
    assert.equal(first, second);
    assert.equal(readFileSync(second, "utf8"), "# PLAN v2");
    // Atomic replacement leaves no temporary residue and never the old content.
    assert.deepEqual(readdirSync(dirname(second)).filter((name) => name.endsWith(".tmp")), []);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("writeBlueprintPlan keeps the prior plan bytes intact when preparation fails before the terminal rename", () => {
  const root = tempRoot("pic-blueprint-plan-fail-");
  try {
    writeBlueprintPlan(root, "wi-abc123", "# PLAN v1");
    // Failure injection: an unwritable plans directory makes the temporary
    // write (the last fallible step before the terminal rename) fail.
    const plansDir = join(root, ".pi", "artifacts", "plans");
    chmodSync(plansDir, 0o500);
    try {
      assert.throws(() => writeBlueprintPlan(root, "wi-abc123", "# PLAN v2"));
      assert.equal(readFileSync(join(plansDir, "wi-abc123.md"), "utf8"), "# PLAN v1", "the prior plan bytes must survive a failed replacement");
    } finally {
      chmodSync(plansDir, 0o700);
    }
    // Restored permissions let the replacement proceed on the same path.
    assert.equal(readFileSync(writeBlueprintPlan(root, "wi-abc123", "# PLAN v2"), "utf8"), "# PLAN v2");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("writeBlueprintPlan commits with renameSync as its terminal filesystem operation", () => {
  const source = readFileSync(new URL("./blueprint-drafts.ts", import.meta.url), "utf8");
  const body = source.slice(source.indexOf("export function writeBlueprintPlan"));
  const fn = body.slice(0, body.indexOf("\n}"));
  const renameCall = "renameSync(temporary, path)";
  const renameAt = fn.indexOf(renameCall);
  assert.ok(renameAt > fn.indexOf("chmodSync(temporary, 0o600)"), "the mode hardening must happen on the temporary file before the commit");
  assert.ok(renameAt > fn.indexOf("writeFileSync"), "the temp write must happen before the commit");
  assert.doesNotMatch(fn.slice(renameAt + renameCall.length), /Sync\(/, "no filesystem operation may follow the terminal rename");
  assert.ok(fn.trimEnd().endsWith("return path;"));
});

// ── Plannotator plan-review runtime (plannotator:request channel) ────────────

type PlannotatorRequest = { action: string; payload: any; respond: (response: unknown) => void };

function fakePlannotatorEvents(responder?: (request: PlannotatorRequest) => void): PlannotatorEvents & { requests: PlannotatorRequest[] } {
  const requests: PlannotatorRequest[] = [];
  return {
    requests,
    emit(channel: string, data: unknown) {
      if (channel !== PLANNOTATOR_REQUEST_CHANNEL) return;
      const request = data as PlannotatorRequest;
      requests.push({ action: request.action, payload: request.payload, respond: request.respond });
      responder?.(request);
    },
  };
}

function planReviewResponder(outcome: () => unknown): (request: PlannotatorRequest) => void {
  return (request) => {
    if (request.action === "plan-review") request.respond({ status: "handled", result: { status: "pending", reviewId: "rev-1" } });
    else if (request.action === "review-status") request.respond({ status: "handled", result: outcome() });
  };
}

test("requestPlanReview persists the extension review identity and the hard gate blocks while pending", async () => {
  const root = tempRoot("pic-blueprint-review-");
  const bus = fakePlannotatorEvents(planReviewResponder(() => ({ status: "pending" })));
  try {
    const state = await requestPlanReview(bus, root, "wi-review1", "# PLAN v1");
    const request = bus.requests.find((entry) => entry.action === "plan-review");
    assert.ok(request, "a plannotator:request plan-review request must be emitted");
    assert.equal(request.payload.planContent, "# PLAN v1");
    assert.equal(request.payload.planFilePath, blueprintPlanPath(root, "wi-review1"));
    assert.equal(state.status, "pending");
    assert.equal(state.reviewId, "rev-1");
    assert.equal(state.planPath, blueprintPlanPath(root, "wi-review1"));
    assert.equal(loadPlanReviewState(root, "wi-review1")?.reviewId, "rev-1", "the review identity must be persisted");
    const gate = planApprovalGate(state);
    assert.equal(gate.ok, false);
    assert.match(gate.reason, /still pending/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("requestPlanReview falls back guardedly when the extension is unavailable, errors, or stays silent", async () => {
  const root = tempRoot("pic-blueprint-review-fb-");
  try {
    // Explicit extension unavailability is recorded without throwing.
    const unavailable = await requestPlanReview(fakePlannotatorEvents((request) => request.respond({ status: "unavailable", error: "Plannotator context is not ready yet." })), root, "wi-reviewfb", "# PLAN");
    assert.equal(unavailable.status, "unavailable");
    assert.match(unavailable.error!, /not ready/);
    assert.equal(planApprovalGate(unavailable).ok, true, "a guarded fallback passes the gate with zero dispositions");

    // Extension-side errors are equally guarded.
    const failed = await requestPlanReview(fakePlannotatorEvents((request) => request.respond({ status: "error", error: "browser start failed" })), root, "wi-reviewfb", "# PLAN");
    assert.equal(failed.status, "unavailable");

    // A silent channel (extension not loaded) times out guardedly.
    const silent = await requestPlanReview(fakePlannotatorEvents(), root, "wi-reviewfb", "# PLAN", { timeoutMs: 20 });
    assert.equal(silent.status, "unavailable");
    assert.match(silent.error!, /did not respond/);
    assert.equal(loadPlanReviewState(root, "wi-reviewfb")?.reviewId, undefined, "no review identity may be fabricated");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("recoverPlanReviewResult resolves the outcome and the gate enforces entered annotations until revision", async () => {
  const root = tempRoot("pic-blueprint-recover-");
  try {
    let outcome: unknown = { status: "pending" };
    const bus = fakePlannotatorEvents(planReviewResponder(() => outcome));

    await requestPlanReview(bus, root, "wi-recover", "# PLAN v1");
    // Still pending: the gate keeps blocking.
    const stillPending = await recoverPlanReviewResult(bus, root, "wi-recover");
    assert.equal(stillPending?.status, "pending");
    assert.equal(planApprovalGate(stillPending).ok, false);

    // Rejected with feedback: annotations become the pending state the gate enforces.
    outcome = { status: "completed", approved: false, feedback: "tighten seam 2" };
    const rejected = await recoverPlanReviewResult(bus, root, "wi-recover");
    assert.equal(rejected?.status, "rejected");
    assert.equal(rejected?.feedback, "tighten seam 2");
    const rejectedGate = planApprovalGate(rejected);
    assert.equal(rejectedGate.ok, false);
    assert.match(rejectedGate.reason, /tighten seam 2/);

    // A fresh review approved with entered annotations stays gated.
    await requestPlanReview(bus, root, "wi-recover", "# PLAN v2");
    outcome = { status: "completed", approved: true, feedback: "naming nit" };
    const annotated = await recoverPlanReviewResult(bus, root, "wi-recover");
    assert.equal(annotated?.status, "rejected");
    assert.equal(planApprovalGate(annotated).ok, false, "entered annotations must block approval even on an approved review");

    // A clean approve with zero annotations passes.
    await requestPlanReview(bus, root, "wi-recover", "# PLAN v3");
    outcome = { status: "completed", approved: true };
    const approved = await recoverPlanReviewResult(bus, root, "wi-recover");
    assert.equal(approved?.status, "approved");
    assert.deepEqual(planApprovalGate(approved), { ok: true, dispositions: 0 });

    // Guarded recovery: no persisted state, or a missing stored result, never throws.
    assert.equal(await recoverPlanReviewResult(bus, root, "wi-unknown"), undefined);
    await requestPlanReview(bus, root, "wi-recover", "# PLAN v4");
    outcome = { status: "missing" };
    assert.equal((await recoverPlanReviewResult(bus, root, "wi-recover"))?.status, "pending");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

// ── Tool-level seam: registerTaskManagerTool's execute path ─────────────────

interface CapturedTool {
  name: string;
  execute: (toolCallId: string, params: Record<string, unknown>, signal: never, onUpdate: never, ctx: { cwd: string }) => Promise<{ content: Array<{ type: string; text: string }>; details?: Record<string, unknown>; isError?: boolean }>;
}

function captureTaskManagerTool(events: unknown = { emit: () => {} }): CapturedTool {
  let captured: CapturedTool | undefined;
  registerTaskManagerTool({ registerTool: (tool: CapturedTool) => { captured = tool; }, events } as never, {} as never);
  assert.ok(captured, "task_manager tool must be registered");
  assert.equal(captured!.name, "task_manager");
  return captured!;
}

// Seam tests run as the primary agent: clear any child-agent identity around
// execute so the task_manager capability gate (read-only for task-worker etc.)
// does not block the owner/contractor actions under test.
async function runTool(tool: CapturedTool, params: Record<string, unknown>, cwd: string) {
  const previousAgentName = process.env.PI_TASK_AGENT_NAME;
  delete process.env.PI_TASK_AGENT_NAME;
  try {
    return await tool.execute("plan-review-seam", params, undefined as never, undefined as never, { cwd });
  } finally {
    if (previousAgentName !== undefined) process.env.PI_TASK_AGENT_NAME = previousAgentName;
  }
}

// Bound the guarded no-listener wait so absent-extension tests stay fast.
async function withPlanReviewTimeout<T>(run: () => Promise<T> | T): Promise<T> {
  const previous = process.env.PI_PLAN_REVIEW_TIMEOUT_MS;
  process.env.PI_PLAN_REVIEW_TIMEOUT_MS = "25";
  try {
    return await run();
  } finally {
    if (previous === undefined) delete process.env.PI_PLAN_REVIEW_TIMEOUT_MS; else process.env.PI_PLAN_REVIEW_TIMEOUT_MS = previous;
  }
}

// Minimal stub Go CLI: deterministic artifact-save/artifact-approve outcomes
// driven by PIC_STUB_SCENARIO, with every invocation logged to PIC_STUB_LOG
// so tests can assert the real pic call sequence around the review gate.
const PIC_CLI_STUB = `#!/bin/sh
printf '%s\\n' "$*" >> "\${PIC_STUB_LOG:?}"
case "$*" in
  *" artifact-save "*)
    if [ "\${PIC_STUB_SCENARIO:-ok}" = "savefails" ]; then echo '{"error":"canonical save failed"}'; else echo '{"id":"art-stub-1","ok":true}'; fi ;;
  *" artifact-approve "*)
    if [ "\${PIC_STUB_SCENARIO:-ok}" = "approvefails" ]; then echo '{"error":"owner approval failed"}'; else echo '{"ok":true}'; fi ;;
  *) echo '{"error":"unexpected pic invocation"}' ;;
esac
`;

async function withPicStub<T>(scenario: string, run: (picLog: string) => Promise<T> | T): Promise<T> {
  const stubDir = mkdtempSync(join(tmpdir(), "pic-cli-stub-"));
  const stubPath = join(stubDir, "pic-stub");
  const logPath = join(stubDir, "pic-calls.log");
  writeFileSync(stubPath, PIC_CLI_STUB);
  chmodSync(stubPath, 0o755);
  const previousCli = process.env.PIC_CLI;
  const previousScenario = process.env.PIC_STUB_SCENARIO;
  const previousLog = process.env.PIC_STUB_LOG;
  process.env.PIC_CLI = stubPath;
  process.env.PIC_STUB_SCENARIO = scenario;
  process.env.PIC_STUB_LOG = logPath;
  try {
    // Await inside the try so the stub dir (and its call log) outlive the
    // entire test body instead of being cleaned up at the first await point.
    return await run(logPath);
  } finally {
    if (previousCli === undefined) delete process.env.PIC_CLI; else process.env.PIC_CLI = previousCli;
    if (previousScenario === undefined) delete process.env.PIC_STUB_SCENARIO; else process.env.PIC_STUB_SCENARIO = previousScenario;
    if (previousLog === undefined) delete process.env.PIC_STUB_LOG; else process.env.PIC_STUB_LOG = previousLog;
    rmSync(stubDir, { recursive: true, force: true });
  }
}

const BLUEPRINT = {
  project_info: { project: "Plan review seam", nature: "verification", date: "2026-02-12" },
  goals: { primary_goal: "Prove the plan review seam", target_audience: "contractor", key_message: "review runtime is the extension" },
  architecture: { building_blocks: ["plan review seam"], connection_summary: "tool -> extension", data_flow: "draft -> render -> review" },
  tech_stack: [{ layer: "cli", choice: "pic", rationale: "canonical", reuse: "yes" }],
  file_structure: [{ path: ".pi/artifacts/plans", purpose: "stable plan renders" }],
  rri_requirements_matrix: [{ blueprint_section: "Plan review seam", requirements: ["REQ-F3-1"], source_questions: ["Q1"] }],
  task_decomposition_preview: { estimated_tasks: 1, estimated_effort_minutes: 30, tasks: [{ tip_id: "TIP-1", title: "Seam", goal: "Prove the plan review seam" }] },
  adr_candidates: [{ context: "Plan review delivery", choice: "Plannotator extension", reason: "No standalone CLI requirement" }],
};
const BLUEPRINT_JSON = JSON.stringify(BLUEPRINT);
const V1_CHECKPOINT = { architecture: true, design: true, requirements: true, task_decomposition: true, nothing_missing: true };

test("review_blueprint_checkpoint projects the exact renderer output to the stable plan path and requests the extension review", async () => {
  const bus = fakePlannotatorEvents(planReviewResponder(() => ({ status: "pending" })));
  const tool = captureTaskManagerTool(bus);
  const root = tempRoot("pic-blueprint-tool-review-");
  try {
    const saved = await runTool(tool, { action: "save_blueprint_draft", id: "wi-planreview", stage: "blueprint", content: BLUEPRINT_JSON }, root);
    assert.equal(saved.isError, undefined, `unexpected save error: ${saved.content[0]?.text}`);
    const draftId = (saved.details as { draft_id?: string }).draft_id!;
    const reviewed = await runTool(tool, { action: "review_blueprint_checkpoint", id: "wi-planreview", artifact_id: draftId, content: JSON.stringify(V1_CHECKPOINT), actor_role: "contractor" }, root);
    assert.equal(reviewed.isError, undefined, `unexpected review error: ${reviewed.content[0]?.text}`);

    // The persisted plan file carries exactly the canonical renderer output.
    const expected = renderBlueprintReportMarkdown(parseBlueprintReportJson(BLUEPRINT_JSON));
    const planFile = join(root, ".pi", "artifacts", "plans", "wi-planreview.md");
    assert.equal(readFileSync(planFile, "utf8"), expected);

    // The extension review request carries the same render and the stable path.
    const request = bus.requests.find((entry) => entry.action === "plan-review");
    assert.ok(request, "the checkpoint must emit a plannotator:request plan-review request");
    assert.equal(request.payload.planContent, expected);
    assert.equal(request.payload.planFilePath, planFile);

    const details = reviewed.details as { plan_review?: { status?: string; reviewId?: string; planPath?: string }; plan_path?: string };
    assert.equal(details.plan_review?.status, "pending");
    assert.equal(details.plan_review?.reviewId, "rev-1");
    assert.equal(details.plan_path, planFile);
    assert.equal(loadPlanReviewState(root, "wi-planreview")?.reviewId, "rev-1", "the review identity must be persisted for the approval gate");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("an absent Plannotator extension leaves the checkpoint loop guarded and approval proceeding with zero dispositions", async () => {
  const tool = captureTaskManagerTool();
  const root = tempRoot("pic-blueprint-tool-absent-");
  try {
    await withPicStub("ok", async (picLog) => {
      const draft = saveBlueprintDraft(root, "wi-absent", BLUEPRINT_JSON);
      await withPlanReviewTimeout(async () => {
        const reviewed = await runTool(tool, { action: "review_blueprint_checkpoint", id: "wi-absent", artifact_id: draft.draftId, content: JSON.stringify(V1_CHECKPOINT), actor_role: "contractor" }, root);
        assert.equal(reviewed.isError, undefined, `the loop must proceed without error: ${reviewed.content[0]?.text}`);
        assert.equal(existsSync(join(root, ".pi", "artifacts", "plans", "wi-absent.md")), true, "the plan render must still be projected");
        const details = reviewed.details as { plan_review?: { status?: string; reviewId?: string } };
        assert.equal(details.plan_review?.status, "unavailable");
        assert.equal(details.plan_review?.reviewId, undefined, "absent-extension discovery must never fabricate a review identity");

        // Guarded fallback: approval proceeds with zero dispositions recorded.
        // The checkpoint re-saves the draft, so approve with the returned ID.
        const reviewedDraftId = (reviewed.details as { draft_id?: string }).draft_id!;
        const approved = await runTool(tool, { action: "approve_blueprint_draft", id: "wi-absent", artifact_id: reviewedDraftId, actor_role: "owner" }, root);
        assert.equal(approved.isError, undefined, `unexpected approval error: ${approved.content[0]?.text}`);
      });
      const calls = readFileSync(picLog, "utf8").trim().split("\n");
      assert.match(calls[0]!, / artifact-save /);
      assert.match(calls[1]!, / artifact-approve /);
      assert.equal(loadPlanReviewState(root, "wi-absent"), undefined, "the review state must be cleared after approval");
    });
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("approve_blueprint_draft blocks while the plan review is still pending and never invokes the Go CLI", async () => {
  const bus = fakePlannotatorEvents(planReviewResponder(() => ({ status: "pending" })));
  const tool = captureTaskManagerTool(bus);
  await withPicStub("ok", async (picLog) => {
    const root = tempRoot("pic-blueprint-tool-pending-");
    try {
      const draft = saveBlueprintDraft(root, "wi-gatepending", BLUEPRINT_JSON, V1_CHECKPOINT);
      await requestPlanReview(bus, root, "wi-gatepending", "# PLAN", { timeoutMs: 50 });
      const result = await runTool(tool, { action: "approve_blueprint_draft", id: "wi-gatepending", artifact_id: draft.draftId, actor_role: "owner" }, root);
      assert.equal(result.isError, true, "a pending plan review must block approval");
      assert.match(result.content[0]!.text, /still pending/);
      assert.equal(existsSync(picLog), false, "the hard gate must run before any Go artifact-save or artifact-approve call");
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});

test("approve_blueprint_draft blocks on annotation feedback until the revision loop resolves the review", async () => {
  let outcome: unknown = { status: "completed", approved: false, feedback: "tighten seam 2" };
  const bus = fakePlannotatorEvents(planReviewResponder(() => outcome));
  const tool = captureTaskManagerTool(bus);
  await withPicStub("ok", async (picLog) => {
    const root = tempRoot("pic-blueprint-tool-reject-");
    try {
      const draft = saveBlueprintDraft(root, "wi-gatereject", BLUEPRINT_JSON, V1_CHECKPOINT);
      await requestPlanReview(bus, root, "wi-gatereject", "# PLAN", { timeoutMs: 50 });
      const rejected = await runTool(tool, { action: "approve_blueprint_draft", id: "wi-gatereject", artifact_id: draft.draftId, actor_role: "owner" }, root);
      assert.equal(rejected.isError, true, "annotation feedback must block approval");
      assert.match(rejected.content[0]!.text, /tighten seam 2/);
      assert.equal(existsSync(picLog), false);

      // The contractor revises and reruns the checkpoint: the same stable plan
      // path is rewritten and a fresh plan review is requested.
      outcome = { status: "pending" };
      const revised = await runTool(tool, { action: "review_blueprint_checkpoint", id: "wi-gatereject", artifact_id: draft.draftId, content: JSON.stringify(V1_CHECKPOINT), actor_role: "contractor" }, root);
      assert.equal(revised.isError, undefined, `unexpected revision error: ${revised.content[0]?.text}`);
      assert.equal(readFileSync(join(root, ".pi", "artifacts", "plans", "wi-gatereject.md"), "utf8"), renderBlueprintReportMarkdown(parseBlueprintReportJson(BLUEPRINT_JSON)), "the revision must reuse the same plan path");
      assert.equal(loadPlanReviewState(root, "wi-gatereject")?.status, "pending");

      // The owner re-reviews the revised plan with zero annotations: the gate lifts.
      outcome = { status: "completed", approved: true };
      const revisedDraftId = (revised.details as { draft_id?: string }).draft_id!;
      const approved = await runTool(tool, { action: "approve_blueprint_draft", id: "wi-gatereject", artifact_id: revisedDraftId, actor_role: "owner" }, root);
      assert.equal(approved.isError, undefined, `unexpected approval error: ${approved.content[0]?.text}`);
      const calls = readFileSync(picLog, "utf8").trim().split("\n");
      assert.match(calls[0]!, / artifact-save /);
      assert.match(calls[1]!, / artifact-approve /);
      assert.equal(loadPlanReviewState(root, "wi-gatereject"), undefined, "the review state must be cleared after approval");
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});

test("approve_blueprint_draft succeeds with zero dispositions for an owner who skipped annotation", async () => {
  const bus = fakePlannotatorEvents(planReviewResponder(() => ({ status: "completed", approved: true })));
  const tool = captureTaskManagerTool(bus);
  await withPicStub("ok", async (picLog) => {
    const root = tempRoot("pic-blueprint-tool-zero-");
    try {
      const draft = saveBlueprintDraft(root, "wi-zero", BLUEPRINT_JSON, V1_CHECKPOINT);
      await requestPlanReview(bus, root, "wi-zero", "# PLAN", { timeoutMs: 50 });
      const result = await runTool(tool, { action: "approve_blueprint_draft", id: "wi-zero", artifact_id: draft.draftId, actor_role: "owner" }, root);
      assert.equal(result.isError, undefined, `unexpected approval error: ${result.content[0]?.text}`);
      const calls = readFileSync(picLog, "utf8").trim().split("\n");
      assert.match(calls[0]!, / artifact-save /);
      assert.match(calls[1]!, / artifact-approve /);
      // Zero-annotation approval clears the review state and completes the
      // ordinary approval flow (exactly one ADR file per candidate).
      assert.equal(loadPlanReviewState(root, "wi-zero"), undefined);
      assert.deepEqual(readdirSync(join(root, "docs", "adr")), ["0001-plannotator-extension.md"]);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});
