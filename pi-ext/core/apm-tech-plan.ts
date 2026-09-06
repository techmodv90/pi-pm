/**
 * /apm tech-plan — technical blueprint (.plan.md) from a clarified Gherkin
 * spec, adopted from don-cheli-sdd dc:planificar-tecnico (tag @lista maps to
 * @ready; constitution check maps to the APM invariants; output lands beside
 * the spec under .apm/specs/ as a discovery artifact feeding the Work Item
 * flow, never replacing the Blueprint).
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

export function handleTechPlan(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^tech-plan\b\s*/, "");
  if (!rest) {
    ctx.ui.notify(
      "Usage: /apm tech-plan <path-to-.feature | Feature: <domain/Name>>",
      "error",
    );
    return;
  }
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-tech-plan.md").replace("{INPUT}", rest);
  sendHiddenPrompt(pi, "apm-tech-plan", prompt);
}
