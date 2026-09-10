/**
 * /apm archive — close completed specs, adopted from don-cheli-sdd dc:archivar
 * (spec archiving phase): verify the @implemented tag, move feature artifacts
 * to .apm/specs/archive/<domain>/<Feature>/, write metadata.json, and record
 * the decision in .apm/decisions.md. English labels per the adoption policy.
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

export function handleArchive(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^archive\b\s*/, "");
  if (!rest) {
    ctx.ui.notify(
      "Usage: /apm archive <path-to-.feature | Feature: <domain/Name> | --all>",
      "error",
    );
    return;
  }
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-archive.md").replace("{INPUT}", rest);
  sendHiddenPrompt(pi, "apm-archive", prompt);
}
