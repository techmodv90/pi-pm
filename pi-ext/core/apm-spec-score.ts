/**
 * /apm spec-score — quantitative spec quality measurement (0–100, 11
 * weighted dimensions), adopted from don-cheli-sdd dc:spec-score.
 * Measurement only, never a gate; the analysis is the canonical procedure
 * in apm-spec-quality.md, shared with /apm spec-validate.
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

export function handleSpecScore(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^spec-score\b\s*/, "");
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-spec-score.md").replace("{INPUT}", rest || "whole project");
  sendHiddenPrompt(pi, "apm-spec-score", prompt);
}
