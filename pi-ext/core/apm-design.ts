/**
 * /apm design — technical design document with ADRs, adopted from
 * don-cheli-sdd dc:diseñar (ADR context/options/consequences, diagrams, and
 * the complexity table kept; complements /apm tech-plan which owns WHAT —
 * this owns HOW and WHY; output lands under .apm/design/).
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

export function handleDesign(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^design\b\s*/, "");
  if (!rest) {
    ctx.ui.notify(
      "Usage: /apm design <path-to-.plan.md | path-to-proposal | Design: <feature-slug>>",
      "error",
    );
    return;
  }
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-design.md").replace("{INPUT}", rest);
  sendHiddenPrompt(pi, "apm-design", prompt);
}
