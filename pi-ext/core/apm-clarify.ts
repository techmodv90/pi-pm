/**
 * /apm clarify — ambiguity review + Auto-QA for a Gherkin spec, adopted from
 * don-cheli-sdd dc:clarificar (tags @borrador/@lista map to @draft/@ready;
 * requisitos.md maps to the requirements.md checklist /apm spec creates).
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

export function handleClarify(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^clarify\b\s*/, "");
  if (!rest) {
    ctx.ui.notify(
      "Usage: /apm clarify <path-to-.feature | Feature: <domain/Name>>",
      "error",
    );
    return;
  }
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-clarify.md").replace("{INPUT}", `- feature: ${rest}`);
  sendHiddenPrompt(pi, "apm-clarify", prompt);
}
