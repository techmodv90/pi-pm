import assert from "node:assert/strict";
import { chmodSync, existsSync, mkdtempSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { registerTaskManagerTool } from "../api/tool.ts";
import { saveBlueprintDraft } from "./blueprint-drafts.ts";
import { planAdrFiles, writeAdrFiles, type AdrCandidate } from "./blueprint-adr.ts";

function tempRoot(): string {
  return mkdtempSync(join(tmpdir(), "pic-blueprint-adr-"));
}

test("Draft save and contractor review leave docs/adr untouched", () => {
  const root = tempRoot();
  try {
    const draft = saveBlueprintDraft(root, "wi-adr1", JSON.stringify({
      adr_candidates: [{ context: "Dashboard delivery", choice: "Embedded static build", reason: "No runtime dependency on Node" }],
    }));
    assert.equal(draft.reviewed, false);
    saveBlueprintDraft(root, "wi-adr1", draft.content, { architecture: true, design: true, requirements: true, task_decomposition: true, nothing_missing: true });
    assert.equal(existsSync(join(root, "docs", "adr")), false, "no docs/adr directory may exist during the draft lifecycle");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("Approved-flow writer creates exactly one deterministic numbered file per candidate", () => {
  const root = tempRoot();
  try {
    const candidates: AdrCandidate[] = [
      { context: "Dashboard delivery", choice: "Embedded Static Build", reason: "No runtime dependency on Node" },
      { context: "Draft persistence", choice: "Runtime drafts under .pi", reason: "Temporary state must not touch the repository" },
    ];
    const first = writeAdrFiles(root, candidates);
    assert.deepEqual(first, ["docs/adr/0001-embedded-static-build.md", "docs/adr/0002-runtime-drafts-under-pi.md"]);
    const body = readFileSync(join(root, first[0]!), "utf8");
    assert.match(body, /# Embedded Static Build/);
    assert.match(body, /\*\*Status\*\*: accepted/);
    assert.match(body, /Dashboard delivery/);
    assert.match(body, /No runtime dependency on Node/);

    const twin = tempRoot();
    try {
      assert.deepEqual(writeAdrFiles(twin, candidates), first, "numbering and slugs are deterministic across roots");
    } finally {
      rmSync(twin, { recursive: true, force: true });
    }

    // Numbering continues after existing repository ADRs.
    mkdirSync(join(root, "docs", "adr"), { recursive: true });
    writeFileSync(join(root, "docs", "adr", "0007-existing-decision.md"), "existing");
    const continued = writeAdrFiles(root, candidates.slice(0, 1));
    assert.deepEqual(continued, ["docs/adr/0008-embedded-static-build.md"]);
    assert.equal(readdirSync(join(root, "docs", "adr")).length, 4);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("Malformed candidates or target conflicts fail without creating any ADR files", () => {
  const root = tempRoot();
  try {
    assert.throws(() => writeAdrFiles(root, [{ context: "c", choice: "ok choice", reason: "  " }]), /adr_candidates\[0\]\.reason must be a non-empty string/);
    assert.throws(() => writeAdrFiles(root, [{ context: "c", choice: "///", reason: "r" }]), /no safe slug/);
    assert.equal(existsSync(join(root, "docs", "adr")), false);

    // Validation happens for the whole batch before any write: a malformed
    // second candidate aborts the first, leaving no partial ADR files.
    assert.throws(
      () => writeAdrFiles(root, [
        { context: "c", choice: "First Choice", reason: "r" },
        { context: "c", choice: "Second Choice", reason: "  " },
      ]),
      /adr_candidates\[1\]\.reason must be a non-empty string/,
    );
    assert.equal(existsSync(join(root, "docs", "adr")), false);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("writeAdrFiles rejects non-string candidate fields instead of coercing them", () => {
  const root = tempRoot();
  try {
    assert.throws(() => writeAdrFiles(root, [{ context: 42, choice: "choice", reason: "r" } as unknown as AdrCandidate]), /adr_candidates\[0\]\.context must be a non-empty string/);
    assert.throws(() => writeAdrFiles(root, [{ context: "c", choice: null, reason: "r" } as unknown as AdrCandidate]), /adr_candidates\[0\]\.choice must be a non-empty string/);
    assert.throws(() => writeAdrFiles(root, [null as unknown as AdrCandidate]), /adr_candidates\[0\] must be an object/);
    assert.throws(() => writeAdrFiles(root, ["nope" as unknown as AdrCandidate]), /adr_candidates\[0\] must be an object/);
    assert.equal(existsSync(join(root, "docs", "adr")), false, "no docs/adr directory may be created for malformed candidates");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("planAdrFiles validates and preflights without touching the filesystem", () => {
  const root = tempRoot();
  try {
    const candidates: AdrCandidate[] = [{ context: "Dashboard delivery", choice: "Embedded Static Build", reason: "No runtime dependency on Node" }];
    const planned = planAdrFiles(root, candidates);
    assert.equal(planned.length, 1);
    assert.match(planned[0]!.filename, /^\d{4}-embedded-static-build\.md$/);
    assert.equal(existsSync(root), true, "planning must not create docs/adr or any files");
    assert.equal(existsSync(join(root, "docs")), false);
    assert.equal(existsSync(join(root, "docs", "adr")), false);

    // The same plan is recomputed deterministically on the second call.
    const again = planAdrFiles(root, candidates);
    assert.equal(again[0]!.filename, planned[0]!.filename);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

// ── Approval-seam integration: the real task_manager tool path ──────────────
// These tests drive registerTaskManagerTool's execute callback with a temp
// project root and a PIC_CLI stub so Go artifact-save/artifact-approve
// outcomes are controlled without touching the repository.

interface CapturedTool {
  name: string;
  execute: (toolCallId: string, params: Record<string, unknown>, signal: never, onUpdate: never, ctx: { cwd: string }) => Promise<{ content: Array<{ type: string; text: string }>; details?: Record<string, unknown>; isError?: boolean }>;
}

function captureTaskManagerTool(): CapturedTool {
  let captured: CapturedTool | undefined;
  registerTaskManagerTool({ registerTool: (tool: CapturedTool) => { captured = tool; } } as never, {} as never);
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
    return await tool.execute("adr-seam", params, undefined as never, undefined as never, { cwd });
  } finally {
    if (previousAgentName !== undefined) process.env.PI_TASK_AGENT_NAME = previousAgentName;
  }
}

// Minimal stub Go CLI: deterministic artifact-save/artifact-approve outcomes
// driven by PIC_STUB_SCENARIO, with every invocation logged to PIC_STUB_LOG
// so tests can assert the real pic call sequence around the ADR writes.
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

const CANDIDATES = [{ context: "Dashboard delivery", choice: "Embedded Static Build", reason: "No runtime dependency on Node" }];

test("approve_blueprint_draft writes exactly one ADR per candidate only after both Go operations succeed", async () => {
  const tool = captureTaskManagerTool();
  await withPicStub("ok", async (picLog) => {
    const root = tempRoot();
    try {
      // Work Item IDs must match the runtime-draft identity rule wi-<alphanumeric>.
      const draft = saveBlueprintDraft(root, "wi-adrok", JSON.stringify({ adr_candidates: CANDIDATES }), { architecture: true, design: true, requirements: true, task_decomposition: true, nothing_missing: true });
      const result = await runTool(tool, { action: "approve_blueprint_draft", id: "wi-adrok", artifact_id: draft.draftId, actor_role: "owner" }, root);
      assert.equal(result.isError, undefined, `unexpected approval error: ${result.content[0]?.text}`);
      const details = result.details as { adr_files?: string[] };
      assert.deepEqual(details.adr_files, ["docs/adr/0001-embedded-static-build.md"]);
      const calls = readFileSync(picLog, "utf8").trim().split("\n");
      assert.match(calls[0]!, / artifact-save /, "canonical artifact-save must run before any ADR write");
      assert.match(calls[1]!, / artifact-approve /, "owner artifact-approve must run before any ADR write");
      const files = readdirSync(join(root, "docs", "adr"));
      assert.deepEqual(files, ["0001-embedded-static-build.md"], "exactly one ADR file per candidate");
      const body = readFileSync(join(root, "docs", "adr", files[0]!), "utf8");
      assert.match(body, /# Embedded Static Build/);
      assert.match(body, /Dashboard delivery/);
      assert.match(body, /No runtime dependency on Node/);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});

test("failed canonical save or failed owner approval leaves docs/adr untouched", async () => {
  const tool = captureTaskManagerTool();
  for (const scenario of ["savefails", "approvefails"] as const) {
    await withPicStub(scenario, async () => {
      const root = tempRoot();
      try {
        const workItemId = `wi-adr${scenario}`;
        const draft = saveBlueprintDraft(root, workItemId, JSON.stringify({ adr_candidates: CANDIDATES }), { architecture: true, design: true, requirements: true, task_decomposition: true, nothing_missing: true });
        const result = await runTool(tool, { action: "approve_blueprint_draft", id: workItemId, artifact_id: draft.draftId, actor_role: "owner" }, root);
        assert.equal(result.isError, true, `scenario ${scenario} must fail the approval`);
        assert.equal(existsSync(join(root, "docs", "adr")), false, `scenario ${scenario} must not create any ADR files`);
        assert.equal(existsSync(join(root, ".pi", "runtime", "blueprint", `${workItemId}.json`)), true, "the temporary draft must survive a failed approval");
      } finally {
        rmSync(root, { recursive: true, force: true });
      }
    });
  }
});

test("approve_blueprint_draft rejects malformed adr_candidates without creating ADR files", async () => {
  const tool = captureTaskManagerTool();
  for (const [label, payload] of [
    ["null candidates", { adr_candidates: null }],
    ["object candidates", { adr_candidates: {} }],
    ["non-string fields", { adr_candidates: [{ context: 42, choice: "Choice", reason: "r" }] }],
  ] as const) {
    await withPicStub("ok", async (picLog) => {
      const root = tempRoot();
      try {
        const draft = saveBlueprintDraft(root, "wi-adrbad", JSON.stringify(payload), { architecture: true, design: true, requirements: true, task_decomposition: true, nothing_missing: true });
        const result = await runTool(tool, { action: "approve_blueprint_draft", id: "wi-adrbad", artifact_id: draft.draftId, actor_role: "owner" }, root);
        assert.equal(result.isError, true, `${label} must fail the approval`);
        assert.match(result.content[0]!.text, /adr_candidates/);
        // Validation runs before the Go seam: malformed input must produce no
        // artifact-save or artifact-approve call at all.
        assert.equal(existsSync(picLog), false, `${label} must not invoke artifact-save or artifact-approve`);
        assert.equal(existsSync(join(root, "docs", "adr")), false, `${label} must not create any ADR files`);
      } finally {
        rmSync(root, { recursive: true, force: true });
      }
    });
  }
});

test("approve_blueprint_draft rejects target conflicts before the Go operations", async () => {
  const tool = captureTaskManagerTool();
  await withPicStub("ok", async (picLog) => {
    const root = tempRoot();
    try {
      // Deterministic conflict: numbering takes max 4-digit prefix + 1 (10000),
      // and a pre-existing file already occupies that derived target name.
      mkdirSync(join(root, "docs", "adr"), { recursive: true });
      writeFileSync(join(root, "docs", "adr", "9999-numbering-limit.md"), "older");
      writeFileSync(join(root, "docs", "adr", "10000-embedded-static-build.md"), "existing");
      const draft = saveBlueprintDraft(root, "wi-adrclash", JSON.stringify({ adr_candidates: CANDIDATES }), { architecture: true, design: true, requirements: true, task_decomposition: true, nothing_missing: true });
      const result = await runTool(tool, { action: "approve_blueprint_draft", id: "wi-adrclash", artifact_id: draft.draftId, actor_role: "owner" }, root);
      assert.equal(result.isError, true, "target conflict must fail the approval");
      assert.match(result.content[0]!.text, /ADR target file already exists/);
      assert.equal(existsSync(picLog), false, "target-conflict preflight must run before artifact-save and artifact-approve");
      assert.equal(readFileSync(join(root, "docs", "adr", "10000-embedded-static-build.md"), "utf8"), "existing", "the conflicting file must stay untouched");
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});

test("tool-level draft save and contractor review never invoke the Go CLI for ADR writes", async () => {
  const tool = captureTaskManagerTool();
  const blueprint = JSON.stringify({
    project_info: { project: "ADR seam", nature: "verification", date: "2026-02-11" },
    goals: { primary_goal: "Prove the draft path", target_audience: "contractor", key_message: "no ADR writes before approval" },
    architecture: { building_blocks: ["approval seam"], connection_summary: "tool -> writer", data_flow: "draft -> review -> approve" },
    tech_stack: [{ layer: "cli", choice: "pic", rationale: "canonical", reuse: "yes" }],
    file_structure: [{ path: "docs/adr", purpose: "decision records" }],
    rri_requirements_matrix: [{ blueprint_section: "ADR seam", requirements: ["REQ-F2-3"], source_questions: ["Q1"] }],
    task_decomposition_preview: { estimated_tasks: 1, estimated_effort_minutes: 30, tasks: [{ tip_id: "TIP-1", title: "Seam", goal: "Prove the draft path writes no ADR files" }] },
    adr_candidates: CANDIDATES,
  });
  await withPicStub("ok", async (picLog) => {
    const root = tempRoot();
    try {
      const saved = await runTool(tool, { action: "save_blueprint_draft", id: "wi-adrdraft", stage: "blueprint", content: blueprint }, root);
      assert.equal(saved.isError, undefined, `unexpected save error: ${saved.content[0]?.text}`);
      const draftId = (saved.details as { draft_id?: string }).draft_id;
      assert.ok(draftId);
      const reviewed = await runTool(tool, { action: "review_blueprint_checkpoint", id: "wi-adrdraft", artifact_id: draftId, content: JSON.stringify({ architecture: true, design: true, requirements: true, task_decomposition: true, nothing_missing: true }), actor_role: "contractor" }, root);
      assert.equal(reviewed.isError, undefined, `unexpected review error: ${reviewed.content[0]?.text}`);
      assert.equal(existsSync(picLog), false, "no pic CLI invocation (hence no ADR write) may happen during draft save or review");
      assert.equal(existsSync(join(root, "docs", "adr")), false);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});
