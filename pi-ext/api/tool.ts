import { execFileSync } from "node:child_process";
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { getMarkdownTheme } from "@mariozechner/pi-coding-agent";
import { Markdown, Text } from "@mariozechner/pi-tui";
import { Type } from "typebox";
import { StringEnum } from "@mariozechner/pi-ai";
import { execPic } from "../core/cli-helpers";
import { buildReviewContext } from "../tasking/settings";
import { buildAggregateVerifyPrompt, buildWorkItemContinuePrompt, buildWorkItemDebugPrompt } from "../tasking/work-item-prompts";
import { assertTaskManagerActionAllowed } from "../tasking/agent-capabilities.ts";
import { prepareCanonicalScanReportArtifact } from "../reporting/scan-report.ts";
import { deleteRriDraft, loadRriDraft, saveRriDraft, type RriDraftLineage } from "../core/rri-drafts.ts";

import { withInheritedParentWorkflowArtifacts } from "../tasking/task-artifacts.ts";
import type { PipelineScheduler } from "../pipeline/pipeline-scheduler.ts";

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

export function registerTaskManagerTool(pi: ExtensionAPI, pipelineScheduler: PipelineScheduler) {
    pi.registerTool({
      name: "task_manager",
      label: "Task Manager",
      description: "Manage canonical Work Items through the pic CLI.",
      promptSnippet: "Use Work Item actions for lifecycle mutations. Archived Task Items are read-only history.",
      parameters: Type.Object({
        action: StringEnum([
          "create_work_item", "update_work_item", "update_work_item_status", "list_work_items", "show_work_item", "ready_work_items", "claim_work_item", "add_work_item_labels", "remove_work_item_labels", "list_work_item_labels", "list_all_work_item_labels", "checkpoint_rri_interview", "load_rri_interview", "save_rri_interview",
          "save_work_item_artifact", "approve_work_item_artifact", "reject_work_item_scan", "reset_work_item_planning", "work_item_workflow_status", "validate_work_item_graph", "materialize_work_item", "authorize_work_item_implementation", "verify_work_item", "accept_work_item", "verify_aggregate_work_item", "accept_aggregate_work_item", "merge_aggregate_work_item", "close_aggregate_work_item",
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
        verification_status: Type.Optional(StringEnum(["passed", "failed", "partial", "blocked"] as const)),
        actor_role: Type.Optional(Type.String({ description: "Explicit actor role; owner-only actions require owner confirmation from the user" })),
        event_type: Type.Optional(Type.String({ description: "Debug trigger type" })),
        work_item_type: Type.Optional(StringEnum(["epic", "feature", "task", "bug", "chore", "gate"] as const)),
        parent_id: Type.Optional(Type.String({ description: "Parent aggregate Work Item ID" })),
        labels: Type.Optional(Type.Array(Type.String(), { description: "Work Item labels" })),
        stage: Type.Optional(StringEnum(["scan", "rri", "vision", "blueprint", "contracts", "task_graph"] as const)),
        artifact_id: Type.Optional(Type.String({ description: "Immutable Work Item artifact ID" })),
        completion_report_id: Type.Optional(Type.String({ description: "Current integrated Completion Report ID" })),
        verification_report_id: Type.Optional(Type.String({ description: "Current aggregate Verification Report ID" })),
        decision: Type.Optional(StringEnum(["accepted", "rejected"] as const)),
        change_type: Type.Optional(StringEnum(["contract", "environment", "runner", "artifact"] as const)),
        evidence_json: Type.Optional(Type.String({ description: "JSON evidence supporting a pipeline circuit reset" })),
        claimant: Type.Optional(Type.String({ description: "Worker or scheduler claiming the Work Item" })),
        deferrable: Type.Optional(Type.Boolean({ description: "Whether the Work Item is deferred" })),
      }),
  
      async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
        let args: string[] = [];
        let scanPresentation = "";
        let scanContent = "";

        try { assertTaskManagerActionAllowed(process.env.PI_TASK_AGENT_NAME, params.action as string, params.stage); }
        catch (error) {
          const message = error instanceof Error ? error.message : String(error);
          return { content: [{ type: "text", text: `Error: ${message}` }], details: {}, isError: true };
        }

        switch (params.action as string) {
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
            if (!params.id || !params.content) return { content: [{ type: "text", text: "Error: id and final RRI JSON content required" }], details: {}, isError: true };
            const result = execPic(["work-item", "rri-finalize", params.id, params.content], ctx.cwd);
            if (result.error) return { content: [{ type: "text", text: `Error: ${result.error}` }], details: result, isError: true };
            pipelineScheduler.finalizeHandoffs(params.id, "rri");
            return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: result };
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
            args = ["work-item", "artifact-save", params.id, params.stage, scanContent || params.content];
            break;
          }
          case "approve_work_item_artifact": {
            if (!params.id || !params.stage || !params.artifact_id) return { content: [{ type: "text", text: "Error: id, stage, and artifact_id required" }], details: {}, isError: true };
            if (params.actor_role !== "owner") return { content: [{ type: "text", text: "Error: actor_role must be owner after explicit owner approval" }], details: {}, isError: true };
            args = ["work-item", "artifact-approve", params.id, params.stage, params.artifact_id, params.stage === "scan" ? "accepted" : "approved"];
            break;
          }
          case "reset_work_item_planning": {
            if (!params.id || params.actor_role !== "owner") return { content: [{ type: "text", text: "Error: id and actor_role must be owner after explicit owner approval" }], details: {}, isError: true };
            args = ["work-item", "planning-reset", params.id, params.actor_role];
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
            args = ["work-item", "aggregate-verify", params.id, params.verification_status, params.summary || params.notes || "", "--actor-role", params.actor_role];
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
            args = ["workflow", "pipeline-circuit-reset", params.id, "--reason", params.notes, "--change-type", params.change_type, "--evidence-json", params.evidence_json, "--actor-role", params.actor_role];
            break;
          }
          case "work_on_work_item": {
            if (!params.id) return { content: [{ type: "text", text: "Error: id required" }], details: {}, isError: true };
            const data = execPic(["show", params.id], ctx.cwd);
            if (!data.work_item) return { content: [{ type: "text", text: `Error: ${data.error || "Work Item not found"}` }], details: {}, isError: true };
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
        )) deleteRriDraft(rriDraftRoot(ctx.cwd), params.id);
        if (!result.error && params.action === "create_work_item" && ["epic", "feature"].includes(params.work_item_type || "")) {
          const workflow = execPic(["work-item", "workflow-status", result.id], ctx.cwd);
          result.next_stage = workflow.next_stage;
          if (workflow.next_stage === "scan") {
            result.orchestration = await pipelineScheduler.start(result.id, ctx);
          }
        }
        if (!result.error && params.action === "verify_work_item" && params.verification_status === "passed") {
          const child = execPic(["show", params.id!], ctx.cwd);
          const parentID = child.work_item?.parent_id;
          if (parentID) {
            const parent = execPic(["show", parentID], ctx.cwd);
            const parentStatus = execPic(["work-item", "workflow-status", parentID], ctx.cwd);
            if (parentStatus.next_stage === "aggregate_verification") {
              return { content: [{ type: "text", text: buildAggregateVerifyPrompt(parent) }], details: { verification: result, next_stage: "aggregate_verification", work_item: parent.work_item } };
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
        let text = result.error
          ? `Error: ${result.error}`
          : scanPresentation
            ? `Scan artifact ${result.id} saved. Ask the owner to approve or reject this Scan Report.`
            : JSON.stringify(result, null, 2);
        if (!result.error && params.id && ((params.action === "save_work_item_artifact" && params.stage === "scan") || params.action === "reject_work_item_scan")) {
          pipelineScheduler.finalizeHandoffs(params.id, "scan");
        }
        if (!result.error && params.action === "work_item_workflow_status" && ["aggregate_verification", "owner_acceptance", "merge_pending"].includes(result.next_stage)) {
          const data = execPic(["show", params.id!], ctx.cwd);
          if (data.error) text = `Error: ${data.error}`;
          else if (result.next_stage === "aggregate_verification") text = buildAggregateVerifyPrompt(data);
          else text = buildWorkItemContinuePrompt(result, data.work_item);
        }
  
        return {
          content: [{ type: "text", text }],
          details: scanPresentation ? { ...result, scanPresentation } : result,
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
        if (details?.scanPresentation) return new Markdown(details.scanPresentation, 0, 0, getMarkdownTheme());
        if (details?.id) {
          return new Text(theme.fg("success", `${details.id}`), 0, 0);
        }
        return new Text("Done", 0, 0);
      },
    });
}
