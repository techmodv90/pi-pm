/**
 * /apm explore — codebase investigation before proposals, adopted from
 * don-cheli-sdd dc:explorar (Assumptions mode default; findings land under
 * .apm/explore/ as evidence-cited research that /apm propose must stay
 * consistent with).
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

export function handleExplore(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^explore\b\s*/, "");
  // Loaded at call time so prompt edits apply without an extension reload.
  // Empty input is valid: the prompt investigates overall architecture.
  const prompt = loadPrompt("apm-explore.md").replace("{INPUT}", rest);
  sendHiddenPrompt(pi, "apm-explore", prompt);
}
