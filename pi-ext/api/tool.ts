import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { getMarkdownTheme } from "@mariozechner/pi-coding-agent";
import { Markdown, Text } from "@mariozechner/pi-tui";
import { execPic } from "../core/cli-helpers.ts";
import { buildAggregateVerifyPrompt, buildWorkItemContinuePrompt } from "../tasking/work-item-prompts.ts";
import { assertTaskManagerActionAllowed } from "../tasking/agent-capabilities.ts";
import { prepareCanonicalScanReportArtifact } from "../reporting/scan-report.ts";
import { parseBlueprintReportJson, renderBlueprintReportMarkdown } from "../reporting/blueprint-report.ts";
import { parseVisionReportJson, renderVisionReportMarkdown } from "../reporting/vision-report.ts";
import { parseContractReportJson, renderContractReportMarkdown } from "../reporting/contract-report.ts";
import { parseTaskGraphReportJson, renderTaskGraphReportMarkdown } from "../reporting/task-graph-report.ts";
import { deleteRriDraft } from "../core/rri-drafts.ts";
import { deleteBlueprintDraft, deletePlanReviewState, loadBlueprintDraft, loadLatestBlueprintDraft, saveBlueprintDraft } from "../core/blueprint-drafts.ts";
import { currentApprovedPlanningArtifact } from "../tasking/task-artifacts.ts";
import { runnerRepairEvidence, type PipelineScheduler } from "../pipeline/pipeline-scheduler.ts";
import { taskManagerParameters } from "./task-manager-schema.ts";
import { aggregateGitEvidence, rriDraftRoot } from "./task-manager-helpers.ts";
import { approveBlueprintDraft, reviewBlueprintCheckpoint } from "./blueprint-approval.ts";
import { checkpointRriInterview, loadRriInterview, saveRriInterview } from "./rri-interview.ts";
import { bindPipelineDispatchAction, completePipelineDispatchAction, listPipelineDispatchesAction } from "./dispatch-actions.ts";
import { debugWorkItem, triggerWorkItemReview, workOnWorkItem } from "./review-actions.ts";
import { previewArtifact } from "./artifact-preview.ts";
import { acceptAggregateWorkItem, mergeAggregateWorkItem, verifyAggregateWorkItem } from "./aggregate-actions.ts";

export function registerTaskManagerTool(pi: ExtensionAPI, pipelineScheduler: PipelineScheduler) {
    pi.registerTool({
      name: "task_manager",
      label: "Task Manager",
      description: "Manage canonical Work Items through the pic CLI.",
      promptSnippet: "Use Work Item actions for lifecycle mutations. Archived Task Items are read-only history.",
      parameters: taskManagerParameters,
  
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
          case "checkpoint_rri_interview": return checkpointRriInterview(ctx, params);
          case "load_rri_interview": return loadRriInterview(ctx, params);
          case "save_rri_interview": return saveRriInterview(pipelineScheduler, ctx, params);
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
          case "preview_artifact": return previewArtifact(params);
          case "approve_work_item_artifact": {
            if (!params.id || !params.stage || !params.artifact_id) return { content: [{ type: "text", text: "Error: id, stage, and artifact_id required" }], details: {}, isError: true };
            if (params.actor_role !== "owner") return { content: [{ type: "text", text: "Error: actor_role must be owner after explicit owner approval" }], details: {}, isError: true };
            args = ["work-item", "artifact-approve", params.id, params.stage, params.artifact_id, params.stage === "scan" ? "accepted" : "approved"];
            break;
          }
          case "review_blueprint_checkpoint": return await reviewBlueprintCheckpoint(pi, ctx, params);
          case "approve_blueprint_draft": return await approveBlueprintDraft(pi, ctx, params);
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
            const prepared = verifyAggregateWorkItem(ctx, params);
            if ("args" in prepared) args = prepared.args;
            else return prepared;
            break;
          }
          case "accept_aggregate_work_item": {
            const prepared = acceptAggregateWorkItem(ctx, params);
            if ("args" in prepared) args = prepared.args;
            else return prepared;
            break;
          }
          case "merge_aggregate_work_item": return await mergeAggregateWorkItem(pipelineScheduler, ctx, params);
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
          case "debug_work_item": return debugWorkItem(ctx, params);
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
          case "list_pipeline_dispatches": return listPipelineDispatchesAction(pipelineScheduler);
          case "bind_pipeline_dispatch": return bindPipelineDispatchAction(pipelineScheduler, params);
          case "complete_pipeline_dispatch": return await completePipelineDispatchAction(pipelineScheduler, params);
          case "work_on_work_item": return await workOnWorkItem(pipelineScheduler, ctx, params);
          case "dry_run_work_item": {
            if (!params.id) return { content: [{ type: "text", text: "Error: id required" }], details: {}, isError: true };
            const result = pipelineScheduler.dryRun(params.id, ctx);
            return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }], details: { action: "dry_run_work_item", ...result } };
          }
          case "trigger_work_item_review": return triggerWorkItemReview(ctx, params);
        }
  
        const result = execPic(args, ctx.cwd);
        if (!result.error && params.id && (
          (params.action === "approve_work_item_artifact" && params.stage === "rri")
          || params.action === "reset_work_item_planning"
          || (params.action === "update_work_item_status" && params.status === "cancelled")
        )) {
          deleteRriDraft(rriDraftRoot(ctx.cwd), params.id);
          deleteBlueprintDraft(ctx.cwd, params.id);
          deletePlanReviewState(ctx.cwd, params.id);
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
            const parentStatus = execPic(["work-item", "workflow-status", parentID], ctx.cwd);
            if (parentStatus.next_stage === "aggregate_verification") {
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
          else if (result.next_stage === "aggregate_verification") text = buildAggregateVerifyPrompt(data);
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
