/**
 * /apm propose — change proposal (RFC) before specification, adopted from
 * don-cheli-sdd dc:proponer (Intent/Scope/Approach/Risks kept; output lands
 * under .apm/proposals/ as a discovery artifact; the gate stays — spec is
 * only written after owner approval).
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

export function handlePropose(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^propose\b\s*/, "");
  if (!rest) {
    ctx.ui.notify(
      "Usage: /apm propose <description of the change | path-to-brief | Proposal: <slug>>",
      "error",
    );
    return;
  }
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-propose.md").replace("{INPUT}", rest);
  sendHiddenPrompt(pi, "apm-propose", prompt);
}
