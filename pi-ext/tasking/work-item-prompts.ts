export interface WorkItemPrompt {
  id?: string;
  title: string;
  type?: string;
  description?: string;
}

export interface WorkItemChecklistEntry {
  id?: string;
  content: string;
}

export interface WorkItemArtifact {
  id?: string;
  summary?: string;
}

export interface RriTScenarioContext {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  artifact: any;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  content: any;
}

// RRI-T execution ordering: the contractor loads scenarios from the persisted
// rri_t_scenarios artifact revision — never from an in-memory persona result —
// so resumed verification reuses saved scenarios without re-running persona
// subagents and grading can only reference persisted scenario identities.
// RRI-T artifact ownership: a scenario artifact belongs to the Work Item that
// persisted it, so a feature aggregate must never grade a parent aggregate's
// higher-revision scenarios. Whenever rows carry work_item_id (pic show always
// does), rows owned by another Work Item are excluded; only legacy/mocked rows
// without ownership metadata fall back to revision order.
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function latestRriTScenarios(data: any): RriTScenarioContext | undefined {
  const workItemId = data?.work_item?.id;
  const artifact = (Array.isArray(data?.artifacts) ? data.artifacts : [])
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    .filter((entry: any) => entry.stage === "rri_t_scenarios")
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    .filter((entry: any) => !entry.work_item_id || !workItemId || String(entry.work_item_id) === String(workItemId))
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    .sort((a: any, b: any) => Number(b.revision || 0) - Number(a.revision || 0))[0];
  if (!artifact) return undefined;
  try {
    const content = JSON.parse(String(artifact.content ?? "{}"));
    if (!content || typeof content !== "object" || !Array.isArray(content.scenarios)) return undefined;
    return { artifact, content };
  } catch {
    return undefined;
  }
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function buildTaskVerifyPrompt(data: any): string {
  const item = data.work_item || {};
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const completion = (data.completion_reports || []).find((report: any) => report.status === "done"
    && (!activePack?.id || report.instruction_pack_id === activePack.id)
    && (!activePack?.version || report.instruction_pack_version === activePack.version)
    && (!activePack?.content_hash || report.instruction_pack_hash === activePack.content_hash));
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  let pack: any = {};
  try { pack = JSON.parse(activePack?.content_json || "{}"); } catch {}
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const checks = (pack.verification || []).flatMap((check: any) => [
    ...(check.setup_commands || []).map((command: string) => ({ command, setup: true })),
    { command: check.command, required: check.required !== false, expected: check.expected },
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  ]).filter((check: any) => check.command);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const setupCommands = checks.filter((check: any) => check.setup);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const requiredCommands = checks.filter((check: any) => !check.setup);
  const commandLines = requiredCommands.length
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    ? requiredCommands.map((check: any, index: number) => `${index + 1}. \`${check.command}\`${check.expected ? ` -> ${check.expected}` : ""}`).join("\n")
    : "No persisted commands were found; inspect the active TIP and run its stated verification requirements.";
  return [
    `# CONTRACTOR VERIFICATION: ${item.title || item.id || "Work Item"}`,
    `Work Item: ${item.id || "unknown"}`,
    `Completion Report: ${completion?.id || "unknown"}`,
    "",
    "The reviewed implementation is integrated. Execute this verification now in the current repository.",
    "You are the main contractor: do not delegate this step, do not merely describe it, and do not modify the task-system extension.",
    "",
    ...(setupCommands.length ? [
      "## PREREQUISITES (run before the required commands)",
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
      ...setupCommands.map((check: any) => `- \`${check.command}\``),
      "If a prerequisite fails to start, report the verification as blocked (environment_blocked) with the concrete failure output; do not modify infrastructure, install global tooling, or weaken the verification to make it pass.",
      "",
    ] : []),
    "## Required Commands",
    commandLines,
    "",
    "Run every required command, inspect the integrated diff and relevant behavior, and record concrete pass/fail evidence.",
    "Then call `verify_work_item` with this Work Item ID, the exact Completion Report ID above, `verification_status` of passed, failed, or blocked, a concise evidence summary, and `actor_role=contractor`.",
    "After a passed executable-child verification, the child closes automatically; do not call `accept_work_item`. Inspect the parent aggregate if this was its final descendant.",
  ].join("\n");
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function buildAggregateVerifyPrompt(data: any): string {
  const item = data.work_item || {};
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const descendants = (data.children || []).map((child: any) => `- ${child.id}: ${child.title} (${child.status})`).join("\n");
  const scenarios = latestRriTScenarios(data);
  const artifactLine = scenarios?.artifact ? `Loaded from artifact ${scenarios.artifact.id} (revision ${scenarios.artifact.revision || 1}, content hash ${scenarios.artifact.content_hash || "unknown"}) — never from in-memory persona output.` : "";
  const scenarioLines = scenarios
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
    ? scenarios.content.scenarios.map((scenario: any, index: number) => `- [${index + 1}] ${scenario.persona} · ${scenario.dimension}/${scenario.stress_axis} (${scenario.requirement_id}, ${scenario.id || "unnamed"}): ${scenario.procedure}${scenario.remediation_hint ? ` — remediation hint: ${scenario.remediation_hint}` : ""}`).join("\n")
    : "_No persisted rri_t_scenarios artifact was found. Author the scenarios in this session first (see /apm review): derive risk-relevant persona x dimension x stress-axis scenarios from the approved requirements, then save them with `save_work_item_artifact` stage `rri_t_scenarios` as a JSON object ({personas, scenarios: [{id, persona, dimension, stress_axis, requirement_id, procedure, remediation_hint}], not_applicable, open_blockers}). Aggregate verification stays blocked until the authored list is persisted; grading below fails closed on an unpersisted list._";
  return [
    `# AGGREGATE VERIFICATION: ${item.title || item.id || "Work Item"}`,
    `Work Item: ${item.id || "unknown"}`,
    "",
    "All required executable descendants are complete. Execute the final aggregate verification now in the current repository.",
    "Inspect the integrated diff and every descendant Completion Report and contractor Verification Report. Run the repository-level checks required by the approved contract and verify each aggregate requirement end to end.",
    "",
    "## Persisted RRI-T Scenarios",
    artifactLine,
    scenarioLines,
    "",
    "Execute only scenarios retained from this persisted list. Scenario authoring is in-session contractor methodology work (no persona subagents); once the list is persisted, do not amend or re-author it — grading must bind to the saved artifact.",
    "",
    "## Owner Scenario Gate (soft)",
    "Present this scenario list to the owner and ask whether any scenario should be trimmed or deferred before execution. Honor explicit owner trim or defer instructions; when no owner response is given, proceed with the retained scenarios without stalling.",
    "",
    "## Execute and Grade in This Session (contractor only)",
    "Apply RRI-T to the integrated delivery using the persisted scenario list. Run each retained scenario's procedure against the integrated repository with concrete commands in this main session and record the executed command and observed output as evidence. You are the main contractor: no subagent executes procedures or produces grades, do not delegate this step, and never grade a scenario you did not execute.",
    "Each retained scenario receives exactly one outcome: PASS, ACCEPTABLE, PAINFUL, and FAIL grade the executed procedure with evidence, or not_applicable with a concrete reason when the procedure cannot execute against the integrated repository (recorded instead of failing verification). Every graded scenario must name an approved REQ-ID and executable evidence. ACCEPTABLE requires an owner tradeoff, PAINFUL requires remediation or explicit owner deferral before acceptance, and FAIL blocks aggregate verification.",
    descendants ? `## Descendants\n${descendants}` : "",
    "",
    "## Submit",
    "Then call `verify_aggregate_work_item` with this Work Item ID, `verification_status` passed, failed, partial, or blocked, a `summary` evidence summary, the graded scenario JSON as `rri_t_evidence_json` ({\"scenarios\":[{\"id\":\"<scenario id verbatim from artifact>\",\"persona\":\"QA / Tester\",\"dimension\":\"D3\",\"stress_axis\":\"ERROR\",\"requirement_id\":\"REQ-1\",\"procedure\":\"<verbatim from artifact>\",\"evidence\":\"<command run and observed output>\",\"result\":\"PASS\"}],\"not_applicable\":[{\"id\":\"<scenario id verbatim from artifact>\",\"persona\":\"QA / Tester\",\"dimension\":\"D3\",\"stress_axis\":\"ERROR\",\"requirement_id\":\"REQ-1\",\"reason\":\"<why it cannot run>\"}]}), and `actor_role=contractor`.",
    "Do not call owner acceptance. A passed aggregate verification creates the single owner decision gate; a failed or partial result must identify targeted corrections and retain the RRI-T evidence.",
    "## Acceptance Brief (mandatory before the owner decides)",
    "When asking the owner to approve the aggregate, present the acceptance brief first, in this exact form (same headings): '## Acceptance brief — <aggregate id> (<title>)' with sections '**What it delivered**' (concrete deliverables with measured evidence, verified requirements/tasks), '**Gaps (deliberate, not defects)**' (numbered, each with its owning requirement/task), '**Open questions**' (numbered, each requiring an owner decision), and an '**Ask:**' line naming the exact accept call. Never request acceptance without the brief.",
  ].filter(Boolean).join("\n");
}

export function formatWorkItemChecklist(items: WorkItemChecklistEntry[], done: boolean): string {
  if (items.length === 0) return done ? "_No completed entries_\n" : "_No pending entries_\n";
  return items.map((item) => `- [${done ? "x" : " "}]${item.id ? ` (id: ${item.id})` : ""} ${item.content}`).join("\n") + "\n";
}

export function buildWorkItemReviewerHandoff(workItemId: string): string {
  return `Run the read-only review for Work Item ${workItemId}. Load the complete review context with task_manager action trigger_work_item_review, then inspect and return the canonical review report.`;
}

export function buildReviewInstructions(_workItemId: string): string {
  return [
    "## Review Instructions",
    "Review the candidate patch against the active Work Item instruction pack.",
    "1. Check requirements, scope, constraints, and acceptance gates.",
    "2. Check the patch for defects, security issues, scope creep, and missing verification.",
    "3. Return exactly one canonical review-report block with a passed or failed verdict.",
    "4. Do not mutate Work Item state; the scheduler persists the result.",
  ].join("\n") + "\n";
}

export { CANONICAL_SCAN_REPORT_XML_FORMAT, buildWorkItemContinuePrompt, buildWorkItemDebugPrompt, buildWorkItemScanPrompt } from "./workflow-stage-prompts.ts";
export {
  PLANNING_HANDOFF_STAGES,
  assertPlanningHandoffAttributes,
  buildPlanningHandoffXml,
  normalizePlanningHandoffAttributes,
  parsePlanningHandoffAttributes,
  type PlanningHandoffAttributes,
} from "./planning-handoff.ts";
export { buildStagePrimer, type StagePrimerContext, type StagePrimerDigest, type StagePrimerProfile } from "./stage-primer.ts";
export { buildWorkProgressLedger, type LedgerFailedVerification, type LedgerPriorReport, type WorkProgressLedgerInput } from "./progress-ledger.ts";
