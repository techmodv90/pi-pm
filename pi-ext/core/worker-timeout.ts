import { isToolCallEventType, type ExtensionAPI } from "@mariozechner/pi-coding-agent";

export const MANAGED_BASH_TIMEOUT_MS = 10 * 60 * 1000;

export function registerManagedBashTimeout(pi: ExtensionAPI): void {
  if (!process.env.PI_TASK_PARENT_RUN_ID) return;
  pi.on("tool_call", (event) => {
    if (!isToolCallEventType("bash", event)) return;
    event.input.timeout = Math.min(event.input.timeout || MANAGED_BASH_TIMEOUT_MS, MANAGED_BASH_TIMEOUT_MS);
  });
}