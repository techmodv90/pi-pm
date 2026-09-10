/**
 * /apm start — complexity assessment and pipeline entry, adopted from
 * don-cheli-sdd dc:comenzar: score scope/unknowns/risk/duration 0-4, level =
 * max (conservative), PoC detection, PRD detection, route to the mapped APM
 * command pipeline. English labels per the adoption policy.
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

export function handleStart(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^start\b\s*/, "");
  if (!rest) {
    ctx.ui.notify("Usage: /apm start <task description>", "error");
    return;
  }
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-start.md").replace("{INPUT}", rest);
  sendHiddenPrompt(pi, "apm-start", prompt);
}
