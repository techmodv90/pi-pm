/**
 * /apm breakdown — executable task list (.tasks.md) from an approved technical
 * blueprint, adopted from don-cheli-sdd dc:desglosar (TDD RED→GREEN markers
 * and [P] parallelism kept; US tags map to the spec's .feature scenarios
 * (Scenario Map), restoring dc:desglosar's per-story traceability and
 * cross-story parallelism; output lands beside the plan under .apm/specs/ as a
 * discovery artifact informing the Work Item's Task Graph, never authoring it).
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

export function handleBreakdown(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^breakdown\b\s*/, "");
  if (!rest) {
    ctx.ui.notify(
      "Usage: /apm breakdown <path-to-.plan.md | Blueprint: <domain/Name>>",
      "error",
    );
    return;
  }
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-breakdown.md").replace("{INPUT}", rest);
  sendHiddenPrompt(pi, "apm-breakdown", prompt);
}
