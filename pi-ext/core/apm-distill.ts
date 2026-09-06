/**
 * /apm distill — blueprint distillation from existing code (.distilled.md),
 * adopted from don-cheli-sdd dc:destilar (DeepCode Blueprint Distillation):
 * reverse-engineer compact specs/contracts/business rules from a module.
 * Output is a discovery/reference artifact under .apm/specs/ — never a
 * requirement source or pipeline authority.
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

export function handleDistill(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^distill\b\s*/, "");
  // Empty input is allowed: distills the current project (module: .).
  const moduleArg = rest || ".";
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-distill.md").replace("{INPUT}", moduleArg);
  sendHiddenPrompt(pi, "apm-distill", prompt);
}
