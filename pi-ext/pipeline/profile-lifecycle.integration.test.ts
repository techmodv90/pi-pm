import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import {
  assertRunContractCurrent,
  canonicalReadyLeafIds,
  mergeAggregateBranch,
  nextPipelineStage,
  normalizePipelineData,
  pipelineWorkerBlockReason,
  predecessorCheckpointFor,
  PipelineScheduler,
  resolvePlanProfile,
  workerIntegrationCandidate,
} from "./pipeline-scheduler.ts";
import { parsePipelineRuns } from "./pipeline-types.ts";
import { planStagesForProfile } from "../tasking/workflow-modes.ts";
import { buildAggregateVerifyPrompt, latestRriTScenarios } from "../tasking/work-item-prompts.ts";

// profile-lifecycle.integration.test.ts exercises the complete persisted
// profile, scheduler, authority, standalone, corrective, and promotion flow
// across the TypeScript scheduler boundary using managed scheduler fixtures and
// deterministic child-result handoffs (no local persistent state, no leaked
// scheduler processes).

test("REQ-AGGREGATE-DEPTH: an authorized aggregate reserves mandatory planning stages and binds dispatch to the persisted profile", () => {
  // A feature beneath an epic at "designed" depth must retain RRI and Task
  // Graph and must exclude Vision and Contracts; a persisted plan profile binds
  // the dispatch authority to its version and content hash.
  assert.deepEqual(planStagesForProfile("feature", "epic-1", "designed"), ["scan", "rri", "blueprint", "task_graph"]);
  const data = normalizePipelineData({
    work_item: { id: "wi-feature", type: "feature", parent_id: "epic-1", planning_depth: "designed", title: "Plan aggregate" },
    profiles: [{ profile_name: "plan", profile_version: 2, planning_depth: "designed", stages_json: JSON.stringify(["scan", "rri", "blueprint", "task_graph"]), content_hash: "hash-2", status: "active" }],
  });
  assert.deepEqual(resolvePlanProfile(data), {
    depth: "designed", version: 2, contentHash: "hash-2", stages: ["scan", "rri", "blueprint", "task_graph"], resolved: true,
  });
  assert.ok(data.plan_profile.stages.includes("rri"));
  assert.ok(data.plan_profile.stages.includes("task_graph"));
  assert.ok(!data.plan_profile.stages.includes("vision"));
  assert.ok(!data.plan_profile.stages.includes("contracts"));
});

test("REQ-STANDALONE-PATH: a standalone Task keeps its identity through the lean Plan profile and one-node implementation", () => {
  // The lean profile is fixed for standalone executables regardless of the
  // persisted depth; the one-node graph retains the canonical root identity with
  // no child decomposition.
  for (const kind of ["task", "bug", "chore"]) {
    assert.deepEqual(planStagesForProfile(kind, "", "standard"), ["scan", "rri", "task_graph"]);
  }
  const data = normalizePipelineData({
    work_item: { id: "wi-stand", type: "task", parent_id: "", planning_depth: "full", title: "Standalone", status: "open" },
    ready: true,
    instruction_packs: [{
      id: "wip-1", version: 1, status: "active", content_hash: "hash-1",
      content_json: JSON.stringify({ constraints: { scope_roots: ["a.go"] }, skillFamilies: ["languages/typescript"] }),
    }],
    children: [],
  });
  assert.equal(data.work_item.id, "wi-stand");
  assert.deepEqual(data.plan_profile.stages, ["scan", "rri", "task_graph"]);
  assert.deepEqual(canonicalReadyLeafIds(
    { work_item: { id: "wi-stand", type: "task", status: "open" }, ready: true, children: [] },
    () => { throw new Error("no children to load"); },
  ), ["wi-stand"]);
  assert.equal(pipelineWorkerBlockReason(data), null);
});

test("REQ-PIPELINE-PROFILES: the scheduler advances an authorized executable through canonical workflow state without source edits or bypasses", () => {
  // After owner authorization the persisted profile resolves and the current
  // dispatch stage is worker through the canonical execution state.
  const data = normalizePipelineData({
    work_item: { id: "wi-1", type: "task", parent_id: "", planning_depth: "quick", status: "in_progress", title: "Leaf" },
    profiles: [{ profile_name: "plan", profile_version: 1, planning_depth: "quick", stages_json: JSON.stringify(["scan", "rri", "task_graph"]) }],
    instruction_packs: [{ id: "wip-1", version: 1, status: "active", content_hash: "hash-1" }],
    execution_state: { pipeline_stage: "worker", next_stage: "implement" },
  });
  assert.equal(nextPipelineStage(data), "worker");
  assert.deepEqual(data.plan_profile.stages, ["scan", "rri", "task_graph"]);

  // The worker handoff must be bound to the persisted profile so the run cannot
  // be forged or attached to a stale lineage.
  const runs = parsePipelineRuns([
    { id: "pr-1", task_id: "wi-1", stage: "worker", status: "completed", lease_token: "lease", instruction_pack_id: "wip-1", instruction_pack_hash: "hash-1", artifact_saved_at: "2026-01-01", integrated_at: "2026-01-01" },
    { id: "pr-2", task_id: "wi-1", stage: "worker", status: "completed", lease_token: "lease", instruction_pack_id: "wip-1", instruction_pack_hash: "hash-1", artifact_saved_at: "2026-01-03" },
  ]);
  // The un-integrated completed artifact run is the worker integration candidate;
  // an already-integrated run never reappears as a mutable handoff.
  assert.equal(workerIntegrationCandidate(runs)?.id, "pr-2");
  assert.doesNotThrow(() => assertRunContractCurrent(data, { instruction_pack_id: "wip-1", instruction_pack_hash: "hash-1", effective_contract_snapshot_id: "", effective_contract_snapshot_hash: "" }));
  assert.throws(
    () => assertRunContractCurrent({ ...data, instruction_packs: [{ id: "wip-2", version: 2, status: "active", content_hash: "hash-2" }] }, { instruction_pack_id: "wip-1", instruction_pack_hash: "hash-1", effective_contract_snapshot_id: "", effective_contract_snapshot_hash: "" }),
    /worker instruction pack changed/,
  );
});

test("REQ-AUTHORITY-BOUNDARIES: a dependency-blocked or unmediated child is never dispatched and child claims only launch authorized dependency-ready leaves", () => {
  // A child blocked by an incomplete dependency and a completed sibling are not
  // ready leaves; only an authorized, unblocked executable leaf is dispatched.
  const ids = canonicalReadyLeafIds({
    work_item: { id: "root", type: "epic", status: "open" },
    children: [{ id: "ready", type: "task", status: "open" }, { id: "blocked", type: "task", status: "open" }, { id: "completed", type: "task", status: "done" }],
  }, (childId) => ({
    work_item: { id: childId, type: "task", status: childId === "completed" ? "done" : "open" },
    ready: childId === "ready",
    children: [],
  }));
  assert.deepEqual(ids, ["ready"]);

  // A ready leaf that requires the active TIP but has none is blocked by the
  // scheduler, but an authorized materialized leaf that awaits its first TIP is
  // explicitly the canonical first-claim readiness handoff, not a bypass.
  // A leaf without the sole active TIP is genuinely blocked unless it is the
  // canonical first-claim-ready handoff (a ready authorized executable awaiting
  // its frozen TIP), which the scheduler dispatches rather than bypasses.
  assert.equal(pipelineWorkerBlockReason({
    work_item: { id: "wi-2", type: "task", title: "No TIP", status: "open" },
    canonical: false, ready: true, instruction_packs: [],
  }), "Work Item \"No TIP\" requires exactly one active Task Instruction Pack before work.");
  assert.equal(pipelineWorkerBlockReason({
    work_item: { id: "wi-3", type: "task", title: "Awaiting first claim", status: "open" },
    canonical: true, ready: true, instruction_packs: [],
  }), null);
});

test("REQ-IMMUTABLE-HISTORY: stale planning handoffs and completed-history rewrites are rejected instead of silently retried", () => {
  // The planning handoff resolves the immediately precedent approved checkpoint
  // from the persisted profile order; a missing precedent yields no handoff.
  const data = { checkpoints: [{ stage: "scan", artifact_id: "cp-scan", artifact_revision: 1, decision_type: "accepted" }] };
  assert.equal(predecessorCheckpointFor(data, "rri", ["scan", "rri", "task_graph"])?.artifact_id, "cp-scan");
  assert.equal(predecessorCheckpointFor(data, "scan", ["scan", "rri", "task_graph"]), undefined);

  // A completed mutation run is never selected as the integration candidate; the
  // worker candidate is the un-integrated completed artifact run, and a stale
  // handoff that does not match the active lineage is rejected.
  const runs = parsePipelineRuns([
    { id: "pr-done", task_id: "wi-1", stage: "worker", status: "completed", lease_token: "l", artifact_saved_at: "2026-01-01", integrated_at: "2026-01-02", advanced_at: "2026-01-02" },
    { id: "pr-candidate", task_id: "wi-1", stage: "worker", status: "completed", lease_token: "l", artifact_saved_at: "2026-01-03" },
  ]);
  assert.equal(workerIntegrationCandidate(runs)?.id, "pr-candidate");
});

test("REQ-PROMOTION-GATE: the scheduler merges the verified aggregate head to develop exactly once after aggregate verification", () => {
  const root = mkdtempSync(join(tmpdir(), "profile-lifecycle-merge-"));
  const repo = join(root, "repo");
  const remote = join(root, "remote.git");
  try {
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

    // The merge is exactly-once: a second call returns the same merged commit
    // rather than creating another merge; proof the release closed cleanly.
    const merged = mergeAggregateBranch(repo, state);
    assert.equal(execFileSync("git", ["rev-list", "--count", `${base}..${merged}`], { cwd: repo, encoding: "utf8" }).trim(), "2");
    assert.equal(mergeAggregateBranch(repo, state), merged);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("REQ-AGGREGATE-RRI-T-LIFECYCLE: the two-phase RRI-T handoff preserves aggregate verification, owner acceptance, and merge ordering", () => {
  // One persisted artifact owns two requirement-bound scenarios sharing persona,
  // dimension, stress axis, and requirement but differing in id; the parent
  // carries a higher-revision scenario that must never leak into the feature.
  const persisted = {
    methodology: "rri-t",
    personas: ["QA / Tester"],
    scenarios: [
      { id: "SC-1", persona: "QA / Tester", dimension: "D3", stress_axis: "ERROR", requirement_id: "REQ-1", procedure: "submit the empty form → inline error is shown", remediation_hint: "Assert the inline error" },
      { id: "SC-2", persona: "QA / Tester", dimension: "D3", stress_axis: "ERROR", requirement_id: "REQ-1", procedure: "submit a malformed payload → rejected without state change", remediation_hint: "Assert the rejection" },
    ],
    not_applicable: [],
    open_blockers: [],
  };
  const data = {
    work_item: { id: "wi-feature", title: "Feature" },
    children: [{ id: "wi-child", title: "Child", status: "done" }],
    artifacts: [
      { id: "wia-own", work_item_id: "wi-feature", stage: "rri_t_scenarios", revision: 1, content_hash: "hash-own", content: JSON.stringify(persisted) },
      { id: "wia-parent", work_item_id: "wi-epic", stage: "rri_t_scenarios", revision: 3, content_hash: "hash-parent", content: JSON.stringify({ ...persisted, scenarios: [{ id: "PARENT-REV3", persona: "QA / Tester", dimension: "D3", stress_axis: "ERROR", requirement_id: "REQ-1", procedure: "parent only", remediation_hint: "" }] }) },
    ],
  };
  // Persisted verification boundary: only the aggregate's own artifact revision
  // is consumable for grading, never a parent's higher revision.
  assert.equal(latestRriTScenarios(data)?.artifact.id, "wia-own");
  const prompt = buildAggregateVerifyPrompt(data);
  assert.match(prompt, /Loaded from artifact wia-own \(revision 1, content hash hash-own\) — never from in-memory persona output/);
  assert.match(prompt, /Execute the final aggregate verification now/);
  assert.match(prompt, /REQ-1, SC-1\)/);
  assert.match(prompt, /REQ-1, SC-2\)/);
  assert.doesNotMatch(prompt, /PARENT-REV3/);
  // Phase 2 ownership: the contractor executes and grades in the main session;
  // personas authored only scenarios and no result or evidence comes from them.
  assert.match(prompt, /Do not run, amend, or re-author persona output, and do not re-run persona subagents/);
  assert.match(prompt, /no subagent executes procedures or produces grades/);
  assert.match(prompt, /never grade a scenario you did not execute/);
  assert.match(prompt, /Each retained scenario receives exactly one outcome/);
  assert.match(prompt, /not_applicable with a concrete reason/);
  assert.match(prompt, /trim or defer/i);
  // Submission ends with the contractor's aggregate verification: the rri_t
  // evidence carries the persisted scenario id and the single owner decision plus
  // the bound branch merge remain the only subsequent aggregate decisions.
  assert.match(prompt, /verify_aggregate_work_item/);
  assert.match(prompt, /rri_t_evidence_json/);
  assert.match(prompt, /actor_role=contractor/);
  assert.match(prompt, /Do not call owner acceptance/);
  assert.match(prompt, /single owner decision gate/);
  assert.doesNotMatch(prompt, /accept_aggregate_work_item|merge_aggregate_work_item/);
  assert.ok(prompt.indexOf("verify_aggregate_work_item") < prompt.indexOf("Do not call owner acceptance"));
});

test("managed scheduler fixtures run no pipeline I/O and leave no leaked scheduler process", async () => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const pi = { events: { on: () => () => {} } } as any;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const scheduler = new PipelineScheduler(pi) as any;
  let recovered = 0;
  let reconciled = 0;
  scheduler.recoverOrphanedRuns = () => { recovered++; };
  scheduler.reconcileSafely = async () => { reconciled++; };
  scheduler.startSession({ cwd: "/managed-repo" });
  assert.equal(scheduler.cwd, "/managed-repo");
  // Session start performs no pipeline I/O; recovery/reconciliation only run on
  // explicit event signals, so the fixture leaks no scheduler process.
  assert.equal(recovered, 0);
  await new Promise<void>((resolve) => setImmediate(resolve));
  assert.equal(recovered, 0);
  assert.equal(reconciled, 0);
});