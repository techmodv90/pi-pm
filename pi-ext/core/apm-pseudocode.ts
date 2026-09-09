/**
 * /apm pseudocode — technology-agnostic logic sketch between spec and design,
 * adopted from don-cheli-sdd dc:pseudocodigo (SPARC "P": flows as
 * WHEN/IF/ELSE/FOR-EACH pseudocode, invariants, edge cases; output lands
 * under .apm/pseudocode/; feeds /apm design / tech-plan).
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

export function handlePseudocode(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^pseudocode\b\s*/, "");
  if (!rest) {
    ctx.ui.notify(
      "Usage: /apm pseudocode <domain directory | path-to-.feature | Pseudocode: <slug>>",
      "error",
    );
    return;
  }
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-pseudocode.md").replace("{INPUT}", rest);
  sendHiddenPrompt(pi, "apm-pseudocode", prompt);
}
