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
    descendants ? `## Descendants\n${descendants}` : "",
    "",
    "Then call `verify_aggregate_work_item` with this Work Item ID, `verification_status` passed, failed, partial, or blocked, a concise evidence summary, and `actor_role=contractor`.",
    "Do not call owner acceptance. A passed aggregate verification creates the single owner decision gate; a failed result must identify targeted corrections.",
  ].filter(Boolean).join("\n");
}

export function buildWorkItemContinuePrompt(status: { work_item_id: string; next_stage: string }, item: Pick<WorkItemPrompt, "title" | "type">): string {
  const actions: Record<string, string> = {
    scan: `Inspect the Scout report and existing drafts. Validate the evidence against source, resolve contradictions, and save the canonical Scan Report with \`save_work_item_artifact\` as structured XML matching this schema:\n\n${CANONICAL_SCAN_REPORT_XML_FORMAT}\n\nDo not format owner-facing Markdown; the tool renders the saved XML deterministically. If inaccurate, call \`reject_work_item_scan\` with actor_role=contractor and a concrete reason; wait for the owner to decide whether to reset and rescan.`,
    rri: "Publish the RRI artifact with `save_work_item_artifact`, then request owner acceptance with `approve_work_item_artifact`.",
    vision: "Publish the Vision artifact with `save_work_item_artifact`, then request owner acceptance with `approve_work_item_artifact`.",
    blueprint: "Publish the Blueprint artifact with `save_work_item_artifact`, then request owner acceptance with `approve_work_item_artifact`.",
    contracts: "Publish the Contracts artifact with `save_work_item_artifact`, then request owner acceptance with `approve_work_item_artifact`.",
    task_graph: ["task", "bug", "chore"].includes(item.type || "")
      ? "Publish a requirement-covering task graph with exactly one executable node matching the existing Work Item type, no parent, and no dependencies. This node specifies the TIP for the existing Work Item; it must not create or decompose Work Items. Save it with `save_work_item_artifact`, validate it with `validate_work_item_graph`, then present it to the owner. Call `approve_work_item_artifact` with `actor_role=owner` only after explicit approval."
      : "Publish the complete requirement-covering task graph with `save_work_item_artifact`, validate the saved draft with `validate_work_item_graph`, then present the validated graph to the owner. Call `approve_work_item_artifact` with `actor_role=owner` only after their explicit approval.",
    materialize: "Materialize the approved task graph with `materialize_work_item`; do not create child Work Items manually before this stage.",
    authorize: "Ask the owner for implementation authorization. Call `authorize_work_item_implementation` with `actor_role=owner` only after their explicit approval.",
    implement: ["task", "bug", "chore"].includes(item.type || "")
      ? "Launch the executable Work Item from its active TIP with `work_on_work_item`."
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