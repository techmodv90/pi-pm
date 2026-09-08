/**
 * /apm drift — spec vs implementation conformance check, adopted from
 * don-cheli-sdd dc:drift. Single canonical drift procedure: /apm implement
 * (GATE step) and /apm review (epic tier, before merge) inject it by
 * reference; CRÍTICO/WARNING findings route to Bug Work Items.
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

export function handleDrift(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^drift\b\s*/, "");
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-drift.md").replace("{INPUT}", rest || "whole project");
  sendHiddenPrompt(pi, "apm-drift", prompt);
}
