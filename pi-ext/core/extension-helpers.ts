import type { ExtensionAPI, ExtensionContext } from "@mariozechner/pi-coding-agent";
import { execPic, hasDb } from "./cli-helpers.ts";

/**
 * Update footer status with in-progress tasks.
 * Expects an extension context with cwd/ui and updates the tasks status widget.
 */
export function updateStatus(ctx: ExtensionContext) {
  if (!hasDb(ctx.cwd)) { ctx.ui.setStatus("tasks", undefined); return; }
  const data = execPic(["list"], ctx.cwd);
  if (!Array.isArray(data)) { ctx.ui.setStatus("tasks", undefined); return; }
  const inProgress = data.filter((t: any) => t.status === "in_progress");
  if (inProgress.length === 0) { ctx.ui.setStatus("tasks", undefined); return; }
  const text = inProgress.map((t: any) => `🔵 ${t.title}`).join("  •  ");
  ctx.ui.setStatus("tasks", text);
}

/**
 * Register task-system lifecycle handlers.
 * Expects the pi extension API and installs status refresh without model switching.
 */
export function registerEventHandlers(_pi: ExtensionAPI) {}
