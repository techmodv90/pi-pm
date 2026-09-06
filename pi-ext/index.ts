import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { registerActivityTracker } from "./core/activity-tracker";
import { registerTaskCommand } from "./api/commands";
import { registerEventHandlers } from "./core/extension-helpers";
import { registerPipelineScheduler } from "./pipeline/pipeline-scheduler";
import { registerTaskAppCommand } from "./core/task-app-command";
import { registerApmCommand } from "./core/apm-command";
import { registerTaskManagerTool } from "./api/tool";
import { registerAgentTrackerUI } from "./subagent/ui";
import { registerManagedBashTimeout } from "./core/worker-timeout";
import { registerSkillLoader } from "./core/skill-loader";
import { registerWorkflowPrimer } from "./core/workflow-primer";
import { registerApmApprovalTool } from "./core/apm-approval";

export default function (pi: ExtensionAPI) {
  registerSkillLoader(pi);
  registerWorkflowPrimer(pi);
  registerManagedBashTimeout(pi);
  registerEventHandlers(pi);
  const pipelineScheduler = registerPipelineScheduler(pi);
  registerTaskCommand(pi, pipelineScheduler);
  registerTaskManagerTool(pi, pipelineScheduler);
  registerAgentTrackerUI(pi);
  registerTaskAppCommand(pi);
  registerApmCommand(pi);
  registerApmApprovalTool(pi);
  registerActivityTracker(pi);
}
