import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { registerActivityTracker } from "./core/activity-tracker";
import { registerTaskCommand } from "./api/commands";
import { registerEventHandlers } from "./core/extension-helpers";
import { registerPipelineScheduler } from "./pipeline/pipeline-scheduler";
import { registerTaskAppCommand } from "./core/task-app-command";
import { registerTaskManagerTool } from "./api/tool";
import { registerAgentTrackerUI } from "./subagent/ui";
import { registerManagedBashTimeout } from "./core/worker-timeout";
import { registerWorkflowPrimer } from "./core/workflow-primer";

export default function (pi: ExtensionAPI) {
  registerWorkflowPrimer(pi);
  registerManagedBashTimeout(pi);
  registerEventHandlers(pi);
  const pipelineScheduler = registerPipelineScheduler(pi);
  registerTaskCommand(pi, pipelineScheduler);
  registerTaskManagerTool(pi, pipelineScheduler);
  registerAgentTrackerUI(pi);
  registerTaskAppCommand(pi);
  registerActivityTracker(pi);
}
