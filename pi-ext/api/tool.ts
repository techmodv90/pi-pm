import { execFileSync } from "node:child_process";
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { getMarkdownTheme } from "@mariozechner/pi-coding-agent";
import { Markdown, Text } from "@mariozechner/pi-tui";
import { Type } from "typebox";
import { StringEnum } from "@mariozechner/pi-ai";
import { execPic } from "../core/cli-helpers.ts";
import { buildReviewContext } from "../tasking/settings.ts";
import { buildAggregateVerifyPrompt, buildWorkItemContinuePrompt, buildWorkItemDebugPrompt, latestRriTScenarios } from "../tasking/work-item-prompts.ts";
import { assertTaskManagerActionAllowed } from "../tasking/agent-capabilities.ts";
import { parseCanonicalScanReportXml, renderScanReportMarkdown, prepareCanonicalScanReportArtifact } from "../reporting/scan-report.ts";
import { parseRriReportJson, renderRriReportMarkdown } from "../reporting/rri-report.ts";
import { parseVisionReportJson, renderVisionReportMarkdown } from "../reporting/vision-report.ts";
import { parseBlueprintReportJson, renderBlueprintReportMarkdown } from "../reporting/blueprint-report.ts";
import { parseContractReportJson, renderContractReportMarkdown } from "../reporting/contract-report.ts";
import { parseTaskGraphReportJson, renderTaskGraphReportMarkdown } from "../reporting/task-graph-report.ts";
import { deleteRriDraft, loadRriDraft, saveRriDraft, type RriDraftLineage } from "../core/rri-drafts.ts";
import { deleteBlueprintDraft, loadBlueprintDraft, loadLatestBlueprintDraft, saveBlueprintDraft } from "../core/blueprint-drafts.ts";
import { planAdrFiles, writeAdrFiles, type AdrCandidate } from "../core/blueprint-adr.ts";

import { currentApprovedPlanningArtifact, withInheritedParentWorkflowArtifacts } from "../tasking/task-artifacts.ts";
import { runnerRepairEvidence, type PipelineScheduler } from "../pipeline/pipeline-scheduler.ts";

function aggregateGitEvidence(cwd: string): { branch: string; head: string; baseBranch: string; baseCommit: string } {
  const git = (...args: string[]) => execFileSync("git", args, { cwd, encoding: "utf8" }).trim();
  const branch = git("branch", "--show-current");
  if (!branch) throw new Error("aggregate delivery requires a named Git branch");
  const baseBranch = "develop";
  let baseCommit = "";
  try { baseCommit = git("rev-parse", `refs/remotes/origin/${baseBranch}`); }
  catch { baseCommit = git("rev-parse", baseBranch); }
  return { branch, head: git("rev-parse", "HEAD"), baseBranch, baseCommit };
}

function approvedScanLineage(cwd: string, workItemId: string): RriDraftLineage {
  const data = execPic(["show", workItemId], cwd);
  const checkpoint = (data.checkpoints || []).find((entry: any) => entry.stage === "scan");
  if (!checkpoint?.artifact_id || !checkpoint?.content_hash) throw new Error("RRI interview requires an approved Scan checkpoint");
  return { artifactId: checkpoint.artifact_id, contentHash: checkpoint.content_hash };
}

function rriDraftRoot(cwd: string): string {
  const project = execPic(["project", "current"], cwd);
  if (!project.root_path) throw new Error(project.error || "RRI interview requires a current project root");
  return project.root_path;
}

// RRI-T scenario identity constraint: the id-based identity
// (dimension|stress_axis|requirement_id|id) — shared with the canonical Go
// validator and the authoring merge — never the persona, so two persisted
// scenarios may share persona, dimension, stress axis, and requirement while
// remaining distinct by id, and one persisted scenario can be deferred at most
// once.
function rriTScenarioIdentity(scenario: any): string {
  return `${scenario.dimension}|${scenario.stress_axis}|${scenario.requirement_id}|${scenario.id}`;
}

// RRI-T save-before-execution (OB-5): persist the validated persona authoring
// result as the rri_t_scenarios artifact before any prompt asks the contractor
// to execute or grade; a resumed verification reuses the saved artifact instead
// of re-running persona subagents, and an authoring or save failure surfaces as
// a blocked aggregate verification rather than an unpersisted execution list.
// Subagent sessions only observe lifecycle state and never author scenarios.
async function ensureRriTScenariosArtifact(scheduler: PipelineScheduler, data: any, cwd: string): Promise<void> {
  if (process.env.PI_TASK_AGENT_NAME) return;
  if (latestRriTScenarios(data)) return;
  const workItemId = data?.work_item?.id;
  if (!workItemId) throw new Error("RRI-T scenario authoring requires a Work Item");
  const authored = await scheduler.runRriT(data);
  const saved = execPic(["work-item", "artifact-save", workItemId, "rri_t_scenarios", authored], cwd);
  if (saved.error) throw new Error(`RRI-T scenario artifact save failed: ${saved.error}`);
}

// RRI-T contractor grading (OB-6/OB-7): compile the submission from the
// persisted scenario artifact and the contractor's in-session evidence; each
// retained scenario receives exactly one outcome (PASS/ACCEPTABLE/PAINFUL/FAIL
// with evidence, or not_applicable with a reason that stays out of the
// executable scenarios), and FAIL blocking plus PAINFUL remediation/deferral
// remain authoritative on the Go validation side.
function compileRriTSubmission(data: any, gradedJson: string): string {
  const persisted = latestRriTScenarios(data);
  if (!persisted) throw new Error("persisted rri_t_scenarios artifact is missing; authoring was never saved, so aggregate verification is blocked instead of executing an unpersisted list");
  let graded: { scenarios?: any[]; not_applicable?: any[] };
  try {
    graded = JSON.parse(gradedJson || "");
  } catch {
    throw new Error("rri_t_evidence_json must be a JSON object with scenarios and/or not_applicable arrays");
  }
  if (!graded || typeof graded !== "object" || Array.isArray(graded) || (!Array.isArray(graded.scenarios) && !Array.isArray(graded.not_applicable))) {
    throw new Error("rri_t_evidence_json must be a JSON object with scenarios and/or not_applicable arrays");
  }
  const persistedByIdentity = new Map<string, any>();
  for (const scenario of persisted.content.scenarios || []) {
    const key = rriTScenarioIdentity(scenario);
    if (!persistedByIdentity.has(key)) persistedByIdentity.set(key, scenario);
  }
  const outcomes = new Set<string>();
  const scenarios: any[] = [];
  for (const grade of graded.scenarios || []) {
    const key = rriTScenarioIdentity(grade);
    const match = persistedByIdentity.get(key);
    if (!match) throw new Error(`graded scenario ${key} is not in the persisted rri_t_scenarios artifact`);
    if (outcomes.has(key)) throw new Error(`scenario ${key} received more than one outcome`);
    outcomes.add(key);
    if (String(grade.procedure || "").trim() !== String(match.procedure || "").trim()) throw new Error(`graded scenario ${key} must reuse the persisted procedure verbatim`);
    if (!String(grade.evidence || "").trim()) throw new Error(`graded scenario ${key} requires executed evidence`);
    if (!["PASS", "ACCEPTABLE", "PAINFUL", "FAIL"].includes(String(grade.result || ""))) throw new Error(`graded scenario ${key} result must be PASS, ACCEPTABLE, PAINFUL, or FAIL`);
    scenarios.push({ id: match.id, persona: match.persona, dimension: match.dimension, stress_axis: match.stress_axis, requirement_id: match.requirement_id, procedure: match.procedure, evidence: String(grade.evidence).trim(), result: String(grade.result).trim() });
  }
  const notApplicable: any[] = [];
  for (const grade of graded.not_applicable || []) {
    const key = rriTScenarioIdentity(grade);
    const match = persistedByIdentity.get(key);
    if (!match) throw new Error(`not_applicable scenario ${key} is not in the persisted rri_t_scenarios artifact`);
    if (outcomes.has(key)) throw new Error(`scenario ${key} received more than one outcome`);
    outcomes.add(key);
    if (!String(grade.reason || "").trim()) throw new Error(`not_applicable scenario ${key} requires a concrete reason`);
    notApplicable.push({ id: match.id, persona: match.persona, dimension: match.dimension, stress_axis: match.stress_axis, requirement_id: match.requirement_id, reason: String(grade.reason).trim() });
  }
  return JSON.stringify({
    methodology: "rri-t",
    personas: persisted.content.personas || [],
    scenarios,
    not_applicable: [...(Array.isArray(persisted.content.not_applicable) ? persisted.content.not_applicable : []), ...notApplicable],
    open_blockers: persisted.content.open_blockers || [],
  });
}

export function registerTaskManagerTool(pi: ExtensionAPI, pipelineScheduler: PipelineScheduler) {
    pi.registerTool({
      name: "task_manager",
      label: "Task Manager",
      description: "Manage canonical Work Items through the pic CLI.",
      promptSnippet: "Use Work Item actions for lifecycle mutations. Archived Task Items are read-only history.",
      parameters: Type.Object({
        action: StringEnum([
          "create_work_item", "update_work_item", "update_work_item_status", "list_work_items", "show_work_item", "ready_work_items", "claim_work_item", "add_work_item_labels", "remove_work_item_labels", "list_work_item_labels", "list_all_work_item_labels", "checkpoint_rri_interview", "load_rri_interview", "save_rri_interview",
          "save_blueprint_draft", "load_blueprint_draft", "review_blueprint_checkpoint", "approve_blueprint_draft", "load_planning_artifact", "preview_artifact", "save_work_item_artifact", "approve_work_item_artifact", "approve_work_item_deviations", "reject_work_item_scan", "reset_work_item_planning", "reset_work_item_execution", "resolve_escalation", "amend_work_item_planning", "work_item_workflow_status", "validate_work_item_graph", "materialize_work_item", "authorize_work_item_implementation", "verify_work_item", "accept_work_item", "verify_aggregate_work_item", "accept_aggregate_work_item", "merge_aggregate_work_item", "close_aggregate_work_item",
          "search", "work_on_work_item", "dry_run_work_item", "trigger_work_item_review", "debug_work_item",
          "relate_work_items", "reset_pipeline_circuit",
        ] as const),
        id: Type.Optional(Type.String({ description: "Work Item ID" })),
        related_work_item_id: Type.Optional(Type.String({ description: "Work Item related to the subject" })),
        relation_type: Type.Optional(StringEnum(["blocks", "gates", "related"] as const)),
        title: Type.Optional(Type.String({ description: "Work Item title" })),
        description: Type.Optional(Type.String({ description: "Description text" })),
        content: Type.Optional(Type.String({ description: "Immutable workflow artifact content" })),
        status: Type.Optional(StringEnum(["open", "in_progress", "done", "cancelled"] as const)),
        priority: Type.Optional(StringEnum(["low", "medium", "high"] as const)),
        notes: Type.Optional(Type.String({ description: "Concise note or summary to append" })),
        query: Type.Optional(Type.String({ description: "Search query" })),
        summary: Type.Optional(Type.String({ description: "Workflow artifact summary" })),
        rri_t_evidence_json: Type.Optional(Type.String({ description: "Graded RRI-T scenario JSON (persisted scenarios plus per-scenario evidence/result or not_applicable with reason) submitted by the contractor with verify_aggregate_work_item" })),
        verification_status: Type.Optional(StringEnum(["passed", "failed", "partial", "blocked"] as const)),
        actor_role: Type.Optional(Type.String({ description: "Explicit actor role; owner-only actions require owner confirmation from the user" })),
        event_type: Type.Optional(Type.String({ description: "Debug trigger type" })),
        work_item_type: Type.Optional(StringEnum(["epic", "feature", "task", "bug", "chore", "gate"] as const)),
        parent_id: Type.Optional(Type.String({ description: "Parent aggregate Work Item ID" })),
        labels: Type.Optional(Type.Array(Type.String(), { description: "Work Item labels" })),
        deviation_ids: Type.Optional(Type.Array(Type.String(), { description: "Requirement IDs approved for deferment" })),
        reason: Type.Optional(Type.String({ description: "Owner-recorded reason for a bounded planning amendment" })),
        substitutions: Type.Optional(Type.Array(Type.Object({ old: Type.String(), new: Type.String() }), { description: "Exact old→new string pairs for amend_work_item_planning; every occurrence across approved planning artifacts, requirements, and owner decisions is replaced" })),
        stage: Type.Optional(StringEnum(["scan", "rri", "vision", "blueprint", "contracts", "task_graph"] as const)),
        artifact_id: Type.Optional(Type.String({ description: "Immutable Work Item artifact ID" })),
        completion_report_id: Type.Optional(Type.String({ description: "Current integrated Completion Report ID" })),
        verification_report_id: Type.Optional(Type.String({ description: "Current aggregate Verification Report ID" })),
        decision: Type.Optional(StringEnum(["accepted", "rejected"] as const)),
        change_type: Type.Optional(StringEnum(["contract", "environment", "runner", "artifact"] as const)),
        evidence_json: Type.Optional(Type.String({ description: "JSON evidence supporting a pipeline circuit reset" })),
        claimant: Type.Optional(Type.String({ description: "Worker or scheduler claiming the Work Item" })),
        deferrable: Type.Optional(Type.Boolean({ description: "Whether the Work Item is deferred" })),
        escalation_id: Type.Optional(Type.String({ description: "Open escalation ID (wies-…) to resolve with a recorded decision" })),
      }),
  
      async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
        let args: string[] = [];
        let scanPresentation = "";
        let scanContent = "";
        let rriPresentation = "";
        let visionPresentation = "";
        let blueprintPresentation = "";
        let contractPresentation = "";
        let taskGraphPresentation = "";

        try { assertTaskManagerActionAllowed(process.env.PI_TASK_AGENT_NAME, params.action as string, params.stage); }
        catch (error) {
          const message = error instanceof Error ? error.message : String(error);
          return { content: [{ type: "text", text: `Error: ${message}` }], details: {}, isError: true };
        }

        switch (params.action as string) {
          case "save_blueprint_draft": {
            if (!params.id || !params.content || params.stage !== "blueprint") return { content: [{ type: "text", text: "Error: id, stage=blueprint, and content required" }], details: {}, isError: true };
            const report = parseBlueprintReportJson(params.content);
            const draft = saveBlueprintDraft(ctx.cwd, params.id, params.content);
            blueprintPresentation = renderBlueprintReportMarkdown(report);
            return { content: [{ type: "text", text: `${blueprintPresentation}\n\nTemporary Blueprint draft: ${draft.draftId}\nContractor review is required before owner approval.` }], details: { draft_id: draft.draftId, temporary: true, path: `.pi/runtime/blueprint/${params.id}.json` } };
          }
          case "load_blueprint_draft": {
            if (!params.id) return { content: [{ type: "text", text: "Error: id required" }], details: {}, isError: true };
            try {
              const draft = params.artifact_id ? loadBlueprintDraft(ctx.cwd, params.id, params.artifact_id) : loadLatestBlueprintDraft(ctx.cwd, params.id);
              return { content: [{ type: "text", text: JSON.stringify(draft, null, 2) }], details: { draft_id: draft.draftId, reviewed: draft.reviewed, temporary: true } };
            } catch (error) {
              const message = error instanceof Error ? error.message : String(error);
              return { content: [{ type: "text", text: `Error: ${message}` }], details: { error: message }, isError: true };
            }
          }
          case "checkpoint_rri_interview": {
            if (!params.id || !params.content) return { content: [{ type: "text", text: "Error: id and JSON content required" }], details: {}, isError: true };
            let state: unknown;
            try { state = JSON.parse(params.content); }
            catch { return { content: [{ type: "text", text: "Error: RRI interview content must be valid JSON" }], details: {}, isError: true }; }
            if (!state || typeof state !== "object" || Array.isArray(state)) return { content: [{ type: "text", text: "Error: RRI interview content must be one JSON object" }], details: {}, isError: true };
            try {
              const path = saveRriDraft(rriDraftRoot(ctx.cwd), params.id, approvedScanLineage(ctx.cwd, params.id), state);
              const result = { work_item_id: params.id, checkpointed: true, path };
              return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
            } catch (error) {
              const message = error instanceof Error ? error.message : String(error);
              return { content: [{ type: "text", text: `Error: ${message}` }], details: { error: message }, isError: true };
            }
          }
          case "load_rri_interview": {
            if (!params.id) return { content: [{ type: "text", text: "Error: id required" }], details: {}, isError: true };
            try {
              const result = loadRriDraft(rriDraftRoot(ctx.cwd), params.id, approvedScanLineage(ctx.cwd, params.id));
              return { content: [{ type: "text", text: JSON.stringify(result.state, null, 2) }], details: result };
            } catch (error) {
              const message = error instanceof Error ? error.message : String(error);
              return { content: [{ type: "text", text: `Error: ${message}` }], details: { error: message }, isError: true };
            }
          }
          case "save_rri_interview": {
            if (!params.id || !params.content || params.actor_role !== "contractor") return { content: [{ type: "text", text: "Error: id, content (final RRI JSON), and actor_role=contractor are required" }], details: {}, isError: true };
            try {
              const payload = JSON.parse(params.content) as { report?: unknown };
              if (!payload.report) throw new Error("RRI finalization requires a structured report object");
              rriPresentation = renderRriReportMarkdown(parseRriReportJson(JSON.stringify(payload.report)));
            } catch (error) {
              const message = error instanceof Error ? error.message : String(error);
              return { content: [{ type: "text", text: `Error: ${message}` }], details: {}, isError: true };
            }
            const result = execPic(["work-item", "rri-finalize", params.id, params.content, "--actor-role", params.actor_role], ctx.cwd);
            if (result.error) return { content: [{ type: "text", text: `Error: ${result.error}` }], details: result, isError: true };
            pipelineScheduler.finalizeHandoffs(params.id, "rri");
            return { content: [{ type: "text", text: rriPresentation }], details: { ...result, rriPresentation } };
          }
          case "create_work_item": {
            if (!params.work_item_type || !params.title) return { content: [{ type: "text", text: "Error: work_item_type and title required" }], details: {}, isError: true };
            args = ["work-item", "create", params.work_item_type, params.title];
            if (params.parent_id) args.push("--parent", params.parent_id);
            if (params.description) args.push("--description", params.description);
            if (params.priority) args.push("--priority", params.priority);
            if (params.deferrable) args.push("--deferred", "1");
            if (params.labels?.length) args.push("--labels", params.labels.join(","));
            break;
          }
          case "update_work_item": {
            if (!params.id) return { content: [{ type: "text", text: "Error: id required" }], details: {}, isError: true };
            args = ["work-item", "update", params.id];
            if (params.title) args.push("--title", params.title);
            if (params.description) args.push("--description", params.description);
            if (params.priority) args.push("--priority", params.priority);
            if (params.parent_id !== undefined) args.push("--parent", params.parent_id);
            break;
          }
          case "update_work_item_status": {
            if (!params.id || !params.status) return { content: [{ type: "text", text: "Error: id and status required" }], details: {}, isError: true };
            args = ["work-item", "status", params.id, params.status];
            break;
          }
          case "list_work_items": {
            args = ["work-item", "list"];
            if (params.labels?.length) args.push("--label", params.labels.join(","));
            break;
          }
          case "show_work_item": {
            if (!params.id) return { content: [{ type: "text", text: "Error: id required" }], details: {}, isError: true };
            args = ["work-item", "show", params.id];
            break;
          }
          case "ready_work_items": args = ["work-item", "ready"]; break;
          case "add_work_item_labels":
          case "remove_work_item_labels": {
            if (!params.id || !params.labels?.length) return { content: [{ type: "text", text: "Error: id and labels required" }], details: {}, isError: true };
            args = ["work-item", "label", params.action === "add_work_item_labels" ? "add" : "remove", params.id, params.labels.join(",")];
            break;
          }
          case "list_work_item_labels": {
            if (!params.id) return { content: [{ type: "text", text: "Error: id required" }], details: {}, isError: true };
            args = ["work-item", "label", "list", params.id];
            break;
          }
          case "list_all_work_item_labels": args = ["work-item", "label", "list-all"]; break;
          case "claim_work_item": {
            if (!params.id || !params.claimant) return { content: [{ type: "text", text: "Error: id and claimant required" }], details: {}, isError: true };
            args = ["work-item", "claim", params.id, params.claimant];
            break;
          }
          case "save_work_item_artifact": {
            if (!params.id || !params.stage || !params.content) return { content: [{ type: "text", text: "Error: id, stage, and content required" }], details: {}, isError: true };
            if (params.stage === "scan") {
              try {
                const prepared = prepareCanonicalScanReportArtifact(params.content);
                scanPresentation = prepared.markdown;
                scanContent = prepared.content;
              }
              catch (error) {
                const message = error instanceof Error ? error.message : String(error);
                return { content: [{ type: "text", text: `Error: ${message}` }], details: {}, isError: true };
              }
            }
            if (params.stage === "vision") {
              try { visionPresentation = renderVisionReportMarkdown(parseVisionReportJson(params.content)); }
              catch (error) { const message = error instanceof Error ? error.message : String(error); return { content: [{ type: "text", text: `Error: ${message}` }], details: {}, isError: true }; }
            }
            if (params.stage === "blueprint") {
              try { blueprintPresentation = renderBlueprintReportMarkdown(parseBlueprintReportJson(params.content)); }
              catch (error) { const message = error instanceof Error ? error.message : String(error); return { content: [{ type: "text", text: `Error: ${message}` }], details: {}, isError: true }; }
            }
            if (params.stage === "contracts") {
              try { contractPresentation = renderContractReportMarkdown(parseContractReportJson(params.content)); }
              catch (error) { const message = error instanceof Error ? error.message : String(error); return { content: [{ type: "text", text: `Error: ${message}` }], details: {}, isError: true }; }
            }
            if (params.stage === "task_graph") {
              try { taskGraphPresentation = renderTaskGraphReportMarkdown(parseTaskGraphReportJson(params.content)); }
              catch (error) { const message = error instanceof Error ? error.message : String(error); return { content: [{ type: "text", text: `Error: ${message}` }], details: {}, isError: true }; }
            }
            args = ["work-item", "artifact-save", params.id, params.stage, scanContent || params.content];
            break;
          }
          case "preview_artifact": {
            if (!params.stage || !params.content) return { content: [{ type: "text", text: "Error: stage and content required" }], details: {}, isError: true };
            try {
              let markdown: string;
              switch (params.stage) {
                case "scan": markdown = renderScanReportMarkdown(parseCanonicalScanReportXml(params.content)); break;
                case "rri": {
                  // RRI finalization content nests the report under .report; accept either shape
                  const payload = JSON.parse(params.content) as { report?: unknown };
                  markdown = renderRriReportMarkdown(parseRriReportJson(JSON.stringify(payload.report ?? payload)));
                  break;
                }
                case "vision": markdown = renderVisionReportMarkdown(parseVisionReportJson(params.content)); break;
                case "blueprint": markdown = renderBlueprintReportMarkdown(parseBlueprintReportJson(params.content)); break;
                case "contracts": markdown = renderContractReportMarkdown(parseContractReportJson(params.content)); break;
                case "task_graph": markdown = renderTaskGraphReportMarkdown(parseTaskGraphReportJson(params.content)); break;
                default: throw new Error(`stage ${params.stage} has no rendered artifact preview`);
              }
              return { content: [{ type: "text", text: markdown }], details: { action: "preview_artifact", stage: params.stage, preview: true, previewPresentation: markdown } };
            } catch (error) {
              const message = error instanceof Error ? error.message : String(error);
              return { content: [{ type: "text", text: `Error: ${message}` }], details: {}, isError: true };
            }
          }
          case "approve_work_item_artifact": {
            if (!params.id || !params.stage || !params.artifact_id) return { content: [{ type: "text", text: "Error: id, stage, and artifact_id required" }], details: {}, isError: true };
            if (params.actor_role !== "owner") return { content: [{ type: "text", text: "Error: actor_role must be owner after explicit owner approval" }], details: {}, isError: true };
            args = ["work-item", "artifact-approve", params.id, params.stage, params.artifact_id, params.stage === "scan" ? "accepted" : "approved"];
            break;
          }
          case "review_blueprint_checkpoint": {
            if (!params.id || !params.artifact_id || !params.content || params.actor_role !== "contractor") return { content: [{ type: "text", text: "Error: id, artifact_id (the draft ID), content, and actor_role=contractor are required" }], details: {}, isError: true };
            const draft = loadBlueprintDraft(ctx.cwd, params.id, params.artifact_id);
            let checkpoint: Record<string, unknown>;
            try { checkpoint = JSON.parse(params.content) as Record<string, unknown>; }
            catch { return { content: [{ type: "text", text: "Error: content must be a JSON object with the five checkpoint booleans: architecture, design, requirements, task_decomposition (verification_seams for decomposition policy v2 drafts), nothing_missing" }], details: {}, isError: true }; }
            // The fifth check is policy-dependent: decomposition policy v2 drafts
            // are reviewed for verification seams, v1 drafts for task decomposition.
            const policyVersion = (JSON.parse(draft.content) as { decomposition_policy_version?: number }).decomposition_policy_version ?? 1;
            const checks = policyVersion === 2
              ? ["architecture", "design", "requirements", "verification_seams", "nothing_missing"]
              : ["architecture", "design", "requirements", "task_decomposition", "nothing_missing"];
            if (!checks.every((key) => checkpoint[key] === true)) return { content: [{ type: "text", text: `Error: all five Blueprint checks must pass; set each to true: ${checks.join(", ")}` }], details: {}, isError: true };
            const reviewed = saveBlueprintDraft(ctx.cwd, params.id, draft.content, checkpoint);
            blueprintPresentation = renderBlueprintReportMarkdown(parseBlueprintReportJson(draft.content)).replaceAll("- [ ]", "- [x]");
            return { content: [{ type: "text", text: `${blueprintPresentation}\n\nContractor checkpoint passed. Draft ${reviewed.draftId} is ready for owner approval.` }], details: { draft_id: reviewed.draftId, reviewed: true } };
          }
          case "approve_blueprint_draft": {
            if (!params.id || !params.artifact_id || params.actor_role !== "owner") return { content: [{ type: "text", text: "Error: id, draft_id, and actor_role=owner are required" }], details: {}, isError: true };
            const draft = loadBlueprintDraft(ctx.cwd, params.id, params.artifact_id);
            if (!draft.reviewed) return { content: [{ type: "text", text: "Error: Contractor review is required before owner approval" }], details: {}, isError: true };
            // OB-F2-3: full adr_candidates validation (shape, string fields,
            // safe slugs) and target-conflict preflight run BEFORE the Go
            // artifact-save/approve, so malformed or conflicting input can
            // never leave the canonical Blueprint approved while the tool
            // returns an error. writeAdrFiles runs only after both Go
            // operations succeed.
            let adrPayload: { adr_candidates?: unknown };
            try { adrPayload = JSON.parse(draft.content); }
            catch {
              return { content: [{ type: "text", text: "Error: Blueprint draft content must be valid JSON" }], details: {}, isError: true };
            }
            const parsedAdrCandidates = adrPayload.adr_candidates;
            if (parsedAdrCandidates !== undefined && !Array.isArray(parsedAdrCandidates)) return { content: [{ type: "text", text: "Error: adr_candidates must be an array of {context, choice, reason} objects" }], details: {}, isError: true };
            const adrCandidates = (parsedAdrCandidates ?? []) as AdrCandidate[];
            try { planAdrFiles(ctx.cwd, adrCandidates); }
            catch (error) {
              const message = error instanceof Error ? error.message : String(error);
              return { content: [{ type: "text", text: `Error: ${message}` }], details: {}, isError: true };
            }
            const saved = execPic(["work-item", "artifact-save", params.id, "blueprint", draft.content], ctx.cwd);
            if (saved.error) return { content: [{ type: "text", text: `Error: ${saved.error}` }], details: saved, isError: true };
            const approved = execPic(["work-item", "artifact-approve", params.id, "blueprint", saved.id, "approved"], ctx.cwd);
            if (approved.error) return { content: [{ type: "text", text: `Error: ${approved.error}` }], details: { saved, approved }, isError: true };
            let adrFiles: string[] = [];
            // Validation already passed before the Go calls; this write only
            // fails on a mid-approval filesystem change, which must surface as
            // a concrete approval-side error, not a thrown exception.
            if (adrCandidates.length > 0) {
              try { adrFiles = writeAdrFiles(ctx.cwd, adrCandidates); }
              catch (error) {
                const message = error instanceof Error ? error.message : String(error);
                return { content: [{ type: "text", text: `Error: ${message}` }], details: { saved, approved }, isError: true };
              }
            }
            deleteBlueprintDraft(ctx.cwd, params.id);
            return { content: [{ type: "text", text: JSON.stringify({ saved, approved, adr_files: adrFiles }, null, 2) }], details: { saved, approved, adr_files: adrFiles } };
          }
          case "load_planning_artifact": {
            if (!params.id || !params.stage || !["scan", "rri", "vision", "blueprint", "contracts", "task_graph"].includes(params.stage)) return { content: [{ type: "text", text: "Error: id and a valid planning stage are required" }], details: {}, isError: true };
            const data = execPic(["show", params.id], ctx.cwd);
            const artifact = currentApprovedPlanningArtifact(data, params.stage);
            if (!artifact) return { content: [{ type: "text", text: `Error: no approved current ${params.stage} artifact exists` }], details: {}, isError: true };
            const result = { stage: params.stage, artifact_id: artifact.id, revision: artifact.revision, content_hash: artifact.content_hash, content: artifact.content };
            return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: { stage: params.stage, artifact_id: artifact.id, revision: artifact.revision, content_hash: artifact.content_hash } };
          }
          case "approve_work_item_deviations": {
            if (!params.id || params.actor_role !== "owner" || !params.deviation_ids?.length) return { content: [{ type: "text", text: "Error: id, deviation_ids, and actor_role must be owner after explicit owner approval" }], details: {}, isError: true };
            args = ["work-item", "approve-deviations", params.id, params.actor_role, ...params.deviation_ids];
            break;
          }
          case "reset_work_item_planning": {
            if (!params.id || params.actor_role !== "owner") return { content: [{ type: "text", text: "Error: id and actor_role must be owner after explicit owner approval" }], details: {}, isError: true };
            args = ["work-item", "planning-reset", params.id, params.actor_role];
            break;
          }
          case "reset_work_item_execution": {
            if (!params.id || params.actor_role !== "owner") return { content: [{ type: "text", text: "Error: id and actor_role must be owner after explicit owner approval" }], details: {}, isError: true };
            args = ["work-item", "execution-reset", params.id, params.actor_role];
            break;
          }
          case "resolve_escalation": {
            if (!params.id || !params.escalation_id || !params.content || params.actor_role !== "contractor") {
              return { content: [{ type: "text", text: "Error: id, escalation_id, content (a JSON string such as {\"decision\":\"use sqlite\",\"rationale\":\"...\"}), and actor_role=contractor are required; owner answers flow through the contractor" }], details: {}, isError: true };
            }
            args = ["workflow", "escalation-resolve", params.id, params.escalation_id, params.content, "--actor-role", params.actor_role];
            break;
          }
          case "amend_work_item_planning": {
            if (!params.id || params.actor_role !== "owner") return { content: [{ type: "text", text: "Error: id and actor_role must be owner after explicit owner confirmation of the exact substitutions" }], details: {}, isError: true };
            const substitutions = params.substitutions;
            if (!params.reason || !Array.isArray(substitutions) || substitutions.length === 0 || substitutions.some((s) => !s?.old || s.old === s?.new)) {
              return { content: [{ type: "text", text: "Error: reason and a nonempty substitutions array of {old,new} exact-string pairs are required" }], details: {}, isError: true };
            }
            args = ["work-item", "planning-amend", params.id, params.actor_role, JSON.stringify({ reason: params.reason, substitutions })];
            break;
          }
          case "reject_work_item_scan": {
            if (!params.id || !params.notes || params.actor_role !== "contractor") return { content: [{ type: "text", text: "Error: id, notes, and actor_role=contractor required" }], details: {}, isError: true };
            args = ["work-item", "scan-reject", params.id, params.actor_role, params.notes];
            break;
          }
          case "work_item_workflow_status": {
            if (!params.id) return { content: [{ type: "text", text: "Error: id required" }], details: {}, isError: true };
            args = ["work-item", "workflow-status", params.id];
            break;
          }
          case "validate_work_item_graph":
          case "materialize_work_item":
          case "close_aggregate_work_item": {
            if (!params.id) return { content: [{ type: "text", text: "Error: id required" }], details: {}, isError: true };
            const subcommand = params.action === "validate_work_item_graph" ? "graph-validate" : params.action === "materialize_work_item" ? "materialize" : "aggregate-close";
            args = ["work-item", subcommand, params.id];
            break;
          }
          case "authorize_work_item_implementation": {
            if (!params.id || params.actor_role !== "owner") return { content: [{ type: "text", text: "Error: id and actor_role must be owner after explicit owner approval" }], details: {}, isError: true };
            args = ["work-item", "authorize", params.id, params.actor_role];
            const git = aggregateGitEvidence(ctx.cwd);
            args.push("--branch-name", git.branch, "--base-branch", git.baseBranch, "--base-commit", git.baseCommit);
            break;
          }
          case "verify_aggregate_work_item": {
            if (!params.id || !params.verification_status || params.actor_role !== "contractor") return { content: [{ type: "text", text: "Error: id, verification_status, and actor_role=contractor required" }], details: {}, isError: true };
            // RRI-T scenario ownership (OB-5): compile the grading submission from
            // the aggregate's own persisted scenarios only; parent-inherited rows
            // are never a valid scenario source for a feature aggregate, so the
            // aggregate is loaded directly instead of merged with parent artifacts.
            const aggregateData = execPic(["show", params.id], ctx.cwd);
            if (!aggregateData.work_item) return { content: [{ type: "text", text: `Error: ${aggregateData.error || "Work Item not found"}` }], details: {}, isError: true };
            let rriTJson = "";
            try {
              // RRI-T grading submission (OB-6): compile the graded scenario
              // evidence from the persisted rri_t_scenarios artifact only; the
              // contractor grades in the main session, so the submission never
              // re-runs persona subagents and fails closed without a saved list.
              rriTJson = compileRriTSubmission(aggregateData, params.rri_t_evidence_json || "");
            } catch (error) {
              const message = error instanceof Error ? error.message : String(error);
              return { content: [{ type: "text", text: `RRI-T verification blocked: ${message}` }], details: { error: message }, isError: true };
            }
            args = ["work-item", "aggregate-verify", params.id, params.verification_status, params.summary || params.notes || "", "--actor-role", params.actor_role, "--rri-t-json", rriTJson];
            const git = aggregateGitEvidence(ctx.cwd);
            args.push("--branch-name", git.branch, "--head-commit", git.head, "--base-commit", git.baseCommit);
            break;
          }
          case "accept_aggregate_work_item": {
            if (!params.id || !params.verification_report_id || !params.decision || params.actor_role !== "owner") return { content: [{ type: "text", text: "Error: id, verification_report_id, decision, and actor_role=owner required" }], details: {}, isError: true };
            const git = aggregateGitEvidence(ctx.cwd);
            args = ["work-item", "aggregate-accept", params.id, params.verification_report_id, params.decision, params.notes || "", "--actor-role", params.actor_role, "--head-commit", git.head, "--base-commit", git.baseCommit];
            break;
          }
          case "merge_aggregate_work_item": {
            if (!params.id) return { content: [{ type: "text", text: "Error: id required" }], details: {}, isError: true };
            try {
              const result = await pipelineScheduler.mergeAggregate(params.id, ctx);
              return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
            } catch (error) {
              const message = error instanceof Error ? error.message : String(error);
              return { content: [{ type: "text", text: `Aggregate merge blocked: ${message}` }], details: { error: message }, isError: true };
            }
          }
          case "verify_work_item": {
            if (!params.id || !params.completion_report_id || !params.verification_status || params.actor_role !== "contractor") return { content: [{ type: "text", text: "Error: id, completion_report_id, verification_status, and actor_role=contractor required" }], details: {}, isError: true };
            args = ["work-item", "verification-save", params.id, params.completion_report_id, params.verification_status, params.summary || params.notes || "", "--actor-role", params.actor_role];
            break;
          }
          case "accept_work_item": {
            if (!params.id || !params.completion_report_id || !params.decision || params.actor_role !== "owner") return { content: [{ type: "text", text: "Error: id, completion_report_id, decision, and actor_role=owner required" }], details: {}, isError: true };
            args = ["work-item", "accept", params.id, params.completion_report_id, params.decision, params.notes || "", "--actor-role", params.actor_role];
            break;
          }

          case "search":
            if (!params.query) return { content: [{ type: "text", text: "Error: query required" }], details: {}, isError: true };
            args = ["search", params.query];
            break;
          case "debug_work_item": {
            if (!params.id) return { content: [{ type: "text", text: "Error: id required for debug_work_item" }], details: {}, isError: true };
            const data = execPic(["show", params.id], ctx.cwd);
            if (!data.work_item) return { content: [{ type: "text", text: `Error: ${data.error || "Work Item not found"}` }], details: {}, isError: true };
            const inheritedData = withInheritedParentWorkflowArtifacts(data, ctx.cwd);
            const text = buildWorkItemDebugPrompt(inheritedData.work_item, {
              scanReports: (inheritedData.artifacts || []).filter((artifact: any) => artifact.stage === "scan"),
              trigger: params.event_type || "manual",
              evidence: params.notes || params.description || "",
            });
            return { content: [{ type: "text", text }], details: { action: "debug_work_item", workItem: inheritedData.work_item, trigger: params.event_type || "manual" } };
          }
          case "reset_pipeline_circuit": {
            if (!params.id || !params.notes || !params.change_type || !params.evidence_json || params.actor_role !== "owner") return { content: [{ type: "text", text: "Error: id, notes, change_type, evidence_json, and actor_role=owner required for reset_pipeline_circuit" }], details: {}, isError: true };
            let evidenceJson = params.evidence_json;
            try {
              if (params.change_type === "runner") evidenceJson = runnerRepairEvidence(evidenceJson);
            } catch (error) {
              return { content: [{ type: "text", text: `Error: ${error instanceof Error ? error.message : String(error)}` }], details: {}, isError: true };
            }
            args = ["workflow", "pipeline-circuit-reset", params.id, "--reason", params.notes, "--change-type", params.change_type, "--evidence-json", evidenceJson, "--actor-role", params.actor_role];
            break;
          }
          case "work_on_work_item": {
            if (!params.id) {
              try {
                const result = await pipelineScheduler.startReadyBatch(ctx);
                return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: { action: "work_on_work_item", pipeline: result } };
              } catch (error) {
                const message = error instanceof Error ? error.message : String(error);
                return { content: [{ type: "text", text: `Work batch blocked: ${message}` }], details: { action: "work_on_work_item", error: message }, isError: true };
              }
            }
            const data = execPic(["show", params.id], ctx.cwd);
            if (!data.work_item) return { content: [{ type: "text", text: `Error: ${data.error || "Work Item not found"}` }], details: {}, isError: true };
            const status = execPic(["work-item", "workflow-status", params.id], ctx.cwd);
            if (!status.error && (status.next_stage === "rri" || status.next_stage === "vision" || status.next_stage === "contracts")) {
              const prompt = buildWorkItemContinuePrompt(status, data.work_item);
              return { content: [{ type: "text", text: prompt }], details: { action: "work_on_work_item", workItem: data.work_item, next_stage: status.next_stage, contractor: true } };
            }
            try {
              const result = await pipelineScheduler.start(params.id, ctx);
              return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: { action: "work_on_work_item", workItem: data.work_item, pipeline: result } };
            } catch (error) {
              const message = error instanceof Error ? error.message : String(error);
              return { content: [{ type: "text", text: `Work pipeline blocked: ${message}` }], details: { action: "work_on_work_item", workItem: data.work_item, error: message }, isError: true };
            }
          }
          case "dry_run_work_item": {
            if (!params.id) return { content: [{ type: "text", text: "Error: id required" }], details: {}, isError: true };
            const result = pipelineScheduler.dryRun(params.id, ctx);
            return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: { action: "dry_run_work_item", ...result } };
          }
          case "trigger_work_item_review": {
            if (!params.id) return { content: [{ type: "text", text: "Error: id required" }], details: {}, isError: true };
            const data = execPic(["show", params.id], ctx.cwd);
            if (!data.work_item || !["task", "bug", "chore"].includes(data.work_item.type)) {
              return { content: [{ type: "text", text: `Error: ${data.error || "Executable Work Item not found"}` }], details: {}, isError: true };
            }
            const review = buildReviewContext(params.id, ctx.cwd);
            if (review.error || !review.text) {
              return { content: [{ type: "text", text: `Error: ${review.error || "Failed to build review context"}` }], details: {}, isError: true };
            }
  
            const text = [
              `# Review Context for Work Item ${params.id}`,
              "",
              "This is the complete pack-bound review context. Review it directly; do not launch another reviewer.",
              "",
              review.text,
            ].join("\n");
            return {
              content: [{ type: "text", text }],
              details: { action: "trigger_work_item_review", readyForSubagent: true, workItem: data.work_item, gitDiff: review.gitDiff, reviewContext: review.text },
            };
          }
        }
  
        const result = execPic(args, ctx.cwd);
        if (!result.error && params.id && (
          (params.action === "approve_work_item_artifact" && params.stage === "rri")
          || params.action === "reset_work_item_planning"
          || (params.action === "update_work_item_status" && params.status === "cancelled")
        )) {
          deleteRriDraft(rriDraftRoot(ctx.cwd), params.id);
          deleteBlueprintDraft(ctx.cwd, params.id);
        }
        if (!result.error && params.action === "create_work_item" && ["epic", "feature"].includes(params.work_item_type || "")) {
          const workflow = execPic(["work-item", "workflow-status", result.id], ctx.cwd);
          result.next_stage = workflow.next_stage;
          if (workflow.next_stage === "scan") {
            result.orchestration = await pipelineScheduler.start(result.id, ctx);
          }
        }
        if (!result.error && params.action === "approve_work_item_artifact" && ["vision", "contracts"].includes(params.stage || "") && params.id) {
          const workflow = execPic(["work-item", "workflow-status", params.id], ctx.cwd);
          if ((params.stage === "vision" && workflow.next_stage === "blueprint") || (params.stage === "contracts" && workflow.next_stage === "task_graph")) result.orchestration = await pipelineScheduler.start(params.id, ctx);
        }
        if (!result.error && params.action === "verify_work_item" && params.verification_status === "passed") {
          const child = execPic(["show", params.id!], ctx.cwd);
          const parentID = child.work_item?.parent_id;
          if (parentID) {
            const parent = execPic(["show", parentID], ctx.cwd);
            const parentStatus = execPic(["work-item", "workflow-status", parentID], ctx.cwd);
            if (parentStatus.next_stage === "aggregate_verification") {
              try {
                // RRI-T save-before-execution (OB-5): persist the authored
                // scenario artifact before the contractor is asked to execute or
                // grade anything; a saved artifact is reused on resumption.
                await ensureRriTScenariosArtifact(pipelineScheduler, parent, ctx.cwd);
              } catch (error) {
                const message = error instanceof Error ? error.message : String(error);
                return { content: [{ type: "text", text: `Aggregate verification blocked: ${message}` }], details: { verification: result, next_stage: "aggregate_verification", error: message }, isError: true };
              }
              const parentWithScenarios = execPic(["show", parentID], ctx.cwd);
              return { content: [{ type: "text", text: buildAggregateVerifyPrompt(parentWithScenarios) }], details: { verification: result, next_stage: "aggregate_verification", work_item: parentWithScenarios.work_item } };
            }
            try {
              const pipeline = await pipelineScheduler.start(parentID, ctx);
              return { content: [{ type: "text", text: JSON.stringify({ verification: result, pipeline }, null, 2) }], details: { verification: result, pipeline } };
            } catch (error) {
              const message = error instanceof Error ? error.message : String(error);
              return { content: [{ type: "text", text: `Child verified; parent scheduling blocked: ${message}` }], details: { verification: result, error: message }, isError: true };
            }
          }
        }
        if (!result.error && params.action === "accept_aggregate_work_item" && params.decision === "accepted") {
          try {
            const merged = await pipelineScheduler.mergeAggregate(params.id!, ctx);
            return { content: [{ type: "text", text: JSON.stringify(merged, null, 2) }], details: merged };
          } catch (error) {
            const message = error instanceof Error ? error.message : String(error);
            return { content: [{ type: "text", text: `Owner acceptance recorded; aggregate merge is pending: ${message}` }], details: { acceptance: result, error: message }, isError: true };
          }
        }
        if (!result.error && params.action === "review_blueprint_checkpoint") {
          const checkpoint = JSON.parse(params.content || "{}");
          const text = ["### CHECKPOINT", "- [x] Architecture matches expectations", "- [x] Design is appropriate (if UI)", "- [x] Requirements are complete (from RRI)", "- [x] Task decomposition is reasonable", "- [x] Nothing important is missing", "", "Contractor checkpoint passed. The Blueprint is ready for owner approval.", checkpoint.summary ? `\n${checkpoint.summary}` : ""].join("\n");
          return { content: [{ type: "text", text }], details: { checkpoint: result } };
        }
        let text = result.error
          ? `Error: ${result.error}`
          : contractPresentation
            ? contractPresentation
          : taskGraphPresentation
            ? taskGraphPresentation
          : blueprintPresentation
            ? blueprintPresentation
          : visionPresentation
            ? visionPresentation
          : rriPresentation
            ? rriPresentation
          : scanPresentation
            ? `Scan artifact ${result.id} saved. Ask the owner to approve or reject this Scan Report.`
            : JSON.stringify(result, null, 2);
        if (!result.error && params.id && ((params.action === "save_work_item_artifact" && params.stage === "scan") || params.action === "reject_work_item_scan")) {
          pipelineScheduler.finalizeHandoffs(params.id, "scan");
        }
        if (!result.error && params.action === "work_item_workflow_status" && ["aggregate_verification", "owner_acceptance", "merge_pending"].includes(result.next_stage)) {
          const data = execPic(["show", params.id!], ctx.cwd);
          if (data.error) text = `Error: ${data.error}`;
          else if (result.next_stage === "aggregate_verification") {
            try {
              // RRI-T save-before-execution (OB-5): when verification resumes,
              // reuse the persisted scenarios and only author when never saved;
              // submission later blocks unless the artifact is present.
              await ensureRriTScenariosArtifact(pipelineScheduler, data, ctx.cwd);
              text = buildAggregateVerifyPrompt(execPic(["show", params.id!], ctx.cwd));
            } catch (error) {
              const message = error instanceof Error ? error.message : String(error);
              return { content: [{ type: "text", text: `Aggregate verification blocked: ${message}` }], details: { ...result, error: message }, isError: true };
            }
          }
          else text = buildWorkItemContinuePrompt(result, data.work_item);
        }
  
        return {
          content: [{ type: "text", text }],
          details: scanPresentation ? { ...result, scanPresentation } : contractPresentation ? { ...result, contractPresentation } : taskGraphPresentation ? { ...result, taskGraphPresentation } : blueprintPresentation ? { ...result, blueprintPresentation } : visionPresentation ? { ...result, visionPresentation } : rriPresentation ? { ...result, rriPresentation } : result,
        };
      },
  
      renderCall(args, theme, _context) {
        let text = theme.fg("toolTitle", theme.bold("task_manager ")) + theme.fg("muted", args.action);

        if (args.title) text += ` "${args.title}"`;
        if (args.id) text += ` ${theme.fg("accent", args.id)}`;
        return new Text(text, 0, 0);
      },
  
      renderResult(result, _options, theme, _context) {
        const details = result.details as any;
        if (details?.error) {
          return new Text(theme.fg("error", details?.error || "Error"), 0, 0);
        }
        if (details?.previewPresentation) return new Markdown(details.previewPresentation, 0, 0, getMarkdownTheme());
        if (details?.rriPresentation) return new Markdown(details.rriPresentation, 0, 0, getMarkdownTheme());
        if (details?.contractPresentation) return new Markdown(details.contractPresentation, 0, 0, getMarkdownTheme());
        if (details?.taskGraphPresentation) return new Markdown(details.taskGraphPresentation, 0, 0, getMarkdownTheme());
        if (details?.blueprintPresentation) return new Markdown(details.blueprintPresentation, 0, 0, getMarkdownTheme());
        if (details?.visionPresentation) return new Markdown(details.visionPresentation, 0, 0, getMarkdownTheme());
        if (details?.scanPresentation) return new Markdown(details.scanPresentation, 0, 0, getMarkdownTheme());
        if (details?.id) {
          return new Text(theme.fg("success", `${details.id}`), 0, 0);
        }
        return new Text("Done", 0, 0);
      },
    });
}
