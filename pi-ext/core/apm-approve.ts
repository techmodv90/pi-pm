/**
 * /apm approve — flip an artifact's approval gate (hash-bound state), per the
 * gate-strengthening proposal: owner sees a short honest summary, confirms,
 * and the artifact gets `State: APPROVED hash=<sha1-of-body>` so downstream
 * commands (spec/breakdown/implement) can enforce the gate.
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

export function handleApprove(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^approve\b\s*/, "");
  if (!rest) {
    ctx.ui.notify(
      "Usage: /apm approve <path-to-proposal | path-to-design | path-to-.tasks.md>",
      "error",
    );
    return;
  }
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-approve.md").replace("{INPUT}", rest);
  sendHiddenPrompt(pi, "apm-approve", prompt);
}
