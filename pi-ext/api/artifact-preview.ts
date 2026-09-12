import { parseCanonicalScanReportXml, renderScanReportMarkdown } from "../reporting/scan-report.ts";
import { parseRriReportJson, renderRriReportMarkdown } from "../reporting/rri-report.ts";
import { parseVisionReportJson, renderVisionReportMarkdown } from "../reporting/vision-report.ts";
import { parseBlueprintReportJson, renderBlueprintReportMarkdown } from "../reporting/blueprint-report.ts";
import { parseContractReportJson, renderContractReportMarkdown } from "../reporting/contract-report.ts";
import { parseTaskGraphReportJson, renderTaskGraphReportMarkdown } from "../reporting/task-graph-report.ts";
import type { TaskManagerParams } from "./task-manager-schema.ts";
import type { ToolOutcome } from "./blueprint-approval.ts";

export function previewArtifact(params: TaskManagerParams): ToolOutcome {
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
