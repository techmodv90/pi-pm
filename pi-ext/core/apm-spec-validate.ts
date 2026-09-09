/**
 * /apm spec-validate — pre-planning spec quality gate, adopted from
 * don-cheli-sdd dc:validar-spec. Gate verdict only; the 11-dimension
 * analysis is the canonical procedure in apm-spec-quality.md, shared with
 * /apm spec-score.
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

export function handleSpecValidate(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^spec-validate\b\s*/, "");
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-spec-validate.md").replace("{INPUT}", rest || "all active specs");
  sendHiddenPrompt(pi, "apm-spec-validate", prompt);
}
