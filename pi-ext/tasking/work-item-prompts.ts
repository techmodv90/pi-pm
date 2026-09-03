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

export const CANONICAL_SCAN_REPORT_XML_FORMAT = `<scan_report>
  <tech_stack><language>...</language><framework>...</framework><styling>...</styling><database>...</database><auth>...</auth><state>...</state><other>...</other></tech_stack>
  <existing_modules><module><name>...</name><description>...</description></module></existing_modules>
  <patterns_detected><pattern><name>...</name><location>...</location></pattern></patterns_detected>
  <reusable_components><component><name>...</name><path>...</path><purpose>...</purpose></component></reusable_components>
  <gaps_detected><gap><name>...</name><description>...</description></gap></gaps_detected>
  <code_health><type_safety>...</type_safety><linting>...</linting><tests>...</tests><debug_artifacts>...</debug_artifacts><todo_fixme>...</todo_fixme></code_health>
  <estimated_size><files>...</files><lines_of_code>...</lines_of_code><components_modules>...</components_modules><api_routes_endpoints>...</api_routes_endpoints></estimated_size>
</scan_report>`;

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
    : "_No persisted rri_t_scenarios artifact was found; aggregate verification is blocked until the authored scenarios are saved before execution._";
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
    "Execute only scenarios retained from this persisted list. Do not run, amend, or re-author persona output, and do not re-run persona subagents.",
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
  ].filter(Boolean).join("\n");
}

export function buildWorkItemContinuePrompt(status: { work_item_id: string; next_stage: string }, item: Pick<WorkItemPrompt, "title" | "type">): string {
  const actions: Record<string, string> = {
    scan: `Inspect the Scout report and existing drafts. Validate the evidence against source, resolve contradictions, and save the canonical Scan Report with \`save_work_item_artifact\` as structured XML matching this schema:\n\n${CANONICAL_SCAN_REPORT_XML_FORMAT}\n\nDo not format owner-facing Markdown; the tool renders the saved XML deterministically. If inaccurate, call \`reject_work_item_scan\` with actor_role=contractor and a concrete reason; wait for the owner to decide whether to reset and rescan.`,
    rri: "Before asking any RRI questions, read the repository root `CONTEXT.md` and applicable `docs/adr/*.md`, and use their canonical terms in every question, requirement title, and decision; if those sources are unavailable, report the missing source to the owner instead of inventing canonical terms and continue with terminology marked unresolved in `not_yet_specified`. Repository context is read-only during the interview: never write `CONTEXT.md`, ADRs, or any glossary file, and surface terminology uncertainty for owner resolution instead of silently renaming terms. As Contractor, apply all relevant RRI persona lenses yourself in one session: End User, Business Analyst, QA / Tester, Developer, and Operator. Use `rri-personas.md` and `rri-question-bank.md`; auto-answer evidence-backed questions, mark irrelevant topics with reasons, deduplicate the decision frontier, and use grilling-style rounds with one recommendation per owner-impacting question. Continue the owner interview from `load_rri_interview` when a draft exists. Checkpoint disposable progress with `checkpoint_rri_interview`; after all owner-impacting P0/P1 questions are resolved and the requirements are testable, publish exactly once with `save_rri_interview`. Do not spawn or request `rri-persona` subagents. Its content must be exactly this JSON shape: {\"requirements\":[{\"key\":str,\"priority\":str,\"title\":str,\"description\":str,\"acceptanceCriteria\":str}],\"decisions\":[{\"key\":str,\"answer\":str}],\"report\":{\"project_name\":str,\"generated\":ISO-timestamp,\"rri_policy_version\":2,\"requirements_matrix\":[{\"req_id\":str,\"requirement\":str,\"source\":str,\"priority\":str,\"persona\":str}],\"auto_answered\":[{\"topic\":str,\"details\":str,\"resolution\":str}],\"decisions_log\":[{\"decision\":str,\"options_considered\":str,\"chosen\":str,\"rationale\":str}],\"not_yet_specified\":[{\"uncertainty\":str,\"graduation_path\":str}],\"out_of_scope\":[{\"exclusion\":str,\"reason\":str}],\"open_questions\":[{\"id\":str,\"question\":str,\"status\":\"open|resolved|deferred\",\"priority\":\"P0|P1|P2|P3\",\"mode\":\"afk|hitl\",\"blocks\":bool,\"resolution\":{\"answer\":str,\"source\":str}(required when status is resolved or deferred, omitted when open)}]}}. Note the exact camelCase key `acceptanceCriteria`; requirements and decisions must be nonempty, every report row field is required, and requirements_matrix plus decisions_log must match the payload requirements and decisions one-for-one by key and title. The report carries rri_policy_version 2, so every open_questions row must carry status, priority, mode, and blocks, and resolved or deferred rows must carry a resolution with answer and source. The report also requires both scope sections: not_yet_specified records each in-scope uncertainty with its graduation path, and out_of_scope records each explicit exclusion with the owner reason. Never add a Destination section because Work Item goals remain the destination authority. Then request owner approval with `approve_work_item_artifact`.",
    vision: "As Contractor, consume the approved Scan and RRI artifacts via `load_planning_artifact`. Save exactly one Vision JSON artifact with `save_work_item_artifact`, matching this shape with every field present and every array nonempty: {\"project_name\":str,\"nature\":{\"interface\":str,\"lifecycle\":str,\"scale\":str},\"dimensions\":{\"interface\":str,\"data_flow\":str,\"user_model\":str,\"lifecycle\":str,\"scale\":str,\"state\":str},\"architecture\":{\"entry_points\":[str],\"core_modules\":[str],\"data_layer\":[str],\"integration_points\":[str],\"cross_cutting_concerns\":[str],\"connection_summary\":str},\"user_flows\":[{\"user_type\":str,\"entry\":str,\"core_loop\":str,\"edge_cases\":[str],\"exit\":str}],\"tech_stack\":[{\"layer\":str,\"choice\":str,\"rationale\":str,\"reuse\":str}],\"design_direction\":{\"layout_ascii\":str,\"font_pairing\":str,\"primary_color\":str,\"density\":str,\"motion\":str,\"rationale\":str}}. For non-UI projects replace design_direction with \"non_ui_direction\":{\"type\":str,\"decisions\":[str]}; exactly one of the two is required. Then present its rendered Markdown to the owner. Do not approve it.",
    blueprint: "Consume the XML blueprint_handoff schema version 2 containing only Work Item identity, project root, and approved artifact lineage metadata. Load scan, rri, and vision with `load_planning_artifact`, verify their IDs/revisions/hashes, and then produce the solution-spec Blueprint JSON with \"decomposition_policy_version\":2, \"schema_version\":2.1, and all required top-level sections: project_info, goals, architecture, tech_stack, file_structure, rri_requirements_matrix, implementation_decisions, adr_candidates, excluded_keys, and verification_seams. Do not include a task_decomposition_preview: ticket boundaries are decided at the Task Graph stage, not here. verification_seams is a nonempty array of owner-approved places where behavior is proven, ordered from highest to lowest seam — [{\"id\":str,\"surface\":str,\"isolates\":str,\"prior_art\":str(optional)}] — with unique non-empty ids; the surface names where the test runs and isolates names the behavior it proves; declare the highest seam that isolates each requirement under test (the aggregate itself is always the highest seam). Reference upstream artifacts by key instead of restating them. The v2.1 solution-spec sections are required in shape: \"implementation_decisions\" is a nonempty array of consequential design decisions — {\"decision\":str,\"rationale\":str,\"alternatives_considered\":[str]} — recorded once with their rejected alternatives. \"adr_candidates\" is an array for durable, consequential decisions where alternatives were genuinely considered (architecture, business rules, schema, cross-artifact contracts); routine implementation choices are not ADR-eligible; each entry carries {\"context\":str,\"choice\":str,\"reason\":str} and is written to docs/adr/ as NNNN-slug.md files only after the Blueprint owner approval — draft state writes nothing. \"excluded_keys\" lists RRI requirement keys that stay excluded: reference only keys the approved RRI out_of_scope already declares (exclusion authority stays RRI-owned) because artifact-save rejects dangling keys fail-closed. Verification surfaces name the exact enforced command that runs the seam (for example `node --test reporting/blueprint-report.test.ts` or `pic work-item artifact-save blueprint` against a temporary SQLite database). The v2.1 marker is additive and marker-gated: an approved artifact without the marker still validates under legacy rules forever; never re-validate approved artifacts under new rules. The Blueprint carries no user stories and no second testing section. Do not use historical revisions. Save it as a temporary draft with `save_blueprint_draft` and retain the returned draft ID. After planner completion, load that temporary state with `load_blueprint_draft`; use its `draft_id` as the `artifact_id` argument for `review_blueprint_checkpoint`. The Contractor must review that draft, revise it through the same temporary-draft loop when any checkpoint item fails, and call `review_blueprint_checkpoint` only when all five checks pass, passing `content` as the JSON object `{\"architecture\":true,\"design\":true,\"requirements\":true,\"verification_seams\":true,\"nothing_missing\":true}` with `actor_role=contractor`. When `review_blueprint_checkpoint` passes, the rendered Blueprint is persisted at `.pi/artifacts/plans/<work-item-id>.md` (the same path is rewritten on every rejection-loop revision) and a plan review is requested automatically through the Plannotator Pi extension (the asynchronous `plannotator:request` plan-review event, never the standalone CLI — its absence is a guarded fallback, and an unavailable extension proceeds without annotations); annotations are optional and zero annotations approve with no dispositions recorded. `approve_blueprint_draft` blocks while that plan review is still pending or was rejected with annotations. Present the checked draft to the owner. Do not call `save_work_item_artifact` or any owner approval action; the owner approves the reviewed draft with `approve_blueprint_draft`, which performs the one canonical save and approval.",
    contracts: "As Contractor, consume the approved Blueprint, RRI, and Vision artifacts via `load_planning_artifact`, and record the approved Blueprint's artifact_id, revision, and content_hash from that load. Draft one Contract JSON object matching exactly this shape: {\"decomposition_policy_version\":2,\"project_name\":str,\"source_blueprint\":{\"artifact_id\":str,\"revision\":int,\"content_hash\":str},\"deliverables\":[{\"item\":str,\"details\":str,\"requirements\":[\"RRI-key\"]}],\"tech_stack\":[{\"layer\":str,\"choice\":str,\"rationale\":str}],\"task_graph_summary\":{\"tip_count\":int>=1,\"estimated_minutes\":int>=1},\"not_included\":[str],\"obligation_schema_version\":2,\"obligations\":[{\"id\":\"OB-unique\",\"requirement_keys\":[\"RRI-key\"],\"behavior\":str,\"acceptance\":\"Given ...\\nWhen ...\\nThen ...\",\"class\":str,\"seam\":str}]}. source_blueprint must bind the exact approved Blueprint lineage returned by load_planning_artifact. Every deliverable needs item, details, and at least one requirement key; obligation ids must be unique and each obligation is atomic behavior whose acceptance contains literal line-start steps Given, When, Then (three lines, newline-separated — mid-sentence when/then words do not count), together covering every non-deferred requirement. Every obligation carries a primary decomposition class from user_behavior, data_invariant, interface_contract, security, migration_rule, operational_rule, integration_gate (hybrids pick the dominant one) and a seam that the approved Blueprint's verification_seams declares — reference them, never restate the Blueprint. Obligation objects carry ONLY id, requirement_keys, behavior, acceptance, class, and seam: provides, consumes, evidence_for, and obligation_keys belong exclusively to Task Graph nodes at the next stage, never to Contract obligations. Call `preview_artifact` (stage=contracts) with the drafted JSON and present its returned rendered Markdown to the owner; never dump or hand-translate the raw JSON payload, and treat any validation error from that action as a required draft revision. Ask the owner to reply exactly `CONFIRM`; do not save it until confirmation. That CONFIRM is explicit owner approval: save the Contract, then approve that current Contract artifact with actor_role=owner so Task Graph planning can start.",
    task_graph: ["task", "bug", "chore"].includes(item.type || "")
      ? "Publish a requirement-covering task graph with exactly one executable node matching the existing Work Item type, no parent, and no dependencies. This node specifies the TIP for the existing Work Item; it must not create or decompose Work Items. This standalone profile has no Blueprint or Contract predecessors, so the graph stays on policy v1: do not set decomposition_policy_version, and give the node a plain verification entry. Save it with `save_work_item_artifact`, validate it with `validate_work_item_graph`, then present it to the owner. Call `approve_work_item_artifact` with `actor_role=owner` only after explicit approval."
      : "Publish the complete requirement-covering task graph with `save_work_item_artifact`, validate it with `validate_work_item_graph`, then present it to the owner. Set \"decomposition_policy_version\":2, use schema version 3, and bind \"source_contract\" to the exact approved Contract lineage returned by `load_planning_artifact` (contracts): {\"artifact_id\":str,\"revision\":int,\"content_hash\":str} — a stale or wrong-lineage binding is rejected. Decompose tracer-bullet style: every node is vertical by default — one requirement-keyed, independently verifiable slice of behavior, small enough for one focused execution session. For each node answer the six node questions: What behavior becomes possible? Which requirement keys does it cover? What is the smallest independently verifiable outcome? What is its direct blocker? What command or test proves it? Can it fit one focused execution session? Horizontal work is an explicit, justified exception only: \"decomposition_mode\" is vertical by default and otherwise one of shared_contract (must provide the shared contract keys and keep at least one downstream consumer node depending on it), wide_refactor (must name \"paired_contract_node\" that contracts or cleans up the expansion, and the declaring node must sit in that node's depends_on closure), or integration_gate (a verification-only node listing the obligations or requirements it verifies, carrying at least one valid seam-bound verification entry regardless of its node type); any non-vertical mode requires a non-empty \"exception_reason\". Every depends_on edge must be a genuine blocker and carry a matching non-empty \"depends_on_rationale\" entry; do not add informational or containment edges. Every executable node needs an effective acceptance contract: a node composing two requirement_keys must author its own \"acceptance\" with literal Given/When/Then steps, while a single-requirement node resolves that requirement's acceptance (reference it; never restate requirements). Every verification entry is seam-bound — {\"seam\":str,\"requirement_keys\":[..] or \"obligation_keys\":[..],\"command\":str,\"expected\":str} — where seam is a verification seam the approved Blueprint declares, the keys name what is proven, command is executable, and expected states the evidence. Every Contract obligation needs exactly one provider node and at least one evidence-producing node. Nodes inherit the approved Blueprint v2.1 scope context: `implementation_decisions` are binding design context, nodes must not plan work for `excluded_keys` content, and any ADR-writing node gates its work on docs/adr/ files being written only after the Blueprint owner approval. Call `approve_work_item_artifact` only after explicit owner approval of granularity, verification, blockers, and exceptions.",
    materialize: "Materialize the approved task graph with `materialize_work_item`; do not create child Work Items manually before this stage.",
    authorize: "Ask the owner for implementation authorization. Call `authorize_work_item_implementation` with `actor_role=owner` only after their explicit approval.",
    implement: ["task", "bug", "chore"].includes(item.type || "")
      ? "Launch the executable Work Item with `work_on_work_item`; its TIP is generated and frozen transactionally before the first worker claim."
      : `Launch only authorized dependency-ready executable descendants. Do not launch this ${item.type || "aggregate"} Work Item as a worker.`,
    contractor_verification: "Verify the integrated completion evidence and publish the verdict with `verify_work_item` using `actor_role=contractor`; a passed executable child closes automatically, while a passed aggregate advances to aggregate verification/owner acceptance.",
    aggregate_verification: "Load the persisted rri_t_scenarios artifact, apply the soft owner trim/defer gate, execute and grade each retained scenario in this session with concrete evidence (contractor only), and publish the graded results with `verify_aggregate_work_item` using `actor_role=contractor`.",
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
// Stage transition priming constraint: every dispatched planning stage prompt
// carries the persisted lineage (profile version/hash, predecessor checkpoint),
// a bounded digest of each approved predecessor artifact, the repo context from
// the accepted Scan, and a per-stage definition of done — so a contractor can
// transit phases without reassembling context from raw artifact IDs.

export interface StagePrimerDigest {
  stage: string;
  artifact_id: string;
  artifact_revision: number | string;
  content_hash: string;
  content: string;
}

export interface StagePrimerProfile {
  version: number | string;
  contentHash: string;
  stages: string[];
}

export interface StagePrimerContext {
  work_item_id: string;
  stage: string;
  profile?: StagePrimerProfile;
  predecessor_checkpoint?: { stage: string; artifact_id: string; artifact_revision: number | string; content_hash: string };
  approved_digests?: StagePrimerDigest[];
}

const PRIMER_DIGEST_CHARS = 1000;

const STAGE_DEFINITIONS_OF_DONE: Record<string, string[]> = {
  scan: [
    "One canonical Scan Report saved as structured XML with source-backed evidence.",
    "Owner acceptance requested; the report is not self-approved.",
  ],
  rri: [
    "Owner-impacting P0/P1 questions resolved or explicitly open.",
    "Requirements are testable with Given/When/Then acceptance criteria.",
    "The interview is published exactly once via save_rri_interview.",
  ],
  vision: [
    "One Vision JSON artifact with every field present and arrays nonempty.",
    "Rendered Markdown presented to the owner for approval.",
  ],
  blueprint: [
    "Blueprint JSON is the solution spec: project_info, goals, architecture, tech_stack, file_structure, rri_requirements_matrix, implementation_decisions, adr_candidates, excluded_keys, and owner-approved verification_seams (decomposition_policy_version 2, schema_version 2.1, no task_decomposition_preview).",
    "ADR candidates carry eligibility criteria and docs/adr/ files are written only after owner approval; draft state writes nothing.",
    "The reviewed draft passed all five checkpoint checks, including verification seams.",
    "The owner approved the draft via approve_blueprint_draft.",
  ],
  contracts: [
    "Every deliverable maps to at least one RRI requirement key.",
    "Every obligation has atomic behavior with literal Given/When/Then acceptance steps, a primary decomposition class, and a seam the approved Blueprint declares.",
    "The Contract binds the exact approved Blueprint artifact id, revision, and content hash.",
    "The owner replied CONFIRM before the Contract was saved and approved.",
  ],
  task_graph: [
    "The graph covers every non-deferred requirement with vertical tracer-bullet slices by default and justified, reasoned exceptions.",
    "Every blocking edge carries a rationale, every executable node an effective acceptance, and every verification entry a Blueprint-declared seam.",
    "validate_work_item_graph passes.",
    "The owner answered the five granularity questions and approved the graph revision.",
  ],
};

function boundedDigest(content: string, budget = PRIMER_DIGEST_CHARS): string {
  const flat = String(content || "").trim().replace(/\s+/g, " ");
  return flat.length > budget ? `${flat.slice(0, budget)}…` : flat;
}

export function buildStagePrimer(context: StagePrimerContext): string {
  const lines: string[] = [
    `# STAGE PRIMER: ${context.stage}`,
    `Work Item: ${context.work_item_id}`,
    `Stage: ${context.stage}`,
  ];
  if (context.profile) {
    lines.push(`Lineage: profile v${context.profile.version} (${context.profile.contentHash || "unhashed"})`);
  }
  if (context.predecessor_checkpoint) {
    const checkpoint = context.predecessor_checkpoint;
    lines.push(`Predecessor: checkpoint ${checkpoint.stage}@${checkpoint.artifact_revision} (${checkpoint.content_hash || "unhashed"})`);
  }
  const digests = context.approved_digests || [];
  if (digests.length) {
    lines.push("", "## APPROVED CONTEXT DIGESTS");
    for (const digest of digests) {
      lines.push(`- ${digest.stage} @${digest.artifact_revision} (${digest.content_hash || "unhashed"}): ${boundedDigest(digest.content)}`);
    }
    lines.push("Load each full artifact with `load_planning_artifact` before authoring. Do not use historical revisions.");
  }
  lines.push("", "## DEFINITION OF DONE");
  for (const item of STAGE_DEFINITIONS_OF_DONE[context.stage] || ["Follow the persisted stage instructions exactly."]) {
    lines.push(`- ${item}`);
  }
  lines.push("- Do not save or approve owner decisions yourself; request explicit owner approval.");
  return lines.join("\n") + "\n";
}

// Attempt ledger constraint: a relaunch (retry, review-fix, autofix) receives a
// deterministic evidence ledger built from persisted attempts — prior reports,
// failed verification commands with trimmed output, and escalation resolutions
// (the generalized GAP-138 injection) — so attempt N continues instead of
// re-planning from scratch. The ledger is size-bounded for prompt budgets.

export interface LedgerPriorReport {
  id?: string;
  status?: string;
  summary?: string;
  created_at?: string;
}

export interface LedgerFailedVerification {
  command: string;
  evidence: string;
}

export interface WorkProgressLedgerInput {
  activePackId: string;
  activePackVersion: number | string;
  attempt: number;
  priorReports: LedgerPriorReport[];
  failedVerifications: LedgerFailedVerification[];
  escalationContext: string;
}

const LEDGER_SUMMARY_CHARS = 300;
const LEDGER_EVIDENCE_CHARS = 300;

function displayTipKey(version: number | string): string {
  return `TIP-${String(version).padStart(3, "0")}`;
}

export function buildWorkProgressLedger(input: WorkProgressLedgerInput): string {
  const lines: string[] = [
    `This is attempt ${input.attempt} of ${displayTipKey(input.activePackVersion)} (pack ${input.activePackId} v${input.activePackVersion}) — continue, do not re-plan from scratch.`,
    "",
    "## Attempt evidence ledger",
  ];
  const reports = input.priorReports.slice(0, 5);
  if (reports.length) {
    for (const report of reports) {
      lines.push(`- ${report.id || "report"} (${report.created_at || "unknown time"}): ${report.status || "unknown"} — ${boundedDigest(report.summary || "no summary", LEDGER_SUMMARY_CHARS)}`);
    }
  } else {
    lines.push("- No prior completion reports for this pack.");
  }
  const verifications = input.failedVerifications.slice(0, 3);
  if (verifications.length) {
    lines.push("", "### Failed verification evidence (from prior attempts)");
    for (const verification of verifications) {
      lines.push(`- \`${verification.command}\`: ${boundedDigest(verification.evidence, LEDGER_EVIDENCE_CHARS)}`);
    }
  }
  if ((input.escalationContext || "").trim()) {
    lines.push("", input.escalationContext.trim());
  }
  return lines.join("\n") + "\n";
}
