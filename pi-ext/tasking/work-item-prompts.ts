export interface WorkItemPrompt {
  id?: string;
  title: string;
  type?: string;
  description?: string;
}

// Planning handoff constraint: every planning stage handoff must carry and
// validate Work Item identity, the dispatched stage, the predecessor checkpoint,
// and the persisted Plan profile version/hash so a consumer can reject a stale
// or mismatched stage binding.
export interface PlanningHandoffAttributes {
  work_item_id: string;
  stage: string;
  predecessor_checkpoint: string;
  profile_version: string;
  profile_hash: string;
}

export const PLANNING_HANDOFF_STAGES = ["rri", "vision", "blueprint", "task_graph"] as const;

export function normalizePlanningHandoffAttributes(attrs: Record<string, unknown>): PlanningHandoffAttributes {
  return {
    work_item_id: String(attrs.work_item_id || ""),
    stage: String(attrs.stage || ""),
    predecessor_checkpoint: String(attrs.predecessor_checkpoint || ""),
    profile_version: String(attrs.profile_version ?? ""),
    profile_hash: String(attrs.profile_hash || ""),
  };
}

function xmlEscapeAttribute(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&apos;");
}

/**
 * Validate planning handoff attributes against the bounded planning stages.
 * Throws when identity, stage, or the required profile version/hash binding is
 * malformed or references an unsupported stage.
 */
export function assertPlanningHandoffAttributes(attrs: PlanningHandoffAttributes): void {
  if (!attrs.work_item_id) throw new Error("planning handoff missing Work Item identity");
  if (!PLANNING_HANDOFF_STAGES.includes(attrs.stage as (typeof PLANNING_HANDOFF_STAGES)[number])) {
    throw new Error(`planning handoff has unsupported stage ${attrs.stage || "none"}`);
  }
  if (!attrs.profile_version || !attrs.profile_hash) {
    throw new Error(`planning ${attrs.stage} handoff requires a persisted profile version and hash`);
  }
}

/**
 * Build a validated planning handoff envelope that binds Work Item identity,
 * stage, predecessor checkpoint, and profile version/hash to the stage payload.
 */
export function buildPlanningHandoffXml(attrs: PlanningHandoffAttributes, body: string): string {
  assertPlanningHandoffAttributes(attrs);
  const escaped = {
    work_item_id: xmlEscapeAttribute(attrs.work_item_id),
    stage: xmlEscapeAttribute(attrs.stage),
    predecessor_checkpoint: xmlEscapeAttribute(attrs.predecessor_checkpoint),
    profile_version: xmlEscapeAttribute(attrs.profile_version),
    profile_hash: xmlEscapeAttribute(attrs.profile_hash),
  };
  // Planning handoff constraint: wrap the stage payload in CDATA so raw output
  // containing XML metacharacters or a literal </planning_handoff> cannot corrupt
  // the envelope, and split any embedded terminator so the block stays well-formed.
  const cdataBody = `<![CDATA[${body.replace(/]]>/g, "]]]]><![CDATA[>")}]]>`;
  return `<planning_handoff schema_version="1" work_item_id="${escaped.work_item_id}" stage="${escaped.stage}" predecessor_checkpoint="${escaped.predecessor_checkpoint}" profile_version="${escaped.profile_version}" profile_hash="${escaped.profile_hash}">\n<body>\n${cdataBody}\n</body>\n</planning_handoff>`;
}

/**
 * Extract and validate the binding attributes of a planning handoff envelope.
 */
export function parsePlanningHandoffAttributes(xml: string): PlanningHandoffAttributes {
  const match = xml.match(/^<planning_handoff\s([^>]+)>[\s\S]*?<\/planning_handoff>$/);
  if (!match) throw new Error("planning handoff must be one <planning_handoff> document");
  const parsed = match[1]!.match(/([a-zA-Z_][\w.-]*)="([^"]*)"/g)?.reduce<Record<string, string>>((values, attribute) => {
    const found = attribute.match(/^([a-zA-Z_][\w.-]*)="([^"]*)"$/);
    if (found) values[found[1]!] = found[2]!;
    return values;
  }, {}) || {};
  const attrs = normalizePlanningHandoffAttributes(parsed);
  assertPlanningHandoffAttributes(attrs);
  return attrs;
}

export interface WorkItemChecklistEntry {
  id?: string;
  content: string;
}

export interface WorkItemArtifact {
  id?: string;
  summary?: string;
}

export const CANONICAL_SCAN_REPORT_XML_FORMAT = `<scan_report>
  <tech_stack><language>...</language><framework>...</framework><styling>...</styling><database>...</database><auth>...</auth><state>...</state><other>...</other></tech_stack>
  <existing_modules><module><name>...</name><description>...</description></module></existing_modules>
  <patterns_detected><pattern><name>...</name><location>...</location></pattern></patterns_detected>
  <reusable_components><component><name>...</name><path>...</path><purpose>...</purpose></component></reusable_components>
  <gaps_detected><gap><name>...</name><description>...</description></gap></gaps_detected>
  <code_health><type_safety>...</type_safety><linting>...</linting><tests>...</tests><debug_artifacts>...</debug_artifacts><todo_fixme>...</todo_fixme></code_health>
  <estimated_size><files>...</files><lines_of_code>...</lines_of_code><components_modules>...</components_modules><api_routes_endpoints>...</api_routes_endpoints></estimated_size>
</scan_report>`;

export function buildTaskVerifyPrompt(data: any): string {
  const item = data.work_item || {};
  const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
  const completion = (data.completion_reports || []).find((report: any) => report.status === "done"
    && (!activePack?.id || report.instruction_pack_id === activePack.id)
    && (!activePack?.version || report.instruction_pack_version === activePack.version)
    && (!activePack?.content_hash || report.instruction_pack_hash === activePack.content_hash));
  let pack: any = {};
  try { pack = JSON.parse(activePack?.content_json || "{}"); } catch {}
  const checks = (pack.verification || []).flatMap((check: any) => [
    ...(check.setup_commands || []).map((command: string) => ({ command, setup: true })),
    { command: check.command, required: check.required !== false, expected: check.expected },
  ]).filter((check: any) => check.command);
  const commands = checks.length
    ? checks.map((check: any, index: number) => `${index + 1}. \`${check.command}\`${check.setup ? " (setup)" : ""}${check.expected ? ` -> ${check.expected}` : ""}`).join("\n")
    : "No persisted commands were found; inspect the active TIP and run its stated verification requirements.";
  return [
    `# CONTRACTOR VERIFICATION: ${item.title || item.id || "Work Item"}`,
    `Work Item: ${item.id || "unknown"}`,
    `Completion Report: ${completion?.id || "unknown"}`,
    "",
    "The reviewed implementation is integrated. Execute this verification now in the current repository.",
    "You are the main contractor: do not delegate this step, do not merely describe it, and do not modify the task-system extension.",
    "",
    "## Required Commands",
    commands,
    "",
    "Run every required command, inspect the integrated diff and relevant behavior, and record concrete pass/fail evidence.",
    "Then call `verify_work_item` with this Work Item ID, the exact Completion Report ID above, `verification_status` of passed, failed, or blocked, a concise evidence summary, and `actor_role=contractor`.",
    "After a passed executable-child verification, the child closes automatically; do not call `accept_work_item`. Inspect the parent aggregate if this was its final descendant.",
  ].join("\n");
}

export function buildAggregateVerifyPrompt(data: any): string {
  const item = data.work_item || {};
  const descendants = (data.children || []).map((child: any) => `- ${child.id}: ${child.title} (${child.status})`).join("\n");
  return [
    `# AGGREGATE VERIFICATION: ${item.title || item.id || "Work Item"}`,
    `Work Item: ${item.id || "unknown"}`,
    "",
    "All required executable descendants are complete. Execute the final aggregate verification now in the current repository.",
    "Inspect the integrated diff and every descendant Completion Report and contractor Verification Report. Run the repository-level checks required by the approved contract and verify each aggregate requirement end to end.",
    "Apply RRI-T to the integrated delivery: select applicable End User, Business Analyst, QA / Tester, Developer, and Operator perspectives from the changed scope, run only risk-relevant dimension x stress-axis scenarios, and record N/A reasons for omitted areas. Every scenario must name an approved REQ-ID and executable evidence. Preserve PASS, ACCEPTABLE, PAINFUL, and FAIL results in the evidence summary; ACCEPTABLE requires an owner tradeoff, PAINFUL requires remediation or owner deferral, and FAIL blocks verification.",
    descendants ? `## Descendants\n${descendants}` : "",
    "",
    "Then call `verify_aggregate_work_item` with this Work Item ID, `verification_status` passed, failed, partial, or blocked, a concise evidence summary containing the RRI-T scenarios, and `actor_role=contractor`.",
    "Do not call owner acceptance. A passed aggregate verification creates the single owner decision gate; a failed or partial result must identify targeted corrections and retain the RRI-T evidence.",
  ].filter(Boolean).join("\n");
}

export function buildWorkItemContinuePrompt(status: { work_item_id: string; next_stage: string }, item: Pick<WorkItemPrompt, "title" | "type">): string {
  const actions: Record<string, string> = {
    scan: `Inspect the Scout report and existing drafts. Validate the evidence against source, resolve contradictions, and save the canonical Scan Report with \`save_work_item_artifact\` as structured XML matching this schema:\n\n${CANONICAL_SCAN_REPORT_XML_FORMAT}\n\nDo not format owner-facing Markdown; the tool renders the saved XML deterministically. If inaccurate, call \`reject_work_item_scan\` with actor_role=contractor and a concrete reason; wait for the owner to decide whether to reset and rescan.`,
    rri: "Continue the owner interview from `load_rri_interview` when a draft exists. Checkpoint disposable progress with `checkpoint_rri_interview`; after all owner-impacting P0/P1 questions are resolved and the requirements are testable, publish exactly once with `save_rri_interview`. Its content must be exactly this JSON shape: {\"requirements\":[{\"key\":str,\"priority\":str,\"title\":str,\"description\":str,\"acceptanceCriteria\":str}],\"decisions\":[{\"key\":str,\"answer\":str}],\"report\":{\"project_name\":str,\"generated\":ISO-timestamp,\"requirements_matrix\":[{\"req_id\":str,\"requirement\":str,\"source\":str,\"priority\":str,\"persona\":str}],\"auto_answered\":[{\"topic\":str,\"details\":str,\"resolution\":str}],\"decisions_log\":[{\"decision\":str,\"options_considered\":str,\"chosen\":str,\"rationale\":str}],\"open_questions\":[{\"id\":str,\"question\":str}]}}. Note the exact camelCase key `acceptanceCriteria`; requirements and decisions must be nonempty, every report row field is required, and requirements_matrix plus decisions_log must match the payload requirements and decisions one-for-one by key and title. Then request owner approval with `approve_work_item_artifact`.",
    vision: "As Contractor, consume the approved Scan and RRI artifacts via `load_planning_artifact`. Save exactly one Vision JSON artifact with `save_work_item_artifact`, matching this shape with every field present and every array nonempty: {\"project_name\":str,\"nature\":{\"interface\":str,\"lifecycle\":str,\"scale\":str},\"dimensions\":{\"interface\":str,\"data_flow\":str,\"user_model\":str,\"lifecycle\":str,\"scale\":str,\"state\":str},\"architecture\":{\"entry_points\":[str],\"core_modules\":[str],\"data_layer\":[str],\"integration_points\":[str],\"cross_cutting_concerns\":[str],\"connection_summary\":str},\"user_flows\":[{\"user_type\":str,\"entry\":str,\"core_loop\":str,\"edge_cases\":[str],\"exit\":str}],\"tech_stack\":[{\"layer\":str,\"choice\":str,\"rationale\":str,\"reuse\":str}],\"design_direction\":{\"layout_ascii\":str,\"font_pairing\":str,\"primary_color\":str,\"density\":str,\"motion\":str,\"rationale\":str}}. For non-UI projects replace design_direction with \"non_ui_direction\":{\"type\":str,\"decisions\":[str]}; exactly one of the two is required. Then present its rendered Markdown to the owner. Do not approve it.",
    blueprint: "Consume the XML blueprint_handoff schema version 2 containing only Work Item identity, project root, and approved artifact lineage metadata. Load scan, rri, and vision with `load_planning_artifact`, verify their IDs/revisions/hashes, and then produce the Blueprint JSON containing all required top-level sections: project_info, goals, architecture, tech_stack, file_structure, rri_requirements_matrix, and task_decomposition_preview with estimated_tasks matching its tasks array length and estimated_effort_minutes >= 1. Do not use historical revisions. Save it as a temporary draft with `save_blueprint_draft` and retain the returned draft ID. After planner completion, load that temporary state with `load_blueprint_draft`; use its `draft_id` as the `artifact_id` argument for `review_blueprint_checkpoint`. The Contractor must review that draft, revise it through the same temporary-draft loop when any checkpoint item fails, and call `review_blueprint_checkpoint` only when all five checks pass, passing `content` as the JSON object `{\"architecture\":true,\"design\":true,\"requirements\":true,\"task_decomposition\":true,\"nothing_missing\":true}` with `actor_role=contractor`. Present the checked draft to the owner. Do not call `save_work_item_artifact` or any owner approval action; the owner approves the reviewed draft with `approve_blueprint_draft`, which performs the one canonical save and approval.",
    contracts: "As Contractor, consume the approved Blueprint, RRI, and Vision artifacts via `load_planning_artifact`. Draft one Contract JSON object matching exactly this shape: {\"project_name\":str,\"deliverables\":[{\"item\":str,\"details\":str,\"requirements\":[\"RRI-key\"]}],\"tech_stack\":[{\"layer\":str,\"choice\":str,\"rationale\":str}],\"task_graph_summary\":{\"tip_count\":int>=1,\"estimated_minutes\":int>=1},\"not_included\":[str],\"obligation_schema_version\":2,\"obligations\":[{\"id\":\"OB-unique\",\"requirement_keys\":[\"RRI-key\"],\"behavior\":str,\"acceptance\":\"Given/When/Then text\"}]}. Every deliverable needs item, details, and at least one requirement key; obligation ids must be unique and each obligation is atomic behavior with Given/When/Then acceptance, together covering every non-deferred requirement. Obligation objects carry ONLY id, requirement_keys, behavior, and acceptance: provides, consumes, evidence_for, and obligation_keys belong exclusively to Task Graph nodes at the next stage, never to Contract obligations. Before saving, present the draft to the owner as readable Markdown — deliverables with their requirement keys, each obligation's behavior and Given/When/Then acceptance, tech stack, and not-included list — never dump the raw JSON payload. Ask the owner to reply exactly `CONFIRM`; do not save it until confirmation. That CONFIRM is explicit owner approval: save the Contract, then approve that current Contract artifact with actor_role=owner so Task Graph planning can start.",
    task_graph: ["task", "bug", "chore"].includes(item.type || "")
      ? "Publish a requirement-covering task graph with exactly one executable node matching the existing Work Item type, no parent, and no dependencies. This node specifies the TIP for the existing Work Item; it must not create or decompose Work Items. Save it with `save_work_item_artifact`, validate it with `validate_work_item_graph`, then present it to the owner. Call `approve_work_item_artifact` with `actor_role=owner` only after explicit approval."
      : "Publish the complete requirement-covering task graph with `save_work_item_artifact`, validate it with `validate_work_item_graph`, then present it to the owner. Prefer small nodes with disjoint file ownership. Run independent Data, Core Logic, Interface, and Secondary lanes in parallel; make Integration depend on every lane it consumes; finish with Polish + Test and VERIFY. Add ordering dependencies whenever ready nodes would overlap files. Call `approve_work_item_artifact` only after explicit owner approval.",
    materialize: "Materialize the approved task graph with `materialize_work_item`; do not create child Work Items manually before this stage.",
    authorize: "Ask the owner for implementation authorization. Call `authorize_work_item_implementation` with `actor_role=owner` only after their explicit approval.",
    implement: ["task", "bug", "chore"].includes(item.type || "")
      ? "Launch the executable Work Item with `work_on_work_item`; its TIP is generated and frozen transactionally before the first worker claim."
      : `Launch only authorized dependency-ready executable descendants. Do not launch this ${item.type || "aggregate"} Work Item as a worker.`,
    contractor_verification: "Verify the integrated completion evidence and publish the verdict with `verify_work_item` using `actor_role=contractor`; a passed executable child closes automatically, while a passed aggregate advances to aggregate verification/owner acceptance.",
    aggregate_verification: "Run the aggregate's final verification over all completed descendants and publish it with `verify_aggregate_work_item` using `actor_role=contractor`.",
    owner_acceptance: ["epic", "feature"].includes(item.type || "")
      ? "Persist the one final aggregate decision with `accept_aggregate_work_item` using `actor_role=owner`."
      : "Executable children do not require owner acceptance; inspect the parent aggregate workflow.",
    merge_pending: "Retry the bound delivery merge with `merge_aggregate_work_item`; do not rerun completed child work.",
    done: "The Work Item lifecycle is complete.",
  };
  return [`# WORK ITEM WORKFLOW: ${item.title || status.work_item_id}`, `Work Item: ${status.work_item_id}`, `Current stage: ${status.next_stage}`, "", actions[status.next_stage] || `No action mapping exists for persisted stage ${status.next_stage}.`].join("\n");
}

export function buildWorkItemScanPrompt(item: WorkItemPrompt, project?: { name?: string; root_path?: string }): string {
  return [
    `# SCAN Work Item: ${item.title}`,
    `Work Item: ${item.id || "unknown"}`,
    project?.name ? `Project: ${project.name}` : "",
    project?.root_path ? `Root: ${project.root_path}` : "",
    item.description || "",
    "You are the read-only task-scout. Inspect the relevant repository scope without modifying files. Record stack, architecture, commands, reusable patterns, and risks with source evidence.",
    "Return evidence for your assigned Scan section only. Do not synthesize the canonical Scan Report or mutate Work Item state.",
  ].filter(Boolean).join("\n\n");
}

export function buildWorkItemDebugPrompt(item: WorkItemPrompt, context: { scanReports?: WorkItemArtifact[]; trigger?: string; evidence?: string } = {}): string {
  const scanEvidence = (context.scanReports || []).map((report) => `- ${report.id || "scan"}: ${report.summary || "No summary"}`).join("\n");
  return [
    `# DEBUG Work Item: ${item.title}`,
    `Work Item: ${item.id || "unknown"}`,
    `Trigger: ${context.trigger || "manual"}`,
    context.evidence ? `Initial evidence: ${context.evidence}` : "",
    item.description || "",
    scanEvidence ? `## Scan Evidence\n${scanEvidence}` : "",
    "Use EVIDENCE -> REPRODUCE -> ROOT CAUSE -> REGRESSION TEST -> FIX -> VERIFY. Return pipeline evidence; do not mutate Work Item lifecycle state directly.",
  ].filter(Boolean).join("\n\n");
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